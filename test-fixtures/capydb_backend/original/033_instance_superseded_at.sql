-- A major-version upgrade swaps the project's primary to a fresh instance and
-- KEEPS the old one asleep as an instant rollback. superseded_at marks when an
-- instance became a retained rollback source, so a periodic sweep can finalize
-- (destroy) it after the rollback window elapses instead of it lingering and
-- charging the pool's admission budget forever. NULL for every normal instance.
ALTER TABLE instances ADD COLUMN IF NOT EXISTS superseded_at timestamptz;

-- Fast lookup for the retention sweep (partial: only retained instances).
CREATE INDEX IF NOT EXISTS instances_superseded_at_idx
  ON instances (superseded_at) WHERE superseded_at IS NOT NULL;
