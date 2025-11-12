// Package metadata provides database metadata extraction and management.
// It handles introspection of PostgreSQL databases, schema analysis,
// and metadata collection for migration squashing operations.
package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/parser"
)

// MetadataManager provides comprehensive PostgreSQL metadata management
// with lazy-loading, caching, and search path resolution
type MetadataManager struct {
	cache      map[string]*DatabaseMetadata
	lastUpdate map[string]time.Time
	cacheTTL   time.Duration
	mutex      sync.RWMutex
	db         *sql.DB
	hitCount   int64
	missCount  int64
}

// DatabaseMetadata represents complete PostgreSQL database metadata
type DatabaseMetadata struct {
	Database   string                        `json:"database"`
	Schemas    map[string]*SchemaMetadata    `json:"schemas"`
	SearchPath []string                      `json:"search_path"`
	Extensions map[string]*ExtensionMetadata `json:"extensions"`
	IsSystemDB bool                          `json:"is_system_db"`
	Version    *PostgreSQLVersion            `json:"version"`
	CreatedAt  time.Time                     `json:"created_at"`
}

// SchemaMetadata represents a PostgreSQL schema with all its objects
type SchemaMetadata struct {
	Name              string                               `json:"name"`
	Tables            map[string]*TableMetadata            `json:"tables"`
	Views             map[string]*ViewMetadata             `json:"views"`
	MaterializedViews map[string]*MaterializedViewMetadata `json:"materialized_views"`
	Functions         map[string][]*FunctionMetadata       `json:"functions"` // Support overloading
	Sequences         map[string]*SequenceMetadata         `json:"sequences"`
	Types             map[string]*TypeMetadata             `json:"types"`
	Indexes           map[string]*IndexMetadata            `json:"indexes"`
	IsSystem          bool                                 `json:"is_system"`
}

// TableMetadata represents comprehensive table information
type TableMetadata struct {
	Name        string                `json:"name"`
	Schema      string                `json:"schema"`
	Columns     []*ColumnMetadata     `json:"columns"`
	Constraints []*ConstraintMetadata `json:"constraints"`
	Indexes     []*IndexMetadata      `json:"indexes"`
	Triggers    []*TriggerMetadata    `json:"triggers"`
	Policies    []*PolicyMetadata     `json:"policies"`
	Comment     string                `json:"comment,omitempty"`
	Tablespace  string                `json:"tablespace,omitempty"`
	RowSecurity bool                  `json:"row_security"`
}

// ColumnMetadata represents detailed column information
type ColumnMetadata struct {
	Name               string `json:"name"`
	DataType           string `json:"data_type"`
	IsNullable         bool   `json:"is_nullable"`
	DefaultValue       string `json:"default_value,omitempty"`
	IsGenerated        bool   `json:"is_generated"`
	GenerationExpr     string `json:"generation_expr,omitempty"`
	IsIdentity         bool   `json:"is_identity"`
	IdentityGeneration string `json:"identity_generation,omitempty"`
	Collation          string `json:"collation,omitempty"`
	Comment            string `json:"comment,omitempty"`
}

// ConstraintMetadata represents table constraints
type ConstraintMetadata struct {
	Name              string   `json:"name"`
	Type              string   `json:"type"` // PRIMARY KEY, FOREIGN KEY, UNIQUE, CHECK
	Columns           []string `json:"columns"`
	RefTable          string   `json:"ref_table,omitempty"`
	RefColumns        []string `json:"ref_columns,omitempty"`
	OnDelete          string   `json:"on_delete,omitempty"`
	OnUpdate          string   `json:"on_update,omitempty"`
	CheckExpression   string   `json:"check_expression,omitempty"`
	IsDeferrable      bool     `json:"is_deferrable"`
	InitiallyDeferred bool     `json:"initially_deferred"`
}

// IndexMetadata represents index information with parent table references
type IndexMetadata struct {
	Name          string         `json:"name"`
	Schema        string         `json:"schema"`
	Table         string         `json:"table"`
	Columns       []string       `json:"columns"`
	Expressions   []string       `json:"expressions,omitempty"`
	Method        string         `json:"method"` // BTREE, HASH, GIN, GIST, etc.
	IsUnique      bool           `json:"is_unique"`
	IsPrimary     bool           `json:"is_primary"`
	IsPartial     bool           `json:"is_partial"`
	WhereClause   string         `json:"where_clause,omitempty"`
	Definition    string         `json:"definition"`
	TableMetadata *TableMetadata `json:"-"` // Back-reference
}

// ViewMetadata represents view information
type ViewMetadata struct {
	Name         string   `json:"name"`
	Schema       string   `json:"schema"`
	Definition   string   `json:"definition"`
	Dependencies []string `json:"dependencies"`
	Comment      string   `json:"comment,omitempty"`
}

// MaterializedViewMetadata represents materialized view information
type MaterializedViewMetadata struct {
	*ViewMetadata
	HasData    bool     `json:"has_data"`
	Indexes    []string `json:"indexes"`
	Tablespace string   `json:"tablespace,omitempty"`
}

