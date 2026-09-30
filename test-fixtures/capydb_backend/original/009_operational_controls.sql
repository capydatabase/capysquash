ALTER TABLE clusters
  ALTER COLUMN backup_storage_mode SET DEFAULT 'walg';

UPDATE clusters
SET backup_storage_mode = 'walg'
WHERE backup_storage_mode IS NULL OR backup_storage_mode = '' OR backup_storage_mode = 'local';

ALTER TABLE projects
  ADD COLUMN IF NOT EXISTS max_connections INTEGER NOT NULL DEFAULT 15,
  ADD COLUMN IF NOT EXISTS storage_limit_bytes BIGINT NOT NULL DEFAULT 1073741824,
  ADD COLUMN IF NOT EXISTS statement_timeout TEXT NOT NULL DEFAULT '30s',
  ADD COLUMN IF NOT EXISTS idle_transaction_timeout TEXT NOT NULL DEFAULT '60s';

UPDATE projects
SET max_connections = CASE plan
    WHEN 'business' THEN 60
    WHEN 'ship' THEN 30
    ELSE 15
  END,
  storage_limit_bytes = CASE plan
    WHEN 'business' THEN 10737418240
    WHEN 'ship' THEN 5368709120
    ELSE 1073741824
  END,
  statement_timeout = CASE plan
    WHEN 'business' THEN '120s'
    WHEN 'ship' THEN '60s'
    ELSE '30s'
  END,
  idle_transaction_timeout = CASE plan
    WHEN 'business' THEN '120s'
    WHEN 'ship' THEN '90s'
    ELSE '60s'
  END;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'projects_max_connections_check'
  ) THEN
    ALTER TABLE projects
      ADD CONSTRAINT projects_max_connections_check CHECK (max_connections > 0);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'projects_storage_limit_check'
  ) THEN
    ALTER TABLE projects
      ADD CONSTRAINT projects_storage_limit_check CHECK (storage_limit_bytes > 0);
  END IF;
END $$;

ALTER TABLE backups
  ADD COLUMN IF NOT EXISTS size_bytes BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS verification_state TEXT NOT NULL DEFAULT 'pending',
  ADD COLUMN IF NOT EXISTS verified_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS verification_error TEXT;

UPDATE backups
SET verification_state = CASE state
    WHEN 'completed' THEN 'legacy_unverified'
    ELSE COALESCE(NULLIF(verification_state, ''), 'pending')
  END
WHERE verification_state IS NULL OR verification_state = 'pending';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'backups_verification_state_check'
  ) THEN
    ALTER TABLE backups
      ADD CONSTRAINT backups_verification_state_check
      CHECK (verification_state IN ('pending', 'verified', 'failed', 'legacy_unverified'));
  END IF;
END $$;

ALTER TABLE jobs
  ADD COLUMN IF NOT EXISTS retry_classification TEXT,
  ADD COLUMN IF NOT EXISTS locked_resource TEXT,
  ADD COLUMN IF NOT EXISTS last_exit_code INTEGER NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS last_stdout TEXT,
  ADD COLUMN IF NOT EXISTS last_stderr TEXT;

CREATE INDEX IF NOT EXISTS jobs_locked_resource_idx
  ON jobs (locked_resource, state)
  WHERE locked_resource IS NOT NULL AND state IN ('pending', 'running');

CREATE TABLE IF NOT EXISTS project_audit_events (
  id TEXT PRIMARY KEY,
  organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id TEXT REFERENCES projects(id) ON DELETE SET NULL,
  actor_kind TEXT NOT NULL,
  actor_id TEXT,
  action TEXT NOT NULL,
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS project_audit_events_project_idx
  ON project_audit_events (project_id, created_at DESC);

CREATE TABLE IF NOT EXISTS scheduled_backups (
  id TEXT PRIMARY KEY,
  organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  label TEXT NOT NULL DEFAULT 'scheduled',
  cron_hour INTEGER NOT NULL DEFAULT 2,
  cron_minute INTEGER NOT NULL DEFAULT 15,
  retention_days INTEGER NOT NULL DEFAULT 14,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  last_job_id TEXT,
  last_run_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS scheduled_backups_project_idx
  ON scheduled_backups (project_id);

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'scheduled_backups_cron_hour_check'
  ) THEN
    ALTER TABLE scheduled_backups
      ADD CONSTRAINT scheduled_backups_cron_hour_check CHECK (cron_hour BETWEEN 0 AND 23);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'scheduled_backups_cron_minute_check'
  ) THEN
    ALTER TABLE scheduled_backups
      ADD CONSTRAINT scheduled_backups_cron_minute_check CHECK (cron_minute BETWEEN 0 AND 59);
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'scheduled_backups_retention_days_check'
  ) THEN
    ALTER TABLE scheduled_backups
      ADD CONSTRAINT scheduled_backups_retention_days_check CHECK (retention_days BETWEEN 1 AND 365);
  END IF;
END $$;

CREATE TABLE IF NOT EXISTS project_integrations (
  id TEXT PRIMARY KEY,
  organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  provider TEXT NOT NULL,
  external_id TEXT,
  config JSONB NOT NULL DEFAULT '{}'::jsonb,
  state TEXT NOT NULL DEFAULT 'active',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT project_integrations_provider_check CHECK (provider IN ('clerk', 'drizzle', 'github', 'netlify', 'supabase_auth', 'vercel')),
  CONSTRAINT project_integrations_state_check CHECK (state IN ('active', 'disabled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS project_integrations_project_provider_idx
  ON project_integrations (project_id, provider);
