package privileges

import (
	"fmt"
	"slices"
	"strings"

	"github.com/capydatabase/capysquash/internal/parser"
	pg_query "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/proto"
)

// Object kinds, keyed by the catalog that stores them: relations and
// sequences share pg_class, so they share a namespace.
const (
	kindRelation = "relation"
	kindRoutine  = "routine"
	kindSchema   = "schema"
	kindType     = "type"
)

// Default-privilege object types, as pg_default_acl.defaclobjtype spells them.
const (
	defaultsTables    = 'r'
	defaultsSequences = 'S'
	defaultsFunctions = 'f'
	defaultsTypes     = 'T'
	defaultsSchemas   = 'n'
)

// object is one database object the history creates.
type object struct {
	kind    string
	keyword string // TABLE, VIEW, MATERIALIZED VIEW, FOREIGN TABLE, SEQUENCE, FUNCTION, PROCEDURE, SCHEMA, TYPE, DOMAIN
	schema  string // "" for schemas
	name    string
	args    string // routines: normalized input-argument signature such as "(integer,text)"

	owner        string // role token
	ownerSpelled string // the role an explicit ownership change named, as written
	acl          acl    // nil while the object has its built-in default privileges
	columns      map[string]acl

	// linked is the table a sequence belongs to (serial, identity, OWNED BY)
	// or the range type a multirange belongs to: it follows that object's
	// owner, schema and lifetime.
	linked       *object
	linkedColumn string

	created int // position in the history, for a stable output order
	dropped bool
}

func (o *object) key() string {
	return objectKey(o.kind, o.schema, o.name, o.args)
}

func objectKey(kind, schema, name, args string) string {
	return kind + "|" + schema + "|" + name + "|" + args
}

// class is the privilege family the object's ACL belongs to.
func (o *object) class() aclClass {
	switch o.kind {
	case kindRelation:
		if o.keyword == "SEQUENCE" {
			return classSequence
		}
		return classTable
	case kindRoutine:
		return classRoutine
	case kindSchema:
		return classSchema
	default:
		return classType
	}
}

// grantTarget renders the object as GRANT names it ("TABLE public.t").
func (o *object) grantTarget() string {
	switch o.kind {
	case kindRelation:
		if o.keyword == "SEQUENCE" {
			return "SEQUENCE " + qualified(o.schema, o.name)
		}
		return "TABLE " + qualified(o.schema, o.name)
	case kindRoutine:
		return o.keyword + " " + qualified(o.schema, o.name) + o.args
	case kindSchema:
		return "SCHEMA " + quoteIdent(o.name)
	default:
		return o.keyword + " " + qualified(o.schema, o.name)
	}
}

// alterTarget renders the object as ALTER ... OWNER TO names it.
func (o *object) alterTarget() string {
	switch o.kind {
	case kindRoutine:
		return o.keyword + " " + qualified(o.schema, o.name) + o.args
	case kindSchema:
		return "SCHEMA " + quoteIdent(o.name)
	default:
		return o.keyword + " " + qualified(o.schema, o.name)
	}
}

// display names the object for warnings.
func (o *object) display() string {
	if o.kind == kindSchema {
		return "schema " + o.name
	}
	return strings.ToLower(o.keyword) + " " + o.schema + "." + o.name + o.args
}

// currentACL returns the object's ACL, materializing the default first.
func (o *object) currentACL() acl {
	if o.acl == nil {
		o.acl = defaultACL(o.class(), o.owner)
	}
	return o.acl
}

type defaultKey struct {
	role    string // role token
	schema  string // "" for the global entry
	objtype byte
}

// schemaRef names a schema in a bulk statement: a schema the history
// creates (so it follows renames and drops) or one it does not know.
type schemaRef struct {
	known   *object
	literal string
}

func (r schemaRef) name() (string, bool) {
	if r.known != nil {
		return r.known.name, !r.known.dropped
	}
	return r.literal, true
}

// grantee is one role a statement names: its token and how it was written.
type grantee struct {
	token   string
	spelled string
}

// bulkGrant is GRANT/REVOKE ... ON ALL <objects> IN SCHEMA.
type bulkGrant struct {
	isGrant     bool
	grantOption bool
	cascade     bool
	objtype     pg_query.ObjectType
	schemas     []schemaRef
	privileges  []string // as written, upper case; empty means ALL
	grantees    []grantee
}

// replay is a statement the PRIVILEGES section repeats in history order:
// one that acts on objects the history does not create (pre-existing ones
// such as schema public, or ones an extension creates), or a bulk statement,
// which also reaches such objects.
type replay struct {
	sql  string
	bulk *bulkGrant
}

// model replays a history's privilege-relevant statements.
type model struct {
	// self is the role name assumed to run the migrations ("" when it is
	// not one the history names).
	self       string
	searchPath []string

	objects  map[string]*object
	all      []*object
	position int

	defaults     map[defaultKey]acl
	defaultOrder []defaultKey

	replays  []replay
	warnings []string
	warned   map[string]bool
}

