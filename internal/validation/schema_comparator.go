package validation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/capy-base/pgsquash-engine/internal/errors"
	catalogsqlc "github.com/capy-base/pgsquash-engine/internal/metadata/sqlc"
	schemamodel "github.com/capy-base/pgsquash-engine/internal/schema"
)

// SchemaComparator compares two live PostgreSQL schemas using catalog signatures.
type SchemaComparator struct {
	logger Logger
}

// Logger interface to decouple from specific logging implementation
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// NewSchemaComparator creates a new schema comparator
func NewSchemaComparator(logger Logger) *SchemaComparator {
	return &SchemaComparator{
		logger: logger,
	}
}

// CompareDatabases compares two databases and returns a detailed difference report.
//
// This implementation intentionally avoids external schema-diff engines and uses
// deterministic catalog signatures gathered directly from each database.
func (sc *SchemaComparator) CompareDatabases(ctx context.Context, sourceDB, targetDB *sql.DB) (*SchemaDiff, error) {
	if sourceDB == nil || targetDB == nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"source and target databases are required for comparison",
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}

	sourceLines, err := collectSchemaSignature(ctx, sourceDB)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to collect source schema signature",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	targetLines, err := collectSchemaSignature(ctx, targetDB)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to collect target schema signature",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	return compareLineSets(sourceLines, targetLines), nil
}

