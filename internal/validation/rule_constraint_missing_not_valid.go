package validation

import (
	"fmt"

	parserutil "github.com/capydatabase/capysquash/internal/parser"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// ConstraintMissingNotValid checks for ADD CONSTRAINT without NOT VALID
//
// Rule: CSQ.SAFETY.CONSTRAINT_NOT_VALID
// Category: Safety
type ConstraintMissingNotValid struct{}

func init() {
	RegisterRule(&ConstraintMissingNotValid{})
}

func (r *ConstraintMissingNotValid) Code() string {
	return RuleCodeSafetyConstraintNotValid
}

func (r *ConstraintMissingNotValid) Name() string {
	return "Constraint Missing NOT VALID"
}

func (r *ConstraintMissingNotValid) Category() ViolationCategory {
	return CategorySafety
}

func (r *ConstraintMissingNotValid) Check(sql string, tree *pg_query.ParseResult) ([]Violation, error) {
	var violations []Violation

	for _, stmt := range parserutil.FilterStatements[*pg_query.Node_AlterTableStmt](tree.GetStmts()) {
		alterStmt := stmt.Stmt.AlterTableStmt
		for _, cmd := range alterStmt.Cmds {
			alterCmd := cmd.GetAlterTableCmd()

			// We care about ADD_CONSTRAINT (AT_AddConstraint)
			if alterCmd.Subtype == pg_query.AlterTableType_AT_AddConstraint {
				// Check if SkipValidation is false (NOT VALID sets it to true)
				// Wait, logic inversion:
				// If "NOT VALID" is specified, `SkipValidation` is true.
				// We want to flag if `SkipValidation` is FALSE.

				constraint := alterCmd.Def.GetConstraint()
				if constraint != nil {
					// Check constraint type.
					// PRIMARY KEY, UNIQUE, FOREIGN KEY, CHECK.
					// PK and UNIQUE always require full scan/index build (though UNIQUE can use CONCURRENTLY index).
					// FK and CHECK support NOT VALID.

					ctype := constraint.Contype
					if ctype == pg_query.ConstrType_CONSTR_CHECK || ctype == pg_query.ConstrType_CONSTR_FOREIGN {
						if !constraint.SkipValidation {
							violations = append(violations, Violation{
								Code:       r.Code(),
								Message:    fmt.Sprintf("Constraint '%s' on table '%s' added without NOT VALID. This will lock the table while validating existing rows.", constraint.Conname, alterStmt.Relation.Relname),
								Category:   r.Category(),
								StmtStart:  stmt.Start,
								StmtEnd:    stmt.End,
								Suggestion: "Add 'NOT VALID' to the constraint, then validate it in a separate transaction.",
								Fix:        notValidFix(sql, constraint.Location, stmt.Start, stmt.End),
							})
						}
					}
				}
			}
		}
	}

	return violations, nil
}

// notValidFix inserts " NOT VALID" right after the constraint definition that
// starts at constraintLoc: after its last token before the next top-level ","
// (the next ALTER TABLE command) or the end of the statement. Tokens come from
// the pg_query scanner, so parentheses, strings and comments inside the
// definition are handled. It returns nil when the end cannot be located.
func notValidFix(sql string, constraintLoc, stmtStart, stmtEnd int32) *Fix {
	if stmtEnd <= stmtStart {
		// The parser reports a zero length for a final statement without ";".
		stmtEnd = int32(len(sql))
	}
	if constraintLoc < stmtStart || constraintLoc >= stmtEnd || int(stmtEnd) > len(sql) {
		return nil
	}

	scan, err := pg_query.Scan(sql[stmtStart:stmtEnd])
	if err != nil {
		return nil
	}

	depth := 0
	insertAt := int32(-1)
	for _, tok := range scan.GetTokens() {
		start := stmtStart + tok.GetStart()
		if start < constraintLoc {
			continue
		}
		switch tok.GetToken() {
		case pg_query.Token_SQL_COMMENT, pg_query.Token_C_COMMENT:
			continue
		case pg_query.Token_ASCII_40: // (
			depth++
		case pg_query.Token_ASCII_41: // )
			depth--
		case pg_query.Token_ASCII_44, pg_query.Token_ASCII_59: // , ;
			if depth == 0 {
				return insertionFix(insertAt)
			}
		}
		insertAt = stmtStart + tok.GetEnd()
	}
	return insertionFix(insertAt)
}

func insertionFix(at int32) *Fix {
	if at < 0 {
		return nil
	}
	return &Fix{Replacement: " NOT VALID", Start: at, End: at}
}
