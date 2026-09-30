-- Two CHECK constraints fell behind the vocabularies the code writes.
--
-- 1. project_alerts_kind_check was last rewritten by 046 and never learned
--    `kv_unreachable` (added with the K/V watcher, 2026-09-10). Every attempt to
--    open that alert failed with 23514, the sweep logged and swallowed it per
--    project, and the K/V reachability alert - email and webhook included -
--    could never fire.
--
-- 2. project_integrations_provider_check dates from 009 and still lists the six
--    original providers. The service accepts nine (service.integrationProviders):
--    `auth0`, `better_auth` and `cloudflare` were added later without widening
--    the constraint, so connecting any of them died at the INSERT with a masked
--    500.
--
-- Both lists below are the code's lists, verbatim. A live-store test now
-- inserts every model.ProjectAlertKind* constant and every accepted provider
-- so the next addition cannot ship without the constraint following it.
ALTER TABLE project_alerts
  DROP CONSTRAINT IF EXISTS project_alerts_kind_check;
ALTER TABLE project_alerts
  ADD CONSTRAINT project_alerts_kind_check CHECK (kind IN (
    'storage', 'connections', 'backup', 'backup_stale',
    'cache_hit', 'blocked_queries', 'deadlocks', 'vacuum',
    'long_transaction', 'subtransactions', 'oom_kill', 'temp_spill',
    'pooler_handshake', 'unreachable', 'kv_unreachable'
  ));

ALTER TABLE project_integrations
  DROP CONSTRAINT IF EXISTS project_integrations_provider_check;
ALTER TABLE project_integrations
  ADD CONSTRAINT project_integrations_provider_check CHECK (provider IN (
    'auth0', 'better_auth', 'clerk', 'cloudflare', 'drizzle', 'github',
    'netlify', 'supabase_auth', 'vercel'
  ));
