package normalize

import (
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// ---- name helpers ----------------------------------------------------------

func stringList(nodes []*pg_query.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if s := node.GetString_(); s != nil {
			out = append(out, s.GetSval())
		}
	}
	return out
}

func nameParts(node *pg_query.Node) []string {
	switch {
	case node == nil:
		return nil
	case node.GetList() != nil:
		return stringList(node.GetList().GetItems())
	case node.GetString_() != nil:
		return []string{node.GetString_().GetSval()}
	case node.GetTypeName() != nil:
		return stringList(node.GetTypeName().GetNames())
	}
	return nil
}

// splitName separates an optional schema from an object name; a
// database-qualified name keeps its last two parts.
func splitName(parts []string) (schemaName, name string) {
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return "", parts[0]
	default:
		return parts[len(parts)-2], parts[len(parts)-1]
	}
}

// schemaText is the schema to write in a rewritten name: none when the name
// was written unqualified and still resolves the way it did (same schema,
// same name), the target schema's final name otherwise. resolvedIn is the
// schema the name was found in at this point of the history; target is the
// first-pass schema the name must end up in.
func (r *rewriter) schemaText(written string, resolvedIn, target *schema) string {
	if target == nil {
		return written
	}
	if r.qualify {
		return target.name
	}
	if written == "" && resolvedIn != nil && (resolvedIn.final == target || resolvedIn == target) && target.name == resolvedIn.name {
		return ""
	}
	return target.name
}

// isOwn reports a relation named as it is at this point: the table whose
// own statement this is, or its implicit index or sequence.
func (r *rewriter) isOwn(rel *relation) bool {
	return r.own != nil && (rel == r.own || rel.implicit && rel.owner == r.own)
}

// targetSchema is the first-pass schema a relation is written in.
func (r *rewriter) targetSchema(rel *relation) *schema {
	if r.isOwn(rel) {
		if rel.schema.final != nil {
			return rel.schema.final
		}
		return rel.schema
	}
	return rel.finalSchema()
}

func (r *rewriter) relationName(rel *relation) string {
	if r.isOwn(rel) {
		return rel.name
	}
	return rel.finalName()
}

func (r *rewriter) columnText(rel *relation, col *column) string {
	if r.isOwn(rel) {
		return col.name
	}
	return col.finalName()
}

// ---- relations -----------------------------------------------------------

func (r *rewriter) findRelation(rv *pg_query.RangeVar) *relation {
	if rv == nil {
		return nil
	}
	return r.s.findRelation(rv.GetSchemaname(), rv.GetRelname())
}

// relRef resolves a relation reference and, in the second pass, rewrites it
// to the name the relation has where the statement runs in the baseline.
func (r *rewriter) relRef(rv *pg_query.RangeVar) *relation {
	if rv == nil {
		return nil
	}
	rel := r.findRelation(rv)
	if !r.rewrite {
		return rel
	}
	if rel == nil {
		r.renameSchemaOnly(&rv.Schemaname)
		return nil
	}
	r.noteKilled(rel.isKilled(), rel.name)
	rv.Schemaname = r.schemaText(rv.GetSchemaname(), rel.schema, r.targetSchema(rel))
	rv.Relname = r.relationName(rel)
	return rel
}

// nameCreated rewrites the name a CREATE statement gives its relation.
func (r *rewriter) nameCreated(rv *pg_query.RangeVar, rel *relation) {
	if !r.rewrite {
		return
	}
	rv.Schemaname = r.schemaText(rv.GetSchemaname(), rel.schema, r.targetSchema(rel))
	rv.Relname = r.relationName(rel)
}

// renameSchemaOnly rewrites the schema of a name the history did not
// create, when the history renamed that schema.
func (r *rewriter) renameSchemaOnly(name *string) {
	if *name == "" {
		return
	}
	if sc := r.s.findSchema(*name); sc != nil {
		*name = sc.finalName()
	}
}

// relationFromList resolves a relation named by a list of String nodes.
func (r *rewriter) relationFromList(items []*pg_query.Node) *relation {
	schemaName, name := splitName(stringList(items))
	return r.s.findRelation(schemaName, name)
}

// rewriteRelationList rewrites a relation name given as a list of String
// nodes in place.
func (r *rewriter) rewriteRelationList(items []*pg_query.Node, rel *relation) {
	if !r.rewrite || rel == nil || len(items) == 0 {
		return
	}
	parts := stringList(items)
	schemaName, _ := splitName(parts)
	if last := items[len(items)-1].GetString_(); last != nil {
		last.Sval = r.relationName(rel)
	}
	target := r.schemaText(schemaName, rel.schema, r.targetSchema(rel))
	if len(items) >= 2 {
		if s := items[len(items)-2].GetString_(); s != nil && target != "" {
			s.Sval = target
		}
	} else if target != "" {
		r.n.warn("%s: %s moves to schema %s, but the statement names it without a schema", r.sql, rel.name, target)
	}
	r.noteKilled(rel.isKilled(), rel.name)
}

