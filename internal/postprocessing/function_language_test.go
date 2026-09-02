package postprocessing

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRemoveDuplicateLanguageDeclarations(t *testing.T) {
	t.Parallel()

	input := "CREATE FUNCTION f() RETURNS text LANGUAGE plpgsql STABLE LANGUAGE plpgsql AS $$ SELECT 'x'; $$;"
	got := RemoveDuplicateLanguageDeclarations(input)

	assert.Equal(t, 1, strings.Count(strings.ToUpper(got), "LANGUAGE PLPGSQL"))
	assert.Contains(t, strings.ToUpper(got), "STABLE")
}

func TestRemoveDuplicateLanguageDeclarations_NoChange(t *testing.T) {
	t.Parallel()

	input := "CREATE FUNCTION f() RETURNS text LANGUAGE sql AS $$ SELECT 'x'; $$;"
	got := RemoveDuplicateLanguageDeclarations(input)

	assert.Equal(t, input, got)
}

func TestFixFunctionLanguageConflicts_MovesTrailingLanguageClause(t *testing.T) {
	t.Parallel()

	input := `CREATE OR REPLACE FUNCTION public.f()
RETURNS text
STABLE AS $$
SELECT 'x';
$$ LANGUAGE sql;`

	got := FixFunctionLanguageConflicts(input)
	upper := strings.ToUpper(got)

	assert.Contains(t, upper, "LANGUAGE SQL")
	assert.Contains(t, upper, "STABLE")
	assert.NotContains(t, upper, "$$ LANGUAGE SQL")
}

func TestFixIncorrectLanguageDeclarations_SQLToPlpgsql(t *testing.T) {
	t.Parallel()

	input := `CREATE FUNCTION public.normalize_email(input text)
RETURNS text
LANGUAGE sql
AS $$
BEGIN
  RETURN lower(input);
END;
$$;`

	got := FixIncorrectLanguageDeclarations(input)
	assert.Contains(t, strings.ToUpper(got), "LANGUAGE PLPGSQL")
}

func TestFixIncorrectLanguageDeclarations_PlsqlToSQL(t *testing.T) {
	t.Parallel()

	input := `CREATE FUNCTION public.identity_email(input text)
RETURNS text
LANGUAGE plpgsql
AS $$
SELECT lower(input);
$$;`

	got := FixIncorrectLanguageDeclarations(input)
	assert.Contains(t, strings.ToUpper(got), "LANGUAGE SQL")
	assert.NotContains(t, strings.ToUpper(got), "LANGUAGE PLPGSQL")
}

func TestFixMissingLanguageDeclarations_AddsInferredLanguage(t *testing.T) {
	t.Parallel()

	input := `CREATE FUNCTION public.f(input text)
RETURNS text
AS $$
SELECT lower(input);
$$;`

	got := FixMissingLanguageDeclarations(input)
	assert.Contains(t, strings.ToUpper(got), "LANGUAGE SQL")
}

func TestFixMissingLanguageDeclarations_RespectsTrailingLanguage(t *testing.T) {
	t.Parallel()

	input := `CREATE FUNCTION public.f(input text)
RETURNS text
AS $$
SELECT lower(input);
$$ LANGUAGE sql;`

	got := FixMissingLanguageDeclarations(input)
	assert.Equal(t, input, got)
}
