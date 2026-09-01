package validation

import (
	parserutil "github.com/capysquash/pgsquash-engine/internal/parser"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

type BanRenameTable struct{}

func init() {
	RegisterRule(&BanRenameTable{})
}

func (r *BanRenameTable) Code() string                { return RuleCodeBreakingRenameTable }
func (r *BanRenameTable) Name() string                { return "Ban Rename Table" }
func (r *BanRenameTable) Category() ViolationCategory { return CategoryBreaking }

func (r *BanRenameTable) Check(sql string, tree *pg_query.ParseResult) ([]Violation, error) {
	violations := make([]Violation, 0)

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_RenameStmt](tree.GetStmts()) {
		renameStmt := stmt.Stmt.RenameStmt
		if renameStmt == nil || renameStmt.RenameType != pg_query.ObjectType_OBJECT_TABLE {
			continue
		}

		tableName := relationName(renameStmt.Relation)
		newName := renameStmt.Newname

		message := "Renaming a table is a breaking change."
		if tableName != "" && newName != "" {
			message = "Renaming table '" + tableName + "' to '" + newName + "' is a breaking change."
		}

		violations = append(violations, Violation{
			Code:       r.Code(),
			Message:    message,
			Category:   r.Category(),
			Statement:  tableName,
			StmtStart:  stmt.Start,
			StmtEnd:    stmt.End,
			Suggestion: "Create a compatibility layer (view/synonym) or coordinate application rollout before renaming tables.",
		})
	}

	return violations, nil
}
