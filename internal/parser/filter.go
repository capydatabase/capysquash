package parser

import (
	pgquery "github.com/pganalyze/pg_query_go/v6"
)

// StmtDecl binds an AST statement node to its source byte range.
//
// Adapted from potential-tools/pgvet/rules/filter.go.
type StmtDecl[T any] struct {
	Stmt  T
	Start int32
	End   int32
}

// FilterStatements returns all statements in the parse tree that match T,
// preserving each statement's source range.
func FilterStatements[T any](rawStmts []*pgquery.RawStmt) []StmtDecl[T] {
	var filtered []StmtDecl[T]
	for _, raw := range rawStmts {
		stmt := raw.GetStmt()
		if stmt == nil {
			continue
		}

		target, ok := stmt.GetNode().(T)
		if !ok {
			continue
		}

		filtered = append(filtered, StmtDecl[T]{
			Stmt:  target,
			Start: raw.StmtLocation,
			End:   raw.StmtLocation + raw.StmtLen,
		})
	}

	return filtered
}
