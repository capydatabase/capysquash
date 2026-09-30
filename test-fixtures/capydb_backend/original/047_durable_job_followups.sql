-- Durable follow-up intent for completed jobs, and idempotent webhook fan-out.
--
-- A job's handler settles the job, and only afterwards does the worker emit its webhook events and
-- enqueue the deployment env re-sync its completion implies. A crash in that gap left a job that
-- looks finished with no record that its follow-ups never ran: the delivery retry queue cannot retry
-- a delivery that was never created, and a completed credential rotation could leave connected
-- deployments authenticating with the old password and nothing queued to repair it.
--
-- followups_done_at makes the job row itself the durable intent. It is set in the same statement
-- that settles the job only once the follow-ups have run, so a NULL on a terminal job is a piece of
-- work the sweep can pick up.

ALTER TABLE jobs ADD COLUMN IF NOT EXISTS followups_done_at TIMESTAMPTZ;

-- Existing terminal jobs already had their follow-ups attempted under the old code. Marking them
-- done keeps the sweep from re-emitting historical events on first boot.
UPDATE jobs SET followups_done_at = updated_at
 WHERE state IN ('completed', 'failed') AND followups_done_at IS NULL;

-- The sweep's only query: terminal jobs whose follow-ups have not been recorded.
CREATE INDEX IF NOT EXISTS jobs_pending_followups_idx
  ON jobs (updated_at ASC)
  WHERE followups_done_at IS NULL AND state IN ('completed', 'failed');

-- Webhook fan-out is re-runnable, so a delivery needs an identity that survives the re-run.
-- event_id is stable for a given (job, event type), which makes the insert below a no-op the second
-- time rather than a duplicate notification.
ALTER TABLE webhook_deliveries ADD COLUMN IF NOT EXISTS event_id TEXT;

-- Partial, so the rows that predate event_id (all NULL) do not collide with each other.
CREATE UNIQUE INDEX IF NOT EXISTS webhook_deliveries_endpoint_event_idx
  ON webhook_deliveries (endpoint_id, event_id)
  WHERE event_id IS NOT NULL;
