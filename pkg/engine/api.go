// Package engine provides a programmatic API for the pgsquash migration consolidation engine.
//
// This package allows Go applications to use pgsquash as a library for custom migration
// workflows, batch processing, or integration into existing tools.
//
// # Basic Usage
//
//	config := engine.DefaultConfig()
//	config.SafetyLevel = engine.Standard
//
//	result, err := engine.SquashDirectory("./migrations", config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Printf("Squashed %d migrations\n", result.FilesProcessed)
//	fmt.Printf("Output: %s\n", result.BaselineSQL)
//
// # Advanced Usage
//
//	// Squash specific files with custom config
//	files := map[int]string{
//	    1: "001_create_users.sql",
//	    2: "002_create_posts.sql",
//	}
//
//	config := &engine.Config{
//	    SafetyLevel:     engine.Conservative,
//	    OutputFormat:    engine.FormatSingle,
//	    EnableStreaming: true,
//	    MemoryLimitMB:   512,
//	}
//
//	result, err := engine.SquashFiles(files, config)
//
// # Analyzing Without Squashing
//
//	analysis, err := engine.AnalyzeDirectory("./migrations", nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Printf("Found %d redundancies\n", len(analysis.Redundancies))
//	fmt.Printf("Total objects: %d\n", analysis.TotalObjects)
package engine

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	internal_config "github.com/capysquash/pgsquash-engine/internal/config"
	internal_parser "github.com/capysquash/pgsquash-engine/internal/parser"
	internal_squasher "github.com/capysquash/pgsquash-engine/internal/squasher"
	internal_tracking "github.com/capysquash/pgsquash-engine/internal/tracking"
	internal_types "github.com/capysquash/pgsquash-engine/internal/types"
	internal_utils "github.com/capysquash/pgsquash-engine/internal/utils"
	public_plugins "github.com/capysquash/pgsquash-engine/pkg/plugins"
)

// SafetyLevel determines how aggressively migrations are consolidated.
type SafetyLevel string

const (
	// Conservative applies minimal consolidation, preserving most operations
	Conservative SafetyLevel = "conservative"

	// Standard balances consolidation and safety (recommended)
	Standard SafetyLevel = "standard"

	// Aggressive maximizes consolidation, suitable for development
	Aggressive SafetyLevel = "aggressive"

	// Paranoid preserves everything, minimal changes
	Paranoid SafetyLevel = "paranoid"
)

// ValidSafetyLevels returns every valid safety level, delegating to the
// internal validation as the single source of truth.
func ValidSafetyLevels() []SafetyLevel {
	internalLevels := internal_squasher.ValidSafetyLevels()
	levels := make([]SafetyLevel, len(internalLevels))
	for i, level := range internalLevels {
		levels[i] = SafetyLevel(level)
	}
	return levels
}

// ParseSafetyLevel parses a safety level string case-insensitively
// (surrounding whitespace is ignored) and rejects unknown values. Consumers
// (API, Studio, CLI wrappers) should use this instead of reimplementing
// safety-level parsing with divergent semantics.
func ParseSafetyLevel(s string) (SafetyLevel, error) {
	parsed, err := internal_squasher.ParseSafetyLevel(strings.ToLower(strings.TrimSpace(s)))
	if err != nil {
		return "", err
	}
	return SafetyLevel(parsed), nil
}

// OutputFormat determines how squashed SQL is formatted.
type OutputFormat string

const (
	// FormatSingle outputs all migrations in a single file
	FormatSingle OutputFormat = "single"

	// FormatSplit outputs migrations in multiple files by category
	FormatSplit OutputFormat = "split"
)

