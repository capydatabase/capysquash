ALTER TYPE public.mood RENAME VALUE 'sad' TO 'gloomy';
ALTER TYPE public.mood ADD VALUE 'calm' AFTER 'happy';
ALTER TYPE public.mood RENAME TO feeling;

ALTER DOMAIN public.score RENAME TO points;

ALTER TYPE public.pair RENAME ATTRIBUTE x TO first;
ALTER TYPE public.pair RENAME TO coordinate;

CREATE TABLE public.readings (
    id bigint PRIMARY KEY,
    feeling public.feeling NOT NULL DEFAULT 'gloomy',
    at public.coordinate,
    total public.points
);

CREATE FUNCTION public.first_of(c public.coordinate) RETURNS integer
    LANGUAGE sql IMMUTABLE
    RETURN (c).first;
