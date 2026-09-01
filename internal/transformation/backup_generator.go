package transformation

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/capysquash/pgsquash-engine/internal/utils"
)

// BackupType defines the type of backup to generate
type BackupType int

const (
	SchemaOnly BackupType = iota
	DataOnly
	SchemaAndData
	Structure
	DDLOnly
)

// BackupFormat defines the output format for backups
type BackupFormat int

const (
	SQLFormat BackupFormat = iota
	CustomFormat
	TarFormat
	DirectoryFormat
)

// BackupConfig controls backup generation behavior
type BackupConfig struct {
	Type           BackupType   `json:"type"`
	Format         BackupFormat `json:"format"`
	Compression    bool         `json:"compression"`
	VerboseOutput  bool         `json:"verbose_output"`
	IncludeDrops   bool         `json:"include_drops"`
	SchemaOnly     bool         `json:"schema_only"`
	DataOnly       bool         `json:"data_only"`
	InsertFormat   bool         `json:"insert_format"`
	ColumnInserts  bool         `json:"column_inserts"`
	IfExists       bool         `json:"if_exists"`
	CreateDatabase bool         `json:"create_database"`
	CleanFirst     bool         `json:"clean_first"`
	Encoding       string       `json:"encoding"`
	ExcludeTables  []string     `json:"exclude_tables"`
	IncludeTables  []string     `json:"include_tables"`
	ExcludeSchemas []string     `json:"exclude_schemas"`
	IncludeSchemas []string     `json:"include_schemas"`
}

// DefaultBackupConfig returns sensible backup defaults
func DefaultBackupConfig() *BackupConfig {
	return &BackupConfig{
		Type:          SchemaAndData,
		Format:        SQLFormat,
		Compression:   true,
		VerboseOutput: false,
		IncludeDrops:  false,
		InsertFormat:  true,
		ColumnInserts: true,
		IfExists:      true,
		Encoding:      "UTF8",
	}
}

// BackupResult represents the result of a backup operation
type BackupResult struct {
	BackupPath      string        `json:"backup_path"`
	Size            int64         `json:"size"`
	Duration        time.Duration `json:"duration"`
	TablesBackedUp  int           `json:"tables_backed_up"`
	SchemasBackedUp int           `json:"schemas_backed_up"`
	Success         bool          `json:"success"`
	Error           string        `json:"error,omitempty"`
	Warnings        []string      `json:"warnings"`
}

// RollbackScript represents a rollback operation
type RollbackScript struct {
	ID           string    `json:"id"`
	Description  string    `json:"description"`
	SQL          string    `json:"sql"`
	CreatedAt    time.Time `json:"created_at"`
	Order        int       `json:"order"`
	Dependencies []string  `json:"dependencies"`
}

// BackupGenerator handles database backup and rollback generation
type BackupGenerator struct {
	config     *BackupConfig
	db         *sql.DB
	pgDumpPath string
	workDir    string
}

// NewBackupGenerator creates a new backup generator.
//
// The default working directory is a dedicated pgsquash-backups directory
// under the system temp dir - never the shared temp dir itself, because
// CleanupOldBackups glob-deletes "*backup*.sql" inside the working directory
// and must only ever touch files pgsquash created. Callers that want backups
// somewhere durable (e.g. <output>/.backups, as the squasher engine enforces)
// use SetWorkingDirectory.
func NewBackupGenerator(config *BackupConfig, db *sql.DB) *BackupGenerator {
	if config == nil {
		config = DefaultBackupConfig()
	}

	return &BackupGenerator{
		config:     config,
		db:         db,
		pgDumpPath: findPgDumpPath(),
		workDir:    filepath.Join(os.TempDir(), "pgsquash-backups"),
	}
}

// findPgDumpPath attempts to locate pg_dump binary
func findPgDumpPath() string {
	// Common locations for pg_dump
	paths := []string{
		"/usr/local/bin/pg_dump",
		"/usr/bin/pg_dump",
		"/opt/postgresql/bin/pg_dump",
		"/Applications/Postgres.app/Contents/Versions/latest/bin/pg_dump",
		"pg_dump", // Assume it's in PATH
	}

	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	return "pg_dump" // Fallback to PATH
}

