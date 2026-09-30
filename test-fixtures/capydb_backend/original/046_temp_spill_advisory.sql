-- The `temp_spill` advisory alert kind.
--
-- A sort, hash join or materialised CTE that does not fit in work_mem is
-- written to a temporary file and read back. Postgres already counts this per
-- database (pg_stat_database.temp_bytes), and CapyDB has sampled it into
-- project_metrics.temp_bytes since migration 018 - but nothing has ever read
-- it, so the platform has been holding the evidence for one of the two
-- commonest causes of a slow statement on a cell and never saying so.
--
-- No new column: this kind is derived entirely from the temp_bytes counter that
-- already exists. What is new is that a customer finds out.
--
-- Advisory tier (warning severity, dashboard/API only, never email or webhook -
-- see alertNotificationPolicies), because the remedy is a query change and not
-- a platform action. work_mem stays at the Postgres default fleet-wide on
-- purpose: at business sizing, poolSize (40) x ~2 sort nodes x
-- (max_parallel_workers_per_gather 4 + 1) already commits the whole
-- non-shared_buffers memory budget, and memory.max is a hard cgroup kill, so
-- raising the global would trade a disk spill for an OOM. The per-statement fix
-- (`SET LOCAL work_mem`, or a plan change that stops the sort) is free and
-- safe. capydb_slow_queries().temp_blks_written - added with converger platform
-- v13 - names the statement to apply it to.
ALTER TABLE project_alerts
  DROP CONSTRAINT IF EXISTS project_alerts_kind_check;
ALTER TABLE project_alerts
  ADD CONSTRAINT project_alerts_kind_check CHECK (kind IN (
    'storage', 'connections', 'backup', 'backup_stale',
    'cache_hit', 'blocked_queries', 'deadlocks', 'vacuum',
    'long_transaction', 'subtransactions', 'oom_kill', 'temp_spill',
    'pooler_handshake', 'unreachable'
  ));
