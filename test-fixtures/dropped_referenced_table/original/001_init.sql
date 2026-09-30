CREATE TABLE clusters (
    id text PRIMARY KEY,
    name text NOT NULL
);

CREATE TABLE api_keys (
    id text PRIMARY KEY
);

CREATE TABLE projects (
    id text PRIMARY KEY,
    cluster_id text NOT NULL REFERENCES clusters (id) ON DELETE RESTRICT,
    name text NOT NULL
);

CREATE TABLE jobs (
    id text PRIMARY KEY,
    cluster_id text NOT NULL REFERENCES clusters (id)
);