// Config contains configuration for the squashing engine.
type Config struct {
	// Context controls cancellation and request lifetime for all engine work.
	// Nil uses context.Background for standalone/library compatibility.
	Context context.Context

	// SafetyLevel determines consolidation aggressiveness (default: Standard)
	SafetyLevel SafetyLevel

	// RuleOverrides force-enables (true) or force-disables (false) specific
	// named consolidation rules relative to the SafetyLevel baseline. Rule
	// names must match the catalog served by pkg/rules.GetRegistry (e.g.
	// "create_alter_consolidation", "function_deduplication"). Unknown rule
	// names are rejected with an error when the engine is constructed, never
	// silently ignored. A nil/empty map applies the baseline unchanged.
	//
	// This is the per-request integration point for org-scoped rule overrides
	// (the global rule registry is an immutable catalog and is never mutated).
	RuleOverrides map[string]bool

	// ProdDBDSN is the production database connection string required by the
	// Paranoid safety level (database validation) and used by backup
	// generation. When empty, the PROD_DB_DSN environment variable is used.
	ProdDBDSN string

	// Version is the caller's tool version stamped into provenance metadata
	// (SquashResult.ProvenanceInfo.Version / .squashmap.json). Callers should
	// set it from their release metadata; empty falls back to "dev".
	Version string

	// OutputFormat determines file organization (default: FormatSingle)
	OutputFormat OutputFormat

	// SeparateDataOps when true, returns separate DDL and data operations files
	SeparateDataOps bool

	// EnableStreaming enables memory-efficient processing for large datasets (default: false)
	EnableStreaming bool

	// MemoryLimitMB sets memory limit for streaming mode (default: 256)
	MemoryLimitMB int

	// BatchSize controls how many migrations are processed in each batch (streaming mode)
	// Default: 50. Increase for better throughput, decrease to reduce memory usage.
	BatchSize int

	// WorkerCount sets the number of parallel workers (streaming mode)
	// Default: 4. Should match available CPU cores for optimal performance.
	WorkerCount int

	// ProgressCallback is called during processing to report progress
	// The callback receives: (processed, total, phase)
	//
	// Example:
	//   config.ProgressCallback = func(processed, total int64, phase string) {
	//       percent := float64(processed) / float64(total) * 100
	//       fmt.Printf("[%s] %.1f%% complete\n", phase, percent)
	//   }
	ProgressCallback ProgressCallback

	// EnableBackup enables backup generation before squashing (default: false)
	// Creates timestamped backups of original migrations for safety
	EnableBackup bool

	// BackupPath specifies where to store backups (default: "./backups")
	// Only used if EnableBackup is true
	BackupPath string

	// BackupRetentionDays specifies how long to keep backups (default: 30)
	// Older backups are automatically cleaned up
	BackupRetentionDays int

	// EnableRollback enables rollback script generation (default: false)
	// Creates scripts to undo the squashing operation
	EnableRollback bool

	// RollbackPath specifies where to store rollback scripts (default: "./rollbacks")
	// Only used if EnableRollback is true
	RollbackPath string

	// EnableCycleDetection enables DDL cycle detection (default: false)
	// Detects circular dependencies in migrations
	EnableCycleDetection bool

	// ShowCycleDetails shows detailed cycle information (default: false)
	// Only used if EnableCycleDetection is true
	ShowCycleDetails bool

	// CycleDetectionDepth sets maximum depth for cycle detection (default: 10)
	// Higher values detect deeper cycles but use more memory
	CycleDetectionDepth int

	// Verbose enables detailed logging (default: false)
	Verbose bool

	// DryRun performs a trial run that writes nothing to disk (default: false).
	// The engine returns SQL strings either way; DryRun additionally disables
	// the file-writing side features (pg_dump backups and rollback plans).
	DryRun bool
}

// SquashResult contains the results of a squashing operation.
type SquashResult struct {
	// BaselineSQL contains the consolidated DDL SQL.
	BaselineSQL string `json:"baseline_sql"`

	// DataOperationsSQL contains data operations SQL (INSERT, UPDATE, DELETE)
	DataOperationsSQL string `json:"data_operations_sql,omitempty"`

	// Warnings contains any warnings generated during squashing
	Warnings []string `json:"warnings,omitempty"`

	// FilesProcessed is the number of migration files processed
	FilesProcessed int `json:"files_processed"`

	// ObjectsConsolidated is the number of database objects consolidated
	ObjectsConsolidated int `json:"objects_consolidated"`

	// ProcessingTime is the duration of the squashing operation
	ProcessingTime string `json:"processing_time"`

	// Extensions contains detected/required PostgreSQL extensions
	Extensions []string `json:"extensions,omitempty"`

	// AuthCompatibilitySQL contains SQL to mock authentication services for validation
	AuthCompatibilitySQL string `json:"auth_compatibility_sql,omitempty"`

	// ProvenanceInfo contains metadata about the squashing operation
	ProvenanceInfo *ProvenanceInfo `json:"provenance_info,omitempty"`

	// DetailedMetrics provides comprehensive analysis metrics for partner
	// integrations. The engine does not populate this field itself; callers
	// build it via CalculateDetailedMetrics or BuildAnalysisMetrics.
	DetailedMetrics *DetailedMetrics `json:"detailed_metrics,omitempty"`

	// RecommendedActions suggests next steps based on analysis. Populated by
	// callers via GenerateRecommendations / BuildAnalysisRecommendedActions.
	RecommendedActions []RecommendedAction `json:"recommended_actions,omitempty"`
}