func collectSchemaSignature(ctx context.Context, db *sql.DB) ([]string, error) {
	lines := make([]string, 0, 1024)
	queries := catalogsqlc.New(db)

	if err := collectExtensions(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectTableColumns(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectConstraints(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectIndexes(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectViews(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectFunctions(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectTriggers(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectPolicies(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectCatalogDefinitions(ctx, db, "sequence", signatureSequencesQuery, &lines); err != nil {
		return nil, err
	}
	if err := collectCatalogDefinitions(ctx, db, "type", signatureTypesQuery, &lines); err != nil {
		return nil, err
	}
	if err := collectCatalogDefinitions(ctx, db, "relation", signatureRelationsQuery, &lines); err != nil {
		return nil, err
	}
	if err := collectCatalogDefinitions(ctx, db, "ownership", signatureOwnershipQuery, &lines); err != nil {
		return nil, err
	}
	if err := collectCatalogDefinitions(ctx, db, "policy_roles", signaturePolicyRolesQuery, &lines); err != nil {
		return nil, err
	}
	if err := collectCatalogDefinitions(ctx, db, "grant", signatureGrantsQuery, &lines); err != nil {
		return nil, err
	}
	if err := collectCatalogDefinitions(ctx, db, "comment", signatureCommentsQuery, &lines); err != nil {
		return nil, err
	}

	sort.Strings(lines)
	return lines, nil
}

func collectExtensions(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureExtensions(ctx)
	if err != nil {
		return fmt.Errorf("list extensions: %w", err)
	}

	for _, row := range rows {
		*lines = append(*lines, fmt.Sprintf("extension|%s|%s", row.Name, row.Version))
	}

	return nil
}

func collectTableColumns(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureTableColumns(ctx)
	if err != nil {
		return fmt.Errorf("list table columns: %w", err)
	}

	for _, row := range rows {

		line := fmt.Sprintf(
			"column|%s.%s|%04d|%s|%s|notnull=%t|default=%s",
			row.SchemaName,
			row.TableName,
			row.Attnum,
			row.ColumnName,
			normalizeSQLWhitespace(row.DataType),
			row.NotNull,
			normalizeSQLWhitespace(row.DefaultExpr),
		)
		*lines = append(*lines, line)
	}

	return nil
}

func collectConstraints(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureConstraints(ctx)
	if err != nil {
		return fmt.Errorf("list constraints: %w", err)
	}

	for _, row := range rows {
		*lines = append(*lines, hashedSignature("constraint", fmt.Sprintf("%s.%s:%s", row.SchemaName, row.TableName, row.ConstraintName), row.Definition))
	}

	return nil
}

func collectIndexes(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureIndexes(ctx)
	if err != nil {
		return fmt.Errorf("list indexes: %w", err)
	}

	for _, row := range rows {
		*lines = append(*lines, hashedSignature("index", fmt.Sprintf("%s.%s:%s", row.SchemaName, row.TableName, row.IndexName), row.Definition))
	}

	return nil
}

func collectViews(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureViews(ctx)
	if err != nil {
		return fmt.Errorf("list views: %w", err)
	}

	for _, row := range rows {

		kind := "view"
		if row.RelKind == "m" {
			kind = "materialized_view"
		}

		*lines = append(*lines, hashedSignature(kind, fmt.Sprintf("%s.%s", row.SchemaName, row.ViewName), row.Definition))
	}

	return nil
}

func collectFunctions(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureFunctions(ctx)
	if err != nil {
		return fmt.Errorf("list functions: %w", err)
	}

	for _, row := range rows {

		identifier := fmt.Sprintf("%s.%s(%s)", row.SchemaName, row.FunctionName, normalizeSQLWhitespace(row.IdentityArguments))
		*lines = append(*lines, hashedSignature("function", identifier, row.Definition))
	}

	return nil
}

func collectTriggers(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureTriggers(ctx)
	if err != nil {
		return fmt.Errorf("list triggers: %w", err)
	}

	for _, row := range rows {
		*lines = append(*lines, hashedSignature("trigger", fmt.Sprintf("%s.%s:%s", row.SchemaName, row.TableName, row.TriggerName), row.Definition))
	}

	return nil
}

func collectPolicies(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignaturePolicies(ctx)
	if err != nil {
		return fmt.Errorf("list policies: %w", err)
	}

	for _, row := range rows {

		definition := fmt.Sprintf(
			"cmd=%s permissive=%t using=%s withcheck=%s",
			row.Command,
			row.Permissive,
			normalizeSQLWhitespace(row.UsingExpr),
			normalizeSQLWhitespace(row.CheckExpr),
		)

		*lines = append(*lines, hashedSignature("policy", fmt.Sprintf("%s.%s:%s", row.SchemaName, row.TableName, row.PolicyName), definition))
	}

	return nil
}

func collectCatalogDefinitions(ctx context.Context, db *sql.DB, kind, query string, lines *[]string) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("list %s signatures: %w", kind, err)
	}
	defer rows.Close()

	for rows.Next() {
		var identifier, definition string
		if err := rows.Scan(&identifier, &definition); err != nil {
			return fmt.Errorf("scan %s signature: %w", kind, err)
		}
		*lines = append(*lines, hashedSignature(kind, identifier, definition))
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate %s signatures: %w", kind, err)
	}
	return nil
}

const signatureSequencesQuery = `
SELECT
  n.nspname || '.' || c.relname AS identifier,
  concat_ws('|',
    pg_catalog.format_type(s.seqtypid, NULL),
    s.seqstart::text,
    s.seqincrement::text,
    s.seqmin::text,
    s.seqmax::text,
    s.seqcache::text,
    s.seqcycle::text,
    COALESCE(dep_ns.nspname || '.' || dep_class.relname || '.' || dep_attr.attname, '')
  ) AS definition
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
JOIN pg_catalog.pg_sequence s ON s.seqrelid = c.oid
LEFT JOIN pg_catalog.pg_depend d ON d.objid = c.oid AND d.deptype IN ('a', 'i')
LEFT JOIN pg_catalog.pg_class dep_class ON dep_class.oid = d.refobjid
LEFT JOIN pg_catalog.pg_namespace dep_ns ON dep_ns.oid = dep_class.relnamespace
LEFT JOIN pg_catalog.pg_attribute dep_attr ON dep_attr.attrelid = d.refobjid AND dep_attr.attnum = d.refobjsubid
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp_%'
ORDER BY identifier`

const signatureTypesQuery = `
SELECT
  n.nspname || '.' || t.typname AS identifier,
  concat_ws('|',
    t.typtype::text,
    pg_catalog.pg_get_userbyid(t.typowner),
    pg_catalog.format_type(t.typbasetype, t.typtypmod),
    t.typnotnull::text,
    COALESCE(pg_catalog.pg_get_expr(t.typdefaultbin, 0), t.typdefault, ''),
    COALESCE((
      SELECT string_agg(e.enumlabel, ',' ORDER BY e.enumsortorder)
      FROM pg_catalog.pg_enum e
      WHERE e.enumtypid = t.oid
    ), ''),
    COALESCE((
      SELECT string_agg(a.attname || ':' || pg_catalog.format_type(a.atttypid, a.atttypmod), ',' ORDER BY a.attnum)
      FROM pg_catalog.pg_attribute a
      WHERE a.attrelid = t.typrelid AND a.attnum > 0 AND NOT a.attisdropped
    ), ''),
    COALESCE(pg_catalog.format_type(r.rngsubtype, NULL), ''),
    COALESCE(r.rngcanonical::regproc::text, ''),
    COALESCE((
      SELECT string_agg(pg_catalog.pg_get_constraintdef(con.oid, true), ',' ORDER BY con.conname)
      FROM pg_catalog.pg_constraint con
      WHERE con.contypid = t.oid
    ), '')
  ) AS definition
FROM pg_catalog.pg_type t
JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
LEFT JOIN pg_catalog.pg_range r ON r.rngtypid = t.oid
WHERE t.typtype IN ('e', 'c', 'd', 'r')
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp_%'
  AND NOT EXISTS (
    SELECT 1 FROM pg_catalog.pg_depend d
    WHERE d.classid = 'pg_type'::regclass AND d.objid = t.oid AND d.deptype = 'e'
  )
ORDER BY identifier`

const signatureRelationsQuery = `
SELECT
  n.nspname || '.' || c.relname AS identifier,
  concat_ws('|',
    c.relkind::text,
    pg_catalog.pg_get_userbyid(c.relowner),
    c.relpersistence::text,
    c.relrowsecurity::text,
    c.relforcerowsecurity::text,
    c.relreplident::text,
    COALESCE(pg_catalog.pg_get_expr(c.relpartbound, c.oid, true), '')
  ) AS definition
FROM pg_catalog.pg_class c
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
WHERE c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
  AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp_%'
ORDER BY identifier`

const signatureOwnershipQuery = `
SELECT identifier, definition
FROM (
  SELECT
    'schema|' || n.nspname AS identifier,
    pg_catalog.pg_get_userbyid(n.nspowner) AS definition
  FROM pg_catalog.pg_namespace n
  WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
    AND n.nspname NOT LIKE 'pg_toast%'
    AND n.nspname NOT LIKE 'pg_temp_%'
  UNION ALL
  SELECT
    'function|' || n.nspname || '.' || p.proname || '(' || pg_catalog.pg_get_function_identity_arguments(p.oid) || ')',
    pg_catalog.pg_get_userbyid(p.proowner)
  FROM pg_catalog.pg_proc p
  JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
  WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
    AND n.nspname NOT LIKE 'pg_toast%'
    AND n.nspname NOT LIKE 'pg_temp_%'
) ownership
ORDER BY identifier`

const signaturePolicyRolesQuery = `
SELECT
  n.nspname || '.' || c.relname || ':' || p.polname AS identifier,
  COALESCE(string_agg(r.rolname, ',' ORDER BY r.rolname), 'PUBLIC') AS definition
FROM pg_catalog.pg_policy p
JOIN pg_catalog.pg_class c ON c.oid = p.polrelid
JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
LEFT JOIN pg_catalog.pg_roles r ON r.oid = ANY(p.polroles)
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp_%'
GROUP BY n.nspname, c.relname, p.polname
ORDER BY identifier`

const signatureGrantsQuery = `
SELECT identifier, definition
FROM (
  SELECT
    'table|' || table_schema || '.' || table_name || '|' || grantee || '|' || privilege_type AS identifier,
    is_grantable AS definition
  FROM information_schema.table_privileges
  WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
  UNION ALL
  SELECT
    'column|' || table_schema || '.' || table_name || '.' || column_name || '|' || grantee || '|' || privilege_type,
    is_grantable
  FROM information_schema.column_privileges
  WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
  UNION ALL
  SELECT
    'routine|' || routine_schema || '.' || routine_name || '|' || grantee || '|' || privilege_type,
    is_grantable
  FROM information_schema.routine_privileges
  WHERE routine_schema NOT IN ('pg_catalog', 'information_schema')
  UNION ALL
  SELECT
    'usage|' || object_type || '|' || object_schema || '.' || object_name || '|' || grantee || '|' || privilege_type,
    is_grantable
  FROM information_schema.usage_privileges
  WHERE object_schema NOT IN ('pg_catalog', 'information_schema')
) grants
ORDER BY identifier`

const signatureCommentsQuery = `
SELECT identifier, definition
FROM (
  SELECT
    'relation|' || n.nspname || '.' || c.relname AS identifier,
    d.description AS definition
  FROM pg_catalog.pg_description d
  JOIN pg_catalog.pg_class c ON c.oid = d.objoid
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  WHERE d.objsubid = 0
    AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  UNION ALL
  SELECT
    'column|' || n.nspname || '.' || c.relname || '.' || a.attname,
    d.description
  FROM pg_catalog.pg_description d
  JOIN pg_catalog.pg_class c ON c.oid = d.objoid
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = d.objsubid
  WHERE d.objsubid > 0
    AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  UNION ALL
  SELECT
    'function|' || n.nspname || '.' || p.proname || '(' || pg_catalog.pg_get_function_identity_arguments(p.oid) || ')',
    d.description
  FROM pg_catalog.pg_description d
  JOIN pg_catalog.pg_proc p ON p.oid = d.objoid
  JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
  WHERE d.objsubid = 0
    AND n.nspname NOT IN ('pg_catalog', 'information_schema')
  UNION ALL
  SELECT
    'type|' || n.nspname || '.' || t.typname,
    d.description
  FROM pg_catalog.pg_description d
  JOIN pg_catalog.pg_type t ON t.oid = d.objoid
  JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
  WHERE d.objsubid = 0
    AND n.nspname NOT IN ('pg_catalog', 'information_schema')
) comments
WHERE definition IS NOT NULL
ORDER BY identifier`

func hashedSignature(kind, identifier, definition string) string {
	normalized := normalizeSQLWhitespace(definition)
	return fmt.Sprintf("%s|%s|sha256:%s", kind, identifier, shortHash(normalized))
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	// 16 bytes (32 hex chars) is enough for readable, collision-resistant signatures here.
	return hex.EncodeToString(sum[:16])
}

func compareLineSets(sourceLines, targetLines []string) *SchemaDiff {
	sourceModel := schemamodel.BuildFromLines(sourceLines)
	targetModel := schemamodel.BuildFromLines(targetLines)
	diff := schemamodel.CompareModelsWithLabels(sourceModel, targetModel, "original", "squashed", 250)

	return &SchemaDiff{
		HasDifferences: diff.HasDifferences,
		Differences:    diff.Differences,
	}
}

// CompareSchemasDirectly compares two schema SQL strings directly using normalized statement sets.
func CompareSchemasDirectly(schema1, schema2 string) (*SchemaDiff, error) {
	leftModel := schemamodel.BuildFromSQL("original.sql", schema1)
	rightModel := schemamodel.BuildFromSQL("squashed.sql", schema2)
	diff := schemamodel.CompareModelsWithLabels(leftModel, rightModel, "original", "squashed", 250)

	return &SchemaDiff{
		HasDifferences: diff.HasDifferences,
		Differences:    diff.Differences,
	}, nil
}

func normalizeSQLWhitespace(sql string) string {
	return schemamodel.NormalizeSQLWhitespace(sql)
}
