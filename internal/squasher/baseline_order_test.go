package squasher

import (
	"strings"
	"testing"

	"github.com/capydatabase/capysquash/internal/config"
	"github.com/stretchr/testify/require"
)

// squashAt squashes migrations at a safety level and returns the baseline.
func squashAt(t *testing.T, level string, migrations map[int]string) string {
	t.Helper()
	e, err := NewEngine(EngineConfig{Config: &config.Config{SafetyLevel: level}})
	require.NoError(t, err)
	result, err := e.SquashWithSeparateFiles(migrations)
	require.NoError(t, err)
	return result.BaselineSQL
}

// requireBefore fails unless first occurs in sql, and before second.
func requireBefore(t *testing.T, sql, first, second string) {
	t.Helper()
	i, j := strings.Index(sql, first), strings.Index(sql, second)
	require.GreaterOrEqual(t, i, 0, "baseline lacks %q:\n%s", first, sql)
	require.GreaterOrEqual(t, j, 0, "baseline lacks %q:\n%s", second, sql)
	require.Less(t, i, j, "%q must come before %q:\n%s", first, second, sql)
}

// A sequence a column default calls with nextval comes before the table,
// whatever the names; ALTER SEQUENCE ... OWNED BY comes after the table.
func TestSquash_SequenceBeforeTableWhoseDefaultCallsIt(t *testing.T) {
	t.Parallel()
	migrations := map[int]string{
		1: `CREATE TABLE zeta_orders (id bigint PRIMARY KEY);`,
		2: `CREATE SEQUENCE alpha_seq;
ALTER TABLE zeta_orders ADD COLUMN num bigint DEFAULT nextval('alpha_seq');
CREATE TABLE aaa_items (id bigint PRIMARY KEY DEFAULT nextval('public.alpha_seq'::regclass));`,
		3: `ALTER SEQUENCE alpha_seq OWNED BY zeta_orders.num;`,
	}
	for _, level := range []string{"conservative", "standard", "aggressive"} {
		t.Run(level, func(t *testing.T) {
			t.Parallel()
			sql := squashAt(t, level, migrations)
			requireBefore(t, sql, "CREATE SEQUENCE alpha_seq", "nextval('alpha_seq')")
			requireBefore(t, sql, "CREATE SEQUENCE alpha_seq", "CREATE TABLE aaa_items")
			requireBefore(t, sql, "CREATE TABLE zeta_orders", "ALTER SEQUENCE alpha_seq OWNED BY")
		})
	}
}

// The shape of CapyDB's own history (migration 019): a table the history
// drops is referenced by tables it keeps, two kept tables reference each
// other through foreign keys added with ALTER TABLE, and a DO block adds a
// constraint on a column added after the table was created.
func TestSquash_DroppedReferencedTableAndForeignKeyCycle(t *testing.T) {
	t.Parallel()
	migrations := map[int]string{
		1: `CREATE TABLE clusters (id text PRIMARY KEY, name text NOT NULL);
CREATE TABLE api_keys (id text PRIMARY KEY);
CREATE TABLE projects (
  id text PRIMARY KEY,
  cluster_id text NOT NULL REFERENCES clusters(id) ON DELETE RESTRICT,
  name text NOT NULL
);
CREATE TABLE jobs (id text PRIMARY KEY, cluster_id text NOT NULL REFERENCES clusters(id));`,
		2: `ALTER TABLE clusters ADD COLUMN IF NOT EXISTS max_databases integer NOT NULL DEFAULT 64;
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'clusters_max_databases_check') THEN
    ALTER TABLE clusters ADD CONSTRAINT clusters_max_databases_check CHECK (max_databases > 0);
  END IF;
END $$;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS max_connections integer NOT NULL DEFAULT 15;
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'projects_max_connections_check') THEN
    ALTER TABLE projects ADD CONSTRAINT projects_max_connections_check CHECK (max_connections > 0);
  END IF;
END $$;
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS project_id text REFERENCES projects(id) ON DELETE CASCADE;`,
		3: `CREATE TABLE instances (id text PRIMARY KEY, project_id text NOT NULL);
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_cluster_id_fkey;
ALTER TABLE projects DROP COLUMN IF EXISTS cluster_id;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS primary_instance_id text;
ALTER TABLE projects
  ADD CONSTRAINT projects_primary_instance_fkey
  FOREIGN KEY (primary_instance_id) REFERENCES instances(id) ON DELETE SET NULL;
ALTER TABLE instances
  ADD CONSTRAINT instances_project_fkey
  FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_cluster_id_fkey;
ALTER TABLE jobs DROP COLUMN IF EXISTS cluster_id;
DROP TABLE IF EXISTS clusters CASCADE;`,
	}
	for _, level := range []string{"conservative", "standard", "aggressive"} {
		t.Run(level, func(t *testing.T) {
			t.Parallel()
			sql := squashAt(t, level, migrations)

			// The tables that referenced the dropped table are kept, without
			// their foreign keys to it; the dropped table and what altered
			// it are gone.
			for _, table := range []string{"projects", "jobs", "api_keys", "instances"} {
				require.Regexp(t, `CREATE TABLE (IF NOT EXISTS )?`+table+` \(`, sql)
			}
			require.NotContains(t, sql, "REFERENCES clusters")
			require.NotRegexp(t, `CREATE TABLE (IF NOT EXISTS )?clusters`, sql)
			require.NotContains(t, sql, "clusters_max_databases_check")

			// A table comes after the table its foreign key references
			// (api_keys was created first, but references projects).
			requireBefore(t, sql, "CREATE TABLE projects", "REFERENCES projects")

			// The cycle between projects and instances is broken by adding
			// both foreign keys once both tables exist, under their names.
			requireBefore(t, sql, "CREATE TABLE instances", "ADD CONSTRAINT projects_primary_instance_fkey")
			requireBefore(t, sql, "CREATE TABLE projects", "ADD CONSTRAINT instances_project_fkey")

			// The DO block's constraint comes after the column it checks.
			requireBefore(t, sql, "max_connections int", "projects_max_connections_check")
		})
	}
}
