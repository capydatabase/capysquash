CREATE ROLE ppe_reader;
CREATE ROLE ppe_app;

REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE USAGE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO ppe_reader;
GRANT USAGE, CREATE ON SCHEMA public TO ppe_app;

CREATE TABLE public.notes (id bigint PRIMARY KEY, body text NOT NULL);
GRANT SELECT ON public.notes TO ppe_reader;
