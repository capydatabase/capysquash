-- Add posts table (should be consolidated)
CREATE TABLE posts (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    title VARCHAR(200) NOT NULL,
    content TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Add posts index (should be consolidated)
CREATE INDEX idx_posts_user_id ON posts (user_id);

-- pgsquash: no-merge
-- This index should NOT be consolidated with others
CREATE UNIQUE INDEX idx_posts_title ON posts (title);
