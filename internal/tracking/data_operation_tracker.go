package tracking

import (
	"fmt"
	"sort"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/capysquash/pgsquash-engine/internal/utils"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// DataOperation represents a single data operation (INSERT/UPDATE/DELETE) with dependency tracking
type DataOperation struct {
	Statement    types.Statement  // Original statement with ParseTree for AST analysis
	Sequence     int              // Migration sequence number (for ordering)
	Index        int              // Statement index within migration (for ordering)
	Table        string           // Target table name (extracted from AST)
	Operation    types.Operation  // INSERT, UPDATE, or DELETE
	DependsOn    []string         // Tables this operation depends on (extracted from AST)
	ReferencedBy []*DataOperation // Operations that reference this one (for dependency graph)
}

// DataOperationTracker tracks all data operations separately from DDL object lifecycles
//
// Design Decision: Data operations are fundamentally different from schema objects:
// - Schema objects have lifecycles (CREATE → ALTER → DROP)
// - Data operations are one-time mutations without lifecycle
// - Data ops must preserve sequence and have different dependency rules
//
// Therefore, we track them separately using AST-based dependency extraction.
type DataOperationTracker struct {
	operations []*DataOperation            // All data operations in original order
	byTable    map[string][]*DataOperation // Operations grouped by table for quick lookup
	insertOps  []*DataOperation            // All INSERT operations (for dependency resolution)
	updateOps  []*DataOperation            // All UPDATE operations
	deleteOps  []*DataOperation            // All DELETE operations
	logger     *utils.Logger               // Logger for tracking operations
}

// NewDataOperationTracker creates a new data operation tracker
func NewDataOperationTracker() *DataOperationTracker {
	return &DataOperationTracker{
		operations: make([]*DataOperation, 0),
		byTable:    make(map[string][]*DataOperation),
		insertOps:  make([]*DataOperation, 0),
		updateOps:  make([]*DataOperation, 0),
		deleteOps:  make([]*DataOperation, 0),
		logger:     utils.GetDefaultLogger().WithPrefix("DATA-OPS"),
	}
}

// AddOperation adds a data operation to the tracker with AST-based dependency extraction
func (dot *DataOperationTracker) AddOperation(stmt types.Statement, sequence int, index int) error {
	if !stmt.IsDataOp {
		return errors.NewError(
			errors.ErrorCodeInvalidSQL,
			"statement is not a data operation",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithStatement(stmt.SQL)
	}

	// Extract table name from AST (not from ObjectName which may be incorrect)
	tableName, err := extractTableNameFromAST(stmt)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeAnalysisError,
			"failed to extract table name from AST",
			errors.SeverityError,
			errors.CategoryParsing,
		).WithStatement(stmt.SQL).WithInnerError(err)
	}

	// Extract dependencies from AST (foreign key references, SELECT subqueries, etc.)
	dependencies, err := extractDependenciesFromAST(stmt)
	if err != nil {
		dot.logger.Warn("Failed to extract dependencies for %s on %s: %v (continuing with empty dependencies)", stmt.Operation, tableName, err)
		dependencies = []string{} // Continue with empty dependencies rather than failing
	}

	// Create data operation
	dataOp := &DataOperation{
		Statement:    stmt,
		Sequence:     sequence,
		Index:        index,
		Table:        tableName,
		Operation:    stmt.Operation,
		DependsOn:    dependencies,
		ReferencedBy: make([]*DataOperation, 0),
	}

	// Add to tracker
	dot.operations = append(dot.operations, dataOp)

	// Add to table index
	if dot.byTable[tableName] == nil {
		dot.byTable[tableName] = make([]*DataOperation, 0)
	}
	dot.byTable[tableName] = append(dot.byTable[tableName], dataOp)

	// Add to operation type index
	switch stmt.Operation {
	case types.OpInsert:
		dot.insertOps = append(dot.insertOps, dataOp)
	case types.OpUpdate:
		dot.updateOps = append(dot.updateOps, dataOp)
	case types.OpDelete:
		dot.deleteOps = append(dot.deleteOps, dataOp)
	}

	return nil
}

