ALTER TABLE jobs
  ADD COLUMN IF NOT EXISTS available_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

UPDATE jobs
SET available_at = COALESCE(updated_at, created_at, NOW())
WHERE available_at IS NULL;

CREATE INDEX IF NOT EXISTS jobs_pending_available_idx
  ON jobs (state, available_at, created_at);
