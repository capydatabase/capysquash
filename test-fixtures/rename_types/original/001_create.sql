CREATE ROLE rt_user;

CREATE TYPE public.mood AS ENUM ('happy', 'sad');
CREATE DOMAIN public.score AS integer CHECK (VALUE >= 0);
CREATE TYPE public.pair AS (x integer, y integer);

CREATE TABLE public.entries (
    id bigint PRIMARY KEY,
    mood public.mood NOT NULL DEFAULT 'sad',
    score public.score,
    point public.pair,
    CHECK (mood <> 'sad' OR score IS NOT NULL)
);

CREATE FUNCTION public.describe(m public.mood, p public.pair) RETURNS text
    LANGUAGE sql IMMUTABLE
    RETURN m::text;

GRANT USAGE ON TYPE public.mood TO rt_user;
REVOKE EXECUTE ON FUNCTION public.describe(public.mood, public.pair) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.describe(public.mood, public.pair) TO rt_user;
