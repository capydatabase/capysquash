package squasher

import (
	"strings"
	"testing"

	"github.com/capy-base/pgsquash-engine/internal/parser"
	"github.com/capy-base/pgsquash-engine/internal/tracking"
	"github.com/capy-base/pgsquash-engine/internal/utils"
	"github.com/stretchr/testify/require"
)

func newIndexOptimizationTestEngine(t *testing.T, createTableSQL string) *Engine {
	t.Helper()

	tracker := tracking.NewTracker()
	migration, err := parser.ParseMigration(createTableSQL, "001.sql")
	require.NoError(t, err)

	tracker.ProcessMigration(migration, 1)

	return &Engine{
		tracker: tracker,
		logger:  utils.GetDefaultLogger().WithPrefix("ENGINE-TEST"),
	}
}

func TestOptimizeIndexTypes_DoesNotForceGiSTForSpatialDefaults(t *testing.T) {
	e := newIndexOptimizationTestEngine(t, `CREATE TABLE profiles (coordinates POINT);`)

	input := `CREATE INDEX idx_profiles_coordinates ON public.profiles (coordinates) WHERE (coordinates IS NOT NULL);`
	output := e.optimizeIndexTypes(input)

	require.NotContains(t, strings.ToLower(output), "using gist")
}

func TestOptimizeIndexTypes_FixesInvalidGiSTForArrays(t *testing.T) {
	e := newIndexOptimizationTestEngine(t, `CREATE TABLE profiles (coordinates DOUBLE PRECISION[]);`)

	input := `CREATE INDEX idx_profiles_coordinates ON public.profiles USING gist (coordinates) WHERE (coordinates IS NOT NULL);`
	output := e.optimizeIndexTypes(input)

	require.Contains(t, strings.ToLower(output), "using btree")
	require.NotContains(t, strings.ToLower(output), "using gist")
}