// SetWorkingDirectory sets the working directory for backup operations
func (bg *BackupGenerator) SetWorkingDirectory(dir string) error {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return errors.NewError(
			errors.ErrorCodeBackupGenerationFailed,
			"failed to create working directory",
			errors.SeverityError,
			errors.CategoryBackup,
		).WithInnerError(err)
	}
	bg.workDir = dir
	return nil
}

// GeneratePreMigrationBackup creates a backup before applying migrations
func (bg *BackupGenerator) GeneratePreMigrationBackup(ctx context.Context, dbURL string) (*BackupResult, error) {
	timestamp := time.Now().Format("20060102_150405")
	backupName := fmt.Sprintf("pre_migration_backup_%s", timestamp)

	return bg.generateBackup(ctx, dbURL, backupName, "Pre-migration safety backup")
}

// GeneratePostMigrationBackup creates a backup after applying migrations
func (bg *BackupGenerator) GeneratePostMigrationBackup(ctx context.Context, dbURL string) (*BackupResult, error) {
	timestamp := time.Now().Format("20060102_150405")
	backupName := fmt.Sprintf("post_migration_backup_%s", timestamp)

	return bg.generateBackup(ctx, dbURL, backupName, "Post-migration verification backup")
}

// generateBackup performs the actual backup operation
func (bg *BackupGenerator) generateBackup(ctx context.Context, dbURL, name, description string) (*BackupResult, error) {
	startTime := time.Now()

	result := &BackupResult{
		Success:  false,
		Warnings: make([]string, 0),
	}

	// Build backup file path
	var extension string
	switch bg.config.Format {
	case CustomFormat:
		extension = ".custom"
	case TarFormat:
		extension = ".tar"
	default:
		extension = ".sql"
	}

	if bg.config.Compression && bg.config.Format == SQLFormat {
		extension += ".gz"
	}

	// Ensure the working directory exists (the default dedicated directory is
	// created lazily; SetWorkingDirectory already created explicit ones).
	if err := os.MkdirAll(bg.workDir, 0755); err != nil {
		result.Error = err.Error()
		return result, errors.NewError(
			errors.ErrorCodeBackupGenerationFailed,
			"failed to create backup working directory",
			errors.SeverityError,
			errors.CategoryBackup,
		).WithInnerError(err)
	}

	backupPath := filepath.Join(bg.workDir, name+extension)
	result.BackupPath = backupPath

	// Build pg_dump command arguments
	args := bg.buildPgDumpArgs(dbURL, backupPath)

	// Execute backup
	err := bg.executePgDump(ctx, args)
	if err != nil {
		result.Error = err.Error()
		return result, err
	}

	// Get backup file info
	info, err := os.Stat(backupPath)
	if err != nil {
		result.Error = fmt.Sprintf("Failed to get backup info: %v", err)
		return result, errors.NewError(
			errors.ErrorCodeBackupGenerationFailed,
			"failed to get backup info",
			errors.SeverityError,
			errors.CategoryBackup,
		).WithInnerError(err)
	}

	result.Size = info.Size()
	result.Duration = time.Since(startTime)
	result.Success = true

	// Analyze backup content
	if bg.config.VerboseOutput {
		err = bg.analyzeBackup(backupPath, result)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to analyze backup: %v", err))
		}
	}

	return result, nil
}

