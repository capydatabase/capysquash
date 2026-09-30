-- Per-project opt-out from the scale-to-zero idle sweep.
--
-- Until now CAPYDB_SCALE_TO_ZERO_ENABLED was platform-wide and the sweep
-- (worker.sweepIdleInstances -> store.ListIdleRunningInstances) filtered on
-- exactly one thing: projects in state 'importing'. A production cell was
-- therefore slept after CAPYDB_IDLE_SLEEP_AFTER like any other, and the first
-- request afterwards paid a wake on top of the app's own cold start. The
-- myroomiev3 cutover hit this 13 times in 11 hours.
--
-- Production defaults to always-on: a customer who marked a project production
-- has already told us the latency answer. Non-production keeps sleeping, which
-- is where scale-to-zero earns its keep.
ALTER TABLE projects
  ADD COLUMN IF NOT EXISTS always_on BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE projects
SET always_on = TRUE
WHERE environment = 'production'
  AND always_on = FALSE;
