-- The metadata loader reads pg_catalog directly and renders every definition
-- with PostgreSQL's own canonical functions (format_type, pg_get_expr,
-- pg_get_constraintdef, pg_get_indexdef, pg_get_viewdef, pg_get_functiondef,
-- pg_get_triggerdef), so two databases built by the same server major version
-- can be compared structurally. information_schema is avoided: it hides
-- objects the connecting role has no privileges on. Objects that belong to an
-- extension (pg_depend deptype 'e') are skipped; extensions are compared by
-- name and version instead.

-- name: GetPostgresVersion :one
SELECT version() AS version;

-- name: GetSearchPath :one
SELECT current_setting('search_path') AS search_path;

-- name: ListUserSchemas :many
SELECT n.nspname::text AS schema_name
FROM pg_namespace n
WHERE n.nspname NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
  AND n.nspname NOT LIKE 'pg_temp_%'
  AND n.nspname NOT LIKE 'pg_toast_%'
  AND NOT EXISTS (
    SELECT 1 FROM pg_depend d
    WHERE d.classid = 'pg_namespace'::regclass AND d.objid = n.oid AND d.deptype = 'e'
  )
ORDER BY n.nspname;

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
  AND NOT EXISTS (
    SELECT 1 FROM pg_depend d
    WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e'
  )
ORDER BY c.relname;

-- name: ListColumnsForTable :many
SELECT
  a.attname::text AS column_name,
  format_type(a.atttypid, a.atttypmod)::text AS data_type,
  (NOT a.attnotnull)::boolean AS is_nullable,
  COALESCE(
    CASE WHEN a.attgenerated = '' THEN pg_get_expr(ad.adbin, ad.adrelid, true) END,
    ''
  )::text AS column_default,
  (a.attgenerated <> '')::boolean AS is_generated,
  COALESCE(
    CASE WHEN a.attgenerated <> '' THEN pg_get_expr(ad.adbin, ad.adrelid, true) END,
    ''
  )::text AS generation_expression,
  (a.attidentity <> '')::boolean AS is_identity,
  CASE a.attidentity
    WHEN 'a' THEN 'ALWAYS'
    WHEN 'd' THEN 'BY DEFAULT'
    ELSE ''
  END::text AS identity_generation,
  CASE WHEN a.attcollation <> t.typcollation
    THEN a.attcollation::regcollation::text
    ELSE ''
  END::text AS collation_name,
  COALESCE(col_description(a.attrelid, a.attnum), '')::text AS column_comment
FROM pg_attribute a
JOIN pg_class c ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_type t ON t.oid = a.atttypid
LEFT JOIN pg_attrdef ad ON ad.adrelid = a.attrelid AND ad.adnum = a.attnum
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND c.relname = sqlc.arg(table_name)::text
  AND a.attnum > 0
  AND NOT a.attisdropped
ORDER BY a.attnum;