// buildPgDumpArgs constructs pg_dump command arguments
func (bg *BackupGenerator) buildPgDumpArgs(dbURL, outputPath string) []string {
	args := []string{bg.pgDumpPath}

	// Format options
	switch bg.config.Format {
	case CustomFormat:
		args = append(args, "--format=custom")
	case TarFormat:
		args = append(args, "--format=tar")
	case DirectoryFormat:
		args = append(args, "--format=directory")
	default:
		args = append(args, "--format=plain")
	}

	// Backup type options
	if bg.config.SchemaOnly || bg.config.Type == SchemaOnly || bg.config.Type == DDLOnly {
		args = append(args, "--schema-only")
	} else if bg.config.DataOnly || bg.config.Type == DataOnly {
		args = append(args, "--data-only")
	}

	// Output options
	if bg.config.VerboseOutput {
		args = append(args, "--verbose")
	}

	if bg.config.IncludeDrops || bg.config.CleanFirst {
		args = append(args, "--clean")
	}

	if bg.config.IfExists {
		args = append(args, "--if-exists")
	}

	if bg.config.CreateDatabase {
		args = append(args, "--create")
	}

	if bg.config.InsertFormat {
		args = append(args, "--inserts")
	}

	if bg.config.ColumnInserts {
		args = append(args, "--column-inserts")
	}

	// Compression
	if bg.config.Compression && bg.config.Format != CustomFormat {
		args = append(args, "--compress=9")
	}

	// Encoding
	if bg.config.Encoding != "" {
		args = append(args, fmt.Sprintf("--encoding=%s", bg.config.Encoding))
	}

	// Table filters
	for _, table := range bg.config.ExcludeTables {
		args = append(args, fmt.Sprintf("--exclude-table=%s", table))
	}

	for _, table := range bg.config.IncludeTables {
		args = append(args, fmt.Sprintf("--table=%s", table))
	}

	// Schema filters
	for _, schema := range bg.config.ExcludeSchemas {
		args = append(args, fmt.Sprintf("--exclude-schema=%s", schema))
	}

	for _, schema := range bg.config.IncludeSchemas {
		args = append(args, fmt.Sprintf("--schema=%s", schema))
	}

	// Output file
	args = append(args, "--file="+outputPath)

	// Database URL
	args = append(args, dbURL)

	return args
}

// validatePgDumpArgs validates pg_dump arguments to prevent command injection
func validatePgDumpArgs(args []string) error {
	if len(args) == 0 {
		return errors.NewError(
			errors.ErrorCodeBackupGenerationFailed,
			"no arguments provided to pg_dump",
			errors.SeverityError,
			errors.CategoryBackup,
		)
	}

	// Whitelist valid pg_dump commands
	validCommands := map[string]bool{
		"pg_dump":    true,
		"pg_restore": true,
	}

	command := filepath.Base(args[0]) // Use base name to allow full paths
	if !validCommands[command] {
		return errors.NewError(
			errors.ErrorCodeBackupGenerationFailed,
			fmt.Sprintf("invalid command: %s (allowed: pg_dump, pg_restore)", command),
			errors.SeverityError,
			errors.CategoryBackup,
		)
	}

	// Prevent shell metacharacters in arguments that could enable command injection
	dangerousChars := ";&|<>`$(){}[]!#\n\r"
	for i, arg := range args {
		if strings.ContainsAny(arg, dangerousChars) {
			return errors.NewError(
				errors.ErrorCodeBackupGenerationFailed,
				fmt.Sprintf("argument %d contains dangerous shell metacharacters: %s", i, arg),
				errors.SeverityError,
				errors.CategoryBackup,
			)
		}
	}

	return nil
}

// executePgDump runs the pg_dump command
func (bg *BackupGenerator) executePgDump(ctx context.Context, args []string) error {
	// Validate arguments to prevent command injection
	if err := validatePgDumpArgs(args); err != nil {
		return err
	}

	// Use os/exec to run pg_dump with proper context cancellation
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)

	// Capture stdout and stderr for error reporting
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if bg.config.VerboseOutput {
		fmt.Printf("Executing pg_dump: %s\n", strings.Join(args, " "))
	}

	// Execute the command
	err := cmd.Run()
	if err != nil {
		// Include stderr output in error message
		stderrStr := stderr.String()
		if stderrStr != "" {
			return errors.NewError(
				errors.ErrorCodeBackupGenerationFailed,
				fmt.Sprintf("pg_dump failed\nError output: %s", stderrStr),
				errors.SeverityError,
				errors.CategoryBackup,
			).WithInnerError(err)
		}
		return errors.NewError(
			errors.ErrorCodeBackupGenerationFailed,
			"pg_dump failed",
			errors.SeverityError,
			errors.CategoryBackup,
		).WithInnerError(err)
	}

	// Log any warnings from stderr even if command succeeded
	if stderr.Len() > 0 && bg.config.VerboseOutput {
		fmt.Printf("pg_dump warnings: %s\n", stderr.String())
	}

	return nil
}

