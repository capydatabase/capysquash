ALTER TABLE public.profiles RENAME TO accounts;
ALTER TABLE public.accounts RENAME COLUMN display_name TO public_name;
ALTER TABLE public.posts RENAME TO articles;
ALTER TABLE public.articles RENAME COLUMN author_id TO account_id;

CREATE TABLE public.follows (
    follower_id bigint REFERENCES public.accounts (id),
    followed_id bigint REFERENCES public.accounts (id),
    PRIMARY KEY (follower_id, followed_id)
);
CREATE INDEX ON public.accounts (public_name, bio);

GRANT SELECT ON public.follows TO rtd_reader;
GRANT SELECT ON public.articles TO rtd_reader;
