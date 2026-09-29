CREATE ROLE rs_reader;

CREATE SCHEMA staging;
COMMENT ON SCHEMA staging IS 'Raw imports';

CREATE TABLE staging.imports (
    id bigint PRIMARY KEY,
    label text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX imports_label ON staging.imports (label);
COMMENT ON TABLE staging.imports IS 'One row per imported file';

CREATE TYPE staging.import_state AS ENUM ('queued', 'done');
CREATE SEQUENCE staging.batch_numbers;

CREATE FUNCTION staging.state_label(s staging.import_state) RETURNS text
    LANGUAGE sql IMMUTABLE
    RETURN s::text;

CREATE VIEW staging.recent_imports AS
    SELECT id, label FROM staging.imports WHERE created_at > now() - interval '1 day';

GRANT USAGE ON SCHEMA staging TO rs_reader;
GRANT SELECT ON staging.imports TO rs_reader;
GRANT EXECUTE ON FUNCTION staging.state_label(staging.import_state) TO rs_reader;
