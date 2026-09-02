package squasher

import (
	"fmt"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/errors"
	"github.com/capy-base/pgsquash-engine/internal/types"
	"github.com/capy-base/pgsquash-engine/internal/utils"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// CircularFKHandler detects and handles circular foreign key dependencies
type CircularFKHandler struct {
	// Maps table name -> list of tables it references via FK
	fkDependencies map[string][]string
	// Maps table name -> list of FK constraints
	tableConstraints map[string][]*ForeignKeyConstraint
}

// ForeignKeyConstraint represents a foreign key constraint
type ForeignKeyConstraint struct {
	ConstraintName string
	SourceTable    string
	SourceColumns  []string
	RefTable       string
	RefColumns     []string
	OnDelete       string
	OnUpdate       string
	Deferrable     bool
	Initially      string
	OriginalSQL    string // Original SQL for the constraint
}

// NewCircularFKHandler creates a new circular FK handler
func NewCircularFKHandler() *CircularFKHandler {
	return &CircularFKHandler{
		fkDependencies:   make(map[string][]string),
		tableConstraints: make(map[string][]*ForeignKeyConstraint),
	}
}

// DetectCircularDependencies finds all circular FK dependencies in a set of tables
func (h *CircularFKHandler) DetectCircularDependencies(tables map[string]*types.Statement) [][]string {
	// Build FK dependency graph
	h.buildDependencyGraph(tables)

	// Detect cycles using DFS
	visited := make(map[string]bool)
	recursionStack := make(map[string]bool)
	var cycles [][]string
	var currentPath []string

	for table := range h.fkDependencies {
		if !visited[table] {
			h.detectCyclesDFS(table, visited, recursionStack, &currentPath, &cycles)
		}
	}

	if len(cycles) > 0 {
		utils.GetDefaultLogger().WithPrefix("CIRCULAR-FK").Info("Detected %d circular FK dependency chains", len(cycles))
		for i, cycle := range cycles {
			utils.GetDefaultLogger().WithPrefix("CIRCULAR-FK").Info("  Cycle %d: %s", i+1, strings.Join(cycle, " -> "))
		}
	}

	return cycles
}

// buildDependencyGraph builds the FK dependency graph from table statements
func (h *CircularFKHandler) buildDependencyGraph(tables map[string]*types.Statement) {
	for tableName, stmt := range tables {
		// Extract FK constraints from the statement
		constraints := h.extractForeignKeys(stmt)

		if len(constraints) > 0 {
			h.tableConstraints[tableName] = constraints

			// Build dependency list
			for _, constraint := range constraints {
				if constraint.RefTable != tableName { // Ignore self-references for cycle detection
					h.fkDependencies[tableName] = append(h.fkDependencies[tableName], constraint.RefTable)
				}
			}
		}
	}
}

// extractForeignKeys extracts all foreign key constraints from a CREATE TABLE statement
func (h *CircularFKHandler) extractForeignKeys(stmt *types.Statement) []*ForeignKeyConstraint {
	var constraints []*ForeignKeyConstraint

	// Parse the statement AST
	if stmt.ParseTree == nil {
		return constraints
	}

	parseResult := stmt.ParseTree
	if parseResult == nil {
		return constraints
	}

	if len(parseResult.Stmts) == 0 {
		return constraints
	}

	rawStmt := parseResult.Stmts[0]
	createStmt := rawStmt.Stmt.GetCreateStmt()
	if createStmt == nil {
		return constraints
	}

	tableName := stmt.ObjectName

	// Extract table-level constraints
	for _, tableElt := range createStmt.TableElts {
		if constraint := tableElt.GetConstraint(); constraint != nil {
			if constraint.Contype == pg_query.ConstrType_CONSTR_FOREIGN {
				fk := h.buildForeignKeyFromConstraint(constraint, tableName)
				if fk != nil {
					constraints = append(constraints, fk)
				}
			}
		}

		// Extract column-level constraints (inline REFERENCES)
		if columnDef := tableElt.GetColumnDef(); columnDef != nil {
			for _, colConstraint := range columnDef.Constraints {
				if constraint := colConstraint.GetConstraint(); constraint != nil {
					if constraint.Contype == pg_query.ConstrType_CONSTR_FOREIGN {
						fk := h.buildForeignKeyFromConstraint(constraint, tableName)
						if fk != nil {
							// Add source column from the column definition
							if fk.SourceColumns == nil {
								fk.SourceColumns = []string{columnDef.Colname}
							}
							constraints = append(constraints, fk)
						}
					}
				}
			}
		}
	}

	return constraints
}

// buildForeignKeyFromConstraint builds a ForeignKeyConstraint from pg_query Constraint
func (h *CircularFKHandler) buildForeignKeyFromConstraint(constraint *pg_query.Constraint, sourceTable string) *ForeignKeyConstraint {
	if constraint.Pktable == nil {
		return nil
	}

	fk := &ForeignKeyConstraint{
		ConstraintName: constraint.Conname,
		SourceTable:    sourceTable,
		RefTable:       h.getTableName(constraint.Pktable),
		Deferrable:     constraint.Deferrable,
	}

	// Extract source columns (local columns)
	for _, key := range constraint.FkAttrs {
		if str := key.GetString_(); str != nil {
			fk.SourceColumns = append(fk.SourceColumns, str.Sval)
		}
	}

	// Extract referenced columns
	for _, key := range constraint.PkAttrs {
		if str := key.GetString_(); str != nil {
			fk.RefColumns = append(fk.RefColumns, str.Sval)
		}
	}

	// Extract ON DELETE/UPDATE actions (stored as bytes/runes in pg_query)
	switch string(constraint.FkDelAction) {
	case "a": // NO ACTION
		fk.OnDelete = "NO ACTION"
	case "r": // RESTRICT
		fk.OnDelete = "RESTRICT"
	case "c": // CASCADE
		fk.OnDelete = "CASCADE"
	case "n": // SET NULL
		fk.OnDelete = "SET NULL"
	case "d": // SET DEFAULT
		fk.OnDelete = "SET DEFAULT"
	}

	switch string(constraint.FkUpdAction) {
	case "a":
		fk.OnUpdate = "NO ACTION"
	case "r":
		fk.OnUpdate = "RESTRICT"
	case "c":
		fk.OnUpdate = "CASCADE"
	case "n":
		fk.OnUpdate = "SET NULL"
	case "d":
		fk.OnUpdate = "SET DEFAULT"
	}

	// Build deferrable status
	if constraint.Deferrable {
		if constraint.Initdeferred {
			fk.Initially = "DEFERRED"
		} else {
			fk.Initially = "IMMEDIATE"
		}
	}

	return fk
}

// getTableName extracts table name from RangeVar
func (h *CircularFKHandler) getTableName(rangeVar *pg_query.RangeVar) string {
	if rangeVar == nil {
		return ""
	}

	if rangeVar.Schemaname != "" {
		return fmt.Sprintf("%s.%s", rangeVar.Schemaname, rangeVar.Relname)
	}
	return rangeVar.Relname
}

// detectCyclesDFS performs DFS to detect cycles in the FK dependency graph
func (h *CircularFKHandler) detectCyclesDFS(
	table string,
	visited map[string]bool,
	recursionStack map[string]bool,
	currentPath *[]string,
	cycles *[][]string,
) {
	visited[table] = true
	recursionStack[table] = true
	*currentPath = append(*currentPath, table)

	// Visit all dependencies
	for _, refTable := range h.fkDependencies[table] {
		if !visited[refTable] {
			h.detectCyclesDFS(refTable, visited, recursionStack, currentPath, cycles)
		} else if recursionStack[refTable] {
			// Found a cycle - extract the cycle from currentPath
			cycle := h.extractCycle(*currentPath, refTable)
			if len(cycle) > 0 {
				*cycles = append(*cycles, cycle)
			}
		}
	}

	// Backtrack
	*currentPath = (*currentPath)[:len(*currentPath)-1]
	recursionStack[table] = false
}

// extractCycle extracts a cycle from the current path
func (h *CircularFKHandler) extractCycle(path []string, cycleStart string) []string {
	// Find where the cycle starts
	startIdx := -1
	for i, table := range path {
		if table == cycleStart {
			startIdx = i
			break
		}
	}

	if startIdx == -1 {
		return nil
	}

	// Extract cycle (including the cycleStart node at the end to close the loop)
	cycle := make([]string, len(path)-startIdx+1)
	copy(cycle, path[startIdx:])
	cycle[len(cycle)-1] = cycleStart // Close the loop

	return cycle
}

// RemoveCircularFKsFromTables removes FK constraints that are part of circular dependencies
// Returns modified table statements and a list of ALTER TABLE statements to add FKs later
func (h *CircularFKHandler) RemoveCircularFKsFromTables(
	tables map[string]*types.Statement,
	cycles [][]string,
) (map[string]*types.Statement, []*types.Statement, error) {

	// Build set of tables involved in cycles
	cycleTableSet := make(map[string]bool)
	for _, cycle := range cycles {
		for _, table := range cycle {
			cycleTableSet[table] = true
		}
	}

	modifiedTables := make(map[string]*types.Statement)
	var alterStatements []*types.Statement

	// Process each table
	for tableName, stmt := range tables {
		if !cycleTableSet[tableName] {
			// Table not in any cycle - keep as is
			modifiedTables[tableName] = stmt
			continue
		}

		utils.GetDefaultLogger().WithPrefix("CIRCULAR-FK").Info("Removing circular FK constraints from table: %s", tableName)

		// Get FK constraints for this table
		constraints := h.tableConstraints[tableName]
		if len(constraints) == 0 {
			modifiedTables[tableName] = stmt
			continue
		}

		// Separate circular FKs from non-circular FKs
		var circularFKs []*ForeignKeyConstraint

		for _, fk := range constraints {
			isCircular := false
			// Check if this FK is part of a cycle
			for _, cycle := range cycles {
				if h.fkInCycle(fk, cycle) {
					isCircular = true
					break
				}
			}

			if isCircular {
				circularFKs = append(circularFKs, fk)
			}
			// Non-circular FKs remain in the original CREATE TABLE
		}

		// Generate modified CREATE TABLE statement (without circular FKs)
		modifiedStmt, err := h.removeConstraintsFromCreateTable(stmt, circularFKs)
		if err != nil {
			return nil, nil, errors.NewError(
				errors.ErrorCodeConsolidationFailed,
				fmt.Sprintf("failed to remove constraints from %s: %v", tableName, err),
				errors.SeverityError,
				errors.CategoryConstraint,
			).WithObject(tableName, "table").WithInnerError(err)
		}

		modifiedTables[tableName] = modifiedStmt

		// Generate ALTER TABLE statements for circular FKs
		for _, fk := range circularFKs {
			alterStmt := h.generateAlterTableAddConstraint(fk)
			alterStatements = append(alterStatements, alterStmt)
		}
	}

	utils.GetDefaultLogger().WithPrefix("CIRCULAR-FK").Info("Generated %d ALTER TABLE statements for circular FK constraints", len(alterStatements))
	return modifiedTables, alterStatements, nil
}

// fkInCycle checks if a foreign key is part of a circular dependency cycle
func (h *CircularFKHandler) fkInCycle(fk *ForeignKeyConstraint, cycle []string) bool {
	// Check if both source and ref tables are in the cycle
	sourceInCycle := false
	refInCycle := false

	for _, table := range cycle {
		if table == fk.SourceTable {
			sourceInCycle = true
		}
		if table == fk.RefTable {
			refInCycle = true
		}
	}

	return sourceInCycle && refInCycle
}

// removeConstraintsFromCreateTable removes specified FK constraints from a CREATE TABLE statement
// using AST manipulation instead of fragile regex patterns.
//
// This implementation:
// 1. Parses stmt.ParseTree to get the CREATE TABLE AST
// 2. Filters out FK constraint nodes from TableElts
// 3. Uses Deparse() to convert the modified AST back to SQL
func (h *CircularFKHandler) removeConstraintsFromCreateTable(
	stmt *types.Statement,
	constraintsToRemove []*ForeignKeyConstraint,
) (*types.Statement, error) {

	// Build set of constraint names to remove (case-insensitive)
	removeSet := make(map[string]bool)
	for _, fk := range constraintsToRemove {
		if fk.ConstraintName != "" {
			removeSet[strings.ToLower(fk.ConstraintName)] = true
		}
	}

	// Build set of source columns for inline FK removal
	// Key format: "columnname:reftable" for matching
	inlineRefsToRemove := make(map[string]bool)
	for _, fk := range constraintsToRemove {
		if len(fk.SourceColumns) == 1 {
			key := strings.ToLower(fk.SourceColumns[0]) + ":" + strings.ToLower(fk.RefTable)
			inlineRefsToRemove[key] = true
		}
	}

	// Parse the statement AST
	if stmt.ParseTree == nil {
		return nil, errors.NewError(
			errors.ErrorCodeSyntaxError,
			"statement has no parse tree",
			errors.SeverityError,
			errors.CategoryParsing,
		).WithObject(stmt.ObjectName, string(stmt.ObjectType))
	}

	parseResult := stmt.ParseTree
	if parseResult == nil {
		return nil, errors.NewError(
			errors.ErrorCodeSyntaxError,
			"invalid parse tree type",
			errors.SeverityError,
			errors.CategoryParsing,
		).WithObject(stmt.ObjectName, string(stmt.ObjectType))
	}

	if len(parseResult.Stmts) == 0 {
		return nil, errors.NewError(
			errors.ErrorCodeSyntaxError,
			"parse tree has no statements",
			errors.SeverityError,
			errors.CategoryParsing,
		).WithObject(stmt.ObjectName, string(stmt.ObjectType))
	}

	rawStmt := parseResult.Stmts[0]
	createStmt := rawStmt.Stmt.GetCreateStmt()
	if createStmt == nil {
		return nil, errors.NewError(
			errors.ErrorCodeSyntaxError,
			"not a CREATE TABLE statement",
			errors.SeverityError,
			errors.CategoryParsing,
		).WithObject(stmt.ObjectName, string(stmt.ObjectType))
	}

	// Filter TableElts to remove FK constraints
	var filteredElts []*pg_query.Node

	for _, tableElt := range createStmt.TableElts {
		shouldKeep := true

		// Check table-level constraints
		if constraint := tableElt.GetConstraint(); constraint != nil {
			if constraint.Contype == pg_query.ConstrType_CONSTR_FOREIGN {
				// Table-level FK constraint - check if it should be removed
				constraintName := strings.ToLower(constraint.Conname)
				if removeSet[constraintName] {
					shouldKeep = false
					utils.GetDefaultLogger().WithPrefix("CIRCULAR-FK").Info("Removing table-level FK constraint: %s", constraint.Conname)
				}
			}
		}

		// Check column-level inline REFERENCES constraints
		if columnDef := tableElt.GetColumnDef(); columnDef != nil {
			var filteredConstraints []*pg_query.Node

			for _, colConstraintNode := range columnDef.Constraints {
				colConstraint := colConstraintNode.GetConstraint()
				if colConstraint != nil && colConstraint.Contype == pg_query.ConstrType_CONSTR_FOREIGN {
					// Inline FK - check if it should be removed
					colName := strings.ToLower(columnDef.Colname)
					refTable := strings.ToLower(h.getTableName(colConstraint.Pktable))
					key := colName + ":" + refTable

					if inlineRefsToRemove[key] {
						utils.GetDefaultLogger().WithPrefix("CIRCULAR-FK").Info("Removing inline FK constraint from column: %s REFERENCES %s", columnDef.Colname, refTable)
						// Don't add this constraint to filteredConstraints
						continue
					}
				}
				// Keep this constraint
				filteredConstraints = append(filteredConstraints, colConstraintNode)
			}

			// Update column's constraints list
			columnDef.Constraints = filteredConstraints
		}

		if shouldKeep {
			filteredElts = append(filteredElts, tableElt)
		}
	}

	// Update the CREATE TABLE statement with filtered elements
	createStmt.TableElts = filteredElts

	// Deparse the modified AST back to SQL
	modifiedSQL, err := Deparse(parseResult)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			fmt.Sprintf("failed to deparse modified statement: %v", err),
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithObject(stmt.ObjectName, string(stmt.ObjectType)).WithInnerError(err)
	}

	// Create modified statement
	modifiedStmt := *stmt
	modifiedStmt.SQL = strings.TrimSpace(modifiedSQL)

	return &modifiedStmt, nil
}

