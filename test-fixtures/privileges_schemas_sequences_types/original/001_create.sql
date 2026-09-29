CREATE ROLE psst_app;
CREATE ROLE psst_owner;

-- Pre-existing object: the public schema.
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO psst_app;

CREATE SCHEMA billing AUTHORIZATION psst_owner;
CREATE SCHEMA reporting;
GRANT USAGE ON SCHEMA reporting TO psst_app;
GRANT CREATE ON SCHEMA reporting TO psst_app WITH GRANT OPTION;

CREATE SEQUENCE public.invoice_number;
GRANT USAGE ON SEQUENCE public.invoice_number TO psst_app;
GRANT SELECT, UPDATE ON public.invoice_number TO psst_owner;

CREATE TABLE public.invoices (id serial PRIMARY KEY, total numeric);
GRANT SELECT, USAGE ON ALL SEQUENCES IN SCHEMA public TO psst_app;

CREATE TYPE public.invoice_state AS ENUM ('draft', 'sent', 'paid');
REVOKE USAGE ON TYPE public.invoice_state FROM PUBLIC;
GRANT USAGE ON TYPE public.invoice_state TO psst_app;

CREATE DOMAIN public.money_amount AS numeric CHECK (VALUE >= 0);
REVOKE ALL ON DOMAIN public.money_amount FROM PUBLIC;

CREATE TYPE public.price_range AS RANGE (subtype = numeric);
GRANT USAGE ON TYPE public.price_range TO psst_app;
