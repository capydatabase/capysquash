package validation

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/errors"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/types"
)

// ChangeType represents the type of schema change
type ChangeType string

const (
	ChangeTypeCreate     ChangeType = "CREATE"
	ChangeTypeAlter      ChangeType = "ALTER"
	ChangeTypeDrop       ChangeType = "DROP"
	ChangeTypeRename     ChangeType = "RENAME"
	ChangeTypeModify     ChangeType = "MODIFY"
	ChangeTypeRecreate   ChangeType = "RECREATE"
	ChangeTypePermission ChangeType = "PERMISSION"
	ChangeTypeData       ChangeType = "DATA"
)

// RiskLevel represents the risk level of a change
type RiskLevel string

const (
	RiskLevelSafe     RiskLevel = "SAFE"
	RiskLevelLow      RiskLevel = "LOW"
	RiskLevelMedium   RiskLevel = "MEDIUM"
	RiskLevelHigh     RiskLevel = "HIGH"
	RiskLevelCritical RiskLevel = "CRITICAL"
)

// SchemaChange represents a detected schema change
type SchemaChange interface {
	Type() ChangeType
	Risk() RiskLevel
	Description() string
	SQL() []string
	ObjectID() ObjectID
	Details() map[string]interface{}
}

// ObjectID represents a unique object identifier for schema diffing
type ObjectID struct {
	Type   types.ObjectType `json:"type"`
	Schema string            `json:"schema"`
	Name   string            `json:"name"`
}

// String returns a string representation of the ObjectID
func (oid ObjectID) String() string {
	if oid.Schema != "" && oid.Schema != "public" {
		return fmt.Sprintf("%s.%s::%s", oid.Schema, oid.Name, oid.Type)
	}
	return fmt.Sprintf("%s::%s", oid.Name, oid.Type)
}

// QualifiedName returns the schema-qualified name without the type suffix
func (oid ObjectID) QualifiedName() string {
	if oid.Schema != "" && oid.Schema != "public" {
		return fmt.Sprintf("%s.%s", oid.Schema, oid.Name)
	}
	return oid.Name
}

// Schema represents a database schema snapshot
type Schema struct {
	Tables      map[ObjectID]*TableDefinition      `json:"tables"`
	Indexes     map[ObjectID]*IndexDefinition      `json:"indexes"`
	Functions   map[ObjectID]*FunctionDefinition   `json:"functions"`
	Triggers    map[ObjectID]*TriggerDefinition    `json:"triggers"`
	Views       map[ObjectID]*ViewDefinition       `json:"views"`
	Constraints map[ObjectID]*ConstraintDefinition `json:"constraints"`
	Policies    map[ObjectID]*PolicyDefinition     `json:"policies"`
	Extensions  map[ObjectID]*ExtensionDefinition  `json:"extensions"`
	Sequences   map[ObjectID]*SequenceDefinition   `json:"sequences"`
	Types       map[ObjectID]*TypeDefinition       `json:"types"`
	timestamp   time.Time
}

// NewSchema creates a new empty schema
func NewSchema() *Schema {
	return &Schema{
		Tables:      make(map[ObjectID]*TableDefinition),
		Indexes:     make(map[ObjectID]*IndexDefinition),
		Functions:   make(map[ObjectID]*FunctionDefinition),
		Triggers:    make(map[ObjectID]*TriggerDefinition),
		Views:       make(map[ObjectID]*ViewDefinition),
		Constraints: make(map[ObjectID]*ConstraintDefinition),
		Policies:    make(map[ObjectID]*PolicyDefinition),
		Extensions:  make(map[ObjectID]*ExtensionDefinition),
		Sequences:   make(map[ObjectID]*SequenceDefinition),
		Types:       make(map[ObjectID]*TypeDefinition),
		timestamp:   time.Now(),
	}
}