func newModel(self string) *model {
	return &model{
		self:       self,
		searchPath: []string{"public"},
		objects:    make(map[string]*object),
		defaults:   make(map[defaultKey]acl),
		warned:     make(map[string]bool),
	}
}

func (m *model) warn(format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	if !m.warned[message] {
		m.warned[message] = true
		m.warnings = append(m.warnings, message)
	}
}

// role normalizes a RoleSpec to a token.
func (m *model) role(spec *pg_query.RoleSpec) grantee {
	if spec == nil {
		return grantee{token: roleMigrator, spelled: "CURRENT_USER"}
	}
	switch spec.GetRoletype() {
	case pg_query.RoleSpecType_ROLESPEC_PUBLIC:
		return grantee{token: rolePublic, spelled: "PUBLIC"}
	case pg_query.RoleSpecType_ROLESPEC_CURRENT_USER:
		return grantee{token: roleMigrator, spelled: "CURRENT_USER"}
	case pg_query.RoleSpecType_ROLESPEC_CURRENT_ROLE:
		return grantee{token: roleMigrator, spelled: "CURRENT_ROLE"}
	case pg_query.RoleSpecType_ROLESPEC_SESSION_USER:
		return grantee{token: roleMigrator, spelled: "SESSION_USER"}
	default:
		name := spec.GetRolename()
		token := name
		if m.self != "" && name == m.self {
			token = roleMigrator
		}
		return grantee{token: token, spelled: quoteIdent(name)}
	}
}

func (m *model) roleNode(node *pg_query.Node) grantee {
	return m.role(node.GetRoleSpec())
}

// ---- object lookup -------------------------------------------------------

func (m *model) creationSchema(schema string) string {
	if schema != "" {
		return schema
	}
	return m.searchPath[0]
}

// find resolves an optionally qualified name the way the search path would.
func (m *model) find(kind, schema, name, args string) *object {
	if schema != "" {
		return m.objects[objectKey(kind, schema, name, args)]
	}
	for _, candidate := range m.searchPath {
		if obj := m.objects[objectKey(kind, candidate, name, args)]; obj != nil {
			return obj
		}
	}
	return nil
}

func (m *model) findRelation(rv *pg_query.RangeVar) *object {
	if rv == nil {
		return nil
	}
	return m.find(kindRelation, rv.GetSchemaname(), rv.GetRelname(), "")
}

func (m *model) findSchema(name string) *object {
	return m.objects[objectKey(kindSchema, "", name, "")]
}

func (m *model) findType(node *pg_query.Node) *object {
	schema, name := splitName(nameParts(node))
	return m.find(kindType, schema, name, "")
}

// findRoutine resolves a routine by signature, or by name alone when the
// statement leaves the arguments out and the name is not overloaded.
func (m *model) findRoutine(owa *pg_query.ObjectWithArgs) *object {
	if owa == nil {
		return nil
	}
	schema, name := splitName(stringList(owa.GetObjname()))
	if signature := parser.FunctionSignatureFromArgs(owa); signature != "" || !owa.GetArgsUnspecified() {
		return m.find(kindRoutine, schema, name, signature)
	}
	schemas := m.searchPath
	if schema != "" {
		schemas = []string{schema}
	}
	for _, candidate := range schemas {
		var match *object
		count := 0
		for _, obj := range m.all {
			if !obj.dropped && obj.kind == kindRoutine && obj.schema == candidate && obj.name == name {
				match = obj
				count++
			}
		}
		if count == 1 {
			return match
		}
		if count > 1 {
			return nil
		}
	}
	return nil
}

// ---- object lifecycle ----------------------------------------------------

func (m *model) create(kind, keyword, schema, name, args, owner string, defaults byte) *object {
	obj := &object{
		kind:    kind,
		keyword: keyword,
		schema:  schema,
		name:    name,
		args:    args,
		owner:   owner,
		created: m.position,
	}
	obj.acl = m.initialACL(obj, defaults)
	m.objects[obj.key()] = obj
	m.all = append(m.all, obj)
	return obj
}

// initialACL applies the owner's default privileges (ALTER DEFAULT
// PRIVILEGES) the way PostgreSQL does at creation: a global entry replaces
// the built-in default and a schema entry adds to it. nil means the object
// keeps the built-in default.
func (m *model) initialACL(obj *object, defaults byte) acl {
	builtIn := defaultACL(obj.class(), obj.owner)
	result := builtIn
	if global, ok := m.defaults[defaultKey{role: obj.owner, objtype: defaults}]; ok {
		result = global.clone()
	}
	if obj.schema != "" {
		if entry, ok := m.defaults[defaultKey{role: obj.owner, schema: obj.schema, objtype: defaults}]; ok {
			result = result.clone()
			result.merge(entry)
		}
	}
	if result.equal(builtIn) {
		return nil
	}
	return result
}

func (m *model) drop(obj *object) {
	if obj == nil || obj.dropped {
		return
	}
	obj.dropped = true
	delete(m.objects, obj.key())
	for _, other := range m.all {
		if other.linked == obj {
			m.drop(other)
		}
	}
}

