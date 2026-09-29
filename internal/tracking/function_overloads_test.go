package tracking

import (
	"strings"
	"testing"

	"github.com/capydatabase/capysquash/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func trackSQL(t *testing.T, migrations ...string) *Tracker {
	t.Helper()
	tracker := NewTracker()
	for i, sql := range migrations {
		m, err := parser.ParseMigration(sql, "m.sql")
		require.NoError(t, err)
		tracker.ProcessMigration(m, i+1)
	}
	return tracker
}

func TestOverloadsAreTrackedAsSeparateObjects(t *testing.T) {
	tracker := trackSQL(t,
		"CREATE FUNCTION f(a int) RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;\nCREATE FUNCTION f(a text) RETURNS int LANGUAGE sql AS $$ SELECT 2 $$;",
		"GRANT EXECUTE ON FUNCTION f(integer) TO PUBLIC;\nCOMMENT ON FUNCTION f(text) IS 'text';",
	)

	objects := tracker.GetObjects()
	intFn := objects["public.f(integer)::FUNCTION"]
	textFn := objects["public.f(text)::FUNCTION"]
	require.NotNil(t, intFn)
	require.NotNil(t, textFn)
	assert.Len(t, intFn.History, 2, "create + grant")
	assert.Len(t, textFn.History, 1)
	assert.Equal(t, "public.f", intFn.Name, "the lifecycle name stays the plain function name")
	assert.NotNil(t, objects["public.f(text)::COMMENT"])
}

// DROP FUNCTION without arguments names the only overload there is.
func TestUnqualifiedDropResolvesTheOnlyOverload(t *testing.T) {
	tracker := trackSQL(t,
		"CREATE FUNCTION g(a int) RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;",
		"DROP FUNCTION g;",
	)

	g := tracker.GetObjects()["public.g(integer)::FUNCTION"]
	require.NotNil(t, g)
	require.Len(t, g.History, 2)
	assert.Nil(t, g.GetFinalState(), "dropped")
	assert.NotContains(t, tracker.GetObjects(), "public.g::FUNCTION")
}

// A trigger names its function without a signature; the overload-keyed
// function still satisfies the reference.
func TestFunctionReferencesWithoutSignatureAreSatisfied(t *testing.T) {
	tracker := trackSQL(t, `
CREATE TABLE t (id int);
CREATE FUNCTION touch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
CREATE TRIGGER t_touch BEFORE UPDATE ON t FOR EACH ROW EXECUTE FUNCTION touch();
`)

	for _, warning := range tracker.ValidateConsistency() {
		assert.False(t, strings.Contains(warning, "touch"), "unexpected warning: %s", warning)
	}
}

// COMMENT ON FUNCTION f without arguments, written while f had one overload,
// is about that overload; its SQL names the arguments so it stays valid once
// a later migration adds another overload.
func TestShortFormStatementsGetTheArgumentsOfTheOnlyOverload(t *testing.T) {
	tracker := trackSQL(t,
		"CREATE FUNCTION h(a varchar(20), b int[]) RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;\nCOMMENT ON FUNCTION h IS 'first';\nGRANT EXECUTE ON FUNCTION h TO PUBLIC;",
		"CREATE FUNCTION h(a text) RETURNS int LANGUAGE sql AS $$ SELECT 2 $$;",
	)

	comment := tracker.GetObjects()["public.h(character varying,integer[])::COMMENT"]
	require.NotNil(t, comment)
	require.Len(t, comment.History, 1)
	assert.Equal(t, "COMMENT ON FUNCTION h(varchar(20), int[]) IS 'first'", comment.History[0].Statement.SQL)

	fn := tracker.GetObjects()["public.h(character varying,integer[])::FUNCTION"]
	require.NotNil(t, fn)
	require.Len(t, fn.History, 2)
	assert.Equal(t, "GRANT execute ON FUNCTION h(varchar(20), int[]) TO public", fn.History[1].Statement.SQL, "pg_query deparse spelling")
}

// After one of two overloads is dropped, a short-form statement refers to the
// overload that is left, as it does in PostgreSQL.
func TestShortFormAfterADropRefersToTheRemainingOverload(t *testing.T) {
	tracker := trackSQL(t,
		"CREATE FUNCTION k(a int) RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;\nCREATE FUNCTION k(a text) RETURNS int LANGUAGE sql AS $$ SELECT 2 $$;",
		"DROP FUNCTION k(text);\nDROP FUNCTION k;",
	)

	intFn := tracker.GetObjects()["public.k(integer)::FUNCTION"]
	require.NotNil(t, intFn)
	require.Len(t, intFn.History, 2, "create + the short-form drop")
	assert.Nil(t, intFn.GetFinalState())
	assert.NotContains(t, tracker.GetObjects(), "public.k::FUNCTION")
}

// A trigger on a function that was dropped still warns.
func TestReferenceToADroppedFunctionWarns(t *testing.T) {
	tracker := trackSQL(t, `
CREATE TABLE t (id int);
CREATE FUNCTION gone() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
DROP FUNCTION gone();
CREATE TRIGGER t_gone BEFORE UPDATE ON t FOR EACH ROW EXECUTE FUNCTION gone();
`)

	found := false
	for _, warning := range tracker.ValidateConsistency() {
		if strings.Contains(warning, "gone") {
			found = true
		}
	}
	assert.True(t, found, "a trigger on a dropped function must warn")
}

// Quoted type names keep their case: "Mood" and mood are different types.
func TestQuotedTypeNamesKeepTheirCase(t *testing.T) {
	tracker := trackSQL(t,
		`CREATE TYPE mood AS ENUM ('a'); CREATE TYPE "Mood" AS ENUM ('b');
CREATE FUNCTION m(a mood) RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;
CREATE FUNCTION m(a "Mood") RETURNS int LANGUAGE sql AS $$ SELECT 2 $$;`,
	)
	assert.Contains(t, tracker.GetObjects(), "public.m(mood)::FUNCTION")
	assert.Contains(t, tracker.GetObjects(), `public.m("mood")::FUNCTION`, "keys are lower-cased; the quotes keep it apart")
	count := 0
	for key := range tracker.GetObjects() {
		if strings.HasPrefix(key, "public.m(") {
			count++
		}
	}
	assert.Equal(t, 2, count)
}
