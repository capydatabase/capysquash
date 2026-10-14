package parser

import (
	"fmt"
	"sync"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// parseTreeVersion is the PG_VERSION_NUM libpg_query stamps on every parse tree.
// pg_query_go does not export it, and since 6.2.5 Deparse rejects a tree whose
// version differs, so it is read once from a trivial parse instead of being
// hardcoded (a hardcoded value would break again on the next libpg_query bump).
var parseTreeVersion = sync.OnceValues(func() (int32, error) {
	tree, err := pg_query.Parse("SELECT 1")
	if err != nil {
		return 0, fmt.Errorf("read the libpg_query parse tree version: %w", err)
	}
	return tree.GetVersion(), nil
})

// DeparseNode renders a single statement node back to SQL. Use it for nodes that
// were built or detached from their original ParseResult; a whole ParseResult
// from pg_query.Parse already carries its version and can go to pg_query.Deparse.
func DeparseNode(node *pg_query.Node) (string, error) {
	version, err := parseTreeVersion()
	if err != nil {
		return "", err
	}
	return pg_query.Deparse(&pg_query.ParseResult{Version: version, Stmts: []*pg_query.RawStmt{{Stmt: node}}})
}