// FunctionMetadata represents function information with overloading support
type FunctionMetadata struct {
	Name       string       `json:"name"`
	Schema     string       `json:"schema"`
	Signature  string       `json:"signature"`
	Language   string       `json:"language"`
	ReturnType string       `json:"return_type"`
	Parameters []*Parameter `json:"parameters"`
	Body       string       `json:"body"`
	Volatility string       `json:"volatility"` // VOLATILE, STABLE, IMMUTABLE
	IsStrict   bool         `json:"is_strict"`
	Security   string       `json:"security"` // DEFINER, INVOKER
	Comment    string       `json:"comment,omitempty"`
}

// Parameter represents function parameter
type Parameter struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Mode    string `json:"mode"` // IN, OUT, INOUT
	Default string `json:"default,omitempty"`
}

// SequenceMetadata represents sequence information
type SequenceMetadata struct {
	Name      string `json:"name"`
	Schema    string `json:"schema"`
	DataType  string `json:"data_type"`
	Start     int64  `json:"start"`
	Increment int64  `json:"increment"`
	MinValue  int64  `json:"min_value"`
	MaxValue  int64  `json:"max_value"`
	Cache     int64  `json:"cache"`
	Cycle     bool   `json:"cycle"`
	OwnedBy   string `json:"owned_by,omitempty"`
}

// TypeMetadata represents custom type information
type TypeMetadata struct {
	Name       string   `json:"name"`
	Schema     string   `json:"schema"`
	Type       string   `json:"type"` // ENUM, COMPOSITE, DOMAIN, RANGE
	Definition string   `json:"definition"`
	Elements   []string `json:"elements,omitempty"` // For ENUM types
	Comment    string   `json:"comment,omitempty"`
}

// TriggerMetadata represents trigger information
type TriggerMetadata struct {
	Name      string   `json:"name"`
	Table     string   `json:"table"`
	Schema    string   `json:"schema"`
	Function  string   `json:"function"`
	Events    []string `json:"events"` // INSERT, UPDATE, DELETE
	Timing    string   `json:"timing"` // BEFORE, AFTER, INSTEAD OF
	Level     string   `json:"level"`  // ROW, STATEMENT
	Condition string   `json:"condition,omitempty"`
	IsEnabled bool     `json:"is_enabled"`
}

// PolicyMetadata represents RLS policy information
type PolicyMetadata struct {
	Name       string   `json:"name"`
	Table      string   `json:"table"`
	Schema     string   `json:"schema"`
	Command    string   `json:"command"`    // ALL, SELECT, INSERT, UPDATE, DELETE
	Permissive bool     `json:"permissive"` // PERMISSIVE or RESTRICTIVE
	Roles      []string `json:"roles"`
	Using      string   `json:"using,omitempty"`
	WithCheck  string   `json:"with_check,omitempty"`
}

// ExtensionMetadata represents extension information
type ExtensionMetadata struct {
	Name        string `json:"name"`
	Schema      string `json:"schema,omitempty"`
	Version     string `json:"version"`
	Comment     string `json:"comment,omitempty"`
	Relocatable bool   `json:"relocatable"`
}

// PostgreSQLVersion represents PostgreSQL version information
type PostgreSQLVersion struct {
	Major    int             `json:"major"`
	Minor    int             `json:"minor"`
	Patch    int             `json:"patch"`
	Features map[string]bool `json:"features"`
}

// NewMetadataManager creates a new metadata manager
func NewMetadataManager(db *sql.DB, cacheTTL time.Duration) *MetadataManager {
	if cacheTTL == 0 {
		cacheTTL = 15 * time.Minute
	}

	return &MetadataManager{
		cache:      make(map[string]*DatabaseMetadata),
		lastUpdate: make(map[string]time.Time),
		cacheTTL:   cacheTTL,
		db:         db,
	}
}

// GetMetadata retrieves database metadata with caching
func (m *MetadataManager) GetMetadata(ctx context.Context, database string) (*DatabaseMetadata, error) {
	m.mutex.RLock()
	if meta, exists := m.cache[database]; exists {
		if time.Since(m.lastUpdate[database]) < m.cacheTTL {
			m.mutex.RUnlock()
			m.hitCount++
			return meta, nil
		}
	}
	m.mutex.RUnlock()

	m.missCount++
	return m.loadAndCacheMetadata(ctx, database)
}

// loadAndCacheMetadata loads metadata from database and caches it
func (m *MetadataManager) loadAndCacheMetadata(ctx context.Context, database string) (*DatabaseMetadata, error) {
	meta, err := m.loadMetadataFromDB(ctx, database)
	if err != nil {
		return nil, err
	}

	m.mutex.Lock()
	m.cache[database] = meta
	m.lastUpdate[database] = time.Now()
	m.mutex.Unlock()

	return meta, nil
}

// InvalidateMetadata removes cached metadata
func (m *MetadataManager) InvalidateMetadata(database string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	delete(m.cache, database)
	delete(m.lastUpdate, database)
}

// GetCacheStats returns cache performance statistics
func (m *MetadataManager) GetCacheStats() (hits, misses int64, ratio float64) {
	total := m.hitCount + m.missCount
	if total == 0 {
		return 0, 0, 0.0
	}
	return m.hitCount, m.missCount, float64(m.hitCount) / float64(total)
}