-- name: ListConstraintsForTable :many
-- NOT NULL constraints (contype 'n', catalogued since PostgreSQL 18) are
-- skipped: nullability is compared as a column property.
SELECT
  con.conname::text AS constraint_name,
  CASE con.contype
    WHEN 'p' THEN 'PRIMARY KEY'
    WHEN 'f' THEN 'FOREIGN KEY'
    WHEN 'u' THEN 'UNIQUE'
    WHEN 'c' THEN 'CHECK'
    WHEN 'x' THEN 'EXCLUDE'
    WHEN 't' THEN 'TRIGGER'
    ELSE 'OTHER'
  END::text AS constraint_type,
  COALESCE(
    (SELECT array_agg(a.attname::text ORDER BY array_position(con.conkey, a.attnum))
     FROM pg_attribute a
     WHERE a.attrelid = con.conrelid AND a.attnum = ANY(con.conkey)),
    ARRAY[]::text[]
  )::text[] AS columns,
  COALESCE(
    (SELECT fn.nspname || '.' || fc.relname
     FROM pg_class fc
     JOIN pg_namespace fn ON fn.oid = fc.relnamespace
     WHERE fc.oid = con.confrelid),
    ''
  )::text AS ref_table,
  COALESCE(
    (SELECT array_agg(a.attname::text ORDER BY array_position(con.confkey, a.attnum))
     FROM pg_attribute a
     WHERE a.attrelid = con.confrelid AND a.attnum = ANY(con.confkey)),
    ARRAY[]::text[]
  )::text[] AS ref_columns,
  CASE con.confdeltype
    WHEN 'a' THEN 'NO ACTION'
    WHEN 'r' THEN 'RESTRICT'
    WHEN 'c' THEN 'CASCADE'
    WHEN 'n' THEN 'SET NULL'
    WHEN 'd' THEN 'SET DEFAULT'
    ELSE ''
  END::text AS on_delete,
  CASE con.confupdtype
    WHEN 'a' THEN 'NO ACTION'
    WHEN 'r' THEN 'RESTRICT'
    WHEN 'c' THEN 'CASCADE'
    WHEN 'n' THEN 'SET NULL'
    WHEN 'd' THEN 'SET DEFAULT'
    ELSE ''
  END::text AS on_update,
  con.condeferrable::boolean AS is_deferrable,
  con.condeferred::boolean AS initially_deferred,
  COALESCE(
    CASE WHEN con.contype = 'c' THEN pg_get_expr(con.conbin, con.conrelid, true) END,
    ''
  )::text AS check_expr,
  pg_get_constraintdef(con.oid, true)::text AS definition
FROM pg_constraint con
JOIN pg_class c ON c.oid = con.conrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND c.relname = sqlc.arg(table_name)::text
  AND con.contype <> 'n'
ORDER BY con.conname;

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
  -- pg_get_expr cannot deparse a WHEN clause that references both OLD and NEW.
  COALESCE(substring(pg_get_triggerdef(t.oid, true) FROM ' WHEN \((.+)\) EXECUTE '), '')::text AS condition,
  (t.tgenabled = 'O') AS is_enabled,
  pg_get_triggerdef(t.oid, true)::text AS definition
FROM pg_trigger t
JOIN pg_class c ON c.oid = t.tgrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_proc p ON p.oid = t.tgfoid
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND c.relname = sqlc.arg(table_name)::text
  AND NOT t.tgisinternal
ORDER BY t.tgname;

-- name: ListPoliciesForTable :many
-- polroles holds 0 for PUBLIC, which has no pg_roles row.
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
    (SELECT array_agg(
       CASE WHEN r.role_oid = 0 THEN 'public' ELSE pg_get_userbyid(r.role_oid)::text END
       ORDER BY 1)
     FROM unnest(pol.polroles) AS r(role_oid)),
    ARRAY[]::text[]
  )::text[] AS roles,
  COALESCE(pg_get_expr(pol.polqual, pol.polrelid), '')::text AS using_expr,
  COALESCE(pg_get_expr(pol.polwithcheck, pol.polrelid), '')::text AS with_check_expr
FROM pg_policy pol
JOIN pg_class c ON c.oid = pol.polrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND c.relname = sqlc.arg(table_name)::text
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
  c.relname::text AS view_name,
  pg_get_viewdef(c.oid, true)::text AS definition,
  COALESCE(obj_description(c.oid, 'pg_class'), '')::text AS comment
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND c.relkind = 'v'
  AND NOT EXISTS (
    SELECT 1 FROM pg_depend d
    WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e'
  )
ORDER BY c.relname;

-- name: ListMaterializedViewsForSchema :many
SELECT
  c.relname::text AS matview_name,
  pg_get_viewdef(c.oid, true)::text AS definition,
  c.relispopulated AS is_populated,
  COALESCE(obj_description(c.oid, 'pg_class'), '')::text AS comment
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND c.relkind = 'm'
  AND NOT EXISTS (
    SELECT 1 FROM pg_depend d
    WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e'
  )
ORDER BY c.relname;

