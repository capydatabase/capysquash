// Package metadata provides schema comparison functionality
package metadata

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/parser"
	schemamodel "github.com/capysquash/pgsquash-engine/internal/schema"
	"github.com/capysquash/pgsquash-engine/internal/types"
	pg_query "github.com/pganalyze/pg_query_go/v6"
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
	IsValid                  bool
	MissingExtensions        []string
	MissingDependencies      []MissingDependency
	TypeMismatches           []TypeMismatch
	ConstraintConflicts      []ConstraintConflict
	BreakingChanges          []BreakingChange
	Warnings                 []string
	SchemaDrift              []SchemaDrift
	GeneratedSchemaHash      string
	DatabaseSchemaHash       string
	DeterministicSchemaDrift []string
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
	Object       string
	Column       string
	ExpectedType string
	ActualType   string
	IsBreaking   bool
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
		IsValid:                  true,
		MissingExtensions:        []string{},
		MissingDependencies:      []MissingDependency{},
		TypeMismatches:           []TypeMismatch{},
		ConstraintConflicts:      []ConstraintConflict{},
		BreakingChanges:          []BreakingChange{},
		Warnings:                 []string{},
		SchemaDrift:              []SchemaDrift{},
		DeterministicSchemaDrift: []string{},
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

	// Deterministic normalized model diff (shared with validation comparator path)
	generatedModel := schemamodel.BuildFromSQL("generated.sql", generatedSQL)
	databaseModel := schemamodel.BuildFromLines(buildMetadataSignatureLines(dbMeta))
	deterministicDiff := schemamodel.CompareModelsWithLabels(databaseModel, generatedModel, "database", "generated", 250)

	result.GeneratedSchemaHash = generatedModel.Fingerprint
	result.DatabaseSchemaHash = databaseModel.Fingerprint
	result.DeterministicSchemaDrift = deterministicDiff.Differences

	if deterministicDiff.HasDifferences {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Deterministic schema model drift detected (%d entries)", len(deterministicDiff.Differences)))
	}

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
	migrationTables := make(map[string]types.Statement)
	for _, stmt := range migration.Statements {
		if stmt.ObjectType == types.TypeTable && stmt.Operation == types.OpCreate {
			migrationTables[strings.ToLower(stmt.ObjectName)] = stmt
		}
	}

	// For now, just verify tables exist in database
	// Full schema comparison would require parsing CREATE TABLE statements fully
	for tableName, stmt := range migrationTables {
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
			// Check for schema drift in columns
			sc.compareColumns(schema, tableName, stmt, dbTable, result)
			// Check for schema drift in constraints
			sc.compareConstraints(schema, tableName, stmt, dbTable, result)
		}
	}
}

// compareColumns compares column definitions
func (sc *SchemaComparator) compareColumns(schema, tableName string, stmt types.Statement, dbTable *TableMetadata, result *ComparisonResult) {
	// Parse the statement to extract column details
	tree, err := pg_query.Parse(stmt.SQL)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Failed to parse SQL for table %s: %v", tableName, err))
		return
	}

	if len(tree.Stmts) == 0 {
		return
	}

	// Extract columns from CreateStmt
	var migColumns = make(map[string]string)

	if createStmt := tree.Stmts[0].Stmt.GetCreateStmt(); createStmt != nil {
		for _, elt := range createStmt.TableElts {
			if col := elt.GetColumnDef(); col != nil {
				colName := col.Colname
				// Extract type name roughly
				var typeName string
				if col.TypeName != nil {
					// Join names to get type (e.g. "pg_catalog.int4" or just "text")
					names := make([]string, len(col.TypeName.Names))
					for i, n := range col.TypeName.Names {
						names[i] = n.GetString_().Sval
					}
					typeName = strings.Join(names, ".")
				}
				migColumns[strings.ToLower(colName)] = typeName
			}
		}
	}

	// Compare with DB columns
	dbCols := make(map[string]*ColumnMetadata)
	for _, col := range dbTable.Columns {
		dbCols[strings.ToLower(col.Name)] = col
	}

	// Check for missing columns in DB
	for colName, typeName := range migColumns {
		if dbCol, exists := dbCols[colName]; !exists {
			result.SchemaDrift = append(result.SchemaDrift, SchemaDrift{
				Object:      fmt.Sprintf("%s.%s", tableName, colName),
				ObjectType:  "COLUMN",
				Description: fmt.Sprintf("Column %s defined in migration but not found in database", colName),
				DriftType:   "missing_in_db",
			})
		} else {
			// Basic type check (using helper if compatible)
			// Normalize types before comparison (very rough)
			if !areTypesCompatible(typeName, dbCol.DataType) {
				// Don't flag specific errors yet as type mapping is complex, but checking existence is broken
				// result.Warnings = append(result.Warnings, fmt.Sprintf("Possible type mismatch for %s.%s: migration=%s, db=%s", tableName, colName, typeName, dbCol.DataType))
			}
		}
	}
}

