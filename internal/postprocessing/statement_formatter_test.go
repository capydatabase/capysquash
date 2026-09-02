package postprocessing

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnsureStatementSpacing(t *testing.T) {
	t.Parallel()

	input := "CREATE FUNCTION a() RETURNS text AS $$ SELECT 'x'; $$;CREATE OR REPLACE FUNCTION b() RETURNS text AS $$ SELECT 'y'; $$;CREATE TABLE t(id int);SELECT 1;ALTER TABLE t ADD COLUMN c text;\n\n\n\nCREATE VIEW v AS SELECT 1;"
	got := EnsureStatementSpacing(input)

	assert.Contains(t, got, "$$;\n\nCREATE OR REPLACE FUNCTION")
	assert.Contains(t, got, "$$;\n\nCREATE TABLE")
	assert.Contains(t, got, "; ALTER TABLE")
	assert.False(t, strings.Contains(got, "\n\n\n\n"))
}

func TestFormatFunctionBody(t *testing.T) {
	t.Parallel()

	formatter := NewStatementFormatter()
	input := "CREATE FUNCTION f() RETURNS text LANGUAGE sql AS   $$ SELECT 'x'; $$   LANGUAGE sql;"

	got := formatter.FormatFunctionBody(input)
	assert.Contains(t, got, "AS $$\n")
	assert.Contains(t, got, "$$\nLANGUAGE")
}