// rekey moves an object to a new name or schema, with what follows it.
func (m *model) rekey(obj *object, schema, name string) {
	delete(m.objects, obj.key())
	oldSchema := obj.schema
	obj.schema, obj.name = schema, name
	m.objects[obj.key()] = obj
	if schema == oldSchema {
		return
	}
	for _, other := range m.all {
		if !other.dropped && other.linked == obj {
			m.rekey(other, schema, other.name)
		}
	}
}

func (m *model) changeOwner(obj *object, owner grantee) {
	if obj.acl != nil {
		obj.acl.changeOwner(obj.owner, owner.token)
	}
	for _, column := range obj.columns {
		column.changeOwner(obj.owner, owner.token)
	}
	obj.owner = owner.token
	obj.ownerSpelled = ""
	if owner.token != roleMigrator || m.self != "" && owner.spelled == quoteIdent(m.self) {
		obj.ownerSpelled = owner.spelled
	}
	for _, other := range m.all {
		if !other.dropped && other.linked == obj {
			m.changeOwner(other, owner)
			other.ownerSpelled = ""
		}
	}
}

var serialTypes = map[string]bool{
	"serial": true, "serial4": true, "bigserial": true, "serial8": true, "smallserial": true, "serial2": true,
}

// columnNeedsSequence reports whether a column definition creates a
// sequence: a serial type or an identity column.
func columnNeedsSequence(def *pg_query.ColumnDef) bool {
	if def == nil {
		return false
	}
	names := stringList(def.GetTypeName().GetNames())
	if len(names) == 1 && serialTypes[names[0]] {
		return true
	}
	for _, node := range def.GetConstraints() {
		if node.GetConstraint().GetContype() == pg_query.ConstrType_CONSTR_IDENTITY {
			return true
		}
	}
	return false
}

// implicitSequence creates the sequence PostgreSQL makes for a serial or
// identity column, named as ChooseRelationName names it.
func (m *model) implicitSequence(table *object, column string) {
	name := chooseRelationName(table.name, column, "seq", func(candidate string) bool {
		return m.objects[objectKey(kindRelation, table.schema, candidate, "")] != nil
	})
	seq := m.create(kindRelation, "SEQUENCE", table.schema, name, "", table.owner, defaultsSequences)
	seq.linked = table
	seq.linkedColumn = column
}

// chooseRelationName follows PostgreSQL's makeObjectName: name1_name2_label
// truncated to 63 bytes by shortening the longer part first, with a number
// appended to the label while the name is taken.
func chooseRelationName(name1, name2, label string, taken func(string) bool) string {
	for pass := 0; ; pass++ {
		modLabel := label
		if pass > 0 {
			modLabel = fmt.Sprintf("%s%d", label, pass)
		}
		name := makeObjectName(name1, name2, modLabel)
		if !taken(name) {
			return name
		}
	}
}

func makeObjectName(name1, name2, label string) string {
	const maxLength = 63
	overhead := 1 + len(label) + 1
	if name2 == "" {
		overhead = len(label) + 1
	}
	chars1, chars2 := len(name1), len(name2)
	for chars1+chars2 > maxLength-overhead {
		if chars1 > chars2 {
			chars1--
		} else {
			chars2--
		}
	}
	chars1 = clipRunes(name1, chars1)
	chars2 = clipRunes(name2, chars2)
	name := name1[:chars1]
	if name2 != "" {
		name += "_" + name2[:chars2]
	}
	return name + "_" + label
}

// clipRunes shortens a byte count so it does not split a UTF-8 character.
func clipRunes(s string, n int) int {
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return n
}

// ---- statements ----------------------------------------------------------

