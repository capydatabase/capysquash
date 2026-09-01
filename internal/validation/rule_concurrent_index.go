package validation

import (
	parserutil "github.com/capysquash/pgsquash-engine/internal/parser"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

type RequireConcurrentIndex struct{}

func init() {
	RegisterRule(&RequireConcurrentIndex{})
}

func (r *RequireConcurrentIndex) Code() string                { return RuleCodeSafetyConcurrentIndex }
func (r *RequireConcurrentIndex) Name() string                { return "Require Concurrent Index Creation" }
func (r *RequireConcurrentIndex) Category() ViolationCategory { return CategorySafety }

func (r *RequireConcurrentIndex) Check(sql string, tree *pg_query.ParseResult) ([]Violation, error) {
	var violations []Violation

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_IndexStmt](tree.GetStmts()) {
		if stmt.Stmt.IndexStmt.GetConcurrent() {
			continue
		}

		violations = append(violations, Violation{
			Code:       r.Code(),
			Message:    "Index creation should be CONCURRENTLY to avoid locking the table.",
			Category:   r.Category(),
			Statement:  stmt.Stmt.IndexStmt.GetIdxname(),
			StmtStart:  stmt.Start,
			StmtEnd:    stmt.End,
			Suggestion: "Add CONCURRENTLY keyword: CREATE INDEX CONCURRENTLY ...",
		})
	}

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_DropStmt](tree.GetStmts()) {
		dropStmt := stmt.Stmt.DropStmt
		if dropStmt.GetRemoveType() != pg_query.ObjectType_OBJECT_INDEX || dropStmt.GetConcurrent() {
			continue
		}

		violations = append(violations, Violation{
			Code:       r.Code(),
			Message:    "Index drop should be CONCURRENTLY to avoid locking the table.",
			Category:   r.Category(),
			StmtStart:  stmt.Start,
			StmtEnd:    stmt.End,
			Statement:  "", // Drop stmt might drop multiple objects
			Suggestion: "Add CONCURRENTLY keyword: DROP INDEX CONCURRENTLY ...",
		})
	}

	return violations, nil
}
