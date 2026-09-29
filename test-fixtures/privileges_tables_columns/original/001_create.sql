CREATE ROLE ptc_reader;
CREATE ROLE ptc_writer;
CREATE ROLE ptc_owner;

CREATE TABLE public.accounts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email text NOT NULL,
    password_hash text NOT NULL,
    balance numeric NOT NULL DEFAULT 0
);

-- Readable by everyone at first; tightened below.
GRANT SELECT ON public.accounts TO PUBLIC;
GRANT SELECT, INSERT, UPDATE ON public.accounts TO ptc_writer WITH GRANT OPTION;

-- Column-level: the reader sees two columns and may update one.
GRANT SELECT (id, email), UPDATE (email) ON public.accounts TO ptc_reader;
GRANT REFERENCES (id) ON public.accounts TO ptc_reader WITH GRANT OPTION;

CREATE VIEW public.account_emails AS SELECT id, email FROM public.accounts;
GRANT SELECT ON public.account_emails TO ptc_reader;

CREATE MATERIALIZED VIEW public.account_totals AS SELECT count(*) AS n, sum(balance) AS total FROM public.accounts;
GRANT ALL ON public.account_totals TO ptc_writer;