// apply feeds one statement of the history to the model.
func (m *model) apply(node *pg_query.Node) {
	m.position++
	switch n := node.GetNode().(type) {
	case *pg_query.Node_CreateStmt:
		m.createTable(n.CreateStmt, "TABLE")
	case *pg_query.Node_CreateForeignTableStmt:
		m.createTable(n.CreateForeignTableStmt.GetBaseStmt(), "FOREIGN TABLE")
	case *pg_query.Node_CreateTableAsStmt:
		into := n.CreateTableAsStmt.GetInto()
		keyword := "TABLE"
		if n.CreateTableAsStmt.GetObjtype() == pg_query.ObjectType_OBJECT_MATVIEW {
			keyword = "MATERIALIZED VIEW"
		}
		m.createRelation(into.GetRel(), keyword, n.CreateTableAsStmt.GetIfNotExists())
	case *pg_query.Node_ViewStmt:
		if m.findRelation(n.ViewStmt.GetView()) == nil {
			m.createRelation(n.ViewStmt.GetView(), "VIEW", false)
		}
	case *pg_query.Node_CreateSeqStmt:
		seq := m.createRelation(n.CreateSeqStmt.GetSequence(), "SEQUENCE", n.CreateSeqStmt.GetIfNotExists())
		m.sequenceOwnedBy(seq, n.CreateSeqStmt.GetOptions())
	case *pg_query.Node_AlterSeqStmt:
		m.sequenceOwnedBy(m.findRelation(n.AlterSeqStmt.GetSequence()), n.AlterSeqStmt.GetOptions())
	case *pg_query.Node_CreateFunctionStmt:
		m.createRoutine(n.CreateFunctionStmt)
	case *pg_query.Node_CreateEnumStmt:
		m.createType(n.CreateEnumStmt.GetTypeName(), "TYPE")
	case *pg_query.Node_CompositeTypeStmt:
		rv := n.CompositeTypeStmt.GetTypevar()
		m.createTypeNamed(rv.GetSchemaname(), rv.GetRelname(), "TYPE")
	case *pg_query.Node_CreateRangeStmt:
		m.createRange(n.CreateRangeStmt)
	case *pg_query.Node_CreateDomainStmt:
		m.createType(n.CreateDomainStmt.GetDomainname(), "DOMAIN")
	case *pg_query.Node_CreateSchemaStmt:
		m.createSchema(n.CreateSchemaStmt)
	case *pg_query.Node_RenameStmt:
		m.rename(n.RenameStmt)
	case *pg_query.Node_AlterObjectSchemaStmt:
		m.setSchema(n.AlterObjectSchemaStmt)
	case *pg_query.Node_DropStmt:
		m.dropObjects(n.DropStmt)
	case *pg_query.Node_AlterTableStmt:
		m.alterTable(n.AlterTableStmt, node)
	case *pg_query.Node_AlterOwnerStmt:
		m.alterOwner(n.AlterOwnerStmt, node)
	case *pg_query.Node_GrantStmt:
		m.grantStmt(n.GrantStmt, node)
	case *pg_query.Node_AlterDefaultPrivilegesStmt:
		m.alterDefaultPrivileges(n.AlterDefaultPrivilegesStmt)
	case *pg_query.Node_VariableSetStmt:
		m.variableSet(n.VariableSetStmt)
	case *pg_query.Node_DropOwnedStmt, *pg_query.Node_ReassignOwnedStmt:
		m.warn("DROP OWNED and REASSIGN OWNED are not modeled; check the ownership and privileges they change")
	}
}

func (m *model) createTable(stmt *pg_query.CreateStmt, keyword string) {
	if stmt == nil {
		return
	}
	table := m.createRelation(stmt.GetRelation(), keyword, stmt.GetIfNotExists())
	if table == nil {
		return
	}
	for _, element := range stmt.GetTableElts() {
		if def := element.GetColumnDef(); columnNeedsSequence(def) {
			m.implicitSequence(table, def.GetColname())
		}
	}
}

// createRelation creates a relation unless it is temporary or IF NOT EXISTS
// finds it; it returns nil when nothing was created.
func (m *model) createRelation(rv *pg_query.RangeVar, keyword string, ifNotExists bool) *object {
	if rv == nil || rv.GetRelpersistence() == "t" {
		return nil
	}
	if existing := m.findRelation(rv); existing != nil {
		if !ifNotExists {
			m.warn("%s is created twice; privileges follow the second definition", existing.display())
			m.drop(existing)
		} else {
			return nil
		}
	}
	defaults := byte(defaultsTables)
	if keyword == "SEQUENCE" {
		defaults = defaultsSequences
	}
	return m.create(kindRelation, keyword, m.creationSchema(rv.GetSchemaname()), rv.GetRelname(), "", roleMigrator, defaults)
}

func (m *model) sequenceOwnedBy(seq *object, options []*pg_query.Node) {
	if seq == nil {
		return
	}
	for _, option := range options {
		def := option.GetDefElem()
		if def.GetDefname() != "owned_by" {
			continue
		}
		parts := nameParts(def.GetArg())
		if len(parts) == 1 && strings.EqualFold(parts[0], "none") {
			seq.linked, seq.linkedColumn = nil, ""
			continue
		}
		if len(parts) < 2 {
			continue
		}
		column := parts[len(parts)-1]
		schema, name := splitName(parts[:len(parts)-1])
		if table := m.find(kindRelation, schema, name, ""); table != nil {
			seq.linked, seq.linkedColumn = table, column
		}
	}
}

func (m *model) createRoutine(stmt *pg_query.CreateFunctionStmt) {
	schema, name := splitName(stringList(stmt.GetFuncname()))
	args := parser.FunctionSignatureFromParameters(stmt.GetParameters())
	if existing := m.find(kindRoutine, schema, name, args); existing != nil {
		if stmt.GetReplace() {
			return // CREATE OR REPLACE keeps owner and privileges
		}
		m.drop(existing)
	}
	keyword := "FUNCTION"
	if stmt.GetIsProcedure() {
		keyword = "PROCEDURE"
	}
	m.create(kindRoutine, keyword, m.creationSchema(schema), name, args, roleMigrator, defaultsFunctions)
}

func (m *model) createType(names []*pg_query.Node, keyword string) *object {
	schema, name := splitName(stringList(names))
	return m.createTypeNamed(schema, name, keyword)
}

func (m *model) createTypeNamed(schema, name, keyword string) *object {
	if existing := m.find(kindType, schema, name, ""); existing != nil {
		m.drop(existing)
	}
	return m.create(kindType, keyword, m.creationSchema(schema), name, "", roleMigrator, defaultsTypes)
}

