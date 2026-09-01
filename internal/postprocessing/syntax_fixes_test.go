package postprocessing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFixDropPolicyDeparseCorruption(t *testing.T) {
	t.Parallel()

	input := "DROP POLICY IF EXISTS public.users.user_policy ON public.users;"
	got := FixDropPolicyDeparseCorruption(input)

	assert.Equal(t, "DROP POLICY IF EXISTS user_policy ON public.users;", got)
}

func TestFixMalformedFunctions_OrphanedVolatility(t *testing.T) {
	t.Parallel()

	input := "$$;STABLE; CREATE OR REPLACE FUNCTION f() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql;"
	got := FixMalformedFunctions(input)

	assert.Contains(t, got, "$$;\nCREATE OR REPLACE FUNCTION")
}

func TestFixMalformedFunctions_RemoveStableWhenImmutablePresent(t *testing.T) {
	t.Parallel()

	input := "CREATE FUNCTION f() RETURNS int STABLE IMMUTABLE AS $$ SELECT 1; $$ LANGUAGE sql;"
	got := FixMalformedFunctions(input)

	assert.NotContains(t, got, " STABLE ")
	assert.Contains(t, got, "IMMUTABLE")
}

func TestFixMissingLanguageClauses_SplitsConcatenatedFunctions(t *testing.T) {
	t.Parallel()

	input := "END;\n$$;STABLE; CREATE OR REPLACE FUNCTION public.f() RETURNS int AS $$ SELECT 1; $$;"
	got := FixMissingLanguageClauses(input)

	assert.Contains(t, got, "$$;\n\nCREATE OR REPLACE FUNCTION")
}

func TestFixMissingLanguageClauses_AddsLanguage(t *testing.T) {
	t.Parallel()

	input := `CREATE OR REPLACE FUNCTION public.f()
RETURNS int
AS $$
SELECT 1;
$$;`

	got := FixMissingLanguageClauses(input)
	assert.Contains(t, got, "RETURNS int\n LANGUAGE plpgsql AS $$")
}
