CREATE ROLE dsr_owner;

CREATE SCHEMA scratch;
CREATE TABLE scratch.work (id integer PRIMARY KEY, note text);
CREATE INDEX work_note ON scratch.work (note);
CREATE VIEW scratch.work_ids AS SELECT id FROM scratch.work;
CREATE TYPE scratch.kind AS ENUM ('a', 'b');
CREATE FUNCTION scratch.one() RETURNS integer LANGUAGE sql RETURN 1;
COMMENT ON TABLE scratch.work IS 'Scratch space';
GRANT USAGE ON SCHEMA scratch TO dsr_owner;
GRANT SELECT ON scratch.work TO dsr_owner;

CREATE SCHEMA IF NOT EXISTS reports AUTHORIZATION dsr_owner;

CREATE SCHEMA emptied;
CREATE TABLE emptied.leftovers (id integer);
