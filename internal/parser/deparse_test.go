package parser

import (
	"testing"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A node built by hand carries no parse tree version; since pg_query_go 6.2.5
// Deparse rejects such a tree ("protobuf parse tree version mismatch: 0").
func TestDeparseNodeRendersADetachedNode(t *testing.T) {
	t.Parallel()

	node := &pg_query.Node{Node: &pg_query.Node_CreateSchemaStmt{CreateSchemaStmt: &pg_query.CreateSchemaStmt{
		Schemaname:  "app",
		IfNotExists: true,
	}}}

	sql, err := DeparseNode(node)
	require.NoError(t, err)
	assert.Equal(t, "CREATE SCHEMA IF NOT EXISTS app", sql)
}
