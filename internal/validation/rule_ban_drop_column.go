package validation

import (
	parserutil "github.com/capy-base/pgsquash-engine/internal/parser"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

type BanDropColumn struct{}

func init() {
	RegisterRule(&BanDropColumn{})
}

func (r *BanDropColumn) Code() string                { return RuleCodeBreakingDropColumn }
func (r *BanDropColumn) Name() string                { return "Ban Drop Column" }
func (r *BanDropColumn) Category() ViolationCategory { return CategoryBreaking }

func (r *BanDropColumn) Check(sql string, tree *pg_query.ParseResult) ([]Violation, error) {
	var violations []Violation

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_AlterTableStmt](tree.GetStmts()) {
		for _, cmd := range stmt.Stmt.AlterTableStmt.GetCmds() {
			alterTableCmd := cmd.GetAlterTableCmd()
			if alterTableCmd == nil || alterTableCmd.GetSubtype() != pg_query.AlterTableType_AT_DropColumn {
				continue
			}

			violations = append(violations, Violation{
				Code:       r.Code(),
				Message:    "Dropping a column is a breaking change.",
				Category:   r.Category(),
				Statement:  alterTableCmd.GetName(), // This gives column name
				StmtStart:  stmt.Start,
				StmtEnd:    stmt.End,
				Suggestion: "Rename the column instead or perform a multi-step migration (add new -> backfill -> ignore old -> drop old).",
			})
		}
	}

	return violations, nil
}
