-- Add some users
-- pgsquash:ignore
INSERT INTO users (username, email) VALUES
    ('alice', 'alice@example.com'),
    ('bob', 'bob@example.com');

-- Add some posts (this should be separated as data operation)
INSERT INTO posts (user_id, title, content) VALUES
    (1, 'First Post', 'This is my first post'),
    (2, 'Second Post', 'This is my second post');
