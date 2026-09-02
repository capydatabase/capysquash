package validation

import (
	"fmt"

	parserutil "github.com/capy-base/pgsquash-engine/internal/parser"
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
								// Fix: Append " NOT VALID" to the statement?
								// Location data for the end of the constraint definition is hard to get.
								// constraint.Location points to the start.
								// For now, no auto-fix.
							})
						}
					}
				}
			}
		}
	}

	return violations, nil
}
