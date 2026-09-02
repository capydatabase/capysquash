package validation

import (
	"sort"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// ConstraintValidateFlow enforces NOT VALID -> VALIDATE lifecycle tracking.
//
// Rule: CSQ.SAFETY.CONSTRAINT_VALIDATE_FLOW
// Category: Safety
type ConstraintValidateFlow struct{}

type pendingConstraintValidation struct {
	tableName      string
	constraintName string
	stmtStart      int32
	stmtEnd        int32
}

func init() {
	RegisterRule(&ConstraintValidateFlow{})
}

func (r *ConstraintValidateFlow) Code() string {
	return RuleCodeSafetyConstraintFlow
}

func (r *ConstraintValidateFlow) Name() string {
	return "Constraint NOT VALID must be validated"
}

func (r *ConstraintValidateFlow) Category() ViolationCategory {
	return CategorySafety
}

func (r *ConstraintValidateFlow) Check(sql string, tree *pg_query.ParseResult) ([]Violation, error) {
	pending := make(map[string]pendingConstraintValidation)

	for _, rawStmt := range tree.GetStmts() {
		node := rawStmt.GetStmt().GetNode()

		switch stmt := node.(type) {
		case *pg_query.Node_AlterTableStmt:
			alterStmt := stmt.AlterTableStmt
			if alterStmt == nil {
				continue
			}

			tableName := relationName(alterStmt.Relation)
			for _, cmd := range alterStmt.Cmds {
				alterCmd := cmd.GetAlterTableCmd()
				if alterCmd == nil {
					continue
				}

				switch alterCmd.Subtype {
				case pg_query.AlterTableType_AT_AddConstraint:
					constraint := alterCmd.Def.GetConstraint()
					if constraint == nil || constraint.Conname == "" {
						continue
					}

					if !constraint.SkipValidation {
						continue
					}

					if constraint.Contype != pg_query.ConstrType_CONSTR_CHECK && constraint.Contype != pg_query.ConstrType_CONSTR_FOREIGN {
						continue
					}

					key := constraintKey(tableName, constraint.Conname)
					pending[key] = pendingConstraintValidation{
						tableName:      tableName,
						constraintName: constraint.Conname,
						stmtStart:      rawStmt.StmtLocation,
						stmtEnd:        rawStmt.StmtLocation + rawStmt.StmtLen,
					}

				case pg_query.AlterTableType_AT_ValidateConstraint, pg_query.AlterTableType_AT_DropConstraint:
					if alterCmd.Name == "" {
						continue
					}
					delete(pending, constraintKey(tableName, alterCmd.Name))
				}
			}

		case *pg_query.Node_DropStmt:
			dropStmt := stmt.DropStmt
			if dropStmt == nil || dropStmt.GetRemoveType() != pg_query.ObjectType_OBJECT_TABLE {
				continue
			}

			for _, obj := range dropStmt.Objects {
				tableName := nodeQualifiedName(obj)
				if tableName == "" {
					continue
				}

				for key, tracked := range pending {
					if tracked.tableName == tableName {
						delete(pending, key)
					}
				}
			}
		}
	}

	keys := make([]string, 0, len(pending))
	for key := range pending {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	violations := make([]Violation, 0, len(keys))
	for _, key := range keys {
		tracked := pending[key]
		violations = append(violations, Violation{
			Code:       r.Code(),
			Category:   r.Category(),
			Statement:  tracked.constraintName,
			StmtStart:  tracked.stmtStart,
			StmtEnd:    tracked.stmtEnd,
			Message:    "Constraint '" + tracked.constraintName + "' on table '" + tracked.tableName + "' is added with NOT VALID but never validated.",
			Suggestion: "Add a follow-up statement: ALTER TABLE " + tracked.tableName + " VALIDATE CONSTRAINT " + tracked.constraintName + ";",
		})
	}

	return violations, nil
}
