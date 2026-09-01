package validation

import (
	parserutil "github.com/capysquash/pgsquash-engine/internal/parser"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// BlockMissingWhere checks for DELETE/UPDATE statements without WHERE clauses
//
// Rule: CSQ.SAFETY.MISSING_WHERE
// Category: Safety
type BlockMissingWhere struct{}

func init() {
	RegisterRule(&BlockMissingWhere{})
}

func (r *BlockMissingWhere) Code() string {
	return RuleCodeSafetyMissingWhere
}

func (r *BlockMissingWhere) Name() string {
	return "Block Missing WHERE Clause"
}

func (r *BlockMissingWhere) Category() ViolationCategory {
	return CategorySafety
}

func (r *BlockMissingWhere) Check(sql string, tree *pg_query.ParseResult) ([]Violation, error) {
	var violations []Violation

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_DeleteStmt](tree.GetStmts()) {
		if stmt.Stmt.DeleteStmt.WhereClause != nil {
			continue
		}

		violations = append(violations, Violation{
			Code:       r.Code(),
			Message:    "DELETE statement missing WHERE clause. This will delete ALL rows.",
			Category:   r.Category(),
			StmtStart:  stmt.Start,
			StmtEnd:    stmt.End,
			Suggestion: "Add a WHERE clause or use TRUNCATE if you intend to empty the table.",
			// No automatic fix for this (unsafe to guess)
		})
	}

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_UpdateStmt](tree.GetStmts()) {
		if stmt.Stmt.UpdateStmt.WhereClause != nil {
			continue
		}

		violations = append(violations, Violation{
			Code:       r.Code(),
			Message:    "UPDATE statement missing WHERE clause. This will update ALL rows.",
			Category:   r.Category(),
			StmtStart:  stmt.Start,
			StmtEnd:    stmt.End,
			Suggestion: "Add a WHERE clause.",
		})
	}

	return violations, nil
}
