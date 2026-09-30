-- Vercel Marketplace installation access token. Vercel hands the partner an
-- API token on installation upsert; the stateless marketplace app forwards it
-- here (the organization row IS the installation record - see migration 014's
-- unique vercel_installation_id index) so billing-data submission, invoicing,
-- and async secret updates can call Vercel's API later. Encrypted with the
-- same cipher as project credentials; never returned by customer-facing
-- endpoints.
ALTER TABLE organizations
  ADD COLUMN IF NOT EXISTS vercel_access_token_encrypted TEXT;

-- Invoicing cursor for the marketplace billing cron: the end of the last
-- billing period an invoice was submitted to Vercel for. NULL = never
-- invoiced. Advancing it is idempotent bookkeeping, not billing state.
ALTER TABLE organizations
  ADD COLUMN IF NOT EXISTS vercel_invoiced_through TIMESTAMPTZ;
