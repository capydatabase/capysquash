ALTER TABLE clusters ADD COLUMN IF NOT EXISTS max_databases integer NOT NULL DEFAULT 64;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'clusters_max_databases_check') THEN
    ALTER TABLE clusters ADD CONSTRAINT clusters_max_databases_check CHECK (max_databases > 0);
  END IF;
END $$;

ALTER TABLE projects ADD COLUMN IF NOT EXISTS max_connections integer NOT NULL DEFAULT 15;

DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'projects_max_connections_check') THEN
    ALTER TABLE projects ADD CONSTRAINT projects_max_connections_check CHECK (max_connections > 0);
  END IF;
END $$;

ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS project_id text REFERENCES projects (id) ON DELETE CASCADE;
