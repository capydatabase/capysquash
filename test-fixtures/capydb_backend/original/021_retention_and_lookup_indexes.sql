-- 021: indexes for the metadata retention sweep and hot lookup paths.
--
-- Retention (PruneExpiredMetadata) deletes by age in batches; without these
-- partial/targeted indexes every sweep is a sequential scan over tables that
-- only ever grow. The proxy resolves inbound SNI hostnames against
-- projects.public_host / preview_databases.public_host on every uncached
-- connection (GetInstanceByConnectionHost) - an unknown hostname currently
-- costs two sequential scans.

-- Retention scans
CREATE INDEX IF NOT EXISTS jobs_terminal_completed_idx
  ON jobs (completed_at)
  WHERE state IN ('completed', 'failed') AND completed_at IS NOT NULL;

CREATE INDEX IF NOT EXISTS cluster_health_samples_created_idx
  ON cluster_health_samples (created_at);

CREATE INDEX IF NOT EXISTS webhook_deliveries_settled_created_idx
  ON webhook_deliveries (created_at)
  WHERE state IN ('delivered', 'failed');

-- Health-probe due check (latest sample per host within the interval)
CREATE INDEX IF NOT EXISTS cluster_health_samples_host_created_idx
  ON cluster_health_samples (host_id, created_at DESC);

-- Proxy SNI route lookups
CREATE INDEX IF NOT EXISTS projects_public_host_idx
  ON projects (public_host)
  WHERE public_host IS NOT NULL;

CREATE INDEX IF NOT EXISTS preview_databases_public_host_idx
  ON preview_databases (public_host)
  WHERE public_host IS NOT NULL;

-- Org-scoped list paths that had no covering index
CREATE INDEX IF NOT EXISTS jobs_org_completed_idx
  ON jobs (organization_id, completed_at DESC);

CREATE INDEX IF NOT EXISTS webhook_deliveries_org_created_idx
  ON webhook_deliveries (organization_id, created_at DESC);

CREATE INDEX IF NOT EXISTS api_keys_org_idx
  ON api_keys (organization_id);
