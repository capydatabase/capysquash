package privileges

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// item is one unit of the PRIVILEGES section: the statements for one object,
// one replayed statement or one default-privilege entry. Items with the same
// id are compared across role hypotheses.
type item struct {
	id         string
	statements []string
}

// roleSQL renders a role token.
func (m *model) roleSQL(token string) string {
	switch token {
	case rolePublic:
		return "PUBLIC"
	case roleMigrator:
		if m.self != "" {
			return quoteIdent(m.self)
		}
		return "CURRENT_USER"
	default:
		return quoteIdent(token)
	}
}

// op is one GRANT or REVOKE for one grantee. privileges nil means ALL.
type op struct {
	revoke      bool
	grantOption bool // WITH GRANT OPTION / REVOKE GRANT OPTION FOR
	privileges  []string
}

// diffPrivileges returns the GRANT/REVOKE steps that take a grantee from
// what it holds (current) to what it should hold (want). A step that would
// have to name MAINTAIN goes through ALL instead, because MAINTAIN only
// exists from PostgreSQL 17 while ALL works everywhere.
func diffPrivileges(class aclClass, current, want privSet) []op {
	universe := class.universe()
	cur := privSet{}
	for p, option := range current {
		cur[p] = option
	}
	var ops []op
	has := func(set privSet, p string) bool { _, ok := set[p]; return ok }

	var remove []string
	for _, p := range universe {
		if has(cur, p) && !has(want, p) {
			remove = append(remove, p)
		}
	}
	if len(remove) > 0 {
		if slices.Contains(remove, privMaintain) {
			ops = append(ops, op{revoke: true})
			cur = privSet{}
		} else {
			ops = append(ops, op{revoke: true, privileges: remove})
			for _, p := range remove {
				delete(cur, p)
			}
		}
	}

	var dropOption []string
	for _, p := range universe {
		if cur[p] && has(want, p) && !want[p] {
			dropOption = append(dropOption, p)
		}
	}
	if len(dropOption) > 0 {
		if slices.Contains(dropOption, privMaintain) {
			ops = append(ops, op{revoke: true, grantOption: true})
			for p := range cur {
				cur[p] = false
			}
		} else {
			ops = append(ops, op{revoke: true, grantOption: true, privileges: dropOption})
			for _, p := range dropOption {
				cur[p] = false
			}
		}
	}

	if has(want, privMaintain) && (!has(cur, privMaintain) || want[privMaintain] && !cur[privMaintain]) {
		withOption := want[privMaintain]
		ops = append(ops, op{grantOption: withOption})
		for _, p := range universe {
			cur[p] = cur[p] || withOption
		}
		var extra, noOption []string
		for _, p := range universe {
			switch {
			case !has(want, p):
				extra = append(extra, p)
			case withOption && !want[p]:
				noOption = append(noOption, p)
			}
		}
		if len(extra) > 0 {
			ops = append(ops, op{revoke: true, privileges: extra})
			for _, p := range extra {
				delete(cur, p)
			}
		}
		if len(noOption) > 0 {
			ops = append(ops, op{revoke: true, grantOption: true, privileges: noOption})
			for _, p := range noOption {
				cur[p] = false
			}
		}
	}

	var add, addOption []string
	for _, p := range universe {
		if !has(want, p) {
			continue
		}
		switch {
		case want[p] && !cur[p]:
			addOption = append(addOption, p)
		case !has(cur, p):
			add = append(add, p)
		}
	}
	if len(add) > 0 {
		ops = append(ops, op{privileges: add})
	}
	if len(addOption) > 0 {
		ops = append(ops, op{grantOption: true, privileges: addOption})
	}
	return ops
}

// privilegeList renders an op's privileges, each followed by column when set.
func privilegeList(class aclClass, privileges []string, column string) string {
	suffix := ""
	if column != "" {
		suffix = " (" + quoteIdent(column) + ")"
	}
	if privileges == nil || len(privileges) == len(class.universe()) {
		return "ALL" + suffix
	}
	parts := make([]string, len(privileges))
	for i, p := range privileges {
		parts[i] = p + suffix
	}
	return strings.Join(parts, ", ")
}

func renderOp(o op, privileges, target, grantees, prefix string) string {
	if o.revoke {
		option := ""
		if o.grantOption {
			option = "GRANT OPTION FOR "
		}
		return fmt.Sprintf("%sREVOKE %s%s ON %s FROM %s", prefix, option, privileges, target, grantees)
	}
	statement := fmt.Sprintf("%sGRANT %s ON %s TO %s", prefix, privileges, target, grantees)
	if o.grantOption {
		statement += " WITH GRANT OPTION"
	}
	return statement
}

