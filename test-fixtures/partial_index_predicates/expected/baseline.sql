-- Create users table
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Create partial index on active users (normalized spacing)
CREATE INDEX idx_users_active ON users (email) WHERE status = 'active';

-- Create partial index on pending users (normalized spacing)
CREATE INDEX idx_users_pending ON users (email) WHERE status = 'pending';

-- Create partial index on verified users (normalized spacing)
CREATE INDEX idx_users_verified ON users (email) WHERE status = 'verified';
