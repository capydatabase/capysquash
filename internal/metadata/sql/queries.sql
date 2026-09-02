-- name: GetPostgresVersion :one
SELECT version() AS version;

-- name: GetSearchPath :one
SELECT current_setting('search_path') AS search_path;

-- name: ListUserSchemas :many
SELECT schema_name::text AS schema_name
FROM information_schema.schemata
WHERE schema_name NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
  AND schema_name NOT LIKE 'pg_temp_%'
  AND schema_name NOT LIKE 'pg_toast_%'
ORDER BY schema_name;

-- name: ListExtensions :many
SELECT
  e.extname::text AS name,
  e.extversion::text AS version,
  COALESCE(n.nspname, '')::text AS schema,
  COALESCE(d.description, '')::text AS comment,
  e.extrelocatable AS relocatable
FROM pg_extension e
LEFT JOIN pg_namespace n ON n.oid = e.extnamespace
LEFT JOIN pg_description d ON d.objoid = e.oid AND d.objsubid = 0
ORDER BY e.extname;

-- name: ListTablesForSchema :many
SELECT
  c.relname::text AS table_name,
  COALESCE(obj_description(c.oid, 'pg_class'), '')::text AS table_comment
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND c.relkind IN ('r', 'p')
ORDER BY c.relname;

-- name: ListColumnsForTable :many
SELECT
  c.column_name::text AS column_name,
  c.data_type::text AS data_type,
  (c.is_nullable = 'YES') AS is_nullable,
  COALESCE(c.column_default, '')::text AS column_default,
  (c.is_generated = 'ALWAYS') AS is_generated,
  COALESCE(c.generation_expression, '')::text AS generation_expression,
  (c.is_identity = 'YES') AS is_identity,
  COALESCE(c.identity_generation, '')::text AS identity_generation,
  COALESCE(c.collation_name, '')::text AS collation_name,
  COALESCE(
    col_description(
      (quote_ident(sqlc.arg(schema_name)::text) || '.' || quote_ident(sqlc.arg(table_name)::text))::regclass,
      c.ordinal_position
    ),
    ''
  )::text AS column_comment
FROM information_schema.columns c
WHERE c.table_schema = sqlc.arg(schema_name)::text
  AND c.table_name = sqlc.arg(table_name)::text
ORDER BY c.ordinal_position;

-- name: ListConstraintsForTable :many
SELECT
  tc.constraint_name::text AS constraint_name,
  tc.constraint_type::text AS constraint_type,
  COALESCE(
    array_agg(DISTINCT kcu.column_name ORDER BY kcu.ordinal_position)
      FILTER (WHERE kcu.column_name IS NOT NULL),
    ARRAY[]::text[]
  )::text[] AS columns,
  COALESCE(MAX(ccu.table_name), '')::text AS ref_table,
  COALESCE(
    array_agg(DISTINCT ccu.column_name ORDER BY ccu.column_name)
      FILTER (WHERE ccu.column_name IS NOT NULL),
    ARRAY[]::text[]
  )::text[] AS ref_columns,
  COALESCE(MAX(rc.delete_rule), '')::text AS on_delete,
  COALESCE(MAX(rc.update_rule), '')::text AS on_update,
  COALESCE(bool_or(pgc.condeferrable), false)::boolean AS is_deferrable,
  COALESCE(bool_or(pgc.condeferred), false)::boolean AS initially_deferred,
  COALESCE(MAX(pg_get_constraintdef(pgc.oid)), '')::text AS check_expr
FROM information_schema.table_constraints tc
LEFT JOIN information_schema.key_column_usage kcu
  ON tc.constraint_name = kcu.constraint_name
  AND tc.table_schema = kcu.table_schema
  AND tc.table_name = kcu.table_name
LEFT JOIN information_schema.constraint_column_usage ccu
  ON tc.constraint_name = ccu.constraint_name
  AND tc.table_schema = ccu.constraint_schema
LEFT JOIN information_schema.referential_constraints rc
  ON tc.constraint_name = rc.constraint_name
  AND tc.table_schema = rc.constraint_schema
