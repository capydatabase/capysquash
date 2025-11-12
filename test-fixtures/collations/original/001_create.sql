-- Create table with collation
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) COLLATE "en_US.utf8",
    email VARCHAR(100) COLLATE "C",
    created_at TIMESTAMP DEFAULT NOW()
);

-- Add index with collation
CREATE INDEX idx_users_username ON users (username COLLATE "en_US.utf8");

-- Create table with different collations
CREATE TABLE posts (
    id SERIAL PRIMARY KEY,
    title VARCHAR(200) COLLATE "C",
    content TEXT COLLATE "en_US.utf8",
    published_at TIMESTAMP DEFAULT NOW()
);

-- Add collation-specific index
CREATE INDEX idx_posts_title ON posts (title COLLATE "C");
