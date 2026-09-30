-- 022: CHECK constraints for the free-text state columns.
--
-- Every state machine in the metadata schema stored its state as unconstrained
-- TEXT, so a typo'd literal in Go (or a manual UPDATE during an incident)
-- silently wedged the row in a state no code path recognizes. Each constraint
-- below enumerates exactly the literals the code writes today:
--
--   projects.state           service.EnqueueProject*/worker Update* transitions
--   instances.state          model.InstanceState* constants + enqueue SQL
--   preview_databases.state  worker/preview lifecycle transitions
--   jobs.state               model.JobState* constants
--   hosts.state              model.HostState* constants (health probe writes)
--   pools.state              model.PoolState* constants (admin create only)
--
-- Constraints on the tables that predate the v2 cutover (projects,
-- preview_databases, jobs) are added NOT VALID: they enforce the enum for
-- every new write without scanning (or failing on) historical rows that may
-- carry pre-v2 vocabulary. hosts/pools/instances were created greenfield in
-- migration 019 and have only ever been written by the current code, so their
-- constraints validate existing rows too.

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'projects_state_check'
  ) THEN
    ALTER TABLE projects
      ADD CONSTRAINT projects_state_check CHECK (state IN (
        'provisioning', 'ready', 'failed', 'deleting', 'deleted',
        'applying_plan', 'backing_up', 'importing', 'restoring', 'rotating_credentials'
      )) NOT VALID;
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'preview_databases_state_check'
  ) THEN
    ALTER TABLE preview_databases
      ADD CONSTRAINT preview_databases_state_check CHECK (state IN (
        'provisioning', 'ready', 'failed', 'deleting', 'deleted'
      )) NOT VALID;
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'jobs_state_check'
  ) THEN
    ALTER TABLE jobs
      ADD CONSTRAINT jobs_state_check CHECK (state IN (
        'pending', 'running', 'completed', 'failed'
      )) NOT VALID;
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'instances_state_check'
  ) THEN
    ALTER TABLE instances
      ADD CONSTRAINT instances_state_check CHECK (state IN (
        'provisioning', 'running', 'asleep', 'waking', 'error', 'destroying'
      ));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'hosts_state_check'
  ) THEN
    ALTER TABLE hosts
      ADD CONSTRAINT hosts_state_check CHECK (state IN (
        'active', 'degraded', 'error'
      ));
  END IF;

  IF NOT EXISTS (
    SELECT 1 FROM pg_constraint WHERE conname = 'pools_state_check'
  ) THEN
    ALTER TABLE pools
      ADD CONSTRAINT pools_state_check CHECK (state IN (
        'active', 'degraded'
      ));
  END IF;
END $$;
