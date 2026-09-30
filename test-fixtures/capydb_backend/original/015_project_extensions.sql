-- Per-database extension management. project_extensions records which
-- allowlisted Postgres extensions are enabled in each project database; the
-- worker keeps it in sync with the host (enable/disable jobs, provision
-- seeding, post-import reconciliation). The UNIQUE constraint doubles as the
-- project_id-prefixed index for FK lookups.
CREATE TABLE IF NOT EXISTS project_extensions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT project_extensions_project_name_unique UNIQUE (project_id, name)
);

-- Expanded host extension allowlist: every CapyDB host now also installs
-- postgis (postgresql-17-postgis-3), plus the contrib extensions unaccent,
-- ltree, fuzzystrmatch, btree_gin, and btree_gist. KEEP IN LOCKSTEP with
-- backend/internal/service/extensions.go (defaultClusterExtensions) and
-- infrastructure/ansible/group_vars/db_servers.yml
-- (capydb_cluster_extension_allowlist).
ALTER TABLE clusters
  ALTER COLUMN extensions SET DEFAULT '["pg_stat_statements", "pgcrypto", "uuid-ossp", "citext", "hstore", "pg_trgm", "vector", "postgis", "unaccent", "ltree", "fuzzystrmatch", "btree_gin", "btree_gist"]'::jsonb;

-- Clusters still carrying the previous default allowlist verbatim are moved to
-- the new one (their hosts get the packages via the updated Ansible postgres
-- role). Clusters with a customized allowlist are left untouched.
UPDATE clusters
SET extensions = '["pg_stat_statements", "pgcrypto", "uuid-ossp", "citext", "hstore", "pg_trgm", "vector", "postgis", "unaccent", "ltree", "fuzzystrmatch", "btree_gin", "btree_gist"]'::jsonb
WHERE extensions = '["pg_stat_statements", "pgcrypto", "uuid-ossp", "citext", "hstore", "pg_trgm", "vector"]'::jsonb;