LEFT JOIN pg_constraint pgc
  ON pgc.conname = tc.constraint_name
  AND pgc.connamespace = (SELECT oid FROM pg_namespace WHERE nspname = tc.table_schema)
WHERE tc.table_schema = sqlc.arg(schema_name)::text
  AND tc.table_name = sqlc.arg(table_name)::text
GROUP BY tc.constraint_name, tc.constraint_type
ORDER BY tc.constraint_name;

-- name: ListIndexesForTable :many
SELECT
  ic.relname::text AS index_name,
  COALESCE(
    array_agg(a.attname ORDER BY array_position(i.indkey, a.attnum))
      FILTER (WHERE a.attname IS NOT NULL),
    ARRAY[]::text[]
  )::text[] AS columns,
  am.amname::text AS index_method,
  i.indisunique AS is_unique,
  i.indisprimary AS is_primary,
  COALESCE(pg_get_expr(i.indpred, i.indrelid), '')::text AS where_clause,
  COALESCE(pg_get_indexdef(i.indexrelid), '')::text AS definition
FROM pg_index i
JOIN pg_class c ON c.oid = i.indrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_class ic ON ic.oid = i.indexrelid
JOIN pg_am am ON am.oid = ic.relam
LEFT JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND c.relname = sqlc.arg(table_name)::text
GROUP BY ic.relname, am.amname, i.indisunique, i.indisprimary, i.indpred, i.indrelid, i.indexrelid
ORDER BY ic.relname;

-- name: ListTriggersForTable :many
SELECT
  t.tgname::text AS trigger_name,
  p.proname::text AS function_name,
  CASE t.tgtype & 66
    WHEN 2 THEN 'BEFORE'
    WHEN 64 THEN 'INSTEAD OF'
    ELSE 'AFTER'
  END::text AS timing,
  CASE t.tgtype & 1
    WHEN 1 THEN 'ROW'
    ELSE 'STATEMENT'
  END::text AS level,
  COALESCE(
    array_remove(ARRAY[
      CASE WHEN t.tgtype & 4 = 4 THEN 'INSERT' END,
      CASE WHEN t.tgtype & 8 = 8 THEN 'DELETE' END,
      CASE WHEN t.tgtype & 16 = 16 THEN 'UPDATE' END,
      CASE WHEN t.tgtype & 32 = 32 THEN 'TRUNCATE' END
    ], NULL),
    ARRAY[]::text[]
  )::text[] AS events,
  COALESCE(pg_get_expr(t.tgqual, t.tgrelid), '')::text AS condition,
  (t.tgenabled = 'O') AS is_enabled
FROM pg_trigger t
JOIN pg_class c ON c.oid = t.tgrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_proc p ON p.oid = t.tgfoid
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND c.relname = sqlc.arg(table_name)::text
  AND NOT t.tgisinternal
ORDER BY t.tgname;

-- name: ListPoliciesForTable :many
SELECT
  pol.polname::text AS policy_name,
  CASE pol.polcmd
    WHEN 'r' THEN 'SELECT'
    WHEN 'a' THEN 'INSERT'
    WHEN 'w' THEN 'UPDATE'
    WHEN 'd' THEN 'DELETE'
    ELSE 'ALL'
  END::text AS command,
  pol.polpermissive AS permissive,
  COALESCE(
    array_agg(r.rolname ORDER BY r.rolname) FILTER (WHERE r.rolname IS NOT NULL),
    ARRAY[]::text[]
  )::text[] AS roles,
  COALESCE(pg_get_expr(pol.polqual, pol.polrelid), '')::text AS using_expr,
  COALESCE(pg_get_expr(pol.polwithcheck, pol.polrelid), '')::text AS with_check_expr
FROM pg_policy pol
JOIN pg_class c ON c.oid = pol.polrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_roles r ON r.oid = ANY(pol.polroles)
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND c.relname = sqlc.arg(table_name)::text
GROUP BY pol.polname, pol.polcmd, pol.polpermissive, pol.polrelid, pol.polqual, pol.polwithcheck
ORDER BY pol.polname;