// PostgreSQL-specific search path resolution
func (dbMeta *DatabaseMetadata) SearchObject(searchPath []string, objectName string) (schema, object string) {
	if len(searchPath) == 0 {
		searchPath = dbMeta.GetSearchPath()
	}

	for _, schemaName := range searchPath {
		schema, exists := dbMeta.Schemas[schemaName]
		if !exists {
			continue
		}

		// Check multiple object types in priority order
		if _, exists := schema.Tables[objectName]; exists {
			return schemaName, objectName
		}
		if _, exists := schema.Views[objectName]; exists {
			return schemaName, objectName
		}
		if _, exists := schema.MaterializedViews[objectName]; exists {
			return schemaName, objectName
		}
		if _, exists := schema.Functions[objectName]; exists {
			return schemaName, objectName
		}
		if _, exists := schema.Sequences[objectName]; exists {
			return schemaName, objectName
		}
		if _, exists := schema.Types[objectName]; exists {
			return schemaName, objectName
		}
	}
	return "", ""
}

// SearchTable searches for a table in the search path
func (dbMeta *DatabaseMetadata) SearchTable(searchPath []string, tableName string) (string, *TableMetadata) {
	if len(searchPath) == 0 {
		searchPath = dbMeta.GetSearchPath()
	}

	for _, schemaName := range searchPath {
		if schema, exists := dbMeta.Schemas[schemaName]; exists {
			if table, exists := schema.Tables[tableName]; exists {
				return schemaName, table
			}
		}
	}
	return "", nil
}

// SearchFunctions searches for functions supporting overloading
func (dbMeta *DatabaseMetadata) SearchFunctions(searchPath []string, funcName string) ([]string, []*FunctionMetadata) {
	if len(searchPath) == 0 {
		searchPath = dbMeta.GetSearchPath()
	}

	var schemas []string
	var functions []*FunctionMetadata

	for _, schemaName := range searchPath {
		if schema, exists := dbMeta.Schemas[schemaName]; exists {
			if funcs, exists := schema.Functions[funcName]; exists {
				for _, function := range funcs {
					schemas = append(schemas, schemaName)
					functions = append(functions, function)
				}
			}
		}
	}
	return schemas, functions
}

// GetSearchPath returns the database search path with proper defaults
func (dbMeta *DatabaseMetadata) GetSearchPath() []string {
	if len(dbMeta.SearchPath) == 0 {
		return []string{"public"} // PostgreSQL default
	}
	return dbMeta.SearchPath
}

// SetSearchPath sets the database search path with system path filtering
func (dbMeta *DatabaseMetadata) SetSearchPath(path []string) {
	var userPath []string
	for _, schema := range path {
		if !IsSystemSchema(schema) {
			userPath = append(userPath, schema)
		}
	}
	dbMeta.SearchPath = userPath
}

// IsSystemSchema checks if a schema is a PostgreSQL system schema
func IsSystemSchema(schemaName string) bool {
	systemSchemas := map[string]bool{
		"information_schema": true,
		"pg_catalog":         true,
		"pg_toast":           true,
	}

	schemaLower := strings.ToLower(schemaName)
	return systemSchemas[schemaLower] ||
		strings.HasPrefix(schemaLower, "pg_temp_") ||
		strings.HasPrefix(schemaLower, "pg_toast_")
}

// IsSystemTable checks if a table is a PostgreSQL system table
func IsSystemTable(tableName string) bool {
	tableLower := strings.ToLower(tableName)
	return strings.HasPrefix(tableLower, "pg_") ||
		strings.HasPrefix(tableLower, "sql_")
}

// IsSystemFunction checks if a function is a PostgreSQL system function
func IsSystemFunction(funcName string, schemaName string) bool {
	if schemaName != "" && !IsSystemSchema(schemaName) {
		return false
	}

	// Simplified check - would need comprehensive system function catalog
	funcLower := strings.ToLower(funcName)
	systemFunctions := map[string]bool{
		"current_user":      true,
		"current_timestamp": true,
		"now":               true,
		"version":           true,
		// Add more system functions as needed
	}

	return systemFunctions[funcLower]
}

// AnalyzeViewDependencies extracts dependencies from view definitions
func (dbMeta *DatabaseMetadata) AnalyzeViewDependencies(view *ViewMetadata) ([]string, error) {
	if view.Definition == "" {
		return nil, errors.NewError(
			errors.ErrorCodeAnalysisError,
			"view definition is empty",
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}

	// Parse view definition to extract table references
	// This would use the parser to analyze the SQL
	migration, err := parser.ParseMigration(view.Definition, "")
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeAnalysisError,
			fmt.Sprintf("failed to analyze view %s", view.Name),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	var dependencies []string
	for _, stmt := range migration.Statements {
		for _, dep := range stmt.Dependencies {
			if !contains(dependencies, dep) {
				dependencies = append(dependencies, dep)
			}
		}
	}

	return dependencies, nil
}

// contains checks if a string slice contains a value
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// loadMetadataFromDB loads comprehensive metadata from PostgreSQL
func (m *MetadataManager) loadMetadataFromDB(ctx context.Context, database string) (*DatabaseMetadata, error) {
	meta := &DatabaseMetadata{
		Database:   database,
		Schemas:    make(map[string]*SchemaMetadata),
		Extensions: make(map[string]*ExtensionMetadata),
		CreatedAt:  time.Now(),
	}

	// Load PostgreSQL version
	version, err := m.loadVersion(ctx)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeAnalysisError,
			"loading PostgreSQL version",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}
	meta.Version = version

	// Load search path
	searchPath, err := m.loadSearchPath(ctx)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeAnalysisError,
			"loading search path",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}
	meta.SearchPath = searchPath

	// Load schemas
	schemas, err := m.loadSchemas(ctx)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeAnalysisError,
			"loading schemas",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}
	meta.Schemas = schemas

	// Load extensions
	extensions, err := m.loadExtensions(ctx)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeAnalysisError,
			"loading extensions",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}
	meta.Extensions = extensions

	return meta, nil
}

