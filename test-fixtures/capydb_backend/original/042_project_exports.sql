-- Customer-downloadable logical exports: pg_dump custom-format artifacts
-- uploaded to object storage by capydb-export-db.sh. Rows are inserted only
-- when the export job succeeds; the retention sweep deletes the object and
-- flips the row to 'expired' once expires_at passes.
CREATE TABLE IF NOT EXISTS project_exports (
    id TEXT PRIMARY KEY,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    database_name TEXT NOT NULL,
    object_key TEXT NOT NULL UNIQUE,
    state TEXT NOT NULL CHECK (state IN ('completed', 'expired')),
    size_bytes BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_project_exports_project_created
    ON project_exports (project_id, created_at DESC);

-- The retention sweep scans for completed exports past their expiry.
CREATE INDEX IF NOT EXISTS idx_project_exports_expiry
    ON project_exports (expires_at)
    WHERE state = 'completed';