// ProvenanceInfo contains metadata about the squashing operation
type ProvenanceInfo struct {
	// Version of the squasher used
	Version string `json:"version"`

	// SafetyLevel applied during squashing
	SafetyLevel string `json:"safety_level"`

	// InputFiles lists source migration files
	InputFiles []string `json:"input_files,omitempty"`

	// OutputFiles lists generated files
	OutputFiles []string `json:"output_files,omitempty"`

	// ContentHash for integrity verification
	ContentHash string `json:"content_hash,omitempty"`
}

// AnalysisResult contains the results of migration analysis.
type AnalysisResult struct {
	// TotalFiles is the number of migration files analyzed
	TotalFiles int

	// TotalStatements is the total number of SQL statements
	TotalStatements int

	// TotalObjects is the number of database objects
	TotalObjects int

	// Redundancies lists redundant operations that can be consolidated
	Redundancies []Redundancy

	// ObjectsByType maps object types to counts
	ObjectsByType map[string]int

	// DataOperations contains detailed data operation counts
	DataOperations DataOpCounts

	// Warnings contains validation warnings
	Warnings []string
}

// Redundancy represents a redundant database operation.
type Redundancy struct {
	// Type is the redundancy type (e.g., "duplicate_create", "overridden_alter")
	Type string

	// ObjectName is the affected database object
	ObjectName string

	// Description explains the redundancy
	Description string

	// Severity indicates importance (low, medium, high)
	Severity string
}

// DataOpCounts contains detailed counts of data operations
type DataOpCounts struct {
	Total   int `json:"total"`
	Inserts int `json:"inserts"`
	Updates int `json:"updates"`
	Deletes int `json:"deletes"`
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		SafetyLevel:          Standard,
		OutputFormat:         FormatSingle,
		EnableStreaming:      false,
		MemoryLimitMB:        256,
		BatchSize:            50,
		WorkerCount:          4,
		EnableBackup:         false,
		BackupPath:           "./backups",
		BackupRetentionDays:  30,
		EnableRollback:       false,
		RollbackPath:         "./rollbacks",
		EnableCycleDetection: false,
		ShowCycleDetails:     false,
		CycleDetectionDepth:  10,
		Verbose:              false,
	}
}

// SquashDirectory consolidates all migration files in a directory.
//
// If config is nil, DefaultConfig() is used.
//
// Example:
//
//	result, err := engine.SquashDirectory("./migrations", nil)
//	if err != nil {
//	    return err
//	}
//	fmt.Println(result.BaselineSQL)
func SquashDirectory(directory string, config *Config) (*SquashResult, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Use streaming directory processing if enabled. This goes through the
	// same fully-configured engine as every other path.
	if config.EnableStreaming {
		eng, err := NewEngine(config)
		if err != nil {
			return nil, err
		}
		defer func() {
			_ = eng.Close()
		}()

		internalResult, err := eng.internal.SquashFromDirectory(directory)
		if err != nil {
			return nil, err
		}

		stats := eng.internal.GetStats()
		return &SquashResult{
			BaselineSQL:          internalResult.BaselineSQL,
			Warnings:             internalResult.Warnings,
			FilesProcessed:       countFilesInDir(directory),
			ObjectsConsolidated:  int(stats.ConsolidationsApplied),
			ProcessingTime:       formatDuration(stats.ProcessingTime),
			Extensions:           internalResult.Extensions,
			AuthCompatibilitySQL: internalResult.AuthCompatibilitySQL,
		}, nil
	}

	// Load migrations from directory
	migrations, err := loadMigrationsFromDir(directory)
	if err != nil {
		return nil, fmt.Errorf("failed to load migrations: %w", err)
	}

	return SquashFiles(migrations, config)
}

