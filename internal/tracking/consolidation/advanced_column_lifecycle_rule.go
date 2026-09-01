package consolidation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/parser"
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// AdvancedColumnLifecycleRule handles complex column evolution patterns with edge cases
type AdvancedColumnLifecycleRule struct {
	enableDataTypeEvolution  bool
	enableConstraintTracking bool
	enableColumnRenaming     bool
}

// ColumnLifecycleState represents the complete state of a column through its lifecycle
type ColumnLifecycleState struct {
	Name            string                 `json:"name"`
	OriginalName    string                 `json:"original_name"`
	DataType        string                 `json:"data_type"`
	IsNullable      bool                   `json:"is_nullable"`
	DefaultValue    string                 `json:"default_value"`
	Constraints     []ColumnConstraint     `json:"constraints"`
	Position        int                    `json:"position"`
	Transformations []ColumnTransformation `json:"transformations"`
	Status          ColumnStatus           `json:"status"`
	Dependencies    []string               `json:"dependencies"`
}

// ColumnConstraint represents a constraint on a column
type ColumnConstraint struct {
	Type       ConstraintType `json:"type"`
	Name       string         `json:"name"`
	Definition string         `json:"definition"`
	TableLevel bool           `json:"table_level"`
}

// ColumnTransformation tracks changes to a column
type ColumnTransformation struct {
	Operation  ColumnOperation `json:"operation"`
	OldValue   string          `json:"old_value"`
	NewValue   string          `json:"new_value"`
	AtSequence int             `json:"at_sequence"`
	SQL        string          `json:"sql"`
	HasDataOps bool            `json:"has_data_ops"`
}

// ColumnStatus represents the current status of a column
type ColumnStatus string

const (
	ColumnStatusActive    ColumnStatus = "ACTIVE"
	ColumnStatusDropped   ColumnStatus = "DROPPED"
	ColumnStatusRenamed   ColumnStatus = "RENAMED"
	ColumnStatusTransient ColumnStatus = "TRANSIENT" // Added and dropped in same lifecycle
)

// ColumnOperation represents operations that can be performed on columns
type ColumnOperation string

const (
	ColumnOpAdd            ColumnOperation = "ADD"
	ColumnOpDrop           ColumnOperation = "DROP"
	ColumnOpRename         ColumnOperation = "RENAME"
	ColumnOpChangeType     ColumnOperation = "CHANGE_TYPE"
	ColumnOpSetDefault     ColumnOperation = "SET_DEFAULT"
	ColumnOpDropDefault    ColumnOperation = "DROP_DEFAULT"
	ColumnOpSetNotNull     ColumnOperation = "SET_NOT_NULL"
	ColumnOpDropNotNull    ColumnOperation = "DROP_NOT_NULL"
	ColumnOpAddConstraint  ColumnOperation = "ADD_CONSTRAINT"
	ColumnOpDropConstraint ColumnOperation = "DROP_CONSTRAINT"
)

// ConstraintType represents different types of column constraints
type ConstraintType string

const (
	ConstraintPrimaryKey ConstraintType = "PRIMARY_KEY"
	ConstraintForeignKey ConstraintType = "FOREIGN_KEY"
	ConstraintUnique     ConstraintType = "UNIQUE"
	ConstraintCheck      ConstraintType = "CHECK"
	ConstraintDefault    ConstraintType = "DEFAULT"
	ConstraintNotNull    ConstraintType = "NOT_NULL"
)

// NewAdvancedColumnLifecycleRule creates a new advanced column lifecycle rule
func NewAdvancedColumnLifecycleRule() *AdvancedColumnLifecycleRule {
	return &AdvancedColumnLifecycleRule{
		enableDataTypeEvolution:  true,
		enableConstraintTracking: true,
		enableColumnRenaming:     true,
	}
}

// CanApply determines if this rule can be applied to the lifecycle
func (r *AdvancedColumnLifecycleRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	if lifecycle.Type != types.TypeTable {
		return false
	}

	// Check for any column-related operations
	for _, event := range lifecycle.History {
		if r.isAdvancedColumnOperation(event.Statement.SQL) {
			return true
		}
	}

	return false
}

// Apply applies the advanced column lifecycle rule
func (r *AdvancedColumnLifecycleRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation,
			"rule cannot be applied to lifecycle",
			map[string]any{"rule": "AdvancedColumnLifecycleRule"})
	}

	// Build comprehensive column lifecycle map
	columnStates, err := r.buildColumnLifecycleMap(lifecycle)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation,
			"failed to build column lifecycle map",
			map[string]any{"object": lifecycle.Name})
	}

	// Generate optimized schema with advanced column handling
	finalSQL, optimizations, err := r.generateAdvancedSchema(lifecycle, columnStates)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation,
			"failed to generate advanced schema",
			map[string]any{"object": lifecycle.Name})
	}

	// Collect all column-related statements
	var originalStmts []types.Statement
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate || r.isAdvancedColumnOperation(event.Statement.SQL) {
			originalStmts = append(originalStmts, event.Statement)
		}
	}

	// Extract column evolution information for data operation rewriting
	columnEvolutions := r.extractColumnEvolutions(lifecycle.Name, columnStates)

	// Identify and warn about orphaned indexes (indexes on dropped columns)
	orphanedIndexes := r.identifyOrphanedIndexes(lifecycle, columnStates, engine)
	var warnings []string
	if len(orphanedIndexes) > 0 {
		for _, indexName := range orphanedIndexes {
			warnings = append(warnings, fmt.Sprintf("Index %s references dropped columns and should be removed", indexName))
		}
		optimizations = append(optimizations, fmt.Sprintf("Identified %d orphaned indexes on dropped columns", len(orphanedIndexes)))
	}

	result := &tracking.ConsolidationResult{
		OriginalStatements: originalStmts,
		ConsolidatedSQL:    finalSQL,
		Optimizations:      optimizations,
		RiskLevel:          tracking.RiskLevelMedium, // Complex column operations have medium risk
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(originalStmts) - 1,
			FilesAffected:     len(originalStmts),
			LinesReduced:      r.estimateLinesSaved(originalStmts),
		},
		ColumnEvolutions: columnEvolutions,
		Warnings:         warnings,
	}

	return result, nil
}

