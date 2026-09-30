-- One caller could hold every unclaimed ephemeral database at once: the
-- platform-wide cap (CAPYDB_EPHEMERAL_MAX_ACTIVE) is the only bound, a slot is
-- held for 72 hours, and the per-address request limiter refills far faster
-- than slots free up. creator_key records who asked - a SHA-256 of the client
-- address, never the address itself - so the create transaction can refuse a
-- creator that already holds CAPYDB_EPHEMERAL_MAX_ACTIVE_PER_CLIENT unclaimed
-- databases. NULL for rows created before this migration and for creates made
-- while the bridge's client address could not be trusted; those are bounded
-- only by the platform-wide cap.
ALTER TABLE ephemeral_databases ADD COLUMN IF NOT EXISTS creator_key TEXT;

CREATE INDEX IF NOT EXISTS idx_ephemeral_databases_unclaimed_creator
    ON ephemeral_databases (creator_key)
    WHERE claimed_at IS NULL AND creator_key IS NOT NULL;
