-- 027_billing_event_ordering.sql
-- Billing-sync ordering guard (audit M10): Polar webhooks carry no dedupe or
-- ordering guarantee, so a redelivered/late subscription.updated could silently
-- overwrite newer billing state. billing_event_at records the event timestamp
-- of the last applied sync; SyncOrganizationBilling no-ops anything at or
-- before it. Full-state manual syncs stamp NOW().
ALTER TABLE organizations
  ADD COLUMN IF NOT EXISTS billing_event_at TIMESTAMPTZ;