// Risk returns the risk level for advanced column lifecycle operations
func (r *AdvancedColumnLifecycleRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelMedium
}

// buildColumnLifecycleMap creates a comprehensive map of column lifecycles
func (r *AdvancedColumnLifecycleRule) buildColumnLifecycleMap(lifecycle *tracking.ObjectLifecycle) (map[string]*ColumnLifecycleState, error) {
	columnStates := make(map[string]*ColumnLifecycleState)

	// Process events in chronological order
	for i, event := range lifecycle.History {
		switch event.Operation {
		case types.OpCreate:
			// Extract initial columns from CREATE statement
			initialColumns := r.extractInitialColumns(event.Statement)
			for pos, col := range initialColumns {
				columnStates[col.Name] = &ColumnLifecycleState{
					Name:            col.Name,
					OriginalName:    col.Name,
					DataType:        col.DataType,
					IsNullable:      col.IsNullable,
					DefaultValue:    col.DefaultValue,
					Constraints:     col.Constraints,
					Position:        pos,
					Status:          ColumnStatusActive,
					Transformations: []ColumnTransformation{},
				}
			}
		case types.OpAlter:
			// Process ALTER operations
			if err := r.processAlterOperation(columnStates, event.Statement, i); err != nil {
				return nil, errors.Wrap(err, errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation,
					"failed to process ALTER operation",
					map[string]any{"sequence": i})
			}
		}
	}

	// Post-process to identify complex patterns
	r.identifyComplexPatterns(columnStates)

	return columnStates, nil
}

// processAlterOperation processes an ALTER TABLE statement and updates column states
func (r *AdvancedColumnLifecycleRule) processAlterOperation(columnStates map[string]*ColumnLifecycleState, stmt types.Statement, sequence int) error {
	alterOps := r.parseAlterOperations(stmt)

	for _, op := range alterOps {
		switch op.Operation {
		case ColumnOpAdd:
			r.processAddColumn(columnStates, op, sequence)
		case ColumnOpDrop:
			r.processDropColumn(columnStates, op, sequence)
		case ColumnOpRename:
			r.processRenameColumn(columnStates, op, sequence)
		case ColumnOpChangeType:
			r.processChangeType(columnStates, op, sequence)
		case ColumnOpSetDefault, ColumnOpDropDefault:
			r.processDefaultChange(columnStates, op, sequence)
		case ColumnOpSetNotNull, ColumnOpDropNotNull:
			r.processNullabilityChange(columnStates, op, sequence)
		case ColumnOpAddConstraint, ColumnOpDropConstraint:
			r.processConstraintChange(columnStates, op, sequence)
		}
	}

	return nil
}

// parseAlterOperations parses ALTER and RENAME statements into column lifecycle transformations.
func (r *AdvancedColumnLifecycleRule) parseAlterOperations(stmt types.Statement) []ColumnTransformation {
	operations := r.parseAlterOperationsFromAST(stmt)
	if len(operations) > 0 {
		return operations
	}

	return r.parseAlterOperationsFallback(stmt.SQL)
}

func (r *AdvancedColumnLifecycleRule) parseAlterOperationsFromAST(stmt types.Statement) []ColumnTransformation {
	parseTree := stmt.ParseTree
	if parseTree == nil {
		parsed, err := parser.ParseMigration(stmt.SQL, "__advanced_column_alter__.sql")
		if err != nil || parsed == nil || len(parsed.Statements) == 0 {
			return nil
		}

		parseTree = parsed.Statements[0].ParseTree
	}

	if parseTree == nil || len(parseTree.Stmts) == 0 {
		return nil
	}

	operations := make([]ColumnTransformation, 0)

	for _, raw := range parseTree.Stmts {
		switch node := raw.Stmt.Node.(type) {
		case *pg_query.Node_AlterTableStmt:
			for _, cmd := range node.AlterTableStmt.Cmds {
				alterCmd := cmd.GetAlterTableCmd()
				if alterCmd == nil {
					continue
				}

				switch alterCmd.Subtype {
				case pg_query.AlterTableType_AT_AddColumn:
					colDef := alterCmd.Def.GetColumnDef()
					if colDef == nil {
						continue
					}

					operations = append(operations, ColumnTransformation{
						Operation: ColumnOpAdd,
						NewValue:  strings.ToLower(colDef.Colname),
						OldValue:  r.buildColumnDefinitionFromAST(colDef),
						SQL:       stmt.SQL,
					})

				case pg_query.AlterTableType_AT_DropColumn:
					operations = append(operations, ColumnTransformation{
						Operation: ColumnOpDrop,
						OldValue:  strings.ToLower(alterCmd.Name),
						SQL:       stmt.SQL,
					})

				case pg_query.AlterTableType_AT_AlterColumnType:
					operations = append(operations, ColumnTransformation{
						Operation: ColumnOpChangeType,
						OldValue:  strings.ToLower(alterCmd.Name),
						NewValue:  r.extractAlterColumnTypeName(alterCmd),
						SQL:       stmt.SQL,
					})

				case pg_query.AlterTableType_AT_AddConstraint:
					constraint := alterCmd.Def.GetConstraint()
					if constraint == nil {
						continue
					}

					operations = append(operations, ColumnTransformation{
						Operation: ColumnOpAddConstraint,
						OldValue:  strings.ToLower(constraint.Conname),
						NewValue:  r.buildConstraintDefinitionFromAST(constraint),
						SQL:       stmt.SQL,
					})

				case pg_query.AlterTableType_AT_DropConstraint:
					operations = append(operations, ColumnTransformation{
						Operation: ColumnOpDropConstraint,
						OldValue:  strings.ToLower(alterCmd.Name),
						SQL:       stmt.SQL,
					})
				}
			}

		case *pg_query.Node_RenameStmt:
			renameStmt := node.RenameStmt
			if renameStmt != nil && renameStmt.RenameType == pg_query.ObjectType_OBJECT_COLUMN {
				operations = append(operations, ColumnTransformation{
					Operation: ColumnOpRename,
					OldValue:  strings.ToLower(renameStmt.Subname),
					NewValue:  strings.ToLower(renameStmt.Newname),
					SQL:       stmt.SQL,
				})
			}
		}
	}

	return operations
}

