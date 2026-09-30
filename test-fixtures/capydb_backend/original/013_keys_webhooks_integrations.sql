-- Project-scoped API keys: a key with project_id set can only act on that
-- project (enforced in the service layer). NULL keeps today's org-wide
-- behavior. ON DELETE CASCADE so deleting a project kills its CI keys.
ALTER TABLE api_keys
  ADD COLUMN IF NOT EXISTS project_id TEXT REFERENCES projects(id) ON DELETE CASCADE;

CREATE INDEX IF NOT EXISTS api_keys_project_idx ON api_keys (project_id) WHERE project_id IS NOT NULL;

-- Cluster extension registry: which Postgres extensions a host actually has
-- installed (kept in lockstep with the Ansible postgres role) plus the major
-- version. Import pre-flight compares a source database's extensions against
-- this allowlist before any destructive step runs.
ALTER TABLE clusters
  ADD COLUMN IF NOT EXISTS postgres_version TEXT NOT NULL DEFAULT '17',
  ADD COLUMN IF NOT EXISTS extensions JSONB NOT NULL DEFAULT '["pg_stat_statements", "pgcrypto", "uuid-ossp", "citext", "hstore", "pg_trgm", "vector"]'::jsonb;

-- Encrypted per-integration credentials (provider API tokens, Clerk secret
-- keys, webhook signing secrets). Kept out of `config` so config can be
-- returned to clients verbatim while secrets never leave the control plane.
ALTER TABLE project_integrations
  ADD COLUMN IF NOT EXISTS credentials_encrypted TEXT;

-- Outbound webhooks: org-level endpoints with an HMAC signing secret and an
-- event-type filter (empty array = all events). Deliveries are durable rows
-- drained by the worker with retries, mirroring the job queue's model.
CREATE TABLE IF NOT EXISTS webhook_endpoints (
  id TEXT PRIMARY KEY,
  organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  url TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  secret_encrypted TEXT NOT NULL,
  event_types JSONB NOT NULL DEFAULT '[]'::jsonb,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS webhook_endpoints_org_idx ON webhook_endpoints (organization_id);

CREATE TABLE IF NOT EXISTS webhook_deliveries (
  id TEXT PRIMARY KEY,
  endpoint_id TEXT NOT NULL REFERENCES webhook_endpoints(id) ON DELETE CASCADE,
  organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending',
  attempts INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 8,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  response_status INTEGER,
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  delivered_at TIMESTAMPTZ,
  CONSTRAINT webhook_deliveries_state_check CHECK (state IN ('pending', 'delivered', 'failed'))
);

CREATE INDEX IF NOT EXISTS webhook_deliveries_pending_idx
  ON webhook_deliveries (next_attempt_at ASC)
  WHERE state = 'pending';
CREATE INDEX IF NOT EXISTS webhook_deliveries_endpoint_idx
  ON webhook_deliveries (endpoint_id, created_at DESC);

-- Studio query history: per-project record of statements run through the SQL
-- runner so the dashboard can offer history/replay across devices. Query text
-- is tenant data the org already owns; rows are pruned beyond a cap per
-- project by the writer.
CREATE TABLE IF NOT EXISTS project_sql_history (
  id TEXT PRIMARY KEY,
  organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  actor_kind TEXT NOT NULL DEFAULT '',
  actor_id TEXT NOT NULL DEFAULT '',
  query TEXT NOT NULL,
  duration_ms BIGINT NOT NULL DEFAULT 0,
  row_count INTEGER NOT NULL DEFAULT 0,
  success BOOLEAN NOT NULL DEFAULT TRUE,
  error TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS project_sql_history_project_idx
  ON project_sql_history (project_id, created_at DESC);