// loadVersion loads PostgreSQL version information
func (m *MetadataManager) loadVersion(ctx context.Context) (*PostgreSQLVersion, error) {
	var versionString string
	err := m.db.QueryRowContext(ctx, "SELECT version()").Scan(&versionString)
	if err != nil {
		return nil, err
	}

	// Parse version string - simplified implementation
	version := &PostgreSQLVersion{
		Features: make(map[string]bool),
	}

	// Extract major.minor from version string
	if strings.Contains(versionString, "PostgreSQL") {
		parts := strings.Fields(versionString)
		if len(parts) >= 2 {
			versionParts := strings.Split(parts[1], ".")
			if len(versionParts) >= 2 {
				_, _ = fmt.Sscanf(versionParts[0], "%d", &version.Major)
				_, _ = fmt.Sscanf(versionParts[1], "%d", &version.Minor)
				if len(versionParts) >= 3 {
					_, _ = fmt.Sscanf(versionParts[2], "%d", &version.Patch)
				}
			}
		}
	}

	// Set feature flags based on version
	if version.Major >= 14 {
		version.Features["multirange_types"] = true
	}
	if version.Major >= 15 {
		version.Features["merge_statements"] = true
		version.Features["enhanced_rls"] = true
	}

	return version, nil
}

// loadSearchPath loads the current search path
func (m *MetadataManager) loadSearchPath(ctx context.Context) ([]string, error) {
	var searchPathStr string
	err := m.db.QueryRowContext(ctx, "SHOW search_path").Scan(&searchPathStr)
	if err != nil {
		return []string{"public"}, nil // Return default on error
	}

	// Parse search path string
	paths := strings.Split(searchPathStr, ",")
	for i, path := range paths {
		paths[i] = strings.TrimSpace(path)
		// Handle "$user" placeholder
		if paths[i] == "$user" {
			var currentUser string
			if err := m.db.QueryRowContext(ctx, "SELECT current_user").Scan(&currentUser); err == nil {
				paths[i] = currentUser
			}
		}
	}

	return paths, nil
}

