-- Multi-major hosts: a host advertises every Postgres major it can run
-- (roles/instances installs the full pg_versions set side by side), and
-- placement filters on the requested major. Replaces the single host-wide
-- postgres_version column outright - the per-INSTANCE postgres_version column
-- (instances.postgres_version) is unchanged and remains the source of truth
-- for what each cell runs.
--
-- The backfill sets every host to the full supported set because roles/
-- instances applies group_vars pg_versions fleet-wide: any host this control
-- plane manages gets all three majors installed on the same apply that ships
-- this migration. KEEP IN LOCKSTEP with infrastructure group_vars pg_versions
-- and service.SupportedPostgresVersions.
ALTER TABLE hosts ADD COLUMN supported_postgres_versions TEXT[] NOT NULL DEFAULT ARRAY['17'];
UPDATE hosts SET supported_postgres_versions = ARRAY['16', '17', '18'];
ALTER TABLE hosts DROP COLUMN postgres_version;
