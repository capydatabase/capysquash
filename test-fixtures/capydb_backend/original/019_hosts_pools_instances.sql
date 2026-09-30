-- 019_hosts_pools_instances.sql
-- Instance-per-project data plane: hard-replace the shared-cluster model
-- (one instance, N databases) with one cgroup-isolated, ZFS-backed Postgres
-- instance per project. See docs/capydb-tenant-isolation-architecture-spec.md §8.2.
--
-- GREENFIELD drop-and-replace (spec §9 "Closed: tenancy-model migration - greenfield,
-- no users -> zero migration cost"): there is no live cluster/project data to preserve
-- (dev/staging run CAPYDB_EXECUTOR_MODE=dry-run). Branch/PITR/premium columns are
-- present-but-nullable so the M2 work needs no re-migration.

-- ---------------------------------------------------------------------------
-- 1. Physical/storage topology (replaces `clusters`).
--    host = a physical box; pool = a ZFS pool on a host.
-- ---------------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS hosts (
  id               TEXT PRIMARY KEY,
  hostname         TEXT NOT NULL,
  region           TEXT NOT NULL,
  vcpu             INTEGER NOT NULL,
  ram_mb           INTEGER NOT NULL,
  arc_max_mb       INTEGER NOT NULL,                 -- reserved ARC, excluded from instance RAM budget (F5/F21)
  swap             BOOLEAN NOT NULL DEFAULT FALSE,   -- swapless by default (F14)
  -- SSH targeting (was clusters.management_*). The InstanceDriver SSHes here as
  -- root: instance create/destroy run useradd/groupadd/zfs/systemctl.
  management_mode  TEXT NOT NULL DEFAULT 'ssh',
  management_host  TEXT NOT NULL,
  management_port  INTEGER NOT NULL DEFAULT 22,
  management_user  TEXT NOT NULL DEFAULT 'root',
  postgres_version TEXT NOT NULL DEFAULT '17',       -- host-wide PG major (M1 = single major)
  state            TEXT NOT NULL DEFAULT 'active',    -- active|degraded|error
  created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS pools (
  id              TEXT PRIMARY KEY,
  host_id         TEXT NOT NULL REFERENCES hosts(id) ON DELETE RESTRICT,
  zpool_name      TEXT NOT NULL,
  tier            TEXT NOT NULL DEFAULT 'standard',  -- 'standard' (shared, best-effort IO) | 'io_isolated' (own pool - F22, M2)
  capacity_bytes  BIGINT NOT NULL,
  admission_pct   INTEGER NOT NULL DEFAULT 80,       -- stop placing instances past this (F13/§9.2)
  state           TEXT NOT NULL DEFAULT 'active',    -- active|degraded
  created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (host_id, zpool_name)
);

CREATE INDEX IF NOT EXISTS pools_host_idx ON pools (host_id);

CREATE TABLE IF NOT EXISTS instances (
  id                  TEXT PRIMARY KEY,
  project_id          TEXT NOT NULL,                  -- FK added after projects re-points (below)
  host_id             TEXT NOT NULL REFERENCES hosts(id) ON DELETE RESTRICT,
  pool_id             TEXT NOT NULL REFERENCES pools(id) ON DELETE RESTRICT,
  parent_instance_id  TEXT REFERENCES instances(id),  -- set for branches (ZFS clone; M2 designed-for, nullable now)
  origin_snapshot     TEXT,                           -- zfs snapshot a branch hangs off (M2)
  dataset             TEXT NOT NULL,                  -- tank/instances/<id>
  postgres_version    TEXT NOT NULL,                  -- channel-resolved major (M1 = the host's single major)
  uid                 INTEGER NOT NULL,               -- per-host unix uid (DB-authoritative: passed to useradd --uid)
  tier                TEXT NOT NULL DEFAULT 'standard',      -- 'standard' | 'premium' (M2)
  network_posture     TEXT NOT NULL DEFAULT 'private_netns', -- 'private_netns' | 'veth_bridge' (M2)
  socket_dir          TEXT,                           -- /run/capydb/<id> (standard tier, socket-only)
  port                INTEGER,                        -- TCP port (premium tier, M2)
  cpu_quota_pct       INTEGER NOT NULL,
  cpu_weight          INTEGER NOT NULL DEFAULT 100,
  mem_high_mb         INTEGER NOT NULL,
  mem_max_mb          INTEGER NOT NULL,
  storage_limit_bytes BIGINT,                         -- prod advertised limit; NULL for branches (F25)
  -- customer connection metadata, carried on the instance so connection-string
  -- builds don't need a second lookup:
  database_name       TEXT NOT NULL,
  role_name           TEXT NOT NULL,
  state               TEXT NOT NULL DEFAULT 'provisioning',
                      -- provisioning|running|asleep|waking|error|destroying
                      -- (asleep/waking unused in M1: always-on, no scale-to-zero)
  last_active_at      TIMESTAMPTZ,
  last_error          TEXT,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE (host_id, uid),
  UNIQUE (host_id, port),    -- Postgres treats NULLs as distinct, so socket-only NULL ports don't collide
  UNIQUE (dataset),          -- catch registry drift (F8)
  CHECK ( (tier = 'standard' AND network_posture = 'private_netns')
       OR (tier = 'premium'  AND network_posture = 'veth_bridge') )
);

CREATE INDEX IF NOT EXISTS instances_project_idx    ON instances (project_id);
CREATE INDEX IF NOT EXISTS instances_host_idx       ON instances (host_id);
CREATE INDEX IF NOT EXISTS instances_pool_idx       ON instances (pool_id);
CREATE INDEX IF NOT EXISTS instances_parent_idx     ON instances (parent_instance_id);
CREATE INDEX IF NOT EXISTS instances_host_state_idx ON instances (host_id, state);
-- exactly one prod (non-branch) instance per project; makes GetInstanceByProjectID well-defined.
CREATE UNIQUE INDEX IF NOT EXISTS instances_one_prod_per_project
  ON instances (project_id) WHERE parent_instance_id IS NULL;

-- ---------------------------------------------------------------------------
-- 2. Re-point existing tables off `clusters` and onto hosts/instances.
--    projects.cluster_id -> projects.primary_instance_id
--    jobs.cluster_id     -> jobs.host_id (+ jobs.instance_id)
--    preview_databases   -> + instance_id (branch instance; M2)
--    cluster_health_samples.cluster_id -> host_id
-- ---------------------------------------------------------------------------

-- projects: drop cluster FK, add primary-instance back-reference.
-- ON DELETE SET NULL so destroying an instance does NOT cascade-delete the project row.
ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_cluster_id_fkey;
ALTER TABLE projects DROP COLUMN IF EXISTS cluster_id;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS primary_instance_id TEXT;
ALTER TABLE projects
  ADD CONSTRAINT projects_primary_instance_fkey
  FOREIGN KEY (primary_instance_id) REFERENCES instances(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS projects_primary_instance_idx ON projects (primary_instance_id);

-- instances.project_id FK (added now that projects has no cluster dependency).
-- Insertion order within CreateProject's tx is load-bearing (non-deferrable FKs):
--   INSERT project (primary_instance_id NULL) -> INSERT instance -> UPDATE project.primary_instance_id.
ALTER TABLE instances
  ADD CONSTRAINT instances_project_fkey
  FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE;

-- jobs: drop cluster routing, add host routing. host_id/instance_id nullable -
-- org-level/integration jobs (clerk_backfill, sync_env) target no host.
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_cluster_id_fkey;
ALTER TABLE jobs DROP COLUMN IF EXISTS cluster_id;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS host_id TEXT REFERENCES hosts(id) ON DELETE SET NULL;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS instance_id TEXT REFERENCES instances(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS jobs_host_idx     ON jobs (host_id, created_at DESC);
CREATE INDEX IF NOT EXISTS jobs_instance_idx ON jobs (instance_id);

-- preview_databases: branch-instance linkage (nullable; M2 wires branches).
ALTER TABLE preview_databases ADD COLUMN IF NOT EXISTS instance_id TEXT REFERENCES instances(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS preview_databases_instance_idx ON preview_databases (instance_id);

-- cluster_health_samples (migration 011): re-point cluster_id -> host_id.
-- The probe now targets a host/instance, not a shared cluster. Greenfield: no rows
-- to backfill. NOTE: the table's timestamp column is created_at (NOT sampled_at).
ALTER TABLE cluster_health_samples DROP CONSTRAINT IF EXISTS cluster_health_samples_cluster_id_fkey;
DROP INDEX IF EXISTS cluster_health_samples_cluster_idx;
ALTER TABLE cluster_health_samples DROP COLUMN IF EXISTS cluster_id;
ALTER TABLE cluster_health_samples ADD COLUMN IF NOT EXISTS host_id TEXT REFERENCES hosts(id) ON DELETE CASCADE;
CREATE INDEX IF NOT EXISTS cluster_health_samples_host_idx ON cluster_health_samples (host_id, created_at DESC);

-- ---------------------------------------------------------------------------
-- 3. Drop the obsolete shared-cluster table + its count-based capacity budget.
--    clusters + clusters.max_databases (migration 005) are replaced by
--    hosts + pools + pools.admission_pct (byte/percent-based admission, F13).
-- ---------------------------------------------------------------------------
DROP TABLE IF EXISTS clusters CASCADE;