func (r *AdvancedColumnLifecycleRule) parseAlterOperationsFallback(sql string) []ColumnTransformation {
	upperSQL := strings.ToUpper(sql)
	operations := make([]ColumnTransformation, 0)

	columnName := extractAlterColumnTarget(sql)
	if columnName == "" {
		return operations
	}

	switch {
	case strings.Contains(upperSQL, "DROP DEFAULT"):
		operations = append(operations, ColumnTransformation{Operation: ColumnOpDropDefault, OldValue: columnName, SQL: sql})
	case strings.Contains(upperSQL, "SET DEFAULT"):
		operations = append(operations, ColumnTransformation{Operation: ColumnOpSetDefault, OldValue: columnName, NewValue: extractDefaultClause(sql), SQL: sql})
	case strings.Contains(upperSQL, "SET NOT NULL"):
		operations = append(operations, ColumnTransformation{Operation: ColumnOpSetNotNull, OldValue: columnName, SQL: sql})
	case strings.Contains(upperSQL, "DROP NOT NULL"):
		operations = append(operations, ColumnTransformation{Operation: ColumnOpDropNotNull, OldValue: columnName, SQL: sql})
	}

	return operations
}

func extractAlterColumnTarget(sql string) string {
	tokens := strings.Fields(sql)
	for i := 0; i+2 < len(tokens); i++ {
		if strings.EqualFold(tokens[i], "ALTER") && strings.EqualFold(tokens[i+1], "COLUMN") {
			return strings.ToLower(strings.Trim(tokens[i+2], ",;"))
		}
	}

	return ""
}

func extractDefaultClause(sql string) string {
	upper := strings.ToUpper(sql)
	idx := strings.Index(upper, "SET DEFAULT")
	if idx == -1 {
		return ""
	}

	defaultExpr := strings.TrimSpace(sql[idx+len("SET DEFAULT"):])
	defaultExpr = strings.TrimSuffix(defaultExpr, ";")
	return strings.TrimSpace(defaultExpr)
}

func (r *AdvancedColumnLifecycleRule) extractAlterColumnTypeName(alterCmd *pg_query.AlterTableCmd) string {
	if alterCmd == nil || alterCmd.Def == nil {
		return ""
	}

	if typeName := alterCmd.Def.GetTypeName(); typeName != nil {
		return strings.ToUpper(typeNameFromTypeName(typeName))
	}

	if colDef := alterCmd.Def.GetColumnDef(); colDef != nil && colDef.TypeName != nil {
		return strings.ToUpper(typeNameFromTypeName(colDef.TypeName))
	}

	return ""
}

func (r *AdvancedColumnLifecycleRule) buildColumnDefinitionFromAST(colDef *pg_query.ColumnDef) string {
	if colDef == nil {
		return ""
	}

	typeName := typeNameFromTypeName(colDef.TypeName)
	parts := make([]string, 0, 4)
	if typeName != "" {
		parts = append(parts, strings.ToUpper(typeName))
	}

	for _, cnode := range colDef.Constraints {
		constraint := cnode.GetConstraint()
		if constraint == nil {
			continue
		}

		switch constraint.Contype {
		case pg_query.ConstrType_CONSTR_NOTNULL:
			parts = append(parts, "NOT NULL")
		case pg_query.ConstrType_CONSTR_DEFAULT:
			parts = append(parts, "DEFAULT")
		}
	}

	return strings.Join(parts, " ")
}

func typeNameFromTypeName(typeName *pg_query.TypeName) string {
	if typeName == nil || len(typeName.Names) == 0 {
		return ""
	}

	parts := make([]string, 0, len(typeName.Names))
	for _, node := range typeName.Names {
		strNode := node.GetString_()
		if strNode == nil {
			continue
		}
		parts = append(parts, strNode.Sval)
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, ".")
}

// processAddColumn handles ADD COLUMN operations
func (r *AdvancedColumnLifecycleRule) processAddColumn(columnStates map[string]*ColumnLifecycleState, op ColumnTransformation, sequence int) {
	columnName := op.NewValue
	columnDef := op.OldValue

	// Parse column definition
	parts := strings.Fields(columnDef)
	dataType := "TEXT" // Default
	if len(parts) > 0 {
		dataType = parts[0]
	}

	// Check if this column was previously dropped (rename pattern)
	var existingCol *ColumnLifecycleState
	for _, col := range columnStates {
		if col.Status == ColumnStatusDropped {
			// Could be a rename pattern
			existingCol = col
			break
		}
	}

	if existingCol != nil && r.enableColumnRenaming {
		// This might be a rename operation disguised as DROP + ADD
		existingCol.Name = columnName
		existingCol.Status = ColumnStatusRenamed
		existingCol.Transformations = append(existingCol.Transformations, ColumnTransformation{
			Operation:  ColumnOpRename,
			OldValue:   existingCol.OriginalName,
			NewValue:   columnName,
			AtSequence: sequence,
			SQL:        op.SQL,
		})
	} else {
		// New column
		columnStates[columnName] = &ColumnLifecycleState{
			Name:         columnName,
			OriginalName: columnName,
			DataType:     dataType,
			IsNullable:   !strings.Contains(strings.ToUpper(columnDef), "NOT NULL"),
			DefaultValue: r.extractDefaultValue(columnDef),
			Position:     len(columnStates),
			Status:       ColumnStatusActive,
			Transformations: []ColumnTransformation{
				{
					Operation:  ColumnOpAdd,
					NewValue:   columnName,
					AtSequence: sequence,
					SQL:        op.SQL,
				},
			},
		}
	}
}