// schemaInList rewrites the schema of a qualified name list (a function, a
// statistics object) when the history renamed that schema.
func (r *rewriter) schemaInList(names []*pg_query.Node) {
	if !r.rewrite || len(names) < 2 {
		return
	}
	if s := names[len(names)-2].GetString_(); s != nil {
		r.renameSchemaOnly(&s.Sval)
	}
}

// columnName rewrites a column name of a relation in place.
func (r *rewriter) columnName(name *string, rel *relation) {
	if !r.rewrite || rel == nil {
		return
	}
	if col := rel.column(*name); col != nil {
		*name = r.columnText(rel, col)
	}
}

func (r *rewriter) columnNames(nodes []*pg_query.Node, rel *relation) {
	for _, node := range nodes {
		if s := node.GetString_(); s != nil {
			r.columnName(&s.Sval, rel)
		}
	}
}

// noteKilled warns when a statement that stays in the baseline uses an
// object DROP SCHEMA ... CASCADE removes: PostgreSQL drops what depends on
// it, the baseline cannot.
func (r *rewriter) noteKilled(isKilled bool, name string) {
	if isKilled && !killed(r.subject) {
		r.n.warn("%s: uses %s, which DROP SCHEMA ... CASCADE removes later; the statement stays in the baseline", r.sql, name)
	}
}

// ---- types ---------------------------------------------------------------

// findTypeNode resolves a type named by a List of String nodes or a TypeName.
func (r *rewriter) findTypeNode(node *pg_query.Node) *typ {
	schemaName, name := splitName(nameParts(node))
	if schemaName == "pg_catalog" {
		return nil
	}
	return r.s.findType(schemaName, name)
}

// typeName resolves and rewrites a type reference. A table's row type and
// %TYPE references follow the table.
func (r *rewriter) typeName(tn *pg_query.TypeName) *typ {
	if tn == nil {
		return nil
	}
	parts := stringList(tn.GetNames())
	if tn.GetPctType() {
		r.pctType(tn, parts)
		return nil
	}
	schemaName, name := splitName(parts)
	if schemaName == "pg_catalog" || name == "" {
		return nil
	}
	t := r.s.findType(schemaName, name)
	if t == nil {
		if rel := r.s.findRelation(schemaName, name); rel != nil && r.rewrite {
			target := r.schemaText(schemaName, rel.schema, r.targetSchema(rel))
			tn.Names = qualifiedNodes(target, r.relationName(rel))
			return nil
		}
		if r.rewrite && schemaName != "" && len(tn.GetNames()) >= 2 {
			if s := tn.GetNames()[len(tn.GetNames())-2].GetString_(); s != nil {
				r.renameSchemaOnly(&s.Sval)
			}
		}
		return nil
	}
	if r.rewrite {
		r.noteKilled(t.isKilled(), t.name)
		target := r.schemaText(schemaName, t.schema, t.finalSchema())
		tn.Names = qualifiedNodes(target, t.finalName())
	}
	return t
}

// pctType rewrites table.column%TYPE.
func (r *rewriter) pctType(tn *pg_query.TypeName, parts []string) {
	if len(parts) < 2 || !r.rewrite {
		return
	}
	rel := r.s.findRelation(func() string {
		if len(parts) >= 3 {
			return parts[len(parts)-3]
		}
		return ""
	}(), parts[len(parts)-2])
	if rel == nil {
		return
	}
	col := rel.column(parts[len(parts)-1])
	target := r.schemaText(func() string {
		if len(parts) >= 3 {
			return parts[len(parts)-3]
		}
		return ""
	}(), rel.schema, r.targetSchema(rel))
	names := qualifiedNodes(target, r.relationName(rel))
	if col != nil {
		names = append(names, pg_query.MakeStrNode(r.columnText(rel, col)))
	} else {
		names = append(names, pg_query.MakeStrNode(parts[len(parts)-1]))
	}
	tn.Names = names
}

func qualifiedNodes(schemaName, name string) []*pg_query.Node {
	if schemaName == "" {
		return []*pg_query.Node{pg_query.MakeStrNode(name)}
	}
	return []*pg_query.Node{pg_query.MakeStrNode(schemaName), pg_query.MakeStrNode(name)}
}

// enumLiteral rewrites a string literal that is a value of enum t (directly,
// or through a domain) to the value's final name: a column default or a
// comparison with an enum column. PostgreSQL stores such a literal as the
// enum value, so it follows RENAME VALUE.
func (r *rewriter) enumLiteral(node *pg_query.Node, t *typ) {
	if !r.rewrite || node == nil {
		return
	}
	enum := t.enumType()
	if enum == nil {
		return
	}
	switch {
	case node.GetAConst() != nil:
		sval := node.GetAConst().GetSval()
		if sval == nil {
			return
		}
		if l := enum.label(sval.GetSval()); l != nil {
			sval.Sval = l.finalName()
		}
	case node.GetTypeCast() != nil:
		r.enumLiteral(node.GetTypeCast().GetArg(), t)
	case node.GetList() != nil:
		for _, item := range node.GetList().GetItems() {
			r.enumLiteral(item, t)
		}
	case node.GetAArrayExpr() != nil:
		for _, item := range node.GetAArrayExpr().GetElements() {
			r.enumLiteral(item, t)
		}
	}
}

