-- Replace clusters with instances: the tables that referenced clusters stay,
-- and projects and instances end up referencing each other.
CREATE TABLE instances (
    id text PRIMARY KEY,
    project_id text NOT NULL
);

ALTER TABLE projects DROP CONSTRAINT IF EXISTS projects_cluster_id_fkey;
ALTER TABLE projects DROP COLUMN IF EXISTS cluster_id;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS primary_instance_id text;
ALTER TABLE projects
    ADD CONSTRAINT projects_primary_instance_fkey
    FOREIGN KEY (primary_instance_id) REFERENCES instances (id) ON DELETE SET NULL;

ALTER TABLE instances
    ADD CONSTRAINT instances_project_fkey
    FOREIGN KEY (project_id) REFERENCES projects (id) ON DELETE CASCADE;

ALTER TABLE jobs DROP COLUMN IF EXISTS cluster_id;

DROP TABLE IF EXISTS clusters CASCADE;
