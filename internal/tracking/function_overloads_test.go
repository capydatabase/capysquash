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
