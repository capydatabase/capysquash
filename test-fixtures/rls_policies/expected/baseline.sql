-- Create organizations table
CREATE TABLE organizations (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    owner_id INTEGER,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Create users table with organization membership
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email VARCHAR(255) UNIQUE NOT NULL,
    organization_id INTEGER REFERENCES organizations(id),
    role VARCHAR(20) DEFAULT 'member',
    created_at TIMESTAMP DEFAULT NOW()
);

-- Enable RLS on organizations table
ALTER TABLE organizations ENABLE ROW LEVEL SECURITY;

-- Create policy for organization owners
CREATE POLICY org_owner_policy ON organizations
    FOR ALL USING (owner_id = current_user_id());

-- Enable RLS on users table
ALTER TABLE users ENABLE ROW LEVEL SECURITY;

-- Create policy for users to see their own organization members
CREATE POLICY user_org_policy ON users
    FOR SELECT USING (organization_id = current_user_org_id());

-- Create admin role
CREATE ROLE admin;

-- Grant admin role to specific users (simplified)
GRANT admin TO user_1, user_2;

-- Create admin policy for organizations
CREATE POLICY admin_org_policy ON organizations
    FOR ALL USING (current_user_role() = 'admin');

-- Create admin policy for users
CREATE POLICY admin_user_policy ON users
    FOR ALL USING (current_user_role() = 'admin');
