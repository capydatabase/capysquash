CREATE ROLE pdp_reader;
CREATE ROLE pdp_service;
CREATE ROLE pdp_other_owner;

-- Created before any default privileges: none of them apply.
CREATE TABLE public.before_defaults (id bigint PRIMARY KEY);
CREATE FUNCTION public.before_defaults_fn() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;

-- Database-wide defaults of the migrating role.
ALTER DEFAULT PRIVILEGES REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
ALTER DEFAULT PRIVILEGES GRANT EXECUTE ON FUNCTIONS TO pdp_service;
ALTER DEFAULT PRIVILEGES GRANT USAGE ON SCHEMAS TO pdp_reader;
ALTER DEFAULT PRIVILEGES REVOKE USAGE ON TYPES FROM PUBLIC;

CREATE SCHEMA app;

-- Schema defaults add to the database-wide ones.
ALTER DEFAULT PRIVILEGES IN SCHEMA app GRANT SELECT ON TABLES TO pdp_reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA app GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO pdp_service WITH GRANT OPTION;
ALTER DEFAULT PRIVILEGES IN SCHEMA app GRANT USAGE ON SEQUENCES TO pdp_service;

-- Defaults for another role only reach objects that role creates.
ALTER DEFAULT PRIVILEGES FOR ROLE pdp_other_owner IN SCHEMA app GRANT ALL ON TABLES TO pdp_reader;
