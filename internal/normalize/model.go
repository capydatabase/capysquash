package normalize

import (
	"slices"
)

// The simulation keeps one entity per database object the history creates
// (and per pre-existing schema it names), in creation order. Both passes
// create the same entities in the same order, so entity n of the second
// pass finds its counterpart, and with it the names the object ends the
// history with, at index n of the first pass.

// schema is a namespace.
type schema struct {
	id      int
	name    string
	created bool // by the history; false for public and other pre-existing schemas
	dead    bool
	killed  bool // dropped by DROP SCHEMA (its own or a pre-existing schema's)
	renamed bool
	final   *schema
}

// relKind tells how the baseline creates a relation. Tables keep the
// statements that create and alter them as written, with their renames:
// PostgreSQL derives the names of their constraints, indexes and sequences
// from the table and column names at creation, and those names must not
// change. Every other kind is created under its final name.
type relKind int

const (
	relTable relKind = iota // tables, partitioned and foreign tables, CREATE TABLE AS
	relView
	relMatview
	relSequence
	relIndex
)

// relation is a pg_class entry: table, view, materialized view, sequence or index.
type relation struct {
	id       int
	kind     relKind
	schema   *schema
	name     string
	created  bool
	dead     bool
	killed   bool
	renamed  bool // the table, one of its columns or one of its implicit objects is renamed or moved
	implicit bool // an index a constraint created or the sequence of a serial or identity column
	owner    *relation
	columns  []*column
	final    *relation

	// ownedBy and ownedColumn are the column a sequence belongs to (a
	// serial or identity column, or OWNED BY): dropping the column or its
	// table drops the sequence.
	ownedBy     *relation
	ownedColumn *column
}

// column is a table, view or composite type column.
type column struct {
	id    int
	name  string
	typ   *typ
	dead  bool
	final *column
}

type typKind int

const (
	typEnum typKind = iota
	typComposite
	typDomain
	typRange
	typBase
)

// typ is a user-defined type.
type typ struct {
	id      int
	kind    typKind
	schema  *schema
	name    string
	created bool
	dead    bool
	killed  bool
	renamed bool
	labels  []*label  // enum values in order
	attrs   []*column // composite attributes
	base    *typ      // domain: its base type, when the history created it
	final   *typ
}

// label is an enum value.
type label struct {
	id    int
	name  string
	final *label
}

// routine is a function or procedure; only its schema matters here.
type routine struct {
	id     int
	schema *schema
	name   string
	dead   bool
	killed bool
	final  *routine
}

// extension is an extension installed into a schema.
type extension struct {
	id     int
	name   string
	schema *schema
	dead   bool
	killed bool
	final  *extension
}

// state is one pass of the simulation.
type state struct {
	entities   []any
	previous   []any // the first pass's entities, in the second pass
	schemas    []*schema
	relations  []*relation
	types      []*typ
	routines   []*routine
	extensions []*extension
	searchPath []string

	constraints []*constraint
	uses        map[string]int // see useRelationName
}

func newState(previous []any) *state {
	return &state{previous: previous, searchPath: []string{"public"}, uses: map[string]int{}}
}

// register numbers a new entity and links it to its first-pass counterpart.
func (s *state) register(entity any) int {
	id := len(s.entities)
	s.entities = append(s.entities, entity)
	return id
}

func counterpart[T any](s *state, id int) T {
	var zero T
	if s.previous == nil || id >= len(s.previous) {
		return zero
	}
	if match, ok := s.previous[id].(T); ok {
		return match
	}
	return zero
}

// ---- entity creation -----------------------------------------------------

func (s *state) newSchema(name string, created bool) *schema {
	sc := &schema{name: name, created: created}
	sc.id = s.register(sc)
	sc.final = counterpart[*schema](s, sc.id)
	s.schemas = append(s.schemas, sc)
	return sc
}

func (s *state) newRelation(kind relKind, sc *schema, name string) *relation {
	rel := &relation{kind: kind, schema: sc, name: name, created: true}
	rel.id = s.register(rel)
	rel.final = counterpart[*relation](s, rel.id)
	s.relations = append(s.relations, rel)
	s.useRelationName(sc, name)
	return rel
}

