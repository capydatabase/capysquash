package parser

import (
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// Function identity: PostgreSQL tells overloads apart by the types of their
// input arguments, so capysquash keys a function by its qualified name plus a
// normalized signature such as "(integer,text)". Two spellings of the same
// type ("int", "int4", "integer") produce the same signature; argument names,
// defaults, OUT/TABLE columns and type modifiers do not take part, exactly as
// in PostgreSQL.

// signatureTypeAliases maps pg_catalog internal type names to the names
// PostgreSQL prints (format_type), so every spelling of a type agrees.
var signatureTypeAliases = map[string]string{
	"int2":        "smallint",
	"int4":        "integer",
	"int":         "integer",
	"int8":        "bigint",
	"float4":      "real",
	"float8":      "double precision",
	"bool":        "boolean",
	"varchar":     "character varying",
	"bpchar":      "character",
	"decimal":     "numeric",
	"varbit":      "bit varying",
	"timestamp":   "timestamp without time zone",
	"timestamptz": "timestamp with time zone",
	"time":        "time without time zone",
	"timetz":      "time with time zone",
}

// FunctionSignatureFromArgs returns the normalized signature of an
// ObjectWithArgs (DROP/COMMENT/GRANT ... FUNCTION f(args)). It returns ""
// when the arguments were not written at all (DROP FUNCTION f), which is
// different from "()" for a function without arguments.
func FunctionSignatureFromArgs(owa *pg_query.ObjectWithArgs) string {
	if owa == nil || owa.GetArgsUnspecified() {
		return ""
	}
	types := make([]string, 0, len(owa.GetObjargs()))
	for _, arg := range owa.GetObjargs() {
		types = append(types, signatureTypeName(arg.GetTypeName()))
	}
	return "(" + strings.Join(types, ",") + ")"
}

// FunctionSignatureFromParameters returns the normalized signature of a
// CREATE FUNCTION/PROCEDURE parameter list: the IN, INOUT and VARIADIC
// parameters (OUT and TABLE columns are not part of a function's identity).
func FunctionSignatureFromParameters(params []*pg_query.Node) string {
	types := make([]string, 0, len(params))
	for _, node := range params {
		param := node.GetFunctionParameter()
		if param == nil {
			continue
		}
		switch param.GetMode() {
		case pg_query.FunctionParameterMode_FUNC_PARAM_OUT, pg_query.FunctionParameterMode_FUNC_PARAM_TABLE:
			continue
		}
		types = append(types, signatureTypeName(param.GetArgType()))
	}
	return "(" + strings.Join(types, ",") + ")"
}

// signatureTypeName renders a parsed type the way it takes part in a
// signature: built-in types under their printed names, "public." dropped (the
// default schema is implied), typmods ignored, arrays as "[]".
func signatureTypeName(tn *pg_query.TypeName) string {
	if tn == nil {
		return ""
	}
	// Svals are already case-folded by the parser unless quoted, and a quoted
	// "Mood" is a different type from mood, so such a name stays quoted (as
	// format_type prints it) and cannot merge with mood when keys are
	// lower-cased.
	names := make([]string, 0, len(tn.GetNames()))
	for _, n := range tn.GetNames() {
		name := n.GetString_().GetSval()
		if name != strings.ToLower(name) {
			name = `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		}
		names = append(names, name)
	}
	if len(names) == 2 && (names[0] == "pg_catalog" || names[0] == "public") {
		names = names[1:]
	}
	name := strings.Join(names, ".")
	if len(names) == 1 {
		if alias, ok := signatureTypeAliases[name]; ok {
			name = alias
		}
	}
	if tn.GetPctType() {
		name += "%type"
	}
	for range tn.GetArrayBounds() {
		name += "[]"
	}
	return name
}

// isRoutineObjectType reports whether objType names a function-like object
// whose identity includes its signature.
func isRoutineObjectType(objType pg_query.ObjectType) bool {
	switch objType {
	case pg_query.ObjectType_OBJECT_FUNCTION, pg_query.ObjectType_OBJECT_PROCEDURE,
		pg_query.ObjectType_OBJECT_ROUTINE:
		return true
	default:
		return false
	}
}