// loadSchemas loads all schema metadata
func (m *MetadataManager) loadSchemas(ctx context.Context) (map[string]*SchemaMetadata, error) {
	schemas := make(map[string]*SchemaMetadata)

	// Load schema names
	rows, err := m.db.QueryContext(ctx, `
		SELECT schema_name
		FROM information_schema.schemata
		WHERE schema_name NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
		ORDER BY schema_name
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var schemaName string
		if err := rows.Scan(&schemaName); err != nil {
			continue
		}

		schema := &SchemaMetadata{
			Name:              schemaName,
			Tables:            make(map[string]*TableMetadata),
			Views:             make(map[string]*ViewMetadata),
			MaterializedViews: make(map[string]*MaterializedViewMetadata),
			Functions:         make(map[string][]*FunctionMetadata),
			Sequences:         make(map[string]*SequenceMetadata),
			Types:             make(map[string]*TypeMetadata),
			Indexes:           make(map[string]*IndexMetadata),
			IsSystem:          IsSystemSchema(schemaName),
		}

		// Load tables for this schema
		tables, err := m.loadTablesForSchema(ctx, schemaName)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeAnalysisError,
				fmt.Sprintf("loading tables for schema %s", schemaName),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}
		schema.Tables = tables

		// Load views
		views, err := m.loadViewsForSchema(ctx, schemaName)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeAnalysisError,
				fmt.Sprintf("loading views for schema %s", schemaName),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}
		schema.Views = views

		// Load materialized views
		matViews, err := m.loadMaterializedViewsForSchema(ctx, schemaName)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeAnalysisError,
				fmt.Sprintf("loading materialized views for schema %s", schemaName),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}
		schema.MaterializedViews = matViews

		// Load functions
		functions, err := m.loadFunctionsForSchema(ctx, schemaName)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeAnalysisError,
				fmt.Sprintf("loading functions for schema %s", schemaName),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}
		schema.Functions = functions

		// Load sequences
		sequences, err := m.loadSequencesForSchema(ctx, schemaName)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeAnalysisError,
				fmt.Sprintf("loading sequences for schema %s", schemaName),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}
		schema.Sequences = sequences

		// Load types
		types, err := m.loadTypesForSchema(ctx, schemaName)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeAnalysisError,
				fmt.Sprintf("loading types for schema %s", schemaName),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}
		schema.Types = types

		schemas[schemaName] = schema
	}

	return schemas, nil
}

// loadTablesForSchema loads table metadata for a specific schema
func (m *MetadataManager) loadTablesForSchema(ctx context.Context, schemaName string) (map[string]*TableMetadata, error) {
	tables := make(map[string]*TableMetadata)

	rows, err := m.db.QueryContext(ctx, `
		SELECT table_name, table_comment
		FROM information_schema.tables t
		LEFT JOIN (
			SELECT schemaname, tablename, description as table_comment
			FROM pg_tables pt
			JOIN pg_description pd ON pd.objoid = (
				SELECT oid FROM pg_class WHERE relname = pt.tablename
				AND relnamespace = (SELECT oid FROM pg_namespace WHERE nspname = pt.schemaname)
			) AND pd.objsubid = 0
		) tc ON tc.schemaname = t.table_schema AND tc.tablename = t.table_name
		WHERE table_schema = $1 AND table_type = 'BASE TABLE'
		ORDER BY table_name
	`, schemaName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var tableName string
		var comment sql.NullString
		if err := rows.Scan(&tableName, &comment); err != nil {
			continue
		}

		table := &TableMetadata{
			Name:        tableName,
			Schema:      schemaName,
			Columns:     []*ColumnMetadata{},
			Constraints: []*ConstraintMetadata{},
			Indexes:     []*IndexMetadata{},
			Triggers:    []*TriggerMetadata{},
			Policies:    []*PolicyMetadata{},
		}

		if comment.Valid {
			table.Comment = comment.String
		}

		// Load detailed table metadata
		if err := m.loadColumnsForTable(ctx, schemaName, tableName, table); err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeAnalysisError,
				fmt.Sprintf("loading columns for %s.%s", schemaName, tableName),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}

		if err := m.loadConstraintsForTable(ctx, schemaName, tableName, table); err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeAnalysisError,
				fmt.Sprintf("loading constraints for %s.%s", schemaName, tableName),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}

		if err := m.loadIndexesForTable(ctx, schemaName, tableName, table); err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeAnalysisError,
				fmt.Sprintf("loading indexes for %s.%s", schemaName, tableName),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}

		if err := m.loadTriggersForTable(ctx, schemaName, tableName, table); err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeAnalysisError,
				fmt.Sprintf("loading triggers for %s.%s", schemaName, tableName),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}

		if err := m.loadPoliciesForTable(ctx, schemaName, tableName, table); err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeAnalysisError,
				fmt.Sprintf("loading policies for %s.%s", schemaName, tableName),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}

		tables[tableName] = table
	}

	return tables, nil
}

// loadExtensions loads extension metadata
func (m *MetadataManager) loadExtensions(ctx context.Context) (map[string]*ExtensionMetadata, error) {
	extensions := make(map[string]*ExtensionMetadata)

	rows, err := m.db.QueryContext(ctx, `
		SELECT extname, extversion, nspname, comment
		FROM pg_extension e
		LEFT JOIN pg_namespace n ON n.oid = e.extnamespace
		LEFT JOIN pg_description d ON d.objoid = e.oid AND d.objsubid = 0
		ORDER BY extname
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var name, version string
		var schema, comment sql.NullString
		if err := rows.Scan(&name, &version, &schema, &comment); err != nil {
			continue
		}

		ext := &ExtensionMetadata{
			Name:    name,
			Version: version,
		}

		if schema.Valid {
			ext.Schema = schema.String
		}
		if comment.Valid {
			ext.Comment = comment.String
		}

		extensions[name] = ext
	}

	return extensions, nil
}

// loadColumnsForTable loads column metadata for a specific table
func (m *MetadataManager) loadColumnsForTable(ctx context.Context, schemaName, tableName string, table *TableMetadata) error {
	rows, err := m.db.QueryContext(ctx, `
		SELECT
			column_name,
			data_type,
			is_nullable = 'YES',
			column_default,
			is_generated = 'ALWAYS',
			generation_expression,
			is_identity = 'YES',
			identity_generation,
			collation_name,
			col_description((quote_ident($1)||'.'||quote_ident($2))::regclass, ordinal_position)
		FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
		ORDER BY ordinal_position
	`, schemaName, tableName)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		col := &ColumnMetadata{}
		var isNullable bool
		var defaultVal, genExpr, identityGen, collation, comment sql.NullString

		if err := rows.Scan(
			&col.Name,
			&col.DataType,
			&isNullable,
			&defaultVal,
			&col.IsGenerated,
			&genExpr,
			&col.IsIdentity,
			&identityGen,
			&collation,
			&comment,
		); err != nil {
			continue
		}

		col.IsNullable = isNullable
		if defaultVal.Valid {
			col.DefaultValue = defaultVal.String
		}
		if genExpr.Valid {
			col.GenerationExpr = genExpr.String
		}
		if identityGen.Valid {
			col.IdentityGeneration = identityGen.String
		}
		if collation.Valid {
			col.Collation = collation.String
		}
		if comment.Valid {
			col.Comment = comment.String
		}

		table.Columns = append(table.Columns, col)
	}

	return rows.Err()
}

