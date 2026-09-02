package validation

import (
	"strings"

	parserutil "github.com/capy-base/pgsquash-engine/internal/parser"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

type BanTypeChange struct{}

func init() {
	RegisterRule(&BanTypeChange{})
}

func (r *BanTypeChange) Code() string                { return RuleCodeBreakingTypeChange }
func (r *BanTypeChange) Name() string                { return "Ban Type Change" }
func (r *BanTypeChange) Category() ViolationCategory { return CategoryBreaking }

func (r *BanTypeChange) Check(sql string, tree *pg_query.ParseResult) ([]Violation, error) {
	violations := make([]Violation, 0)

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_AlterTableStmt](tree.GetStmts()) {
		alterStmt := stmt.Stmt.AlterTableStmt
		if alterStmt == nil {
			continue
		}

		tableName := relationName(alterStmt.Relation)

		for _, cmd := range alterStmt.Cmds {
			alterCmd := cmd.GetAlterTableCmd()
			if alterCmd == nil || alterCmd.Subtype != pg_query.AlterTableType_AT_AlterColumnType {
				continue
			}

			columnName := alterCmd.Name
			typeName := extractAlterColumnTypeName(alterCmd)

			message := "Changing a column type can be a breaking change."
			if tableName != "" && columnName != "" && typeName != "" {
				message = "Changing column type for '" + tableName + "." + columnName + "' to '" + typeName + "' can be a breaking change."
			}

			violations = append(violations, Violation{
				Code:       r.Code(),
				Message:    message,
				Category:   r.Category(),
				Statement:  columnName,
				StmtStart:  stmt.Start,
				StmtEnd:    stmt.End,
				Suggestion: "Use a phased migration strategy (new column, backfill, dual writes, cutover) when changing types.",
			})
		}
	}

	return violations, nil
}

func extractAlterColumnTypeName(alterCmd *pg_query.AlterTableCmd) string {
	if alterCmd == nil || alterCmd.Def == nil {
		return ""
	}

	if typeName := alterCmd.Def.GetTypeName(); typeName != nil {
		return stringifyTypeName(typeName)
	}

	if colDef := alterCmd.Def.GetColumnDef(); colDef != nil && colDef.TypeName != nil {
		return stringifyTypeName(colDef.TypeName)
	}

	return ""
}

func stringifyTypeName(typeName *pg_query.TypeName) string {
	if typeName == nil {
		return ""
	}

	parts := make([]string, 0, len(typeName.Names))
	for _, node := range typeName.Names {
		if str := node.GetString_(); str != nil {
			parts = append(parts, str.Sval)
		}
	}

	return strings.Join(parts, ".")
}
