-- A check whose first sibling is dropped: the one left keeps its number.
ALTER TABLE members ADD CHECK (id > 0);
ALTER TABLE members ADD CHECK (id < 1000000);
ALTER TABLE members DROP CONSTRAINT members_id_check;

-- A table renamed, and another created under its old name: its
-- constraints and indexes are numbered past those the first table keeps.
CREATE TABLE teams (id bigint PRIMARY KEY, slug text UNIQUE, CHECK (slug <> ''));
ALTER TABLE teams RENAME TO old_teams;
CREATE TABLE teams (id bigint PRIMARY KEY, slug text UNIQUE, CHECK (slug <> ''));

-- A domain renamed, and another created under its old name.
CREATE DOMAIN positive_int AS int CHECK (VALUE > 0);
ALTER DOMAIN positive_int RENAME TO old_positive_int;
CREATE DOMAIN positive_int AS int CHECK (VALUE > 0);

-- A check whose name another table takes explicitly, on a table the
-- baseline creates after that other table: a foreign key added later makes
-- it depend on it.
CREATE TABLE items (id bigint PRIMARY KEY, qty int CHECK (qty > 0));
CREATE TABLE orders (id bigint PRIMARY KEY, CONSTRAINT items_qty_check CHECK (id > 0));
ALTER TABLE items ADD COLUMN order_id bigint REFERENCES orders (id);
