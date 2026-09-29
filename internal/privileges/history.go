package privileges

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/capydatabase/capysquash/internal/types"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// History collects a migration history's statements for the privilege
// model. The statements it owns - GRANT/REVOKE on objects, ALTER DEFAULT
// PRIVILEGES, ownership changes of relations, routines, schemas and types,
// CREATE ROLE and role membership - are taken out of the consolidation
// pipeline, which would merge or reorder them away from the objects they
// act on; the baseline gets them back from RolesSQL and PrivilegesSQL.
type History struct {
	statements []*pg_query.Node // every schema statement, in history order
	roles      []*pg_query.Node // CREATE ROLE
	membership []*pg_query.Node // GRANT/REVOKE role TO role
	forRoles   []string         // role names ALTER DEFAULT PRIVILEGES FOR ROLE mentions
	roleNames  map[string]bool  // every role a consumed statement names
}

// NewHistory returns an empty history.
func NewHistory() *History {
	return &History{roleNames: map[string]bool{}}
}

func statementNode(stmt types.Statement) *pg_query.Node {
	if stmt.ParseTree == nil || len(stmt.ParseTree.GetStmts()) == 0 {
		return nil
	}
	return stmt.ParseTree.GetStmts()[0].GetStmt()
}

// ownerOnlyAlterTable reports an ALTER TABLE/VIEW/SEQUENCE/... that only
// changes the owner.
func ownerOnlyAlterTable(stmt *pg_query.AlterTableStmt) bool {
	switch stmt.GetObjtype() {
	case pg_query.ObjectType_OBJECT_TABLE, pg_query.ObjectType_OBJECT_VIEW, pg_query.ObjectType_OBJECT_MATVIEW,
		pg_query.ObjectType_OBJECT_FOREIGN_TABLE, pg_query.ObjectType_OBJECT_SEQUENCE:
	default:
		return false
	}
	if len(stmt.GetCmds()) == 0 {
		return false
	}
	for _, cmd := range stmt.GetCmds() {
		if cmd.GetAlterTableCmd().GetSubtype() != pg_query.AlterTableType_AT_ChangeOwner {
			return false
		}
	}
	return true
}

// ownsAlterOwner reports the ALTER ... OWNER TO statements the model handles.
func ownsAlterOwner(stmt *pg_query.AlterOwnerStmt) bool {
	switch stmt.GetObjectType() {
	case pg_query.ObjectType_OBJECT_FUNCTION, pg_query.ObjectType_OBJECT_PROCEDURE, pg_query.ObjectType_OBJECT_ROUTINE,
		pg_query.ObjectType_OBJECT_SCHEMA, pg_query.ObjectType_OBJECT_TYPE, pg_query.ObjectType_OBJECT_DOMAIN:
		return true
	}
	return false
}

// Owns reports whether a statement belongs to the privilege model instead
// of the consolidation pipeline. SET ROLE and SET SESSION AUTHORIZATION are
// among them: the model follows the role each statement runs as, and the
// baseline writes the owners and grantors that follow from it out
// explicitly instead of switching roles between object definitions.
func Owns(node *pg_query.Node) bool {
	switch n := node.GetNode().(type) {
	case *pg_query.Node_GrantStmt, *pg_query.Node_GrantRoleStmt, *pg_query.Node_AlterDefaultPrivilegesStmt,
		*pg_query.Node_CreateRoleStmt:
		return true
	case *pg_query.Node_VariableSetStmt:
		switch strings.ToLower(n.VariableSetStmt.GetName()) {
		case "role", "session_authorization":
			return true
		}
	case *pg_query.Node_AlterOwnerStmt:
		return ownsAlterOwner(n.AlterOwnerStmt)
	case *pg_query.Node_AlterTableStmt:
		return ownerOnlyAlterTable(n.AlterTableStmt)
	}
	return false
}