// compareConstraints compares constraint definitions
func (sc *SchemaComparator) compareConstraints(schema, tableName string, stmt types.Statement, dbTable *TableMetadata, result *ComparisonResult) {
	// Parse to extract constraints (PK, Unique) if possible
	tree, err := pg_query.Parse(stmt.SQL)
	if err == nil && len(tree.Stmts) > 0 {
		if createStmt := tree.Stmts[0].Stmt.GetCreateStmt(); createStmt != nil {
			for _, elt := range createStmt.TableElts {
				if constraint := elt.GetConstraint(); constraint != nil {
					// Check named constraints
					if constraint.Conname != "" {
						found := false
						for _, dbUnq := range dbTable.Constraints {
							if strings.EqualFold(dbUnq.Name, constraint.Conname) {
								found = true
								break
							}
						}
						if !found {
							result.SchemaDrift = append(result.SchemaDrift, SchemaDrift{
								Object:      fmt.Sprintf("%s.%s", tableName, constraint.Conname),
								ObjectType:  "CONSTRAINT",
								Description: fmt.Sprintf("Constraint %s defined in migration but not found in database", constraint.Conname),
								DriftType:   "missing_in_db",
							})
						}
					}
				}
			}
		}
	}

	// Basic PK Check
	hasPK := false
	for _, constraint := range dbTable.Constraints {
		if constraint.Type == "PRIMARY KEY" {
			hasPK = true
			break
		}
	}

	if !hasPK {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Table %s.%s has no primary key in database", schema, tableName))
	}
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
			if slices.Contains(aliases, dbType) {
				return true
			}
		}
		if dbType == canonical {
			if slices.Contains(aliases, migType) {
				return true
			}
		}
	}

	return false
}

func normalizeType(typ string) string {
	typ = strings.ToLower(strings.TrimSpace(typ))
	// Remove precision/length for comparison
	if idx := strings.Index(typ, "("); idx > 0 {
		typ = typ[:idx]
	}
	return typ
}

func buildMetadataSignatureLines(dbMeta *DatabaseMetadata) []string {
	lines := make([]string, 0, 1024)

	extensionNames := make([]string, 0, len(dbMeta.Extensions))
	for name := range dbMeta.Extensions {
		extensionNames = append(extensionNames, name)
	}
	sort.Strings(extensionNames)
	for _, name := range extensionNames {
		ext := dbMeta.Extensions[name]
		lines = append(lines, fmt.Sprintf("extension|%s|%s", strings.ToLower(ext.Name), strings.ToLower(ext.Version)))
	}

	schemaNames := make([]string, 0, len(dbMeta.Schemas))
	for schemaName := range dbMeta.Schemas {
		schemaNames = append(schemaNames, schemaName)
	}
	sort.Strings(schemaNames)

	for _, schemaName := range schemaNames {
		schemaMeta := dbMeta.Schemas[schemaName]

		tableNames := make([]string, 0, len(schemaMeta.Tables))
		for tableName := range schemaMeta.Tables {
			tableNames = append(tableNames, tableName)
		}
		sort.Strings(tableNames)

		for _, tableName := range tableNames {
			table := schemaMeta.Tables[tableName]
			lines = append(lines, fmt.Sprintf("table|%s.%s", strings.ToLower(schemaName), strings.ToLower(tableName)))

			for idx, col := range table.Columns {
				lines = append(lines, fmt.Sprintf(
					"column|%s.%s|%04d|%s|%s|notnull=%t|default=%s",
					strings.ToLower(schemaName),
					strings.ToLower(tableName),
					idx,
					strings.ToLower(col.Name),
					schemamodel.NormalizeSQLWhitespace(col.DataType),
					!col.IsNullable,
					schemamodel.NormalizeSQLWhitespace(col.DefaultValue),
				))
			}

			for _, constraint := range table.Constraints {
				lines = append(lines, fmt.Sprintf(
					"constraint|%s.%s:%s|%s",
					strings.ToLower(schemaName),
					strings.ToLower(tableName),
					strings.ToLower(constraint.Name),
					schemamodel.NormalizeSQLWhitespace(constraint.Type),
				))
			}

			for _, index := range table.Indexes {
				lines = append(lines, fmt.Sprintf(
					"index|%s.%s:%s|%s",
					strings.ToLower(schemaName),
					strings.ToLower(tableName),
					strings.ToLower(index.Name),
					schemamodel.NormalizeSQLWhitespace(index.Definition),
				))
			}
		}

		for viewName, view := range schemaMeta.Views {
			lines = append(lines, fmt.Sprintf(
				"view|%s.%s|%s",
				strings.ToLower(schemaName),
				strings.ToLower(viewName),
				schemamodel.NormalizeSQLWhitespace(view.Definition),
			))
		}

		for functionName, funcs := range schemaMeta.Functions {
			for _, fn := range funcs {
				lines = append(lines, fmt.Sprintf(
					"function|%s.%s(%s)|%s",
					strings.ToLower(schemaName),
					strings.ToLower(functionName),
					schemamodel.NormalizeSQLWhitespace(fn.Signature),
					schemamodel.NormalizeSQLWhitespace(fn.Body),
				))
			}
		}
	}

	return lines
}

// Simplified constraint and type comparison helpers
// Full comparison would require detailed CREATE statement parsing
