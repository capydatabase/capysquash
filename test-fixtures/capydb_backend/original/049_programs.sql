-- Growth programs: startup and partner applications, and customer referrals.
--
-- The control plane is the system of record for who applied, who was accepted
-- and which Polar discount they were issued; the discounts themselves live in
-- Polar and are created by the frontend at decision time (the control plane
-- never holds a Polar token). Nothing here changes an organization's plan -
-- the reward is always a discount redeemed at Polar checkout, which then flows
-- back through the ordinary billing sync.

CREATE TABLE IF NOT EXISTS program_applications (
    id TEXT PRIMARY KEY,
    program TEXT NOT NULL CHECK (program IN ('startup', 'partner')),
    status TEXT NOT NULL DEFAULT 'received' CHECK (status IN ('received', 'accepted', 'declined')),
    company_name TEXT NOT NULL,
    company_website TEXT NOT NULL DEFAULT '',
    contact_name TEXT NOT NULL,
    email TEXT NOT NULL,
    -- BCP-47 locale of the page the application was submitted from; decision
    -- emails render in it.
    locale TEXT NOT NULL DEFAULT 'en',
    -- startup: pre_seed | seed | series_a | bootstrapped
    -- partner: agency | consultancy | freelancer | other
    segment TEXT NOT NULL,
    team_size INTEGER NOT NULL DEFAULT 0,
    use_case TEXT NOT NULL,
    referral_source TEXT NOT NULL DEFAULT '',
    operator_note TEXT NOT NULL DEFAULT '',
    -- The Polar discount issued on acceptance. Empty until accepted.
    discount_code TEXT NOT NULL DEFAULT '',
    discount_id TEXT NOT NULL DEFAULT '',
    decided_at TIMESTAMPTZ,
    decided_by_user_id TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One live application per program per email: a second submission while one
-- is pending or accepted is a conflict, a declined applicant may apply again.
CREATE UNIQUE INDEX IF NOT EXISTS program_applications_live_email
    ON program_applications (program, lower(email))
    WHERE status IN ('received', 'accepted');

CREATE INDEX IF NOT EXISTS idx_program_applications_status
    ON program_applications (status, created_at DESC);

-- One referral code per organization. The code is the Polar discount code the
-- referred workspace redeems; discount_id is filled in once the frontend has
-- created that discount.
CREATE TABLE IF NOT EXISTS referral_codes (
    organization_id TEXT PRIMARY KEY REFERENCES organizations(id) ON DELETE CASCADE,
    code TEXT NOT NULL UNIQUE,
    discount_id TEXT NOT NULL DEFAULT '',
    -- Locale of the dashboard session that generated the code; the reward
    -- email to this organization renders in it.
    locale TEXT NOT NULL DEFAULT 'en',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A referred workspace's journey from redeeming a code to the referrer being
-- rewarded. `rewarding` is the claimed state between "the referred workspace
-- paid a first invoice" and "the reward discount was applied at Polar", so a
-- redelivered order.paid webhook cannot reward twice.
CREATE TABLE IF NOT EXISTS referrals (
    id TEXT PRIMARY KEY,
    referrer_organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    referred_organization_id TEXT NOT NULL UNIQUE REFERENCES organizations(id) ON DELETE CASCADE,
    code TEXT NOT NULL,
    referred_subscription_id TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'rewarding', 'rewarded', 'void')),
    reward_discount_id TEXT NOT NULL DEFAULT '',
    void_reason TEXT NOT NULL DEFAULT '',
    rewarded_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (referrer_organization_id <> referred_organization_id)
);

CREATE INDEX IF NOT EXISTS idx_referrals_referrer
    ON referrals (referrer_organization_id, created_at DESC);
