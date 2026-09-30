ALTER TABLE organizations
  ADD COLUMN IF NOT EXISTS billing_subscription_id TEXT,
  ADD COLUMN IF NOT EXISTS billing_product_id TEXT;

UPDATE organizations
SET billing_plan = CASE LOWER(COALESCE(billing_plan, ''))
  WHEN 'hobby' THEN 'vibe'
  WHEN 'pro' THEN 'ship'
  WHEN 'team' THEN 'business'
  WHEN 'vibe' THEN 'vibe'
  WHEN 'ship' THEN 'ship'
  WHEN 'business' THEN 'business'
  ELSE 'none'
END;

UPDATE organizations
SET billing_status = CASE LOWER(COALESCE(billing_status, ''))
  WHEN 'active' THEN 'active'
  WHEN 'trialing' THEN 'trialing'
  WHEN 'past_due' THEN 'past_due'
  WHEN 'canceled' THEN 'canceled'
  WHEN 'revoked' THEN 'revoked'
  WHEN 'unpaid' THEN 'unpaid'
  ELSE 'inactive'
END;

UPDATE organizations
SET billing_provider = 'polar'
WHERE LOWER(COALESCE(billing_provider, '')) IN ('', 'manual', 'polar');

ALTER TABLE organizations
  ALTER COLUMN billing_plan SET DEFAULT 'none',
  ALTER COLUMN billing_status SET DEFAULT 'inactive',
  ALTER COLUMN billing_provider SET DEFAULT 'polar';

UPDATE projects
SET plan = CASE LOWER(COALESCE(plan, ''))
  WHEN 'hobby' THEN 'vibe'
  WHEN 'pro' THEN 'ship'
  WHEN 'team' THEN 'business'
  WHEN 'vibe' THEN 'vibe'
  WHEN 'ship' THEN 'ship'
  WHEN 'business' THEN 'business'
  ELSE 'vibe'
END
WHERE state <> 'deleted';

ALTER TABLE projects
  ALTER COLUMN plan SET DEFAULT 'vibe';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'organizations_billing_plan_check'
  ) THEN
    ALTER TABLE organizations
      ADD CONSTRAINT organizations_billing_plan_check
      CHECK (billing_plan IN ('none', 'vibe', 'ship', 'business'));
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'organizations_billing_status_check'
  ) THEN
    ALTER TABLE organizations
      ADD CONSTRAINT organizations_billing_status_check
      CHECK (billing_status IN ('inactive', 'trialing', 'active', 'past_due', 'canceled', 'revoked', 'unpaid'));
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'organizations_billing_provider_check'
  ) THEN
    ALTER TABLE organizations
      ADD CONSTRAINT organizations_billing_provider_check
      CHECK (billing_provider = 'polar');
  END IF;
END $$;

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'projects_plan_check'
  ) THEN
    ALTER TABLE projects
      ADD CONSTRAINT projects_plan_check
      CHECK (plan IN ('vibe', 'ship', 'business'));
  END IF;
END $$;
