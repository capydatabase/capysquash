-- Time-series observability history: one row per project per alert-sweep
-- evaluation (CAPYDB_ALERT_SWEEP_INTERVAL cadence, default 5m), persisted by
-- the worker alongside the threshold-alert evaluation. Retention is enforced
-- by the same sweep (samples older than 30 days are pruned for the projects
-- being swept), so the table never needs a dedicated scheduler.
CREATE TABLE IF NOT EXISTS project_metrics (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  sampled_at TIMESTAMPTZ NOT NULL,
  storage_bytes BIGINT NOT NULL,
  storage_limit_bytes BIGINT NOT NULL,
  connections INT NOT NULL,
  connection_limit INT NOT NULL,
  active_queries INT NOT NULL,
  slow_queries INT NOT NULL
);

-- Serves both the history endpoint's range scan (project + time window) and
-- the projects(id) foreign key.
CREATE INDEX IF NOT EXISTS project_metrics_project_sampled_idx
  ON project_metrics (project_id, sampled_at);
