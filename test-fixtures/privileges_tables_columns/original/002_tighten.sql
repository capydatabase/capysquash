-- REVOKE after GRANT: PUBLIC loses what it was given.
REVOKE SELECT ON public.accounts FROM PUBLIC;

-- The writer keeps UPDATE but can no longer pass INSERT on.
REVOKE GRANT OPTION FOR INSERT ON public.accounts FROM ptc_writer;
REVOKE UPDATE ON public.accounts FROM ptc_writer;

-- A table-level REVOKE also takes the privilege off every column.
REVOKE UPDATE ON public.accounts FROM ptc_reader;

-- ALL then one less: on PostgreSQL 17+ ALL includes MAINTAIN, before it does not.
GRANT ALL ON public.account_totals TO ptc_reader;
REVOKE DELETE, TRUNCATE ON public.account_totals FROM ptc_reader;
REVOKE ALL ON public.account_totals FROM ptc_writer;

-- Ownership moves after the grants: PostgreSQL rewrites the grantor of every
-- privilege above to the new owner.
ALTER TABLE public.accounts OWNER TO ptc_owner;
ALTER VIEW public.account_emails OWNER TO ptc_owner;
