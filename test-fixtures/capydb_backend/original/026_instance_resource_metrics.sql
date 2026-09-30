-- Instance resource metrics (migration 026):
--
-- project_metrics grows the instance's cgroup CPU/memory reading, collected
-- by the worker's metrics sweep via one host stats probe per host per sweep
-- (capydb-host-stats.sh over the SSH executor). cpu_usage_usec is cumulative
-- (cpu.stat usage_usec - deltas between consecutive samples yield
-- utilization); the other three are gauges (cpu.max quota in cores,
-- memory.current, memory.max). NULLABLE as a group on purpose: samples
-- recorded before this migration - or while the probe was unavailable - carry
-- NULLs, keyed off cpu_usage_usec like the migration-018 counter group is
-- keyed off xact_commit.
ALTER TABLE project_metrics
  ADD COLUMN IF NOT EXISTS cpu_usage_usec BIGINT,
  ADD COLUMN IF NOT EXISTS cpu_quota_cores DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS mem_usage_bytes BIGINT,
  ADD COLUMN IF NOT EXISTS mem_limit_bytes BIGINT;
