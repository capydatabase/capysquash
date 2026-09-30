-- 038: ARC-pollution response state + wake-event history.
--
-- arc_cache_demoted: the automated cache-demotion trip (a scan-heavy tenant
-- polluting the host-wide ZFS ARC gets its heap dataset flipped to
-- primarycache=metadata, and back once its read rate stays low) needs its
-- state durable so a worker restart cannot strand an instance demoted with
-- nothing left to revert it.
--
-- instance_wake_events: one row per asleep/waking -> running transition,
-- stamped by the same store paths that stamp instances.last_woke_at (which
-- only keeps the LAST wake - useless for pattern detection). Feeds the
-- predictive pre-wake sweep's hour-of-week histogram. Pruned by the sweep
-- after 8 weeks; no FK so instance deletion never blocks on history.
ALTER TABLE instances ADD COLUMN IF NOT EXISTS arc_cache_demoted boolean NOT NULL DEFAULT false;

CREATE TABLE IF NOT EXISTS instance_wake_events (
    instance_id text NOT NULL,
    woke_at timestamptz NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS instance_wake_events_instance_woke_idx
    ON instance_wake_events (instance_id, woke_at DESC);