// ---- scopes --------------------------------------------------------------

// source is one FROM item a query can name columns of.
type source struct {
	name    string    // how the query refers to it: its alias, or the relation name as written
	schema  string    // schema as written, for three-part column references
	rel     *relation // the relation, when the history created it
	cols    []string  // output columns of a derived table, CTE or aliased function
	known   bool      // its columns are known (rel, or cols)
	aliased bool
	emit    string // qualifier to write in rewritten column references
}

type scope struct {
	sources []*source
	parent  *scope
	ctes    map[string][]string // name -> output columns (nil when unknown)
}

func (sc *scope) cte(name string) ([]string, bool) {
	for s := sc; s != nil; s = s.parent {
		if cols, ok := s.ctes[name]; ok {
			return cols, true
		}
	}
	return nil, false
}

// findSource finds a FROM item by the name the query uses for it.
func (sc *scope) findSource(schemaName, name string) *source {
	for s := sc; s != nil; s = s.parent {
		for _, src := range s.sources {
			if src.name != name {
				continue
			}
			if schemaName != "" && src.schema != schemaName && (src.rel == nil || src.rel.schema.name != schemaName) {
				continue
			}
			return src
		}
	}
	return nil
}

// lookupColumn resolves an unqualified column name the way PostgreSQL does:
// the innermost query level that has it. A statement that ran is not
// ambiguous, so a column found in a known relation is that relation's; a
// level with FROM items whose columns are unknown cannot rule them out.
func (sc *scope) lookupColumn(name string) (*source, *column) {
	for s := sc; s != nil; s = s.parent {
		unknown := false
		for _, src := range s.sources {
			switch {
			case src.rel != nil:
				if col := src.rel.column(name); col != nil {
					return src, col
				}
			case src.known:
				for _, c := range src.cols {
					if c == name {
						return src, nil
					}
				}
			default:
				unknown = true
			}
		}
		if unknown {
			return nil, nil
		}
	}
	return nil, nil
}

// tableScope is the scope of an expression about one table (a constraint,
// an index, a policy): its columns, named with or without the table name.
func (r *rewriter) tableScope(written string, rel *relation) *scope {
	if rel == nil {
		return &scope{sources: []*source{{known: false}}}
	}
	if written == "" {
		written = rel.name
	}
	return &scope{sources: []*source{{name: written, rel: rel, known: true, emit: r.relationName(rel)}}}
}

// rowScope is the scope of a trigger's WHEN or a rule: NEW and OLD rows.
func (r *rewriter) rowScope(rel *relation) *scope {
	if rel == nil {
		return nil
	}
	return &scope{sources: []*source{
		{name: "new", rel: rel, known: true, aliased: true, emit: "new"},
		{name: "old", rel: rel, known: true, aliased: true, emit: "old"},
	}}
}

// ---- expressions -----------------------------------------------------------

// walk rewrites the references in a parse tree node.
func (r *rewriter) walk(m proto.Message, sc *scope) {
	if m == nil {
		return
	}
	if !m.ProtoReflect().IsValid() {
		return
	}
	if node, ok := m.(*pg_query.Node); ok && node.GetNode() == nil {
		return
	}
	switch n := m.(type) {
	case *pg_query.ColumnRef:
		r.columnRef(n, sc)
		return
	case *pg_query.SelectStmt:
		r.selectStmt(n, sc)
		return
	case *pg_query.InsertStmt:
		r.insert(n, sc)
		return
	case *pg_query.UpdateStmt:
		r.update(n, sc)
		return
	case *pg_query.DeleteStmt:
		r.delete(n, sc)
		return
	case *pg_query.RangeVar:
		r.relRef(n)
		return
	case *pg_query.TypeName:
		r.typeName(n)
		return
	case *pg_query.TypeCast:
		r.typeCast(n)
	case *pg_query.A_Expr:
		r.comparison(n, sc)
	case *pg_query.FuncCall:
		r.funcCall(n)
	case *pg_query.ObjectWithArgs:
		r.schemaInList(n.GetObjname())
	case *pg_query.A_Indirection:
		r.fieldAccess(n)
	}
	r.children(m, sc)
}