// GetSortedOperations returns all data operations sorted by dependencies
//
// Sorting Algorithm:
// 1. Within same table: INSERT before UPDATE before DELETE
// 2. Cross-table: topological sort by foreign key dependencies
// 3. Preserve original sequence when no dependencies exist
func (dot *DataOperationTracker) GetSortedOperations() []*DataOperation {
	if len(dot.operations) == 0 {
		return []*DataOperation{}
	}

	// Build dependency graph
	dot.buildDependencyGraph()

	// Topological sort with stable ordering
	sorted := dot.topologicalSort()

	return sorted
}

// buildDependencyGraph builds the ReferencedBy relationships
func (dot *DataOperationTracker) buildDependencyGraph() {
	// For each operation, find operations it depends on and add reverse references
	for _, op := range dot.operations {
		for _, depTable := range op.DependsOn {
			// Find INSERTs to this table that come before this operation
			if tableOps, exists := dot.byTable[depTable]; exists {
				for _, depOp := range tableOps {
					// Only depend on operations that come before us in sequence
					if depOp.Operation == types.OpInsert &&
						(depOp.Sequence < op.Sequence || (depOp.Sequence == op.Sequence && depOp.Index < op.Index)) {
						depOp.ReferencedBy = append(depOp.ReferencedBy, op)
					}
				}
			}
		}

		// Special case: UPDATE and DELETE depend on INSERT to same table
		if op.Operation == types.OpUpdate || op.Operation == types.OpDelete {
			// Find the most recent INSERT to this table that comes before this operation
			if tableOps, exists := dot.byTable[op.Table]; exists {
				var mostRecentInsert *DataOperation
				for _, tableOp := range tableOps {
					if tableOp.Operation == types.OpInsert &&
						(tableOp.Sequence < op.Sequence || (tableOp.Sequence == op.Sequence && tableOp.Index < op.Index)) {
						if mostRecentInsert == nil ||
							tableOp.Sequence > mostRecentInsert.Sequence ||
							(tableOp.Sequence == mostRecentInsert.Sequence && tableOp.Index > mostRecentInsert.Index) {
							mostRecentInsert = tableOp
						}
					}
				}
				if mostRecentInsert != nil {
					mostRecentInsert.ReferencedBy = append(mostRecentInsert.ReferencedBy, op)
				}
			}
		}
	}
}

// topologicalSort performs topological sort with stable ordering (preserves original sequence when possible)
func (dot *DataOperationTracker) topologicalSort() []*DataOperation {
	// Kahn's algorithm with stable ordering
	inDegree := make(map[*DataOperation]int)
	for _, op := range dot.operations {
		inDegree[op] = 0
	}

	// Calculate in-degrees (how many operations must come before each operation)
	for _, op := range dot.operations {
		for _, referenced := range op.ReferencedBy {
			inDegree[referenced]++
		}
	}

	// Start with operations that have no dependencies
	queue := make([]*DataOperation, 0)
	for _, op := range dot.operations {
		if inDegree[op] == 0 {
			queue = append(queue, op)
		}
	}

	// Sort queue by original sequence to maintain stable ordering
	sort.Slice(queue, func(i, j int) bool {
		if queue[i].Sequence != queue[j].Sequence {
			return queue[i].Sequence < queue[j].Sequence
		}
		return queue[i].Index < queue[j].Index
	})

	// Process queue
	result := make([]*DataOperation, 0, len(dot.operations))
	for len(queue) > 0 {
		// Pop first element (already sorted by sequence)
		current := queue[0]
		queue = queue[1:]

		result = append(result, current)

		// Reduce in-degree for operations that depend on current
		for _, referenced := range current.ReferencedBy {
			inDegree[referenced]--
			if inDegree[referenced] == 0 {
				queue = append(queue, referenced)
			}
		}

		// Re-sort queue to maintain stable ordering
		sort.Slice(queue, func(i, j int) bool {
			if queue[i].Sequence != queue[j].Sequence {
				return queue[i].Sequence < queue[j].Sequence
			}
			return queue[i].Index < queue[j].Index
		})
	}

	// Check for cycles (should not happen with data operations, but be defensive)
	if len(result) != len(dot.operations) {
		dot.logger.Warn("Topological sort detected cycle in data operations (sorted %d of %d), falling back to sequence order", len(result), len(dot.operations))
		// Fallback: return operations in original sequence order
		sortedBySequence := make([]*DataOperation, len(dot.operations))
		copy(sortedBySequence, dot.operations)
		sort.Slice(sortedBySequence, func(i, j int) bool {
			if sortedBySequence[i].Sequence != sortedBySequence[j].Sequence {
				return sortedBySequence[i].Sequence < sortedBySequence[j].Sequence
			}
			return sortedBySequence[i].Index < sortedBySequence[j].Index
		})
		return sortedBySequence
	}

	return result
}