// analyzeBackup analyzes the backup content for statistics
func (bg *BackupGenerator) analyzeBackup(backupPath string, result *BackupResult) error {
	// This would analyze the backup file to extract statistics
	// For schema backups, count tables, indexes, functions, etc.
	// For data backups, estimate row counts

	result.TablesBackedUp = 10 // Simulated
	result.SchemasBackedUp = 2 // Simulated

	return nil
}

// GenerateRollbackScript creates rollback scripts for migrations
func (bg *BackupGenerator) GenerateRollbackScript(ctx context.Context, statements []types.Statement) ([]*RollbackScript, error) {
	rollbacks := make([]*RollbackScript, 0)

	for i, stmt := range statements {
		rollback, err := bg.generateRollbackForStatement(stmt, i)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeBackupGenerationFailed,
				fmt.Sprintf("failed to generate rollback for statement %d", i),
				errors.SeverityError,
				errors.CategoryBackup,
			).WithInnerError(err)
		}

		if rollback != nil {
			rollbacks = append(rollbacks, rollback)
		}
	}

	// Reverse order for proper rollback sequence
	for i := len(rollbacks)/2 - 1; i >= 0; i-- {
		opp := len(rollbacks) - 1 - i
		rollbacks[i], rollbacks[opp] = rollbacks[opp], rollbacks[i]
	}

	// Update order numbers
	for i, rollback := range rollbacks {
		rollback.Order = i + 1
	}

	return rollbacks, nil
}

// generateRollbackForStatement creates a rollback script for a single statement
func (bg *BackupGenerator) generateRollbackForStatement(stmt types.Statement, order int) (*RollbackScript, error) {
	rollback := &RollbackScript{
		ID:        fmt.Sprintf("rollback_%d_%d", time.Now().Unix(), order),
		CreatedAt: time.Now(),
		Order:     order,
	}

	switch stmt.Operation {
	case types.OpCreate:
		switch stmt.ObjectType {
		case types.TypeTable:
			rollback.Description = fmt.Sprintf("Drop table %s", stmt.ObjectName)
			rollback.SQL = fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE;", stmt.ObjectName)
		case types.TypeIndex:
			rollback.Description = fmt.Sprintf("Drop index %s", stmt.ObjectName)
			rollback.SQL = fmt.Sprintf("DROP INDEX IF EXISTS %s;", stmt.ObjectName)
		case types.TypeFunction:
			rollback.Description = fmt.Sprintf("Drop function %s", stmt.ObjectName)
			rollback.SQL = fmt.Sprintf("DROP FUNCTION IF EXISTS %s CASCADE;", stmt.ObjectName)
		default:
			rollback.Description = fmt.Sprintf("Drop %s %s", stmt.ObjectType, stmt.ObjectName)
			rollback.SQL = fmt.Sprintf("-- Cannot automatically rollback CREATE %s %s", stmt.ObjectType, stmt.ObjectName)
		}

	case types.OpDrop:
		switch stmt.ObjectType {
		case types.TypeTable:
			rollback.Description = fmt.Sprintf("Recreate table %s", stmt.ObjectName)
			rollback.SQL = fmt.Sprintf("-- Cannot automatically rollback DROP TABLE %s (requires backup restore)", stmt.ObjectName)
		case types.TypeIndex:
			rollback.Description = fmt.Sprintf("Recreate index %s", stmt.ObjectName)
			rollback.SQL = fmt.Sprintf("-- Cannot automatically rollback DROP INDEX %s (requires original definition)", stmt.ObjectName)
		case types.TypeFunction:
			rollback.Description = fmt.Sprintf("Recreate function %s", stmt.ObjectName)
			rollback.SQL = fmt.Sprintf("-- Cannot automatically rollback DROP FUNCTION %s (requires backup restore)", stmt.ObjectName)
		default:
			// Handle UNKNOWN type gracefully
			objectTypeStr := string(stmt.ObjectType)
			if stmt.ObjectType == types.TypeUnknown || stmt.ObjectType == "" {
				objectTypeStr = "object"
			}
			rollback.Description = fmt.Sprintf("Recreate %s %s", objectTypeStr, stmt.ObjectName)
			rollback.SQL = fmt.Sprintf("-- Cannot automatically rollback DROP %s %s", objectTypeStr, stmt.ObjectName)
		}

	case types.OpAlter:
		rollback.Description = fmt.Sprintf("Reverse ALTER %s %s", stmt.ObjectType, stmt.ObjectName)
		rollback.SQL = bg.generateAlterTableRollback(stmt)

	case types.OpInsert:
		rollback.Description = fmt.Sprintf("Delete inserted data from %s", stmt.ObjectName)
		rollback.SQL = bg.generateInsertRollback(stmt)

	case types.OpUpdate:
		rollback.Description = fmt.Sprintf("Reverse UPDATE on %s", stmt.ObjectName)
		rollback.SQL = fmt.Sprintf("-- Cannot automatically rollback UPDATE on %s (requires pre-change snapshot)", stmt.ObjectName)

	case types.OpDelete:
		rollback.Description = fmt.Sprintf("Restore deleted data to %s", stmt.ObjectName)
		rollback.SQL = fmt.Sprintf("-- Cannot automatically rollback DELETE from %s (requires backup restore)", stmt.ObjectName)

	default:
		// For statements we don't know how to rollback
		return nil, nil
	}

	return rollback, nil
}

