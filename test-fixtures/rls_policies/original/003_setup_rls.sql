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
