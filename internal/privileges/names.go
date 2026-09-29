package privileges

import (
	"regexp"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

var plainIdentifier = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// quoteIdent renders an identifier as PostgreSQL needs it: bare when it is a
// plain lower-case name the scanner reads back as an identifier (so not a
// keyword), double-quoted otherwise.
func quoteIdent(name string) string {
	if plainIdentifier.MatchString(name) {
		if scan, err := pg_query.Scan(name); err == nil && len(scan.GetTokens()) == 1 &&
			scan.GetTokens()[0].GetToken() == pg_query.Token_IDENT {
			return name
		}
	}
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

// quoteLiteral renders a string literal.
func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

// qualified renders schema.name.
func qualified(schema, name string) string {
	if schema == "" {
		return quoteIdent(name)
	}
	return quoteIdent(schema) + "." + quoteIdent(name)
}

// stringList returns the svals of a list of String nodes.
func stringList(nodes []*pg_query.Node) []string {
	out := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if s := node.GetString_(); s != nil {
			out = append(out, s.GetSval())
		}
	}
	return out
}

// nameParts returns the dotted name a node spells: a List of String nodes, a
// bare String, a TypeName or the name of an ObjectWithArgs.
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
	case node.GetObjectWithArgs() != nil:
		return stringList(node.GetObjectWithArgs().GetObjname())
	default:
		return nil
	}
}

// splitName separates an optional schema from an object name; a
// database-qualified name keeps its last two parts.
func splitName(parts []string) (schema, name string) {
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return "", parts[0]
	default:
		return parts[len(parts)-2], parts[len(parts)-1]
	}
}

// deparse renders a single statement node back to SQL.
func deparse(node *pg_query.Node) (string, error) {
	return pg_query.Deparse(&pg_query.ParseResult{Stmts: []*pg_query.RawStmt{{Stmt: node}}})
}
