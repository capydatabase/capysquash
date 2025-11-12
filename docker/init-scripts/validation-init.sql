-- Simple PostgreSQL initialization for validation containers
-- This script avoids dependencies on specific database names

-- Create extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "pg_trgm";
CREATE EXTENSION IF NOT EXISTS "btree_gin";

-- Create simple roles for compatibility
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pgsquash_readonly') THEN
        CREATE ROLE pgsquash_readonly;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'pgsquash_readwrite') THEN
        CREATE ROLE pgsquash_readwrite;
    END IF;
END $$;

-- Grant basic privileges
GRANT USAGE ON SCHEMA public TO pgsquash_readonly, pgsquash_readwrite;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO pgsquash_readonly;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO pgsquash_readwrite;

-- Set default privileges
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO pgsquash_readonly;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pgsquash_readwrite;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE, SELECT ON SEQUENCES TO pgsquash_readwrite;

-- Create validation function
CREATE OR REPLACE FUNCTION validation_report()
RETURNS TABLE (
    object_type TEXT,
    object_name TEXT,
    object_count BIGINT
) AS $$
BEGIN
    RETURN QUERY
    SELECT 'table'::TEXT, schemaname || '.' || tablename, COUNT(*)::BIGINT
    FROM pg_tables
    WHERE schemaname NOT IN ('information_schema', 'pg_catalog')
    GROUP BY schemaname, tablename

    UNION ALL

    SELECT 'index'::TEXT, schemaname || '.' || indexname, COUNT(*)::BIGINT
    FROM pg_indexes
    WHERE schemaname NOT IN ('information_schema', 'pg_catalog')
    GROUP BY schemaname, indexname

    UNION ALL

    SELECT 'function'::TEXT, n.nspname || '.' || p.proname, COUNT(*)::BIGINT
    FROM pg_proc p
    JOIN pg_namespace n ON p.pronamespace = n.oid
    WHERE n.nspname NOT IN ('information_schema', 'pg_catalog')
    GROUP BY n.nspname, p.proname;
END;
$$ LANGUAGE plpgsql;