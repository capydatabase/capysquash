-- PostgreSQL initialization script for pgsquash
-- This script sets up the initial database configuration

-- Create database if it doesn't exist (for local development)
-- Note: This won't work in Docker entrypoint, but useful for reference

-- Create extensions
-- UUID DATATYPE vs uuid-ossp EXTENSION
-- ====================================
-- UUID datatype is built-in PostgreSQL since version 8.3 - no extension needed
-- Only create uuid-ossp extension if you need UUID generation functions:
--   ► uuid_generate_v1(), uuid_generate_v1mc()
--   ► uuid_generate_v3(), uuid_generate_v4(), uuid_generate_v5()
--
-- If your migrations only use UUID datatype (e.g., column type), you don't need this extension.
-- If you use uuid_generate_v4() or similar functions, keep this extension enabled.
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pg_stat_statements";

-- Create application user (if not exists)
DO $$
BEGIN
   IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'pgsquash_app') THEN
      CREATE ROLE pgsquash_app LOGIN PASSWORD 'pgsquash_app_password';
   END IF;
END
$$;

-- Grant permissions
GRANT CONNECT ON DATABASE pgsquash TO pgsquash_app;
GRANT USAGE ON SCHEMA public TO pgsquash_app;
GRANT CREATE ON SCHEMA public TO pgsquash_app;

-- Set default permissions for future objects
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pgsquash_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO pgsquash_app;

-- Performance optimizations
ALTER SYSTEM SET shared_preload_libraries = 'pg_stat_statements';
ALTER SYSTEM SET pg_stat_statements.track = 'all';
ALTER SYSTEM SET pg_stat_statements.max = 1000;