CREATE OR REPLACE FUNCTION public.fmt(v int4) RETURNS text LANGUAGE sql AS $$ SELECT 'n=' || v::text $$;
DROP FUNCTION public.fmt(text);
CREATE FUNCTION public.fmt(v boolean) RETURNS text LANGUAGE sql AS $$ SELECT v::text $$;

-- A second overload makes the short form ambiguous from here on.
CREATE FUNCTION public.label(v text) RETURNS text LANGUAGE sql AS $$ SELECT 't' || v $$;