// SquashFiles consolidates specific migration files.
//
// The migrations map can contain either:
// - File paths: values ending in .sql will be read from disk
// - SQL content: multi-line SQL strings will be used directly
//
// Example with file paths:
//
//	migrations := map[int]string{
//	    1: "001_create_users.sql",
//	    2: "002_create_posts.sql",
//	}
//	result, err := engine.SquashFiles(migrations, nil)
//
// Example with SQL content:
//
//	migrations := map[int]string{
//	    1: "CREATE TABLE users (id INT);\nCREATE TABLE posts (id INT);",
//	}
//	result, err := engine.SquashFiles(migrations, nil)
func SquashFiles(migrations map[int]string, config *Config) (*SquashResult, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Delegate to the Engine method path so the package-level function honors
	// the exact same configuration surface (backup, rollback, batch size,
	// worker count, progress callback, cycle detection, dry-run).
	eng, err := NewEngine(config)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = eng.Close()
	}()

	return eng.SquashFiles(migrations)
}

// AnalyzeDirectory analyzes migration files without making modifications.
//
// If config is nil, DefaultConfig() is used.
//
// Analysis is a read-only, static pass over the migration set. The provided
// Config is validated up front (SafetyLevel, OutputFormat, and RuleOverrides
// names are rejected when invalid) and Verbose controls logging. The
// remaining Config options are ignored by design because analysis neither
// consolidates nor writes anything: SafetyLevel/RuleOverrides select
// consolidation rules (analysis applies none), and the output, streaming,
// backup, rollback, and cycle-detection options only affect squash runs.
//
// Example:
//
//	analysis, err := engine.AnalyzeDirectory("./migrations", nil)
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Found %d redundancies\n", len(analysis.Redundancies))
func AnalyzeDirectory(directory string, config *Config) (*AnalysisResult, error) {
	if config == nil {
		config = DefaultConfig()
	}

	ctx := config.Context
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Validate the configuration instead of silently ignoring invalid values
	// (an unknown safety level or rule override name is a caller bug).
	if _, err := convertConfig(config); err != nil {
		return nil, err
	}

	// Keep the plugin surface identical to squash runs.
	if err := ensureDefaultPlugins(); err != nil {
		return nil, err
	}

	// Setup logger
	logLevel := internal_utils.LogLevelInfo
	if config.Verbose {
		logLevel = internal_utils.LogLevelDebug
	}
	logger := internal_utils.NewLogger(logLevel, os.Stdout)
	internal_utils.SetDefaultLogger(logger)

	// Load and parse migrations
	migrations, err := loadAndParseMigrations(ctx, directory)
	if err != nil {
		return nil, err
	}

	// Create tracker for analysis
	tracker := internal_tracking.NewTracker()
	for i, migration := range migrations {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tracker.ProcessMigration(migration, i)
	}

	// Get analysis results
	redundancies := tracker.GetRedundantObjects()
	stats := tracker.GetStatistics()
	warnings := tracker.ValidateConsistency()

	// Convert to public types
	// Convert ObjectsByType map to string keys
	objectsByTypeStr := make(map[string]int)
	for objType, count := range stats.ObjectsByType {
		objectsByTypeStr[string(objType)] = count
	}

	result := &AnalysisResult{
		TotalFiles:      len(migrations),
		TotalStatements: stats.TotalStatements,
		TotalObjects:    stats.TotalObjects,
		Redundancies:    convertRedundancies(redundancies),
		ObjectsByType:   objectsByTypeStr,
		DataOperations: DataOpCounts{
			Total:   stats.DataOperations,
			Inserts: stats.Inserts,
			Updates: stats.Updates,
			Deletes: stats.Deletes,
		},
		Warnings: warnings,
	}

	return result, nil
}

// Helper functions

