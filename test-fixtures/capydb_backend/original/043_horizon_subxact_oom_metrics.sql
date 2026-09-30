-- Three new observability signals, all sampled by the existing alert sweep, and
-- the three advisory alert kinds they back.
--
-- 1. MVCC horizon (xmin_age, oldest_tx_seconds)
--    The existing "vacuum" advisory measures SUM(n_dead_tup) against a floor -
--    the SYMPTOM. The cause is an open transaction pinning the horizon: vacuum
--    cannot reclaim any tuple that transaction might still see, so the cell
--    degrades into a quiet equilibrium where lock times creep and nothing
--    crosses a dead-tuple floor. max(age(backend_xmin)) names the actual
--    culprit, and it is the one number that tells a customer WHICH session to
--    close. Backs the "long_transaction" advisory.
--
-- 2. Subtransaction cache overflow (subxact_overflowed, subtrans_blks_read)
--    Every backend caches PGPROC_MAX_CACHED_SUBXIDS (64) subtransaction ids;
--    past that it falls back to the pg_subtrans SLRU and EVERY concurrent
--    query in the cluster pays an SLRU lock - and possibly a disk read - per
--    scanned tuple. It is trivially reached by ordinary application code:
--    every PL/pgSQL EXCEPTION block and every nested ORM transaction is a
--    SAVEPOINT. The symptom is a throughput cliff with no visible cause that
--    disappears the moment the transaction commits, which is precisely the
--    kind of report we have had no way to answer. Backs "subtransactions".
--
-- 3. cgroup OOM kills (oom_kills)
--    memory.current is a workspace gauge - it reads high whenever an instance
--    is doing its job, so it is a poor alerting signal. memory.events oom_kill
--    is not: it is always a backend or the postmaster being killed. Cumulative
--    on the host, so the control plane alerts on the delta. Backs "oom_kill".
--
-- All three new kinds are ADVISORY tier (warning severity, dashboard/API only,
-- never email or webhook - see alertNotificationPolicies). For oom_kill that is
-- a deliberate call rather than an oversight: an instance killed hard and
-- staying down already opens the full-pipeline "unreachable" alert, so this
-- kind's job is to EXPLAIN that outage rather than to announce it a second
-- time.

-- 1/2. Tenant-sampled counters. Nullable and written all-or-nothing with the
--      rest of the counter group, so pre-043 rows and samples taken while the
--      probe failed both read back as "group absent".
ALTER TABLE project_metrics
  ADD COLUMN IF NOT EXISTS xmin_age BIGINT,
  ADD COLUMN IF NOT EXISTS oldest_tx_seconds BIGINT,
  ADD COLUMN IF NOT EXISTS subxact_overflowed BIGINT,
  ADD COLUMN IF NOT EXISTS subtrans_blks_read BIGINT;

-- 3. Host-sampled, so it joins the resource group (migration 026), not the
--    counter group. A host still running a pre-043 stats script simply omits
--    the field and it reads back as absent.
ALTER TABLE project_metrics
  ADD COLUMN IF NOT EXISTS oom_kills BIGINT;

ALTER TABLE project_alerts
  DROP CONSTRAINT IF EXISTS project_alerts_kind_check;
ALTER TABLE project_alerts
  ADD CONSTRAINT project_alerts_kind_check CHECK (kind IN (
    'storage', 'connections', 'backup', 'backup_stale',
    'cache_hit', 'blocked_queries', 'deadlocks', 'vacuum',
    'long_transaction', 'subtransactions', 'oom_kill',
    'pooler_handshake', 'unreachable'
  ));
