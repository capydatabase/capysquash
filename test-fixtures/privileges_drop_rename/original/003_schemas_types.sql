-- A renamed schema keeps its privileges, its tables' privileges and its
-- default privileges, which reach a table created after the rename.
CREATE SCHEMA pdr_archive;
CREATE TABLE pdr_archive.entries (id bigint PRIMARY KEY);
GRANT USAGE ON SCHEMA pdr_archive TO pdr_reader;
GRANT SELECT ON pdr_archive.entries TO pdr_reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA pdr_archive GRANT SELECT ON TABLES TO pdr_reader;
ALTER SCHEMA pdr_archive RENAME TO pdr_history;
CREATE TABLE pdr_history.more_entries (id bigint PRIMARY KEY);

-- Renamed types keep their privileges.
CREATE TYPE public.pdr_state AS ENUM ('on', 'off');
REVOKE USAGE ON TYPE public.pdr_state FROM PUBLIC;
GRANT USAGE ON TYPE public.pdr_state TO pdr_writer;
ALTER TYPE public.pdr_state RENAME TO pdr_status;
CREATE DOMAIN public.pdr_amount AS numeric CHECK (VALUE >= 0);
GRANT USAGE ON DOMAIN public.pdr_amount TO pdr_reader;
ALTER DOMAIN public.pdr_amount RENAME TO pdr_money;

-- A dropped schema takes its privileges with it: the schema created again
-- under the same name starts from the defaults.
CREATE SCHEMA pdr_temp;
GRANT USAGE ON SCHEMA pdr_temp TO pdr_writer;
CREATE TABLE pdr_temp.t (id integer);
GRANT SELECT ON pdr_temp.t TO pdr_writer;
DROP SCHEMA pdr_temp CASCADE;
CREATE SCHEMA pdr_temp;