// aclStatements renders the steps from one ACL to another. Grantees that
// need the same steps share statements.
func (m *model) aclStatements(class aclClass, from, to acl, target, column, prefix string) []string {
	grantees := map[string]bool{}
	for g := range from {
		grantees[g] = true
	}
	for g := range to {
		grantees[g] = true
	}
	type group struct {
		ops      []op
		grantees []string
	}
	var groups []*group
	bySignature := map[string]*group{}
	names := make([]string, 0, len(grantees))
	for g := range grantees {
		names = append(names, g)
	}
	sort.Strings(names)
	for _, g := range names {
		ops := diffPrivileges(class, from[g], to[g])
		if len(ops) == 0 {
			continue
		}
		signature := fmt.Sprintf("%v", ops)
		if existing, ok := bySignature[signature]; ok {
			existing.grantees = append(existing.grantees, m.roleSQL(g))
			continue
		}
		grp := &group{ops: ops, grantees: []string{m.roleSQL(g)}}
		bySignature[signature] = grp
		groups = append(groups, grp)
	}
	var statements []string
	for _, grp := range groups {
		for _, o := range grp.ops {
			statements = append(statements, renderOp(o, privilegeList(class, o.privileges, column), target, strings.Join(grp.grantees, ", "), prefix))
		}
	}
	return statements
}

// renderBulk renders GRANT/REVOKE ... ON ALL ... IN SCHEMA with the schemas'
// final names; it returns "" when every schema was dropped.
func renderBulk(b *bulkGrant) string {
	var schemas []string
	for _, ref := range b.schemas {
		if name, ok := ref.name(); ok {
			schemas = append(schemas, quoteIdent(name))
		}
	}
	if len(schemas) == 0 {
		return ""
	}
	objects := map[pg_query.ObjectType]string{
		pg_query.ObjectType_OBJECT_TABLE:     "TABLES",
		pg_query.ObjectType_OBJECT_SEQUENCE:  "SEQUENCES",
		pg_query.ObjectType_OBJECT_FUNCTION:  "FUNCTIONS",
		pg_query.ObjectType_OBJECT_PROCEDURE: "PROCEDURES",
		pg_query.ObjectType_OBJECT_ROUTINE:   "ROUTINES",
	}[b.objtype]
	privileges := "ALL"
	if len(b.privileges) > 0 {
		privileges = strings.Join(b.privileges, ", ")
	}
	grantees := make([]string, len(b.grantees))
	for i, g := range b.grantees {
		grantees[i] = g.spelled
	}
	target := fmt.Sprintf("ALL %s IN SCHEMA %s", objects, strings.Join(schemas, ", "))
	if b.isGrant {
		statement := fmt.Sprintf("GRANT %s ON %s TO %s", privileges, target, strings.Join(grantees, ", "))
		if b.grantOption {
			statement += " WITH GRANT OPTION"
		}
		return statement
	}
	option := ""
	if b.grantOption {
		option = "GRANT OPTION FOR "
	}
	statement := fmt.Sprintf("REVOKE %s%s ON %s FROM %s", option, privileges, target, strings.Join(grantees, ", "))
	if b.cascade {
		statement += " CASCADE"
	}
	return statement
}

// asRole runs statements as the role a history ran them as, when that is
// not the role running the baseline.
func asRole(role grantee, statements []string) []string {
	if role.token == roleMigrator || len(statements) == 0 {
		return statements
	}
	out := make([]string, 0, len(statements)+2)
	out = append(out, "SET ROLE "+role.spelled)
	out = append(out, statements...)
	return append(out, "RESET ROLE")
}

// stateAfterReplays is what an object holds once the baseline has created
// it and replayed the bulk statements: its built-in default, changed by every
// ON ALL ... IN SCHEMA statement that reaches it and grants as its owner
// (one run as another role is written out with that role's grants).
func (m *model) stateAfterReplays(obj *object) acl {
	state := defaultACL(obj.class(), obj.owner)
	for _, r := range m.replays {
		b := r.bulk
		if b == nil || !inBulkScope(b.objtype, obj) {
			continue
		}
		if role := r.role.token; role != roleMigrator && role != obj.owner && !m.superusers[role] && !m.memberOf(role, obj.owner) {
			continue
		}
		for _, ref := range b.schemas {
			name, ok := ref.name()
			if !ok || name != obj.schema {
				continue
			}
			privs := normalizePrivileges(obj.class(), b.privileges)
			for _, g := range b.grantees {
				if b.isGrant {
					state.grant(g.token, privs, b.grantOption)
				} else {
					state.revoke(g.token, privs, b.grantOption)
				}
			}
			break
		}
	}
	return state
}

