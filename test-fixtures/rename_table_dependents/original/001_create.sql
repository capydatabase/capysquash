CREATE ROLE rtd_reader;

CREATE TABLE public.profiles (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    handle text NOT NULL UNIQUE,
    display_name text,
    bio text CHECK (length(bio) < 500)
);
CREATE INDEX profiles_display_name ON public.profiles (display_name);
CREATE INDEX ON public.profiles (lower(handle));

CREATE TABLE public.posts (
    id bigserial PRIMARY KEY,
    author_id bigint NOT NULL REFERENCES public.profiles (id),
    body text
);
CREATE INDEX ON public.posts (author_id);

CREATE VIEW public.profile_names AS SELECT id, display_name FROM public.profiles;

ALTER TABLE public.profiles ENABLE ROW LEVEL SECURITY;
CREATE POLICY profiles_visible ON public.profiles FOR SELECT USING (display_name IS NOT NULL);

CREATE FUNCTION public.touch() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RETURN NEW;
END
$$;
CREATE TRIGGER profiles_touch BEFORE UPDATE OF display_name ON public.profiles
    FOR EACH ROW WHEN (NEW.display_name IS DISTINCT FROM OLD.display_name)
    EXECUTE FUNCTION public.touch();

GRANT SELECT, UPDATE (display_name) ON public.profiles TO rtd_reader;
GRANT SELECT ON public.profile_names TO rtd_reader;
COMMENT ON COLUMN public.profiles.display_name IS 'Shown name';

INSERT INTO public.profiles (handle, display_name) VALUES ('capy', 'Capy');
