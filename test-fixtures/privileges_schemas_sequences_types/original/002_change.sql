ALTER SCHEMA reporting OWNER TO psst_owner;
REVOKE GRANT OPTION FOR CREATE ON SCHEMA reporting FROM psst_app;

ALTER TYPE public.invoice_state OWNER TO psst_owner;
ALTER DOMAIN public.money_amount OWNER TO psst_owner;
ALTER SEQUENCE public.invoice_number OWNER TO psst_owner;
REVOKE UPDATE ON SEQUENCE public.invoice_number FROM psst_owner;