// loadConstraintsForTable loads constraint metadata for a specific table
func (m *MetadataManager) loadConstraintsForTable(ctx context.Context, schemaName, tableName string, table *TableMetadata) error {
	rows, err := m.db.QueryContext(ctx, `
		SELECT
			tc.constraint_name,
			tc.constraint_type,
			array_agg(DISTINCT kcu.column_name ORDER BY kcu.column_name),
			ccu.table_name as ref_table,
			array_agg(DISTINCT ccu.column_name ORDER BY ccu.column_name) FILTER (WHERE ccu.column_name IS NOT NULL),
			rc.delete_rule,
			rc.update_rule,
			pgc.condeferrable,
			pgc.condeferred,
			pg_get_constraintdef(pgc.oid) as check_expr
		FROM information_schema.table_constraints tc
		LEFT JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
			AND tc.table_name = kcu.table_name
		LEFT JOIN information_schema.constraint_column_usage ccu
			ON tc.constraint_name = ccu.constraint_name
			AND tc.table_schema = ccu.constraint_schema
		LEFT JOIN information_schema.referential_constraints rc
			ON tc.constraint_name = rc.constraint_name
			AND tc.table_schema = rc.constraint_schema
		LEFT JOIN pg_constraint pgc
			ON pgc.conname = tc.constraint_name
			AND pgc.connamespace = (SELECT oid FROM pg_namespace WHERE nspname = tc.table_schema)
		WHERE tc.table_schema = $1 AND tc.table_name = $2
		GROUP BY tc.constraint_name, tc.constraint_type, ccu.table_name, rc.delete_rule, rc.update_rule, pgc.condeferrable, pgc.condeferred, pgc.oid
		ORDER BY tc.constraint_name
	`, schemaName, tableName)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		con := &ConstraintMetadata{}
		var columns, refColumns []string
		var refTable, onDelete, onUpdate sql.NullString
		var isDeferrable, initiallyDeferred sql.NullBool
		var checkExpr sql.NullString

		if err := rows.Scan(
			&con.Name,
			&con.Type,
			&columns,
			&refTable,
			&refColumns,
			&onDelete,
			&onUpdate,
			&isDeferrable,
			&initiallyDeferred,
			&checkExpr,
		); err != nil {
			continue
		}

		con.Columns = columns
		if refTable.Valid {
			con.RefTable = refTable.String
			con.RefColumns = refColumns
		}
		if onDelete.Valid {
			con.OnDelete = onDelete.String
		}
		if onUpdate.Valid {
			con.OnUpdate = onUpdate.String
		}
		if isDeferrable.Valid {
			con.IsDeferrable = isDeferrable.Bool
		}
		if initiallyDeferred.Valid {
			con.InitiallyDeferred = initiallyDeferred.Bool
		}
		if checkExpr.Valid && strings.HasPrefix(con.Type, "CHECK") {
			// Extract just the check expression, not the full constraint definition
			con.CheckExpression = checkExpr.String
		}

		table.Constraints = append(table.Constraints, con)
	}

	return rows.Err()
}

// loadIndexesForTable loads index metadata for a specific table
func (m *MetadataManager) loadIndexesForTable(ctx context.Context, schemaName, tableName string, table *TableMetadata) error {
	rows, err := m.db.QueryContext(ctx, `
		SELECT
			i.indexrelname as index_name,
			array_agg(a.attname ORDER BY array_position(i.indkey, a.attnum)),
			am.amname as index_method,
			i.indisunique,
			i.indisprimary,
			pg_get_expr(i.indpred, i.indrelid) as where_clause,
			pg_get_indexdef(i.indexrelid) as definition
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_class ic ON ic.oid = i.indexrelid
		JOIN pg_am am ON am.oid = ic.relam
		JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
		WHERE n.nspname = $1 AND c.relname = $2
		GROUP BY i.indexrelname, am.amname, i.indisunique, i.indisprimary, i.indpred, i.indrelid, i.indexrelid
		ORDER BY i.indexrelname
	`, schemaName, tableName)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		idx := &IndexMetadata{
			Schema: schemaName,
			Table:  tableName,
		}
		var columns []string
		var whereClause, definition sql.NullString

		if err := rows.Scan(
			&idx.Name,
			&columns,
			&idx.Method,
			&idx.IsUnique,
			&idx.IsPrimary,
			&whereClause,
			&definition,
		); err != nil {
			continue
		}

		idx.Columns = columns
		idx.IsPartial = whereClause.Valid
		if whereClause.Valid {
			idx.WhereClause = whereClause.String
		}
		if definition.Valid {
			idx.Definition = definition.String
		}

		table.Indexes = append(table.Indexes, idx)
	}

	return rows.Err()
}

// loadTriggersForTable loads trigger metadata for a specific table
func (m *MetadataManager) loadTriggersForTable(ctx context.Context, schemaName, tableName string, table *TableMetadata) error {
	rows, err := m.db.QueryContext(ctx, `
		SELECT
			t.tgname as trigger_name,
			p.proname as function_name,
			CASE t.tgtype & 66
				WHEN 2 THEN 'BEFORE'
				WHEN 64 THEN 'INSTEAD OF'
				ELSE 'AFTER'
			END as timing,
			CASE t.tgtype & 1
				WHEN 1 THEN 'ROW'
				ELSE 'STATEMENT'
			END as level,
			array_remove(ARRAY[
				CASE WHEN t.tgtype & 4 = 4 THEN 'INSERT' END,
				CASE WHEN t.tgtype & 8 = 8 THEN 'DELETE' END,
				CASE WHEN t.tgtype & 16 = 16 THEN 'UPDATE' END,
				CASE WHEN t.tgtype & 32 = 32 THEN 'TRUNCATE' END
			], NULL) as events,
			pg_get_expr(t.tgqual, t.tgrelid) as condition,
			t.tgenabled = 'O' as is_enabled
		FROM pg_trigger t
		JOIN pg_class c ON c.oid = t.tgrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_proc p ON p.oid = t.tgfoid
		WHERE n.nspname = $1 AND c.relname = $2 AND NOT t.tgisinternal
		ORDER BY t.tgname
	`, schemaName, tableName)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		trg := &TriggerMetadata{
			Schema: schemaName,
			Table:  tableName,
		}
		var events []string
		var condition sql.NullString

		if err := rows.Scan(
			&trg.Name,
			&trg.Function,
			&trg.Timing,
			&trg.Level,
			&events,
			&condition,
			&trg.IsEnabled,
		); err != nil {
			continue
		}

		trg.Events = events
		if condition.Valid {
			trg.Condition = condition.String
		}

		table.Triggers = append(table.Triggers, trg)
	}

	return rows.Err()
}

