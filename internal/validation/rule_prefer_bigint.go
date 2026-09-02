package validation

import (
	"fmt"
	"strings"

	parserutil "github.com/capy-base/pgsquash-engine/internal/parser"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// PreferBigInt checks for INT/INTEGER columns and suggests BIGINT
//
// Rule: CSQ.HYGIENE.PREFER_BIGINT
// Category: Hygiene
type PreferBigInt struct{}

func init() {
	RegisterRule(&PreferBigInt{})
}

func (r *PreferBigInt) Code() string {
	return RuleCodeHygienePreferBigInt
}

func (r *PreferBigInt) Name() string {
	return "Prefer BIGINT over INT"
}

func (r *PreferBigInt) Category() ViolationCategory {
	return CategoryHygiene
}

func (r *PreferBigInt) Check(sql string, tree *pg_query.ParseResult) ([]Violation, error) {
	var violations []Violation

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_CreateStmt](tree.GetStmts()) {
		createStmt := stmt.Stmt.CreateStmt
		for _, item := range createStmt.TableElts {
			if colDef := item.GetColumnDef(); colDef != nil {
				vio := r.checkColumnDef(sql, colDef, createStmt.Relation.Relname, stmt.Start, stmt.End)
				if vio != nil {
					violations = append(violations, *vio)
				}
			}
		}
	}

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_AlterTableStmt](tree.GetStmts()) {
		alterStmt := stmt.Stmt.AlterTableStmt
		for _, cmd := range alterStmt.Cmds {
			alterCmd := cmd.GetAlterTableCmd()
			if alterCmd.Subtype == pg_query.AlterTableType_AT_AddColumn {
				if colDef := alterCmd.Def.GetColumnDef(); colDef != nil {
					vio := r.checkColumnDef(sql, colDef, alterStmt.Relation.Relname, stmt.Start, stmt.End)
					if vio != nil {
						violations = append(violations, *vio)
					}
				}
			}
			// Also check ALTER COLUMN TYPE?
			// The task only mentioned detecting the type usage.
			// Let's stick to definitions for now.
		}
	}

	return violations, nil
}

func (r *PreferBigInt) checkColumnDef(sql string, colDef *pg_query.ColumnDef, tableName string, stmtStart, stmtEnd int32) *Violation {
	typeName := colDef.TypeName
	if typeName == nil {
		return nil
	}

	// Check for "int", "integer", "int4"
	// pg_query_go usually puts the type name in Names[last].String.Str
	if len(typeName.Names) == 0 {
		return nil
	}

	// Usually standard types are in the last element of Names
	// e.g. "pg_catalog"."int4" -> Names has 2 elements
	// or just "int" -> Names has 1 element

	lastNode := typeName.Names[len(typeName.Names)-1]
	strVal := lastNode.GetString_()
	if strVal == nil {
		return nil
	}

	typ := strings.ToLower(strVal.Sval)

	if typ == "int" || typ == "integer" || typ == "int4" {
		// Found violation
		start := typeName.Location

		// Determine exact length from SQL
		// The parser might normalize the type name (e.g. "integer" -> "int4" or vice versa in weird cases),
		// but typically valid SQL will match one of the keywords.
		// We need to look at what's actually in the string at 'start'.

		var end int32
		if int(start) < len(sql) {
			suffix := sql[start:]
			lowerSuffix := strings.ToLower(suffix)

			if strings.HasPrefix(lowerSuffix, "integer") {
				end = start + 7
			} else if strings.HasPrefix(lowerSuffix, "int4") {
				end = start + 4
			} else if strings.HasPrefix(lowerSuffix, "int") {
				end = start + 3
			} else {
				// Fallback: If we can't match it (maybe comments?), don't suggest fix.
				// Or rely on typ length if it matches one of known.
				end = start + int32(len(typ))
			}
		} else {
			end = start + int32(len(typ))
		}

		fix := &Fix{
			Replacement: "BIGINT",
			Start:       start,
			End:         end,
		}

		return &Violation{
			Code:       r.Code(),
			Message:    fmt.Sprintf("Column '%s' in table '%s' uses %s. Prefer BIGINT to avoid integer overflow.", colDef.Colname, tableName, strings.ToUpper(typ)),
			Category:   r.Category(),
			StmtStart:  stmtStart,
			StmtEnd:    stmtEnd,
			Statement:  "", // filled by caller if needed
			Suggestion: "Use BIGINT instead of INT/INTEGER",
			Fix:        fix,
		}
	}

	return nil
}
