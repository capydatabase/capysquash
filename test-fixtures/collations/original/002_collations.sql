-- Add new table with collation
CREATE TABLE comments (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    post_id INTEGER REFERENCES posts(id),
    content TEXT COLLATE "en_US.utf8",
    created_at TIMESTAMP DEFAULT NOW()
);

-- Add collation-aware index
CREATE INDEX idx_comments_content ON comments (content COLLATE "en_US.utf8");

-- Modify users table collation
ALTER TABLE users ADD COLUMN display_name VARCHAR(100) COLLATE "C";