// loadPoliciesForTable loads RLS policy metadata for a specific table
func (m *MetadataManager) loadPoliciesForTable(ctx context.Context, schemaName, tableName string, table *TableMetadata) error {
	rows, err := m.db.QueryContext(ctx, `
		SELECT
			pol.polname as policy_name,
			CASE pol.polcmd
				WHEN 'r' THEN 'SELECT'
				WHEN 'a' THEN 'INSERT'
				WHEN 'w' THEN 'UPDATE'
				WHEN 'd' THEN 'DELETE'
				ELSE 'ALL'
			END as command,
			pol.polpermissive as permissive,
			array_agg(r.rolname) as roles,
			pg_get_expr(pol.polqual, pol.polrelid) as using_expr,
			pg_get_expr(pol.polwithcheck, pol.polrelid) as with_check_expr
		FROM pg_policy pol
		JOIN pg_class c ON c.oid = pol.polrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_roles r ON r.oid = ANY(pol.polroles)
		WHERE n.nspname = $1 AND c.relname = $2
		GROUP BY pol.polname, pol.polcmd, pol.polpermissive, pol.polrelid, pol.polqual, pol.polwithcheck
		ORDER BY pol.polname
	`, schemaName, tableName)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		pol := &PolicyMetadata{
			Schema: schemaName,
			Table:  tableName,
		}
		var roles []string
		var usingExpr, withCheckExpr sql.NullString

		if err := rows.Scan(
			&pol.Name,
			&pol.Command,
			&pol.Permissive,
			&roles,
			&usingExpr,
			&withCheckExpr,
		); err != nil {
			continue
		}

		pol.Roles = roles
		if usingExpr.Valid {
			pol.Using = usingExpr.String
		}
		if withCheckExpr.Valid {
			pol.WithCheck = withCheckExpr.String
		}

		table.Policies = append(table.Policies, pol)
	}

	// Also check if RLS is enabled on the table
	err = m.db.QueryRowContext(ctx, `
		SELECT relrowsecurity
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2
	`, schemaName, tableName).Scan(&table.RowSecurity)
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	return rows.Err()
}

// loadViewsForSchema loads view metadata for a specific schema
func (m *MetadataManager) loadViewsForSchema(ctx context.Context, schemaName string) (map[string]*ViewMetadata, error) {
	views := make(map[string]*ViewMetadata)

	rows, err := m.db.QueryContext(ctx, `
		SELECT
			table_name,
			view_definition,
			obj_description((quote_ident($1)||'.'||quote_ident(table_name))::regclass) as comment
		FROM information_schema.views
		WHERE table_schema = $1
		ORDER BY table_name
	`, schemaName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		view := &ViewMetadata{
			Schema:       schemaName,
			Dependencies: []string{},
		}
		var comment sql.NullString

		if err := rows.Scan(&view.Name, &view.Definition, &comment); err != nil {
			continue
		}

		if comment.Valid {
			view.Comment = comment.String
		}

		views[view.Name] = view
	}

	return views, nil
}

// loadMaterializedViewsForSchema loads materialized view metadata for a specific schema
func (m *MetadataManager) loadMaterializedViewsForSchema(ctx context.Context, schemaName string) (map[string]*MaterializedViewMetadata, error) {
	matViews := make(map[string]*MaterializedViewMetadata)

	rows, err := m.db.QueryContext(ctx, `
		SELECT
			matviewname,
			definition,
			ispopulated,
			obj_description((quote_ident($1)||'.'||quote_ident(matviewname))::regclass) as comment
		FROM pg_matviews
		WHERE schemaname = $1
		ORDER BY matviewname
	`, schemaName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		view := &ViewMetadata{
			Schema:       schemaName,
			Dependencies: []string{},
		}
		matView := &MaterializedViewMetadata{
			ViewMetadata: view,
			Indexes:      []string{},
		}
		var comment sql.NullString

		if err := rows.Scan(&view.Name, &view.Definition, &matView.HasData, &comment); err != nil {
			continue
		}

		if comment.Valid {
			view.Comment = comment.String
		}

		matViews[view.Name] = matView
	}

	return matViews, nil
}

