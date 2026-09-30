package normalize

import (
	"fmt"
	"slices"

	"github.com/capydatabase/capysquash/internal/pgnames"
	pg_query "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// PostgreSQL names a constraint or index created without a name after its
// table and columns, appending a number while the name is in use: a CHECK,
// foreign key or domain constraint avoids every constraint name of its
// schema (ChooseConstraintName), the index of a PRIMARY KEY, UNIQUE or
// EXCLUDE constraint avoids relation and constraint names (ChooseRelationName
// with isconstraint), a plain index avoids relation names. Which names are in
// use depends on everything the history ran before, and the baseline runs
// statements in another order and leaves dropped objects out, so the number
// PostgreSQL appends there can differ. The simulation therefore tracks
// constraint names too, and the rewrite spells out a chosen name whenever
// the baseline could get another one: when PostgreSQL numbered it, or when
// another object of the schema has the same name at some point of the
// history. An index created without a name always gets its name spelled
// out: the pipeline tracks indexes by name.

// constraint is a pg_constraint entry of a table or domain.
type constraint struct {
	schema     *schema
	name       string
	table      *relation // nil for a domain constraint
	domain     *typ
	columns    []*column // the table columns it involves: dropping one drops it
	references *relation // the table a foreign key references
	refColumns []*column
	index      *relation // the index of a PRIMARY KEY, UNIQUE or EXCLUDE constraint
	dead       bool
}

// nameUse keys the count of objects that have a name in a schema.
func nameUse(namespace string, sc *schema, name string) string {
	return fmt.Sprintf("%s:%d:%s", namespace, sc.id, name)
}

// useRelationName and useConstraintName count, over the whole history, the
// objects of a schema that take a name, by creation, rename or move.
func (s *state) useRelationName(sc *schema, name string) {
	if sc != nil {
		s.uses[nameUse("r", sc, name)]++
	}
}

func (s *state) useConstraintName(sc *schema, name string) {
	if sc != nil {
		s.uses[nameUse("c", sc, name)]++
	}
}

func (s *state) addConstraint(c *constraint) *constraint {
	s.constraints = append(s.constraints, c)
	s.useConstraintName(c.schema, c.name)
	return c
}

// constraintTaken reports whether a constraint of the schema has the name.
func (s *state) constraintTaken(sc *schema, name string) bool {
	for _, c := range s.constraints {
		if !c.dead && c.schema == sc && c.name == name {
			return true
		}
	}
	return false
}

// renameRelation renames a relation; an index that belongs to a constraint
// renames the constraint with it.
func (s *state) renameRelation(rel *relation, name string) {
	rel.name = name
	s.useRelationName(rel.schema, name)
	for _, c := range s.constraints {
		if !c.dead && c.index == rel {
			c.name = name
			s.useConstraintName(c.schema, name)
		}
	}
}

// renameConstraint renames a constraint and the index it owns.
func (s *state) renameConstraint(c *constraint, name string) {
	c.name = name
	s.useConstraintName(c.schema, name)
	if c.index != nil {
		c.index.name = name
		s.useRelationName(c.index.schema, name)
	}
}

func (s *state) tableConstraint(table *relation, name string) *constraint {
	for _, c := range s.constraints {
		if !c.dead && c.table == table && c.name == name {
			return c
		}
	}
	return nil
}

// dropConstraintsOf drops the constraints a dropped relation takes with it:
// its own, and the foreign keys referencing it (DROP TABLE ... CASCADE).
func (s *state) dropConstraintsOf(rel *relation) {
	for _, c := range s.constraints {
		if !c.dead && (c.table == rel || c.references == rel || c.index == rel) {
			c.dead = true
		}
	}
}

// dropConstraintsOnColumn drops the constraints that involve a dropped
// column, with the indexes they own.
func (s *state) dropConstraintsOnColumn(rel *relation, col *column) {
	for _, c := range s.constraints {
		if c.dead {
			continue
		}
		if c.table == rel && slices.Contains(c.columns, col) || c.references == rel && slices.Contains(c.refColumns, col) {
			c.dead = true
			if c.index != nil {
				s.dropRelation(c.index, false)
			}
		}
	}
}

// spellOut reports whether the rewrite writes out a name PostgreSQL chose
// (unnumbered is the name it chooses when nothing is in the way): when it
// is numbered, or when another relation (relation) or constraint
// (constraint) of the schema takes the same name somewhere in the history.
func (r *rewriter) spellOut(sc *schema, chosen, unnumbered string, relation, constraint bool) bool {
	if chosen != unnumbered {
		return true
	}
	uses := r.n.observedUses
	return relation && uses[nameUse("r", sc, chosen)] > 1 || constraint && uses[nameUse("c", sc, chosen)] > 1
}

// pendingConstraints are the constraints one CREATE TABLE or ALTER TABLE
// adds, which PostgreSQL names in this order: CHECK constraints, then the
// indexes of PRIMARY KEY (first), UNIQUE and EXCLUDE constraints, then
// foreign keys, each group in the order written.
type pendingConstraints struct {
	checks  []indexConstraint
	indexes []indexConstraint
	foreign []indexConstraint
}

func (p *pendingConstraints) add(c *pg_query.Constraint, col *column) {
	entry := indexConstraint{c: c, column: col}
	switch c.GetContype() {
	case pg_query.ConstrType_CONSTR_CHECK:
		p.checks = append(p.checks, entry)
	case pg_query.ConstrType_CONSTR_PRIMARY, pg_query.ConstrType_CONSTR_UNIQUE, pg_query.ConstrType_CONSTR_EXCLUSION:
		p.indexes = append(p.indexes, entry)
	case pg_query.ConstrType_CONSTR_FOREIGN:
		p.foreign = append(p.foreign, entry)
	}
}

// nameConstraints names the pending constraints in PostgreSQL's order and records
// them; the index constraints also get their indexes.
func (r *rewriter) nameConstraints(table *relation, sc *scope, p *pendingConstraints) {
	for _, entry := range p.checks {
		r.checkConstraint(entry.c, table)
	}
	r.indexConstraints(table, sc, p.indexes)
	for _, entry := range p.foreign {
		r.foreignKey(entry.c, table, entry.column)
	}
}

// checkConstraint names a table CHECK constraint after the one column it
// uses, or the table alone when it uses none or several.
func (r *rewriter) checkConstraint(c *pg_query.Constraint, table *relation) {
	var names []string
	walkColumnRefs(c.GetRawExpr(), func(fields []string) {
		if len(fields) > 0 && !slices.Contains(names, fields[len(fields)-1]) {
			names = append(names, fields[len(fields)-1])
		}
	})
	name := c.GetConname()
	if name == "" {
		second := ""
		if len(names) == 1 {
			second = names[0]
		}
		name = pgnames.ChooseRelationName(table.name, second, "check", func(candidate string) bool {
			return r.s.constraintTaken(table.schema, candidate)
		})
		if r.rewrite && r.spellOut(table.schema, name, pgnames.MakeObjectName(table.name, second, "check"), false, true) {
			c.Conname = name
		}
	}
	entry := &constraint{schema: table.schema, name: name, table: table}
	for _, n := range names {
		if col := table.column(n); col != nil {
			entry.columns = append(entry.columns, col)
		}
	}
	r.s.addConstraint(entry)
}

// foreignKey names a foreign key after its columns (an inline REFERENCES
// after its column).
func (r *rewriter) foreignKey(c *pg_query.Constraint, table *relation, col *column) {
	columns := stringList(c.GetFkAttrs())
	if len(columns) == 0 && col != nil {
		columns = []string{col.name}
	}
	name := c.GetConname()
	if name == "" {
		second := pgnames.IndexNameAddition(columns)
		name = pgnames.ChooseRelationName(table.name, second, "fkey", func(candidate string) bool {
			return r.s.constraintTaken(table.schema, candidate)
		})
		if r.rewrite && r.spellOut(table.schema, name, pgnames.MakeObjectName(table.name, second, "fkey"), false, true) {
			c.Conname = name
		}
	}
	entry := &constraint{schema: table.schema, name: name, table: table, references: r.s.findRelation(c.GetPktable().GetSchemaname(), c.GetPktable().GetRelname())}
	for _, n := range columns {
		if fk := table.column(n); fk != nil {
			entry.columns = append(entry.columns, fk)
		}
	}
	if entry.references != nil {
		for _, n := range stringList(c.GetPkAttrs()) {
			if pk := entry.references.column(n); pk != nil {
				entry.refColumns = append(entry.refColumns, pk)
			}
		}
	}
	r.s.addConstraint(entry)
}

// walkColumnRefs calls visit with the fields of every column reference in
// an expression.
func walkColumnRefs(m proto.Message, visit func(fields []string)) {
	if m == nil || !m.ProtoReflect().IsValid() {
		return
	}
	if cr, ok := m.(*pg_query.ColumnRef); ok {
		visit(stringList(cr.GetFields()))
		return
	}
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Message() == nil || fd.IsMap() {
			return true
		}
		if fd.IsList() {
			list := v.List()
			for i := 0; i < list.Len(); i++ {
				walkColumnRefs(list.Get(i).Message().Interface(), visit)
			}
			return true
		}
		walkColumnRefs(v.Message().Interface(), visit)
		return true
	})
}