func (s *state) newColumn(rel *relation, name string, t *typ) *column {
	col := &column{name: name, typ: t}
	col.id = s.register(col)
	col.final = counterpart[*column](s, col.id)
	rel.columns = append(rel.columns, col)
	return col
}

func (s *state) newType(kind typKind, sc *schema, name string) *typ {
	t := &typ{kind: kind, schema: sc, name: name, created: true}
	t.id = s.register(t)
	t.final = counterpart[*typ](s, t.id)
	s.types = append(s.types, t)
	return t
}

func (s *state) newLabel(t *typ, name string, at int) *label {
	l := &label{name: name}
	l.id = s.register(l)
	l.final = counterpart[*label](s, l.id)
	t.labels = slices.Insert(t.labels, at, l)
	return l
}

func (s *state) newAttribute(t *typ, name string, attrType *typ) *column {
	col := &column{name: name, typ: attrType}
	col.id = s.register(col)
	col.final = counterpart[*column](s, col.id)
	t.attrs = append(t.attrs, col)
	return col
}

func (s *state) newRoutine(sc *schema, name string) *routine {
	r := &routine{schema: sc, name: name}
	r.id = s.register(r)
	r.final = counterpart[*routine](s, r.id)
	s.routines = append(s.routines, r)
	return r
}

func (s *state) newExtension(sc *schema, name string) *extension {
	e := &extension{schema: sc, name: name}
	e.id = s.register(e)
	e.final = counterpart[*extension](s, e.id)
	s.extensions = append(s.extensions, e)
	return e
}

// ---- lookup --------------------------------------------------------------

// findSchema returns the live schema with that name.
func (s *state) findSchema(name string) *schema {
	for i := len(s.schemas) - 1; i >= 0; i-- {
		if sc := s.schemas[i]; !sc.dead && sc.name == name {
			return sc
		}
	}
	return nil
}

// schemaNamed returns the live schema with that name, recording a schema
// the history did not create (public, a platform schema) on first mention.
func (s *state) schemaNamed(name string) *schema {
	if name == "" {
		return nil
	}
	if sc := s.findSchema(name); sc != nil {
		return sc
	}
	return s.newSchema(name, false)
}

// creationSchema is the schema an unqualified CREATE puts its object in.
func (s *state) creationSchema(name string) *schema {
	if name != "" {
		return s.schemaNamed(name)
	}
	return s.schemaNamed(s.searchPath[0])
}

// searchSchemas lists the schemas an unqualified name is looked up in.
func (s *state) searchSchemas(qualifier string) []*schema {
	if qualifier != "" {
		if sc := s.findSchema(qualifier); sc != nil {
			return []*schema{sc}
		}
		return nil
	}
	var out []*schema
	for _, name := range s.searchPath {
		if sc := s.findSchema(name); sc != nil {
			out = append(out, sc)
		}
	}
	return out
}

func (s *state) findRelation(qualifier, name string) *relation {
	for _, sc := range s.searchSchemas(qualifier) {
		for i := len(s.relations) - 1; i >= 0; i-- {
			if rel := s.relations[i]; !rel.dead && rel.schema == sc && rel.name == name {
				return rel
			}
		}
	}
	return nil
}

func (s *state) findType(qualifier, name string) *typ {
	for _, sc := range s.searchSchemas(qualifier) {
		for i := len(s.types) - 1; i >= 0; i-- {
			if t := s.types[i]; !t.dead && t.schema == sc && t.name == name {
				return t
			}
		}
	}
	return nil
}

// relationTaken reports whether a relation name is in use in a schema, for
// the names PostgreSQL chooses itself.
func (s *state) relationTaken(sc *schema, name string) bool {
	for _, rel := range s.relations {
		if !rel.dead && rel.schema == sc && rel.name == name {
			return true
		}
	}
	return false
}

func (rel *relation) column(name string) *column {
	for _, col := range rel.columns {
		if !col.dead && col.name == name {
			return col
		}
	}
	return nil
}

