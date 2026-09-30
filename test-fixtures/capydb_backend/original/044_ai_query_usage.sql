-- Monthly per-organization AI query counter backing the plans' AI soft cap
-- (Studio "Ask AI" natural-language SQL). One row per organization per UTC
-- calendar month; the increment is an upsert so the first query of a month
-- creates the row. The cap is soft - the service reports soft_cap_reached and
-- never blocks - so this table is accounting, not enforcement.
CREATE TABLE IF NOT EXISTS ai_query_usage (
    organization_id TEXT NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    month DATE NOT NULL,
    used INTEGER NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (organization_id, month)
);
