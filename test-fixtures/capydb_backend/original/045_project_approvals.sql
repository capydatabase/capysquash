-- Single-use approvals for destructive project actions (production
-- project.delete, project.restore_overwrite). A separate mint call
-- (POST /v1/projects/{projectID}/approvals) returns the raw ap_ token exactly
-- once; only its SHA-256 is stored here. The destructive endpoint consumes the
-- row (consumed_at) and binds the job it enqueued (job_id), so presenting the
-- same token again idempotently returns that job instead of re-running the
-- action. Rows expire ~10 minutes after mint and expired rows are inert; no
-- reaper is needed because the table only ever holds a handful of short-lived
-- rows per project.
CREATE TABLE IF NOT EXISTS project_approvals (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    action TEXT NOT NULL,
    actor_id TEXT NOT NULL,
    token_sha256 TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    job_id TEXT
);

CREATE INDEX IF NOT EXISTS idx_project_approvals_project ON project_approvals (project_id);