func (rel *relation) liveColumns() []*column {
	var out []*column
	for _, col := range rel.columns {
		if !col.dead {
			out = append(out, col)
		}
	}
	return out
}

func (t *typ) label(name string) *label {
	for _, l := range t.labels {
		if l.name == name {
			return l
		}
	}
	return nil
}

func (t *typ) attribute(name string) *column {
	for _, col := range t.attrs {
		if !col.dead && col.name == name {
			return col
		}
	}
	return nil
}

// enumType follows a domain to the enum it is over.
func (t *typ) enumType() *typ {
	for seen := 0; t != nil && seen < 16; seen++ {
		if t.kind == typEnum {
			return t
		}
		t = t.base
	}
	return nil
}

// ---- lifecycle -----------------------------------------------------------

func (s *state) dropRelation(rel *relation, killed bool) {
	if rel == nil || rel.dead {
		return
	}
	rel.dead = true
	rel.killed = rel.killed || killed
	s.dropConstraintsOf(rel)
	for _, other := range s.relations {
		if !other.dead && (other.owner == rel || other.ownedBy == rel) {
			s.dropRelation(other, killed)
		}
	}
}

// dropColumn drops a column with the constraints that involve it and the
// sequences that belong to it.
func (s *state) dropColumn(rel *relation, col *column) {
	col.dead = true
	s.dropConstraintsOnColumn(rel, col)
	for _, other := range s.relations {
		if !other.dead && other.ownedBy == rel && other.ownedColumn == col {
			s.dropRelation(other, false)
		}
	}
}

func (s *state) dropType(t *typ, killed bool) {
	if t == nil || t.dead {
		return
	}
	t.dead = true
	t.killed = t.killed || killed
	for _, c := range s.constraints {
		if c.domain == t {
			c.dead = true
		}
	}
}

// dropSchemaContents drops what lives in a schema; it reports whether
// anything did.
func (s *state) dropSchemaContents(sc *schema) bool {
	found := false
	for _, rel := range s.relations {
		if !rel.dead && rel.schema == sc {
			found = true
			s.dropRelation(rel, true)
		}
	}
	for _, t := range s.types {
		if !t.dead && t.schema == sc {
			found = true
			s.dropType(t, true)
		}
	}
	for _, r := range s.routines {
		if !r.dead && r.schema == sc {
			found = true
			r.dead, r.killed = true, true
		}
	}
	for _, e := range s.extensions {
		if !e.dead && e.schema == sc {
			found = true
			e.dead, e.killed = true, true
		}
	}
	return found
}

// ---- final names (second pass) -------------------------------------------

func (sc *schema) finalName() string {
	if sc.final != nil {
		return sc.final.name
	}
	return sc.name
}

func (rel *relation) finalName() string {
	if rel.final != nil {
		return rel.final.name
	}
	return rel.name
}

func (rel *relation) finalSchema() *schema {
	if rel.final != nil {
		return rel.final.schema
	}
	return rel.schema
}

func (rel *relation) isKilled() bool {
	return rel.final != nil && rel.final.killed
}

// isDroppedSequence reports a sequence the history creates and drops again,
// directly or with the column or table it belongs to: none of its
// statements belongs in the baseline.
func (rel *relation) isDroppedSequence() bool {
	return rel.kind == relSequence && rel.final != nil && rel.final.dead
}

func (rel *relation) isRenamed() bool {
	if rel.final != nil {
		return rel.final.renamed
	}
	return rel.renamed
}

func (col *column) finalName() string {
	if col.final != nil {
		return col.final.name
	}
	return col.name
}

func (t *typ) finalName() string {
	if t.final != nil {
		return t.final.name
	}
	return t.name
}

func (t *typ) finalSchema() *schema {
	if t.final != nil {
		return t.final.schema
	}
	return t.schema
}

func (t *typ) isKilled() bool {
	return t.final != nil && t.final.killed
}

func (l *label) finalName() string {
	if l.final != nil {
		return l.final.name
	}
	return l.name
}
