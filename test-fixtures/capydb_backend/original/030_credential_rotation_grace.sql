-- Grace-period credential rotation: rotating with a grace window issues a new
-- login role and retires the old one instead of overwriting its password.
-- projects.role_name stays the stable OWNER role forever; only
-- project_credentials.username advances to the new login role. The retired
-- role keeps authenticating until expires_at - enforcement is Postgres's own
-- VALID UNTIL on the role, so expiry holds even if the control plane is down.
-- This table only schedules the cleanup DROP and surfaces the deadline.
CREATE TABLE IF NOT EXISTS project_credential_retirements (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  username TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  -- Sweep claim marker: set when the drop job is enqueued so ticks don't
  -- enqueue duplicates; a claim older than 24h re-arms (job exhausted retries).
  drop_enqueued_at TIMESTAMPTZ,
  dropped_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS project_credential_retirements_due_idx
  ON project_credential_retirements (expires_at)
  WHERE dropped_at IS NULL;
