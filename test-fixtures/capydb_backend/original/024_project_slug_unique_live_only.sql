-- A deleted project must release its slug: org+slug uniqueness applies to live rows
-- only, otherwise a project name can never be reused after deletion (soft-deleted
-- rows kept blocking the unique index forever).
DROP INDEX IF EXISTS projects_org_slug_idx;
CREATE UNIQUE INDEX IF NOT EXISTS projects_org_slug_idx
    ON projects (organization_id, slug)
    WHERE state <> 'deleted';