// processDropColumn handles DROP COLUMN operations
func (r *AdvancedColumnLifecycleRule) processDropColumn(columnStates map[string]*ColumnLifecycleState, op ColumnTransformation, sequence int) {
	columnName := op.OldValue

	if col, exists := columnStates[columnName]; exists {
		// Check if this column was added in the same lifecycle (transient column)
		wasAddedInLifecycle := false
		for _, transformation := range col.Transformations {
			if transformation.Operation == ColumnOpAdd {
				wasAddedInLifecycle = true
				break
			}
		}

		if wasAddedInLifecycle {
			col.Status = ColumnStatusTransient
		} else {
			col.Status = ColumnStatusDropped
		}

		col.Transformations = append(col.Transformations, ColumnTransformation{
			Operation:  ColumnOpDrop,
			OldValue:   columnName,
			AtSequence: sequence,
			SQL:        op.SQL,
		})
	}
}

// processRenameColumn handles RENAME COLUMN operations
func (r *AdvancedColumnLifecycleRule) processRenameColumn(columnStates map[string]*ColumnLifecycleState, op ColumnTransformation, sequence int) {
	oldName := op.OldValue
	newName := op.NewValue

	if col, exists := columnStates[oldName]; exists {
		// Update column state
		col.Name = newName
		col.Status = ColumnStatusRenamed

		// Move to new key in map
		columnStates[newName] = col
		delete(columnStates, oldName)

		col.Transformations = append(col.Transformations, ColumnTransformation{
			Operation:  ColumnOpRename,
			OldValue:   oldName,
			NewValue:   newName,
			AtSequence: sequence,
			SQL:        op.SQL,
		})
	}
}

// processChangeType handles ALTER COLUMN TYPE operations
func (r *AdvancedColumnLifecycleRule) processChangeType(columnStates map[string]*ColumnLifecycleState, op ColumnTransformation, sequence int) {
	columnName := op.OldValue
	newType := op.NewValue

	if col, exists := columnStates[columnName]; exists {
		oldType := col.DataType
		col.DataType = newType

		col.Transformations = append(col.Transformations, ColumnTransformation{
			Operation:  ColumnOpChangeType,
			OldValue:   oldType,
			NewValue:   newType,
			AtSequence: sequence,
			SQL:        op.SQL,
		})
	}
}

// processDefaultChange handles SET/DROP DEFAULT operations
func (r *AdvancedColumnLifecycleRule) processDefaultChange(columnStates map[string]*ColumnLifecycleState, op ColumnTransformation, sequence int) {
	columnName := op.OldValue

	if col, exists := columnStates[columnName]; exists {
		oldDefault := col.DefaultValue

		if op.Operation == ColumnOpSetDefault {
			col.DefaultValue = op.NewValue
		} else {
			col.DefaultValue = ""
		}

		col.Transformations = append(col.Transformations, ColumnTransformation{
			Operation:  op.Operation,
			OldValue:   oldDefault,
			NewValue:   col.DefaultValue,
			AtSequence: sequence,
			SQL:        op.SQL,
		})
	}
}

// processNullabilityChange handles SET/DROP NOT NULL operations
func (r *AdvancedColumnLifecycleRule) processNullabilityChange(columnStates map[string]*ColumnLifecycleState, op ColumnTransformation, sequence int) {
	columnName := op.OldValue

	if col, exists := columnStates[columnName]; exists {
		oldNullable := col.IsNullable

		if op.Operation == ColumnOpSetNotNull {
			col.IsNullable = false
		} else {
			col.IsNullable = true
		}

		col.Transformations = append(col.Transformations, ColumnTransformation{
			Operation:  op.Operation,
			OldValue:   fmt.Sprintf("%v", oldNullable),
			NewValue:   fmt.Sprintf("%v", col.IsNullable),
			AtSequence: sequence,
			SQL:        op.SQL,
		})
	}
}

// processConstraintChange handles ADD/DROP CONSTRAINT operations
func (r *AdvancedColumnLifecycleRule) processConstraintChange(columnStates map[string]*ColumnLifecycleState, op ColumnTransformation, sequence int) {
	// Parse constraint definition from SQL
	constraintInfo := r.parseConstraintDefinition(op.SQL)
	if constraintInfo == nil {
		return
	}

	if len(constraintInfo.AffectedColumns) == 0 {
		if op.Operation == ColumnOpDropConstraint && constraintInfo.Name != "" {
			for _, col := range columnStates {
				for i := len(col.Constraints) - 1; i >= 0; i-- {
					if strings.EqualFold(col.Constraints[i].Name, constraintInfo.Name) {
						col.Constraints = append(col.Constraints[:i], col.Constraints[i+1:]...)
					}
				}
			}
		}
		return
	}

	// If constraint affects specific columns, update those column states
	for _, columnName := range constraintInfo.AffectedColumns {
		if col, exists := columnStates[columnName]; exists {
			if op.Operation == ColumnOpAddConstraint {
				// Add constraint to column
				col.Constraints = append(col.Constraints, ColumnConstraint{
					Type:       constraintInfo.Type,
					Name:       constraintInfo.Name,
					Definition: constraintInfo.Definition,
					TableLevel: constraintInfo.TableLevel,
				})
			} else if op.Operation == ColumnOpDropConstraint {
				// Remove constraint from column
				for i, c := range col.Constraints {
					if c.Name == constraintInfo.Name || c.Type == constraintInfo.Type {
						col.Constraints = append(col.Constraints[:i], col.Constraints[i+1:]...)
						break
					}
				}
			}

			// Track transformation
			col.Transformations = append(col.Transformations, ColumnTransformation{
				Operation:  op.Operation,
				OldValue:   constraintInfo.Name,
				NewValue:   constraintInfo.Definition,
				AtSequence: sequence,
				SQL:        op.SQL,
			})
		}
	}
}

