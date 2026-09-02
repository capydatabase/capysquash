package volatility

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolatilityFixerFix_AddsRegisteredMarker(t *testing.T) {
	t.Parallel()

	registry := NewFunctionRegistry()
	registry.Register("jwt", Stable)

	fixer := NewVolatilityFixer(registry)
	input := "CREATE FUNCTION auth.jwt() RETURNS jsonb LANGUAGE sql AS $$ SELECT '{}'::jsonb; $$;"

	got, err := fixer.Fix(input)
	require.NoError(t, err)

	upper := strings.ToUpper(got)
	assert.Contains(t, upper, "FUNCTION AUTH.JWT()")
	assert.Contains(t, upper, "STABLE")
}

func TestVolatilityFixerFix_DoesNotDuplicateExistingMarker(t *testing.T) {
	t.Parallel()

	registry := NewFunctionRegistry()
	registry.Register("jwt", Stable)

	fixer := NewVolatilityFixer(registry)
	input := "CREATE FUNCTION auth.jwt() RETURNS jsonb LANGUAGE sql STABLE AS $$ SELECT '{}'::jsonb; $$;"

	got, err := fixer.Fix(input)
	require.NoError(t, err)

	assert.Equal(t, 1, strings.Count(strings.ToUpper(got), "STABLE"))
}
