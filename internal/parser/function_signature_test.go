package parser

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFunctionSignaturesNormalizeTypeSpellings(t *testing.T) {
	tests := []struct {
		sql       string
		signature string
	}{
		{"CREATE FUNCTION f(a int, b varchar(10), c int4[]) RETURNS int LANGUAGE sql AS $$ SELECT 1 $$", "(integer,character varying,integer[])"},
		{"CREATE FUNCTION f(a integer, b character varying, c integer[]) RETURNS int LANGUAGE sql AS $$ SELECT 1 $$", "(integer,character varying,integer[])"},
		{"CREATE FUNCTION f(IN a bigint, OUT total numeric, INOUT b bool, VARIADIC rest text[]) LANGUAGE sql AS $$ SELECT 1 $$", "(bigint,boolean,text[])"},
		{"CREATE FUNCTION f(a public.mood, b app.mood) RETURNS TABLE (x int) LANGUAGE sql AS $$ SELECT 1 $$", "(mood,app.mood)"},
		{"CREATE FUNCTION f() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$", "()"},
		{"CREATE PROCEDURE p(a timestamptz) LANGUAGE sql AS $$ SELECT 1 $$", "(timestamp with time zone)"},
		{"DROP FUNCTION f(int, varchar)", "(integer,character varying)"},
		{"DROP FUNCTION f", ""},
		{"DROP FUNCTION f()", "()"},
		{"COMMENT ON FUNCTION public.f(float8) IS 'x'", "(double precision)"},
		{"GRANT EXECUTE ON FUNCTION f(int8) TO PUBLIC", "(bigint)"},
		{"REVOKE EXECUTE ON FUNCTION f(int8) FROM PUBLIC", "(bigint)"},
	}
	for _, tc := range tests {
		t.Run(tc.sql, func(t *testing.T) {
			m, err := ParseMigration(tc.sql+";", "sig.sql")
			require.NoError(t, err)
			require.Len(t, m.Statements, 1)
			assert.Equal(t, tc.signature, m.Statements[0].FunctionSignature)
		})
	}
}

func TestStatementsNotAboutFunctionsHaveNoSignature(t *testing.T) {
	m, err := ParseMigration("CREATE TABLE t (id int); COMMENT ON TABLE t IS 'x'; GRANT SELECT ON t TO PUBLIC; DROP TABLE t;", "sig.sql")
	require.NoError(t, err)
	for _, s := range m.Statements {
		assert.Empty(t, s.FunctionSignature, s.SQL)
	}
}
