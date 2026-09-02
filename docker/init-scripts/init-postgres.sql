-- PostgreSQL initialization script for pgsquash validation databases
-- This script sets up the necessary extensions, roles, and configurations

-- Enable required extensions
-- NOTE: cube must be created before earthdistance (dependency)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
CREATE EXTENSION IF NOT EXISTS "btree_gin";
CREATE EXTENSION IF NOT EXISTS "btree_gist";
CREATE EXTENSION IF NOT EXISTS "cube";           -- Required by earthdistance
CREATE EXTENSION IF NOT EXISTS "earthdistance";  -- Depends on cube
CREATE EXTENSION IF NOT EXISTS "postgis";        -- Geospatial extension
CREATE EXTENSION IF NOT EXISTS "pg_trgm";        -- Trigram matching for text search
CREATE EXTENSION IF NOT EXISTS "pg_stat_statements";  -- Query statistics

-- Create additional roles for testing
CREATE ROLE pgsquash_readonly;
CREATE ROLE pgsquash_readwrite;

-- Grant basic privileges
GRANT CONNECT ON DATABASE pgsquash_primary TO pgsquash_readonly;
GRANT CONNECT ON DATABASE pgsquash_primary TO pgsquash_readwrite;

GRANT USAGE ON SCHEMA public TO pgsquash_readonly;
GRANT USAGE, CREATE ON SCHEMA public TO pgsquash_readwrite;

-- Set up default privileges for future objects
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO pgsquash_readonly;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pgsquash_readwrite;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE ON SEQUENCES TO pgsquash_readwrite;

-- Create a schema for validation testing
CREATE SCHEMA IF NOT EXISTS validation_test;
GRANT USAGE ON SCHEMA validation_test TO pgsquash_readonly;
GRANT USAGE, CREATE ON SCHEMA validation_test TO pgsquash_readwrite;

-- Set some optimal configurations for validation
ALTER SYSTEM SET shared_preload_libraries = 'pg_stat_statements';
ALTER SYSTEM SET log_statement = 'ddl';
ALTER SYSTEM SET log_min_duration_statement = 1000;
ALTER SYSTEM SET track_activities = on;
ALTER SYSTEM SET track_counts = on;
ALTER SYSTEM SET track_functions = 'all';

-- Create a function for validation reporting
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
    GROUP BY n.nspname, p.proname

    ORDER BY object_type, object_name;
END;
$$ LANGUAGE plpgsql;

-- Create a comprehensive schema comparison function
CREATE OR REPLACE FUNCTION compare_schemas(schema1 TEXT, schema2 TEXT)
RETURNS TABLE (
    difference_type TEXT,
    object_type TEXT,
    object_name TEXT,
    schema1_exists BOOLEAN,
    schema2_exists BOOLEAN,
    details JSONB
) AS $$
BEGIN
    -- Compare tables
    RETURN QUERY
    WITH table_comparison AS (
        SELECT
            t1.table_name,
            t1.table_name IS NOT NULL as in_schema1,
            t2.table_name IS NOT NULL as in_schema2
        FROM (
            SELECT table_name FROM information_schema.tables
            WHERE table_schema = schema1
        ) t1
        FULL OUTER JOIN (
            SELECT table_name FROM information_schema.tables
            WHERE table_schema = schema2
        ) t2 ON t1.table_name = t2.table_name
        WHERE t1.table_name IS NULL OR t2.table_name IS NULL
    )
    SELECT
        'table_mismatch'::TEXT,
        'table'::TEXT,
        COALESCE(table_name, ''),
        in_schema1,
        in_schema2,
        jsonb_build_object('table', table_name)
    FROM table_comparison;

    -- Add more comparisons as needed...
END;
$$ LANGUAGE plpgsql;