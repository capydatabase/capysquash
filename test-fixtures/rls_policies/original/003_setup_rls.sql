-- Session helpers the policies call: the application sets app.user_id,
-- app.org_id and app.role for each connection.
CREATE FUNCTION current_user_id() RETURNS integer
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.user_id', true), '')::integer $$;

CREATE FUNCTION current_user_org_id() RETURNS integer
    LANGUAGE sql STABLE
    AS $$ SELECT nullif(current_setting('app.org_id', true), '')::integer $$;

CREATE FUNCTION current_user_role() RETURNS text
    LANGUAGE sql STABLE
    AS $$ SELECT current_setting('app.role', true) $$;

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
