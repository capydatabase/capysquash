CREATE ROLE prt_app;
CREATE ROLE prt_admin;

-- Overloads get different privileges: identity is name plus argument types.
CREATE FUNCTION public.normalize(v integer) RETURNS integer LANGUAGE sql AS $$ SELECT abs(v) $$;
CREATE FUNCTION public.normalize(v text) RETURNS text LANGUAGE sql AS $$ SELECT lower(v) $$;
REVOKE EXECUTE ON FUNCTION public.normalize(int4) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.normalize(integer) TO prt_app;
GRANT EXECUTE ON FUNCTION public.normalize(text) TO prt_admin WITH GRANT OPTION;

-- Named without arguments while it has a single overload.
CREATE FUNCTION public.secret_count() RETURNS bigint LANGUAGE sql AS $$ SELECT 42::bigint $$;
REVOKE ALL ON FUNCTION public.secret_count FROM PUBLIC;

CREATE PROCEDURE public.rotate_keys() LANGUAGE sql AS $$ SELECT 1 $$;
REVOKE EXECUTE ON PROCEDURE public.rotate_keys() FROM PUBLIC;
GRANT EXECUTE ON PROCEDURE public.rotate_keys() TO prt_admin;
