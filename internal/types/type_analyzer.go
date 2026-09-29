// Package types provides PostgreSQL type system analysis and management.
// It handles type compatibility checking, custom type analysis, and
// database type introspection for migration squashing operations.
package types

import (
	"context"
	"database/sql"
	stderrors "errors"
	"fmt"
	"strings"

	"github.com/capydatabase/capysquash/internal/errors"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// TypeAnalyzer analyzes PostgreSQL types in SQL statements and migrations
type TypeAnalyzer struct {
	typeSystem *PostgreSQLTypeSystem
	db         *sql.DB
	cache      map[string]*TypeInfo
}

// TypeInfo contains comprehensive information about a database type
type TypeInfo struct {
	Name         string           `json:"name"`
	Schema       string           `json:"schema"`
	Category     TypeCategory     `json:"category"`
	IsBuiltin    bool             `json:"is_builtin"`
	BaseType     string           `json:"base_type,omitempty"`
	Size         int              `json:"size"`
	Precision    int              `json:"precision,omitempty"`
	Scale        int              `json:"scale,omitempty"`
	Length       int              `json:"length,omitempty"`
	IsArray      bool             `json:"is_array"`
	ArrayDims    int              `json:"array_dims,omitempty"`
	ElementType  string           `json:"element_type,omitempty"`
	Modifiers    []string         `json:"modifiers"`
	Constraints  []string         `json:"constraints"`
	Dependencies []TypeDependency `json:"dependencies"`
	UsageContext []UsageContext   `json:"usage_context"`
}

// TypeDependency represents a dependency relationship between types
type TypeDependency struct {
	DependentType string         `json:"dependent_type"`
	DependsOnType string         `json:"depends_on_type"`
	Relationship  DependencyType `json:"relationship"`
	Optional      bool           `json:"optional"`
}

// DependencyType defines the type of dependency relationship
type DependencyType int

const (
	CompositionDependency DependencyType = iota
	InheritanceDependency
	ArrayElementDependency
	DomainBaseDependency
	FunctionParameterDependency
	TableColumnDependency
)

// UsageContext tracks where and how a type is used
type UsageContext struct {
	Location    string      `json:"location"`
	Context     ContextType `json:"context"`
	Required    bool        `json:"required"`
	Constraints []string    `json:"constraints"`
}

// ContextType defines where a type is used
type ContextType int

const (
	TableColumnContext ContextType = iota
	FunctionParameterContext
	FunctionReturnContext
	DomainBaseContext
	CompositeAttributeContext
	ArrayElementContext
	IndexExpressionContext
)

// TypeConversion represents a type conversion operation
type TypeConversion struct {
	FromType        string   `json:"from_type"`
	ToType          string   `json:"to_type"`
	ConversionSQL   string   `json:"conversion_sql"`
	Reversible      bool     `json:"reversible"`
	ReverseSQL      string   `json:"reverse_sql,omitempty"`
	DataLoss        bool     `json:"data_loss"`
	LossDescription string   `json:"loss_description,omitempty"`
	Warnings        []string `json:"warnings"`
}

// NewTypeAnalyzer creates a new type analyzer
func NewTypeAnalyzer(typeSystem *PostgreSQLTypeSystem, db *sql.DB) *TypeAnalyzer {
	return &TypeAnalyzer{
		typeSystem: typeSystem,
		db:         db,
		cache:      make(map[string]*TypeInfo),
	}
}

// AnalyzeStatement analyzes types used in a SQL statement
func (ta *TypeAnalyzer) AnalyzeStatement(ctx context.Context, sql string) ([]*TypeInfo, error) {
	// Parse SQL to get AST
	parseResult, err := pg_query.Parse(sql)
	if err != nil {
		return nil, errors.NewTypeError(errors.ErrorCodeAnalysisError, "failed to parse SQL", "").WithInnerError(err)
	}

	types := make(map[string]*TypeInfo)

	// Extract types from AST
	for _, stmt := range parseResult.Stmts {
		err := ta.extractTypesFromNode(ctx, stmt.Stmt, types)
		if err != nil {
			return nil, errors.NewTypeError(errors.ErrorCodeAnalysisError, "failed to extract types", "").WithInnerError(err)
		}
	}

	// Convert map to slice
	result := make([]*TypeInfo, 0, len(types))
	for _, typeInfo := range types {
		result = append(result, typeInfo)
	}

	return result, nil
}

// extractTypesFromNode recursively extracts type information from AST nodes
func (ta *TypeAnalyzer) extractTypesFromNode(ctx context.Context, node *pg_query.Node, types map[string]*TypeInfo) error {
	if node == nil {
		return nil
	}

	switch n := node.Node.(type) {
	case *pg_query.Node_CreateStmt:
		return ta.extractTypesFromCreateTable(ctx, n.CreateStmt, types)

	case *pg_query.Node_AlterTableStmt:
		return ta.extractTypesFromAlterTable(ctx, n.AlterTableStmt, types)

	case *pg_query.Node_CreateDomainStmt:
		return ta.extractTypesFromCreateDomain(ctx, n.CreateDomainStmt, types)

	case *pg_query.Node_CreateEnumStmt:
		return ta.extractTypesFromCreateEnum(ctx, n.CreateEnumStmt, types)

	case *pg_query.Node_CompositeTypeStmt:
		return ta.extractTypesFromCreateComposite(ctx, n.CompositeTypeStmt, types)

	case *pg_query.Node_CreateFunctionStmt:
		return ta.extractTypesFromCreateFunction(ctx, n.CreateFunctionStmt, types)
	}

	return nil
}

// extractTypesFromCreateTable extracts types from CREATE TABLE statement
func (ta *TypeAnalyzer) extractTypesFromCreateTable(ctx context.Context, stmt *pg_query.CreateStmt, types map[string]*TypeInfo) error {
	if stmt.Relation == nil {
		return nil
	}

	tableName := stmt.Relation.Relname

	// Process each column
	for _, element := range stmt.TableElts {
		if colDef := element.GetColumnDef(); colDef != nil {
			typeInfo, err := ta.analyzeColumnType(ctx, colDef)
			if err != nil {
				continue // Skip invalid types
			}

			// Add usage context
			typeInfo.UsageContext = append(typeInfo.UsageContext, UsageContext{
				Location: fmt.Sprintf("table %s, column %s", tableName, colDef.Colname),
				Context:  TableColumnContext,
				Required: true,
			})

			types[typeInfo.Name] = typeInfo
		}
	}

	return nil
}

// extractTypesFromAlterTable extracts types from ALTER TABLE statement
func (ta *TypeAnalyzer) extractTypesFromAlterTable(ctx context.Context, stmt *pg_query.AlterTableStmt, types map[string]*TypeInfo) error {
	if stmt.Relation == nil {
		return nil
	}

	tableName := stmt.Relation.Relname

	for _, cmd := range stmt.Cmds {
		if alterCmd := cmd.GetAlterTableCmd(); alterCmd != nil {
			switch alterCmd.Subtype {
			case pg_query.AlterTableType_AT_AddColumn:
				if colDef := alterCmd.Def.GetColumnDef(); colDef != nil {
					typeInfo, err := ta.analyzeColumnType(ctx, colDef)
					if err != nil {
						continue
					}

					typeInfo.UsageContext = append(typeInfo.UsageContext, UsageContext{
						Location: fmt.Sprintf("table %s, column %s (added)", tableName, colDef.Colname),
						Context:  TableColumnContext,
						Required: true,
					})

					types[typeInfo.Name] = typeInfo
				}

			case pg_query.AlterTableType_AT_AlterColumnType:
				// Extract column name
				columnName := alterCmd.Name
				if columnName == "" {
					continue
				}

				// Extract new type from Def node
				if colDef := alterCmd.Def.GetColumnDef(); colDef != nil && colDef.TypeName != nil {
					newTypeName := ta.extractTypeNameFromNode(colDef.TypeName)

					typeInfo := ta.getOrCreateTypeInfo(newTypeName)
					if typeInfo != nil {
						typeInfo.UsageContext = append(typeInfo.UsageContext, UsageContext{
							Location: fmt.Sprintf("table %s, column %s (type changed)", tableName, columnName),
							Context:  TableColumnContext,
							Required: true,
						})
						types[typeInfo.Name] = typeInfo
					}
				}
			}
		}
	}

	return nil
}

// extractTypesFromCreateDomain extracts types from CREATE DOMAIN statement
func (ta *TypeAnalyzer) extractTypesFromCreateDomain(ctx context.Context, stmt *pg_query.CreateDomainStmt, types map[string]*TypeInfo) error {
	if len(stmt.Domainname) == 0 || stmt.TypeName == nil {
		return nil
	}

	domainName := stmt.Domainname[len(stmt.Domainname)-1].GetString_().Sval
	baseTypeName := ta.extractTypeNameFromNode(stmt.TypeName)

	typeInfo := &TypeInfo{
		Name:      domainName,
		Category:  DomainType,
		IsBuiltin: false,
		BaseType:  baseTypeName,
		Dependencies: []TypeDependency{
			{
				DependentType: domainName,
				DependsOnType: baseTypeName,
				Relationship:  DomainBaseDependency,
				Optional:      false,
			},
		},
		UsageContext: []UsageContext{
			{
				Location: fmt.Sprintf("domain %s", domainName),
				Context:  DomainBaseContext,
				Required: true,
			},
		},
	}

	types[domainName] = typeInfo
	return nil
}

// extractTypesFromCreateEnum extracts types from CREATE TYPE ... AS ENUM statement
func (ta *TypeAnalyzer) extractTypesFromCreateEnum(ctx context.Context, stmt *pg_query.CreateEnumStmt, types map[string]*TypeInfo) error {
	if len(stmt.TypeName) == 0 {
		return nil
	}

	enumName := stmt.TypeName[len(stmt.TypeName)-1].GetString_().Sval

	values := make([]string, len(stmt.Vals))
	for i, val := range stmt.Vals {
		if strVal := val.GetString_(); strVal != nil {
			values[i] = strVal.Sval
		}
	}

	typeInfo := &TypeInfo{
		Name:      enumName,
		Category:  EnumTypeCategory,
		IsBuiltin: false,
		Modifiers: values,
		UsageContext: []UsageContext{
			{
				Location: fmt.Sprintf("enum %s", enumName),
				Context:  TableColumnContext,
				Required: true,
			},
		},
	}

	types[enumName] = typeInfo
	return nil
}

// extractTypesFromCreateComposite extracts types from CREATE TYPE ... AS (...) statement
func (ta *TypeAnalyzer) extractTypesFromCreateComposite(ctx context.Context, stmt *pg_query.CompositeTypeStmt, types map[string]*TypeInfo) error {
	if stmt.Typevar == nil || stmt.Typevar.Relname == "" {
		return nil
	}

	compositeName := stmt.Typevar.Relname

	typeInfo := &TypeInfo{
		Name:         compositeName,
		Category:     CompositeTypeCategory,
		IsBuiltin:    false,
		Dependencies: make([]TypeDependency, 0),
		UsageContext: []UsageContext{
			{
				Location: fmt.Sprintf("composite type %s", compositeName),
				Context:  CompositeAttributeContext,
				Required: true,
			},
		},
	}

	// Process attributes
	for _, col := range stmt.Coldeflist {
		if colDef := col.GetColumnDef(); colDef != nil {
			attributeTypeName := ta.extractTypeNameFromColumnDef(colDef)

			// Add dependency on attribute type
			typeInfo.Dependencies = append(typeInfo.Dependencies, TypeDependency{
				DependentType: compositeName,
				DependsOnType: attributeTypeName,
				Relationship:  CompositionDependency,
				Optional:      false,
			})
		}
	}

	types[compositeName] = typeInfo
	return nil
}

// extractTypesFromCreateFunction extracts types from CREATE FUNCTION statement
func (ta *TypeAnalyzer) extractTypesFromCreateFunction(ctx context.Context, stmt *pg_query.CreateFunctionStmt, types map[string]*TypeInfo) error {
	if len(stmt.Funcname) == 0 {
		return nil
	}

	functionName := stmt.Funcname[len(stmt.Funcname)-1].GetString_().Sval

	// Process parameter types
	for _, param := range stmt.Parameters {
		if funcParam := param.GetFunctionParameter(); funcParam != nil && funcParam.ArgType != nil {
			paramTypeName := ta.extractTypeNameFromNode(funcParam.ArgType)

			if typeInfo := ta.getOrCreateTypeInfo(paramTypeName); typeInfo != nil {
				typeInfo.UsageContext = append(typeInfo.UsageContext, UsageContext{
					Location: fmt.Sprintf("function %s parameter", functionName),
					Context:  FunctionParameterContext,
					Required: true,
				})
				types[paramTypeName] = typeInfo
			}
		}
	}

	// Process return type
	if stmt.ReturnType != nil {
		returnTypeName := ta.extractTypeNameFromNode(stmt.ReturnType)

		if typeInfo := ta.getOrCreateTypeInfo(returnTypeName); typeInfo != nil {
			typeInfo.UsageContext = append(typeInfo.UsageContext, UsageContext{
				Location: fmt.Sprintf("function %s return type", functionName),
				Context:  FunctionReturnContext,
				Required: true,
			})
			types[returnTypeName] = typeInfo
		}
	}

	return nil
}

// analyzeColumnType analyzes a column definition to extract type information
func (ta *TypeAnalyzer) analyzeColumnType(ctx context.Context, colDef *pg_query.ColumnDef) (*TypeInfo, error) {
	if colDef.TypeName == nil {
		return nil, errors.NewTypeError(errors.ErrorCodeInvalidType, "column has no type", "")
	}

	typeName := ta.extractTypeNameFromColumnDef(colDef)

	typeInfo := ta.getOrCreateTypeInfo(typeName)
	if typeInfo == nil {
		return nil, errors.NewTypeError(errors.ErrorCodeInvalidType, "failed to create type info", typeName)
	}

	// Extract constraints
	for _, constraint := range colDef.Constraints {
		if constr := constraint.GetConstraint(); constr != nil {
			switch constr.Contype {
			case pg_query.ConstrType_CONSTR_NOTNULL:
				typeInfo.Constraints = append(typeInfo.Constraints, "NOT NULL")
			case pg_query.ConstrType_CONSTR_UNIQUE:
				typeInfo.Constraints = append(typeInfo.Constraints, "UNIQUE")
			case pg_query.ConstrType_CONSTR_PRIMARY:
				typeInfo.Constraints = append(typeInfo.Constraints, "PRIMARY KEY")
			case pg_query.ConstrType_CONSTR_CHECK:
				typeInfo.Constraints = append(typeInfo.Constraints, "CHECK")
			}
		}
	}

	return typeInfo, nil
}

// extractTypeNameFromColumnDef extracts type name from column definition
func (ta *TypeAnalyzer) extractTypeNameFromColumnDef(colDef *pg_query.ColumnDef) string {
	return ta.extractTypeNameFromNode(colDef.TypeName)
}

// extractTypeNameFromNode extracts type name from a TypeName node
func (ta *TypeAnalyzer) extractTypeNameFromNode(typeNameNode *pg_query.TypeName) string {
	if typeNameNode == nil || len(typeNameNode.Names) == 0 {
		return ""
	}

	// Build qualified type name
	parts := make([]string, 0, len(typeNameNode.Names))
	for _, name := range typeNameNode.Names {
		if strVal := name.GetString_(); strVal != nil {
			parts = append(parts, strVal.Sval)
		}
	}

	var typeName strings.Builder
	typeName.WriteString(strings.Join(parts, "."))

	// Handle array types
	if len(typeNameNode.ArrayBounds) > 0 {
		for range typeNameNode.ArrayBounds {
			typeName.WriteString("[]")
		}
	}

	// Handle type modifiers (precision, scale, length)
	if len(typeNameNode.Typmods) > 0 {
		modParts := make([]string, 0, len(typeNameNode.Typmods))
		for _, mod := range typeNameNode.Typmods {
			if aConst := mod.GetAConst(); aConst != nil {
				if ival := aConst.GetIval(); ival != nil {
					modParts = append(modParts, fmt.Sprintf("%d", ival.Ival))
				}
			}
		}
		if len(modParts) > 0 {
			typeName.WriteString("(" + strings.Join(modParts, ",") + ")")
		}
	}

	return typeName.String()
}

// getOrCreateTypeInfo gets existing type info or creates new one
func (ta *TypeAnalyzer) getOrCreateTypeInfo(typeName string) *TypeInfo {
	// Check cache first
	if cached, exists := ta.cache[typeName]; exists {
		return cached
	}

	// Parse array type
	arrayType, err := ta.typeSystem.ParseArrayType(typeName)
	isArray := err == nil

	typeInfo := &TypeInfo{
		Name:         typeName,
		IsBuiltin:    ta.typeSystem.IsBuiltinType(typeName),
		IsArray:      isArray,
		Dependencies: make([]TypeDependency, 0),
		UsageContext: make([]UsageContext, 0),
		Modifiers:    make([]string, 0),
		Constraints:  make([]string, 0),
	}

	if isArray {
		typeInfo.ArrayDims = arrayType.Dimensions
		typeInfo.ElementType = arrayType.ElementType
		typeInfo.Category = ArrayTypeCategory

		// Add dependency on element type
		typeInfo.Dependencies = append(typeInfo.Dependencies, TypeDependency{
			DependentType: typeName,
			DependsOnType: arrayType.ElementType,
			Relationship:  ArrayElementDependency,
			Optional:      false,
		})
	} else if typeInfo.IsBuiltin {
		typeInfo.Category = BaseType
	}

	// Get size information
	if size, err := ta.typeSystem.GetTypeSize(typeName); err == nil {
		typeInfo.Size = size
	}

	// Cache the result
	ta.cache[typeName] = typeInfo

	return typeInfo
}

// GenerateTypeConversion generates SQL for type conversion
func (ta *TypeAnalyzer) GenerateTypeConversion(ctx context.Context, fromType, toType string, columnName string) (*TypeConversion, error) {
	compatibility := ta.typeSystem.CheckTypeCompatibility(fromType, toType)

	conversion := &TypeConversion{
		FromType: fromType,
		ToType:   toType,
		Warnings: compatibility.Warnings,
		DataLoss: ta.typeSystem.isPotentiallyLossyConversion(fromType, toType),
	}

	if !compatibility.Compatible {
		return nil, errors.NewTypeError(errors.ErrorCodeIncompatibleTypes, "types are not compatible", fromType).WithAdditional("target_type", toType)
	}

	switch compatibility.CastType {
	case NoCast, ImplicitCast:
		// No explicit cast needed
		conversion.ConversionSQL = fmt.Sprintf("ALTER TABLE {table} ALTER COLUMN %s TYPE %s", columnName, toType)
		conversion.Reversible = !conversion.DataLoss

	case AssignmentCast:
		// Assignment cast (usually safe)
		conversion.ConversionSQL = fmt.Sprintf("ALTER TABLE {table} ALTER COLUMN %s TYPE %s", columnName, toType)
		conversion.Reversible = !conversion.DataLoss

	case ExplicitCast:
		// Explicit cast required
		conversion.ConversionSQL = fmt.Sprintf("ALTER TABLE {table} ALTER COLUMN %s TYPE %s USING %s::%s",
			columnName, toType, columnName, toType)
		conversion.Reversible = false // Explicit casts are generally not reversible
	}

	// Generate reverse SQL if reversible
	if conversion.Reversible {
		reverseCompatibility := ta.typeSystem.CheckTypeCompatibility(toType, fromType)
		if reverseCompatibility.Compatible {
			switch reverseCompatibility.CastType {
			case NoCast, ImplicitCast, AssignmentCast:
				conversion.ReverseSQL = fmt.Sprintf("ALTER TABLE {table} ALTER COLUMN %s TYPE %s", columnName, fromType)
			case ExplicitCast:
				conversion.ReverseSQL = fmt.Sprintf("ALTER TABLE {table} ALTER COLUMN %s TYPE %s USING %s::%s",
					columnName, fromType, columnName, fromType)
				conversion.Reversible = false // If reverse requires explicit cast, mark as not reversible
			}
		} else {
			conversion.Reversible = false
		}
	}

	// Add data loss description
	if conversion.DataLoss {
		conversion.LossDescription = ta.generateDataLossDescription(fromType, toType)
	}

	return conversion, nil
}

// generateDataLossDescription generates a description of potential data loss
func (ta *TypeAnalyzer) generateDataLossDescription(fromType, toType string) string {
	fromNorm := ta.typeSystem.normalizeTypeName(fromType)
	toNorm := ta.typeSystem.normalizeTypeName(toType)

	descriptions := map[string]map[string]string{
		"bigint": {
			"integer":  "Values outside the range -2147483648 to 2147483647 will cause an error",
			"smallint": "Values outside the range -32768 to 32767 will cause an error",
		},
		"integer": {
			"smallint": "Values outside the range -32768 to 32767 will cause an error",
		},
		"double precision": {
			"real": "Precision may be lost due to reduced floating-point precision",
		},
		"numeric": {
			"integer": "Fractional part will be truncated",
			"bigint":  "Fractional part will be truncated",
		},
		"text": {
			"character varying": "Text may be truncated if longer than the specified length",
			"character":         "Text may be truncated or padded to the specified length",
		},
		"timestamp with time zone": {
			"timestamp without time zone": "Time zone information will be lost",
		},
	}

	if fromTypes, exists := descriptions[fromNorm]; exists {
		if description, exists := fromTypes[toNorm]; exists {
			return description
		}
	}

	return fmt.Sprintf("Converting from %s to %s may result in data loss", fromType, toType)
}

// AnalyzeMigrationTypes analyzes all types used in a migration
func (ta *TypeAnalyzer) AnalyzeMigrationTypes(ctx context.Context, statements []Statement) (*MigrationTypeAnalysis, error) {
	analysis := &MigrationTypeAnalysis{
		TypesUsed:    make(map[string]*TypeInfo),
		TypeChanges:  make([]*TypeChange, 0),
		Dependencies: make([]*TypeDependency, 0),
		Warnings:     make([]string, 0),
	}

	// Column types as the statements leave them, so an ALTER COLUMN TYPE
	// knows the type it changes from.
	columns := make(map[string]string)

	for _, stmt := range statements {
		stmtTypes, err := ta.AnalyzeStatement(ctx, stmt.SQL)
		if err != nil {
			analysis.Warnings = append(analysis.Warnings,
				fmt.Sprintf("Failed to analyze statement: %v", err))
			continue
		}

		// Add types to analysis
		for _, typeInfo := range stmtTypes {
			analysis.TypesUsed[typeInfo.Name] = typeInfo

			// Collect dependencies
			for _, dep := range typeInfo.Dependencies {
				analysis.Dependencies = append(analysis.Dependencies, &dep)
			}
		}

		changes, warnings := ta.trackColumnTypes(ctx, stmt, columns)
		analysis.TypeChanges = append(analysis.TypeChanges, changes...)
		analysis.Warnings = append(analysis.Warnings, warnings...)
	}

	return analysis, nil
}

// MigrationTypeAnalysis represents the result of analyzing types in a migration
type MigrationTypeAnalysis struct {
	TypesUsed    map[string]*TypeInfo `json:"types_used"`
	TypeChanges  []*TypeChange        `json:"type_changes"`
	Dependencies []*TypeDependency    `json:"dependencies"`
	Warnings     []string             `json:"warnings"`
}

// TypeChange represents a type change in a migration
type TypeChange struct {
	Table      string `json:"table"`
	Column     string `json:"column"`
	FromType   string `json:"from_type"`
	ToType     string `json:"to_type"`
	Reversible bool   `json:"reversible"`
	DataLoss   bool   `json:"data_loss"`
}

// trackColumnTypes applies stmt to columns (schema.table.column -> type) and
// returns the ALTER COLUMN ... TYPE changes it makes. The type a column
// changes from comes from the earlier statements; for a column the migrations
// never defined it is read from ta.db when there is one. Only when neither
// knows it is FromType "unknown" (and the change is then treated as
// irreversible, with a warning).
func (ta *TypeAnalyzer) trackColumnTypes(ctx context.Context, stmt Statement, columns map[string]string) ([]*TypeChange, []string) {
	var changes []*TypeChange
	var warnings []string
	if stmt.ParseTree == nil {
		return nil, nil
	}

	for _, raw := range stmt.ParseTree.GetStmts() {
		switch n := raw.GetStmt().GetNode().(type) {
		case *pg_query.Node_CreateStmt:
			table := qualifiedRelation(n.CreateStmt.GetRelation())
			for _, elt := range n.CreateStmt.GetTableElts() {
				if col := elt.GetColumnDef(); col != nil && col.GetTypeName() != nil {
					columns[table+"."+col.GetColname()] = ta.canonicalTypeName(col.GetTypeName())
				}
			}

		case *pg_query.Node_DropStmt:
			if n.DropStmt.GetRemoveType() != pg_query.ObjectType_OBJECT_TABLE {
				continue
			}
			for _, obj := range n.DropStmt.GetObjects() {
				table := qualifiedNameList(obj.GetList().GetItems())
				for key := range columns {
					if strings.HasPrefix(key, table+".") {
						delete(columns, key)
					}
				}
			}

		case *pg_query.Node_RenameStmt:
			rename := n.RenameStmt
			if rename.GetRenameType() != pg_query.ObjectType_OBJECT_COLUMN || rename.GetRelation() == nil {
				continue
			}
			table := qualifiedRelation(rename.GetRelation())
			if typ, ok := columns[table+"."+rename.GetSubname()]; ok {
				delete(columns, table+"."+rename.GetSubname())
				columns[table+"."+rename.GetNewname()] = typ
			}

		case *pg_query.Node_AlterTableStmt:
			table := qualifiedRelation(n.AlterTableStmt.GetRelation())
			for _, cmd := range n.AlterTableStmt.GetCmds() {
				alterCmd := cmd.GetAlterTableCmd()
				if alterCmd == nil {
					continue
				}
				switch alterCmd.GetSubtype() {
				case pg_query.AlterTableType_AT_AddColumn:
					if col := alterCmd.GetDef().GetColumnDef(); col != nil && col.GetTypeName() != nil {
						columns[table+"."+col.GetColname()] = ta.canonicalTypeName(col.GetTypeName())
					}
				case pg_query.AlterTableType_AT_DropColumn:
					delete(columns, table+"."+alterCmd.GetName())
				case pg_query.AlterTableType_AT_AlterColumnType:
					col := alterCmd.GetDef().GetColumnDef()
					if col == nil || col.GetTypeName() == nil || alterCmd.GetName() == "" {
						continue
					}
					key := table + "." + alterCmd.GetName()
					toType := ta.canonicalTypeName(col.GetTypeName())

					fromType, known := columns[key]
					if !known {
						var err error
						fromType, known, err = ta.currentColumnType(ctx, n.AlterTableStmt.GetRelation(), alterCmd.GetName())
						if err != nil {
							warnings = append(warnings, fmt.Sprintf("Could not read the current type of %s: %v", key, err))
						}
					}

					change := &TypeChange{Table: table, Column: alterCmd.GetName(), FromType: "unknown", ToType: toType}
					if known {
						change.FromType = fromType
						change.DataLoss = ta.conversionLosesData(fromType, toType)
						// Reversible: converting back cannot lose what this change kept.
						change.Reversible = !change.DataLoss && !ta.conversionLosesData(toType, fromType) &&
							ta.typeSystem.CheckTypeCompatibility(toType, fromType).Compatible
					} else {
						warnings = append(warnings, fmt.Sprintf("Type change of %s to %s: the previous type is unknown (not defined by these migrations), so data loss and reversibility cannot be assessed", key, toType))
					}
					changes = append(changes, change)
					columns[key] = toType
				}
			}
		}
	}

	return changes, warnings
}

// conversionLosesData reports whether converting from -> to can lose or
// reject existing values: a narrowing conversion, or a shorter length/precision
// of the same base type (varchar(50) -> varchar(20)).
func (ta *TypeAnalyzer) conversionLosesData(from, to string) bool {
	// normalizeTypeName strips a size first and resolves an alias second, so
	// "varchar(20)" needs both passes to become "character varying".
	fromNorm := ta.typeSystem.normalizeTypeName(ta.typeSystem.normalizeTypeName(from))
	toNorm := ta.typeSystem.normalizeTypeName(ta.typeSystem.normalizeTypeName(to))
	if ta.typeSystem.isPotentiallyLossyConversion(fromNorm, toNorm) {
		return true
	}
	if fromNorm != toNorm {
		return false
	}
	fromSize, fromSized := extractFirstSizeFromTypeSpec(from)
	toSize, toSized := extractFirstSizeFromTypeSpec(to)
	// No size means unbounded: bounding it, or shrinking a bound, can reject rows.
	return (toSized && !fromSized) || (fromSized && toSized && toSize < fromSize)
}

// currentColumnType reads a column's type from ta.db (format_type, the same
// spelling PostgreSQL prints). known is false when there is no database or
// the column does not exist there.
func (ta *TypeAnalyzer) currentColumnType(ctx context.Context, rel *pg_query.RangeVar, column string) (typ string, known bool, err error) {
	if ta.db == nil || rel == nil {
		return "", false, nil
	}
	schema := rel.GetSchemaname()
	if schema == "" {
		schema = "public"
	}
	err = ta.db.QueryRowContext(ctx, `
		SELECT format_type(a.atttypid, a.atttypmod)
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = $1 AND c.relname = $2 AND a.attname = $3
		  AND a.attnum > 0 AND NOT a.attisdropped`,
		schema, rel.GetRelname(), column).Scan(&typ)
	if stderrors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return typ, true, nil
}

// canonicalTypeName is extractTypeNameFromNode without the pg_catalog
// qualifier the parser adds to built-in types ("int" parses as
// pg_catalog.int4).
func (ta *TypeAnalyzer) canonicalTypeName(tn *pg_query.TypeName) string {
	return strings.TrimPrefix(ta.extractTypeNameFromNode(tn), "pg_catalog.")
}

// qualifiedRelation returns schema.table, defaulting the schema to public.
func qualifiedRelation(rel *pg_query.RangeVar) string {
	schema := rel.GetSchemaname()
	if schema == "" {
		schema = "public"
	}
	return schema + "." + rel.GetRelname()
}

// qualifiedNameList turns a parsed name list ([schema,] table) into
// schema.table, defaulting the schema to public.
func qualifiedNameList(items []*pg_query.Node) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		parts = append(parts, item.GetString_().GetSval())
	}
	if len(parts) == 1 {
		return "public." + parts[0]
	}
	return strings.Join(parts, ".")
}
