package validation

import (
	parserutil "github.com/capysquash/pgsquash-engine/internal/parser"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

type BanDropTable struct{}

func init() {
	RegisterRule(&BanDropTable{})
}

func (r *BanDropTable) Code() string                { return RuleCodeBreakingDropTable }
func (r *BanDropTable) Name() string                { return "Ban Drop Table" }
func (r *BanDropTable) Category() ViolationCategory { return CategoryBreaking }

func (r *BanDropTable) Check(sql string, tree *pg_query.ParseResult) ([]Violation, error) {
	violations := make([]Violation, 0)

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_DropStmt](tree.GetStmts()) {
		dropStmt := stmt.Stmt.DropStmt
		if dropStmt == nil || dropStmt.GetRemoveType() != pg_query.ObjectType_OBJECT_TABLE {
			continue
		}

		if len(dropStmt.Objects) == 0 {
			violations = append(violations, Violation{
				Code:       r.Code(),
				Message:    "Dropping a table is a breaking change.",
				Category:   r.Category(),
				StmtStart:  stmt.Start,
				StmtEnd:    stmt.End,
				Suggestion: "Use staged deprecation (rename/archive/backfill) before permanently dropping a table.",
			})
			continue
		}

		for _, obj := range dropStmt.Objects {
			tableName := nodeQualifiedName(obj)
			message := "Dropping a table is a breaking change."
			if tableName != "" {
				message = "Dropping table '" + tableName + "' is a breaking change."
			}

			violations = append(violations, Violation{
				Code:       r.Code(),
				Message:    message,
				Category:   r.Category(),
				Statement:  tableName,
				StmtStart:  stmt.Start,
				StmtEnd:    stmt.End,
				Suggestion: "Use staged deprecation (rename/archive/backfill) before permanently dropping a table.",
			})
		}
	}

	return violations, nil
}
