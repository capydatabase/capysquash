-- =====================================================================
-- SUPABASE COMPATIBILITY LAYER FOR PURE POSTGRESQL
-- This script creates the necessary schemas and functions to make
-- Supabase-dependent migrations work on pure PostgreSQL instances
-- =====================================================================

-- Create required schemas
CREATE SCHEMA IF NOT EXISTS auth;
CREATE SCHEMA IF NOT EXISTS storage;
CREATE SCHEMA IF NOT EXISTS realtime;
CREATE SCHEMA IF NOT EXISTS extensions;

-- =====================================================================
-- AUTH SCHEMA FUNCTIONS
-- =====================================================================

-- JWT function that returns a realistic JWT structure
CREATE OR REPLACE FUNCTION auth.jwt()
RETURNS JSONB
LANGUAGE SQL
STABLE
SECURITY DEFINER
AS $$
  SELECT COALESCE(
    current_setting('request.jwt.claims', true)::jsonb,
    jsonb_build_object(
      'sub', COALESCE(current_setting('auth.user_id', true), 'test-user-' || extract(epoch from now())::bigint::text),
      'email', COALESCE(current_setting('auth.user_email', true), 'test@example.com'),
      'role', COALESCE(current_setting('auth.user_role', true), 'authenticated'),
      'aud', 'authenticated',
      'iss', 'supabase',
      'iat', extract(epoch from now())::bigint,
      'exp', extract(epoch from now() + interval '1 hour')::bigint,
      'o', jsonb_build_object(
        'id', COALESCE(current_setting('auth.org_id', true), 'test-org-' || extract(epoch from now())::bigint::text),
        'role', COALESCE(current_setting('auth.org_role', true), 'member'),
        'name', COALESCE(current_setting('auth.org_name', true), 'Test Organization')
      ),
      'v', 2,
      'publicMetadata', jsonb_build_object('role', 'user'),
      'privateMetadata', jsonb_build_object(),
      'user_metadata', jsonb_build_object(),
      'app_metadata', jsonb_build_object(),
      'fva', jsonb_build_array(true, 1)
    )
  );
$$;

-- User ID helper
CREATE OR REPLACE FUNCTION auth.uid()
RETURNS TEXT
LANGUAGE SQL
STABLE
SECURITY DEFINER
AS $$
  SELECT auth.jwt() ->> 'sub';
$$;

-- Role helper
CREATE OR REPLACE FUNCTION auth.role()
RETURNS TEXT
LANGUAGE SQL
STABLE
SECURITY DEFINER
AS $$
  SELECT auth.jwt() ->> 'role';
$$;

-- Email helper
CREATE OR REPLACE FUNCTION auth.email()
RETURNS TEXT
LANGUAGE SQL
STABLE
SECURITY DEFINER
AS $$
  SELECT auth.jwt() ->> 'email';
$$;

-- =====================================================================
-- STORAGE SCHEMA FUNCTIONS
-- =====================================================================

-- Folder name extraction function
CREATE OR REPLACE FUNCTION storage.foldername(name TEXT)
RETURNS TEXT[]
LANGUAGE SQL
IMMUTABLE
AS $$
  SELECT CASE
    WHEN name IS NULL THEN ARRAY[]::TEXT[]
    ELSE string_to_array(trim(both '/' from name), '/')
  END;
$$;

-- Filename extraction function
CREATE OR REPLACE FUNCTION storage.filename(name TEXT)
RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
AS $$
  SELECT CASE
    WHEN name IS NULL OR name = '' THEN NULL
    WHEN position('/' in reverse(name)) = 0 THEN name
    ELSE substring(name from position('/' in reverse(name)) + 1)
  END;
$$;

-- Extension extraction function
CREATE OR REPLACE FUNCTION storage.extension(name TEXT)
RETURNS TEXT
LANGUAGE SQL
IMMUTABLE
AS $$
  SELECT CASE
    WHEN name IS NULL OR position('.' in reverse(name)) = 0 THEN NULL
    ELSE lower(substring(reverse(name) from 1 for position('.' in reverse(name)) - 1))
  END;
$$;

-- =====================================================================
-- REALTIME SCHEMA SETUP
-- =====================================================================

-- Create publication if it doesn't exist
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = 'supabase_realtime') THEN
        CREATE PUBLICATION supabase_realtime FOR ALL TABLES;
    END IF;
END $$;

-- =====================================================================
-- EXTENSIONS SCHEMA SETUP
-- =====================================================================

-- Mock extension management for Supabase-specific extensions
CREATE TABLE IF NOT EXISTS extensions.enabled_extensions (
    name TEXT PRIMARY KEY,
    version TEXT,
    schema TEXT,
    enabled_at TIMESTAMPTZ DEFAULT NOW()
);

-- Insert commonly used Supabase extensions
INSERT INTO extensions.enabled_extensions (name, version, schema)
VALUES
    ('uuid-ossp', '1.1', 'public'),
    ('pgcrypto', '1.3', 'public'),
    ('postgis', '3.4', 'public'),
    ('pg_trgm', '1.6', 'public'),
    ('btree_gin', '1.3', 'public'),
    ('pg_stat_statements', '1.10', 'public')
