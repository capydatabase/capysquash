package consolidation

import (
	"testing"

	"github.com/capydatabase/capysquash/internal/parser"
	"github.com/capydatabase/capysquash/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseEnumValues(t *testing.T) {
	t.Parallel()

	values := parseEnumValues("'draft', 'ready', 'it''s complicated'")
	require.Len(t, values, 3)
	assert.Equal(t, []string{"draft", "ready", "it's complicated"}, values)
}

func TestExtractAndReplaceCreateEnumValues(t *testing.T) {
	t.Parallel()

	sql := "CREATE TYPE public.status AS ENUM ('draft', 'ready');"

	parsed := extractEnumValuesFromSQL(sql)
	require.Len(t, parsed, 2)
	assert.Equal(t, []string{"draft", "ready"}, parsed)

	rewritten, ok := replaceCreateEnumValues(sql, []string{"draft", "ready", "archived"})
	require.True(t, ok)
	assert.Equal(t, "CREATE TYPE public.status AS ENUM ('draft', 'ready', 'archived');", rewritten)
}

func TestIsCreateEnumStatement(t *testing.T) {
	t.Parallel()

	assert.True(t, isCreateEnumStatement("CREATE TYPE foo AS ENUM ('a', 'b');"))
	assert.False(t, isCreateEnumStatement("ALTER TYPE foo ADD VALUE 'c';"))
}

func TestReplaceCreateEnumValuesEscapesQuotes(t *testing.T) {
	t.Parallel()

	rewritten, ok := replaceCreateEnumValues("CREATE TYPE s AS ENUM ('a');", []string{"a", "it's"})
	require.True(t, ok)
	assert.Equal(t, "CREATE TYPE s AS ENUM ('a', 'it''s');", rewritten)
}

// enumAlter parses one ALTER TYPE statement the way the tracker sees it.
func enumAlter(t *testing.T, sql string) types.Statement {
	t.Helper()
	migration, err := parser.ParseMigration(sql, "001_alter.sql")
	require.NoError(t, err)
	require.Len(t, migration.Statements, 1)
	return migration.Statements[0]
}

func TestApplyEnumAlterationsPlacesValuesLikePostgreSQL(t *testing.T) {
	t.Parallel()

	base := []string{"pending", "verified", "suspended"}
	cases := []struct {
		name       string
		alters     []string
		want       []string
		appendOnly bool
		ok         bool
	}{
		{
			name:       "appends",
			alters:     []string{"ALTER TYPE s ADD VALUE 'active'", "ALTER TYPE s ADD VALUE 'inactive'"},
			want:       []string{"pending", "verified", "suspended", "active", "inactive"},
			appendOnly: true, ok: true,
		},
		{
			name:   "after a label in the middle",
			alters: []string{"ALTER TYPE s ADD VALUE 'active'", "ALTER TYPE s ADD VALUE 'banned' AFTER 'suspended'"},
			want:   []string{"pending", "verified", "suspended", "banned", "active"},
			ok:     true,
		},
		{
			name:   "before",
			alters: []string{"ALTER TYPE s ADD VALUE 'new' BEFORE 'pending'"},
			want:   []string{"new", "pending", "verified", "suspended"},
			ok:     true,
		},
		{
			name:       "after the last label is an append",
			alters:     []string{"ALTER TYPE s ADD VALUE 'closed' AFTER 'suspended'"},
			want:       []string{"pending", "verified", "suspended", "closed"},
			appendOnly: true, ok: true,
		},
		{
			name:       "if not exists on an existing label",
			alters:     []string{"ALTER TYPE s ADD VALUE IF NOT EXISTS 'verified'"},
			want:       base,
			appendOnly: true, ok: true,
		},
		{
			name:   "rename",
			alters: []string{"ALTER TYPE s RENAME VALUE 'verified' TO 'confirmed'"},
			want:   []string{"pending", "confirmed", "suspended"},
			ok:     true,
		},
		{
			name:   "unknown neighbour",
			alters: []string{"ALTER TYPE s ADD VALUE 'x' AFTER 'missing'"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			alters := make([]types.Statement, 0, len(tc.alters))
			for _, sql := range tc.alters {
				alters = append(alters, enumAlter(t, sql))
			}
			got, appendOnly, ok := applyEnumAlterations(base, alters)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.appendOnly, appendOnly)
			if tc.ok {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}
