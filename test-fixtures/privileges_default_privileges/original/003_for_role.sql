-- FOR ROLE naming the role that runs the migrations (postgres in the e2e
-- suite) applies to the objects it creates from here on, exactly like the
-- defaults above. Which role that is cannot be known when squashing.
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA audit GRANT SELECT ON TABLES TO pdp_reader;
CREATE TABLE audit.events (id bigint PRIMARY KEY, payload jsonb);
