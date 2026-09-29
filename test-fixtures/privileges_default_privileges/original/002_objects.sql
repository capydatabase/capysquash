-- Created after the defaults: each gets them at creation.
CREATE TABLE app.orders (id bigserial PRIMARY KEY, total numeric);
CREATE VIEW app.order_totals AS SELECT sum(total) AS total FROM app.orders;
CREATE FUNCTION app.order_count() RETURNS bigint LANGUAGE sql AS $$ SELECT count(*) FROM app.orders $$;
CREATE TYPE app.order_state AS ENUM ('open', 'closed');
CREATE SCHEMA audit;

-- Explicit grants and revokes on top of the defaults.
REVOKE DELETE ON app.orders FROM pdp_service;
GRANT EXECUTE ON FUNCTION app.order_count() TO pdp_reader;

-- Later default changes do not touch the objects above.
ALTER DEFAULT PRIVILEGES IN SCHEMA app REVOKE SELECT ON TABLES FROM pdp_reader;
CREATE TABLE app.refunds (id bigint PRIMARY KEY);
