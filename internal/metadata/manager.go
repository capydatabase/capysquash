// Package metadata provides database metadata extraction and management.
// It handles introspection of PostgreSQL databases, schema analysis,
// and metadata collection for migration squashing operations.
package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	catalogsqlc "github.com/capysquash/pgsquash-engine/internal/metadata/sqlc"
	"github.com/capysquash/pgsquash-engine/internal/parser"
	"github.com/capysquash/pgsquash-engine/internal/utils"
)

// MetadataManager provides comprehensive PostgreSQL metadata management
// with lazy-loading, caching, and search path resolution
type MetadataManager struct {
	cache      map[string]*DatabaseMetadata
	lastUpdate map[string]time.Time
	cacheTTL   time.Duration
	mutex      sync.RWMutex
	db         *sql.DB
	queries    *catalogsqlc.Queries
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
		queries:    catalogsqlc.New(db),
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
	return slices.Contains(slice, item)
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
	versionString, err := m.queries.GetPostgresVersion(ctx)
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
				if _, err := fmt.Sscanf(versionParts[0], "%d", &version.Major); err != nil {
					utils.GetDefaultLogger().Warn("Failed to parse major version from %s: %v", versionParts[0], err)
				}
				if _, err := fmt.Sscanf(versionParts[1], "%d", &version.Minor); err != nil {
					utils.GetDefaultLogger().Warn("Failed to parse minor version from %s: %v", versionParts[1], err)
				}
				if len(versionParts) >= 3 {
					if _, err := fmt.Sscanf(versionParts[2], "%d", &version.Patch); err != nil {
						utils.GetDefaultLogger().Warn("Failed to parse patch version from %s: %v", versionParts[2], err)
					}
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
	searchPathStr, err := m.queries.GetSearchPath(ctx)
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

	schemaRows, err := m.queries.ListUserSchemas(ctx)
	if err != nil {
		return nil, err
	}

	for _, schemaName := range schemaRows {

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

	tableRows, err := m.queries.ListTablesForSchema(ctx, schemaName)
	if err != nil {
		return nil, err
	}

	for _, row := range tableRows {
		tableName := row.TableName

		table := &TableMetadata{
			Name:        tableName,
			Schema:      schemaName,
			Columns:     []*ColumnMetadata{},
			Constraints: []*ConstraintMetadata{},
			Indexes:     []*IndexMetadata{},
			Triggers:    []*TriggerMetadata{},
			Policies:    []*PolicyMetadata{},
		}

		if row.TableComment != "" {
			table.Comment = row.TableComment
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

	rows, err := m.queries.ListExtensions(ctx)
	if err != nil {
		return nil, err
	}

	for _, row := range rows {

		ext := &ExtensionMetadata{
			Name:        row.Name,
			Version:     row.Version,
			Relocatable: boolFromNullBool(row.Relocatable),
		}

		if row.Schema != "" {
			ext.Schema = row.Schema
		}
		if row.Comment != "" {
			ext.Comment = row.Comment
		}

		extensions[row.Name] = ext
	}

	return extensions, nil
}

// loadColumnsForTable loads column metadata for a specific table
func (m *MetadataManager) loadColumnsForTable(ctx context.Context, schemaName, tableName string, table *TableMetadata) error {
	columnRows, err := m.queries.ListColumnsForTable(ctx, catalogsqlc.ListColumnsForTableParams{
		SchemaName: schemaName,
		TableName:  tableName,
	})
	if err != nil {
		return err
	}

	for _, row := range columnRows {
		col := &ColumnMetadata{
			Name:               row.ColumnName,
			DataType:           row.DataType,
			IsNullable:         row.IsNullable,
			DefaultValue:       row.ColumnDefault,
			IsGenerated:        row.IsGenerated,
			GenerationExpr:     row.GenerationExpression,
			IsIdentity:         row.IsIdentity,
			IdentityGeneration: row.IdentityGeneration,
			Collation:          row.CollationName,
			Comment:            row.ColumnComment,
		}

		table.Columns = append(table.Columns, col)
	}

	return nil
}

// loadConstraintsForTable loads constraint metadata for a specific table
func (m *MetadataManager) loadConstraintsForTable(ctx context.Context, schemaName, tableName string, table *TableMetadata) error {
	constraintRows, err := m.queries.ListConstraintsForTable(ctx, catalogsqlc.ListConstraintsForTableParams{
		SchemaName: schemaName,
		TableName:  tableName,
	})
	if err != nil {
		return err
	}

	for _, row := range constraintRows {
		con := &ConstraintMetadata{
			Name:              row.ConstraintName,
			Type:              row.ConstraintType,
			Columns:           append([]string(nil), row.Columns...),
			OnDelete:          row.OnDelete,
			OnUpdate:          row.OnUpdate,
			IsDeferrable:      row.IsDeferrable,
			InitiallyDeferred: row.InitiallyDeferred,
		}

		if row.RefTable != "" {
			con.RefTable = row.RefTable
			con.RefColumns = append([]string(nil), row.RefColumns...)
		}

		if row.CheckExpr != "" && strings.HasPrefix(con.Type, "CHECK") {
			// Extract just the check expression, not the full constraint definition.
			con.CheckExpression = row.CheckExpr
		}

		table.Constraints = append(table.Constraints, con)
	}

	return nil
}

// loadIndexesForTable loads index metadata for a specific table
func (m *MetadataManager) loadIndexesForTable(ctx context.Context, schemaName, tableName string, table *TableMetadata) error {
	indexRows, err := m.queries.ListIndexesForTable(ctx, catalogsqlc.ListIndexesForTableParams{
		SchemaName: schemaName,
		TableName:  tableName,
	})
	if err != nil {
		return err
	}

	for _, row := range indexRows {
		idx := &IndexMetadata{
			Schema:     schemaName,
			Table:      tableName,
			Name:       row.IndexName,
			Method:     row.IndexMethod,
			Columns:    append([]string(nil), row.Columns...),
			IsUnique:   boolFromNullBool(row.IsUnique),
			IsPrimary:  boolFromNullBool(row.IsPrimary),
			Definition: row.Definition,
		}

		if row.WhereClause != "" {
			idx.IsPartial = true
			idx.WhereClause = row.WhereClause
		}

		table.Indexes = append(table.Indexes, idx)
	}

	return nil
}

// loadTriggersForTable loads trigger metadata for a specific table
func (m *MetadataManager) loadTriggersForTable(ctx context.Context, schemaName, tableName string, table *TableMetadata) error {
	triggerRows, err := m.queries.ListTriggersForTable(ctx, catalogsqlc.ListTriggersForTableParams{
		SchemaName: schemaName,
		TableName:  tableName,
	})
	if err != nil {
		return err
	}

	for _, row := range triggerRows {
		trg := &TriggerMetadata{
			Schema:    schemaName,
			Table:     tableName,
			Name:      row.TriggerName,
			Function:  row.FunctionName,
			Timing:    row.Timing,
			Level:     row.Level,
			Events:    append([]string(nil), row.Events...),
			Condition: row.Condition,
			IsEnabled: row.IsEnabled,
		}

		table.Triggers = append(table.Triggers, trg)
	}

	return nil
}

// loadPoliciesForTable loads RLS policy metadata for a specific table
func (m *MetadataManager) loadPoliciesForTable(ctx context.Context, schemaName, tableName string, table *TableMetadata) error {
	policyRows, err := m.queries.ListPoliciesForTable(ctx, catalogsqlc.ListPoliciesForTableParams{
		SchemaName: schemaName,
		TableName:  tableName,
	})
	if err != nil {
		return err
	}

	for _, row := range policyRows {
		pol := &PolicyMetadata{
			Schema:     schemaName,
			Table:      tableName,
			Name:       row.PolicyName,
			Command:    row.Command,
			Permissive: boolFromNullBool(row.Permissive),
			Roles:      append([]string(nil), row.Roles...),
			Using:      row.UsingExpr,
			WithCheck:  row.WithCheckExpr,
		}

		table.Policies = append(table.Policies, pol)
	}

	// Also check if RLS is enabled on the table
	table.RowSecurity, err = m.queries.GetTableRowSecurity(ctx, catalogsqlc.GetTableRowSecurityParams{
		SchemaName: schemaName,
		TableName:  tableName,
	})
	if err != nil && err != sql.ErrNoRows {
		return err
	}

	return nil
}

// loadViewsForSchema loads view metadata for a specific schema
func (m *MetadataManager) loadViewsForSchema(ctx context.Context, schemaName string) (map[string]*ViewMetadata, error) {
	views := make(map[string]*ViewMetadata)

	viewRows, err := m.queries.ListViewsForSchema(ctx, schemaName)
	if err != nil {
		return nil, err
	}

	for _, row := range viewRows {
		view := &ViewMetadata{
			Name:         row.ViewName,
			Schema:       schemaName,
			Definition:   row.Definition,
			Comment:      row.Comment,
			Dependencies: []string{},
		}

		views[view.Name] = view
	}

	return views, nil
}

// loadMaterializedViewsForSchema loads materialized view metadata for a specific schema
func (m *MetadataManager) loadMaterializedViewsForSchema(ctx context.Context, schemaName string) (map[string]*MaterializedViewMetadata, error) {
	matViews := make(map[string]*MaterializedViewMetadata)

	matViewRows, err := m.queries.ListMaterializedViewsForSchema(ctx, schemaName)
	if err != nil {
		return nil, err
	}

	for _, row := range matViewRows {
		view := &ViewMetadata{
			Name:         row.MatviewName,
			Schema:       schemaName,
			Definition:   row.Definition,
			Comment:      row.Comment,
			Dependencies: []string{},
		}
		matView := &MaterializedViewMetadata{
			ViewMetadata: view,
			HasData:      boolFromNullBool(row.IsPopulated),
			Indexes:      []string{},
		}

		matViews[view.Name] = matView
	}

	return matViews, nil
}

// loadFunctionsForSchema loads function metadata for a specific schema
func (m *MetadataManager) loadFunctionsForSchema(ctx context.Context, schemaName string) (map[string][]*FunctionMetadata, error) {
	functions := make(map[string][]*FunctionMetadata)

	functionRows, err := m.queries.ListFunctionsForSchema(ctx, schemaName)
	if err != nil {
		return nil, err
	}

	for _, row := range functionRows {
		fn := &FunctionMetadata{
			Name:       row.FunctionName,
			Schema:     schemaName,
			Signature:  row.Signature,
			Language:   row.Language,
			ReturnType: row.ReturnType,
			Parameters: []*Parameter{},
			Body:       row.Body,
			Volatility: row.Volatility,
			IsStrict:   boolFromNullBool(row.IsStrict),
			Security:   row.Security,
			Comment:    row.Comment,
		}

		// Functions can be overloaded, so we store them as arrays
		functions[fn.Name] = append(functions[fn.Name], fn)
	}

	return functions, nil
}

// loadSequencesForSchema loads sequence metadata for a specific schema
func (m *MetadataManager) loadSequencesForSchema(ctx context.Context, schemaName string) (map[string]*SequenceMetadata, error) {
	sequences := make(map[string]*SequenceMetadata)

	sequenceRows, err := m.queries.ListSequencesForSchema(ctx, schemaName)
	if err != nil {
		return nil, err
	}

	for _, row := range sequenceRows {
		seq := &SequenceMetadata{
			Name:      row.SequenceName,
			Schema:    schemaName,
			DataType:  row.DataType,
			Start:     row.StartValue,
			Increment: row.Increment,
			MinValue:  row.MinValue,
			MaxValue:  row.MaxValue,
			Cache:     row.CacheSize,
			Cycle:     boolFromNullBool(row.IsCycle),
			OwnedBy:   row.OwnedBy,
		}

		sequences[seq.Name] = seq
	}

	return sequences, nil
}

// loadTypesForSchema loads custom type metadata for a specific schema
func (m *MetadataManager) loadTypesForSchema(ctx context.Context, schemaName string) (map[string]*TypeMetadata, error) {
	types := make(map[string]*TypeMetadata)

	typeRows, err := m.queries.ListTypesForSchema(ctx, schemaName)
	if err != nil {
		return nil, err
	}

	for _, row := range typeRows {
		typ := &TypeMetadata{
			Name:     row.TypeName,
			Schema:   schemaName,
			Type:     row.TypeKind,
			Elements: append([]string(nil), row.EnumElements...),
			Comment:  row.Comment,
		}

		// For non-enum types, get the full definition
		if typ.Type != "ENUM" {
			def, err := m.queries.GetTypeDefinition(ctx, catalogsqlc.GetTypeDefinitionParams{
				TypeName:   typ.Name,
				SchemaName: schemaName,
			})
			if err == nil && def != "" {
				typ.Definition = def
			}
		}

		types[typ.Name] = typ
	}

	return types, nil
}

func boolFromNullBool(value sql.NullBool) bool {
	return value.Valid && value.Bool
}
