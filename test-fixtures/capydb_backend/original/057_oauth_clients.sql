-- OAuth clients registered through dynamic client registration (RFC 7591) by MCP
-- hosts - Claude, Claude Code, Cursor, VS Code - before they send a user through
-- the control plane's authorization server (/oauth/*) to connect to the remote
-- MCP server. Every client is public (PKCE, no secret), so a row holds only what
-- the authorize step checks and the consent screen shows. Hosts keep a client_id
-- indefinitely, which is why this is a table and not process memory: a restart
-- must not orphan every connected host. Pending authorizations and codes live
-- ten minutes and one minute respectively and stay in memory.
CREATE TABLE IF NOT EXISTS oauth_clients (
    id TEXT PRIMARY KEY,
    client_name TEXT NOT NULL,
    redirect_uris JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
