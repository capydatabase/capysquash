-- Split role model: a project can opt into a second login role on its
-- database - the runtime role (a fixed name, model.AppRoleName). It owns
-- nothing and is never a member of the owner, so row-level security applies
-- to it, while the owner holds membership in it WITH INHERIT FALSE, SET TRUE
-- (one owner credential can SET ROLE to it). See capydb-app-role.sh.
--
-- One row per opted-in project, mirroring project_credentials (same AES-256-GCM
-- encryption under CAPYDB_ENCRYPTION_KEY_BASE64). No row = the project never
-- opted in; existing projects are untouched. The row is written by the worker
-- only once the role exists on the instance, so a row always names a login
-- that works.
CREATE TABLE IF NOT EXISTS project_app_credentials (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL UNIQUE REFERENCES projects(id) ON DELETE CASCADE,
  username TEXT NOT NULL,
  password_encrypted TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  rotated_at TIMESTAMPTZ
);

-- A preview of an opted-in project carries the runtime role too (a ZFS clone
-- inherits it), with its OWN password - the clone would otherwise accept
-- production's. Set once per preview (first provision) and reused by resets
-- and restores into the same preview. NULL = no runtime login on this preview.
ALTER TABLE preview_databases ADD COLUMN IF NOT EXISTS app_password_encrypted TEXT;