// createRange also creates the multirange type PostgreSQL 14+ derives from
// a range type, which follows the range's owner, schema and lifetime.
func (m *model) createRange(stmt *pg_query.CreateRangeStmt) {
	rangeType := m.createType(stmt.GetTypeName(), "TYPE")
	multirange := ""
	for _, param := range stmt.GetParams() {
		def := param.GetDefElem()
		if strings.EqualFold(def.GetDefname(), "multirange_type_name") {
			_, multirange = splitName(nameParts(def.GetArg()))
		}
	}
	if multirange == "" {
		if strings.Contains(rangeType.name, "range") {
			multirange = strings.Replace(rangeType.name, "range", "multirange", 1)
		} else {
			multirange = rangeType.name + "_multirange"
		}
	}
	multi := m.create(kindType, "TYPE", rangeType.schema, multirange, "", rangeType.owner, defaultsTypes)
	multi.linked = rangeType
}

func (m *model) createSchema(stmt *pg_query.CreateSchemaStmt) {
	name := stmt.GetSchemaname()
	owner := grantee{token: roleMigrator}
	if stmt.GetAuthrole() != nil {
		owner = m.role(stmt.GetAuthrole())
		if name == "" {
			name = stmt.GetAuthrole().GetRolename()
		}
	}
	if m.findSchema(name) != nil && stmt.GetIfNotExists() {
		return
	}
	obj := &object{kind: kindSchema, keyword: "SCHEMA", name: name, owner: owner.token, created: m.position}
	if owner.token != roleMigrator || m.self != "" && owner.spelled == quoteIdent(m.self) {
		obj.ownerSpelled = owner.spelled
	}
	obj.acl = m.initialACL(obj, defaultsSchemas)
	m.objects[obj.key()] = obj
	m.all = append(m.all, obj)

	// Elements are created in the new schema.
	saved := m.searchPath
	m.searchPath = []string{name}
	for _, element := range stmt.GetSchemaElts() {
		m.apply(element)
	}
	m.searchPath = saved
}

func (m *model) rename(stmt *pg_query.RenameStmt) {
	switch stmt.GetRenameType() {
	case pg_query.ObjectType_OBJECT_TABLE, pg_query.ObjectType_OBJECT_VIEW, pg_query.ObjectType_OBJECT_MATVIEW,
		pg_query.ObjectType_OBJECT_FOREIGN_TABLE, pg_query.ObjectType_OBJECT_SEQUENCE:
		if obj := m.findRelation(stmt.GetRelation()); obj != nil {
			m.rekey(obj, obj.schema, stmt.GetNewname())
		}
	case pg_query.ObjectType_OBJECT_COLUMN:
		table := m.findRelation(stmt.GetRelation())
		if table == nil {
			return
		}
		if column, ok := table.columns[stmt.GetSubname()]; ok {
			delete(table.columns, stmt.GetSubname())
			table.columns[stmt.GetNewname()] = column
		}
		for _, other := range m.all {
			if !other.dropped && other.linked == table && other.linkedColumn == stmt.GetSubname() {
				other.linkedColumn = stmt.GetNewname()
			}
		}
	case pg_query.ObjectType_OBJECT_FUNCTION, pg_query.ObjectType_OBJECT_PROCEDURE, pg_query.ObjectType_OBJECT_ROUTINE:
		if obj := m.findRoutine(stmt.GetObject().GetObjectWithArgs()); obj != nil {
			m.rekey(obj, obj.schema, stmt.GetNewname())
		}
	case pg_query.ObjectType_OBJECT_TYPE, pg_query.ObjectType_OBJECT_DOMAIN:
		if obj := m.findType(stmt.GetObject()); obj != nil {
			m.rekey(obj, obj.schema, stmt.GetNewname())
		}
	case pg_query.ObjectType_OBJECT_SCHEMA:
		m.renameSchema(stmt.GetSubname(), stmt.GetNewname())
	}
}

func (m *model) renameSchema(oldName, newName string) {
	if schema := m.findSchema(oldName); schema != nil {
		m.rekey(schema, "", newName)
	}
	for _, obj := range slices.Clone(m.all) {
		if !obj.dropped && obj.kind != kindSchema && obj.schema == oldName {
			delete(m.objects, obj.key())
			obj.schema = newName
			m.objects[obj.key()] = obj
		}
	}
	for i, key := range m.defaultOrder {
		if key.schema != oldName {
			continue
		}
		entry, ok := m.defaults[key]
		delete(m.defaults, key)
		key.schema = newName
		m.defaultOrder[i] = key
		if ok {
			m.defaults[key] = entry
		}
	}
	for i, path := range m.searchPath {
		if path == oldName {
			m.searchPath[i] = newName
		}
	}
}