func (h *History) noteRole(spec *pg_query.RoleSpec) {
	if spec != nil && spec.GetRoletype() == pg_query.RoleSpecType_ROLESPEC_CSTRING {
		h.roleNames[spec.GetRolename()] = true
	}
}

// Filter returns the statements the consolidation pipeline handles: all but
// the ones the privilege model owns.
func Filter(statements []types.Statement) []types.Statement {
	kept := make([]types.Statement, 0, len(statements))
	for _, stmt := range statements {
		if node := statementNode(stmt); node == nil || !Owns(node) {
			kept = append(kept, stmt)
		}
	}
	return kept
}

// Record reads one migration's statements in order and returns the ones
// the consolidation pipeline still handles.
func (h *History) Record(statements []types.Statement) []types.Statement {
	kept := make([]types.Statement, 0, len(statements))
	for _, stmt := range statements {
		node := statementNode(stmt)
		if node == nil {
			kept = append(kept, stmt)
			continue
		}
		if !stmt.IsDataOp {
			h.statements = append(h.statements, node)
		}
		switch n := node.GetNode().(type) {
		case *pg_query.Node_CreateRoleStmt:
			h.roles = append(h.roles, node)
		case *pg_query.Node_GrantRoleStmt:
			h.membership = append(h.membership, node)
		case *pg_query.Node_GrantStmt:
			for _, g := range n.GrantStmt.GetGrantees() {
				h.noteRole(g.GetRoleSpec())
			}
		case *pg_query.Node_AlterDefaultPrivilegesStmt:
			for _, g := range n.AlterDefaultPrivilegesStmt.GetAction().GetGrantees() {
				h.noteRole(g.GetRoleSpec())
			}
			for _, option := range n.AlterDefaultPrivilegesStmt.GetOptions() {
				if option.GetDefElem().GetDefname() != "roles" {
					continue
				}
				for _, item := range option.GetDefElem().GetArg().GetList().GetItems() {
					spec := item.GetRoleSpec()
					if spec.GetRoletype() == pg_query.RoleSpecType_ROLESPEC_CSTRING && !slices.Contains(h.forRoles, spec.GetRolename()) {
						h.forRoles = append(h.forRoles, spec.GetRolename())
					}
				}
			}
		}
		if Owns(node) {
			continue
		}
		kept = append(kept, stmt)
	}
	return kept
}

// ReferencesRole reports whether a statement the history owns grants to or
// names the role.
func (h *History) ReferencesRole(name string) bool {
	return h.roleNames[name]
}

// dollarQuote picks a dollar-quote tag the body does not contain.
func dollarQuote(body string) string {
	tag := "$capysquash$"
	for i := 1; strings.Contains(body, tag); i++ {
		tag = fmt.Sprintf("$capysquash%d$", i)
	}
	return tag
}

