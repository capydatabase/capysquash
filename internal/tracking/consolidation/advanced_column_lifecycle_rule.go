package consolidation

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// AdvancedColumnLifecycleRule handles complex column evolution patterns with edge cases
type AdvancedColumnLifecycleRule struct {
	enableDataTypeEvolution bool
	enableConstraintTracking bool
	enableColumnRenaming    bool
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
	Operation    ColumnOperation `json:"operation"`
	OldValue     string          `json:"old_value"`
	NewValue     string          `json:"new_value"`
	AtSequence   int             `json:"at_sequence"`
	SQL          string          `json:"sql"`
	HasDataOps   bool            `json:"has_data_ops"`
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
	ColumnOpAdd           ColumnOperation = "ADD"
	ColumnOpDrop          ColumnOperation = "DROP"
	ColumnOpRename        ColumnOperation = "RENAME"
	ColumnOpChangeType    ColumnOperation = "CHANGE_TYPE"
	ColumnOpSetDefault    ColumnOperation = "SET_DEFAULT"
	ColumnOpDropDefault   ColumnOperation = "DROP_DEFAULT"
	ColumnOpSetNotNull    ColumnOperation = "SET_NOT_NULL"
	ColumnOpDropNotNull   ColumnOperation = "DROP_NOT_NULL"
	ColumnOpAddConstraint ColumnOperation = "ADD_CONSTRAINT"
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
			map[string]interface{}{"rule": "AdvancedColumnLifecycleRule"})
	}

	// Build comprehensive column lifecycle map
	columnStates, err := r.buildColumnLifecycleMap(lifecycle)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation,
			"failed to build column lifecycle map",
			map[string]interface{}{"object": lifecycle.Name})
	}

	// Generate optimized schema with advanced column handling
	finalSQL, optimizations, err := r.generateAdvancedSchema(lifecycle, columnStates)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation,
			"failed to generate advanced schema",
			map[string]interface{}{"object": lifecycle.Name})
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

	// BUG #3 FIX: Identify and warn about orphaned indexes (indexes on dropped columns)
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
			initialColumns := r.extractInitialColumns(event.Statement.SQL)
			for pos, col := range initialColumns {
				columnStates[col.Name] = &ColumnLifecycleState{
					Name:         col.Name,
					OriginalName: col.Name,
					DataType:     col.DataType,
					IsNullable:   col.IsNullable,
					DefaultValue: col.DefaultValue,
					Constraints:  col.Constraints,
					Position:     pos,
					Status:       ColumnStatusActive,
					Transformations: []ColumnTransformation{},
				}
			}
		case types.OpAlter:
			// Process ALTER operations
			if err := r.processAlterOperation(columnStates, event.Statement.SQL, i); err != nil {
				return nil, errors.Wrap(err, errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation,
					"failed to process ALTER operation",
					map[string]interface{}{"sequence": i})
			}
		}
	}

	// Post-process to identify complex patterns
	r.identifyComplexPatterns(columnStates)

	return columnStates, nil
}

