-- The retention prune (store.PruneMetrics) and the metadata archiver
-- (store/archive.go) select old rows by their timestamp alone:
--   project_metrics      WHERE sampled_at  < $1
--   project_query_stats  WHERE captured_at < $1
--   instance_wake_events WHERE woke_at     < $1
-- Every existing index on these tables leads with project_id or instance_id,
-- so each sweep scanned the whole table (500k and 300k rows in prod on
-- 2026-09-23). Migration 021 gave the other retention streams exactly this
-- index; these three were added later and missed it.
CREATE INDEX IF NOT EXISTS project_metrics_sampled_idx ON project_metrics (sampled_at);
CREATE INDEX IF NOT EXISTS project_query_stats_captured_idx ON project_query_stats (captured_at);
CREATE INDEX IF NOT EXISTS instance_wake_events_woke_idx ON instance_wake_events (woke_at);

-- 021 created cluster_health_samples_host_created_idx with the same definition
-- as 019's cluster_health_samples_host_idx, (host_id, created_at DESC): one of
-- the pair only doubles the write cost of every health sample.
DROP INDEX IF EXISTS cluster_health_samples_host_created_idx;
