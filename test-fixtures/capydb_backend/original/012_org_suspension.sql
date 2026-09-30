-- Soft-suspension for organizations. Set when an upstream identity event (e.g. a
-- Clerk organization deletion) should stop new provisioning without destroying
-- the real databases the org still owns. Operators reconcile suspended orgs
-- manually; nothing here cascades to project/database deletion.
ALTER TABLE organizations
  ADD COLUMN IF NOT EXISTS suspended_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS suspended_reason TEXT;
