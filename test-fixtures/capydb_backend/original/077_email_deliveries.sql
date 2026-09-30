-- Durable email delivery: every notification email (customer alert emails,
-- billing notices, operator ops alerts) is written to email_deliveries and sent
-- by the worker's email sweep, instead of being sent inline where a failed or
-- interrupted send was simply lost.
--
-- One row is one Resend request: up to 50 recipients (Resend's per-request cap)
-- with the subject and body already rendered, so a retry sends exactly what was
-- queued. No secrets are stored; the body is the notification copy. The row id
-- is sent as Resend's Idempotency-Key, so a worker that crashes between the send
-- and the state update re-sends under the same key and Resend returns the
-- original result instead of delivering twice.
--
-- organization_id is NULL for ops alerts (they go to the platform operator);
-- deleting an organization drops its undelivered mail with it.
CREATE TABLE IF NOT EXISTS email_deliveries (
  id TEXT PRIMARY KEY,
  organization_id TEXT REFERENCES organizations(id) ON DELETE CASCADE,
  category TEXT NOT NULL CHECK (category IN ('alert', 'billing', 'ops')),
  recipients TEXT[] NOT NULL CHECK (cardinality(recipients) BETWEEN 1 AND 50),
  subject TEXT NOT NULL,
  body TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'sent', 'dead')),
  attempts INTEGER NOT NULL DEFAULT 0,
  max_attempts INTEGER NOT NULL DEFAULT 8,
  next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  last_error TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  sent_at TIMESTAMPTZ,
  CHECK (category = 'ops' OR organization_id IS NOT NULL)
);

CREATE INDEX IF NOT EXISTS email_deliveries_due_idx
  ON email_deliveries (next_attempt_at ASC)
  WHERE state = 'pending';
CREATE INDEX IF NOT EXISTS email_deliveries_settled_idx
  ON email_deliveries (updated_at)
  WHERE state <> 'pending';

-- Per-organization notification preferences. No row means the defaults, which
-- are the behaviour before preferences existed: alert emails on, no extra
-- recipients. Billing notices (suspension, read-only, offline, restored) have
-- no off switch on purpose - they announce that the customer's databases are
-- about to change state - so only their extra recipients are configurable.
CREATE TABLE IF NOT EXISTS organization_notification_preferences (
  organization_id TEXT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
  alert_emails_enabled BOOLEAN NOT NULL DEFAULT TRUE,
  alert_email_recipients TEXT[] NOT NULL DEFAULT '{}',
  billing_email_recipients TEXT[] NOT NULL DEFAULT '{}',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- A billing notice is now settled as 'queued' once its email is handed to
-- email_deliveries; the delivery row records whether it was actually sent.
ALTER TABLE organization_billing_notices
  DROP CONSTRAINT IF EXISTS organization_billing_notices_outcome_check;
ALTER TABLE organization_billing_notices
  ADD CONSTRAINT organization_billing_notices_outcome_check
  CHECK (outcome IN ('sent', 'skipped', 'no_recipient', 'failed', 'queued'));
