-- Persist the destructive multi-job lifecycles that cannot be reconstructed
-- safely from instance ancestry or a project's free-text state alone.
--
-- Major upgrades retain both exact instance ids through confirm/rollback.
-- Import-follow retains the exact target instance and serializes status,
-- cutover, and abort against one active follower.
CREATE TABLE project_major_upgrades (
  project_id        TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
  source_instance_id TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
  target_instance_id TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
  target_major       INTEGER NOT NULL CHECK (target_major > 0),
  state              TEXT NOT NULL CHECK (state IN (
    'staging', 'rollback_available', 'confirming', 'rolling_back'
  )),
  created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CHECK (source_instance_id <> target_instance_id)
);

CREATE UNIQUE INDEX project_major_upgrades_source_idx
  ON project_major_upgrades (source_instance_id);
CREATE UNIQUE INDEX project_major_upgrades_target_idx
  ON project_major_upgrades (target_instance_id);

CREATE TABLE project_import_follows (
  project_id   TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
  instance_id  TEXT NOT NULL REFERENCES instances(id) ON DELETE CASCADE,
  state        TEXT NOT NULL CHECK (state IN (
    'starting', 'following', 'cutting_over', 'aborting'
  )),
  created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Turn the inbound replay guard into an atomic claim/complete lease. Historical
-- rows were recorded only after successful processing, so they are completed.
ALTER TABLE inbound_webhook_deliveries
  ADD COLUMN state TEXT NOT NULL DEFAULT 'completed',
  ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  ADD COLUMN last_error TEXT;

ALTER TABLE inbound_webhook_deliveries
  ADD CONSTRAINT inbound_webhook_deliveries_state_check
  CHECK (state IN ('processing', 'completed', 'failed'));

-- A project may have only one non-deleted preview with a given normalized name.
-- This is the final concurrency guard behind provider webhook retries.
CREATE UNIQUE INDEX preview_databases_one_live_name_per_project
  ON preview_databases (project_id, LOWER(name))
  WHERE state NOT IN ('deleted', 'deleting');
