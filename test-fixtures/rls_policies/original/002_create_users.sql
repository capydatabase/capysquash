-- Create users table with organization membership
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    organization_id INTEGER REFERENCES organizations(id),
    role VARCHAR(20) DEFAULT 'member',
    created_at TIMESTAMP DEFAULT NOW()
);
