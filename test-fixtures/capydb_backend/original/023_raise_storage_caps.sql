-- 023_raise_storage_caps.sql
-- Raise the included per-project storage caps for competitiveness:
--   vibe     1 GiB -> 2 GiB
--   ship     5 GiB -> 10 GiB
--   business 10 GiB -> 25 GiB
--
-- billing.go (planEntitlementsByPlan) is the source of truth: new projects get the
-- new cap at creation, and the worker's plan-divergence sweep re-applies the ZFS
-- refquota to existing instances (instances.storage_limit_bytes mismatch -> apply_plan
-- resize). This migration backfills the denormalized projects.storage_limit_bytes
-- column that the alert sweep reads, so existing projects on an unchanged plan pick up
-- the higher cap immediately rather than only on their next billing sync.
--
-- The WHERE clause makes it idempotent and non-destructive: it only lifts rows still
-- sitting at the exact previous plan cap, leaving any manually customized limit alone.
UPDATE projects
SET storage_limit_bytes = CASE plan
    WHEN 'business' THEN 26843545600  -- 25 GiB
    WHEN 'ship'     THEN 10737418240  -- 10 GiB
    ELSE 2147483648                   --  2 GiB (vibe / default)
  END
WHERE storage_limit_bytes = CASE plan
    WHEN 'business' THEN 10737418240  -- old 10 GiB
    WHEN 'ship'     THEN 5368709120   -- old  5 GiB
    ELSE 1073741824                   -- old  1 GiB
  END;