// fieldAccess warns about (value).field where field is a composite type
// attribute renamed later: the type of value is not known here, so the
// field is not rewritten.
func (r *rewriter) fieldAccess(ind *pg_query.A_Indirection) {
	if !r.rewrite {
		return
	}
	for _, node := range ind.GetIndirection() {
		field := node.GetString_()
		if field == nil {
			continue
		}
		for _, t := range r.s.types {
			if t.dead || t.kind != typComposite {
				continue
			}
			if attr := t.attribute(field.GetSval()); attr != nil && attr.finalName() != attr.name {
				r.n.warn("%s: (...).%s may name attribute %s of type %s, which is renamed later; field accesses are not rewritten", r.sql, attr.name, attr.name, t.name)
			}
		}
	}
}

func (r *rewriter) children(m proto.Message, sc *scope) {
	if m == nil {
		return
	}
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Message() == nil || fd.IsMap() {
			return true
		}
		if fd.IsList() {
			list := v.List()
			for i := 0; i < list.Len(); i++ {
				r.walk(list.Get(i).Message().Interface(), sc)
			}
			return true
		}
		r.walk(v.Message().Interface(), sc)
		return true
	})
}

// typeCast rewrites 'value'::enum and 'relation'::regclass literals.
func (r *rewriter) typeCast(tc *pg_query.TypeCast) {
	arg := tc.GetArg().GetAConst().GetSval()
	if arg == nil || !r.rewrite {
		return
	}
	parts := stringList(tc.GetTypeName().GetNames())
	if len(parts) > 0 && parts[len(parts)-1] == "regclass" {
		r.relationLiteral(arg)
		return
	}
	schemaName, name := splitName(parts)
	if schemaName == "pg_catalog" {
		return
	}
	if t := r.s.findType(schemaName, name); t != nil {
		r.enumLiteral(tc.GetArg(), t)
	}
}

// relationLiteral rewrites a relation named in a string ('seq' in
// nextval('seq'), 'table'::regclass).
func (r *rewriter) relationLiteral(s *pg_query.String) {
	parsed, err := pg_query.Parse("SELECT FROM " + s.GetSval())
	if err != nil {
		return
	}
	from := parsed.GetStmts()[0].GetStmt().GetSelectStmt().GetFromClause()
	if len(from) != 1 || from[0].GetRangeVar() == nil {
		return
	}
	rv := from[0].GetRangeVar()
	rel := r.findRelation(rv)
	if rel == nil {
		return
	}
	target := r.schemaText(rv.GetSchemaname(), rel.schema, r.targetSchema(rel))
	name := quoteIdent(r.relationName(rel))
	if target != "" {
		name = quoteIdent(target) + "." + name
	}
	s.Sval = name
}

// comparison rewrites a literal compared with an enum column.
func (r *rewriter) comparison(expr *pg_query.A_Expr, sc *scope) {
	if !r.rewrite || sc == nil {
		return
	}
	left, right := expr.GetLexpr(), expr.GetRexpr()
	for _, pair := range [][2]*pg_query.Node{{left, right}, {right, left}} {
		cr := pair[0].GetColumnRef()
		if cr == nil {
			continue
		}
		if _, col := r.resolveColumn(cr, sc); col != nil && col.typ != nil {
			r.enumLiteral(pair[1], col.typ)
		}
	}
}

// funcCall rewrites a schema-qualified function name and the relation
// nextval, currval and setval name in a string.
func (r *rewriter) funcCall(fc *pg_query.FuncCall) {
	r.schemaInList(fc.GetFuncname())
	names := stringList(fc.GetFuncname())
	if len(names) == 0 || !r.rewrite {
		return
	}
	switch names[len(names)-1] {
	case "nextval", "currval", "setval":
		if len(fc.GetArgs()) > 0 {
			if s := fc.GetArgs()[0].GetAConst().GetSval(); s != nil {
				r.relationLiteral(s)
			}
		}
	}
}

// resolveColumn finds the column a column reference names, without
// rewriting it.
func (r *rewriter) resolveColumn(cr *pg_query.ColumnRef, sc *scope) (*source, *column) {
	fields := cr.GetFields()
	if len(fields) == 0 || fields[len(fields)-1].GetAStar() != nil {
		return nil, nil
	}
	names := stringList(fields)
	if len(names) != len(fields) || sc == nil {
		return nil, nil
	}
	switch len(names) {
	case 1:
		return sc.lookupColumn(names[0])
	case 2, 3:
		schemaName := ""
		if len(names) == 3 {
			schemaName = names[0]
		}
		src := sc.findSource(schemaName, names[len(names)-2])
		if src == nil || src.rel == nil {
			return src, nil
		}
		return src, src.rel.column(names[len(names)-1])
	}
	return nil, nil
}