// GetStatistics returns statistics about tracked data operations
func (dot *DataOperationTracker) GetStatistics() map[string]any {
	return map[string]any{
		"total_operations": len(dot.operations),
		"insert_count":     len(dot.insertOps),
		"update_count":     len(dot.updateOps),
		"delete_count":     len(dot.deleteOps),
		"tables_affected":  len(dot.byTable),
	}
}

// extractTableNameFromAST extracts the target table name from the AST parse tree
func extractTableNameFromAST(stmt types.Statement) (string, error) {
	if stmt.ParseTree == nil {
		// Fallback to ObjectName if no parse tree
		if stmt.ObjectName != "" {
			return stmt.ObjectName, nil
		}
		return "", errors.NewError(
			errors.ErrorCodeAnalysisError,
			"no parse tree and no object name available",
			errors.SeverityError,
			errors.CategoryParsing,
		)
	}

	parseResult := stmt.ParseTree
	if parseResult == nil || len(parseResult.Stmts) == 0 {
		return "", errors.NewError(
			errors.ErrorCodeAnalysisError,
			"invalid parse tree structure",
			errors.SeverityError,
			errors.CategoryParsing,
		)
	}

	node := parseResult.Stmts[0].Stmt
	switch n := node.Node.(type) {
	case *pg_query.Node_InsertStmt:
		if n.InsertStmt.Relation != nil {
			return extractRelationName(n.InsertStmt.Relation), nil
		}
	case *pg_query.Node_UpdateStmt:
		if n.UpdateStmt.Relation != nil {
			return extractRelationName(n.UpdateStmt.Relation), nil
		}
	case *pg_query.Node_DeleteStmt:
		if n.DeleteStmt.Relation != nil {
			return extractRelationName(n.DeleteStmt.Relation), nil
		}
	}

	// Fallback to ObjectName
	if stmt.ObjectName != "" {
		return stmt.ObjectName, nil
	}

	return "", errors.NewError(
		errors.ErrorCodeAnalysisError,
		"could not extract table name from AST",
		errors.SeverityError,
		errors.CategoryParsing,
	)
}

// extractRelationName extracts table name from RangeVar node
func extractRelationName(relation *pg_query.RangeVar) string {
	if relation == nil {
		return ""
	}
	// Handle schema.table format
	if relation.Schemaname != "" {
		return fmt.Sprintf("%s.%s", relation.Schemaname, relation.Relname)
	}
	return relation.Relname
}

