-- Expanded observability collection (migration 018):
--
-- 1. project_metrics grows cumulative pg_stat_database counters plus two
--    gauges and two query-stat aggregates, all read by the worker's alert
--    sweep on the same tenant connection it already opens. The new columns
--    are NULLABLE on purpose: samples recorded before this migration have no
--    counters, and the history endpoint must report null (not a fabricated
--    zero) for delta-derived fields whose base sample predates collection.
--    The sweep always writes the whole group together, so NULLness is
--    all-or-nothing per row (keyed off xact_commit).
ALTER TABLE project_metrics
  ADD COLUMN IF NOT EXISTS xact_commit BIGINT,
  ADD COLUMN IF NOT EXISTS xact_rollback BIGINT,
  ADD COLUMN IF NOT EXISTS tup_returned BIGINT,
  ADD COLUMN IF NOT EXISTS tup_inserted BIGINT,
  ADD COLUMN IF NOT EXISTS tup_updated BIGINT,
  ADD COLUMN IF NOT EXISTS tup_deleted BIGINT,
  ADD COLUMN IF NOT EXISTS blks_read BIGINT,
  ADD COLUMN IF NOT EXISTS blks_hit BIGINT,
  ADD COLUMN IF NOT EXISTS temp_bytes BIGINT,
  ADD COLUMN IF NOT EXISTS deadlocks BIGINT,
  ADD COLUMN IF NOT EXISTS blocked_queries BIGINT,
  ADD COLUMN IF NOT EXISTS dead_tuples BIGINT,
  ADD COLUMN IF NOT EXISTS query_calls BIGINT,
  ADD COLUMN IF NOT EXISTS query_total_exec_ms DOUBLE PRECISION;

-- 2. Per-query latency snapshots: each sweep records the top queries by
--    total_exec_time as reported by the capydb_slow_queries() wrapper (which
--    returns at most the database's top 20 statements). The queries endpoint
--    computes window deltas per query_hash. Retention (7 days) piggybacks on
--    the alert sweep like project_metrics retention does.
CREATE TABLE IF NOT EXISTS project_query_stats (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  query_hash TEXT NOT NULL,
  query_text TEXT NOT NULL,
  captured_at TIMESTAMPTZ NOT NULL,
  calls BIGINT NOT NULL,
  total_exec_ms DOUBLE PRECISION NOT NULL,
  rows BIGINT NOT NULL
);

-- Serves the window scan (project + time range) and the projects(id) FK.
CREATE INDEX IF NOT EXISTS project_query_stats_project_captured_idx
  ON project_query_stats (project_id, captured_at);

-- 3. New project_alerts kinds: "backup" (critical, full notification) plus the
--    advisory kinds "cache_hit", "blocked_queries", "deadlocks", "vacuum"
--    (warning severity, dashboard/API surface only - never email or webhook).
ALTER TABLE project_alerts
  DROP CONSTRAINT IF EXISTS project_alerts_kind_check;
ALTER TABLE project_alerts
  ADD CONSTRAINT project_alerts_kind_check CHECK (kind IN (
    'storage', 'connections', 'backup',
    'cache_hit', 'blocked_queries', 'deadlocks', 'vacuum'
  ));

-- 4. Preview expiring-soon notification stamp: the preview sweep emits one
--    "preview.expiring_soon" webhook event when a ready preview enters the
--    final two hours of its TTL. The stamp makes emission once-only per
--    expiry window (extending the TTL re-arms it because the stamp falls out
--    of the new window).
ALTER TABLE preview_databases
  ADD COLUMN IF NOT EXISTS expiring_notified_at TIMESTAMPTZ;
