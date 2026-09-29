-- Create admin role and the login roles that act as admins
CREATE ROLE rls_admin;
CREATE ROLE rls_user_1 LOGIN;
CREATE ROLE rls_user_2 LOGIN;

-- Grant admin role to specific users (simplified)
GRANT rls_admin TO rls_user_1, rls_user_2;

-- Create admin policy for organizations
CREATE POLICY admin_org_policy ON organizations
    FOR ALL TO rls_admin USING (current_user_role() = 'admin');

-- Create admin policy for users
CREATE POLICY admin_user_policy ON users
    FOR ALL TO rls_admin USING (current_user_role() = 'admin');
