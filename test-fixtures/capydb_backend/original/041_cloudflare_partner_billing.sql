-- Cloudflare-billed databases (Hyperdrive database-integration partner program).
-- A customer creates the database from the Cloudflare dashboard; Cloudflare
-- mints a short-lived signed authorization, CapyDB provisions against it, and
-- Cloudflare invoices the usage. The unit of identity is the Cloudflare account
-- id (there is no installation object like Vercel's), so organizations are keyed
-- by it exactly the way marketplace organizations are keyed by installation id.
ALTER TABLE organizations
  ADD COLUMN IF NOT EXISTS cloudflare_account_id TEXT;

-- One CapyDB organization per Cloudflare account. Partial so the (NULL) bulk of
-- non-Cloudflare organizations stays out of the index, and unique so concurrent
-- authorizations for the same account cannot create two organizations.
CREATE UNIQUE INDEX IF NOT EXISTS organizations_cloudflare_account_idx
  ON organizations (cloudflare_account_id)
  WHERE cloudflare_account_id IS NOT NULL;

-- billing_provider was constrained to ('polar', 'vercel') in 014; Cloudflare is
-- the third party that can own an organization's invoice.
ALTER TABLE organizations
  DROP CONSTRAINT IF EXISTS organizations_billing_provider_check;
ALTER TABLE organizations
  ADD CONSTRAINT organizations_billing_provider_check
  CHECK (billing_provider IN ('polar', 'vercel', 'cloudflare'));
