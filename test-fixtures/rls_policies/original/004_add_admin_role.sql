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
