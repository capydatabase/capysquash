package squasher

import (
	"strings"
	"testing"

	"github.com/capydatabase/capysquash/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Overloads are separate objects: replacing, dropping and commenting on one
// overload must not touch the others.
func TestSquashKeepsFunctionOverloadsApart(t *testing.T) {
	// Paranoid is left out: it requires a production database.
	for _, level := range []string{"conservative", "standard", "aggressive"} {
		t.Run(level, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.SafetyLevel = level
			cfg.ProdDBDSN = ""
			e, err := NewEngine(EngineConfig{Config: cfg})
			require.NoError(t, err)
			t.Cleanup(func() { _ = e.Close() })

			result, err := e.SquashWithSeparateFiles(map[int]string{
				1: `
CREATE FUNCTION public.fmt(v integer) RETURNS text LANGUAGE sql AS $$ SELECT v::text $$;
CREATE FUNCTION public.fmt(v text) RETURNS text LANGUAGE sql AS $$ SELECT v $$;
COMMENT ON FUNCTION public.fmt(integer) IS 'int version';
COMMENT ON FUNCTION public.fmt(text) IS 'text version';
`,
				2: `
CREATE OR REPLACE FUNCTION public.fmt(v int4) RETURNS text LANGUAGE sql AS $$ SELECT 'n=' || v::text $$;
DROP FUNCTION public.fmt(text);
CREATE FUNCTION public.fmt(v boolean) RETURNS text LANGUAGE sql AS $$ SELECT v::text $$;
`,
			})
			require.NoError(t, err)
			sql := result.BaselineSQL

			assert.Contains(t, sql, "CREATE OR REPLACE FUNCTION public.fmt(v int4) RETURNS text LANGUAGE sql AS $$ SELECT 'n=' || v::text $$")
			assert.NotContains(t, sql, "SELECT v::text $$;\n\nCREATE FUNCTION public.fmt(v integer)", "the replaced integer overload is emitted once")
			assert.Equal(t, 1, strings.Count(sql, "public.fmt(v int"), "one integer overload")
			assert.Contains(t, sql, "CREATE FUNCTION public.fmt(v boolean)")
			assert.NotContains(t, sql, "public.fmt(v text)", "the dropped text overload is gone")
			assert.Contains(t, sql, "COMMENT ON FUNCTION public.fmt(integer) IS 'int version'")
			assert.NotContains(t, sql, "text version", "the comment on the dropped overload is gone")
			assert.NotContains(t, sql, ";;")
		})
	}
}
