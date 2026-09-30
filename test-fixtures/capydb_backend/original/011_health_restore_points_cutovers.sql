ALTER TABLE jobs
  ALTER COLUMN organization_id DROP NOT NULL;

ALTER TABLE clusters
  ADD COLUMN IF NOT EXISTS last_replication_lag_seconds DOUBLE PRECISION,
  ADD COLUMN IF NOT EXISTS last_replication_in_recovery BOOLEAN,
  ADD COLUMN IF NOT EXISTS last_replication_sampled_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS last_replication_error TEXT,
  ADD COLUMN IF NOT EXISTS health_alert_state TEXT NOT NULL DEFAULT 'ok';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'clusters_health_alert_state_check'
  ) THEN
    ALTER TABLE clusters
      ADD CONSTRAINT clusters_health_alert_state_check
      CHECK (health_alert_state IN ('ok', 'warning', 'critical'));
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS cluster_health_samples (
  id TEXT PRIMARY KEY,
  cluster_id TEXT NOT NULL REFERENCES clusters(id) ON DELETE CASCADE,
  role TEXT NOT NULL,
  in_recovery BOOLEAN,
  received_lsn TEXT,
  replayed_lsn TEXT,
  replay_lag_seconds DOUBLE PRECISION,
  probe_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS cluster_health_samples_cluster_idx
  ON cluster_health_samples (cluster_id, created_at DESC);

CREATE TABLE IF NOT EXISTS restore_points (
  id TEXT PRIMARY KEY,
  organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  label TEXT NOT NULL,
  kind TEXT NOT NULL,
  backup_id TEXT REFERENCES backups(id) ON DELETE SET NULL,
  pitr_time TIMESTAMPTZ,
  note TEXT,
  created_by_actor_kind TEXT NOT NULL,
  created_by_actor_id TEXT,
  state TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'restore_points_kind_check'
  ) THEN
    ALTER TABLE restore_points
      ADD CONSTRAINT restore_points_kind_check CHECK (kind IN ('backup', 'pitr'));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'restore_points_state_check'
  ) THEN
    ALTER TABLE restore_points
      ADD CONSTRAINT restore_points_state_check CHECK (state IN ('active', 'stale'));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'restore_points_anchor_check'
  ) THEN
    ALTER TABLE restore_points
      ADD CONSTRAINT restore_points_anchor_check CHECK (
        (kind = 'backup' AND backup_id IS NOT NULL AND pitr_time IS NULL)
        OR
        (kind = 'pitr' AND pitr_time IS NOT NULL AND backup_id IS NULL)
      );
  END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS restore_points_project_label_idx
  ON restore_points (project_id, label);

CREATE INDEX IF NOT EXISTS restore_points_project_idx
  ON restore_points (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS cutover_requests (
  id TEXT PRIMARY KEY,
  organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  preview_database_id TEXT NOT NULL REFERENCES preview_databases(id) ON DELETE CASCADE,
  requested_by_actor_kind TEXT NOT NULL,
  requested_by_actor_id TEXT,
  customer_note TEXT,
  external_ticket_ref TEXT,
  status TEXT NOT NULL DEFAULT 'open',
  operator_note TEXT,
  ack_by_user_id TEXT,
  ack_at TIMESTAMPTZ,
  promote_job_id TEXT,
  pre_cutover_backup_id TEXT REFERENCES backups(id) ON DELETE SET NULL,
  completed_at TIMESTAMPTZ,
  rejected_reason TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'cutover_requests_status_check'
  ) THEN
    ALTER TABLE cutover_requests
      ADD CONSTRAINT cutover_requests_status_check
      CHECK (status IN ('open', 'acked', 'in_progress', 'completed', 'rejected'));
  END IF;
END $$;

CREATE INDEX IF NOT EXISTS cutover_requests_project_idx
  ON cutover_requests (project_id, created_at DESC);

CREATE INDEX IF NOT EXISTS cutover_requests_status_idx
  ON cutover_requests (status, created_at DESC);

CREATE UNIQUE INDEX IF NOT EXISTS cutover_requests_active_preview_idx
  ON cutover_requests (preview_database_id)
  WHERE status IN ('open', 'acked', 'in_progress');
