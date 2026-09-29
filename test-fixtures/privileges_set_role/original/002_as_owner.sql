SET ROLE psr_owner;

CREATE TABLE psr_app.accounts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email text NOT NULL
);
CREATE VIEW psr_app.account_emails AS SELECT email FROM psr_app.accounts;
CREATE FUNCTION psr_app.account_count() RETURNS bigint
    LANGUAGE sql STABLE SECURITY DEFINER
    AS $$ SELECT count(*) FROM psr_app.accounts $$;
CREATE TYPE psr_app.account_state AS ENUM ('open', 'closed');

-- Default privileges of the current role, psr_owner.
ALTER DEFAULT PRIVILEGES IN SCHEMA psr_app GRANT SELECT ON TABLES TO psr_reader;
CREATE TABLE psr_app.events (
    id bigint PRIMARY KEY,
    account_id bigint NOT NULL REFERENCES psr_app.accounts (id),
    state psr_app.account_state NOT NULL
);

GRANT USAGE ON SCHEMA psr_app TO psr_reader, psr_delegate;
GRANT SELECT ON psr_app.accounts TO psr_delegate WITH GRANT OPTION;
REVOKE EXECUTE ON FUNCTION psr_app.account_count() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION psr_app.account_count() TO psr_reader;

RESET ROLE;

-- The delegate passes on what it may grant; it is recorded as the grantor.
SET ROLE psr_delegate;
GRANT SELECT ON psr_app.accounts TO psr_reader;
RESET ROLE;

-- Back as the migrating role: this table is its own.
CREATE TABLE psr_app.audit (id bigint PRIMARY KEY, note text);