// generateAlterTableAddConstraint generates an ALTER TABLE ADD CONSTRAINT statement
func (h *CircularFKHandler) generateAlterTableAddConstraint(fk *ForeignKeyConstraint) *types.Statement {
	var sql strings.Builder

	sql.WriteString(fmt.Sprintf("ALTER TABLE %s\n", fk.SourceTable))
	sql.WriteString("    ADD CONSTRAINT ")

	// Generate constraint name if not present
	constraintName := fk.ConstraintName
	if constraintName == "" {
		constraintName = fmt.Sprintf("fk_%s_%s",
			strings.ReplaceAll(fk.SourceTable, ".", "_"),
			strings.Join(fk.SourceColumns, "_"))
	}

	sql.WriteString(constraintName)
	sql.WriteString(" FOREIGN KEY (")
	sql.WriteString(strings.Join(fk.SourceColumns, ", "))
	sql.WriteString(")\n    REFERENCES ")
	sql.WriteString(fk.RefTable)

	if len(fk.RefColumns) > 0 {
		sql.WriteString(" (")
		sql.WriteString(strings.Join(fk.RefColumns, ", "))
		sql.WriteString(")")
	}

	if fk.OnDelete != "" && fk.OnDelete != "NO ACTION" {
		sql.WriteString("\n    ON DELETE ")
		sql.WriteString(fk.OnDelete)
	}

	if fk.OnUpdate != "" && fk.OnUpdate != "NO ACTION" {
		sql.WriteString("\n    ON UPDATE ")
		sql.WriteString(fk.OnUpdate)
	}

	if fk.Deferrable {
		sql.WriteString("\n    DEFERRABLE")
		if fk.Initially != "" {
			sql.WriteString(" INITIALLY ")
			sql.WriteString(fk.Initially)
		}
	}

	sql.WriteString(";")

	return &types.Statement{
		SQL:        sql.String(),
		ObjectType: types.TypeConstraint,
		Operation:  types.OpAlter,
		ObjectName: constraintName,
		Category:   types.CategoryConstraints,
		Comments:   []string{fmt.Sprintf("-- Circular FK constraint: %s -> %s", fk.SourceTable, fk.RefTable)},
	}
}
