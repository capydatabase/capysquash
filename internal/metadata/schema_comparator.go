// Package metadata provides schema comparison functionality
package metadata

import (
	"context"
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/parser"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// SchemaComparator provides comprehensive schema comparison and validation
type SchemaComparator struct {
	manager *MetadataManager
}

// NewSchemaComparator creates a new schema comparator
func NewSchemaComparator(manager *MetadataManager) *SchemaComparator {
	return &SchemaComparator{
		manager: manager,
	}
}

// ComparisonResult represents the result of schema comparison
type ComparisonResult struct {
	IsValid            bool
	MissingExtensions  []string
	MissingDependencies []MissingDependency
	TypeMismatches     []TypeMismatch
	ConstraintConflicts []ConstraintConflict
	BreakingChanges    []BreakingChange
	Warnings           []string
	SchemaDrift        []SchemaDrift
}

// MissingDependency represents a missing database object
type MissingDependency struct {
	ObjectName   string
	ObjectType   types.ObjectType
	ReferencedBy string
	Severity     string // "error" or "warning"
}

// TypeMismatch represents a data type mismatch
type TypeMismatch struct {
	Object         string
	Column         string
	ExpectedType   string
	ActualType     string
	IsBreaking     bool
}

// ConstraintConflict represents a constraint mismatch
type ConstraintConflict struct {
	Table          string
	ConstraintName string
	ExpectedDef    string
	ActualDef      string
	ConflictType   string // "missing", "different", "extra"
}

// BreakingChange represents a potentially breaking schema change
type BreakingChange struct {
	Description string
	Impact      string
	Mitigation  string
}

// SchemaDrift represents drift between migrations and database
type SchemaDrift struct {
	Object      string
	ObjectType  string
	Description string
	DriftType   string // "missing_in_db", "extra_in_db", "definition_mismatch"
}

// CompareSchema compares generated SQL against production database schema
func (sc *SchemaComparator) CompareSchema(ctx context.Context, generatedSQL string) (*ComparisonResult, error) {
	result := &ComparisonResult{
		IsValid:             true,
		MissingExtensions:   []string{},
		MissingDependencies: []MissingDependency{},
		TypeMismatches:      []TypeMismatch{},
		ConstraintConflicts: []ConstraintConflict{},
		BreakingChanges:     []BreakingChange{},
		Warnings:            []string{},
		SchemaDrift:         []SchemaDrift{},
	}

	// Get production database metadata
	dbMeta, err := sc.manager.GetMetadata(ctx, "")
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrorCodeAnalysisError, errors.CategoryValidation, "failed to get database metadata", nil)
	}

	// Parse generated SQL to extract schema
	migration, err := parser.ParseMigration(generatedSQL, "squashed.sql")
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrorCodeAnalysisError, errors.CategoryValidation, "failed to parse generated SQL", nil)
	}

	// Track all required extensions from migration
	requiredExtensions := sc.extractRequiredExtensions(migration)
	for _, extName := range requiredExtensions {
		if _, exists := dbMeta.Extensions[extName]; !exists {
			result.MissingExtensions = append(result.MissingExtensions, extName)
			result.IsValid = false
		}
	}

	// Validate dependencies for each statement
	for _, stmt := range migration.Statements {
		sc.validateStatementDependencies(ctx, stmt, dbMeta, result)
	}

	// Compare table schemas
	sc.compareTableSchemas(ctx, migration, dbMeta, result)

	// Compare function signatures
	sc.compareFunctionSignatures(ctx, migration, dbMeta, result)

	// Compare types (ENUMs, COMPOSITE, etc.)
	sc.compareTypes(ctx, migration, dbMeta, result)

	// Detect schema drift
	sc.detectSchemaDrift(ctx, migration, dbMeta, result)

	return result, nil
}

// extractRequiredExtensions extracts extension names from CREATE EXTENSION statements
func (sc *SchemaComparator) extractRequiredExtensions(migration *types.Migration) []string {
	extensions := []string{}
	for _, stmt := range migration.Statements {
		if stmt.ObjectType == types.TypeExtension && stmt.Operation == types.OpCreate {
			if extName := extractExtensionName(stmt.SQL); extName != "" {
				extensions = append(extensions, extName)
			}
		}
	}
	return extensions
}

// extractExtensionName extracts extension name from CREATE EXTENSION SQL
func extractExtensionName(sql string) string {
	// Simple extraction: CREATE EXTENSION "name" or CREATE EXTENSION IF NOT EXISTS "name"
	sql = strings.ToUpper(sql)
	if !strings.Contains(sql, "CREATE EXTENSION") {
		return ""
	}

	// Remove CREATE EXTENSION [IF NOT EXISTS]
	sql = strings.Replace(sql, "CREATE EXTENSION", "", 1)
	sql = strings.Replace(sql, "IF NOT EXISTS", "", 1)
	sql = strings.TrimSpace(sql)

	// Extract first word/quoted string
	parts := strings.Fields(sql)
	if len(parts) > 0 {
		name := parts[0]
		// Remove quotes if present
		name = strings.Trim(name, "\"'")
		// Remove WITH/SCHEMA clauses
		if idx := strings.IndexAny(name, " \t\n"); idx > 0 {
			name = name[:idx]
		}
		return strings.ToLower(name)
	}

	return ""
}