// RolesSQL returns the ROLES section: every CREATE ROLE of the history,
// guarded because roles belong to the whole cluster and a baseline is
// applied to new databases in clusters that often have them already, then
// every GRANT/REVOKE of role membership in history order.
func (h *History) RolesSQL() (string, error) {
	var sb strings.Builder
	for _, node := range h.roles {
		create, err := deparse(node)
		if err != nil {
			return "", fmt.Errorf("render CREATE ROLE: %w", err)
		}
		name := node.GetCreateRoleStmt().GetRole()
		body := fmt.Sprintf("\nBEGIN\n  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = %s) THEN\n    %s;\n  END IF;\nEND\n", quoteLiteral(name), create)
		tag := dollarQuote(body)
		fmt.Fprintf(&sb, "DO %s%s%s;\n\n", tag, body, tag)
	}
	for _, node := range h.membership {
		statement, err := deparse(node)
		if err != nil {
			return "", fmt.Errorf("render role membership: %w", err)
		}
		sb.WriteString(statement + ";\n\n")
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// PrivilegesSQL returns the PRIVILEGES section for a baseline: ownership,
// privileges and default privileges as the history leaves them, for the
// objects the baseline creates. It is meant to run after every statement
// of the baseline. The warnings name what the section cannot reproduce.
func (h *History) PrivilegesSQL(baselineSQL string) (string, []string, error) {
	parsed, err := pg_query.Parse(baselineSQL)
	if err != nil {
		return "", nil, fmt.Errorf("parse the baseline to find the objects it creates: %w", err)
	}
	baseline := newModel("")
	for _, raw := range parsed.GetStmts() {
		baseline.apply(raw.GetStmt())
	}
	alive := func(obj *object) bool {
		if obj.kind == kindSchema {
			return baseline.findSchema(obj.name) != nil
		}
		return baseline.objects[obj.key()] != nil
	}

	hypotheses := append([]string{""}, h.forRoles...)
	renders := make([][]item, len(hypotheses))
	var warnings []string
	for i, self := range hypotheses {
		m := newModel(self)
		for _, node := range h.statements {
			m.apply(node)
		}
		renders[i] = m.render(alive)
		if i == 0 {
			warnings = m.warnings
		}
	}

	var unconditional []string
	conditional := make([][]string, len(hypotheses))
	for _, id := range itemOrder(renders) {
		texts := make([][]string, len(hypotheses))
		for i, items := range renders {
			texts[i] = findItem(items, id)
		}
		same := true
		for i := 1; i < len(texts); i++ {
			if strings.Join(texts[i], "\n") != strings.Join(texts[0], "\n") {
				same = false
				break
			}
		}
		if same {
			unconditional = append(unconditional, texts[0]...)
			continue
		}
		for i := range texts {
			conditional[i] = append(conditional[i], texts[i]...)
		}
	}

	var sb strings.Builder
	for _, statement := range unconditional {
		sb.WriteString(statement + ";\n")
	}
	if hasStatements(conditional) {
		sb.WriteString(conditionalBlock(hypotheses, conditional))
	}
	sort.Strings(warnings)
	return strings.TrimRight(sb.String(), "\n"), warnings, nil
}

func hasStatements(groups [][]string) bool {
	for _, group := range groups {
		if len(group) > 0 {
			return true
		}
	}
	return false
}

// conditionalBlock renders the statements that depend on which role runs
// the baseline: ALTER DEFAULT PRIVILEGES FOR ROLE r reaches the history's
// objects only when r is the role that created them.
func conditionalBlock(hypotheses []string, statements [][]string) string {
	var body strings.Builder
	body.WriteString("\nBEGIN\n")
	for i := 1; i < len(hypotheses); i++ {
		keyword := "ELSIF"
		if i == 1 {
			keyword = "IF"
		}
		fmt.Fprintf(&body, "  %s current_user = %s THEN\n", keyword, quoteLiteral(hypotheses[i]))
		writeBranch(&body, statements[i])
	}
	body.WriteString("  ELSE\n")
	writeBranch(&body, statements[0])
	body.WriteString("  END IF;\nEND\n")
	tag := dollarQuote(body.String())
	names := slices.Clone(hypotheses[1:])
	return fmt.Sprintf("\n-- ALTER DEFAULT PRIVILEGES FOR ROLE %s applies to the objects above only when\n-- that role runs the migrations, so these privileges depend on who applies the baseline.\nDO %s%s%s;\n",
		strings.Join(names, ", "), tag, body.String(), tag)
}

func writeBranch(body *strings.Builder, statements []string) {
	if len(statements) == 0 {
		body.WriteString("    NULL;\n")
		return
	}
	for _, statement := range statements {
		body.WriteString("    " + statement + ";\n")
	}
}

// itemOrder lists item ids in first-seen order across the renders.
func itemOrder(renders [][]item) []string {
	seen := map[string]bool{}
	var order []string
	for _, items := range renders {
		for _, it := range items {
			if !seen[it.id] {
				seen[it.id] = true
				order = append(order, it.id)
			}
		}
	}
	return order
}

func findItem(items []item, id string) []string {
	for _, it := range items {
		if it.id == id {
			return it.statements
		}
	}
	return nil
}
