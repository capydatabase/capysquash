-- Threshold alerting: per-project storage/connection usage alerts evaluated by
-- the worker sweep against plan limits. One open (resolved_at IS NULL) alert
-- per (project, kind); severity escalates/downgrades in place and the row is
-- closed by setting resolved_at.
CREATE TABLE IF NOT EXISTS project_alerts (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  severity TEXT NOT NULL,
  observed_value BIGINT NOT NULL,
  limit_value BIGINT NOT NULL,
  triggered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  resolved_at TIMESTAMPTZ,
  last_notified_at TIMESTAMPTZ,
  acknowledged_at TIMESTAMPTZ,
  CONSTRAINT project_alerts_kind_check CHECK (kind IN ('storage', 'connections')),
  CONSTRAINT project_alerts_severity_check CHECK (severity IN ('warning', 'critical'))
);

CREATE INDEX IF NOT EXISTS project_alerts_project_idx
  ON project_alerts (project_id, triggered_at DESC);

-- At most one open alert per project and kind; the sweep updates it in place.
CREATE UNIQUE INDEX IF NOT EXISTS project_alerts_open_unique
  ON project_alerts (project_id, kind)
  WHERE resolved_at IS NULL;

-- Sweep scheduling gate, mirroring clusters.last_replication_sampled_at for
-- health probes: a project is due for alert evaluation when this is NULL or
-- older than the sweep interval.
ALTER TABLE projects
  ADD COLUMN IF NOT EXISTS alerts_evaluated_at TIMESTAMPTZ;
