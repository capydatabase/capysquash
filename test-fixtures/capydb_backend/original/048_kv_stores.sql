-- 048: managed Valkey KV stores (the rate-limit / cache surface that lives next
-- to a cell).
--
-- Deliberately a SEPARATE table from `instances` rather than an `engine` column
-- on it, for two reasons:
--
--   1. `instances` is Postgres-shaped. postgres_version, role_name, database_name,
--      pool_id, storage_limit_bytes, parent_instance_id/origin_snapshot (branch
--      lineage) and the whole backup/PITR/branch/import lineage mean nothing for
--      a KV store. Widening it would make half the columns conditionally NULL and
--      every PG-shaped query defensive.
--
--   2. The reconciler diffs the host's ACTUAL instance footprint (ListInstances:
--      zfs datasets under tank/instances + capydb-pg@ units + sockets) against
--      the `instances` rows and destroys what it cannot account for. A Valkey
--      bundle sharing that namespace would either be deleted as an orphan or
--      force the probe to grow engine-awareness. Instead KV lives under a
--      different ZFS parent (tank/capydb-kv/<id>, NOT tank/instances/) and a
--      different unit template (capydb-valkey@ vs capydb-pg@), so the existing
--      reconciler stays correctly blind to it. A KV reconciler is a later,
--      separate sweep.
--
-- Scope of v1, stated so the constraints below read as intent and not oversight:
-- one KV store per project (UNIQUE project_id) - "next to your cell" is the whole
-- positioning, and the per-project env-var push has one slot. Relaxing 0..1 to
-- 0..N later is a dropped constraint; tightening the other way would not be. No
-- preview/branch KV: preview databases are ZFS clones of a Postgres instance and
-- have no KV analogue in v1.
CREATE TABLE IF NOT EXISTS kv_stores (
  id                TEXT PRIMARY KEY,
  project_id        TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  organization_id   TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  host_id           TEXT NOT NULL REFERENCES hosts(id) ON DELETE RESTRICT,
  dataset           TEXT NOT NULL,                -- tank/capydb-kv/<id>
  socket_dir        TEXT NOT NULL,                -- /run/capydb-kv/<id>
  uid               INTEGER NOT NULL,             -- per-host unix uid (DB-authoritative, like instances.uid)

  -- Memory is the ONLY resource that matters here, and the two numbers below are
  -- a KEEP IN LOCKSTEP pair. mem_max_mb becomes the cgroup MemoryMax (a hard
  -- kill); maxmemory_mb becomes Valkey's own maxmemory (a soft policy limit).
  -- BGSAVE forks, and copy-on-write during the save can approach 2x RSS, so
  -- maxmemory_mb must stay at or below half of mem_max_mb whenever persistence
  -- is on - otherwise a routine snapshot is an OOM kill. The CHECK enforces it
  -- in the one place both values are visible; service.kvSizing computes them.
  mem_max_mb        INTEGER NOT NULL,
  maxmemory_mb      INTEGER NOT NULL,
  maxmemory_policy  TEXT NOT NULL DEFAULT 'volatile-lru',
  persistence       TEXT NOT NULL DEFAULT 'rdb',  -- 'rdb' | 'none'

  -- Auth boundary. There is NO Valkey-side password: the store listens on a unix
  -- socket inside a loopback-only netns, so the namespace IS the isolation wall
  -- and there is no second tenant on that socket to defend against. The gateway
  -- verifies this hash and then relays. Same hashing helper as organization API
  -- keys (capy_live_); the prefix is capy_kv_ and is stored plaintext only as a
  -- display fragment, never the secret.
  token_hash        TEXT NOT NULL,
  token_prefix      TEXT NOT NULL DEFAULT '',

  public_host       TEXT NOT NULL DEFAULT '',     -- <slug>-<suffix>.db.capydb.dev (shares the existing wildcard)
  state             TEXT NOT NULL DEFAULT 'provisioning',
                    -- provisioning|running|error|destroying
                    -- No asleep/waking: KV is deliberately NOT scale-to-zero. A
                    -- rate limiter cannot pay a wake on the first request after a
                    -- quiet minute, and an idle valkey is ~5-10MB - cheaper than
                    -- the wake path's complexity, and it keeps KV out of the
                    -- reconciler/agent/instance-active livelock surface entirely.
  last_error        TEXT NOT NULL DEFAULT '',
  created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),

  UNIQUE (project_id),
  UNIQUE (host_id, uid),
  UNIQUE (dataset),
  UNIQUE (public_host),
  CHECK (persistence IN ('rdb', 'none')),
  CHECK (state IN ('provisioning', 'running', 'error', 'destroying')),
  CHECK (mem_max_mb > 0 AND maxmemory_mb > 0),
  CHECK (persistence <> 'rdb' OR maxmemory_mb * 2 <= mem_max_mb)
);

CREATE INDEX IF NOT EXISTS kv_stores_org_idx   ON kv_stores (organization_id);
CREATE INDEX IF NOT EXISTS kv_stores_host_idx  ON kv_stores (host_id);
CREATE INDEX IF NOT EXISTS kv_stores_state_idx ON kv_stores (state);

-- Admission for KV is on RAM, not bytes on disk.
--
-- Pool admission (capacity_bytes * admission_pct) is a ZFS-byte budget, which is
-- the right dimension for Postgres instances and the wrong one for Valkey: a KV
-- store's disk footprint is a rounding error and its memory footprint is the
-- entire cost. Worse, the host's free RAM is not actually free - ZFS ARC grows
-- into it up to arc_max, so an unbudgeted KV fleet silently steals Postgres page
-- cache from every cell on the box and shows up as a diffuse read-latency
-- regression nobody can attribute.
--
-- So KV placement is gated by its own explicit per-host budget, checked as
-- sum(mem_max_mb of the host's kv_stores) + requested <= kv_mem_budget_mb before
-- the create job is enqueued. 0 (the default) means "this host admits no KV",
-- which is the correct initial state for every host until an operator has looked
-- at free RAM and arc_max together and decided a number.
ALTER TABLE hosts ADD COLUMN IF NOT EXISTS kv_mem_budget_mb INTEGER NOT NULL DEFAULT 0;