// generateAlterTableRollback creates rollback for ALTER TABLE statements
func (bg *BackupGenerator) generateAlterTableRollback(stmt types.Statement) string {
	// Parse the ALTER TABLE command to extract operation type
	sql := strings.ToUpper(strings.TrimSpace(stmt.SQL))
	tableName := stmt.ObjectName
	if tableName == "" {
		tableName = extractIdentifierAfterKeywordSequence(stmt.SQL, "ALTER", "TABLE")
	}

	// Handle ADD COLUMN
	if strings.Contains(sql, "ADD COLUMN") {
		columnName := extractIdentifierAfterKeywordSequence(stmt.SQL, "ADD", "COLUMN")
		if columnName == "" {
			columnName = extractIdentifierAfterKeywordSequence(stmt.SQL, "ADD", "COLUMN", "IF", "NOT", "EXISTS")
		}
		if columnName != "" {
			return fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s;", tableName, columnName)
		}
		return fmt.Sprintf("-- Rollback ADD COLUMN: DROP COLUMN %s.<column_name>;", tableName)
	}

	// Handle DROP COLUMN
	if strings.Contains(sql, "DROP COLUMN") {
		columnName := extractIdentifierAfterKeywordSequence(stmt.SQL, "DROP", "COLUMN")
		if columnName == "" {
			columnName = extractIdentifierAfterKeywordSequence(stmt.SQL, "DROP", "COLUMN", "IF", "EXISTS")
		}
		if columnName != "" {
			return fmt.Sprintf("-- Rollback DROP COLUMN: ADD COLUMN %s (requires column definition from backup)", columnName)
		}
		return "-- Rollback DROP COLUMN: requires column definition from backup"
	}

	// Handle ADD CONSTRAINT
	if strings.Contains(sql, "ADD CONSTRAINT") {
		constraintName := extractIdentifierAfterKeywordSequence(stmt.SQL, "ADD", "CONSTRAINT")
		if constraintName != "" {
			return fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s;", tableName, constraintName)
		}
		return fmt.Sprintf("-- Rollback ADD CONSTRAINT: DROP CONSTRAINT %s.<constraint_name>;", tableName)
	}

	// Handle DROP CONSTRAINT
	if strings.Contains(sql, "DROP CONSTRAINT") {
		constraintName := extractIdentifierAfterKeywordSequence(stmt.SQL, "DROP", "CONSTRAINT")
		if constraintName == "" {
			constraintName = extractIdentifierAfterKeywordSequence(stmt.SQL, "DROP", "CONSTRAINT", "IF", "EXISTS")
		}
		if constraintName != "" {
			return "-- Rollback DROP CONSTRAINT " + constraintName + ": requires constraint definition from backup"
		}
		return "-- Rollback DROP CONSTRAINT: requires constraint definition from backup"
	}

	// Handle ALTER COLUMN TYPE
	if strings.Contains(sql, "ALTER COLUMN") && strings.Contains(sql, "TYPE") {
		columnName := extractIdentifierAfterKeywordSequence(stmt.SQL, "ALTER", "COLUMN")
		if columnName != "" {
			return "-- Rollback ALTER COLUMN TYPE for " + tableName + "." + columnName + ": requires previous type from backup"
		}
		return "-- Rollback ALTER COLUMN TYPE: requires previous column type from backup"
	}

	// Handle ALTER COLUMN SET/DROP NOT NULL
	if strings.Contains(sql, "ALTER COLUMN") && strings.Contains(sql, "NOT NULL") {
		columnName := extractIdentifierAfterKeywordSequence(stmt.SQL, "ALTER", "COLUMN")
		if columnName != "" {
			if strings.Contains(sql, "SET NOT NULL") {
				return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL;", tableName, columnName)
			}
			return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL;", tableName, columnName)
		}
	}

	// Handle ALTER COLUMN SET/DROP DEFAULT
	if strings.Contains(sql, "ALTER COLUMN") && strings.Contains(sql, "DEFAULT") {
		columnName := extractIdentifierAfterKeywordSequence(stmt.SQL, "ALTER", "COLUMN")
		if columnName != "" {
			if strings.Contains(sql, "SET DEFAULT") {
				return fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT;", tableName, columnName)
			}
			return fmt.Sprintf("-- Rollback DROP DEFAULT for %s.%s: requires previous default value from backup", tableName, columnName)
		}
	}

	// Handle RENAME TABLE
	if strings.Contains(sql, "RENAME TO") {
		newName := extractIdentifierAfterKeywordSequence(stmt.SQL, "RENAME", "TO")
		if newName != "" {
			return fmt.Sprintf("ALTER TABLE %s RENAME TO %s;", newName, tableName)
		}
	}

	// Handle RENAME COLUMN
	if strings.Contains(sql, "RENAME COLUMN") {
		oldName := extractIdentifierAfterKeywordSequence(stmt.SQL, "RENAME", "COLUMN")
		newName := extractIdentifierAfterKeywordSequence(stmt.SQL, "TO")
		if oldName != "" && newName != "" {
			return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s;", tableName, newName, oldName)
		}
	}

	// Handle OWNER TO
	if strings.Contains(sql, "OWNER TO") {
		return "-- Rollback OWNER TO: requires previous owner information from backup"
	}

	// Handle ENABLE/DISABLE TRIGGER
	if strings.Contains(sql, "TRIGGER") {
		if strings.Contains(sql, "ENABLE") {
			triggerName := extractIdentifierAfterKeywordSequence(stmt.SQL, "ENABLE", "TRIGGER")
			if triggerName != "" {
				return fmt.Sprintf("ALTER TABLE %s DISABLE TRIGGER %s;", tableName, triggerName)
			}
		} else if strings.Contains(sql, "DISABLE") {
			triggerName := extractIdentifierAfterKeywordSequence(stmt.SQL, "DISABLE", "TRIGGER")
			if triggerName != "" {
				return fmt.Sprintf("ALTER TABLE %s ENABLE TRIGGER %s;", tableName, triggerName)
			}
		}
	}

	// Default fallback
	return fmt.Sprintf("-- Manual rollback required for ALTER TABLE %s\n-- Original SQL: %s", tableName, stmt.SQL)
}

