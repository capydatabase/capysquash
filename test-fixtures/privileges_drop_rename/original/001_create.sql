CREATE ROLE pdr_reader;
CREATE ROLE pdr_writer;

CREATE TABLE public.sessions (id bigint PRIMARY KEY, token text);
GRANT SELECT, DELETE ON public.sessions TO pdr_writer;
CREATE VIEW public.session_tokens AS SELECT token FROM public.sessions;
GRANT SELECT ON public.session_tokens TO pdr_reader;

CREATE TABLE public.profiles (id bigint PRIMARY KEY, display_name text, bio text);
GRANT SELECT (display_name) ON public.profiles TO pdr_reader;
GRANT UPDATE (bio) ON public.profiles TO pdr_writer;

CREATE SCHEMA staging;
CREATE TABLE staging.imports (id bigint PRIMARY KEY);
GRANT USAGE ON SCHEMA staging TO pdr_reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA staging GRANT SELECT ON TABLES TO pdr_reader;

-- Bulk grant: reaches the tables that exist now, not later ones.
GRANT SELECT ON ALL TABLES IN SCHEMA public TO pdr_reader;
