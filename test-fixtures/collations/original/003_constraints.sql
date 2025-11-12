-- Add collation-specific constraint
ALTER TABLE users ADD CONSTRAINT check_username_length
    CHECK (length(username COLLATE "C") > 3);

-- Create collation-aware unique constraint
ALTER TABLE users ADD CONSTRAINT unique_username_case_insensitive
    UNIQUE (lower(username COLLATE "en_US.utf8"));

-- Add more collation examples
CREATE TABLE categories (
    id SERIAL PRIMARY KEY,
    name VARCHAR(50) COLLATE "en_US.utf8",
    slug VARCHAR(50) COLLATE "C" UNIQUE
);

-- Collation-aware index
CREATE INDEX idx_categories_name ON categories (name COLLATE "en_US.utf8");
