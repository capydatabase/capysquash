package parser

import (
	"strings"
	"testing"

	pgquery "github.com/pganalyze/pg_query_go/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterStatements(t *testing.T) {
	t.Parallel()

	t.Run("finds single matching statement", func(t *testing.T) {
		t.Parallel()

		tree := mustParseFilterTest(t, "ALTER TABLE widgets ADD COLUMN id INTEGER;")
		require.Len(t, tree.Stmts, 1)

		filtered := FilterStatements[*pgquery.Node_AlterTableStmt](tree.Stmts)
		require.Len(t, filtered, 1)
		assert.NotNil(t, filtered[0].Stmt.AlterTableStmt)
		assert.GreaterOrEqual(t, filtered[0].Start, int32(0))
		assert.Greater(t, filtered[0].End, filtered[0].Start)
	})

	t.Run("finds multiple matching statements", func(t *testing.T) {
		t.Parallel()

		var b strings.Builder
		b.WriteString("ALTER TABLE widgets ADD COLUMN id INTEGER;")
		b.WriteString("ALTER TABLE widgets DROP COLUMN id;")

		tree := mustParseFilterTest(t, b.String())
		require.Len(t, tree.Stmts, 2)

		filtered := FilterStatements[*pgquery.Node_AlterTableStmt](tree.Stmts)
		require.Len(t, filtered, 2)
		assert.NotNil(t, filtered[0].Stmt.AlterTableStmt)
		assert.NotNil(t, filtered[1].Stmt.AlterTableStmt)
		assert.NotEqual(t, filtered[0].Start, filtered[1].Start)
	})

	t.Run("filters non-matching statements", func(t *testing.T) {
		t.Parallel()

		var b strings.Builder
		b.WriteString("CREATE INDEX ON widgets (id);")
		b.WriteString("ALTER TABLE widgets DROP COLUMN id;")
		b.WriteString("CREATE TABLE widgets2 (id INTEGER);")

		tree := mustParseFilterTest(t, b.String())
		require.Len(t, tree.Stmts, 3)

		filtered := FilterStatements[*pgquery.Node_CreateStmt](tree.Stmts)
		require.Len(t, filtered, 1)
		assert.NotNil(t, filtered[0].Stmt.CreateStmt)
	})
}

func mustParseFilterTest(t *testing.T, sql string) *pgquery.ParseResult {
	t.Helper()

	tree, err := pgquery.Parse(sql)
	require.NoError(t, err)

	return tree
}
