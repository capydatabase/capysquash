-- Add collation-specific constraint
ALTER TABLE users ADD CONSTRAINT check_username_length
    CHECK (length(username COLLATE "C") > 3);

-- PostgreSQL expression uniqueness is represented by a unique index; UNIQUE
-- constraints accept columns, not expressions.
CREATE UNIQUE INDEX unique_username_case_insensitive
    ON users (lower(username COLLATE "en_US.utf8"));

-- Add more collation examples
CREATE TABLE categories (
    id SERIAL PRIMARY KEY,
    name VARCHAR(50) COLLATE "en_US.utf8",
    slug VARCHAR(50) COLLATE "C" UNIQUE
);

-- Collation-aware index
CREATE INDEX idx_categories_name ON categories (name COLLATE "en_US.utf8");
