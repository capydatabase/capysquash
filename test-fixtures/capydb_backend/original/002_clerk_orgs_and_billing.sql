ALTER TABLE organizations
  ADD COLUMN IF NOT EXISTS clerk_organization_id TEXT,
  ADD COLUMN IF NOT EXISTS clerk_organization_slug TEXT,
  ADD COLUMN IF NOT EXISTS billing_plan TEXT NOT NULL DEFAULT 'none',
  ADD COLUMN IF NOT EXISTS billing_status TEXT NOT NULL DEFAULT 'inactive',
  ADD COLUMN IF NOT EXISTS billing_email TEXT,
  ADD COLUMN IF NOT EXISTS billing_name TEXT,
  ADD COLUMN IF NOT EXISTS billing_provider TEXT NOT NULL DEFAULT 'polar',
  ADD COLUMN IF NOT EXISTS billing_customer_id TEXT,
  ADD COLUMN IF NOT EXISTS billing_period_end TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE UNIQUE INDEX IF NOT EXISTS organizations_clerk_org_idx
  ON organizations (clerk_organization_id)
  WHERE clerk_organization_id IS NOT NULL;
