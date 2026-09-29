package types

import (
	"context"
	"testing"

	pg_query "github.com/pganalyze/pg_query_go/v6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parsedStatements(t *testing.T, sqls ...string) []Statement {
	t.Helper()
	stmts := make([]Statement, 0, len(sqls))
	for _, s := range sqls {
		tree, err := pg_query.Parse(s)
		require.NoError(t, err)
		stmts = append(stmts, Statement{SQL: s, ParseTree: tree})
	}
	return stmts
}

// A type change knows the type it changes from, from the earlier migrations,
// and whether it can lose data.
func TestAnalyzeMigrationTypesTracksTheTypeAColumnChangesFrom(t *testing.T) {
	ta := NewTypeAnalyzer(NewPostgreSQLTypeSystem("17"), nil)

	analysis, err := ta.AnalyzeMigrationTypes(context.Background(), parsedStatements(t,
		"CREATE TABLE app.users (id int, name varchar(50), bio text)",
		"ALTER TABLE app.users ALTER COLUMN id TYPE bigint, ALTER COLUMN name TYPE varchar(20)",
		"ALTER TABLE app.users RENAME COLUMN bio TO about",
		"ALTER TABLE app.users ALTER COLUMN about TYPE varchar(100)",
		"ALTER TABLE app.users ALTER COLUMN id TYPE int",
	))
	require.NoError(t, err)

	got := map[string]TypeChange{}
	var order []string
	for _, c := range analysis.TypeChanges {
		key := c.Column + ":" + c.ToType
		got[key] = *c
		order = append(order, key)
	}
	assert.Equal(t, []string{"id:int8", "name:varchar(20)", "about:varchar(100)", "id:int4"}, order)

	assert.Equal(t, TypeChange{Table: "app.users", Column: "id", FromType: "int4", ToType: "int8", Reversible: false, DataLoss: false}, got["id:int8"],
		"widening int -> bigint loses nothing; going back is lossy, so it is not reversible")
	assert.Equal(t, "varchar(50)", got["name:varchar(20)"].FromType)
	assert.True(t, got["name:varchar(20)"].DataLoss, "shrinking a varchar can reject rows")
	assert.Equal(t, "text", got["about:varchar(100)"].FromType, "the rename carries the column type")
	assert.True(t, got["about:varchar(100)"].DataLoss)
	assert.Equal(t, "int8", got["id:int4"].FromType, "the earlier ALTER is the new starting point")
	assert.True(t, got["id:int4"].DataLoss)
	assert.Empty(t, analysis.Warnings)
}

func TestAnalyzeMigrationTypesWarnsWhenThePreviousTypeIsUnknown(t *testing.T) {
	ta := NewTypeAnalyzer(NewPostgreSQLTypeSystem("17"), nil)

	analysis, err := ta.AnalyzeMigrationTypes(context.Background(), parsedStatements(t,
		"ALTER TABLE legacy ALTER COLUMN amount TYPE numeric(12,2)",
	))
	require.NoError(t, err)
	require.Len(t, analysis.TypeChanges, 1)
	assert.Equal(t, "unknown", analysis.TypeChanges[0].FromType)
	assert.Equal(t, "public.legacy", analysis.TypeChanges[0].Table)
	assert.False(t, analysis.TypeChanges[0].Reversible)
	require.Len(t, analysis.Warnings, 1)
	assert.Contains(t, analysis.Warnings[0], "public.legacy.amount")
}
