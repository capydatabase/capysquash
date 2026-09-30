-- Whether an API key may perform organization-manager actions (key issuance,
-- project deletion, production-overwrite restores, webhook and K/V management -
-- everything behind ensureOrganizationManager). Until now every org-wide key
-- passed that check, which was safe only while admins were the sole minters.
-- The remote MCP consent page lets an organization member approve a connection,
-- and the key it mints must not carry more power than the member's own session:
-- it is stored with manager = false. Existing keys were all minted by admins (or
-- by the platform), so they keep manager = true.
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS manager BOOLEAN NOT NULL DEFAULT TRUE;
