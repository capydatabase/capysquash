CREATE FUNCTION public.fmt(v integer) RETURNS text LANGUAGE sql AS $$ SELECT v::text $$;
CREATE FUNCTION public.fmt(v text) RETURNS text LANGUAGE sql AS $$ SELECT v $$;
COMMENT ON FUNCTION public.fmt(integer) IS 'integer version';
COMMENT ON FUNCTION public.fmt(text) IS 'text version';

CREATE FUNCTION public.label(v bigint) RETURNS text LANGUAGE sql AS $$ SELECT 'n' || v::text $$;
-- Short form: valid while label has one overload.
COMMENT ON FUNCTION public.label IS 'bigint label';

CREATE TABLE public.items (id bigint PRIMARY KEY, name text);
CREATE FUNCTION public.touch() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RETURN NEW; END $$;
CREATE TRIGGER items_touch BEFORE UPDATE ON public.items FOR EACH ROW EXECUTE FUNCTION public.touch();