// extractDependenciesFromAST extracts table dependencies from the AST
//
// Dependencies include:
// - Tables referenced in FROM clause (INSERT ... SELECT FROM)
// - Tables referenced in WHERE clause subqueries
// - Tables referenced in JOIN clauses
func extractDependenciesFromAST(stmt types.Statement) ([]string, error) {
	if stmt.ParseTree == nil {
		return []string{}, nil
	}

	parseResult := stmt.ParseTree
	if parseResult == nil || len(parseResult.Stmts) == 0 {
		return []string{}, nil
	}

	dependencies := make(map[string]bool) // Use map to avoid duplicates
	node := parseResult.Stmts[0].Stmt

	switch n := node.Node.(type) {
	case *pg_query.Node_InsertStmt:
		// Extract dependencies from SELECT clause (INSERT INTO x SELECT FROM y)
		if n.InsertStmt.SelectStmt != nil {
			extractFromSelectStmt(n.InsertStmt.SelectStmt, dependencies)
		}

	case *pg_query.Node_UpdateStmt:
		// Extract dependencies from FROM clause
		if n.UpdateStmt.FromClause != nil {
			for _, fromItem := range n.UpdateStmt.FromClause {
				extractFromNode(fromItem, dependencies)
			}
		}
		// Extract dependencies from WHERE clause subqueries
		if n.UpdateStmt.WhereClause != nil {
			extractFromWhereClause(n.UpdateStmt.WhereClause, dependencies)
		}

	case *pg_query.Node_DeleteStmt:
		// Extract dependencies from USING clause
		if n.DeleteStmt.UsingClause != nil {
			for _, usingItem := range n.DeleteStmt.UsingClause {
				extractFromNode(usingItem, dependencies)
			}
		}
		// Extract dependencies from WHERE clause subqueries
		if n.DeleteStmt.WhereClause != nil {
			extractFromWhereClause(n.DeleteStmt.WhereClause, dependencies)
		}
	}

	// Convert map to slice
	result := make([]string, 0, len(dependencies))
	for dep := range dependencies {
		result = append(result, dep)
	}

	return result, nil
}

// extractFromSelectStmt extracts table references from a SELECT statement
func extractFromSelectStmt(selectStmt *pg_query.Node, dependencies map[string]bool) {
	if selectStmt == nil {
		return
	}

	switch s := selectStmt.Node.(type) {
	case *pg_query.Node_SelectStmt:
		if s.SelectStmt.FromClause != nil {
			for _, fromItem := range s.SelectStmt.FromClause {
				extractFromNode(fromItem, dependencies)
			}
		}
	}
}

// extractFromNode extracts table name from a FROM clause node
func extractFromNode(node *pg_query.Node, dependencies map[string]bool) {
	if node == nil {
		return
	}

	switch n := node.Node.(type) {
	case *pg_query.Node_RangeVar:
		tableName := extractRelationName(n.RangeVar)
		if tableName != "" {
			dependencies[tableName] = true
		}
	case *pg_query.Node_JoinExpr:
		// Recursively extract from left and right sides of join
		extractFromNode(n.JoinExpr.Larg, dependencies)
		extractFromNode(n.JoinExpr.Rarg, dependencies)
	}
}

// extractFromWhereClause extracts table references from WHERE clause subqueries
func extractFromWhereClause(whereClause *pg_query.Node, dependencies map[string]bool) {
	if whereClause == nil {
		return
	}

	// Simple recursive walker to find SubLinks
	// In a production-grade implementation, this should use a proper AST walker
	// For now, we manually check the top-level structure and common patterns

	switch n := whereClause.Node.(type) {
	case *pg_query.Node_SubLink:
		if n.SubLink.Subselect != nil {
			extractFromSelectStmt(n.SubLink.Subselect, dependencies)
		}
	case *pg_query.Node_BoolExpr:
		for _, arg := range n.BoolExpr.Args {
			extractFromWhereClause(arg, dependencies)
		}
	case *pg_query.Node_AExpr:
		extractFromWhereClause(n.AExpr.Lexpr, dependencies)
		extractFromWhereClause(n.AExpr.Rexpr, dependencies)
	}

	// Note: Deep extraction of all possible expression types types is omitted for brevity
	// but this covers the most common cases of subqueries in WHERE clauses.
}
