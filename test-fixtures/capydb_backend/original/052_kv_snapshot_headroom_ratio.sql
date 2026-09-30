-- The K/V memory pair moves from a 2x to a 1.5x snapshot ceiling.
--
-- 048 required maxmemory_mb * 2 <= mem_max_mb whenever persistence is `rdb`:
-- BGSAVE forks, and copy-on-write can in the worst case duplicate the whole
-- dataset while the child serialises it. That worst case is a keyspace
-- rewritten wholesale mid-snapshot. The workloads K/V is sold for - counters,
-- sessions, queues - touch a small fraction of their keys per snapshot window,
-- and the first week of production stores held 4 MB RSS against 1024 MB
-- ceilings. The 2x ceiling was what exhausted the host budget after three
-- stores, not the 512 MB customers can actually use.
--
-- service.kvSizing now derives 768/512, 192/128 and 48/32; the worker's
-- plan-divergence sweep brings every running store to the new pair through
-- kv.resize (cgroup shrunk AFTER maxmemory is confirmed, never before). The
-- host scripts carry the same 1.5x guard.
--
-- Postgres named the 048 CHECK kv_stores_check1; it is dropped by that name and
-- recreated with one that says what it is.
ALTER TABLE kv_stores DROP CONSTRAINT IF EXISTS kv_stores_check1;
ALTER TABLE kv_stores ADD CONSTRAINT kv_stores_snapshot_headroom_check
  CHECK (persistence <> 'rdb' OR maxmemory_mb * 3 <= mem_max_mb * 2);
