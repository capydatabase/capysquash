-- 025_instance_read_only.sql
-- Soft read-only enforcement (F35 WAL-archiver trip, U1 suspension ladder).
-- read_only_reason records WHY default_transaction_read_only was applied
-- ('wal_archiver_lag' | 'org_suspended'); the instance.set_readonly handler only
-- clears a matching reason, so a WAL-lag recovery can never undo a suspension
-- trip (and vice versa). NULL means no trip is active.

ALTER TABLE instances ADD COLUMN IF NOT EXISTS read_only_reason TEXT;

-- The clear sweeps scan for instances still carrying a reason; partial index
-- keeps that cheap without indexing the (overwhelmingly NULL) common case.
CREATE INDEX IF NOT EXISTS instances_read_only_reason_idx
  ON instances (read_only_reason) WHERE read_only_reason IS NOT NULL;