// validateStatementDependencies validates that statement dependencies exist in database
func (sc *SchemaComparator) validateStatementDependencies(ctx context.Context, stmt types.Statement, dbMeta *DatabaseMetadata, result *ComparisonResult) {
	for _, dep := range stmt.Dependencies {
		// Parse dependency (format: "schema.object" or "object")
		parts := strings.Split(dep, ".")
		var schema string

		if len(parts) == 2 {
			schema = parts[0]
			// object = parts[1] - not needed, we only check if schema is found
		} else {
			// Use default search path
			schema, _ = dbMeta.SearchObject(dbMeta.GetSearchPath(), parts[0])
		}

		if schema == "" {
			// Object not found in any schema
			missing := MissingDependency{
				ObjectName:   dep,
				ObjectType:   types.TypeUnknown,
				ReferencedBy: stmt.SQL[:min(100, len(stmt.SQL))],
				Severity:     "error",
			}

			// Determine severity based on context
			if isOptionalDependency(stmt, dep) {
				missing.Severity = "warning"
			}

			result.MissingDependencies = append(result.MissingDependencies, missing)
			if missing.Severity == "error" {
				result.IsValid = false
			}
		}
	}
}

// compareTableSchemas compares table definitions between migration and database
func (sc *SchemaComparator) compareTableSchemas(ctx context.Context, migration *types.Migration, dbMeta *DatabaseMetadata, result *ComparisonResult) {
	// Extract table names from migration (CREATE TABLE and ALTER TABLE statements)
	migrationTables := make(map[string]bool)
	for _, stmt := range migration.Statements {
		if stmt.ObjectType == types.TypeTable && (stmt.Operation == types.OpCreate || stmt.Operation == types.OpAlter) {
			migrationTables[strings.ToLower(stmt.ObjectName)] = true
		}
	}

	// For now, just verify tables exist in database
	// Full schema comparison would require parsing CREATE TABLE statements fully
	for tableName := range migrationTables {
		schema, dbTable := dbMeta.SearchTable(dbMeta.GetSearchPath(), tableName)

		if dbTable == nil {
			// Table doesn't exist in database
			result.SchemaDrift = append(result.SchemaDrift, SchemaDrift{
				Object:      tableName,
				ObjectType:  "TABLE",
				Description: fmt.Sprintf("Table %s defined in migration but not found in database", tableName),
				DriftType:   "missing_in_db",
			})
		} else {
			// Table exists - basic validation passed
			_ = schema // Use schema for logging if needed
		}
	}
}

// compareColumns compares column definitions
// Note: Full column comparison requires detailed parsing of CREATE TABLE statements
// For now, this is a placeholder for future enhancement
//
//nolint:unused // Reserved for future feature implementation
func (sc *SchemaComparator) compareColumns(schema, tableName string, dbTable *TableMetadata, result *ComparisonResult) {
	// This would require full CREATE TABLE parsing to extract column definitions
	// For paranoid mode, we rely on dependency validation and extension checks
	_ = schema
	_ = tableName
	_ = dbTable
	_ = result
}

// compareConstraints compares constraint definitions
// Note: Full constraint comparison requires detailed parsing of ALTER TABLE statements
// For now, this is a placeholder for future enhancement
//
//nolint:unused // Reserved for future feature implementation
func (sc *SchemaComparator) compareConstraints(schema, tableName string, dbTable *TableMetadata, result *ComparisonResult) {
	// This would require full ALTER TABLE parsing to extract constraint definitions
	// For paranoid mode, we rely on dependency validation and extension checks
	_ = schema
	_ = tableName
	_ = dbTable
	_ = result
}

// compareFunctionSignatures compares function definitions
func (sc *SchemaComparator) compareFunctionSignatures(ctx context.Context, migration *types.Migration, dbMeta *DatabaseMetadata, result *ComparisonResult) {
	// Extract function names from migration
	migrationFuncs := make(map[string]bool)
	for _, stmt := range migration.Statements {
		if stmt.ObjectType == types.TypeFunction && stmt.Operation == types.OpCreate {
			migrationFuncs[strings.ToLower(stmt.ObjectName)] = true
		}
	}

	// Check if functions exist in database
	for funcName := range migrationFuncs {
		schemas, dbFuncs := dbMeta.SearchFunctions(dbMeta.GetSearchPath(), funcName)

		if len(dbFuncs) == 0 {
			// Function doesn't exist in database
			result.SchemaDrift = append(result.SchemaDrift, SchemaDrift{
				Object:      funcName,
				ObjectType:  "FUNCTION",
				Description: fmt.Sprintf("Function %s defined in migration but not found in database", funcName),
				DriftType:   "missing_in_db",
			})
		}

		_ = schemas // Use for logging if needed
	}
}