// TableDefinition represents a table structure
type TableDefinition struct {
	ID          ObjectID               `json:"id"`
	Columns     map[string]*Column     `json:"columns"`
	Constraints map[string]*Constraint `json:"constraints"`
	Indexes     []ObjectID             `json:"indexes"`
	Triggers    []ObjectID             `json:"triggers"`
	Policies    []ObjectID             `json:"policies"`
	Tablespace  string                 `json:"tablespace,omitempty"`
	Options     map[string]string      `json:"options,omitempty"`
	Comment     string                 `json:"comment,omitempty"`
}

// Column represents a table column
type Column struct {
	Name         string            `json:"name"`
	DataType     string            `json:"data_type"`
	IsNullable   bool              `json:"is_nullable"`
	DefaultValue string            `json:"default_value,omitempty"`
	IsGenerated  bool              `json:"is_generated"`
	GeneratedExp string            `json:"generated_expression,omitempty"`
	Comment      string            `json:"comment,omitempty"`
	Collation    string            `json:"collation,omitempty"`
	Options      map[string]string `json:"options,omitempty"`
}

// Constraint represents a table constraint
type Constraint struct {
	Name       string            `json:"name"`
	Type       string            `json:"type"`
	Columns    []string          `json:"columns"`
	Expression string            `json:"expression,omitempty"`
	RefTable   string            `json:"ref_table,omitempty"`
	RefColumns []string          `json:"ref_columns,omitempty"`
	OnUpdate   string            `json:"on_update,omitempty"`
	OnDelete   string            `json:"on_delete,omitempty"`
	Options    map[string]string `json:"options,omitempty"`
}

// IndexDefinition represents an index
type IndexDefinition struct {
	ID         ObjectID          `json:"id"`
	Table      ObjectID          `json:"table"`
	Columns    []string          `json:"columns"`
	Method     string            `json:"method"`
	IsUnique   bool              `json:"is_unique"`
	IsPrimary  bool              `json:"is_primary"`
	Expression string            `json:"expression,omitempty"`
	Where      string            `json:"where,omitempty"`
	Options    map[string]string `json:"options,omitempty"`
}

// FunctionDefinition represents a function
type FunctionDefinition struct {
	ID         ObjectID          `json:"id"`
	Parameters []Parameter       `json:"parameters"`
	ReturnType string            `json:"return_type"`
	Language   string            `json:"language"`
	Body       string            `json:"body"`
	Volatility string            `json:"volatility"`
	Security   string            `json:"security"`
	Options    map[string]string `json:"options,omitempty"`
}

// Parameter represents a function parameter
type Parameter struct {
	Name     string `json:"name"`
	DataType string `json:"data_type"`
	Mode     string `json:"mode"` // IN, OUT, INOUT
	Default  string `json:"default,omitempty"`
}

// TriggerDefinition represents a trigger
type TriggerDefinition struct {
	ID        ObjectID          `json:"id"`
	Table     ObjectID          `json:"table"`
	Events    []string          `json:"events"`
	Timing    string            `json:"timing"`
	Function  ObjectID          `json:"function"`
	Condition string            `json:"condition,omitempty"`
	Options   map[string]string `json:"options,omitempty"`
}

// ViewDefinition represents a view
type ViewDefinition struct {
	ID         ObjectID          `json:"id"`
	Definition string            `json:"definition"`
	IsMat      bool              `json:"is_materialized"`
	Options    map[string]string `json:"options,omitempty"`
}

// ConstraintDefinition represents a standalone constraint
type ConstraintDefinition struct {
	ID      ObjectID   `json:"id"`
	Table   ObjectID   `json:"table"`
	Details Constraint `json:"details"`
}

// PolicyDefinition represents an RLS policy
type PolicyDefinition struct {
	ID         ObjectID          `json:"id"`
	Table      ObjectID          `json:"table"`
	Command    string            `json:"command"`
	Roles      []string          `json:"roles"`
	Using      string            `json:"using,omitempty"`
	WithCheck  string            `json:"with_check,omitempty"`
	Permissive bool              `json:"permissive"`
	Options    map[string]string `json:"options,omitempty"`
}

