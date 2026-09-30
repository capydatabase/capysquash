-- 020_instance_standby.sql
-- Premium HA: a streaming-replica standby is an instance row that points at its
-- primary via standby_of (distinct from parent_instance_id, which is a ZFS-clone
-- branch). role distinguishes the primary from its replica. Feature-flagged
-- (CAPYDB_PREMIUM_TIER_ENABLED) and only populated once a second host exists.

ALTER TABLE instances ADD COLUMN IF NOT EXISTS standby_of TEXT REFERENCES instances(id) ON DELETE CASCADE;
ALTER TABLE instances ADD COLUMN IF NOT EXISTS role TEXT NOT NULL DEFAULT 'primary'; -- 'primary' | 'replica'
CREATE INDEX IF NOT EXISTS instances_standby_of_idx ON instances (standby_of);
