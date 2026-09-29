-- Dropped (a view) and dropped and recreated (a table): the grants go with
-- the objects they were made on.
DROP VIEW public.session_tokens;
DROP TABLE public.sessions;
CREATE TABLE public.sessions (id bigint PRIMARY KEY, token text, expires_at timestamptz);
GRANT INSERT ON public.sessions TO pdr_writer;

-- Renames keep privileges on the renamed table and column.
ALTER TABLE public.profiles RENAME TO user_profiles;
ALTER TABLE public.user_profiles RENAME COLUMN display_name TO public_name;

-- The schema default privileges reach a table created now.
CREATE TABLE staging.batches (id bigint PRIMARY KEY);

-- Moved to another schema with its privileges.
ALTER TABLE staging.imports SET SCHEMA public;