// ColumnConstraintInfo represents parsed constraint information for column lifecycle tracking
type ColumnConstraintInfo struct {
	Name            string
	Type            ConstraintType
	Definition      string
	AffectedColumns []string
	TableLevel      bool
}

// parseConstraintDefinition parses a constraint definition from SQL
func (r *AdvancedColumnLifecycleRule) parseConstraintDefinition(sql string) *ColumnConstraintInfo {
	info := &ColumnConstraintInfo{AffectedColumns: make([]string, 0)}

	migration, err := parser.ParseMigration(sql, "__constraint_parse__.sql")
	if err == nil && migration != nil && len(migration.Statements) > 0 {
		for _, stmt := range migration.Statements {
			if stmt.ParseTree == nil {
				continue
			}

			for _, raw := range stmt.ParseTree.Stmts {
				alterStmt := raw.Stmt.GetAlterTableStmt()
				if alterStmt == nil {
					continue
				}

				for _, cmd := range alterStmt.Cmds {
					alterCmd := cmd.GetAlterTableCmd()
					if alterCmd == nil {
						continue
					}

					switch alterCmd.Subtype {
					case pg_query.AlterTableType_AT_AddConstraint:
						constraint := alterCmd.Def.GetConstraint()
						if constraint == nil {
							continue
						}

						info.Name = strings.ToLower(strings.TrimSpace(constraint.Conname))
						info.Type = mapConstraintType(constraint.Contype)
						info.Definition = r.buildConstraintDefinitionFromAST(constraint)
						info.TableLevel = true

						info.AffectedColumns = append(info.AffectedColumns, extractNodeStringNames(constraint.Keys)...)
						info.AffectedColumns = append(info.AffectedColumns, extractNodeStringNames(constraint.FkAttrs)...)
						info.AffectedColumns = r.uniqueLowercase(info.AffectedColumns)
						return info

					case pg_query.AlterTableType_AT_DropConstraint:
						info.Name = strings.ToLower(strings.TrimSpace(alterCmd.Name))
						info.TableLevel = true
						return info
					}
				}
			}
		}
	}

	// Minimal fallback for statements parser may not recover.
	upperSQL := strings.ToUpper(sql)
	if strings.Contains(upperSQL, "SET NOT NULL") {
		info.Type = ConstraintNotNull
		info.TableLevel = false
		if column := extractAlterColumnTarget(sql); column != "" {
			info.AffectedColumns = append(info.AffectedColumns, column)
		}
		info.Definition = "NOT NULL"
		return info
	}

	if strings.Contains(upperSQL, "SET DEFAULT") {
		info.Type = ConstraintDefault
		info.TableLevel = false
		if column := extractAlterColumnTarget(sql); column != "" {
			info.AffectedColumns = append(info.AffectedColumns, column)
		}
		info.Definition = "DEFAULT " + extractDefaultClause(sql)
		return info
	}

	return nil
}

func mapConstraintType(ctype pg_query.ConstrType) ConstraintType {
	switch ctype {
	case pg_query.ConstrType_CONSTR_PRIMARY:
		return ConstraintPrimaryKey
	case pg_query.ConstrType_CONSTR_FOREIGN:
		return ConstraintForeignKey
	case pg_query.ConstrType_CONSTR_UNIQUE:
		return ConstraintUnique
	case pg_query.ConstrType_CONSTR_CHECK:
		return ConstraintCheck
	case pg_query.ConstrType_CONSTR_DEFAULT:
		return ConstraintDefault
	case pg_query.ConstrType_CONSTR_NOTNULL:
		return ConstraintNotNull
	default:
		return ""
	}
}

func extractNodeStringNames(nodes []*pg_query.Node) []string {
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}

		strNode := node.GetString_()
		if strNode == nil {
			continue
		}

		name := strings.ToLower(strings.TrimSpace(strNode.Sval))
		if name != "" {
			names = append(names, name)
		}
	}

	return names
}

func (r *AdvancedColumnLifecycleRule) uniqueLowercase(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	result := make([]string, 0, len(items))

	for _, item := range items {
		normalized := strings.ToLower(strings.TrimSpace(item))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}

		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}

	return result
}

