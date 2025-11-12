-- Create users table with status enum
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    status user_status,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Create user_status enum type (merged from all migrations)
-- Note: In real implementation, the order would be: pending, verified, suspended, banned, active, inactive
CREATE TYPE user_status AS ENUM ('pending', 'verified', 'suspended', 'banned', 'active', 'inactive');