// columnRef rewrites a column reference.
func (r *rewriter) columnRef(cr *pg_query.ColumnRef, sc *scope) {
	if !r.rewrite || sc == nil {
		return
	}
	fields := cr.GetFields()
	if len(fields) == 0 {
		return
	}
	star := fields[len(fields)-1].GetAStar() != nil
	qualifierAt := len(fields) - 2
	var src *source
	var col *column
	if star {
		if len(fields) >= 2 {
			names := stringList(fields[:len(fields)-1])
			schemaName, name := splitName(names)
			src = sc.findSource(schemaName, name)
		}
	} else {
		src, col = r.resolveColumn(cr, sc)
	}
	if src == nil || src.rel == nil {
		return
	}
	if col != nil {
		if s := fields[len(fields)-1].GetString_(); s != nil {
			s.Sval = r.columnText(src.rel, col)
		}
	}
	if qualifierAt >= 0 && !src.aliased {
		if s := fields[qualifierAt].GetString_(); s != nil {
			s.Sval = src.emit
		}
		if qualifierAt >= 1 {
			if s := fields[qualifierAt-1].GetString_(); s != nil {
				s.Sval = r.targetSchema(src.rel).name
			}
		}
	}
}

// ---- queries ---------------------------------------------------------------

// selectStmt rewrites a query and returns the names of its output columns,
// which the rewrite keeps: an output column named after a column that is
// renamed later gets that name as an explicit alias, the way PostgreSQL
// keeps a view's column names when a column it reads is renamed.
func (r *rewriter) selectStmt(sel *pg_query.SelectStmt, parent *scope) []string {
	if sel == nil {
		return nil
	}
	if sel.GetOp() != pg_query.SetOperation_SETOP_NONE {
		// A WITH on a UNION is visible to both of its queries.
		shared := &scope{parent: parent, ctes: map[string][]string{}}
		r.withClause(sel.GetWithClause(), shared)
		names := r.selectStmt(sel.GetLarg(), shared)
		r.selectStmt(sel.GetRarg(), shared)
		for _, node := range sel.GetSortClause() {
			r.walk(node, nil)
		}
		return names
	}
	sc := &scope{parent: parent, ctes: map[string][]string{}}
	r.withClause(sel.GetWithClause(), sc)
	if len(sel.GetValuesLists()) > 0 {
		var names []string
		for _, row := range sel.GetValuesLists() {
			items := row.GetList().GetItems()
			for len(names) < len(items) {
				names = append(names, "column"+itoa(len(names)+1))
			}
			for _, item := range items {
				r.walk(item, parent)
			}
		}
		return names
	}
	for _, item := range sel.GetFromClause() {
		r.fromItem(item, sc)
	}
	names := r.targetList(sel, sc)
	for _, node := range []*pg_query.Node{sel.GetWhereClause(), sel.GetHavingClause(), sel.GetLimitCount(), sel.GetLimitOffset()} {
		r.walk(node, sc)
	}
	for _, list := range [][]*pg_query.Node{sel.GetGroupClause(), sel.GetSortClause(), sel.GetDistinctClause(), sel.GetWindowClause(), sel.GetLockingClause()} {
		for _, node := range list {
			r.walk(node, sc)
		}
	}
	return names
}

func (r *rewriter) withClause(with *pg_query.WithClause, sc *scope) {
	for _, node := range with.GetCtes() {
		cte := node.GetCommonTableExpr()
		if cte == nil {
			continue
		}
		if with.GetRecursive() {
			sc.ctes[cte.GetCtename()] = nil
		}
		var names []string
		switch q := cte.GetCtequery().GetNode().(type) {
		case *pg_query.Node_SelectStmt:
			names = r.selectStmt(q.SelectStmt, sc)
		default:
			r.walk(cte.GetCtequery(), sc)
		}
		if aliases := stringList(cte.GetAliascolnames()); len(aliases) > 0 {
			for i := range aliases {
				if i < len(names) {
					names[i] = aliases[i]
				} else {
					names = append(names, aliases[i])
				}
			}
		}
		sc.ctes[cte.GetCtename()] = names
	}
}

