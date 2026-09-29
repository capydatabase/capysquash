package normalize

import (
	"slices"
	"strings"

	"github.com/capydatabase/capysquash/internal/pgnames"
	"github.com/capydatabase/capysquash/internal/privileges"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// rewriter handles one statement: it applies the statement to the
// simulation and, in the second pass, rewrites the names it uses.
type rewriter struct {
	n       *Normalizer
	s       *state
	rewrite bool
	sql     string // first line of the statement, for warnings

	// own is the table whose own statement this is: it names the table and
	// its columns as they are at this point of the history.
	own *relation
	// qualify forces schema-qualified names (elements of CREATE SCHEMA,
	// which run with that schema first in the search path).
	qualify bool
	subject any
}

func (r *rewriter) keep(node *pg_query.Node) []result {
	return []result{{node: node, own: r.own, subject: r.subject}}
}

func removed() []result {
	return []result{{remove: true}}
}

// statement applies and rewrites one statement.
func (r *rewriter) statement(node *pg_query.Node) []result {
	switch n := node.GetNode().(type) {
	case *pg_query.Node_CreateSchemaStmt:
		return r.createSchema(n.CreateSchemaStmt)
	case *pg_query.Node_RenameStmt:
		return r.renameStmt(node, n.RenameStmt)
	case *pg_query.Node_AlterObjectSchemaStmt:
		return r.setSchema(node, n.AlterObjectSchemaStmt)
	case *pg_query.Node_DropStmt:
		return r.dropStmt(node, n.DropStmt)
	case *pg_query.Node_CreateStmt:
		r.createTable(n.CreateStmt)
	case *pg_query.Node_CreateForeignTableStmt:
		r.createTable(n.CreateForeignTableStmt.GetBaseStmt())
	case *pg_query.Node_AlterTableStmt:
		r.alterTable(n.AlterTableStmt)
	case *pg_query.Node_IndexStmt:
		r.createIndex(n.IndexStmt)
	case *pg_query.Node_ViewStmt:
		r.createView(n.ViewStmt)
	case *pg_query.Node_CreateTableAsStmt:
		r.createTableAs(n.CreateTableAsStmt)
	case *pg_query.Node_CreateSeqStmt:
		r.createSequence(n.CreateSeqStmt)
	case *pg_query.Node_AlterSeqStmt:
		r.alterSequence(n.AlterSeqStmt)
	case *pg_query.Node_CompositeTypeStmt:
		r.createComposite(n.CompositeTypeStmt)
	case *pg_query.Node_CreateEnumStmt:
		r.createEnum(n.CreateEnumStmt)
	case *pg_query.Node_AlterEnumStmt:
		if r.alterEnum(n.AlterEnumStmt) {
			return removed()
		}
	case *pg_query.Node_CreateDomainStmt:
		r.createDomain(n.CreateDomainStmt)
	case *pg_query.Node_AlterDomainStmt:
		r.alterDomain(n.AlterDomainStmt)
	case *pg_query.Node_CreateRangeStmt:
		r.createRange(n.CreateRangeStmt)
	case *pg_query.Node_DefineStmt:
		r.defineStmt(n.DefineStmt)
	case *pg_query.Node_CreateFunctionStmt:
		r.createFunction(n.CreateFunctionStmt)
	case *pg_query.Node_CreateTrigStmt:
		r.createTrigger(n.CreateTrigStmt)
	case *pg_query.Node_CreatePolicyStmt:
		written := n.CreatePolicyStmt.GetTable().GetRelname()
		rel := r.relRef(n.CreatePolicyStmt.GetTable())
		r.subject = rel
		sc := r.tableScope(written, rel)
		r.walk(n.CreatePolicyStmt.GetQual(), sc)
		r.walk(n.CreatePolicyStmt.GetWithCheck(), sc)
	case *pg_query.Node_AlterPolicyStmt:
		written := n.AlterPolicyStmt.GetTable().GetRelname()
		rel := r.relRef(n.AlterPolicyStmt.GetTable())
		r.subject = rel
		sc := r.tableScope(written, rel)
		r.walk(n.AlterPolicyStmt.GetQual(), sc)
		r.walk(n.AlterPolicyStmt.GetWithCheck(), sc)
	case *pg_query.Node_RuleStmt:
		rel := r.relRef(n.RuleStmt.GetRelation())
		r.subject = rel
		sc := r.rowScope(rel)
		r.walk(n.RuleStmt.GetWhereClause(), sc)
		for _, action := range n.RuleStmt.GetActions() {
			r.walk(action, sc)
		}
	case *pg_query.Node_CommentStmt:
		r.comment(n.CommentStmt)
	case *pg_query.Node_InsertStmt:
		r.subject = r.insert(n.InsertStmt, nil)
	case *pg_query.Node_UpdateStmt:
		r.subject = r.update(n.UpdateStmt, nil)
	case *pg_query.Node_DeleteStmt:
		r.subject = r.delete(n.DeleteStmt, nil)
	case *pg_query.Node_MergeStmt:
		r.subject = r.relRef(n.MergeStmt.GetRelation())
		r.children(n.MergeStmt, nil)
	case *pg_query.Node_SelectStmt:
		r.selectStmt(n.SelectStmt, nil)
	case *pg_query.Node_CreateStatsStmt:
		r.createStatistics(n.CreateStatsStmt)
	case *pg_query.Node_CreateExtensionStmt:
		r.createExtension(n.CreateExtensionStmt)
	case *pg_query.Node_VariableSetStmt:
		r.variableSet(n.VariableSetStmt)
	case *pg_query.Node_DoStmt:
		r.doBlock(n.DoStmt)
	case *pg_query.Node_RefreshMatViewStmt:
		r.subject = r.relRef(n.RefreshMatViewStmt.GetRelation())
	default:
		r.walk(node, nil)
	}
	return r.keep(node)
}

// ---- schemas -------------------------------------------------------------

func (r *rewriter) createSchema(stmt *pg_query.CreateSchemaStmt) []result {
	name := stmt.GetSchemaname()
	if name == "" && stmt.GetAuthrole() != nil {
		name = stmt.GetAuthrole().GetRolename()
	}
	elements := stmt.GetSchemaElts()
	stmt.SchemaElts = nil

	if existing := r.s.findSchema(name); existing != nil && stmt.GetIfNotExists() {
		// A no-op in the history. On a schema the history did not create
		// the baseline repeats it: the target database may lack the schema.
		if !existing.created && r.rewrite {
			if sql, err := deparse(&pg_query.Node{Node: &pg_query.Node_CreateSchemaStmt{CreateSchemaStmt: stmt}}); err == nil {
				r.n.schemaOps = append(r.n.schemaOps, sql)
			}
		}
		return removed()
	}

	sc := r.s.newSchema(name, true)
	if r.rewrite {
		if stmt.GetSchemaname() != "" || sc.finalName() != name {
			stmt.Schemaname = sc.finalName()
		}
		if sql, err := deparse(&pg_query.Node{Node: &pg_query.Node_CreateSchemaStmt{CreateSchemaStmt: stmt}}); err == nil {
			r.n.schemaCreates = append(r.n.schemaCreates, created{sql: sql, schema: sc})
		} else {
			r.n.warn("CREATE SCHEMA %s could not be rendered and is not in the baseline: %v", name, err)
		}
	}

	// Elements become statements of their own, qualified with the schema
	// they are created in. Privilege statements among them belong to the
	// privilege model, which reads them from the original CREATE SCHEMA.
	saved := slices.Clone(r.s.searchPath)
	r.s.searchPath = append([]string{name}, saved...)
	defer func() { r.s.searchPath = saved }()
	var out []result
	for _, element := range elements {
		if privileges.Owns(element) {
			continue
		}
		child := &rewriter{n: r.n, s: r.s, rewrite: r.rewrite, sql: r.sql, qualify: true}
		qualifyElement(element, name)
		for _, res := range child.statement(element) {
			res.split = true
			out = append(out, res)
		}
	}
	return out
}

// qualifyElement names the schema a CREATE SCHEMA element creates its
// object in, which the element leaves implicit.
func qualifyElement(node *pg_query.Node, schemaName string) {
	var rv *pg_query.RangeVar
	switch n := node.GetNode().(type) {
	case *pg_query.Node_CreateStmt:
		rv = n.CreateStmt.GetRelation()
	case *pg_query.Node_ViewStmt:
		rv = n.ViewStmt.GetView()
	case *pg_query.Node_CreateSeqStmt:
		rv = n.CreateSeqStmt.GetSequence()
	}
	if rv != nil && rv.GetSchemaname() == "" {
		rv.Schemaname = schemaName
	}
}

// ---- renames -------------------------------------------------------------

func (r *rewriter) renameStmt(node *pg_query.Node, stmt *pg_query.RenameStmt) []result {
	switch stmt.GetRenameType() {
	case pg_query.ObjectType_OBJECT_SCHEMA:
		sc := r.s.findSchema(stmt.GetSubname())
		if sc == nil {
			return r.keep(node)
		}
		r.noteRename(sc.name)
		if !sc.created && r.rewrite {
			if sql, err := deparse(node); err == nil {
				r.n.schemaOps = append(r.n.schemaOps, sql)
			}
		}
		sc.name = stmt.GetNewname()
		sc.renamed = true
		return removed()

	case pg_query.ObjectType_OBJECT_TABLE, pg_query.ObjectType_OBJECT_VIEW, pg_query.ObjectType_OBJECT_MATVIEW,
		pg_query.ObjectType_OBJECT_FOREIGN_TABLE, pg_query.ObjectType_OBJECT_SEQUENCE, pg_query.ObjectType_OBJECT_INDEX:
		rel := r.findRelation(stmt.GetRelation())
		if rel == nil {
			r.walk(node, nil)
			return r.keep(node)
		}
		r.noteRename(rel.name)
		if table := ownerTable(rel); table != nil {
			r.own, r.subject = table, table
			r.relRef(stmt.GetRelation())
			rel.name = stmt.GetNewname()
			table.renamed = true
			return r.keep(node)
		}
		rel.name = stmt.GetNewname()
		rel.renamed = true
		return removed()

	case pg_query.ObjectType_OBJECT_COLUMN:
		rel := r.findRelation(stmt.GetRelation())
		if rel == nil {
			r.walk(node, nil)
			return r.keep(node)
		}
		col := rel.column(stmt.GetSubname())
		if rel.kind == relTable {
			r.own, r.subject = rel, rel
			r.relRef(stmt.GetRelation())
			if col != nil {
				col.name = stmt.GetNewname()
			}
			rel.renamed = true
			return r.keep(node)
		}
		if col == nil {
			r.n.warn("%s: column %s of %s is not known; the rename is not carried into the baseline", r.sql, stmt.GetSubname(), rel.name)
			return removed()
		}
		col.name = stmt.GetNewname()
		return removed()

	case pg_query.ObjectType_OBJECT_ATTRIBUTE:
		rv := stmt.GetRelation()
		t := r.s.findType(rv.GetSchemaname(), rv.GetRelname())
		if t == nil || t.kind != typComposite {
			r.walk(node, nil)
			return r.keep(node)
		}
		if attr := t.attribute(stmt.GetSubname()); attr != nil {
			attr.name = stmt.GetNewname()
		}
		return removed()

	case pg_query.ObjectType_OBJECT_TYPE, pg_query.ObjectType_OBJECT_DOMAIN:
		t := r.findTypeNode(stmt.GetObject())
		if t == nil {
			r.walk(node, nil)
			return r.keep(node)
		}
		if t.kind == typRange {
			r.n.warn("%s: renaming a range type is not carried into the baseline (its constructor functions keep their names)", r.sql)
			r.walk(node, nil)
			return r.keep(node)
		}
		r.noteRename(t.name)
		t.name = stmt.GetNewname()
		t.renamed = true
		return removed()

	case pg_query.ObjectType_OBJECT_TABCONSTRAINT:
		if rel := r.findRelation(stmt.GetRelation()); rel != nil && rel.kind == relTable {
			r.own, r.subject = rel, rel
		}
	}
	r.walk(node, nil)
	return r.keep(node)
}

// ownerTable returns the table whose statements carry a relation's rename:
// the table itself, or the table an implicit index or sequence belongs to.
func ownerTable(rel *relation) *relation {
	if rel.kind == relTable {
		return rel
	}
	if rel.implicit && rel.owner != nil && rel.owner.kind == relTable {
		return rel.owner
	}
	return nil
}

func (r *rewriter) noteRename(oldName string) {
	if !slices.Contains(r.n.renamedNames, oldName) {
		r.n.renamedNames = append(r.n.renamedNames, oldName)
	}
}

func (r *rewriter) setSchema(node *pg_query.Node, stmt *pg_query.AlterObjectSchemaStmt) []result {
	target := func() *schema { return r.s.schemaNamed(stmt.GetNewschema()) }
	switch stmt.GetObjectType() {
	case pg_query.ObjectType_OBJECT_TABLE, pg_query.ObjectType_OBJECT_FOREIGN_TABLE, pg_query.ObjectType_OBJECT_VIEW,
		pg_query.ObjectType_OBJECT_MATVIEW, pg_query.ObjectType_OBJECT_SEQUENCE:
		rel := r.findRelation(stmt.GetRelation())
		if rel == nil {
			break
		}
		if table := ownerTable(rel); table != nil {
			r.own, r.subject = table, table
			r.relRef(stmt.GetRelation())
			sc := target()
			r.moveRelation(rel, sc)
			table.renamed = true
			if r.rewrite {
				stmt.Newschema = sc.finalName()
			}
			return r.keep(node)
		}
		r.moveRelation(rel, target())
		rel.renamed = true
		return removed()
	case pg_query.ObjectType_OBJECT_TYPE, pg_query.ObjectType_OBJECT_DOMAIN:
		t := r.findTypeNode(stmt.GetObject())
		if t == nil || t.kind == typRange {
			break
		}
		t.schema = target()
		t.renamed = true
		return removed()
	case pg_query.ObjectType_OBJECT_FUNCTION, pg_query.ObjectType_OBJECT_PROCEDURE, pg_query.ObjectType_OBJECT_ROUTINE:
		owa := stmt.GetObject().GetObjectWithArgs()
		schemaName, name := splitName(stringList(owa.GetObjname()))
		if routine := r.findRoutine(schemaName, name); routine != nil {
			routine.schema = target()
		}
	}
	r.walk(node, nil)
	if r.rewrite {
		if sc := r.s.findSchema(stmt.GetNewschema()); sc != nil {
			stmt.Newschema = sc.finalName()
		}
	}
	return r.keep(node)
}

// moveRelation moves a relation, with the indexes and sequences that
// belong to it, to another schema.
func (r *rewriter) moveRelation(rel *relation, sc *schema) {
	rel.schema = sc
	for _, other := range r.s.relations {
		if !other.dead && other.owner == rel {
			other.schema = sc
		}
	}
}

// ---- drops ---------------------------------------------------------------

func (r *rewriter) dropStmt(node *pg_query.Node, stmt *pg_query.DropStmt) []result {
	switch stmt.GetRemoveType() {
	case pg_query.ObjectType_OBJECT_SCHEMA:
		return r.dropSchemas(stmt)
	case pg_query.ObjectType_OBJECT_TABLE, pg_query.ObjectType_OBJECT_VIEW, pg_query.ObjectType_OBJECT_MATVIEW,
		pg_query.ObjectType_OBJECT_FOREIGN_TABLE, pg_query.ObjectType_OBJECT_SEQUENCE, pg_query.ObjectType_OBJECT_INDEX:
		var dropped []*relation
		for _, object := range stmt.GetObjects() {
			items := object.GetList().GetItems()
			rel := r.relationFromList(items)
			if rel == nil {
				continue
			}
			if table := ownerTable(rel); table != nil && len(stmt.GetObjects()) == 1 {
				r.own, r.subject = table, table
			}
			r.rewriteRelationList(items, rel)
			dropped = append(dropped, rel)
		}
		for _, rel := range dropped {
			r.s.dropRelation(rel, false)
		}
		return r.keep(node)
	case pg_query.ObjectType_OBJECT_TYPE, pg_query.ObjectType_OBJECT_DOMAIN:
		var dropped []*typ
		for _, object := range stmt.GetObjects() {
			if t := r.typeName(object.GetTypeName()); t != nil {
				dropped = append(dropped, t)
			}
		}
		for _, t := range dropped {
			r.s.dropType(t, false)
		}
		return r.keep(node)
	case pg_query.ObjectType_OBJECT_FUNCTION, pg_query.ObjectType_OBJECT_PROCEDURE, pg_query.ObjectType_OBJECT_ROUTINE:
		for _, object := range stmt.GetObjects() {
			owa := object.GetObjectWithArgs()
			schemaName, name := splitName(stringList(owa.GetObjname()))
			if routine := r.findRoutine(schemaName, name); routine != nil {
				routine.dead = true
			}
		}
	case pg_query.ObjectType_OBJECT_EXTENSION:
		for _, object := range stmt.GetObjects() {
			for _, e := range r.s.extensions {
				if !e.dead && e.name == object.GetString_().GetSval() {
					e.dead = true
				}
			}
		}
	case pg_query.ObjectType_OBJECT_TRIGGER, pg_query.ObjectType_OBJECT_POLICY, pg_query.ObjectType_OBJECT_RULE:
		for _, object := range stmt.GetObjects() {
			items := object.GetList().GetItems()
			if len(items) < 2 {
				continue
			}
			if rel := r.relationFromList(items[:len(items)-1]); rel != nil {
				r.subject = rel
				r.rewriteRelationList(items[:len(items)-1], rel)
			}
		}
		return r.keep(node)
	}
	r.walk(node, nil)
	return r.keep(node)
}

func (r *rewriter) dropSchemas(stmt *pg_query.DropStmt) []result {
	var preexisting []*pg_query.Node
	for _, object := range stmt.GetObjects() {
		name := object.GetString_().GetSval()
		sc := r.s.findSchema(name)
		if sc == nil {
			if !stmt.GetMissingOk() {
				preexisting = append(preexisting, object)
			}
			continue
		}
		if r.s.dropSchemaContents(sc) && stmt.GetBehavior() != pg_query.DropBehavior_DROP_CASCADE {
			r.n.warn("%s: schema %s still holds objects the history created; they are left out of the baseline with it", r.sql, name)
		}
		sc.dead, sc.killed = true, true
		if !sc.created {
			preexisting = append(preexisting, object)
		}
	}
	if len(preexisting) > 0 && r.rewrite {
		stmt.Objects = preexisting
		if sql, err := deparse(&pg_query.Node{Node: &pg_query.Node_DropStmt{DropStmt: stmt}}); err == nil {
			r.n.schemaOps = append(r.n.schemaOps, sql)
		}
	}
	return removed()
}

// ---- tables --------------------------------------------------------------

func (r *rewriter) createTable(stmt *pg_query.CreateStmt) {
	if stmt == nil {
		return
	}
	rv := stmt.GetRelation()
	if existing := r.findRelation(rv); existing != nil && stmt.GetIfNotExists() {
		r.own, r.subject = existing, existing
		r.relRef(rv)
		return
	}
	sc := r.s.creationSchema(rv.GetSchemaname())
	parents := make([]*relation, 0, len(stmt.GetInhRelations()))
	for _, parent := range stmt.GetInhRelations() {
		parents = append(parents, r.relRef(parent.GetRangeVar()))
	}
	var ofType *typ
	if stmt.GetOfTypename() != nil {
		ofType = r.typeName(stmt.GetOfTypename())
	}

	written := rv.GetRelname()
	table := r.s.newRelation(relTable, sc, rv.GetRelname())
	r.own, r.subject = table, table
	r.nameCreated(rv, table)

	// Inherited and partition-parent columns come first, then the type's
	// attributes, then the columns written here.
	for _, parent := range parents {
		if parent == nil {
			continue
		}
		for _, col := range parent.liveColumns() {
			if table.column(col.name) == nil {
				r.s.newColumn(table, col.name, col.typ)
			}
		}
	}
	if ofType != nil {
		for _, attr := range ofType.attrs {
			if !attr.dead {
				r.s.newColumn(table, attr.name, attr.typ)
			}
		}
	}
	sc2 := r.tableScope(written, table)
	var indexes []indexConstraint
	var constraints []*pg_query.Constraint
	for _, element := range stmt.GetTableElts() {
		switch {
		case element.GetColumnDef() != nil:
			def := element.GetColumnDef()
			col := table.column(def.GetColname())
			t := r.typeName(def.GetTypeName())
			if col == nil {
				col = r.s.newColumn(table, def.GetColname(), t)
			} else if t != nil {
				col.typ = t
			}
			r.columnDef(def, table, col, sc2, &indexes)
			r.walk(def.GetCollClause(), nil)
		case element.GetConstraint() != nil:
			switch c := element.GetConstraint(); c.GetContype() {
			case pg_query.ConstrType_CONSTR_PRIMARY, pg_query.ConstrType_CONSTR_UNIQUE, pg_query.ConstrType_CONSTR_EXCLUSION:
				indexes = append(indexes, indexConstraint{c: c})
			default:
				constraints = append(constraints, c)
			}
		case element.GetTableLikeClause() != nil:
			if source := r.relRef(element.GetTableLikeClause().GetRelation()); source != nil {
				for _, col := range source.liveColumns() {
					r.s.newColumn(table, col.name, col.typ)
				}
			}
		default:
			r.walk(element, sc2)
		}
	}
	for _, c := range constraints {
		r.constraint(c, table, nil, sc2)
	}
	r.indexConstraints(table, sc2, indexes)
	if spec := stmt.GetPartspec(); spec != nil {
		r.walk(spec, sc2)
	}
	r.walk(stmt.GetPartbound(), nil)
}

// indexConstraint is a PRIMARY KEY, UNIQUE or EXCLUDE constraint whose
// index PostgreSQL creates once the columns exist; column is set for one
// written on a column.
type indexConstraint struct {
	c      *pg_query.Constraint
	column *column
}

// columnDef rewrites a column definition's constraints and creates the
// sequence a serial or identity column gets. Index constraints are
// collected: PostgreSQL creates their indexes after the columns.
func (r *rewriter) columnDef(def *pg_query.ColumnDef, table *relation, col *column, sc *scope, indexes *[]indexConstraint) {
	if isSerial(def.GetTypeName()) {
		r.implicitSequence(table, col.name, "")
	}
	for _, node := range def.GetConstraints() {
		c := node.GetConstraint()
		if c == nil {
			continue
		}
		switch c.GetContype() {
		case pg_query.ConstrType_CONSTR_PRIMARY, pg_query.ConstrType_CONSTR_UNIQUE, pg_query.ConstrType_CONSTR_EXCLUSION:
			*indexes = append(*indexes, indexConstraint{c: c, column: col})
			continue
		}
		r.constraint(c, table, col, sc)
	}
}

// indexConstraints creates the indexes of a statement's index constraints,
// the primary key first, as PostgreSQL does.
func (r *rewriter) indexConstraints(table *relation, sc *scope, indexes []indexConstraint) {
	slices.SortStableFunc(indexes, func(a, b indexConstraint) int {
		ap, bp := a.c.GetContype() == pg_query.ConstrType_CONSTR_PRIMARY, b.c.GetContype() == pg_query.ConstrType_CONSTR_PRIMARY
		switch {
		case ap && !bp:
			return -1
		case bp && !ap:
			return 1
		}
		return 0
	})
	for _, ic := range indexes {
		r.constraint(ic.c, table, ic.column, sc)
	}
}

// constraint rewrites a table or column constraint and creates the index a
// PRIMARY KEY, UNIQUE or EXCLUDE constraint gets.
func (r *rewriter) constraint(c *pg_query.Constraint, table *relation, col *column, sc *scope) {
	switch c.GetContype() {
	case pg_query.ConstrType_CONSTR_DEFAULT:
		if col != nil {
			r.enumLiteral(c.GetRawExpr(), col.typ)
		}
		r.walk(c.GetRawExpr(), sc)
	case pg_query.ConstrType_CONSTR_CHECK, pg_query.ConstrType_CONSTR_GENERATED:
		r.walk(c.GetRawExpr(), sc)
	case pg_query.ConstrType_CONSTR_IDENTITY:
		if col != nil {
			r.implicitSequence(table, col.name, sequenceNameOption(c.GetOptions()))
		}
		r.walk(c, sc)
	case pg_query.ConstrType_CONSTR_FOREIGN:
		target := r.relRef(c.GetPktable())
		r.columnNames(c.GetFkAttrs(), table)
		if target != nil {
			r.columnNames(c.GetPkAttrs(), target)
		}
		r.walk(c.GetWhereClause(), sc)
	case pg_query.ConstrType_CONSTR_PRIMARY, pg_query.ConstrType_CONSTR_UNIQUE, pg_query.ConstrType_CONSTR_EXCLUSION:
		keys := stringList(c.GetKeys())
		if len(keys) == 0 && col != nil && c.GetContype() != pg_query.ConstrType_CONSTR_EXCLUSION {
			keys = []string{col.name}
		}
		include := stringList(c.GetIncluding())
		var columns []string
		label := "key"
		switch c.GetContype() {
		case pg_query.ConstrType_CONSTR_PRIMARY:
			label = "pkey"
		case pg_query.ConstrType_CONSTR_EXCLUSION:
			label = "excl"
			for _, pair := range c.GetExclusions() {
				items := pair.GetList().GetItems()
				if len(items) > 0 {
					columns = append(columns, items[0].GetIndexElem().GetName())
				}
			}
		}
		columns = append(append(columns, keys...), include...)
		r.columnNames(c.GetKeys(), table)
		r.columnNames(c.GetIncluding(), table)
		for _, pair := range c.GetExclusions() {
			r.walk(pair, sc)
		}
		r.walk(c.GetWhereClause(), sc)
		if c.GetIndexname() != "" {
			return // USING INDEX: the index exists already
		}
		name := c.GetConname()
		if name == "" {
			second := pgnames.IndexNameAddition(pgnames.IndexColumnNames(columns))
			if label == "pkey" {
				second = ""
			}
			name = pgnames.ChooseRelationName(table.name, second, label, func(candidate string) bool {
				return r.s.relationTaken(table.schema, candidate)
			})
		}
		index := r.s.newRelation(relIndex, table.schema, name)
		index.implicit, index.owner = true, table
	default:
		r.walk(c, sc)
	}
}

// implicitSequence records the sequence PostgreSQL creates for a serial or
// identity column.
func (r *rewriter) implicitSequence(table *relation, column, explicit string) {
	name := explicit
	if name == "" {
		name = pgnames.ChooseRelationName(table.name, column, "seq", func(candidate string) bool {
			return r.s.relationTaken(table.schema, candidate)
		})
	}
	seq := r.s.newRelation(relSequence, table.schema, name)
	seq.implicit, seq.owner = true, table
}

func isSerial(tn *pg_query.TypeName) bool {
	names := stringList(tn.GetNames())
	if len(names) != 1 {
		return false
	}
	switch names[0] {
	case "serial", "serial4", "bigserial", "serial8", "smallserial", "serial2":
		return true
	}
	return false
}

func sequenceNameOption(options []*pg_query.Node) string {
	for _, option := range options {
		if def := option.GetDefElem(); def.GetDefname() == "sequence_name" {
			_, name := splitName(nameParts(def.GetArg()))
			return name
		}
	}
	return ""
}

func (r *rewriter) alterTable(stmt *pg_query.AlterTableStmt) {
	rv := stmt.GetRelation()
	if stmt.GetObjtype() == pg_query.ObjectType_OBJECT_TYPE {
		r.alterComposite(stmt)
		return
	}
	rel := r.findRelation(rv)
	if rel != nil {
		if table := ownerTable(rel); table != nil {
			r.own = table
		}
		r.subject = rel
	}
	written := rv.GetRelname()
	r.relRef(rv)
	if rel == nil {
		for _, cmd := range stmt.GetCmds() {
			r.walk(cmd, nil)
		}
		return
	}
	sc := r.tableScope(written, rel)
	for _, node := range stmt.GetCmds() {
		cmd := node.GetAlterTableCmd()
		if cmd == nil {
			continue
		}
		col := rel.column(cmd.GetName())
		switch cmd.GetSubtype() {
		case pg_query.AlterTableType_AT_AddColumn, pg_query.AlterTableType_AT_AddColumnToView:
			def := cmd.GetDef().GetColumnDef()
			if def == nil {
				break
			}
			if existing := rel.column(def.GetColname()); existing != nil && cmd.GetMissingOk() {
				r.walk(def, sc)
				break
			}
			t := r.typeName(def.GetTypeName())
			added := r.s.newColumn(rel, def.GetColname(), t)
			var indexes []indexConstraint
			r.columnDef(def, rel, added, sc, &indexes)
			r.walk(def.GetCollClause(), nil)
			r.indexConstraints(rel, sc, indexes)
			continue
		case pg_query.AlterTableType_AT_DropColumn:
			r.columnName(&cmd.Name, rel)
			if col != nil {
				col.dead = true
			}
			continue
		case pg_query.AlterTableType_AT_AlterColumnType:
			r.columnName(&cmd.Name, rel)
			if def := cmd.GetDef().GetColumnDef(); def != nil {
				if t := r.typeName(def.GetTypeName()); col != nil {
					col.typ = t
				}
				r.walk(def.GetRawDefault(), sc)
			}
			continue
		case pg_query.AlterTableType_AT_ColumnDefault:
			r.columnName(&cmd.Name, rel)
			if col != nil {
				r.enumLiteral(cmd.GetDef(), col.typ)
			}
			r.walk(cmd.GetDef(), sc)
			continue
		case pg_query.AlterTableType_AT_AddConstraint:
			if c := cmd.GetDef().GetConstraint(); c != nil {
				r.constraint(c, rel, nil, sc)
				continue
			}
		case pg_query.AlterTableType_AT_AddIdentity:
			r.columnName(&cmd.Name, rel)
			if c := cmd.GetDef().GetConstraint(); c != nil && col != nil {
				r.implicitSequence(rel, col.name, sequenceNameOption(c.GetOptions()))
			}
			continue
		case pg_query.AlterTableType_AT_DropIdentity:
			r.columnName(&cmd.Name, rel)
			if col != nil {
				for _, other := range r.s.relations {
					if !other.dead && other.owner == rel && other.kind == relSequence && other.implicit {
						r.s.dropRelation(other, false)
						break
					}
				}
			}
			continue
		}
		if cmd.GetName() != "" && col != nil {
			r.columnName(&cmd.Name, rel)
		}
		r.walk(cmd.GetDef(), sc)
	}
}

// ---- indexes, views, sequences --------------------------------------------

func (r *rewriter) createIndex(stmt *pg_query.IndexStmt) {
	rv := stmt.GetRelation()
	rel := r.findRelation(rv)
	var timeTable string
	var columns []string
	if rel != nil {
		timeTable = rel.name
		for _, node := range append(slices.Clone(stmt.GetIndexParams()), stmt.GetIndexIncludingParams()...) {
			elem := node.GetIndexElem()
			switch {
			case elem.GetIndexcolname() != "":
				columns = append(columns, elem.GetIndexcolname())
			case elem.GetExpr() != nil:
				// PostgreSQL names an expression column the way it names an
				// output column (FigureIndexColname), "expr" failing that.
				columns = append(columns, naturalName(elem.GetExpr()))
			default:
				columns = append(columns, elem.GetName())
			}
		}
	}
	changed := false
	if rel != nil {
		changed = r.rewrite && (rel.finalName() != rel.name || columnsRenamed(rel, stmt))
	}
	written := rv.GetRelname()
	r.relRef(rv)
	r.subject = rel
	sc := r.tableScope(written, rel)
	for _, node := range append(slices.Clone(stmt.GetIndexParams()), stmt.GetIndexIncludingParams()...) {
		elem := node.GetIndexElem()
		if elem.GetName() != "" && rel != nil {
			r.columnName(&elem.Name, rel)
		}
		r.walk(elem.GetExpr(), sc)
	}
	r.walk(stmt.GetWhereClause(), sc)
	if rel == nil {
		return
	}
	name := stmt.GetIdxname()
	implicit := name == ""
	if implicit {
		name = pgnames.ChooseRelationName(timeTable, pgnames.IndexNameAddition(pgnames.IndexColumnNames(columns)), "idx", func(candidate string) bool {
			return r.s.relationTaken(rel.schema, candidate)
		})
	}
	if stmt.GetIfNotExists() && r.s.relationTaken(rel.schema, name) {
		return
	}
	index := r.s.newRelation(relIndex, rel.schema, name)
	index.owner = rel
	r.subject = index
	if r.rewrite && (!implicit || changed || index.finalName() != name) {
		stmt.Idxname = index.finalName()
	}
}

// columnsRenamed reports an index column whose name changes by the end.
func columnsRenamed(rel *relation, stmt *pg_query.IndexStmt) bool {
	for _, node := range append(slices.Clone(stmt.GetIndexParams()), stmt.GetIndexIncludingParams()...) {
		if col := rel.column(node.GetIndexElem().GetName()); col != nil && col.finalName() != col.name {
			return true
		}
	}
	return false
}

func (r *rewriter) createView(stmt *pg_query.ViewStmt) {
	rv := stmt.GetView()
	view := r.findRelation(rv)
	if view == nil || !stmt.GetReplace() {
		view = r.s.newRelation(relView, r.s.creationSchema(rv.GetSchemaname()), rv.GetRelname())
	}
	r.subject = view
	r.nameCreated(rv, view)
	sel := stmt.GetQuery().GetSelectStmt()
	names := r.selectStmt(sel, nil)
	aliases := stringList(stmt.GetAliases())
	for i := range aliases {
		if i < len(names) {
			names[i] = aliases[i]
		}
	}
	r.viewColumns(view, names, sel, stmt.GetAliases())
}

// viewColumns records a view's columns and, in the second pass, gives them
// the names they end with.
func (r *rewriter) viewColumns(view *relation, names []string, sel *pg_query.SelectStmt, aliases []*pg_query.Node) {
	for i, name := range names {
		if i < len(view.columns) {
			continue
		}
		r.s.newColumn(view, name, nil)
	}
	if !r.rewrite {
		return
	}
	for i, col := range view.columns {
		final := col.finalName()
		if i < len(names) && final == names[i] {
			continue
		}
		switch {
		case i < len(aliases):
			aliases[i] = pg_query.MakeStrNode(final)
		case sel != nil && sel.GetOp() == pg_query.SetOperation_SETOP_NONE && i < len(sel.GetTargetList()):
			if rt := sel.GetTargetList()[i].GetResTarget(); rt != nil {
				rt.Name = final
			}
		default:
			r.n.warn("%s: column %s of view %s is renamed later and cannot be renamed in its definition", r.sql, col.name, view.name)
		}
	}
}

func (r *rewriter) createTableAs(stmt *pg_query.CreateTableAsStmt) {
	into := stmt.GetInto()
	rv := into.GetRel()
	if existing := r.findRelation(rv); existing != nil && stmt.GetIfNotExists() {
		r.subject = existing
		r.relRef(rv)
		return
	}
	kind := relTable
	if stmt.GetObjtype() == pg_query.ObjectType_OBJECT_MATVIEW {
		kind = relMatview
	}
	rel := r.s.newRelation(kind, r.s.creationSchema(rv.GetSchemaname()), rv.GetRelname())
	r.subject = rel
	if kind == relTable {
		r.own = rel
	}
	r.nameCreated(rv, rel)
	sel := stmt.GetQuery().GetSelectStmt()
	names := r.selectStmt(sel, nil)
	aliases := stringList(into.GetColNames())
	for i := range aliases {
		if i < len(names) {
			names[i] = aliases[i]
		}
	}
	if kind == relTable {
		for _, name := range names {
			r.s.newColumn(rel, name, nil)
		}
		return
	}
	r.viewColumns(rel, names, sel, into.GetColNames())
}

func (r *rewriter) createSequence(stmt *pg_query.CreateSeqStmt) {
	rv := stmt.GetSequence()
	if existing := r.findRelation(rv); existing != nil && stmt.GetIfNotExists() {
		r.subject = existing
		r.relRef(rv)
		return
	}
	seq := r.s.newRelation(relSequence, r.s.creationSchema(rv.GetSchemaname()), rv.GetRelname())
	r.subject = seq
	r.nameCreated(rv, seq)
	r.sequenceOptions(stmt.GetOptions())
}

func (r *rewriter) alterSequence(stmt *pg_query.AlterSeqStmt) {
	rel := r.findRelation(stmt.GetSequence())
	if rel != nil {
		if table := ownerTable(rel); table != nil {
			r.own = table
		}
		r.subject = rel
	}
	r.relRef(stmt.GetSequence())
	r.sequenceOptions(stmt.GetOptions())
}

// sequenceOptions rewrites OWNED BY table.column.
func (r *rewriter) sequenceOptions(options []*pg_query.Node) {
	for _, option := range options {
		def := option.GetDefElem()
		if def.GetDefname() != "owned_by" {
			r.walk(option, nil)
			continue
		}
		items := def.GetArg().GetList().GetItems()
		if len(items) < 2 {
			continue
		}
		rel := r.relationFromList(items[:len(items)-1])
		if rel == nil {
			continue
		}
		r.rewriteRelationList(items[:len(items)-1], rel)
		if last := items[len(items)-1].GetString_(); last != nil {
			r.columnName(&last.Sval, rel)
		}
	}
}

// ---- types ---------------------------------------------------------------

func (r *rewriter) createComposite(stmt *pg_query.CompositeTypeStmt) {
	rv := stmt.GetTypevar()
	t := r.s.newType(typComposite, r.s.creationSchema(rv.GetSchemaname()), rv.GetRelname())
	r.subject = t
	if r.rewrite {
		rv.Schemaname = r.schemaText(rv.GetSchemaname(), t.schema, t.finalSchema())
		rv.Relname = t.finalName()
	}
	for _, node := range stmt.GetColdeflist() {
		def := node.GetColumnDef()
		attr := r.s.newAttribute(t, def.GetColname(), r.typeName(def.GetTypeName()))
		if r.rewrite {
			def.Colname = attr.finalName()
		}
		r.walk(def.GetCollClause(), nil)
	}
}

// alterComposite handles ALTER TYPE ... ADD/DROP/ALTER ATTRIBUTE.
func (r *rewriter) alterComposite(stmt *pg_query.AlterTableStmt) {
	rv := stmt.GetRelation()
	t := r.s.findType(rv.GetSchemaname(), rv.GetRelname())
	if t == nil {
		for _, cmd := range stmt.GetCmds() {
			r.walk(cmd, nil)
		}
		return
	}
	r.subject = t
	if r.rewrite {
		rv.Schemaname = r.schemaText(rv.GetSchemaname(), t.schema, t.finalSchema())
		rv.Relname = t.finalName()
	}
	for _, node := range stmt.GetCmds() {
		cmd := node.GetAlterTableCmd()
		attr := t.attribute(cmd.GetName())
		switch cmd.GetSubtype() {
		case pg_query.AlterTableType_AT_AddColumn:
			if def := cmd.GetDef().GetColumnDef(); def != nil {
				added := r.s.newAttribute(t, def.GetColname(), r.typeName(def.GetTypeName()))
				if r.rewrite {
					def.Colname = added.finalName()
				}
			}
			continue
		case pg_query.AlterTableType_AT_DropColumn:
			if attr != nil {
				if r.rewrite {
					cmd.Name = attr.finalName()
				}
				attr.dead = true
			}
			continue
		}
		if attr != nil && r.rewrite {
			cmd.Name = attr.finalName()
		}
		r.walk(cmd.GetDef(), nil)
	}
}

func (r *rewriter) createEnum(stmt *pg_query.CreateEnumStmt) {
	schemaName, name := splitName(stringList(stmt.GetTypeName()))
	t := r.s.newType(typEnum, r.s.creationSchema(schemaName), name)
	r.subject = t
	stmt.TypeName = r.typeList(stmt.GetTypeName(), t, schemaName)
	for _, node := range stmt.GetVals() {
		value := node.GetString_()
		if value == nil {
			continue
		}
		l := r.s.newLabel(t, value.GetSval(), len(t.labels))
		if r.rewrite {
			value.Sval = l.finalName()
		}
	}
}

// alterEnum handles ADD VALUE and RENAME VALUE; it reports a statement the
// baseline does not need (a RENAME VALUE of an enum the history creates).
func (r *rewriter) alterEnum(stmt *pg_query.AlterEnumStmt) bool {
	schemaName, _ := splitName(stringList(stmt.GetTypeName()))
	t := r.findTypeNode(&pg_query.Node{Node: &pg_query.Node_List{List: &pg_query.List{Items: stmt.GetTypeName()}}})
	if t == nil || t.kind != typEnum {
		return false
	}
	r.subject = t
	if stmt.GetOldVal() != "" {
		if l := t.label(stmt.GetOldVal()); l != nil {
			l.name = stmt.GetNewVal()
		}
		return true
	}
	stmt.TypeName = r.typeList(stmt.GetTypeName(), t, schemaName)
	if existing := t.label(stmt.GetNewVal()); existing != nil {
		if r.rewrite {
			stmt.NewVal = existing.finalName()
		}
		return false
	}
	at := len(t.labels)
	if neighbor := t.label(stmt.GetNewValNeighbor()); neighbor != nil {
		at = slices.Index(t.labels, neighbor)
		if stmt.GetNewValIsAfter() {
			at++
		}
		if r.rewrite {
			stmt.NewValNeighbor = neighbor.finalName()
		}
	}
	l := r.s.newLabel(t, stmt.GetNewVal(), at)
	if r.rewrite {
		stmt.NewVal = l.finalName()
	}
	return false
}

func (r *rewriter) createDomain(stmt *pg_query.CreateDomainStmt) {
	schemaName, name := splitName(stringList(stmt.GetDomainname()))
	base := r.typeName(stmt.GetTypeName())
	t := r.s.newType(typDomain, r.s.creationSchema(schemaName), name)
	t.base = base
	r.subject = t
	stmt.Domainname = r.typeList(stmt.GetDomainname(), t, schemaName)
	r.domainConstraints(t, name, stmt.GetConstraints())
}

// domainConstraints rewrites a domain's constraints. A CHECK without a name
// is named after the domain; when the domain is renamed later, the name it
// got is spelled out.
func (r *rewriter) domainConstraints(t *typ, timeName string, constraints []*pg_query.Node) {
	var chosen []string
	for _, node := range constraints {
		c := node.GetConstraint()
		if c == nil {
			continue
		}
		switch c.GetContype() {
		case pg_query.ConstrType_CONSTR_CHECK:
			if c.GetConname() == "" {
				name := pgnames.ChooseRelationName(timeName, "", "check", func(candidate string) bool {
					return slices.Contains(chosen, candidate)
				})
				chosen = append(chosen, name)
				if r.rewrite && t.finalName() != timeName {
					c.Conname = name
				}
			}
		case pg_query.ConstrType_CONSTR_DEFAULT:
			r.enumLiteral(c.GetRawExpr(), t.base)
		}
		r.walk(c.GetRawExpr(), nil)
	}
}

func (r *rewriter) alterDomain(stmt *pg_query.AlterDomainStmt) {
	schemaName, _ := splitName(stringList(stmt.GetTypeName()))
	t := r.findTypeNode(&pg_query.Node{Node: &pg_query.Node_List{List: &pg_query.List{Items: stmt.GetTypeName()}}})
	if t == nil {
		r.walk(stmt.GetDef(), nil)
		return
	}
	r.subject = t
	timeName := t.name
	stmt.TypeName = r.typeList(stmt.GetTypeName(), t, schemaName)
	switch stmt.GetSubtype() {
	case "C":
		r.domainConstraints(t, timeName, []*pg_query.Node{stmt.GetDef()})
	case "T":
		r.enumLiteral(stmt.GetDef(), t.base)
		r.walk(stmt.GetDef(), nil)
	default:
		r.walk(stmt.GetDef(), nil)
	}
}

func (r *rewriter) createRange(stmt *pg_query.CreateRangeStmt) {
	schemaName, name := splitName(stringList(stmt.GetTypeName()))
	t := r.s.newType(typRange, r.s.creationSchema(schemaName), name)
	r.subject = t
	stmt.TypeName = r.typeList(stmt.GetTypeName(), t, schemaName)
	for _, param := range stmt.GetParams() {
		r.walk(param, nil)
	}
}

func (r *rewriter) defineStmt(stmt *pg_query.DefineStmt) {
	if stmt.GetKind() != pg_query.ObjectType_OBJECT_TYPE {
		r.walk(stmt, nil)
		return
	}
	schemaName, name := splitName(stringList(stmt.GetDefnames()))
	t := r.s.findType(schemaName, name)
	if t == nil {
		t = r.s.newType(typBase, r.s.creationSchema(schemaName), name)
	}
	r.subject = t
	stmt.Defnames = r.typeList(stmt.GetDefnames(), t, schemaName)
	for _, def := range stmt.GetDefinition() {
		r.walk(def, nil)
	}
}

// typeList returns the name list that creates or alters a type, with the
// type's final name.
func (r *rewriter) typeList(names []*pg_query.Node, t *typ, writtenSchema string) []*pg_query.Node {
	if !r.rewrite || len(names) == 0 {
		return names
	}
	if schemaText := r.schemaText(writtenSchema, t.schema, t.finalSchema()); schemaText != "" {
		return []*pg_query.Node{pg_query.MakeStrNode(schemaText), pg_query.MakeStrNode(t.finalName())}
	}
	return []*pg_query.Node{pg_query.MakeStrNode(t.finalName())}
}

// ---- routines, triggers, comments, statistics, extensions -----------------

func (r *rewriter) createFunction(stmt *pg_query.CreateFunctionStmt) {
	schemaName, name := splitName(stringList(stmt.GetFuncname()))
	routine := r.findRoutine(schemaName, name)
	if routine == nil || !stmt.GetReplace() {
		routine = r.s.newRoutine(r.s.creationSchema(schemaName), name)
	}
	r.subject = routine
	r.schemaInList(stmt.GetFuncname())
	for _, param := range stmt.GetParameters() {
		r.walk(param, nil)
	}
	r.walk(stmt.GetReturnType(), nil)
	r.walk(stmt.GetSqlBody(), nil)
	for _, option := range stmt.GetOptions() {
		def := option.GetDefElem()
		if def.GetDefname() != "as" {
			continue
		}
		for _, part := range def.GetArg().GetList().GetItems() {
			r.checkText(part.GetString_().GetSval(), "the body of function "+name)
		}
	}
}

func (r *rewriter) findRoutine(schemaName, name string) *routine {
	for _, sc := range r.s.searchSchemas(schemaName) {
		for i := len(r.s.routines) - 1; i >= 0; i-- {
			if rt := r.s.routines[i]; !rt.dead && rt.schema == sc && rt.name == name {
				return rt
			}
		}
	}
	return nil
}

func (r *rewriter) createTrigger(stmt *pg_query.CreateTrigStmt) {
	rel := r.relRef(stmt.GetRelation())
	r.subject = rel
	r.schemaInList(stmt.GetFuncname())
	if rel != nil {
		for _, node := range stmt.GetColumns() {
			if s := node.GetString_(); s != nil {
				r.columnName(&s.Sval, rel)
			}
		}
	}
	r.walk(stmt.GetWhenClause(), r.rowScope(rel))
	for _, arg := range stmt.GetArgs() {
		r.walk(arg, nil)
	}
	r.walk(stmt.GetConstrrel(), nil)
}

func (r *rewriter) comment(stmt *pg_query.CommentStmt) {
	object := stmt.GetObject()
	switch stmt.GetObjtype() {
	case pg_query.ObjectType_OBJECT_TABLE, pg_query.ObjectType_OBJECT_VIEW, pg_query.ObjectType_OBJECT_MATVIEW,
		pg_query.ObjectType_OBJECT_FOREIGN_TABLE, pg_query.ObjectType_OBJECT_SEQUENCE, pg_query.ObjectType_OBJECT_INDEX:
		items := object.GetList().GetItems()
		if rel := r.relationFromList(items); rel != nil {
			r.subject = rel
			r.rewriteRelationList(items, rel)
		}
	case pg_query.ObjectType_OBJECT_COLUMN:
		items := object.GetList().GetItems()
		if len(items) < 2 {
			return
		}
		if rel := r.relationFromList(items[:len(items)-1]); rel != nil {
			r.subject = rel
			r.rewriteRelationList(items[:len(items)-1], rel)
			if last := items[len(items)-1].GetString_(); last != nil {
				r.columnName(&last.Sval, rel)
			}
		}
	case pg_query.ObjectType_OBJECT_TABCONSTRAINT, pg_query.ObjectType_OBJECT_TRIGGER,
		pg_query.ObjectType_OBJECT_POLICY, pg_query.ObjectType_OBJECT_RULE:
		items := object.GetList().GetItems()
		if len(items) < 2 {
			return
		}
		if rel := r.relationFromList(items[:len(items)-1]); rel != nil {
			r.subject = rel
			r.rewriteRelationList(items[:len(items)-1], rel)
		}
	case pg_query.ObjectType_OBJECT_TYPE, pg_query.ObjectType_OBJECT_DOMAIN:
		if t := r.typeName(object.GetTypeName()); t != nil {
			r.subject = t
		}
	case pg_query.ObjectType_OBJECT_SCHEMA:
		if s := object.GetString_(); s != nil {
			if sc := r.s.findSchema(s.GetSval()); sc != nil {
				r.subject = sc
				if r.rewrite {
					s.Sval = sc.finalName()
				}
			}
		}
	default:
		r.walk(object, nil)
	}
}

func (r *rewriter) createStatistics(stmt *pg_query.CreateStatsStmt) {
	var rel *relation
	written := ""
	for _, node := range stmt.GetRelations() {
		if rv := node.GetRangeVar(); rv != nil {
			written = rv.GetRelname()
			rel = r.relRef(rv)
		}
	}
	r.subject = rel
	r.schemaInList(stmt.GetDefnames())
	sc := r.tableScope(written, rel)
	for _, node := range stmt.GetExprs() {
		elem := node.GetStatsElem()
		if elem.GetName() != "" && rel != nil {
			r.columnName(&elem.Name, rel)
		}
		r.walk(elem.GetExpr(), sc)
	}
}

func (r *rewriter) createExtension(stmt *pg_query.CreateExtensionStmt) {
	schemaName := ""
	for _, option := range stmt.GetOptions() {
		def := option.GetDefElem()
		if def.GetDefname() == "schema" {
			schemaName = def.GetArg().GetString_().GetSval()
			if sc := r.s.findSchema(schemaName); sc != nil && r.rewrite {
				def.Arg = pg_query.MakeStrNode(sc.finalName())
			}
		}
	}
	for _, e := range r.s.extensions {
		if !e.dead && e.name == stmt.GetExtname() {
			r.subject = e
			return
		}
	}
	r.subject = r.s.newExtension(r.s.creationSchema(schemaName), stmt.GetExtname())
}

func (r *rewriter) variableSet(stmt *pg_query.VariableSetStmt) {
	if !strings.EqualFold(stmt.GetName(), "search_path") {
		return
	}
	if stmt.GetKind() == pg_query.VariableSetKind_VAR_RESET || stmt.GetKind() == pg_query.VariableSetKind_VAR_SET_DEFAULT {
		r.s.searchPath = []string{"public"}
		return
	}
	var path []string
	for _, arg := range stmt.GetArgs() {
		value := arg.GetAConst().GetSval().GetSval()
		if value != "" && value != "$user" && value != "pg_catalog" {
			path = append(path, value)
		}
	}
	if len(path) > 0 {
		r.s.searchPath = path
	}
}

// doBlock checks a DO block's text for names the history gives up.
func (r *rewriter) doBlock(stmt *pg_query.DoStmt) {
	for _, arg := range stmt.GetArgs() {
		if def := arg.GetDefElem(); def.GetDefname() == "as" {
			r.checkText(def.GetArg().GetString_().GetSval(), "a DO block")
		}
	}
}

// checkText warns when code kept as text (a DO block, a function body
// written as a string) names an object by a name it later gives up. Such
// text is not rewritten, as PostgreSQL does not rewrite it either, but the
// baseline runs it once the object has its final name.
func (r *rewriter) checkText(text, what string) {
	if !r.rewrite || text == "" {
		return
	}
	body := strings.ToLower(text)
	for _, name := range r.n.renamedNames {
		if containsWord(body, strings.ToLower(name)) {
			r.n.warn("%s: %s names %s, which the history renames; the text is not rewritten", r.sql, what, name)
		}
	}
}

func containsWord(text, word string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], word)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(word)
		if (start == 0 || !isIdentByte(text[start-1])) && (end == len(text) || !isIdentByte(text[end])) {
			return true
		}
		i = start + 1
	}
}

func isIdentByte(b byte) bool {
	return b == '_' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9'
}