func (r *AdvancedColumnLifecycleRule) buildConstraintDefinitionFromAST(constraint *pg_query.Constraint) string {
	if constraint == nil {
		return ""
	}

	switch constraint.Contype {
	case pg_query.ConstrType_CONSTR_PRIMARY:
		cols := extractNodeStringNames(constraint.Keys)
		if len(cols) == 0 {
			return "PRIMARY KEY"
		}
		return fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(cols, ", "))

	case pg_query.ConstrType_CONSTR_FOREIGN:
		localCols := extractNodeStringNames(constraint.FkAttrs)
		refCols := extractNodeStringNames(constraint.PkAttrs)

		var b strings.Builder
		b.WriteString("FOREIGN KEY")
		if len(localCols) > 0 {
			b.WriteString(" (")
			b.WriteString(strings.Join(localCols, ", "))
			b.WriteString(")")
		}

		if constraint.Pktable != nil {
			refTable := strings.ToLower(strings.TrimSpace(constraint.Pktable.Relname))
			if schema := strings.ToLower(strings.TrimSpace(constraint.Pktable.Schemaname)); schema != "" {
				refTable = schema + "." + refTable
			}

			if refTable != "" {
				b.WriteString(" REFERENCES ")
				b.WriteString(refTable)
				if len(refCols) > 0 {
					b.WriteString(" (")
					b.WriteString(strings.Join(refCols, ", "))
					b.WriteString(")")
				}
			}
		}

		return strings.TrimSpace(b.String())

	case pg_query.ConstrType_CONSTR_UNIQUE:
		cols := extractNodeStringNames(constraint.Keys)
		if len(cols) == 0 {
			return "UNIQUE"
		}
		return fmt.Sprintf("UNIQUE (%s)", strings.Join(cols, ", "))

	case pg_query.ConstrType_CONSTR_CHECK:
		return "CHECK"

	default:
		if constraint.Conname != "" {
			return strings.ToLower(strings.TrimSpace(constraint.Conname))
		}
		return ""
	}
}

// extractConstraintDefinition extracts the full constraint definition
func (r *AdvancedColumnLifecycleRule) extractConstraintDefinition(sql string, constraintType string) string {
	// Find the constraint clause in the SQL
	upperSQL := strings.ToUpper(sql)
	startIdx := strings.Index(upperSQL, strings.ToUpper(constraintType))
	if startIdx == -1 {
		return ""
	}

	// Extract until the next comma or closing parenthesis
	endIdx := len(sql)
	for i := startIdx; i < len(sql); i++ {
		if sql[i] == ',' || sql[i] == ')' || sql[i] == ';' {
			endIdx = i
			break
		}
	}

	return strings.TrimSpace(sql[startIdx:endIdx])
}

// isSQLKeyword checks if a word is a SQL keyword
func (r *AdvancedColumnLifecycleRule) isSQLKeyword(word string) bool {
	keywords := map[string]bool{
		"and": true, "or": true, "not": true, "null": true,
		"true": true, "false": true, "between": true, "in": true,
		"like": true, "is": true, "exists": true,
	}
	return keywords[strings.ToLower(word)]
}

// identifyComplexPatterns identifies complex patterns in column lifecycles
func (r *AdvancedColumnLifecycleRule) identifyComplexPatterns(columnStates map[string]*ColumnLifecycleState) {
	for _, col := range columnStates {
		// Identify rename chains
		r.identifyRenameChains(col)

		// Identify type evolution chains
		r.identifyTypeEvolutionChains(col)

		// Identify constraint evolution
		r.identifyConstraintEvolution(col)
	}
}

// identifyRenameChains identifies columns that were renamed multiple times
func (r *AdvancedColumnLifecycleRule) identifyRenameChains(col *ColumnLifecycleState) {
	renameCount := 0
	for _, transformation := range col.Transformations {
		if transformation.Operation == ColumnOpRename {
			renameCount++
		}
	}

	if renameCount > 1 {
		// Add to dependencies for special handling
		col.Dependencies = append(col.Dependencies, "MULTIPLE_RENAMES")
	}
}

// identifyTypeEvolutionChains identifies columns with complex type evolution
func (r *AdvancedColumnLifecycleRule) identifyTypeEvolutionChains(col *ColumnLifecycleState) {
	typeChanges := 0
	for _, transformation := range col.Transformations {
		if transformation.Operation == ColumnOpChangeType {
			typeChanges++
		}
	}

	if typeChanges > 1 {
		col.Dependencies = append(col.Dependencies, "COMPLEX_TYPE_EVOLUTION")
	}
}

// identifyConstraintEvolution identifies complex constraint patterns
func (r *AdvancedColumnLifecycleRule) identifyConstraintEvolution(col *ColumnLifecycleState) {
	constraintOps := 0
	for _, transformation := range col.Transformations {
		if transformation.Operation == ColumnOpAddConstraint || transformation.Operation == ColumnOpDropConstraint {
			constraintOps++
		}
	}

	if constraintOps > 2 {
		col.Dependencies = append(col.Dependencies, "COMPLEX_CONSTRAINT_EVOLUTION")
	}
}

// generateAdvancedSchema generates the final schema with advanced column handling
func (r *AdvancedColumnLifecycleRule) generateAdvancedSchema(lifecycle *tracking.ObjectLifecycle, columnStates map[string]*ColumnLifecycleState) (string, []string, error) {
	// Get base CREATE statement
	var baseCreateSQL string
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			baseCreateSQL = event.Statement.SQL
			break
		}
	}

	if baseCreateSQL == "" {
		return "", nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation,
			"no CREATE statement found",
			map[string]any{"object": lifecycle.Name})
	}

	// Build final column list
	finalColumns := r.buildFinalColumnList(columnStates)

	// Generate optimized CREATE statement
	optimizedSQL := r.generateOptimizedCreate(baseCreateSQL, finalColumns, lifecycle.Name)

	// Generate optimization report
	optimizations := r.generateOptimizationReport(columnStates)

	return optimizedSQL, optimizations, nil
}

// buildFinalColumnList builds the final list of columns in the correct order
func (r *AdvancedColumnLifecycleRule) buildFinalColumnList(columnStates map[string]*ColumnLifecycleState) []*ColumnLifecycleState {
	var finalColumns []*ColumnLifecycleState

	for _, col := range columnStates {
		// Only include active and renamed columns
		if col.Status == ColumnStatusActive || col.Status == ColumnStatusRenamed {
			finalColumns = append(finalColumns, col)
		}
	}

	// Sort by position
	sort.Slice(finalColumns, func(i, j int) bool {
		return finalColumns[i].Position < finalColumns[j].Position
	})

	return finalColumns
}

