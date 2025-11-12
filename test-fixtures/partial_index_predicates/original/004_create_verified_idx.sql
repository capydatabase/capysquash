-- Create partial index on verified users (mixed case)
CREATE INDEX idx_users_verified ON users (email) WHERE status = 'verified';