// ExtensionDefinition represents an extension
type ExtensionDefinition struct {
	ID      ObjectID          `json:"id"`
	Version string            `json:"version"`
	Schema  string            `json:"schema"`
	Options map[string]string `json:"options,omitempty"`
}

// SequenceDefinition represents a sequence
type SequenceDefinition struct {
	ID        ObjectID          `json:"id"`
	StartWith int64             `json:"start_with"`
	Increment int64             `json:"increment"`
	MinValue  *int64            `json:"min_value,omitempty"`
	MaxValue  *int64            `json:"max_value,omitempty"`
	Cache     int64             `json:"cache"`
	Cycle     bool              `json:"cycle"`
	OwnedBy   string            `json:"owned_by,omitempty"`
	Options   map[string]string `json:"options,omitempty"`
}

// TypeDefinition represents a custom type
type TypeDefinition struct {
	ID         ObjectID          `json:"id"`
	Kind       string            `json:"kind"` // enum, composite, domain, etc.
	Definition string            `json:"definition"`
	Options    map[string]string `json:"options,omitempty"`
}

// SchemaDiffer performs comprehensive schema comparison
type SchemaDiffer struct {
	db         *sql.DB
	fromSchema *Schema
	toSchema   *Schema
	config     *DiffConfig
	validator  *ExpressionValidator
}

// DiffConfig configures schema diffing behavior
type DiffConfig struct {
	IgnoreComments       bool     `json:"ignore_comments"`
	IgnoreOptions        bool     `json:"ignore_options"`
	IgnoreColumnOrder    bool     `json:"ignore_column_order"`
	IgnoreWhitespace     bool     `json:"ignore_whitespace"`
	CompareExpressions   bool     `json:"compare_expressions"`
	StrictTypeComparison bool     `json:"strict_type_comparison"`
	IgnoreSchemas        []string `json:"ignore_schemas"`
	FocusObjects         []string `json:"focus_objects"`
	MaxConcurrentQueries int      `json:"max_concurrent_queries"`
}

// DefaultDiffConfig returns a default diff configuration
func DefaultDiffConfig() *DiffConfig {
	return &DiffConfig{
		IgnoreComments:       false,
		IgnoreOptions:        false,
		IgnoreColumnOrder:    false,
		IgnoreWhitespace:     true,
		CompareExpressions:   true,
		StrictTypeComparison: false,
		MaxConcurrentQueries: 4,
	}
}

// NewSchemaDiffer creates a new schema differ
func NewSchemaDiffer(db *sql.DB, fromSchema, toSchema *Schema, config *DiffConfig) *SchemaDiffer {
	if config == nil {
		config = DefaultDiffConfig()
	}

	return &SchemaDiffer{
		db:         db,
		fromSchema: fromSchema,
		toSchema:   toSchema,
		config:     config,
		validator:  NewExpressionValidator(db),
	}
}

// Compare performs comprehensive schema comparison
func (sd *SchemaDiffer) Compare(ctx context.Context) ([]SchemaChange, error) {
	var changes []SchemaChange

	// Compare each object type
	tableChanges, err := sd.compareTables(ctx)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation, "comparing tables", nil)
	}
	changes = append(changes, tableChanges...)

	indexChanges, err := sd.compareIndexes(ctx)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation, "comparing indexes", nil)
	}
	changes = append(changes, indexChanges...)

	functionChanges, err := sd.compareFunctions(ctx)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation, "comparing functions", nil)
	}
	changes = append(changes, functionChanges...)

	// Add more comparisons as needed...

	// Sort changes by risk level and type
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Risk() != changes[j].Risk() {
			return getRiskPriority(changes[i].Risk()) > getRiskPriority(changes[j].Risk())
		}
		return changes[i].Type() < changes[j].Type()
	})

	return changes, nil
}

