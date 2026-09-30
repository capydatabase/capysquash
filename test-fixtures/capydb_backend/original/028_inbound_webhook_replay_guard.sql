-- 028: Replay guard for inbound provider deploy webhooks (Vercel, Netlify).
--
-- These webhooks are authenticated only by an HMAC/JWS over the body, with no
-- timestamp (Vercel) or timestamp-bearing signature (Netlify's JWS carries only
-- iss+sha256), so a captured signed delivery could be replayed. The action
-- (ensureBranchPreview) is idempotent, but replay still refreshes a preview TTL
-- it should not. Each accepted delivery is recorded here after it is processed;
-- a matching id arriving again is skipped. Rows are GC'd after a short window
-- (the worker's metadata retention sweep), which is also the replay window.
--
-- id = sha256hex(provider | project_id | raw-signature-header): the signature is
-- deterministic per delivery body, so identical bodies collapse to one id.
CREATE TABLE IF NOT EXISTS inbound_webhook_deliveries (
  id          TEXT PRIMARY KEY,
  provider    TEXT NOT NULL,
  project_id  TEXT NOT NULL,
  received_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS inbound_webhook_deliveries_received_idx
  ON inbound_webhook_deliveries (received_at);
