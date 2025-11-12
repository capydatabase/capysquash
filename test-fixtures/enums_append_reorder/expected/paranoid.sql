-- Create users table with status enum
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    username VARCHAR(50) UNIQUE NOT NULL,
    status user_status,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Create user_status enum type
CREATE TYPE user_status AS ENUM ('pending', 'verified', 'suspended');

-- Add 'active' status to user_status enum
ALTER TYPE user_status ADD VALUE 'active';

-- Add 'inactive' status to user_status enum
ALTER TYPE user_status ADD VALUE 'inactive';

-- This simulates a reorder scenario that should not be merged in conservative mode
-- The value 'banned' is being added in a position that breaks the append-only assumption
ALTER TYPE user_status ADD VALUE 'banned' AFTER 'suspended';
