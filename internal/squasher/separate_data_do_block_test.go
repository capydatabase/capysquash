package squasher

import (
	"strings"
	"testing"

	"github.com/capysquash/pgsquash-engine/internal/config"
	"github.com/capysquash/pgsquash-engine/internal/parser"
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

func TestSquash_DOBlockTableAlterPrecedesDependentIndex(t *testing.T) {
	t.Parallel()

	e, err := NewEngine(EngineConfig{
		Config: &config.Config{SafetyLevel: "standard"},
	})
	require.NoError(t, err)

	migrations := map[int]string{
		1: `CREATE TABLE subscription_plans (
  id BIGINT PRIMARY KEY,
  is_active BOOLEAN NOT NULL DEFAULT true
);`,
		2: `DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_name = 'subscription_plans' AND column_name = 'is_popular'
  ) THEN
    ALTER TABLE subscription_plans ADD COLUMN is_popular BOOLEAN DEFAULT false;
  END IF;
END $$;`,
		3: `CREATE INDEX idx_subscription_plans_is_popular
ON subscription_plans(is_popular) WHERE is_active = true;`,
	}

	result, err := e.SquashWithSeparateFiles(migrations)
	require.NoError(t, err)
	require.NotNil(t, result)

	alterPosition := strings.Index(result.BaselineSQL, "ALTER TABLE subscription_plans ADD COLUMN is_popular")
	indexPosition := strings.Index(result.BaselineSQL, "CREATE INDEX idx_subscription_plans_is_popular")
	require.GreaterOrEqual(t, alterPosition, 0, "conditional table alteration must remain in baseline")
	require.Greater(t, indexPosition, alterPosition, "dependent index must follow the column addition")
	require.Equal(t, 1, strings.Count(result.BaselineSQL, "ADD COLUMN is_popular"))
}

func TestSquash_DOBlockTableAltersPreserveStatementOrder(t *testing.T) {
	t.Parallel()

	e, err := NewEngine(EngineConfig{
		Config: &config.Config{SafetyLevel: "standard"},
	})
	require.NoError(t, err)

	migrations := map[int]string{
		1: `CREATE TABLE widgets (id BIGINT PRIMARY KEY);`,
		2: `DO $$
BEGIN
  ALTER TABLE widgets ADD COLUMN color TEXT;
  ALTER TABLE widgets ALTER COLUMN color SET DEFAULT 'blue';
END $$;`,
		3: `CREATE INDEX idx_widgets_color ON widgets(color);`,
	}

	result, err := e.SquashWithSeparateFiles(migrations)
	require.NoError(t, err)
	require.NotNil(t, result)

	addPosition := strings.Index(result.BaselineSQL, "ALTER TABLE widgets ADD COLUMN color")
	defaultPosition := strings.Index(result.BaselineSQL, "ALTER TABLE widgets ALTER COLUMN color SET DEFAULT")
	indexPosition := strings.Index(result.BaselineSQL, "CREATE INDEX idx_widgets_color")
	require.GreaterOrEqual(t, addPosition, 0)
	require.Greater(t, defaultPosition, addPosition)
	require.Greater(t, indexPosition, defaultPosition)
	_, err = parser.ParseMigration(result.BaselineSQL, "ordered_alters_baseline.sql")
	require.NoError(t, err)
}

func TestInsertSQLAfterCreateTable_PrecedesIntegratedIndex(t *testing.T) {
	t.Parallel()

	tableSQL := `CREATE TABLE subscription_plans (
  id BIGINT PRIMARY KEY,
  is_active BOOLEAN NOT NULL DEFAULT true
);

CREATE INDEX idx_subscription_plans_is_popular
ON subscription_plans(is_popular) WHERE is_active = true;`

	result, inserted := insertSQLAfterCreateTable(
		tableSQL,
		"ALTER TABLE subscription_plans ADD COLUMN is_popular BOOLEAN DEFAULT false;",
	)
	require.True(t, inserted)

	createPosition := strings.Index(result, "CREATE TABLE subscription_plans")
	alterPosition := strings.Index(result, "ALTER TABLE subscription_plans ADD COLUMN is_popular")
	indexPosition := strings.Index(result, "CREATE INDEX idx_subscription_plans_is_popular")
	require.GreaterOrEqual(t, createPosition, 0)
	require.Greater(t, alterPosition, createPosition)
	require.Greater(t, indexPosition, alterPosition)
}

func TestTableAlterAlreadyApplied_DetectsIntegratedColumn(t *testing.T) {
	t.Parallel()

	migration, err := parser.ParseMigration(
		"ALTER TABLE profiles ADD COLUMN field_of_study TEXT;",
		"integrated_alter.sql",
	)
	require.NoError(t, err)
	require.Len(t, migration.Statements, 1)

	tableSQL := `CREATE TABLE profiles (
  id BIGINT PRIMARY KEY,
  field_of_study TEXT
);

CREATE INDEX idx_profiles_field_of_study ON profiles(field_of_study);`

	require.True(t, tableAlterAlreadyApplied(tableSQL, migration.Statements[0]))
}

func TestTableAlterAlreadyApplied_DetectsAlreadyAbsentDroppedColumn(t *testing.T) {
	t.Parallel()

	migration, err := parser.ParseMigration(
		"ALTER TABLE profiles DROP COLUMN profile_image_url;",
		"integrated_drop.sql",
	)
	require.NoError(t, err)
	require.Len(t, migration.Statements, 1)

	tableSQL := `CREATE TABLE profiles (
  id BIGINT PRIMARY KEY,
  avatar_url TEXT
);`

	require.True(t, tableAlterAlreadyApplied(tableSQL, migration.Statements[0]))
}
