package squasher

import (
	"testing"

	"github.com/capysquash/pgsquash-engine/internal/config"
	"github.com/stretchr/testify/require"
)

func TestSquashWithSeparateFiles_RoutesDataDoBlocksToDataFile(t *testing.T) {
	t.Parallel()

	e, err := NewEngine(EngineConfig{
		Config: &config.Config{SafetyLevel: "standard"},
	})
	require.NoError(t, err)
	require.NotNil(t, e)

	migrations := map[int]string{
		1: `
CREATE TABLE profiles (
  id TEXT PRIMARY KEY
);

CREATE TABLE properties (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id TEXT NOT NULL REFERENCES profiles (id)
);

INSERT INTO profiles (id) VALUES ('owner_1');

DO $$
BEGIN
  INSERT INTO properties (owner_id) VALUES ('owner_1');
END $$;
`,
	}

	result, err := e.SquashWithSeparateFiles(migrations)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotEmpty(t, result.DataOperationsSQL)
	require.Contains(t, result.DataOperationsSQL, "INSERT INTO profiles (id) VALUES ('owner_1')")
	require.Contains(t, result.DataOperationsSQL, "INSERT INTO properties (owner_id) VALUES ('owner_1')")

	require.NotContains(t, result.BaselineSQL, "INSERT INTO profiles (id) VALUES ('owner_1')")
	require.NotContains(t, result.BaselineSQL, "INSERT INTO properties (owner_id) VALUES ('owner_1')")
}