// generateOptimizedCreate generates an optimized CREATE statement
func (r *AdvancedColumnLifecycleRule) generateOptimizedCreate(baseSQL string, columns []*ColumnLifecycleState, tableName string) string {
	// Extract table name from base SQL if needed
	if tableName == "" {
		tableName = r.extractTableName(baseSQL)
	}

	var columnDefs []string
	for _, col := range columns {
		colDef := r.buildColumnDefinition(col)
		columnDefs = append(columnDefs, colDef)
	}

	// Build optimized CREATE statement
	result := fmt.Sprintf("CREATE TABLE %s (\n", tableName)
	result += "  " + strings.Join(columnDefs, ",\n  ")
	result += "\n);"

	return result
}

// buildColumnDefinition builds a column definition from its lifecycle state
func (r *AdvancedColumnLifecycleRule) buildColumnDefinition(col *ColumnLifecycleState) string {
	var def strings.Builder
	def.WriteString(fmt.Sprintf("%s %s", col.Name, col.DataType))

	if !col.IsNullable {
		def.WriteString(" NOT NULL")
	}

	if col.DefaultValue != "" {
		def.WriteString(" DEFAULT " + col.DefaultValue)
	}

	// Add constraints
	for _, constraint := range col.Constraints {
		if !constraint.TableLevel {
			def.WriteString(" " + constraint.Definition)
		}
	}

	return def.String()
}

// generateOptimizationReport generates a report of optimizations performed
func (r *AdvancedColumnLifecycleRule) generateOptimizationReport(columnStates map[string]*ColumnLifecycleState) []string {
	var optimizations []string

	totalOps := 0
	droppedCols := 0
	renamedCols := 0
	transientCols := 0
	complexEvolution := 0

	for _, col := range columnStates {
		totalOps += len(col.Transformations)

		switch col.Status {
		case ColumnStatusDropped:
			droppedCols++
		case ColumnStatusRenamed:
			renamedCols++
		case ColumnStatusTransient:
			transientCols++
		}

		if len(col.Dependencies) > 0 {
			complexEvolution++
		}
	}

	optimizations = append(optimizations, fmt.Sprintf("Consolidated %d column operations", totalOps))

	if droppedCols > 0 {
		optimizations = append(optimizations, fmt.Sprintf("Eliminated %d dropped columns", droppedCols))
	}

	if renamedCols > 0 {
		optimizations = append(optimizations, fmt.Sprintf("Resolved %d column renames", renamedCols))
	}

	if transientCols > 0 {
		optimizations = append(optimizations, fmt.Sprintf("Eliminated %d transient columns", transientCols))
	}

	if complexEvolution > 0 {
		optimizations = append(optimizations, fmt.Sprintf("Handled %d complex evolution patterns", complexEvolution))
	}

	return optimizations
}

// Helper methods

func (r *AdvancedColumnLifecycleRule) isAdvancedColumnOperation(sql string) bool {
	upperSQL := strings.ToUpper(sql)
	patterns := []string{
		"ADD COLUMN",
		"DROP COLUMN",
		"ALTER COLUMN",
		"RENAME COLUMN",
		"ADD CONSTRAINT",
		"DROP CONSTRAINT",
	}

	for _, pattern := range patterns {
		if strings.Contains(upperSQL, pattern) {
			return true
		}
	}

	return false
}

func (r *AdvancedColumnLifecycleRule) extractInitialColumns(stmt types.Statement) []ColumnLifecycleState {
	parseTree := stmt.ParseTree
	if parseTree == nil {
		parsed, err := parser.ParseMigration(stmt.SQL, "__advanced_column_create__.sql")
		if err == nil && parsed != nil && len(parsed.Statements) > 0 {
			parseTree = parsed.Statements[0].ParseTree
		}
	}

	if parseTree == nil || len(parseTree.Stmts) == 0 {
		return nil
	}

	columns := make([]ColumnLifecycleState, 0)

	for _, raw := range parseTree.Stmts {
		createStmt := raw.Stmt.GetCreateStmt()
		if createStmt == nil {
			continue
		}

		for _, tableElt := range createStmt.TableElts {
			colDef := tableElt.GetColumnDef()
			if colDef == nil {
				continue
			}

			dataType := strings.ToUpper(typeNameFromTypeName(colDef.TypeName))
			if dataType == "" {
				dataType = "TEXT"
			}

			isNullable := true
			defaultValue := ""
			constraints := make([]ColumnConstraint, 0)

			for _, cnode := range colDef.Constraints {
				constraint := cnode.GetConstraint()
				if constraint == nil {
					continue
				}

				switch constraint.Contype {
				case pg_query.ConstrType_CONSTR_NOTNULL:
					isNullable = false
					constraints = append(constraints, ColumnConstraint{Type: ConstraintNotNull, Definition: "NOT NULL", TableLevel: false})
				case pg_query.ConstrType_CONSTR_DEFAULT:
					defaultValue = "DEFAULT"
					constraints = append(constraints, ColumnConstraint{Type: ConstraintDefault, Definition: "DEFAULT", TableLevel: false})
				case pg_query.ConstrType_CONSTR_PRIMARY:
					isNullable = false
					constraints = append(constraints, ColumnConstraint{Type: ConstraintPrimaryKey, Definition: "PRIMARY KEY", TableLevel: false})
				case pg_query.ConstrType_CONSTR_UNIQUE:
					constraints = append(constraints, ColumnConstraint{Type: ConstraintUnique, Definition: "UNIQUE", TableLevel: false})
				case pg_query.ConstrType_CONSTR_CHECK:
					constraints = append(constraints, ColumnConstraint{Type: ConstraintCheck, Definition: "CHECK", TableLevel: false})
				}
			}

			columns = append(columns, ColumnLifecycleState{
				Name:         strings.ToLower(strings.TrimSpace(colDef.Colname)),
				OriginalName: strings.ToLower(strings.TrimSpace(colDef.Colname)),
				DataType:     dataType,
				IsNullable:   isNullable,
				DefaultValue: defaultValue,
				Constraints:  constraints,
			})
		}

		break
	}

	return columns
}