// render produces the PRIVILEGES section items for the objects the baseline
// creates (alive reports which), in four parts: ownership, replayed
// statements, per-object privileges, default privileges.
func (m *model) render(alive func(*object) bool) []item {
	objects := make([]*object, 0, len(m.all))
	for _, obj := range m.all {
		if obj.dropped {
			continue
		}
		changed := obj.ownerSpelled != "" || obj.acl != nil || len(obj.columns) > 0
		if !alive(obj) {
			if changed {
				m.warn("privileges of %s are not in the baseline: the baseline does not create it", obj.display())
			}
			continue
		}
		objects = append(objects, obj)
	}
	sort.SliceStable(objects, func(i, j int) bool { return objects[i].created < objects[j].created })

	var items []item
	for _, obj := range objects {
		if obj.ownerSpelled != "" && obj.linked == nil {
			items = append(items, item{
				id:         "owner|" + obj.key(),
				statements: []string{fmt.Sprintf("ALTER %s OWNER TO %s", obj.alterTarget(), obj.ownerSpelled)},
			})
		}
	}
	for i, r := range m.replays {
		sql := r.sql
		if r.bulk != nil {
			sql = renderBulk(r.bulk)
		}
		if sql != "" {
			items = append(items, item{id: fmt.Sprintf("replay|%d", i), statements: asRole(r.role, []string{sql})})
		}
	}
	for _, obj := range objects {
		want := obj.acl
		if want == nil {
			want = defaultACL(obj.class(), obj.owner)
		}
		statements := m.aclStatements(obj.class(), m.stateAfterReplays(obj), want, obj.grantTarget(), "", "")
		columns := make([]string, 0, len(obj.columns))
		for column := range obj.columns {
			columns = append(columns, column)
		}
		sort.Strings(columns)
		for _, column := range columns {
			statements = append(statements, m.aclStatements(classColumn, acl{}, obj.columns[column], "TABLE "+qualified(obj.schema, obj.name), column, "")...)
		}
		// What another role granted with its grant option is granted by that
		// role, so PostgreSQL records it as the grantor: GRANTED BY only
		// accepts the current role, in every version.
		grantors := make([]string, 0, len(obj.delegated))
		for grantor := range obj.delegated {
			grantors = append(grantors, grantor)
		}
		sort.Strings(grantors)
		for _, grantor := range grantors {
			delegated := m.aclStatements(obj.class(), acl{}, obj.delegated[grantor], obj.grantTarget(), "", "")
			statements = append(statements, asRole(grantee{token: grantor, spelled: quoteIdent(grantor)}, delegated)...)
		}
		items = append(items, item{id: "acl|" + obj.key(), statements: statements})
	}
	for _, key := range m.defaultOrder {
		entry, ok := m.defaults[key]
		if !ok {
			continue
		}
		if schema := m.findSchema(key.schema); key.schema != "" && schema != nil && !alive(schema) {
			m.warn("default privileges in schema %s are not in the baseline: the baseline does not create the schema", key.schema)
			continue
		}
		_, class, objects, _ := defaultsTargetByCode(key.objtype)
		base := acl{}
		if key.schema == "" {
			base = defaultACL(class, key.role)
		}
		prefix := "ALTER DEFAULT PRIVILEGES "
		if key.role != roleMigrator {
			prefix += "FOR ROLE " + m.roleSQL(key.role) + " "
		}
		if key.schema != "" {
			prefix += "IN SCHEMA " + quoteIdent(key.schema) + " "
		}
		items = append(items, item{
			id:         fmt.Sprintf("defaults|%s|%s|%c", key.role, key.schema, key.objtype),
			statements: m.aclStatements(class, base, entry, objects, "", prefix),
		})
	}
	return items
}

func defaultsTargetByCode(code byte) (byte, aclClass, string, bool) {
	for _, objtype := range []pg_query.ObjectType{
		pg_query.ObjectType_OBJECT_TABLE, pg_query.ObjectType_OBJECT_SEQUENCE, pg_query.ObjectType_OBJECT_FUNCTION,
		pg_query.ObjectType_OBJECT_TYPE, pg_query.ObjectType_OBJECT_SCHEMA,
	} {
		if c, class, name, ok := defaultsTarget(objtype); ok && c == code {
			return c, class, name, true
		}
	}
	return 0, 0, "", false
}
