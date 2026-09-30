CREATE TABLE accounts (id bigint PRIMARY KEY, email text, handle text, score int);
CREATE TABLE members (id bigint PRIMARY KEY, account_id bigint REFERENCES accounts (id), team text);

-- Unnamed indexes on two tables, and the same unnamed index twice
-- (members_account_id_idx, members_account_id_idx1).
CREATE INDEX ON accounts (email);
CREATE INDEX ON members (account_id);
CREATE INDEX ON members (account_id);

-- The same unnamed foreign key, unique constraint and check twice.
ALTER TABLE members ADD FOREIGN KEY (account_id) REFERENCES accounts (id);
ALTER TABLE accounts ADD UNIQUE (email);
ALTER TABLE accounts ADD UNIQUE (email);
ALTER TABLE accounts ADD CHECK (score > 0);
ALTER TABLE accounts ADD CHECK (score < 1000);

-- A check named after its table alone (no column, then two columns).
ALTER TABLE accounts ADD CHECK (true);
ALTER TABLE accounts ADD CHECK (email <> handle);

-- One statement adding a check, a unique constraint and a foreign key,
-- which PostgreSQL names in that order whatever the order written.
ALTER TABLE members
    ADD FOREIGN KEY (account_id) REFERENCES accounts (id),
    ADD UNIQUE (team),
    ADD CHECK (team <> '');