-- name: GetTableRowSecurity :one
SELECT COALESCE(c.relrowsecurity, false) AS row_security
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND c.relname = sqlc.arg(table_name)::text
LIMIT 1;

-- name: ListViewsForSchema :many
SELECT
  v.table_name::text AS view_name,
  COALESCE(v.view_definition, '')::text AS definition,
  COALESCE(
    obj_description((quote_ident(sqlc.arg(schema_name)::text) || '.' || quote_ident(v.table_name))::regclass),
    ''
  )::text AS comment
FROM information_schema.views v
WHERE v.table_schema = sqlc.arg(schema_name)::text
ORDER BY v.table_name;

-- name: ListMaterializedViewsForSchema :many
SELECT
  m.matviewname::text AS matview_name,
  COALESCE(m.definition, '')::text AS definition,
  m.ispopulated AS is_populated,
  COALESCE(
    obj_description((quote_ident(sqlc.arg(schema_name)::text) || '.' || quote_ident(m.matviewname))::regclass),
    ''
  )::text AS comment
FROM pg_matviews m
WHERE m.schemaname = sqlc.arg(schema_name)::text
ORDER BY m.matviewname;

-- name: ListFunctionsForSchema :many
SELECT
  p.proname::text AS function_name,
  pg_get_function_identity_arguments(p.oid)::text AS signature,
  l.lanname::text AS language,
  pg_get_function_result(p.oid)::text AS return_type,
  pg_get_functiondef(p.oid)::text AS body,
  CASE p.provolatile
    WHEN 'i' THEN 'IMMUTABLE'
    WHEN 's' THEN 'STABLE'
    ELSE 'VOLATILE'
  END::text AS volatility,
  p.proisstrict AS is_strict,
  CASE p.prosecdef
    WHEN true THEN 'DEFINER'
    ELSE 'INVOKER'
  END::text AS security,
  COALESCE(obj_description(p.oid, 'pg_proc'), '')::text AS comment
FROM pg_proc p
JOIN pg_namespace n ON n.oid = p.pronamespace
JOIN pg_language l ON l.oid = p.prolang
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND p.prokind = 'f'
ORDER BY p.proname, p.oid;

-- name: ListSequencesForSchema :many
SELECT
  c.relname::text AS sequence_name,
  format_type(s.seqtypid, NULL)::text AS data_type,
  s.seqstart::bigint AS start_value,
  s.seqincrement::bigint AS increment,
  s.seqmin::bigint AS min_value,
  s.seqmax::bigint AS max_value,
  s.seqcache::bigint AS cache_size,
  s.seqcycle AS is_cycle,
  COALESCE(
    (SELECT attrelid::regclass::text || '.' || attname
     FROM pg_attribute
     WHERE attrelid = d.refobjid AND attnum = d.refobjsubid),
    ''
  )::text AS owned_by
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_sequence s ON s.seqrelid = c.oid
LEFT JOIN pg_depend d ON d.objid = c.oid AND d.deptype = 'a'
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND c.relkind = 'S'
ORDER BY c.relname;

-- name: ListTypesForSchema :many
SELECT
  t.typname::text AS type_name,
  CASE t.typtype
    WHEN 'e' THEN 'ENUM'
    WHEN 'c' THEN 'COMPOSITE'
    WHEN 'd' THEN 'DOMAIN'
    WHEN 'r' THEN 'RANGE'
    ELSE 'OTHER'
  END::text AS type_kind,
  COALESCE(
    (SELECT array_agg(enumlabel ORDER BY enumsortorder)
     FROM pg_enum
     WHERE enumtypid = t.oid),
    ARRAY[]::text[]
  )::text[] AS enum_elements,
  COALESCE(obj_description(t.oid, 'pg_type'), '')::text AS comment
FROM pg_type t
JOIN pg_namespace n ON n.oid = t.typnamespace
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND t.typtype IN ('e', 'c', 'd', 'r')
ORDER BY t.typname;