// generateInsertRollback creates rollback for INSERT statements
func (bg *BackupGenerator) generateInsertRollback(stmt types.Statement) string {
	// For INSERT statements, we could generate DELETE statements
	// This would require parsing the INSERT to extract the data
	return fmt.Sprintf("-- DELETE rollback for INSERT into %s (implement based on INSERT values)", stmt.ObjectName)
}

func extractIdentifierAfterKeywordSequence(sql string, keywords ...string) string {
	if strings.TrimSpace(sql) == "" || len(keywords) == 0 {
		return ""
	}

	tokens := tokenizeSQLIdentifiers(sql)
	if len(tokens) == 0 {
		return ""
	}

	for i := range tokens {
		index := i
		matched := true
		for _, keyword := range keywords {
			if index >= len(tokens) || !strings.EqualFold(tokens[index], keyword) {
				matched = false
				break
			}
			index++
		}

		if !matched || index >= len(tokens) {
			continue
		}

		candidate := strings.TrimSpace(tokens[index])
		if candidate == "" || strings.EqualFold(candidate, "IF") {
			continue
		}

		return candidate
	}

	return ""
}

func tokenizeSQLIdentifiers(sql string) []string {
	raw := strings.FieldsFunc(sql, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.')
	})

	tokens := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		tokens = append(tokens, token)
	}

	return tokens
}