ON CONFLICT (name) DO NOTHING;

-- =====================================================================
-- TESTING HELPERS
-- =====================================================================

-- Function to set test user context
CREATE OR REPLACE FUNCTION auth.set_test_user(
    p_user_id TEXT DEFAULT NULL,
    p_email TEXT DEFAULT NULL,
    p_role TEXT DEFAULT 'authenticated',
    p_org_id TEXT DEFAULT NULL,
    p_org_role TEXT DEFAULT 'member',
    p_org_name TEXT DEFAULT NULL
)
RETURNS VOID
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
BEGIN
    PERFORM set_config('auth.user_id', COALESCE(p_user_id, 'test-user-' || extract(epoch from now())::bigint::text), true);
    PERFORM set_config('auth.user_email', COALESCE(p_email, 'test@example.com'), true);
    PERFORM set_config('auth.user_role', p_role, true);
    PERFORM set_config('auth.org_id', COALESCE(p_org_id, 'test-org-' || extract(epoch from now())::bigint::text), true);
    PERFORM set_config('auth.org_role', p_org_role, true);
    PERFORM set_config('auth.org_name', COALESCE(p_org_name, 'Test Organization'), true);
END;
$$;

-- Function to clear test user context
CREATE OR REPLACE FUNCTION auth.clear_test_user()
RETURNS VOID
LANGUAGE plpgsql
SECURITY DEFINER
AS $$
BEGIN
    PERFORM set_config('auth.user_id', '', true);
    PERFORM set_config('auth.user_email', '', true);
    PERFORM set_config('auth.user_role', '', true);
    PERFORM set_config('auth.org_id', '', true);
    PERFORM set_config('auth.org_role', '', true);
    PERFORM set_config('auth.org_name', '', true);
END;
$$;

-- =====================================================================
-- SECURITY POLICIES HELPERS
-- =====================================================================

-- Helper to check if user is authenticated
CREATE OR REPLACE FUNCTION auth.is_authenticated()
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
SECURITY DEFINER
AS $$
  SELECT auth.jwt() ->> 'sub' IS NOT NULL;
$$;

-- Helper to check if user is admin
CREATE OR REPLACE FUNCTION auth.is_admin()
RETURNS BOOLEAN
LANGUAGE SQL
STABLE
SECURITY DEFINER
AS $$
  SELECT (
    (auth.jwt() ->> 'role') = 'admin' OR
    (auth.jwt() -> 'publicMetadata' ->> 'role') = 'admin' OR
    (auth.jwt() -> 'privateMetadata' ->> 'role') = 'admin' OR
    (auth.jwt() -> 'o' ->> 'role') IN ('admin', 'owner') OR
    (auth.jwt() ->> 'email') IN ('dominikos@myroomieapp.com', 'hey@myroomieapp.com')
  );
$$;

-- =====================================================================
-- COMMENTS AND DOCUMENTATION
-- =====================================================================

COMMENT ON SCHEMA auth IS 'Supabase compatibility layer for authentication functions';
COMMENT ON SCHEMA storage IS 'Supabase compatibility layer for storage functions';
COMMENT ON SCHEMA realtime IS 'Supabase compatibility layer for realtime features';
COMMENT ON SCHEMA extensions IS 'Supabase compatibility layer for extension management';

COMMENT ON FUNCTION auth.jwt() IS 'Returns JWT claims object compatible with Supabase auth.jwt()';
COMMENT ON FUNCTION auth.uid() IS 'Returns current user ID from JWT';
COMMENT ON FUNCTION auth.role() IS 'Returns current user role from JWT';
COMMENT ON FUNCTION auth.email() IS 'Returns current user email from JWT';
COMMENT ON FUNCTION storage.foldername(TEXT) IS 'Extracts folder path components from storage object name';
COMMENT ON FUNCTION storage.filename(TEXT) IS 'Extracts filename from storage object path';
COMMENT ON FUNCTION storage.extension(TEXT) IS 'Extracts file extension from storage object name';

-- =====================================================================
-- FINAL SETUP
-- =====================================================================

-- Grant necessary permissions
GRANT USAGE ON SCHEMA auth TO PUBLIC;
GRANT USAGE ON SCHEMA storage TO PUBLIC;
GRANT USAGE ON SCHEMA realtime TO PUBLIC;
GRANT USAGE ON SCHEMA extensions TO PUBLIC;

GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA auth TO PUBLIC;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA storage TO PUBLIC;

-- Set default search path to include compatibility schemas
ALTER DATABASE postgres SET search_path TO public, auth, storage, realtime, extensions;

-- Log successful setup
DO $$
BEGIN
    RAISE NOTICE 'Supabase compatibility layer installed successfully';
    RAISE NOTICE 'Use auth.set_test_user() to configure test user context';
    RAISE NOTICE 'Use auth.clear_test_user() to reset user context';
END $$;