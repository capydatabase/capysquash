package squasher

import (
	"strings"

	"github.com/capydatabase/capysquash/internal/pgnames"
	pg_query "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// sequenceKey is how the ordering names a sequence on both sides of a
// dependency: "sequence:" and the schema-qualified name.
func sequenceKey(schema, name string) string {
	if schema == "" {
		schema = "public"
	}
	return "sequence:" + strings.ToLower(schema) + "." + strings.ToLower(name)
}

// sequenceProvisions lists the sequences sql creates: CREATE SEQUENCE, and
// the sequence PostgreSQL creates for a serial or identity column
// (table_column_seq).
func sequenceProvisions(sql string) []string {
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return nil
	}
	var provides []string
	columns := func(relation *pg_query.RangeVar, column *pg_query.ColumnDef) {
		if !ownsSequence(column) {
			return
		}
		name := pgnames.MakeObjectName(relation.GetRelname(), column.GetColname(), "seq")
		provides = append(provides, sequenceKey(relation.GetSchemaname(), name))
	}
	for _, raw := range tree.GetStmts() {
		switch node := raw.GetStmt().GetNode().(type) {
		case *pg_query.Node_CreateSeqStmt:
			seq := node.CreateSeqStmt.GetSequence()
			provides = append(provides, sequenceKey(seq.GetSchemaname(), seq.GetRelname()))
		case *pg_query.Node_CreateStmt:
			for _, element := range node.CreateStmt.GetTableElts() {
				if column := element.GetColumnDef(); column != nil {
					columns(node.CreateStmt.GetRelation(), column)
				}
			}
		case *pg_query.Node_AlterTableStmt:
			for _, command := range node.AlterTableStmt.GetCmds() {
				if column := command.GetAlterTableCmd().GetDef().GetColumnDef(); column != nil {
					columns(node.AlterTableStmt.GetRelation(), column)
				}
			}
		}
	}
	return provides
}

// ownsSequence reports whether PostgreSQL creates a sequence for a column:
// a serial type or GENERATED ... AS IDENTITY.
func ownsSequence(column *pg_query.ColumnDef) bool {
	names := column.GetTypeName().GetNames()
	if len(names) > 0 {
		switch strings.ToLower(names[len(names)-1].GetString_().GetSval()) {
		case "serial", "serial4", "bigserial", "serial8", "smallserial", "serial2":
			return true
		}
	}
	for _, node := range column.GetConstraints() {
		if node.GetConstraint().GetContype() == pg_query.ConstrType_CONSTR_IDENTITY {
			return true
		}
	}
	return false
}

// sequenceDependencies lists what sql needs to exist because of sequences:
// the sequences it names in nextval, currval and setval and in
// 'name'::regclass (a column default, typically), the sequence an ALTER
// SEQUENCE changes, and the table an OWNED BY names.
func sequenceDependencies(sql string) []string {
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return nil
	}
	var deps []string
	literal := func(s string) {
		if schema, name, ok := relationInString(s); ok {
			deps = append(deps, sequenceKey(schema, name))
		}
	}
	for _, raw := range tree.GetStmts() {
		var options []*pg_query.Node
		switch node := raw.GetStmt().GetNode().(type) {
		case *pg_query.Node_CreateSeqStmt:
			options = node.CreateSeqStmt.GetOptions()
		case *pg_query.Node_AlterSeqStmt:
			seq := node.AlterSeqStmt.GetSequence()
			deps = append(deps, sequenceKey(seq.GetSchemaname(), seq.GetRelname()))
			options = node.AlterSeqStmt.GetOptions()
		}
		for _, option := range options {
			def := option.GetDefElem()
			if def.GetDefname() != "owned_by" {
				continue
			}
			var parts []string
			for _, item := range def.GetArg().GetList().GetItems() {
				parts = append(parts, item.GetString_().GetSval())
			}
			if len(parts) >= 2 { // [schema.]table.column; OWNED BY NONE has one part
				deps = append(deps, strings.ToLower(strings.Join(parts[:len(parts)-1], ".")))
			}
		}

		walkMessages(raw.GetStmt(), func(m proto.Message) {
			switch n := m.(type) {
			case *pg_query.FuncCall:
				names := n.GetFuncname()
				if len(names) == 0 || len(n.GetArgs()) == 0 {
					return
				}
				switch names[len(names)-1].GetString_().GetSval() {
				case "nextval", "currval", "setval":
					arg := n.GetArgs()[0]
					if cast := arg.GetTypeCast(); cast != nil {
						arg = cast.GetArg()
					}
					if s := arg.GetAConst().GetSval(); s != nil {
						literal(s.GetSval())
					}
				}
			case *pg_query.TypeCast:
				names := n.GetTypeName().GetNames()
				if len(names) > 0 && names[len(names)-1].GetString_().GetSval() == "regclass" {
					if s := n.GetArg().GetAConst().GetSval(); s != nil {
						literal(s.GetSval())
					}
				}
			}
		})
	}
	return deps
}

// relationInString reads a relation name written in a string, as
// nextval('schema.name') takes it.
func relationInString(s string) (schema, name string, ok bool) {
	parsed, err := pg_query.Parse("SELECT FROM " + s)
	if err != nil {
		return "", "", false
	}
	from := parsed.GetStmts()[0].GetStmt().GetSelectStmt().GetFromClause()
	if len(from) != 1 || from[0].GetRangeVar() == nil {
		return "", "", false
	}
	rv := from[0].GetRangeVar()
	return rv.GetSchemaname(), rv.GetRelname(), true
}

// walkMessages calls visit for m and every message below it.
func walkMessages(m proto.Message, visit func(proto.Message)) {
	if m == nil {
		return
	}
	visit(m)
	m.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if fd.Message() == nil || fd.IsMap() {
			return true
		}
		if fd.IsList() {
			list := v.List()
			for i := 0; i < list.Len(); i++ {
				walkMessages(list.Get(i).Message().Interface(), visit)
			}
			return true
		}
		walkMessages(v.Message().Interface(), visit)
		return true
	})
}
