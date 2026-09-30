-- Ephemeral databases: a throwaway Postgres anyone can create without an
-- account, that destroys itself after a fixed lifetime unless it is claimed.
--
-- An ephemeral database is an ordinary project. It is owned by one platform
-- organization (seeded below) instead of a customer, so provisioning, the
-- entitlement sweeps, scale-to-zero and instance.destroy all treat it exactly
-- like any other vibe-sized project and none of them needed an "ephemeral"
-- branch. What makes it ephemeral is the row in ephemeral_databases: the
-- lifetime, and the SHA-256 of the claim token handed to the anonymous creator
-- (POST /v1/ephemeral-databases returns the raw eph_ token exactly once).
--
-- That token is the only credential an unclaimed database has. It reads the
-- connection strings back, and it lets a signed-in organization claim the
-- database - which moves the project to that organization
-- (transferProjectOrganizationTx), stamps claimed_at, and ends the lifetime.
-- The worker's expiry sweep and the claim both lock this row first, so a
-- database is either claimed or destroyed, never both.
--
-- The platform organization carries billing_plan 'vibe' / 'active' so the
-- provisioning gate (ensureOrganizationCanProvision) holds for it without a
-- bypass. Its live-project count IS the number of unclaimed ephemeral
-- databases, which is how CAPYDB_EPHEMERAL_MAX_ACTIVE is enforced inside the
-- create transaction. KEEP IN LOCKSTEP with model.EphemeralOrganizationID.
INSERT INTO organizations (id, name, slug, billing_plan, billing_status)
VALUES ('org_ephemeral', 'Ephemeral databases', 'capydb-ephemeral', 'vibe', 'active')
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS ephemeral_databases (
    project_id TEXT PRIMARY KEY REFERENCES projects(id) ON DELETE CASCADE,
    claim_token_sha256 TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    claimed_at TIMESTAMPTZ,
    claimed_by_organization_id TEXT REFERENCES organizations(id) ON DELETE SET NULL
);

-- The expiry sweep only ever looks at unclaimed rows, oldest lifetime first.
CREATE INDEX IF NOT EXISTS idx_ephemeral_databases_unclaimed_expiry
    ON ephemeral_databases (expires_at)
    WHERE claimed_at IS NULL;
