package validation

import (
	"strings"

	parserutil "github.com/capysquash/pgsquash-engine/internal/parser"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

type PreferText struct{}

func init() {
	RegisterRule(&PreferText{})
}

func (r *PreferText) Code() string                { return RuleCodeHygienePreferText }
func (r *PreferText) Name() string                { return "Prefer Text Over Varchar" }
func (r *PreferText) Category() ViolationCategory { return CategoryHygiene }

func (r *PreferText) Check(sql string, tree *pg_query.ParseResult) ([]Violation, error) {
	var violations []Violation

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_CreateStmt](tree.GetStmts()) {
		for _, elt := range stmt.Stmt.CreateStmt.GetTableElts() {
			colDef := elt.GetColumnDef()
			if colDef != nil {
				if violation := r.checkColumnDef(sql, colDef, stmt.Start, stmt.End); violation != nil {
					violations = append(violations, *violation)
				}
			}
		}
	}

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_AlterTableStmt](tree.GetStmts()) {
		for _, cmd := range stmt.Stmt.AlterTableStmt.GetCmds() {
			alterTableCmd := cmd.GetAlterTableCmd()
			if alterTableCmd != nil && alterTableCmd.GetSubtype() == pg_query.AlterTableType_AT_AddColumn {
				colDef := alterTableCmd.GetDef().GetColumnDef()
				if colDef != nil {
					if violation := r.checkColumnDef(sql, colDef, stmt.Start, stmt.End); violation != nil {
						violations = append(violations, *violation)
					}
				}
			}
		}
	}

	return violations, nil
}

func (r *PreferText) checkColumnDef(sql string, colDef *pg_query.ColumnDef, stmtStart, stmtEnd int32) *Violation {
	typeName := colDef.GetTypeName()
	if typeName == nil || len(typeName.GetTypmods()) == 0 {
		return nil
	}

	if !isVarcharType(typeName) {
		return nil
	}

	var fix *Fix
	start := typeName.GetLocation()
	if fixStart, fixEnd, ok := findVarcharTypeSpan(sql, start, stmtEnd); ok {
		fix = &Fix{
			Replacement: "TEXT",
			Start:       fixStart,
			End:         fixEnd,
		}
	}

	return &Violation{
		Code:       r.Code(),
		Message:    "Avoid using VARCHAR(n), use TEXT instead.",
		Category:   r.Category(),
		Statement:  colDef.GetColname(),
		StmtStart:  stmtStart,
		StmtEnd:    stmtEnd,
		Suggestion: "Postgres TEXT is preferred over VARCHAR(n): same performance, fewer arbitrary limits.",
		Fix:        fix,
	}
}

func isVarcharType(typeName *pg_query.TypeName) bool {
	names := make([]string, 0, len(typeName.GetNames()))
	for _, name := range typeName.GetNames() {
		val := name.GetString_()
		if val == nil {
			continue
		}
		names = append(names, strings.ToLower(val.GetSval()))
	}

	joined := strings.Join(names, " ")
	return strings.Contains(joined, "varchar") || strings.Contains(joined, "character varying")
}

func findVarcharTypeSpan(sql string, start, stmtEnd int32) (int32, int32, bool) {
	if start < 0 || int(start) >= len(sql) {
		return 0, 0, false
	}

	limit := len(sql)
	if stmtEnd > start && int(stmtEnd) <= len(sql) {
		limit = int(stmtEnd)
	}

	i := skipSQLWhitespace(sql, int(start), limit)
	if i >= limit {
		return 0, 0, false
	}

	word1, next := readSQLWordLower(sql, i, limit)
	if word1 == "" {
		return 0, 0, false
	}

	typeStart := i
	cursor := next

	switch word1 {
	case "varchar":
		// ok
	case "character":
		cursor = skipSQLWhitespace(sql, cursor, limit)
		word2, nextWord := readSQLWordLower(sql, cursor, limit)
		if word2 != "varying" {
			return 0, 0, false
		}
		cursor = nextWord
	default:
		return 0, 0, false
	}

	cursor = skipSQLWhitespace(sql, cursor, limit)
	if cursor >= limit || sql[cursor] != '(' {
		return 0, 0, false
	}

	depth := 0
	for cursor < limit {
		switch sql[cursor] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return int32(typeStart), int32(cursor + 1), true
			}
		}
		cursor++
	}

	return 0, 0, false
}

func skipSQLWhitespace(sql string, i, limit int) int {
	for i < limit {
		switch sql[i] {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			i++
		default:
			return i
		}
	}
	return i
}

func readSQLWordLower(sql string, i, limit int) (string, int) {
	if i >= limit {
		return "", i
	}

	start := i
	for i < limit {
		c := sql[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' {
			i++
			continue
		}
		break
	}

	if i == start {
		return "", i
	}

	return strings.ToLower(sql[start:i]), i
}
