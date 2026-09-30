-- 036: scale-to-zero cycle timestamps for the idle sweep's thrash backoff.
-- 2026-08-03 audit: a cell was re-woken by traffic 45 seconds after the idle
-- sweep slept it. The sweep now compares the previous sleep->wake gap and
-- requires a longer idle window before re-sleeping an instance whose last
-- sleep was immediately undone by traffic. Stamped by UpdateInstanceState /
-- WakeInstanceRow / MarkInstanceActive on the state transitions themselves so
-- every sleep/wake path (job, proxy wake writeback, reconciler settle)
-- records them.
ALTER TABLE instances ADD COLUMN IF NOT EXISTS last_slept_at timestamptz;
ALTER TABLE instances ADD COLUMN IF NOT EXISTS last_woke_at timestamptz;
