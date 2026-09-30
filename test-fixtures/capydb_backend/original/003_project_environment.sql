ALTER TABLE projects
  ADD COLUMN IF NOT EXISTS environment TEXT NOT NULL DEFAULT 'production';

UPDATE projects
SET environment = 'production'
WHERE environment IS NULL OR environment = '';

DO $$
BEGIN
  IF NOT EXISTS (
    SELECT 1
    FROM pg_constraint
    WHERE conname = 'projects_environment_check'
  ) THEN
    ALTER TABLE projects
      ADD CONSTRAINT projects_environment_check
      CHECK (environment IN ('production', 'non_production'));
  END IF;
END $$;