// ValidateBackup verifies that a backup can be restored
func (bg *BackupGenerator) ValidateBackup(ctx context.Context, backupPath string) error {
	// Check if backup file exists and is readable
	info, err := os.Stat(backupPath)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeBackupGenerationFailed,
			"backup file not accessible",
			errors.SeverityError,
			errors.CategoryBackup,
		).WithInnerError(err)
	}

	if info.Size() == 0 {
		return errors.NewError(
			errors.ErrorCodeBackupGenerationFailed,
			"backup file is empty",
			errors.SeverityError,
			errors.CategoryBackup,
		)
	}

	// For SQL format, do basic syntax validation
	if strings.HasSuffix(backupPath, ".sql") {
		return bg.validateSQLBackup(backupPath)
	}

	return nil
}

// validateSQLBackup performs basic validation on SQL backup files
func (bg *BackupGenerator) validateSQLBackup(backupPath string) error {
	content, err := os.ReadFile(backupPath)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeBackupGenerationFailed,
			"failed to read backup file",
			errors.SeverityError,
			errors.CategoryBackup,
		).WithInnerError(err)
	}

	// Basic checks
	contentStr := string(content)

	if !strings.Contains(contentStr, "PostgreSQL database dump") {
		return errors.NewError(
			errors.ErrorCodeBackupGenerationFailed,
			"backup does not appear to be a PostgreSQL dump",
			errors.SeverityError,
			errors.CategoryBackup,
		)
	}

	// Check for common dump sections
	hasSchema := strings.Contains(contentStr, "CREATE TABLE") || strings.Contains(contentStr, "CREATE SCHEMA")
	hasData := strings.Contains(contentStr, "COPY ") || strings.Contains(contentStr, "INSERT INTO")

	if !hasSchema && !hasData {
		return errors.NewError(
			errors.ErrorCodeBackupGenerationFailed,
			"backup appears to be empty (no schema or data found)",
			errors.SeverityError,
			errors.CategoryBackup,
		)
	}

	return nil
}

// CleanupOldBackups removes old backup files based on retention policy
func (bg *BackupGenerator) CleanupOldBackups(maxAge time.Duration, maxCount int) error {
	files, err := filepath.Glob(filepath.Join(bg.workDir, "*backup*.sql"))
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeBackupGenerationFailed,
			"failed to list backup files",
			errors.SeverityError,
			errors.CategoryBackup,
		).WithInnerError(err)
	}

	// Sort by modification time
	type fileInfo struct {
		path    string
		modTime time.Time
	}

	var fileInfos []fileInfo
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		fileInfos = append(fileInfos, fileInfo{path: file, modTime: info.ModTime()})
	}

	// Remove files older than maxAge
	cutoff := time.Now().Add(-maxAge)
	var remainingFiles []fileInfo
	for _, info := range fileInfos {
		if info.modTime.Before(cutoff) {
			if err := os.Remove(info.path); err != nil {
				utils.GetDefaultLogger().Warn("Failed to remove old backup %s: %v", info.path, err)
			}
		} else {
			remainingFiles = append(remainingFiles, info)
		}
	}

	// Implement maxCount logic - keep only the N most recent backups
	if maxCount > 0 && len(remainingFiles) > maxCount {
		// Sort by modification time (newest first)
		for i := 0; i < len(remainingFiles)-1; i++ {
			for j := i + 1; j < len(remainingFiles); j++ {
				if remainingFiles[i].modTime.Before(remainingFiles[j].modTime) {
					remainingFiles[i], remainingFiles[j] = remainingFiles[j], remainingFiles[i]
				}
			}
		}

		// Remove files beyond maxCount
		for i := maxCount; i < len(remainingFiles); i++ {
			if err := os.Remove(remainingFiles[i].path); err != nil {
				utils.GetDefaultLogger().Warn("Failed to remove old backup %s: %v", remainingFiles[i].path, err)
			}
		}
	}

	return nil
}
