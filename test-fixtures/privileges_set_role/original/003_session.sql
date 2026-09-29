SET SESSION AUTHORIZATION psr_owner;
CREATE SEQUENCE psr_app.invoice_numbers;
RESET SESSION AUTHORIZATION;

BEGIN;
SET LOCAL ROLE psr_owner;
CREATE TYPE psr_app.money_amount AS (amount numeric, currency text);
COMMIT;

CREATE TABLE psr_app.settings (key text PRIMARY KEY, value text);