func (m *model) setSchema(stmt *pg_query.AlterObjectSchemaStmt) {
	var obj *object
	switch stmt.GetObjectType() {
	case pg_query.ObjectType_OBJECT_TABLE, pg_query.ObjectType_OBJECT_VIEW, pg_query.ObjectType_OBJECT_MATVIEW,
		pg_query.ObjectType_OBJECT_FOREIGN_TABLE, pg_query.ObjectType_OBJECT_SEQUENCE:
		obj = m.findRelation(stmt.GetRelation())
	case pg_query.ObjectType_OBJECT_FUNCTION, pg_query.ObjectType_OBJECT_PROCEDURE, pg_query.ObjectType_OBJECT_ROUTINE:
		obj = m.findRoutine(stmt.GetObject().GetObjectWithArgs())
	case pg_query.ObjectType_OBJECT_TYPE, pg_query.ObjectType_OBJECT_DOMAIN:
		obj = m.findType(stmt.GetObject())
	}
	if obj != nil {
		m.rekey(obj, stmt.GetNewschema(), obj.name)
	}
}

func (m *model) dropObjects(stmt *pg_query.DropStmt) {
	for _, target := range stmt.GetObjects() {
		switch stmt.GetRemoveType() {
		case pg_query.ObjectType_OBJECT_TABLE, pg_query.ObjectType_OBJECT_VIEW, pg_query.ObjectType_OBJECT_MATVIEW,
			pg_query.ObjectType_OBJECT_FOREIGN_TABLE, pg_query.ObjectType_OBJECT_SEQUENCE:
			schema, name := splitName(nameParts(target))
			m.drop(m.find(kindRelation, schema, name, ""))
		case pg_query.ObjectType_OBJECT_FUNCTION, pg_query.ObjectType_OBJECT_PROCEDURE, pg_query.ObjectType_OBJECT_ROUTINE:
			m.drop(m.findRoutine(target.GetObjectWithArgs()))
		case pg_query.ObjectType_OBJECT_TYPE, pg_query.ObjectType_OBJECT_DOMAIN:
			m.drop(m.findType(target))
		case pg_query.ObjectType_OBJECT_SCHEMA:
			m.dropSchema(target.GetString_().GetSval())
		}
	}
}

func (m *model) dropSchema(name string) {
	m.drop(m.findSchema(name))
	for _, obj := range m.all {
		if !obj.dropped && obj.kind != kindSchema && obj.schema == name {
			m.drop(obj)
		}
	}
	for key := range m.defaults {
		if key.schema == name {
			delete(m.defaults, key)
		}
	}
}

func (m *model) alterTable(stmt *pg_query.AlterTableStmt, node *pg_query.Node) {
	obj := m.findRelation(stmt.GetRelation())
	for _, cmdNode := range stmt.GetCmds() {
		cmd := cmdNode.GetAlterTableCmd()
		switch cmd.GetSubtype() {
		case pg_query.AlterTableType_AT_ChangeOwner:
			if obj == nil {
				if ownerOnlyAlterTable(stmt) {
					m.replayStatement(node)
				}
				continue
			}
			m.changeOwner(obj, m.role(cmd.GetNewowner()))
		case pg_query.AlterTableType_AT_AddColumn:
			if def := cmd.GetDef().GetColumnDef(); obj != nil && columnNeedsSequence(def) {
				m.implicitSequence(obj, def.GetColname())
			}
		case pg_query.AlterTableType_AT_AddIdentity:
			if obj != nil {
				m.implicitSequence(obj, cmd.GetName())
			}
		case pg_query.AlterTableType_AT_DropIdentity, pg_query.AlterTableType_AT_DropColumn:
			if obj == nil {
				continue
			}
			if cmd.GetSubtype() == pg_query.AlterTableType_AT_DropColumn {
				delete(obj.columns, cmd.GetName())
			}
			for _, other := range m.all {
				if !other.dropped && other.linked == obj && other.linkedColumn == cmd.GetName() {
					m.drop(other)
				}
			}
		}
	}
}

func (m *model) alterOwner(stmt *pg_query.AlterOwnerStmt, node *pg_query.Node) {
	var obj *object
	switch stmt.GetObjectType() {
	case pg_query.ObjectType_OBJECT_FUNCTION, pg_query.ObjectType_OBJECT_PROCEDURE, pg_query.ObjectType_OBJECT_ROUTINE:
		obj = m.findRoutine(stmt.GetObject().GetObjectWithArgs())
	case pg_query.ObjectType_OBJECT_SCHEMA:
		obj = m.findSchema(stmt.GetObject().GetString_().GetSval())
	case pg_query.ObjectType_OBJECT_TYPE, pg_query.ObjectType_OBJECT_DOMAIN:
		obj = m.findType(stmt.GetObject())
	}
	if obj == nil {
		if ownsAlterOwner(stmt) {
			m.replayStatement(node)
		}
		return
	}
	m.changeOwner(obj, m.role(stmt.GetNewowner()))
}

