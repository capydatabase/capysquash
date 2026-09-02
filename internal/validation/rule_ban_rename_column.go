package validation

import (
	parserutil "github.com/capysquash/pgsquash-engine/internal/parser"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

type BanRenameColumn struct{}

func init() {
	RegisterRule(&BanRenameColumn{})
}

func (r *BanRenameColumn) Code() string                { return RuleCodeBreakingRenameColumn }
func (r *BanRenameColumn) Name() string                { return "Ban Rename Column" }
func (r *BanRenameColumn) Category() ViolationCategory { return CategoryBreaking }

func (r *BanRenameColumn) Check(sql string, tree *pg_query.ParseResult) ([]Violation, error) {
	violations := make([]Violation, 0)

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_RenameStmt](tree.GetStmts()) {
		renameStmt := stmt.Stmt.RenameStmt
		if renameStmt == nil || renameStmt.RenameType != pg_query.ObjectType_OBJECT_COLUMN {
			continue
		}

		tableName := relationName(renameStmt.Relation)
		oldName := renameStmt.Subname
		newName := renameStmt.Newname

		message := "Renaming a column is a breaking change."
		if tableName != "" && oldName != "" && newName != "" {
			message = "Renaming column '" + tableName + "." + oldName + "' to '" + newName + "' is a breaking change."
		}

		violations = append(violations, Violation{
			Code:       r.Code(),
			Message:    message,
			Category:   r.Category(),
			Statement:  oldName,
			StmtStart:  stmt.Start,
			StmtEnd:    stmt.End,
			Suggestion: "Prefer additive migrations (new column + backfill + dual-read/write) before removing legacy columns.",
		})
	}

	return violations, nil
}