// compareTypes compares custom type definitions
func (sc *SchemaComparator) compareTypes(ctx context.Context, migration *types.Migration, dbMeta *DatabaseMetadata, result *ComparisonResult) {
	// Extract type names from migration
	migrationTypes := make(map[string]bool)
	for _, stmt := range migration.Statements {
		if stmt.ObjectType == types.TypeType && stmt.Operation == types.OpCreate {
			migrationTypes[strings.ToLower(stmt.ObjectName)] = true
		}
	}

	// Check if types exist in database
	for typeName := range migrationTypes {
		found := false
		for _, schemaName := range dbMeta.GetSearchPath() {
			if schemaMeta, exists := dbMeta.Schemas[schemaName]; exists {
				if _, exists := schemaMeta.Types[typeName]; exists {
					found = true
					break
				}
			}
		}

		if !found {
			result.SchemaDrift = append(result.SchemaDrift, SchemaDrift{
				Object:      typeName,
				ObjectType:  "TYPE",
				Description: fmt.Sprintf("Type %s defined in migration but not found in database", typeName),
				DriftType:   "missing_in_db",
			})
		}
	}
}

// detectSchemaDrift detects drift between migration schema and database schema
func (sc *SchemaComparator) detectSchemaDrift(ctx context.Context, migration *types.Migration, dbMeta *DatabaseMetadata, result *ComparisonResult) {
	// Extract all objects from migration
	migrationObjects := extractAllObjects(migration)

	// Compare against database objects
	for objName, objType := range migrationObjects {
		schema, _ := dbMeta.SearchObject(dbMeta.GetSearchPath(), objName)

		if schema == "" {
			// Object in migration but not in database
			result.SchemaDrift = append(result.SchemaDrift, SchemaDrift{
				Object:      objName,
				ObjectType:  string(objType),
				Description: fmt.Sprintf("%s %s defined in migration but not found in database", objType, objName),
				DriftType:   "missing_in_db",
			})
		}
	}
}

// Helper functions

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func isOptionalDependency(stmt types.Statement, dep string) bool {
	// Determine if dependency is optional based on statement context
	// For example, IF EXISTS clauses make dependencies optional
	upperSQL := strings.ToUpper(stmt.SQL)
	return strings.Contains(upperSQL, "IF EXISTS") || strings.Contains(upperSQL, "IF NOT EXISTS")
}

// Note: These helper functions are simplified placeholders
// Full table/function/type definition extraction would require
// detailed parsing of CREATE statements, which is complex
// For paranoid mode, we focus on dependency and extension validation

func extractAllObjects(migration *types.Migration) map[string]types.ObjectType {
	objects := make(map[string]types.ObjectType)
	for _, stmt := range migration.Statements {
		objects[strings.ToLower(stmt.ObjectName)] = stmt.ObjectType
	}
	return objects
}

//nolint:unused // Reserved for future feature implementation
func areTypesCompatible(migType, dbType string) bool {
	// Normalize types for comparison
	migType = normalizeType(migType)
	dbType = normalizeType(dbType)

	// Exact match
	if migType == dbType {
		return true
	}

	// Compatible type aliases
	compatibleTypes := map[string][]string{
		"integer":   {"int", "int4"},
		"bigint":    {"int8"},
		"smallint":  {"int2"},
		"boolean":   {"bool"},
		"character": {"char"},
		"text":      {"varchar"},
	}

	for canonical, aliases := range compatibleTypes {
		if migType == canonical {
			for _, alias := range aliases {
				if dbType == alias {
					return true
				}
			}
		}
		if dbType == canonical {
			for _, alias := range aliases {
				if migType == alias {
					return true
				}
			}
		}
	}

	return false
}

//nolint:unused // Reserved for future feature implementation
func normalizeType(typ string) string {
	typ = strings.ToLower(strings.TrimSpace(typ))
	// Remove precision/length for comparison
	if idx := strings.Index(typ, "("); idx > 0 {
		typ = typ[:idx]
	}
	return typ
}

//nolint:unused // Reserved for future feature implementation
func isBreakingTypeChange(migType, dbType string) bool {
	// Some type changes are breaking (incompatible casts)
	breakingChanges := map[string][]string{
		"text":    {"integer", "bigint", "smallint", "numeric"},
		"integer": {"text", "varchar"},
		"boolean": {"integer", "text"},
	}

	migType = normalizeType(migType)
	dbType = normalizeType(dbType)

	if incompatible, exists := breakingChanges[migType]; exists {
		for _, incompatibleType := range incompatible {
			if dbType == incompatibleType {
				return true
			}
		}
	}

	return false
}

// Simplified constraint and type comparison helpers
// Full comparison would require detailed CREATE statement parsing