func (r *rewriter) fromItem(node *pg_query.Node, sc *scope) {
	switch n := node.GetNode().(type) {
	case *pg_query.Node_RangeVar:
		rv := n.RangeVar
		if rv.GetSchemaname() == "" {
			if cols, ok := sc.cte(rv.GetRelname()); ok {
				src := &source{name: rv.GetRelname(), cols: cols, known: cols != nil, emit: rv.GetRelname()}
				r.aliasSource(src, rv.GetAlias())
				sc.sources = append(sc.sources, src)
				return
			}
		}
		written, writtenSchema := rv.GetRelname(), rv.GetSchemaname()
		rel := r.relRef(rv)
		src := &source{name: written, schema: writtenSchema, rel: rel, known: rel != nil, emit: rv.GetRelname()}
		r.aliasSource(src, rv.GetAlias())
		sc.sources = append(sc.sources, src)
	case *pg_query.Node_RangeSubselect:
		lookup := sc.parent
		if n.RangeSubselect.GetLateral() {
			lookup = sc
		}
		var names []string
		if sub := n.RangeSubselect.GetSubquery().GetSelectStmt(); sub != nil {
			names = r.selectStmt(sub, lookup)
		}
		src := &source{cols: names, known: names != nil}
		r.aliasSource(src, n.RangeSubselect.GetAlias())
		sc.sources = append(sc.sources, src)
	case *pg_query.Node_JoinExpr:
		join := n.JoinExpr
		r.fromItem(join.GetLarg(), sc)
		r.fromItem(join.GetRarg(), sc)
		r.walk(join.GetQuals(), sc)
		if r.rewrite {
			for _, name := range stringList(join.GetUsingClause()) {
				if src, col := sc.lookupColumn(name); col != nil && r.columnText(src.rel, col) != name {
					r.n.warn("%s: JOIN ... USING (%s) names a column that is renamed later; the join is not rewritten", r.sql, name)
				}
			}
			if join.GetIsNatural() {
				r.n.warn("%s: a NATURAL JOIN is not checked against later column renames", r.sql)
			}
		}
		if join.GetAlias() != nil {
			sc.sources = append(sc.sources, &source{name: join.GetAlias().GetAliasname(), aliased: true})
		}
	case *pg_query.Node_RangeFunction:
		r.walk(node, sc)
		src := &source{}
		r.aliasSource(src, n.RangeFunction.GetAlias())
		if !src.known {
			for _, col := range n.RangeFunction.GetColdeflist() {
				src.cols = append(src.cols, col.GetColumnDef().GetColname())
			}
			src.known = len(src.cols) > 0
		}
		sc.sources = append(sc.sources, src)
	default:
		r.walk(node, sc)
		sc.sources = append(sc.sources, &source{})
	}
}

// aliasSource applies an alias to a FROM item: the query names it by the
// alias, and alias column names replace its first column names.
func (r *rewriter) aliasSource(src *source, alias *pg_query.Alias) {
	if alias == nil {
		return
	}
	src.name, src.emit, src.aliased = alias.GetAliasname(), alias.GetAliasname(), true
	colnames := stringList(alias.GetColnames())
	if len(colnames) == 0 {
		return
	}
	cols := append([]string(nil), colnames...)
	switch {
	case src.rel != nil:
		live := src.rel.liveColumns()
		for i := len(colnames); i < len(live); i++ {
			cols = append(cols, live[i].name)
		}
	case len(src.cols) > len(colnames):
		cols = append(cols, src.cols[len(colnames):]...)
	}
	src.rel, src.cols, src.known = nil, cols, true
}

// targetList rewrites a query's output columns and returns their names.
func (r *rewriter) targetList(sel *pg_query.SelectStmt, sc *scope) []string {
	var names []string
	var out []*pg_query.Node
	for _, node := range sel.GetTargetList() {
		rt := node.GetResTarget()
		if rt == nil {
			out = append(out, node)
			continue
		}
		if cr := rt.GetVal().GetColumnRef(); cr != nil && len(cr.GetFields()) > 0 && cr.GetFields()[len(cr.GetFields())-1].GetAStar() != nil {
			expanded, expandedNames, ok := r.expandStar(cr, sc)
			if ok {
				out = append(out, expanded...)
				names = append(names, expandedNames...)
				continue
			}
			r.columnRef(cr, sc)
			out = append(out, node)
			names = append(names, r.starNames(cr, sc)...)
			continue
		}
		natural := naturalName(rt.GetVal())
		r.walk(rt.GetVal(), sc)
		if r.rewrite && rt.GetName() == "" && natural != "" && naturalName(rt.GetVal()) != natural {
			rt.Name = natural
		}
		name := rt.GetName()
		if name == "" {
			name = natural
		}
		if name == "" {
			name = "?column?"
		}
		out = append(out, node)
		names = append(names, name)
	}
	if r.rewrite {
		sel.TargetList = out
	}
	return names
}

// starSources lists the FROM items a * or name.* covers.
func starSources(cr *pg_query.ColumnRef, sc *scope) []*source {
	fields := cr.GetFields()
	if len(fields) == 1 {
		return sc.sources
	}
	schemaName, name := splitName(stringList(fields[:len(fields)-1]))
	for _, src := range sc.sources {
		if src.name == name && (schemaName == "" || src.schema == schemaName) {
			return []*source{src}
		}
	}
	return nil
}

func (r *rewriter) starNames(cr *pg_query.ColumnRef, sc *scope) []string {
	var names []string
	for _, src := range starSources(cr, sc) {
		switch {
		case src.rel != nil:
			for _, col := range src.rel.liveColumns() {
				names = append(names, col.name)
			}
		case src.known:
			names = append(names, src.cols...)
		}
	}
	return names
}

