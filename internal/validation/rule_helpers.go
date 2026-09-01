package validation

import (
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

func relationName(relation *pg_query.RangeVar) string {
	if relation == nil {
		return ""
	}

	if relation.Schemaname != "" {
		return relation.Schemaname + "." + relation.Relname
	}

	return relation.Relname
}

func nodeQualifiedName(node *pg_query.Node) string {
	if node == nil {
		return ""
	}

	if list := node.GetList(); list != nil {
		parts := make([]string, 0, len(list.Items))
		for _, item := range list.Items {
			if str := item.GetString_(); str != nil {
				parts = append(parts, str.Sval)
			}
		}

		return strings.Join(parts, ".")
	}

	if args := node.GetObjectWithArgs(); args != nil {
		parts := make([]string, 0, len(args.Objname))
		for _, item := range args.Objname {
			if str := item.GetString_(); str != nil {
				parts = append(parts, str.Sval)
			}
		}

		return strings.Join(parts, ".")
	}

	return ""
}

func constraintKey(tableName, constraintName string) string {
	return strings.ToLower(strings.TrimSpace(tableName)) + "|" + strings.ToLower(strings.TrimSpace(constraintName))
}
