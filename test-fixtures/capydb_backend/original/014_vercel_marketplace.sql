-- Vercel Marketplace (native integration) support. Organizations provisioned
-- through the Vercel Marketplace are billed by Vercel, not Polar, and are keyed
-- by the Vercel installation id so the integration server can resolve them
-- without knowing CapyDB org ids up front.
ALTER TABLE organizations
  ADD COLUMN IF NOT EXISTS vercel_installation_id TEXT;

-- One CapyDB organization per Vercel installation. Partial so the (NULL) bulk
-- of non-marketplace organizations stays out of the index.
CREATE UNIQUE INDEX IF NOT EXISTS organizations_vercel_installation_idx
  ON organizations (vercel_installation_id)
  WHERE vercel_installation_id IS NOT NULL;

-- billing_provider was constrained to 'polar' in 007; Vercel-billed orgs need
-- the second provider value.
ALTER TABLE organizations
  DROP CONSTRAINT IF EXISTS organizations_billing_provider_check;
ALTER TABLE organizations
  ADD CONSTRAINT organizations_billing_provider_check
  CHECK (billing_provider IN ('polar', 'vercel'));
