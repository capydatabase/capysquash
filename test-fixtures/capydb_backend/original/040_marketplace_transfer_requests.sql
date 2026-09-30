-- Vercel Marketplace resource transfers: a customer moving a store from one
-- Vercel team to another. Vercel drives this as three separate partner calls
-- (create -> verify -> accept), each of which can arrive minutes apart and from
-- a different installation, so the claim has to outlive a single request. The
-- integration server is stateless, so the claim lives here.
CREATE TABLE IF NOT EXISTS marketplace_transfer_requests (
  transfer_id TEXT PRIMARY KEY,
  source_installation_id TEXT NOT NULL,
  resource_ids TEXT[] NOT NULL,
  -- Several teams may verify the same claim; only one can accept it.
  target_installation_ids TEXT[] NOT NULL DEFAULT '{}',
  claimed_by_installation_id TEXT,
  status TEXT NOT NULL DEFAULT 'unclaimed',
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT marketplace_transfer_requests_status_check
    CHECK (status IN ('unclaimed', 'verified', 'complete'))
);

CREATE INDEX IF NOT EXISTS marketplace_transfer_requests_source_idx
  ON marketplace_transfer_requests (source_installation_id);