// expandStar spells out * where the baseline would read different columns:
// a relation whose columns are renamed, added or dropped later. PostgreSQL
// fixes the column list of a view at creation, and a baseline creates the
// view after the table's last change.
func (r *rewriter) expandStar(cr *pg_query.ColumnRef, sc *scope) ([]*pg_query.Node, []string, bool) {
	sources := starSources(cr, sc)
	if len(sources) == 0 || !r.rewrite {
		return nil, nil, false
	}
	needed := false
	for _, src := range sources {
		if src.rel != nil && columnsChange(src.rel) {
			needed = true
		}
		if src.rel == nil && !src.known {
			return nil, nil, false
		}
	}
	if !needed {
		return nil, nil, false
	}
	for _, src := range sc.sources {
		if src.name == "" && len(sources) == len(sc.sources) && len(sc.sources) > 1 {
			return nil, nil, false // an unnamed FROM item cannot be qualified
		}
	}
	qualify := len(sc.sources) > 1
	var out []*pg_query.Node
	var names []string
	for _, src := range sources {
		var cols []string
		var finals []string
		if src.rel != nil {
			for _, col := range src.rel.liveColumns() {
				cols = append(cols, col.name)
				finals = append(finals, r.columnText(src.rel, col))
			}
		} else {
			cols, finals = src.cols, src.cols
		}
		for i, name := range cols {
			fields := []*pg_query.Node{pg_query.MakeStrNode(finals[i])}
			if qualify {
				fields = append([]*pg_query.Node{pg_query.MakeStrNode(src.emit)}, fields...)
			}
			rt := &pg_query.ResTarget{Val: &pg_query.Node{Node: &pg_query.Node_ColumnRef{ColumnRef: &pg_query.ColumnRef{Fields: fields}}}}
			if finals[i] != name {
				rt.Name = name
			}
			out = append(out, &pg_query.Node{Node: &pg_query.Node_ResTarget{ResTarget: rt}})
			names = append(names, name)
		}
	}
	return out, names, true
}

// columnsChange reports a relation whose live columns at this point differ
// from those it ends with, by name or membership.
func columnsChange(rel *relation) bool {
	if rel.final == nil {
		return false
	}
	now := rel.liveColumns()
	end := rel.final.liveColumns()
	if len(now) != len(end) {
		return true
	}
	for i, col := range now {
		if col.final != end[i] || col.name != end[i].name {
			return true
		}
	}
	return false
}

// naturalName is the output column name PostgreSQL gives an expression
// without an alias (FigureColname), for the cases a rewrite can change.
func naturalName(node *pg_query.Node) string {
	switch n := node.GetNode().(type) {
	case *pg_query.Node_ColumnRef:
		fields := n.ColumnRef.GetFields()
		if len(fields) > 0 {
			return fields[len(fields)-1].GetString_().GetSval()
		}
	case *pg_query.Node_TypeCast:
		if name := naturalName(n.TypeCast.GetArg()); name != "" {
			return name
		}
		names := stringList(n.TypeCast.GetTypeName().GetNames())
		if len(names) > 0 {
			return names[len(names)-1]
		}
	case *pg_query.Node_AIndirection:
		indirection := n.AIndirection.GetIndirection()
		if len(indirection) > 0 {
			if s := indirection[len(indirection)-1].GetString_(); s != nil {
				return s.GetSval()
			}
		}
		return naturalName(n.AIndirection.GetArg())
	case *pg_query.Node_FuncCall:
		names := stringList(n.FuncCall.GetFuncname())
		if len(names) > 0 {
			return names[len(names)-1]
		}
	case *pg_query.Node_CaseExpr:
		return "case"
	case *pg_query.Node_CoalesceExpr:
		return "coalesce"
	case *pg_query.Node_AArrayExpr:
		return "array"
	case *pg_query.Node_RowExpr:
		return "row"
	case *pg_query.Node_MinMaxExpr:
		if n.MinMaxExpr.GetOp() == pg_query.MinMaxOp_IS_GREATEST {
			return "greatest"
		}
		return "least"
	case *pg_query.Node_AExpr:
		if n.AExpr.GetKind() == pg_query.A_Expr_Kind_AEXPR_NULLIF {
			return "nullif"
		}
	case *pg_query.Node_SubLink:
		switch n.SubLink.GetSubLinkType() {
		case pg_query.SubLinkType_EXISTS_SUBLINK:
			return "exists"
		case pg_query.SubLinkType_ARRAY_SUBLINK:
			return "array"
		}
	}
	return ""
}

// ---- data changes ----------------------------------------------------------