func (m *model) variableSet(stmt *pg_query.VariableSetStmt) {
	switch strings.ToLower(stmt.GetName()) {
	case "search_path":
		if stmt.GetKind() == pg_query.VariableSetKind_VAR_RESET || stmt.GetKind() == pg_query.VariableSetKind_VAR_SET_DEFAULT {
			m.searchPath = []string{"public"}
			return
		}
		path := make([]string, 0, len(stmt.GetArgs()))
		for _, arg := range stmt.GetArgs() {
			value := arg.GetAConst().GetSval().GetSval()
			if value != "" && value != "$user" && value != "pg_catalog" {
				path = append(path, value)
			}
		}
		if len(path) > 0 {
			m.searchPath = path
		}
	case "role", "session_authorization":
		m.warn("SET ROLE and SET SESSION AUTHORIZATION are not followed; privileges are modeled as if the migrating role ran every statement")
	}
}

// replayStatement keeps a statement on objects the history does not create.
func (m *model) replayStatement(node *pg_query.Node) {
	sql, err := deparse(node)
	if err != nil {
		m.warn("a privilege statement could not be rendered and is not in the baseline: %v", err)
		return
	}
	m.replays = append(m.replays, replay{sql: sql})
}

// ---- GRANT / REVOKE ------------------------------------------------------

// accessPrivileges splits a GRANT's privilege list into table-level
// privileges and per-column privileges. all is true for ALL / no list.
func accessPrivileges(privileges []*pg_query.Node) (table []string, columns map[string][]string, all bool) {
	if len(privileges) == 0 {
		return nil, nil, true
	}
	columns = map[string][]string{}
	for _, node := range privileges {
		priv := node.GetAccessPriv()
		name := strings.ToUpper(priv.GetPrivName())
		if name == "" {
			name = "ALL"
		}
		if cols := stringList(priv.GetCols()); len(cols) > 0 {
			for _, col := range cols {
				columns[col] = append(columns[col], name)
			}
			continue
		}
		table = append(table, name)
	}
	return table, columns, false
}

func (m *model) grantStmt(stmt *pg_query.GrantStmt, node *pg_query.Node) {
	grantees := make([]grantee, 0, len(stmt.GetGrantees()))
	for _, g := range stmt.GetGrantees() {
		grantees = append(grantees, m.roleNode(g))
	}
	tablePrivs, columnPrivs, all := accessPrivileges(stmt.GetPrivileges())

	if stmt.GetTargtype() == pg_query.GrantTargetType_ACL_TARGET_ALL_IN_SCHEMA {
		m.bulkGrant(stmt, grantees, tablePrivs, all)
		return
	}

	var unknown []*pg_query.Node
	for _, target := range stmt.GetObjects() {
		obj := m.grantObject(stmt.GetObjtype(), target)
		if obj == nil {
			unknown = append(unknown, target)
			continue
		}
		m.applyGrant(obj, stmt.GetIsGrant(), stmt.GetGrantOption(), grantees, tablePrivs, columnPrivs, all)
	}
	if len(unknown) > 0 {
		clone, ok := proto.Clone(node).(*pg_query.Node)
		if !ok {
			return
		}
		clone.GetGrantStmt().Objects = unknown
		m.replayStatement(clone)
	}
}

// grantObject resolves one GRANT target to an object the history created.
func (m *model) grantObject(objtype pg_query.ObjectType, target *pg_query.Node) *object {
	switch objtype {
	case pg_query.ObjectType_OBJECT_TABLE:
		return m.findRelation(target.GetRangeVar())
	case pg_query.ObjectType_OBJECT_SEQUENCE:
		if obj := m.findRelation(target.GetRangeVar()); obj != nil && obj.keyword == "SEQUENCE" {
			return obj
		}
	case pg_query.ObjectType_OBJECT_FUNCTION, pg_query.ObjectType_OBJECT_PROCEDURE, pg_query.ObjectType_OBJECT_ROUTINE:
		return m.findRoutine(target.GetObjectWithArgs())
	case pg_query.ObjectType_OBJECT_SCHEMA:
		return m.findSchema(target.GetString_().GetSval())
	case pg_query.ObjectType_OBJECT_TYPE, pg_query.ObjectType_OBJECT_DOMAIN:
		return m.findType(target)
	}
	return nil
}

func (m *model) applyGrant(obj *object, isGrant, grantOption bool, grantees []grantee, tablePrivs []string, columnPrivs map[string][]string, all bool) {
	class := obj.class()
	var privs []string
	if all || len(tablePrivs) > 0 {
		privs = normalizePrivileges(class, tablePrivs)
	}
	for _, g := range grantees {
		if len(privs) > 0 {
			current := obj.currentACL()
			if isGrant {
				current.grant(g.token, privs, grantOption)
			} else {
				current.revoke(g.token, privs, grantOption)
				// Revoking a table privilege revokes it from every column too.
				columnLevel := normalizePrivileges(classColumn, privs)
				for _, column := range obj.columns {
					column.revoke(g.token, columnLevel, grantOption)
				}
			}
		}
		for col, names := range columnPrivs {
			if obj.kind != kindRelation || class == classSequence {
				continue
			}
			colPrivs := normalizePrivileges(classColumn, names)
			if obj.columns == nil {
				obj.columns = map[string]acl{}
			}
			column := obj.columns[col]
			if column == nil {
				column = acl{}
				obj.columns[col] = column
			}
			if isGrant {
				column.grant(g.token, colPrivs, grantOption)
			} else {
				column.revoke(g.token, colPrivs, grantOption)
			}
		}
	}
}