// processAlterOperation processes an ALTER TABLE statement and updates column states
func (r *AdvancedColumnLifecycleRule) processAlterOperation(columnStates map[string]*ColumnLifecycleState, sql string, sequence int) error {
	alterOps := r.parseAlterOperations(sql)

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

// parseAlterOperations parses an ALTER TABLE statement to extract individual operations
func (r *AdvancedColumnLifecycleRule) parseAlterOperations(sql string) []ColumnTransformation {
	var operations []ColumnTransformation

	// Regular expressions for different ALTER operations
	patterns := map[ColumnOperation]*regexp.Regexp{
		ColumnOpAdd:           regexp.MustCompile(`ADD\s+COLUMN\s+(\w+)\s+([^,;]+)`),
		ColumnOpDrop:          regexp.MustCompile(`DROP\s+COLUMN\s+(\w+)`),
		ColumnOpRename:        regexp.MustCompile(`RENAME\s+COLUMN\s+(\w+)\s+TO\s+(\w+)`),
		ColumnOpChangeType:    regexp.MustCompile(`ALTER\s+COLUMN\s+(\w+)\s+TYPE\s+([^,;]+)`),
		ColumnOpSetDefault:    regexp.MustCompile(`ALTER\s+COLUMN\s+(\w+)\s+SET\s+DEFAULT\s+([^,;]+)`),
		ColumnOpDropDefault:   regexp.MustCompile(`ALTER\s+COLUMN\s+(\w+)\s+DROP\s+DEFAULT`),
		ColumnOpSetNotNull:    regexp.MustCompile(`ALTER\s+COLUMN\s+(\w+)\s+SET\s+NOT\s+NULL`),
		ColumnOpDropNotNull:   regexp.MustCompile(`ALTER\s+COLUMN\s+(\w+)\s+DROP\s+NOT\s+NULL`),
	}

	upperSQL := strings.ToUpper(sql)

	for operation, pattern := range patterns {
		matches := pattern.FindAllStringSubmatch(upperSQL, -1)
		for _, match := range matches {
			transformation := ColumnTransformation{
				Operation: operation,
				SQL:       sql,
			}

			switch operation {
			case ColumnOpAdd:
				if len(match) >= 3 {
					transformation.NewValue = match[1] // Column name
					transformation.OldValue = match[2]  // Column definition
				}
			case ColumnOpDrop:
				if len(match) >= 2 {
					transformation.OldValue = match[1] // Column name being dropped
				}
			case ColumnOpRename:
				if len(match) >= 3 {
					transformation.OldValue = match[1] // Old name
					transformation.NewValue = match[2] // New name
				}
			case ColumnOpChangeType:
				if len(match) >= 3 {
					transformation.OldValue = match[1] // Column name
					transformation.NewValue = match[2] // New type
				}
			default:
				if len(match) >= 2 {
					transformation.OldValue = match[1] // Column name
					if len(match) >= 3 {
						transformation.NewValue = match[2] // Value (for DEFAULT, etc.)
					}
				}
			}

			operations = append(operations, transformation)
		}
	}

	return operations
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
	upperSQL := strings.ToUpper(sql)

	info := &ColumnConstraintInfo{
		AffectedColumns: make([]string, 0),
	}

	// Extract constraint name
	constraintNamePattern := regexp.MustCompile(`CONSTRAINT\s+(\w+)`)
	if matches := constraintNamePattern.FindStringSubmatch(upperSQL); len(matches) > 1 {
		info.Name = matches[1]
	}

	// Determine constraint type and affected columns
	switch {
	case strings.Contains(upperSQL, "PRIMARY KEY"):
		info.Type = ConstraintPrimaryKey
		info.TableLevel = true
		// Extract column(s) in parentheses
		if matches := regexp.MustCompile(`PRIMARY\s+KEY\s*\(([^)]+)\)`).FindStringSubmatch(upperSQL); len(matches) > 1 {
			columnList := strings.Split(matches[1], ",")
			for _, col := range columnList {
				info.AffectedColumns = append(info.AffectedColumns, strings.TrimSpace(col))
			}
			info.Definition = "PRIMARY KEY (" + matches[1] + ")"
		}

	case strings.Contains(upperSQL, "FOREIGN KEY"):
		info.Type = ConstraintForeignKey
		info.TableLevel = true
		// Extract source column(s)
		if matches := regexp.MustCompile(`FOREIGN\s+KEY\s*\(([^)]+)\)`).FindStringSubmatch(upperSQL); len(matches) > 1 {
			columnList := strings.Split(matches[1], ",")
			for _, col := range columnList {
				info.AffectedColumns = append(info.AffectedColumns, strings.TrimSpace(col))
			}
		}
		info.Definition = r.extractConstraintDefinition(sql, "FOREIGN KEY")

	case strings.Contains(upperSQL, "UNIQUE"):
		info.Type = ConstraintUnique
		// Check if table-level or column-level
		if matches := regexp.MustCompile(`UNIQUE\s*\(([^)]+)\)`).FindStringSubmatch(upperSQL); len(matches) > 1 {
			info.TableLevel = true
			columnList := strings.Split(matches[1], ",")
			for _, col := range columnList {
				info.AffectedColumns = append(info.AffectedColumns, strings.TrimSpace(col))
			}
			info.Definition = "UNIQUE (" + matches[1] + ")"
		} else {
			// Column-level UNIQUE
			info.TableLevel = false
			info.Definition = "UNIQUE"
		}

	case strings.Contains(upperSQL, "CHECK"):
		info.Type = ConstraintCheck
		info.TableLevel = true
		// Extract CHECK expression
		if matches := regexp.MustCompile(`CHECK\s*\(([^)]+)\)`).FindStringSubmatch(upperSQL); len(matches) > 1 {
			info.Definition = "CHECK (" + matches[1] + ")"
			// Try to extract column names from CHECK expression
			columnPattern := regexp.MustCompile(`\b([a-z_][a-z0-9_]*)\b`)
			colMatches := columnPattern.FindAllStringSubmatch(strings.ToLower(matches[1]), -1)
			for _, match := range colMatches {
				// Filter out SQL keywords
				colName := match[1]
				if !r.isSQLKeyword(colName) {
					info.AffectedColumns = append(info.AffectedColumns, colName)
				}
			}
		}

	case strings.Contains(upperSQL, "NOT NULL"):
		info.Type = ConstraintNotNull
		info.TableLevel = false
		// Extract column name from "ALTER COLUMN columnname SET NOT NULL"
		if matches := regexp.MustCompile(`ALTER\s+COLUMN\s+(\w+)\s+SET\s+NOT\s+NULL`).FindStringSubmatch(upperSQL); len(matches) > 1 {
			info.AffectedColumns = append(info.AffectedColumns, matches[1])
		}
		info.Definition = "NOT NULL"

	case strings.Contains(upperSQL, "DEFAULT"):
		info.Type = ConstraintDefault
		info.TableLevel = false
		// Extract column name and default value
		if matches := regexp.MustCompile(`ALTER\s+COLUMN\s+(\w+)\s+SET\s+DEFAULT\s+([^,;]+)`).FindStringSubmatch(upperSQL); len(matches) > 2 {
			info.AffectedColumns = append(info.AffectedColumns, matches[1])
			info.Definition = "DEFAULT " + matches[2]
		}
	}

	return info
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
			map[string]interface{}{"object": lifecycle.Name})
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
	def := fmt.Sprintf("%s %s", col.Name, col.DataType)

	if !col.IsNullable {
		def += " NOT NULL"
	}

	if col.DefaultValue != "" {
		def += " DEFAULT " + col.DefaultValue
	}

	// Add constraints
	for _, constraint := range col.Constraints {
		if !constraint.TableLevel {
			def += " " + constraint.Definition
		}
	}

	return def
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

func (r *AdvancedColumnLifecycleRule) extractInitialColumns(sql string) []ColumnLifecycleState {
	// Simplified column extraction - in practice would use proper SQL parsing
	return []ColumnLifecycleState{}
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
// BUG #3 FIX: When columns are dropped, indexes referencing those columns become orphaned
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
				// Simple check: does the index SQL contain the dropped column name?
				// This is a heuristic - may produce false positives, but that's safe
				// We look for column name as a word boundary to avoid partial matches
				columnPattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(droppedColumn) + `\b`)
				if columnPattern.MatchString(indexSQL) {
					orphanedIndexes = append(orphanedIndexes, indexLifecycle.Name)
					break
				}
			}
		}
	}

	return orphanedIndexes
}