func (r *rewriter) insert(stmt *pg_query.InsertStmt, parent *scope) *relation {
	sc := &scope{parent: parent, ctes: map[string][]string{}}
	r.withClause(stmt.GetWithClause(), sc)
	written := stmt.GetRelation().GetRelname()
	rel := r.relRef(stmt.GetRelation())
	var types []*typ
	for _, node := range stmt.GetCols() {
		rt := node.GetResTarget()
		var col *column
		if rel != nil {
			col = rel.column(rt.GetName())
		}
		if col != nil {
			types = append(types, col.typ)
			if r.rewrite {
				rt.Name = r.columnText(rel, col)
			}
		} else {
			types = append(types, nil)
		}
		r.walk(&pg_query.Node{Node: &pg_query.Node_List{List: &pg_query.List{Items: rt.GetIndirection()}}}, nil)
	}
	if len(stmt.GetCols()) == 0 && rel != nil {
		for _, col := range rel.liveColumns() {
			types = append(types, col.typ)
		}
	}
	if sel := stmt.GetSelectStmt().GetSelectStmt(); sel != nil {
		for _, row := range sel.GetValuesLists() {
			for i, item := range row.GetList().GetItems() {
				if i < len(types) && types[i] != nil {
					r.enumLiteral(item, types[i])
				}
			}
		}
		r.selectStmt(sel, sc)
	}
	target := r.tableScope(written, rel)
	target.parent = sc
	if alias := stmt.GetRelation().GetAlias(); alias != nil && len(target.sources) > 0 {
		target.sources[0].name, target.sources[0].emit, target.sources[0].aliased = alias.GetAliasname(), alias.GetAliasname(), true
	}
	if conflict := stmt.GetOnConflictClause(); conflict != nil {
		excluded := &scope{parent: sc, sources: append(append([]*source(nil), target.sources...), &source{name: "excluded", rel: rel, known: rel != nil, aliased: true, emit: "excluded"})}
		if infer := conflict.GetInfer(); infer != nil {
			for _, node := range infer.GetIndexElems() {
				elem := node.GetIndexElem()
				if elem.GetName() != "" && rel != nil {
					r.columnName(&elem.Name, rel)
				}
				r.walk(elem.GetExpr(), target)
			}
			r.walk(infer.GetWhereClause(), target)
		}
		r.setTargets(conflict.GetTargetList(), rel, excluded)
		r.walk(conflict.GetWhereClause(), excluded)
	}
	for _, node := range stmt.GetReturningList() {
		r.walk(node, target)
	}
	return rel
}

// setTargets rewrites UPDATE SET column = value lists.
func (r *rewriter) setTargets(targets []*pg_query.Node, rel *relation, sc *scope) {
	for _, node := range targets {
		rt := node.GetResTarget()
		if rt == nil {
			r.walk(node, sc)
			continue
		}
		if rel != nil {
			if col := rel.column(rt.GetName()); col != nil {
				r.enumLiteral(rt.GetVal(), col.typ)
				if r.rewrite {
					rt.Name = r.columnText(rel, col)
				}
			}
		}
		r.walk(rt.GetVal(), sc)
	}
}

func (r *rewriter) update(stmt *pg_query.UpdateStmt, parent *scope) *relation {
	sc := &scope{parent: parent, ctes: map[string][]string{}}
	r.withClause(stmt.GetWithClause(), sc)
	written := stmt.GetRelation().GetRelname()
	rel := r.relRef(stmt.GetRelation())
	src := &source{name: written, rel: rel, known: rel != nil, emit: stmt.GetRelation().GetRelname()}
	r.aliasSource(src, stmt.GetRelation().GetAlias())
	sc.sources = append(sc.sources, src)
	for _, item := range stmt.GetFromClause() {
		r.fromItem(item, sc)
	}
	r.setTargets(stmt.GetTargetList(), rel, sc)
	r.walk(stmt.GetWhereClause(), sc)
	for _, node := range stmt.GetReturningList() {
		r.walk(node, sc)
	}
	return rel
}

func (r *rewriter) delete(stmt *pg_query.DeleteStmt, parent *scope) *relation {
	sc := &scope{parent: parent, ctes: map[string][]string{}}
	r.withClause(stmt.GetWithClause(), sc)
	written := stmt.GetRelation().GetRelname()
	rel := r.relRef(stmt.GetRelation())
	src := &source{name: written, rel: rel, known: rel != nil, emit: stmt.GetRelation().GetRelname()}
	r.aliasSource(src, stmt.GetRelation().GetAlias())
	sc.sources = append(sc.sources, src)
	for _, item := range stmt.GetUsingClause() {
		r.fromItem(item, sc)
	}
	r.walk(stmt.GetWhereClause(), sc)
	for _, node := range stmt.GetReturningList() {
		r.walk(node, sc)
	}
	return rel
}

// ---- small helpers ---------------------------------------------------------

func itoa(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return digits[i : i+1]
	}
	return itoa(i/10) + digits[i%10:i%10+1]
}

// quoteIdent renders an identifier for a string that PostgreSQL parses as a
// name (nextval('...')): bare when it reads back as the same identifier.
func quoteIdent(name string) string {
	plain := name != ""
	for i := 0; i < len(name); i++ {
		c := name[i]
		letter := c == '_' || c >= 'a' && c <= 'z'
		digit := i > 0 && c >= '0' && c <= '9'
		if !letter && !digit {
			plain = false
			break
		}
	}
	if plain {
		return name
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
