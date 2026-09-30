-- New project_alerts kind: "pooler_handshake" - the instance's pooled
-- endpoint is refusing/failing client connections at handshake (unsupported
-- startup parameters, authentication failures) while Postgres itself is
-- healthy. Counted on the host from the pooler unit's journal by the
-- host-stats probe and evaluated by the alert sweep (2026-07-22 incident
-- follow-up: a client sending statement_timeout as a wire startup parameter
-- had every pooled connection refused for ~34h with no platform-side signal).
-- Unlike the metrics advisories this kind notifies through the full pipeline
-- (webhook + email): the affected application is typically fully down.
ALTER TABLE project_alerts
  DROP CONSTRAINT IF EXISTS project_alerts_kind_check;
ALTER TABLE project_alerts
  ADD CONSTRAINT project_alerts_kind_check CHECK (kind IN (
    'storage', 'connections', 'backup',
    'cache_hit', 'blocked_queries', 'deadlocks', 'vacuum',
    'pooler_handshake'
  ));