// compareTables compares table definitions
func (sd *SchemaDiffer) compareTables(ctx context.Context) ([]SchemaChange, error) {
	var changes []SchemaChange

	// Find tables in from schema but not in to schema (dropped)
	for id, table := range sd.fromSchema.Tables {
		if _, exists := sd.toSchema.Tables[id]; !exists {
			changes = append(changes, &TableDropChange{
				ID:         id,
				Definition: table,
			})
		}
	}

	// Find tables in to schema but not in from schema (created)
	for id, table := range sd.toSchema.Tables {
		if _, exists := sd.fromSchema.Tables[id]; !exists {
			changes = append(changes, &TableCreateChange{
				ID:         id,
				Definition: table,
			})
		}
	}

	// Compare tables that exist in both schemas (modified)
	for id, fromTable := range sd.fromSchema.Tables {
		if toTable, exists := sd.toSchema.Tables[id]; exists {
			tableChanges, err := sd.compareTableDefinitions(ctx, fromTable, toTable)
			if err != nil {
				return nil, errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation, fmt.Sprintf("comparing table %s", id), nil)
			}
			changes = append(changes, tableChanges...)
		}
	}

	return changes, nil
}

// compareTableDefinitions compares two table definitions
func (sd *SchemaDiffer) compareTableDefinitions(ctx context.Context, from, to *TableDefinition) ([]SchemaChange, error) {
	var changes []SchemaChange

	// Compare columns
	columnChanges, err := sd.compareColumns(ctx, from, to)
	if err != nil {
		return nil, err
	}
	changes = append(changes, columnChanges...)

	// Compare constraints
	constraintChanges, err := sd.compareTableConstraints(ctx, from, to)
	if err != nil {
		return nil, err
	}
	changes = append(changes, constraintChanges...)

	return changes, nil
}

// compareColumns compares table columns
func (sd *SchemaDiffer) compareColumns(ctx context.Context, from, to *TableDefinition) ([]SchemaChange, error) {
	var changes []SchemaChange

	// Find dropped columns
	for colName, column := range from.Columns {
		if _, exists := to.Columns[colName]; !exists {
			changes = append(changes, &ColumnDropChange{
				ID:         from.ID,
				ColumnName: colName,
				Definition: column,
			})
		}
	}

	// Find added columns
	for colName, column := range to.Columns {
		if _, exists := from.Columns[colName]; !exists {
			changes = append(changes, &ColumnAddChange{
				ID:         to.ID,
				ColumnName: colName,
				Definition: column,
			})
		}
	}

	// Compare modified columns
	for colName, fromCol := range from.Columns {
		if toCol, exists := to.Columns[colName]; exists {
			if !sd.columnsEqual(fromCol, toCol) {
				changes = append(changes, &ColumnModifyChange{
					ID:             from.ID,
					ColumnName:     colName,
					FromDefinition: fromCol,
					ToDefinition:   toCol,
				})
			}
		}
	}

	return changes, nil
}

// columnsEqual compares two column definitions
func (sd *SchemaDiffer) columnsEqual(from, to *Column) bool {
	if from.Name != to.Name ||
		from.DataType != to.DataType ||
		from.IsNullable != to.IsNullable ||
		from.IsGenerated != to.IsGenerated {
		return false
	}

	if !sd.config.IgnoreComments && from.Comment != to.Comment {
		return false
	}

	// Compare default values
	if sd.config.CompareExpressions && sd.validator != nil {
		if equal, err := sd.validator.ExpressionsEqual(from.DefaultValue, to.DefaultValue); err == nil && !equal {
			return false
		}
	} else if from.DefaultValue != to.DefaultValue {
		return false
	}

	return true
}

