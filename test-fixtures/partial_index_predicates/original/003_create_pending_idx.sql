-- Create partial index on pending users (compact formatting)
CREATE INDEX idx_users_pending ON users(email) WHERE status='pending';
