package consolidation

import (
	"testing"

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
