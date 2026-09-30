CREATE TABLE parents (
    id bigint PRIMARY KEY,
    code text NOT NULL,
    region text NOT NULL
);
CREATE UNIQUE INDEX parents_code_idx ON parents (code);
CREATE UNIQUE INDEX parents_region_code_idx ON parents (region, code);

-- A foreign key written in CREATE TABLE, on a column only a unique index
-- makes unique.
CREATE TABLE children (
    id bigint PRIMARY KEY,
    parent_code text REFERENCES parents (code)
);

-- A table referencing itself through a unique index.
CREATE TABLE categories (
    id bigint PRIMARY KEY,
    slug text NOT NULL,
    parent_slug text
);
CREATE UNIQUE INDEX categories_slug_idx ON categories (slug);
ALTER TABLE categories ADD CONSTRAINT categories_parent_slug_fkey
    FOREIGN KEY (parent_slug) REFERENCES categories (slug);

-- A unique index in another schema.
CREATE SCHEMA catalog;
CREATE TABLE catalog.skus (id bigint PRIMARY KEY, sku text NOT NULL);
CREATE UNIQUE INDEX skus_sku_idx ON catalog.skus (sku);