// loadFunctionsForSchema loads function metadata for a specific schema
func (m *MetadataManager) loadFunctionsForSchema(ctx context.Context, schemaName string) (map[string][]*FunctionMetadata, error) {
	functions := make(map[string][]*FunctionMetadata)

	rows, err := m.db.QueryContext(ctx, `
		SELECT
			p.proname as function_name,
			pg_get_function_identity_arguments(p.oid) as signature,
			l.lanname as language,
			pg_get_function_result(p.oid) as return_type,
			pg_get_functiondef(p.oid) as body,
			CASE p.provolatile
				WHEN 'i' THEN 'IMMUTABLE'
				WHEN 's' THEN 'STABLE'
				ELSE 'VOLATILE'
			END as volatility,
			p.proisstrict as is_strict,
			CASE p.prosecdef
				WHEN true THEN 'DEFINER'
				ELSE 'INVOKER'
			END as security,
			obj_description(p.oid, 'pg_proc') as comment
		FROM pg_proc p
		JOIN pg_namespace n ON n.oid = p.pronamespace
		JOIN pg_language l ON l.oid = p.prolang
		WHERE n.nspname = $1
		AND p.prokind = 'f' -- Functions only, not procedures
		ORDER BY p.proname, p.oid
	`, schemaName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		fn := &FunctionMetadata{
			Schema:     schemaName,
			Parameters: []*Parameter{},
		}
		var comment sql.NullString

		if err := rows.Scan(
			&fn.Name,
			&fn.Signature,
			&fn.Language,
			&fn.ReturnType,
			&fn.Body,
			&fn.Volatility,
			&fn.IsStrict,
			&fn.Security,
			&comment,
		); err != nil {
			continue
		}

		if comment.Valid {
			fn.Comment = comment.String
		}

		// Functions can be overloaded, so we store them as arrays
		functions[fn.Name] = append(functions[fn.Name], fn)
	}

	return functions, nil
}

// loadSequencesForSchema loads sequence metadata for a specific schema
func (m *MetadataManager) loadSequencesForSchema(ctx context.Context, schemaName string) (map[string]*SequenceMetadata, error) {
	sequences := make(map[string]*SequenceMetadata)

	rows, err := m.db.QueryContext(ctx, `
		SELECT
			c.relname as sequence_name,
			format_type(s.seqtypid, NULL) as data_type,
			s.seqstart as start_value,
			s.seqincrement as increment,
			s.seqmin as min_value,
			s.seqmax as max_value,
			s.seqcache as cache_size,
			s.seqcycle as is_cycle,
			COALESCE(
				(SELECT attrelid::regclass::text || '.' || attname
				FROM pg_attribute
				WHERE attrelid = d.refobjid AND attnum = d.refobjsubid),
				''
			) as owned_by
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_sequence s ON s.seqrelid = c.oid
		LEFT JOIN pg_depend d ON d.objid = c.oid AND d.deptype = 'a'
		WHERE n.nspname = $1 AND c.relkind = 'S'
		ORDER BY c.relname
	`, schemaName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		seq := &SequenceMetadata{
			Schema: schemaName,
		}
		var ownedBy sql.NullString

		if err := rows.Scan(
			&seq.Name,
			&seq.DataType,
			&seq.Start,
			&seq.Increment,
			&seq.MinValue,
			&seq.MaxValue,
			&seq.Cache,
			&seq.Cycle,
			&ownedBy,
		); err != nil {
			continue
		}

		if ownedBy.Valid && ownedBy.String != "" {
			seq.OwnedBy = ownedBy.String
		}

		sequences[seq.Name] = seq
	}

	return sequences, nil
}

// loadTypesForSchema loads custom type metadata for a specific schema
func (m *MetadataManager) loadTypesForSchema(ctx context.Context, schemaName string) (map[string]*TypeMetadata, error) {
	types := make(map[string]*TypeMetadata)

	rows, err := m.db.QueryContext(ctx, `
		SELECT
			t.typname as type_name,
			CASE t.typtype
				WHEN 'e' THEN 'ENUM'
				WHEN 'c' THEN 'COMPOSITE'
				WHEN 'd' THEN 'DOMAIN'
				WHEN 'r' THEN 'RANGE'
				ELSE 'OTHER'
			END as type_kind,
			COALESCE(
				(SELECT array_agg(enumlabel ORDER BY enumsortorder)
				FROM pg_enum WHERE enumtypid = t.oid),
				'{}'
			) as enum_elements,
			obj_description(t.oid, 'pg_type') as comment
		FROM pg_type t
		JOIN pg_namespace n ON n.oid = t.typnamespace
		WHERE n.nspname = $1
		AND t.typtype IN ('e', 'c', 'd', 'r') -- ENUM, COMPOSITE, DOMAIN, RANGE
		ORDER BY t.typname
	`, schemaName)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		typ := &TypeMetadata{
			Schema:   schemaName,
			Elements: []string{},
		}
		var elements []string
		var comment sql.NullString

		if err := rows.Scan(&typ.Name, &typ.Type, &elements, &comment); err != nil {
			continue
		}

		typ.Elements = elements
		if comment.Valid {
			typ.Comment = comment.String
		}

		// For non-enum types, get the full definition
		if typ.Type != "ENUM" {
			var def sql.NullString
			err := m.db.QueryRowContext(ctx, `
				SELECT pg_get_typedef(oid)
				FROM pg_type
				WHERE typname = $1 AND typnamespace = (
					SELECT oid FROM pg_namespace WHERE nspname = $2
				)
			`, typ.Name, schemaName).Scan(&def)
			if err == nil && def.Valid {
				typ.Definition = def.String
			}
		}

		types[typ.Name] = typ
	}

	return types, nil
}