// compareIndexes compares index definitions
func (sd *SchemaDiffer) compareIndexes(ctx context.Context) ([]SchemaChange, error) {
	var changes []SchemaChange

	// Find dropped indexes
	for id, index := range sd.fromSchema.Indexes {
		if _, exists := sd.toSchema.Indexes[id]; !exists {
			changes = append(changes, &IndexDropChange{
				ID:         id,
				Definition: index,
			})
		}
	}

	// Find added indexes
	for id, index := range sd.toSchema.Indexes {
		if _, exists := sd.fromSchema.Indexes[id]; !exists {
			changes = append(changes, &IndexCreateChange{
				ID:         id,
				Definition: index,
			})
		}
	}

	// Compare modified indexes
	for id, fromIndex := range sd.fromSchema.Indexes {
		if toIndex, exists := sd.toSchema.Indexes[id]; exists {
			if !sd.indexesEqual(fromIndex, toIndex) {
				changes = append(changes, &IndexModifyChange{
					ID:             id,
					FromDefinition: fromIndex,
					ToDefinition:   toIndex,
				})
			}
		}
	}

	return changes, nil
}

// indexesEqual compares two index definitions
func (sd *SchemaDiffer) indexesEqual(from, to *IndexDefinition) bool {
	if from.Method != to.Method ||
		from.IsUnique != to.IsUnique ||
		from.IsPrimary != to.IsPrimary ||
		!stringSlicesEqual(from.Columns, to.Columns) {
		return false
	}

	if sd.config.CompareExpressions && sd.validator != nil {
		if from.Expression != "" || to.Expression != "" {
			if equal, err := sd.validator.ExpressionsEqual(from.Expression, to.Expression); err == nil && !equal {
				return false
			}
		}
		if from.Where != "" || to.Where != "" {
			if equal, err := sd.validator.ExpressionsEqual(from.Where, to.Where); err == nil && !equal {
				return false
			}
		}
	} else {
		if from.Expression != to.Expression || from.Where != to.Where {
			return false
		}
	}

	return true
}

// compareFunctions compares function definitions
func (sd *SchemaDiffer) compareFunctions(ctx context.Context) ([]SchemaChange, error) {
	var changes []SchemaChange

	// Find dropped functions
	for id, function := range sd.fromSchema.Functions {
		if _, exists := sd.toSchema.Functions[id]; !exists {
			changes = append(changes, &FunctionDropChange{
				ID:         id,
				Definition: function,
			})
		}
	}

	// Find added functions
	for id, function := range sd.toSchema.Functions {
		if _, exists := sd.fromSchema.Functions[id]; !exists {
			changes = append(changes, &FunctionCreateChange{
				ID:         id,
				Definition: function,
			})
		}
	}

	// Compare modified functions
	for id, fromFunc := range sd.fromSchema.Functions {
		if toFunc, exists := sd.toSchema.Functions[id]; exists {
			if !sd.functionsEqual(fromFunc, toFunc) {
				changes = append(changes, &FunctionModifyChange{
					ID:             id,
					FromDefinition: fromFunc,
					ToDefinition:   toFunc,
				})
			}
		}
	}

	return changes, nil
}

// functionsEqual compares two function definitions
func (sd *SchemaDiffer) functionsEqual(from, to *FunctionDefinition) bool {
	if from.ReturnType != to.ReturnType ||
		from.Language != to.Language ||
		from.Volatility != to.Volatility ||
		from.Security != to.Security ||
		len(from.Parameters) != len(to.Parameters) {
		return false
	}

	// Compare parameters
	for i, fromParam := range from.Parameters {
		toParam := to.Parameters[i]
		if fromParam.Name != toParam.Name ||
			fromParam.DataType != toParam.DataType ||
			fromParam.Mode != toParam.Mode ||
			fromParam.Default != toParam.Default {
			return false
		}
	}

	// Compare function bodies
	if sd.config.IgnoreWhitespace {
		fromBody := strings.ReplaceAll(strings.TrimSpace(from.Body), "\n", " ")
		toBody := strings.ReplaceAll(strings.TrimSpace(to.Body), "\n", " ")
		return fromBody == toBody
	}

	return from.Body == to.Body
}

