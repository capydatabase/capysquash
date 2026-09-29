-- Replacing a function keeps its owner and privileges.
CREATE OR REPLACE FUNCTION public.normalize(v integer) RETURNS integer LANGUAGE sql AS $$ SELECT abs(v) + 0 $$;

-- A rename carries the privileges along.
ALTER FUNCTION public.secret_count() RENAME TO hidden_count;
GRANT EXECUTE ON FUNCTION public.hidden_count() TO prt_admin;

ALTER FUNCTION public.normalize(text) OWNER TO prt_admin;
ALTER PROCEDURE public.rotate_keys() OWNER TO prt_admin;

-- Bulk revoke reaches every function in the schema at this point.
REVOKE EXECUTE ON ALL FUNCTIONS IN SCHEMA public FROM prt_app;

-- Created after the bulk revoke: PUBLIC keeps EXECUTE on it.
CREATE FUNCTION public.version_label() RETURNS text LANGUAGE sql AS $$ SELECT 'v1' $$;
