-- Storage-overage metering: the per-organization, per-billing-period ledger of
-- what has been reported to the billing provider.
--
-- The billed unit is the period's PEAK over-cap storage in whole gigabytes: the
-- frontend's meter emitter sends a usage event whenever an organization's
-- over-cap storage exceeds what this row already records for the period, and
-- the provider's meter aggregates those events with `max`. A period is keyed by
-- the subscription's current period end (organizations.billing_period_end), the
-- only period boundary billing sync stores.
--
-- reported_gigabytes only ever rises (the upsert takes GREATEST), and a row is
-- written only after the provider accepted the event, so a retried report never
-- raises the bill: the event id is deterministic per organization, period and
-- quantity, the provider skips a duplicate id, and max-aggregation makes a
-- repeated quantity a no-op even if it did not.
CREATE TABLE IF NOT EXISTS storage_overage_reports (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    period_end TIMESTAMPTZ NOT NULL,
    reported_gigabytes BIGINT NOT NULL CHECK (reported_gigabytes > 0),
    first_reported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_reported_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, period_end)
);