// compareTableConstraints compares table constraints
func (sd *SchemaDiffer) compareTableConstraints(ctx context.Context, from, to *TableDefinition) ([]SchemaChange, error) {
	var changes []SchemaChange

	// Find dropped constraints
	for name, constraint := range from.Constraints {
		if _, exists := to.Constraints[name]; !exists {
			changes = append(changes, &ConstraintDropChange{
				ID:             from.ID,
				ConstraintName: name,
				Definition:     constraint,
			})
		}
	}

	// Find added constraints
	for name, constraint := range to.Constraints {
		if _, exists := from.Constraints[name]; !exists {
			changes = append(changes, &ConstraintAddChange{
				ID:             to.ID,
				ConstraintName: name,
				Definition:     constraint,
			})
		}
	}

	// Compare modified constraints
	for name, fromConstraint := range from.Constraints {
		if toConstraint, exists := to.Constraints[name]; exists {
			if !sd.constraintsEqual(fromConstraint, toConstraint) {
				changes = append(changes, &ConstraintModifyChange{
					ID:             from.ID,
					ConstraintName: name,
					FromDefinition: fromConstraint,
					ToDefinition:   toConstraint,
				})
			}
		}
	}

	return changes, nil
}

// constraintsEqual compares two constraint definitions
func (sd *SchemaDiffer) constraintsEqual(from, to *Constraint) bool {
	if from.Type != to.Type ||
		!stringSlicesEqual(from.Columns, to.Columns) ||
		from.RefTable != to.RefTable ||
		!stringSlicesEqual(from.RefColumns, to.RefColumns) ||
		from.OnUpdate != to.OnUpdate ||
		from.OnDelete != to.OnDelete {
		return false
	}

	// Compare expressions
	if sd.config.CompareExpressions && sd.validator != nil {
		if equal, err := sd.validator.ExpressionsEqual(from.Expression, to.Expression); err == nil && !equal {
			return false
		}
	} else if from.Expression != to.Expression {
		return false
	}

	return true
}

// Helper functions

// stringSlicesEqual compares two string slices for equality
func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, v := range a {
		if v != b[i] {
			return false
		}
	}
	return true
}

// getRiskPriority returns a numeric priority for risk levels
func getRiskPriority(risk RiskLevel) int {
	switch risk {
	case RiskLevelCritical:
		return 5
	case RiskLevelHigh:
		return 4
	case RiskLevelMedium:
		return 3
	case RiskLevelLow:
		return 2
	case RiskLevelSafe:
		return 1
	default:
		return 0
	}
}

// ExpressionValidator validates and compares SQL expressions using database connection
type ExpressionValidator struct {
	db *sql.DB
}

// NewExpressionValidator creates a new expression validator
func NewExpressionValidator(db *sql.DB) *ExpressionValidator {
	return &ExpressionValidator{db: db}
}

// ExpressionsEqual compares two SQL expressions for logical equivalence
func (ev *ExpressionValidator) ExpressionsEqual(expr1, expr2 string) (bool, error) {
	if ev.db == nil {
		// Fallback to string comparison if no database connection
		return strings.TrimSpace(expr1) == strings.TrimSpace(expr2), nil
	}

	// Handle empty expressions
	if strings.TrimSpace(expr1) == "" && strings.TrimSpace(expr2) == "" {
		return true, nil
	}
	if strings.TrimSpace(expr1) == "" || strings.TrimSpace(expr2) == "" {
		return false, nil
	}

	// Try to compare expressions using database
	query := fmt.Sprintf("SELECT (%s) = (%s)", expr1, expr2)
	var result sql.NullBool
	err := ev.db.QueryRow(query).Scan(&result)

	if err != nil {
		// If database comparison fails, fall back to string comparison
		return strings.TrimSpace(expr1) == strings.TrimSpace(expr2), nil
	}

	return result.Valid && result.Bool, nil
}

// ValidateExpression checks if an expression is valid SQL
func (ev *ExpressionValidator) ValidateExpression(expr string) error {
	if ev.db == nil || strings.TrimSpace(expr) == "" {
		return nil
	}

	query := fmt.Sprintf("SELECT (%s) FROM (SELECT 1) AS dummy WHERE false", expr)
	_, err := ev.db.Query(query)
	return err
}
