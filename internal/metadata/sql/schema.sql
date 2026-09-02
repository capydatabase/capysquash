-- Minimal schema stubs for sqlc static analysis.
--
-- The metadata query layer introspects PostgreSQL catalogs and information_schema.
-- sqlc requires relation/type shape at generation time, so we provide lightweight
-- table definitions for only the referenced columns.

CREATE SCHEMA IF NOT EXISTS pg_catalog;
CREATE SCHEMA IF NOT EXISTS information_schema;

CREATE TABLE IF NOT EXISTS information_schema.schemata (
	schema_name text
);

CREATE TABLE IF NOT EXISTS information_schema.columns (
	table_schema text,
	table_name text,
	column_name text,
	data_type text,
	is_nullable text,
	column_default text,
	is_generated text,
	generation_expression text,
	is_identity text,
	identity_generation text,
	collation_name text,
	ordinal_position integer
);

CREATE TABLE IF NOT EXISTS information_schema.table_constraints (
	table_schema text,
	table_name text,
	constraint_name text,
	constraint_type text,
	constraint_schema text
);

CREATE TABLE IF NOT EXISTS information_schema.key_column_usage (
	table_schema text,
	table_name text,
	constraint_name text,
	column_name text,
	ordinal_position integer
);

CREATE TABLE IF NOT EXISTS information_schema.constraint_column_usage (
	constraint_schema text,
	constraint_name text,
	table_name text,
	column_name text
);

CREATE TABLE IF NOT EXISTS information_schema.referential_constraints (
	constraint_schema text,
	constraint_name text,
	delete_rule text,
	update_rule text
);

CREATE TABLE IF NOT EXISTS information_schema.views (
	table_schema text,
	table_name text,
	view_definition text
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_namespace (
	oid bigint,
	nspname text
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_description (
	objoid bigint,
	objsubid integer,
	description text
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_extension (
	oid bigint,
	extname text,
	extversion text,
	extnamespace bigint,
	extrelocatable boolean
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_class (
	oid bigint,
	relname text,
	relnamespace bigint,
	relkind text,
	relam bigint,
	relrowsecurity boolean
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_index (
	indrelid bigint,
	indexrelid bigint,
	indkey integer[],
	indisunique boolean,
	indisprimary boolean,
	indpred text
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_am (
	oid bigint,
	amname text
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_attribute (
	attrelid bigint,
	attnum integer,
	attname text,
	atttypid bigint,
	atttypmod integer,
	attnotnull boolean,
	attisdropped boolean
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_trigger (
	oid bigint,
	tgname text,
	tgrelid bigint,
	tgfoid bigint,
	tgtype integer,
	tgqual text,
	tgenabled text,
	tgisinternal boolean
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_language (
	oid bigint,
	lanname text
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_proc (
	oid bigint,
	proname text,
	pronamespace bigint,
	prolang bigint,
	provolatile text,
	proisstrict boolean,
	prosecdef boolean,
	prokind text
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_roles (
	oid bigint,
	rolname text
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_policy (
	polname text,
	polcmd text,
	polpermissive boolean,
	polroles bigint[],
	polqual text,
	polwithcheck text,
	polrelid bigint
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_matviews (
	schemaname text,
	matviewname text,
	definition text,
	ispopulated boolean
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_sequence (
	seqrelid bigint,
	seqtypid bigint,
	seqstart bigint,
	seqincrement bigint,
	seqmin bigint,
	seqmax bigint,
	seqcache bigint,
	seqcycle boolean
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_depend (
	objid bigint,
	deptype text,
	refobjid bigint,
	refobjsubid integer
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_type (
	oid bigint,
	typname text,
	typnamespace bigint,
	typtype text
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_enum (
	enumtypid bigint,
	enumlabel text,
	enumsortorder real
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_attrdef (
	adrelid bigint,
	adnum integer,
	adbin text
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_constraint (
	oid bigint,
	conname text,
	connamespace bigint,
	condeferrable boolean,
	condeferred boolean,
	conrelid bigint
);

CREATE TABLE IF NOT EXISTS pg_catalog.pg_indexes (
	schemaname text,
	tablename text,
	indexname text,
	indexdef text
);

-- Unqualified aliases used by queries where PostgreSQL would normally resolve
-- via implicit pg_catalog search path.
CREATE TABLE IF NOT EXISTS pg_extension (
	oid bigint,
	extname text,
	extversion text,
	extnamespace bigint,
	extrelocatable boolean
);

CREATE TABLE IF NOT EXISTS pg_class (
	oid bigint,
	relname text,
	relnamespace bigint,
	relkind text,
	relam bigint,
	relrowsecurity boolean
);

CREATE TABLE IF NOT EXISTS pg_constraint (
	oid bigint,
	conname text,
	connamespace bigint,
	condeferrable boolean,
	condeferred boolean,
	conrelid bigint
);

CREATE TABLE IF NOT EXISTS pg_index (
	indrelid bigint,
	indexrelid bigint,
	indkey integer[],
	indisunique boolean,
	indisprimary boolean,
	indpred text
);

CREATE TABLE IF NOT EXISTS pg_trigger (
	oid bigint,
	tgname text,
	tgrelid bigint,
	tgfoid bigint,
	tgtype integer,
	tgqual text,
	tgenabled text,
	tgisinternal boolean
);

CREATE TABLE IF NOT EXISTS pg_policy (
	polname text,
	polcmd text,
	polpermissive boolean,
	polroles bigint[],
	polqual text,
	polwithcheck text,
	polrelid bigint
);

CREATE TABLE IF NOT EXISTS pg_matviews (
	schemaname text,
	matviewname text,
	definition text,
	ispopulated boolean
);

CREATE TABLE IF NOT EXISTS pg_proc (
	oid bigint,
	proname text,
	pronamespace bigint,
	prolang bigint,
	provolatile text,
	proisstrict boolean,
	prosecdef boolean,
	prokind text
);

CREATE TABLE IF NOT EXISTS pg_attribute (
	attrelid bigint,
	attnum integer,
	attname text,
	atttypid bigint,
	atttypmod integer,
	attnotnull boolean,
	attisdropped boolean
);

CREATE TABLE IF NOT EXISTS pg_enum (
	enumtypid bigint,
	enumlabel text,
	enumsortorder real
);

CREATE TABLE IF NOT EXISTS pg_type (
	oid bigint,
	typname text,
	typnamespace bigint,
	typtype text
);

CREATE TABLE IF NOT EXISTS pg_namespace (
	oid bigint,
	nspname text
);

CREATE TABLE IF NOT EXISTS pg_description (
	objoid bigint,
	objsubid integer,
	description text
);

CREATE TABLE IF NOT EXISTS pg_am (
	oid bigint,
	amname text
);

CREATE TABLE IF NOT EXISTS pg_language (
	oid bigint,
	lanname text
);

CREATE TABLE IF NOT EXISTS pg_roles (
	oid bigint,
	rolname text
);

CREATE TABLE IF NOT EXISTS pg_sequence (
	seqrelid bigint,
	seqtypid bigint,
	seqstart bigint,
	seqincrement bigint,
	seqmin bigint,
	seqmax bigint,
	seqcache bigint,
	seqcycle boolean
);

CREATE TABLE IF NOT EXISTS pg_depend (
	objid bigint,
	deptype text,
	refobjid bigint,
	refobjsubid integer
);

CREATE TABLE IF NOT EXISTS pg_attrdef (
	adrelid bigint,
	adnum integer,
	adbin text
);