-- name: GetTypeDefinition :one
SELECT COALESCE(pg_catalog.format_type(t.oid, NULL), '')::text AS definition
FROM pg_type t
JOIN pg_namespace n ON n.oid = t.typnamespace
WHERE t.typname = sqlc.arg(type_name)::text
  AND n.nspname = sqlc.arg(schema_name)::text
LIMIT 1;

-- name: ListSignatureExtensions :many
SELECT
  extname::text AS name,
  extversion::text AS version
FROM pg_catalog.pg_extension
ORDER BY extname;

-- name: ListSignatureTableColumns :many
SELECT
  n.nspname::text AS schema_name,
  c.relname::text AS table_name,
  a.attnum::int AS attnum,
  a.attname::text AS column_name,
  pg_catalog.format_type(a.atttypid, a.atttypmod)::text AS data_type,
  a.attnotnull AS not_null,
  COALESCE(pg_catalog.pg_get_expr(ad.adbin, ad.adrelid), '')::text AS default_expr
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid
LEFT JOIN pg_catalog.pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
WHERE c.relkind IN ('r', 'p')
  AND a.attnum > 0
  AND NOT a.attisdropped
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp_%'
ORDER BY n.nspname, c.relname, a.attnum;

-- name: ListSignatureConstraints :many
SELECT
  n.nspname::text AS schema_name,
  c.relname::text AS table_name,
  con.conname::text AS constraint_name,
  pg_catalog.pg_get_constraintdef(con.oid, true)::text AS definition
FROM pg_catalog.pg_constraint con
JOIN pg_catalog.pg_class c ON c.oid = con.conrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp_%'
ORDER BY n.nspname, c.relname, con.conname;

-- name: ListSignatureIndexes :many
SELECT
  schemaname::text AS schema_name,
  tablename::text AS table_name,
  indexname::text AS index_name,
  indexdef::text AS definition
FROM pg_catalog.pg_indexes
WHERE schemaname NOT IN ('pg_catalog', 'information_schema')
  AND schemaname NOT LIKE 'pg_toast%'
  AND schemaname NOT LIKE 'pg_temp_%'
ORDER BY schemaname, tablename, indexname;

-- name: ListSignatureViews :many
SELECT
  n.nspname::text AS schema_name,
  c.relname::text AS view_name,
  c.relkind::text AS rel_kind,
  pg_catalog.pg_get_viewdef(c.oid, true)::text AS definition
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('v', 'm')
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp_%'
ORDER BY n.nspname, c.relname;

-- name: ListSignatureFunctions :many
SELECT
  n.nspname::text AS schema_name,
  p.proname::text AS function_name,
  pg_catalog.pg_get_function_identity_arguments(p.oid)::text AS identity_arguments,
  pg_catalog.pg_get_functiondef(p.oid)::text AS definition
FROM pg_catalog.pg_proc p
JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp_%'
  AND p.prokind IN ('f', 'p', 'w')
ORDER BY n.nspname, p.proname, pg_catalog.pg_get_function_identity_arguments(p.oid), p.oid;

-- name: ListSignatureTriggers :many
SELECT
  n.nspname::text AS schema_name,
  c.relname::text AS table_name,
  t.tgname::text AS trigger_name,
  pg_catalog.pg_get_triggerdef(t.oid, true)::text AS definition
FROM pg_catalog.pg_trigger t
JOIN pg_catalog.pg_class c ON c.oid = t.tgrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE NOT t.tgisinternal
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp_%'
ORDER BY n.nspname, c.relname, t.tgname;

-- name: ListSignaturePolicies :many
SELECT
  n.nspname::text AS schema_name,
  c.relname::text AS table_name,
  p.polname::text AS policy_name,
  p.polcmd::text AS command,
  p.polpermissive AS permissive,
  COALESCE(pg_catalog.pg_get_expr(p.polqual, p.polrelid, true), '')::text AS using_expr,
  COALESCE(pg_catalog.pg_get_expr(p.polwithcheck, p.polrelid, true), '')::text AS check_expr
FROM pg_catalog.pg_policy p
JOIN pg_catalog.pg_class c ON c.oid = p.polrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp_%'
ORDER BY n.nspname, c.relname, p.polname;
