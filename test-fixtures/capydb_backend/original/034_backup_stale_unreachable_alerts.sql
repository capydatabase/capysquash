-- Two new project_alerts kinds, both full-pipeline (webhook + email + operator
-- webhook), added after the 2026-07-24 backup outage + hermesai livelock:
--
--   "backup_stale"  - a project with an ACTIVE scheduled backup has no verified
--                     backup within its cadence + grace. Outcome-based: unlike
--                     the event-driven "backup" kind (which needs a backup JOB to
--                     reach terminal failure), this fires even when backups stop
--                     being enqueued/verified/recorded entirely - the exact shape
--                     of the 9-day silent gap (dumps reached R2 but never verified
--                     or recorded, so the job never "failed", it just never
--                     succeeded).
--
--   "unreachable"   - the alert sweep's probe connection to a state=running
--                     instance failed: the metadata row says running but the
--                     backend is not serving (the desync/livelock that left
--                     hermesai dark for 8h). Resolves on the next sweep that
--                     connects; the reconciler settles the row to asleep within a
--                     cycle so this is a bounded, high-signal notification.
ALTER TABLE project_alerts
  DROP CONSTRAINT IF EXISTS project_alerts_kind_check;
ALTER TABLE project_alerts
  ADD CONSTRAINT project_alerts_kind_check CHECK (kind IN (
    'storage', 'connections', 'backup', 'backup_stale',
    'cache_hit', 'blocked_queries', 'deadlocks', 'vacuum',
    'pooler_handshake', 'unreachable'
  ));
