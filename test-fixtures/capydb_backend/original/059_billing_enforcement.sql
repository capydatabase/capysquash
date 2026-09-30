-- Billing enforcement: the state a lapsed organization's suspension ladder runs
-- on, and the ledger of the notices it sends. Policy lives in
-- internal/service/billing.go (organizationBillingStanding) and the worker's
-- standing and ladder sweeps; this migration only adds the columns they read.

-- When billing_status last changed. The past_due grace period is measured from
-- here, not from billing_period_end: Polar's retries run past the period end,
-- and Vercel sends no period at all. Written only when the status actually
-- changes (updateOrganizationBillingIfNewerTx), so a replayed past_due does not
-- restart the clock. Backfilled from the last provider event, which is the best
-- record of when the current status was set.
ALTER TABLE organizations
  ADD COLUMN IF NOT EXISTS billing_status_since TIMESTAMPTZ;
UPDATE organizations
  SET billing_status_since = COALESCE(billing_event_at, updated_at, created_at)
  WHERE billing_status_since IS NULL;
ALTER TABLE organizations
  ALTER COLUMN billing_status_since SET DEFAULT NOW(),
  ALTER COLUMN billing_status_since SET NOT NULL;

-- Where a suspended organization stands on the ladder: 0 = suspended (API
-- mutations and provisioning refused, data plane untouched), 1 = read-only,
-- 2 = offline (connections, K/V and Studio refused). Only the ladder sweep
-- raises it; clearing the suspension resets it. suspension_rung_since is when
-- the current rung was entered.
ALTER TABLE organizations
  ADD COLUMN IF NOT EXISTS suspension_rung SMALLINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS suspension_rung_since TIMESTAMPTZ;
ALTER TABLE organizations
  DROP CONSTRAINT IF EXISTS organizations_suspension_rung_check;
ALTER TABLE organizations
  ADD CONSTRAINT organizations_suspension_rung_check
  CHECK (suspension_rung BETWEEN 0 AND 2 AND (suspended_at IS NOT NULL OR suspension_rung = 0));

-- suspended_reason becomes a code, because two writers now clear suspensions
-- and each may clear only its own: the Clerk sync clears clerk_deleted, a
-- successful payment clears billing_lapsed, and nothing automatic clears admin.
-- Until now the Clerk deletion was the only writer, so every existing
-- suspension is one.
UPDATE organizations
  SET suspended_reason = 'clerk_deleted'
  WHERE suspended_at IS NOT NULL
    AND COALESCE(suspended_reason, '') NOT IN ('clerk_deleted', 'billing_lapsed', 'admin');
UPDATE organizations
  SET suspended_reason = NULL
  WHERE suspended_at IS NULL AND suspended_reason IS NOT NULL;
ALTER TABLE organizations
  DROP CONSTRAINT IF EXISTS organizations_suspended_reason_check;
ALTER TABLE organizations
  ADD CONSTRAINT organizations_suspended_reason_check
  CHECK (
    (suspended_at IS NULL AND suspended_reason IS NULL)
    OR (suspended_at IS NOT NULL AND suspended_reason IN ('clerk_deleted', 'billing_lapsed', 'admin'))
  );

-- manual: an organization billed outside any provider (the platform's own and
-- internal organizations, invoiced deals). Always in good standing, and a
-- provider sync that would clear its plan is ignored.
ALTER TABLE organizations
  DROP CONSTRAINT IF EXISTS organizations_billing_provider_check;
ALTER TABLE organizations
  ADD CONSTRAINT organizations_billing_provider_check
  CHECK (billing_provider IN ('polar', 'vercel', 'cloudflare', 'manual'));

-- Paid organizations with no provider subscription behind them are exactly
-- those billed by hand. Left on 'polar' they would read as lapsed the moment
-- enforcement is switched on.
UPDATE organizations
  SET billing_provider = 'manual'
  WHERE billing_provider = 'polar'
    AND billing_plan <> 'none'
    AND COALESCE(billing_subscription_id, '') = ''
    AND vercel_installation_id IS NULL
    AND cloudflare_account_id IS NULL;

-- One row per notice per suspension episode (keyed by the episode's
-- suspended_at), so each notice goes out at most once however often the sweeps
-- run. outcome/settled_at record how delivery ended; attempts bounds retries.
CREATE TABLE IF NOT EXISTS organization_billing_notices (
  organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
  episode_started_at TIMESTAMPTZ NOT NULL,
  kind TEXT NOT NULL CHECK (kind IN ('suspended', 'read_only', 'offline', 'restored', 'offline_ops_alert')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  attempts INTEGER NOT NULL DEFAULT 0,
  outcome TEXT CHECK (outcome IN ('sent', 'skipped', 'no_recipient', 'failed')),
  recipients TEXT,
  last_error TEXT,
  settled_at TIMESTAMPTZ,
  PRIMARY KEY (organization_id, episode_started_at, kind)
);

CREATE INDEX IF NOT EXISTS organization_billing_notices_pending_idx
  ON organization_billing_notices (created_at)
  WHERE settled_at IS NULL;