func (r *AdvancedColumnLifecycleRule) extractDefaultValue(columnDef string) string {
	upperDef := strings.ToUpper(columnDef)
	if idx := strings.Index(upperDef, "DEFAULT"); idx != -1 {
		parts := strings.Fields(columnDef[idx:])
		if len(parts) > 1 {
			return parts[1]
		}
	}
	return ""
}

func (r *AdvancedColumnLifecycleRule) extractTableName(sql string) string {
	// Extract table name from CREATE TABLE statement
	parts := strings.Fields(strings.ToUpper(sql))
	for i, part := range parts {
		if part == "TABLE" && i+1 < len(parts) {
			return strings.ToLower(parts[i+1])
		}
	}
	return "unknown_table"
}

func (r *AdvancedColumnLifecycleRule) estimateLinesSaved(statements []types.Statement) int {
	return len(statements) * 3 // Rough estimate
}

// extractColumnEvolutions extracts column rename mappings from column lifecycle states
func (r *AdvancedColumnLifecycleRule) extractColumnEvolutions(tableName string, columnStates map[string]*ColumnLifecycleState) map[string]*tracking.ColumnEvolutionInfo {
	evolutions := make(map[string]*tracking.ColumnEvolutionInfo)

	for _, col := range columnStates {
		// Only track columns that were renamed or involved in DROP+ADD patterns
		if col.Status == ColumnStatusRenamed || (col.OriginalName != col.Name) {
			// Build rename chain from transformations
			renameChain := []string{col.OriginalName}
			for _, transform := range col.Transformations {
				if transform.Operation == ColumnOpRename {
					renameChain = append(renameChain, transform.NewValue)
				}
			}

			// Store evolution info with all intermediate names as keys
			for _, oldName := range renameChain[:len(renameChain)-1] {
				key := fmt.Sprintf("%s.%s", strings.ToLower(tableName), strings.ToLower(oldName))
				evolutions[key] = &tracking.ColumnEvolutionInfo{
					TableName:    strings.ToLower(tableName),
					OriginalName: col.OriginalName,
					FinalName:    col.Name,
					RenameChain:  renameChain,
				}
			}
		}
	}

	return evolutions
}

// identifyOrphanedIndexes finds indexes that reference dropped columns
// When columns are dropped, indexes referencing those columns become orphaned
// and should be excluded from the consolidated output to prevent errors
func (r *AdvancedColumnLifecycleRule) identifyOrphanedIndexes(
	tableLifecycle *tracking.ObjectLifecycle,
	columnStates map[string]*ColumnLifecycleState,
	engine ConsolidationEngine,
) []string {
	if engine == nil {
		return nil
	}

	tracker := engine.GetTracker()
	if tracker == nil {
		return nil
	}

	// Collect names of dropped columns
	droppedColumns := make(map[string]bool)
	for _, col := range columnStates {
		if col.Status == ColumnStatusDropped || col.Status == ColumnStatusTransient {
			// Store both original and current name (in case of renames before drop)
			droppedColumns[strings.ToLower(col.OriginalName)] = true
			droppedColumns[strings.ToLower(col.Name)] = true
		}
	}

	if len(droppedColumns) == 0 {
		return nil // No dropped columns, no orphaned indexes
	}

	// Find all indexes that reference this table
	tableName := strings.ToLower(tableLifecycle.Name)
	var orphanedIndexes []string

	// Get all index lifecycles from tracker
	allLifecycles := tracker.GetObjectsByCategory()
	for _, categoryObjects := range allLifecycles {
		for _, indexLifecycle := range categoryObjects {
			if indexLifecycle.Type != types.TypeIndex {
				continue
			}

			// Check if this index references our table
			// Indexes store their table name in Dependencies
			indexReferencesTable := false
			for _, dep := range indexLifecycle.Dependencies {
				if strings.ToLower(dep.DependsOn.Name) == tableName {
					indexReferencesTable = true
					break
				}
			}

			if !indexReferencesTable {
				continue
			}

			// Check if the index references any dropped columns
			// Parse the index SQL to extract column names
			finalState := indexLifecycle.GetFinalState()
			if finalState == nil {
				continue
			}

			indexSQL := strings.ToLower(finalState.SQL)
			for droppedColumn := range droppedColumns {
				if sqlMentionsIdentifier(indexSQL, droppedColumn) {
					orphanedIndexes = append(orphanedIndexes, indexLifecycle.Name)
					break
				}
			}
		}
	}

	return orphanedIndexes
}

func sqlMentionsIdentifier(sql, identifier string) bool {
	normalizedIdentifier := strings.ToLower(strings.TrimSpace(identifier))
	if normalizedIdentifier == "" {
		return false
	}

	tokens := strings.FieldsFunc(strings.ToLower(sql), func(r rune) bool {
		isLowerAlpha := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		return !(isLowerAlpha || isDigit || r == '_' || r == '.')
	})

	for _, token := range tokens {
		if token == normalizedIdentifier || strings.HasSuffix(token, "."+normalizedIdentifier) {
			return true
		}
	}

	return false
}
