-- Create partial index on active users (with extra spacing)
CREATE INDEX idx_users_active    ON users (email) WHERE status    =    'active';