// inBulkScope reports whether GRANT ... ON ALL <objtype> reaches obj.
func inBulkScope(objtype pg_query.ObjectType, obj *object) bool {
	switch objtype {
	case pg_query.ObjectType_OBJECT_TABLE:
		return obj.kind == kindRelation && obj.keyword != "SEQUENCE"
	case pg_query.ObjectType_OBJECT_SEQUENCE:
		return obj.kind == kindRelation && obj.keyword == "SEQUENCE"
	case pg_query.ObjectType_OBJECT_FUNCTION:
		return obj.kind == kindRoutine && obj.keyword == "FUNCTION"
	case pg_query.ObjectType_OBJECT_PROCEDURE:
		return obj.kind == kindRoutine && obj.keyword == "PROCEDURE"
	case pg_query.ObjectType_OBJECT_ROUTINE:
		return obj.kind == kindRoutine
	}
	return false
}

func (m *model) bulkGrant(stmt *pg_query.GrantStmt, grantees []grantee, tablePrivs []string, all bool) {
	bulk := &bulkGrant{
		isGrant:     stmt.GetIsGrant(),
		grantOption: stmt.GetGrantOption(),
		cascade:     stmt.GetBehavior() == pg_query.DropBehavior_DROP_CASCADE,
		objtype:     stmt.GetObjtype(),
		grantees:    grantees,
	}
	if !all {
		bulk.privileges = tablePrivs
	}
	for _, node := range stmt.GetObjects() {
		name := node.GetString_().GetSval()
		ref := schemaRef{literal: name}
		if schema := m.findSchema(name); schema != nil {
			ref = schemaRef{known: schema}
		}
		bulk.schemas = append(bulk.schemas, ref)
		for _, obj := range m.all {
			if obj.dropped || obj.schema != name || !inBulkScope(bulk.objtype, obj) {
				continue
			}
			m.applyGrant(obj, bulk.isGrant, bulk.grantOption, grantees, tablePrivs, nil, all)
		}
	}
	m.replays = append(m.replays, replay{bulk: bulk})
}

// ---- ALTER DEFAULT PRIVILEGES --------------------------------------------

func defaultsTarget(objtype pg_query.ObjectType) (byte, aclClass, string, bool) {
	switch objtype {
	case pg_query.ObjectType_OBJECT_TABLE:
		return defaultsTables, classTable, "TABLES", true
	case pg_query.ObjectType_OBJECT_SEQUENCE:
		return defaultsSequences, classSequence, "SEQUENCES", true
	case pg_query.ObjectType_OBJECT_FUNCTION, pg_query.ObjectType_OBJECT_PROCEDURE, pg_query.ObjectType_OBJECT_ROUTINE:
		return defaultsFunctions, classRoutine, "FUNCTIONS", true
	case pg_query.ObjectType_OBJECT_TYPE, pg_query.ObjectType_OBJECT_DOMAIN:
		return defaultsTypes, classType, "TYPES", true
	case pg_query.ObjectType_OBJECT_SCHEMA:
		return defaultsSchemas, classSchema, "SCHEMAS", true
	}
	return 0, 0, "", false
}

func (m *model) alterDefaultPrivileges(stmt *pg_query.AlterDefaultPrivilegesStmt) {
	action := stmt.GetAction()
	objtype, class, _, ok := defaultsTarget(action.GetObjtype())
	if !ok {
		m.warn("ALTER DEFAULT PRIVILEGES on %s is not modeled and is not in the baseline", action.GetObjtype())
		return
	}
	roles := []string{roleMigrator}
	schemas := []string{""}
	for _, option := range stmt.GetOptions() {
		def := option.GetDefElem()
		items := def.GetArg().GetList().GetItems()
		switch def.GetDefname() {
		case "roles":
			roles = roles[:0]
			for _, item := range items {
				roles = append(roles, m.roleNode(item).token)
			}
		case "schemas":
			schemas = stringList(items)
		}
	}
	tablePrivs, _, all := accessPrivileges(action.GetPrivileges())
	var privs []string
	if all {
		privs = class.universe()
	} else {
		privs = normalizePrivileges(class, tablePrivs)
	}
	for _, role := range roles {
		for _, schema := range schemas {
			key := defaultKey{role: role, schema: schema, objtype: objtype}
			entry, exists := m.defaults[key]
			if !exists {
				entry = acl{}
				if schema == "" {
					entry = defaultACL(class, role)
				}
				if !slices.Contains(m.defaultOrder, key) {
					m.defaultOrder = append(m.defaultOrder, key)
				}
			}
			for _, g := range action.GetGrantees() {
				token := m.roleNode(g).token
				if action.GetIsGrant() {
					entry.grant(token, privs, action.GetGrantOption())
				} else {
					entry.revoke(token, privs, action.GetGrantOption())
				}
			}
			if (schema == "" && entry.equal(defaultACL(class, role))) || (schema != "" && len(entry) == 0) {
				delete(m.defaults, key)
				continue
			}
			m.defaults[key] = entry
		}
	}
}