-- name: ListFunctionsForSchema :many
-- Functions, procedures and window functions; aggregates are skipped because
-- pg_get_functiondef rejects them.
SELECT
  p.proname::text AS function_name,
  pg_get_function_identity_arguments(p.oid)::text AS signature,
  l.lanname::text AS language,
  COALESCE(pg_get_function_result(p.oid), '')::text AS return_type,
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
  AND p.prokind IN ('f', 'p', 'w')
  AND NOT EXISTS (
    SELECT 1 FROM pg_depend d
    WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e'
  )
ORDER BY p.proname, pg_get_function_identity_arguments(p.oid);

-- name: ListSequencesForSchema :many
-- owned_by covers both OWNED BY (deptype 'a') and identity columns (deptype
-- 'i'), rendered as schema.table.column independent of search_path.
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
    (SELECT dn.nspname || '.' || dc.relname || '.' || da.attname
     FROM pg_depend d
     JOIN pg_class dc ON dc.oid = d.refobjid
     JOIN pg_namespace dn ON dn.oid = dc.relnamespace
     JOIN pg_attribute da ON da.attrelid = d.refobjid AND da.attnum = d.refobjsubid
     WHERE d.classid = 'pg_class'::regclass
       AND d.objid = c.oid
       AND d.refclassid = 'pg_class'::regclass
       AND d.deptype IN ('a', 'i')
     LIMIT 1),
    ''
  )::text AS owned_by
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
JOIN pg_sequence s ON s.seqrelid = c.oid
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND c.relkind = 'S'
  AND NOT EXISTS (
    SELECT 1 FROM pg_depend d
    WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype = 'e'
  )
ORDER BY c.relname;

-- name: ListTypesForSchema :many
-- Composite types are limited to standalone ones (CREATE TYPE ... AS); every
-- table also has a row type that is compared through the table itself.
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
    (SELECT array_agg(e.enumlabel::text ORDER BY e.enumsortorder)
     FROM pg_enum e
     WHERE e.enumtypid = t.oid),
    ARRAY[]::text[]
  )::text[] AS enum_elements,
  COALESCE(
    CASE t.typtype
      WHEN 'd' THEN
        format_type(t.typbasetype, t.typtypmod)
        || CASE WHEN t.typnotnull THEN ' NOT NULL' ELSE '' END
        || COALESCE(' DEFAULT ' || pg_get_expr(t.typdefaultbin, 0, true), '')
        || COALESCE(
          (SELECT string_agg(' CONSTRAINT ' || quote_ident(con.conname) || ' ' || pg_get_constraintdef(con.oid, true), '' ORDER BY con.conname)
           FROM pg_constraint con
           WHERE con.contypid = t.oid AND con.contype <> 'n'),
          ''
        )
      WHEN 'c' THEN
        (SELECT string_agg(
           quote_ident(a.attname) || ' ' || format_type(a.atttypid, a.atttypmod)
           || CASE WHEN a.attcollation <> at.typcollation
                THEN ' COLLATE ' || a.attcollation::regcollation::text
                ELSE '' END,
           ', ' ORDER BY a.attnum)
         FROM pg_attribute a
         JOIN pg_type at ON at.oid = a.atttypid
         WHERE a.attrelid = t.typrelid AND a.attnum > 0 AND NOT a.attisdropped)
      WHEN 'r' THEN
        (SELECT 'SUBTYPE ' || format_type(r.rngsubtype, NULL)
         FROM pg_range r
         WHERE r.rngtypid = t.oid)
    END,
    ''
  )::text AS definition,
  COALESCE(obj_description(t.oid, 'pg_type'), '')::text AS comment
FROM pg_type t
JOIN pg_namespace n ON n.oid = t.typnamespace
WHERE n.nspname = sqlc.arg(schema_name)::text
  AND (
    t.typtype IN ('e', 'd', 'r')
    OR (t.typtype = 'c' AND EXISTS (
      SELECT 1 FROM pg_class tc WHERE tc.oid = t.typrelid AND tc.relkind = 'c'
    ))
  )
  AND NOT EXISTS (
    SELECT 1 FROM pg_depend d
    WHERE d.classid = 'pg_type'::regclass AND d.objid = t.oid AND d.deptype = 'e'
  )
ORDER BY t.typname;

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
