package postprocessing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFixMisplacedLanguageAfterReturnsTable(t *testing.T) {
	t.Parallel()

	input := "CREATE FUNCTION f() RETURNS TABLE LANGUAGE plpgsql (id bigint) AS $$ SELECT 1; $$;"
	got, count := fixMisplacedLanguageAfterReturnsTable(input)

	assert.Equal(t, 1, count)
	assert.Equal(t, "CREATE FUNCTION f() RETURNS TABLE (id bigint) AS $$ SELECT 1; $$;", got)
}

func TestFixMisplacedLanguageAfterReturnsTable_NoChange(t *testing.T) {
	t.Parallel()

	input := "CREATE FUNCTION f() RETURNS TABLE (id bigint) LANGUAGE plpgsql AS $$ SELECT 1; $$;"
	got, count := fixMisplacedLanguageAfterReturnsTable(input)

	assert.Equal(t, 0, count)
	assert.Equal(t, input, got)
}
