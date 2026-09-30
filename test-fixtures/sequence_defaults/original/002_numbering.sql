-- A sequence created after the table whose column default calls it, under a
-- name that sorts before the table's.
CREATE SEQUENCE alpha_seq;
ALTER TABLE zeta_orders ADD COLUMN num bigint NOT NULL DEFAULT nextval('alpha_seq');

CREATE TABLE aaa_items (
    id bigint PRIMARY KEY DEFAULT nextval('public.alpha_seq'::regclass)
);

-- A sequence in another schema, named with its schema.
CREATE SCHEMA billing;
CREATE SEQUENCE billing.invoice_numbers START 1000;
CREATE TABLE abc_invoices (
    id bigint PRIMARY KEY,
    number bigint NOT NULL DEFAULT nextval('billing.invoice_numbers')
);

-- The sequence of another table's serial column.
CREATE TABLE aa_shared (
    id integer PRIMARY KEY DEFAULT nextval('zz_counters_id_seq')
);