// ensureDefaultPlugins registers the built-in plugin set (Supabase, Clerk,
// Prisma, Drizzle) into the global plugin registry. Registration is
// idempotent, so every pkg/engine entry point calls this instead of relying
// on binary mains having done it; a registration failure is surfaced as an
// error, never a silent degradation to plugin-less squashing.
func ensureDefaultPlugins() error {
	if err := public_plugins.RegisterDefault(); err != nil {
		return fmt.Errorf("failed to register default plugins: %w", err)
	}
	return nil
}

func convertConfig(config *Config) (*internal_config.Config, error) {
	// Reject invalid safety levels instead of silently coercing to Standard.
	level := config.SafetyLevel
	if level == "" {
		level = Standard
	}
	parsed, err := ParseSafetyLevel(string(level))
	if err != nil {
		return nil, err
	}

	// Create internal config with defaults
	internalCfg := internal_config.DefaultConfig()
	internalCfg.SafetyLevel = string(parsed)

	// Per-rule overrides relative to the safety baseline. Unknown rule names
	// are an error here (fail at construction), never silently dropped.
	if len(config.RuleOverrides) > 0 {
		if err := internal_squasher.ValidateRuleOverrides(config.RuleOverrides); err != nil {
			return nil, err
		}
		internalCfg.Rules.Overrides = maps.Clone(config.RuleOverrides)
	}

	// Explicit ProdDBDSN wins over the PROD_DB_DSN environment default that
	// internal_config.DefaultConfig() already applied.
	if dsn := strings.TrimSpace(config.ProdDBDSN); dsn != "" {
		internalCfg.ProdDBDSN = dsn
	}

	// Map the public output format onto the internal format vocabulary
	// (organized/sequential/minimal). Writing the raw public value would
	// produce an invalid internal config. Both public formats use the
	// organized layout; FormatSplit only controls SeparateDataOps handling
	// at the API layer.
	switch config.OutputFormat {
	case FormatSingle, FormatSplit, "":
		internalCfg.Output.Format = "organized"
	default:
		return nil, fmt.Errorf("invalid output format %q (valid: %q, %q)", config.OutputFormat, FormatSingle, FormatSplit)
	}

	return internalCfg, nil
}

func loadMigrationsFromDir(directory string) (map[int]string, error) {
	return loadMigrationsFromDirContext(context.Background(), directory)
}

func loadMigrationsFromDirContext(ctx context.Context, directory string) (map[int]string, error) {
	files, err := filepath.Glob(filepath.Join(directory, "*.sql"))
	if err != nil {
		return nil, err
	}

	migrations := make(map[int]string)
	for i, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Read file content (internal engine expects SQL content, not paths)
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}
		migrations[i+1] = string(content)
	}

	return migrations, nil
}

func loadAndParseMigrations(ctx context.Context, directory string) ([]*internal_types.Migration, error) {
	files, err := filepath.Glob(filepath.Join(directory, "*.sql"))
	if err != nil {
		return nil, err
	}

	var migrations []*internal_types.Migration
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}

		// Parse migration file
		migration, err := internal_parser.ParseMigrationWithContext(ctx, string(content), file)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", file, err)
		}

		migrations = append(migrations, migration)
	}

	return migrations, nil
}

func countFilesInDir(directory string) int {
	files, err := filepath.Glob(filepath.Join(directory, "*.sql"))
	if err != nil {
		return 0
	}
	return len(files)
}

func convertRedundancies(internal []internal_tracking.RedundancyReport) []Redundancy {
	var redundancies []Redundancy
	for _, r := range internal {
		// Determine severity based on pattern
		severity := "medium"
		if r.Pattern == internal_tracking.PatternDropCreateSequence ||
			r.Pattern == internal_tracking.PatternDuplicateOperations {
			severity = "high"
		}

		redundancies = append(redundancies, Redundancy{
			Type:        string(r.Pattern),
			ObjectName:  r.Object,
			Description: r.Explanation,
			Severity:    severity,
		})
	}
	return redundancies
}

// formatDuration converts time.Duration to human-readable string
func formatDuration(d time.Duration) string {
	if d == 0 {
		return "N/A"
	}

	// Round to milliseconds for display
	ms := d.Milliseconds()

	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}

	if ms < 60000 {
		return fmt.Sprintf("%.2fs", float64(ms)/1000.0)
	}

	minutes := ms / 60000
	seconds := (ms % 60000) / 1000
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}
