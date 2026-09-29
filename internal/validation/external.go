package validation

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/lib/pq"
)

const CatalogSnapshotContractVersion = "capysquash.catalog-snapshot.v1"

// CatalogSnapshot is a portable, deterministic representation of a PostgreSQL
// schema. It contains no connection details or data values.
type CatalogSnapshot struct {
	ContractVersion   string   `json:"contract_version"`
	PostgreSQLVersion string   `json:"postgresql_version"`
	Signature         []string `json:"signature"`
}

// ExternalValidationOptions describes platform-owned schemas that may exist in
// an otherwise empty validation database. Extension-owned objects are always
// allowed because many managed Postgres services preinstall extensions.
type ExternalValidationOptions struct {
	AllowedSchemas []string
}

// ApplyAndSnapshot applies a migration path to a caller-owned empty database
// and returns its catalog signature. It refuses to touch a non-empty database.
// The caller remains responsible for provisioning and deleting the database.
func (sv *SchemaValidator) ApplyAndSnapshot(
	ctx context.Context,
	migrationPath, dsn string,
	options ...ExternalValidationOptions,
) (*CatalogSnapshot, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("external validation DSN is required")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open external validation database: %w", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("connect to external validation database: %w", err)
	}
	allowedSchemas := make([]string, 0)
	if len(options) > 0 {
		allowedSchemas = normalizeAllowedSchemas(options[0].AllowedSchemas)
	}
	if err := requireEmptyValidationDatabase(ctx, db, allowedSchemas); err != nil {
		return nil, err
	}

	if err := sv.applyMigrationsToDatabase(ctx, dsn, migrationPath); err != nil {
		return nil, err
	}

	signature, err := collectSchemaSignature(ctx, db)
	if err != nil {
		return nil, fmt.Errorf("collect external validation schema: %w", err)
	}

	var version string
	if err := db.QueryRowContext(ctx, "SHOW server_version").Scan(&version); err != nil {
		return nil, fmt.Errorf("read external validation PostgreSQL version: %w", err)
	}

	return &CatalogSnapshot{
		ContractVersion:   CatalogSnapshotContractVersion,
		PostgreSQLVersion: version,
		Signature:         signature,
	}, nil
}

// CompareCatalogSnapshots compares two previously captured catalog snapshots.
func CompareCatalogSnapshots(original, candidate *CatalogSnapshot) (*SchemaDiff, error) {
	if original == nil || candidate == nil {
		return nil, fmt.Errorf("original and candidate catalog snapshots are required")
	}
	if original.ContractVersion != CatalogSnapshotContractVersion {
		return nil, fmt.Errorf("unsupported original catalog snapshot contract %q", original.ContractVersion)
	}
	if candidate.ContractVersion != CatalogSnapshotContractVersion {
		return nil, fmt.Errorf("unsupported candidate catalog snapshot contract %q", candidate.ContractVersion)
	}
	if original.PostgreSQLVersion != candidate.PostgreSQLVersion {
		return nil, fmt.Errorf(
			"PostgreSQL versions differ: original %s, candidate %s",
			original.PostgreSQLVersion,
			candidate.PostgreSQLVersion,
		)
	}

	return compareLineSets(original.Signature, candidate.Signature), nil
}

// ClaimedDatabase is an empty, caller-owned validation database that
// capysquash is about to populate. It records what the database held before so
// Reset can return it to that state.
type ClaimedDatabase struct {
	db             *sql.DB
	allowedSchemas []string
	schemas        []string // every non-system schema present at claim time
	extensions     []string // every extension installed at claim time
	resetSchemas   []string // pre-existing schemas whose contents Reset drops (not allowed, not extension-owned)
}

// ClaimEmptyDatabase refuses a database that is not empty, with the same check
// validate-external applies: only allowedSchemas and extension-owned objects
// may exist. It then records the schemas and extensions present so Reset can
// undo whatever is applied afterwards.
func ClaimEmptyDatabase(ctx context.Context, db *sql.DB, allowedSchemas []string) (*ClaimedDatabase, error) {
	allowed := normalizeAllowedSchemas(allowedSchemas)
	if err := requireEmptyValidationDatabase(ctx, db, allowed); err != nil {
		return nil, err
	}
	schemas, err := queryNames(ctx, db, userSchemasQuery)
	if err != nil {
		return nil, fmt.Errorf("list validation database schemas: %w", err)
	}
	extensions, err := queryNames(ctx, db, "SELECT extname FROM pg_catalog.pg_extension ORDER BY extname")
	if err != nil {
		return nil, fmt.Errorf("list validation database extensions: %w", err)
	}
	resetSchemas, err := queryNames(ctx, db, resetSchemasQuery, pq.Array(allowed))
	if err != nil {
		return nil, fmt.Errorf("list validation database schemas to reset: %w", err)
	}
	return &ClaimedDatabase{
		db:             db,
		allowedSchemas: allowed,
		schemas:        schemas,
		extensions:     extensions,
		resetSchemas:   resetSchemas,
	}, nil
}

// Reset drops what was created since the claim: new extensions and new
// schemas (CASCADE), then every relation, routine and type left in the
// pre-existing schemas that were required to be empty (usually public).
// Allowed schemas are not touched, and objects that live outside schemas are
// kept: roles are cluster-wide, and publications and event triggers belong to
// the database. Reset finally re-runs the emptiness check, so a database it
// returns without error can be claimed again.
func (c *ClaimedDatabase) Reset(ctx context.Context) error {
	extensions, err := queryNames(ctx, c.db, "SELECT extname FROM pg_catalog.pg_extension ORDER BY extname")
	if err != nil {
		return fmt.Errorf("list validation database extensions: %w", err)
	}
	schemas, err := queryNames(ctx, c.db, userSchemasQuery)
	if err != nil {
		return fmt.Errorf("list validation database schemas: %w", err)
	}

	statements := make([]string, 0)
	for _, name := range extensions {
		if !slices.Contains(c.extensions, name) {
			statements = append(statements, "DROP EXTENSION IF EXISTS "+pq.QuoteIdentifier(name)+" CASCADE")
		}
	}
	for _, name := range schemas {
		if !slices.Contains(c.schemas, name) {
			statements = append(statements, "DROP SCHEMA IF EXISTS "+pq.QuoteIdentifier(name)+" CASCADE")
		}
	}
	for _, statement := range statements {
		if _, err := c.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("reset validation database (%s): %w", statement, err)
		}
	}

	objectDrops, err := queryNames(ctx, c.db, resetObjectsQuery, pq.Array(c.resetSchemas))
	if err != nil {
		return fmt.Errorf("list validation database objects to drop: %w", err)
	}
	for _, statement := range objectDrops {
		if _, err := c.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("reset validation database (%s): %w", statement, err)
		}
	}

	if err := requireEmptyValidationDatabase(ctx, c.db, c.allowedSchemas); err != nil {
		return fmt.Errorf("reset validation database: %w", err)
	}
	return nil
}

func queryNames(ctx context.Context, db *sql.DB, query string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

const userSchemasQuery = `
SELECT nspname
FROM pg_catalog.pg_namespace
WHERE nspname NOT IN ('pg_catalog', 'information_schema')
  AND nspname NOT LIKE 'pg_toast%'
  AND nspname NOT LIKE 'pg_temp_%'
ORDER BY nspname`

const resetSchemasQuery = `
SELECT n.nspname
FROM pg_catalog.pg_namespace n
WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
  AND n.nspname NOT LIKE 'pg_toast%'
  AND n.nspname NOT LIKE 'pg_temp_%'
  AND NOT (n.nspname = ANY($1::text[]))
  AND NOT EXISTS (
    SELECT 1 FROM pg_catalog.pg_depend d
    WHERE d.classid = 'pg_namespace'::regclass AND d.objid = n.oid AND d.deptype = 'e'
  )
ORDER BY n.nspname`

// resetObjectsQuery lists DROP statements for user objects in the given
// schemas. Extension members are dropped with their extension, and objects
// with an internal dependency (identity sequences, range constructors, the
// pg_class row of a composite type) with their owner, so both are skipped;
// standalone composite types are dropped through pg_type. IF EXISTS absorbs
// objects an earlier CASCADE already removed.
const resetObjectsQuery = `
SELECT statement
FROM (
  SELECT 1 AS phase, format('DROP %s IF EXISTS %I.%I CASCADE',
    CASE c.relkind
      WHEN 'v' THEN 'VIEW'
      WHEN 'm' THEN 'MATERIALIZED VIEW'
      WHEN 'S' THEN 'SEQUENCE'
      WHEN 'f' THEN 'FOREIGN TABLE'
      ELSE 'TABLE'
    END,
    n.nspname, c.relname) AS statement
  FROM pg_catalog.pg_class c
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname = ANY($1::text[])
    AND c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
    AND NOT EXISTS (
      SELECT 1 FROM pg_catalog.pg_depend d
      WHERE d.classid = 'pg_class'::regclass AND d.objid = c.oid AND d.deptype IN ('e', 'i')
    )
  UNION ALL
  SELECT 2, format('DROP ROUTINE IF EXISTS %s CASCADE', p.oid::regprocedure)
  FROM pg_catalog.pg_proc p
  JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
  WHERE n.nspname = ANY($1::text[])
    AND NOT EXISTS (
      SELECT 1 FROM pg_catalog.pg_depend d
      WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid AND d.deptype IN ('e', 'i')
    )
  UNION ALL
  SELECT 3, format('DROP TYPE IF EXISTS %I.%I CASCADE', n.nspname, t.typname)
  FROM pg_catalog.pg_type t
  JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
  WHERE n.nspname = ANY($1::text[])
    AND (
      t.typtype IN ('e', 'd', 'r')
      OR (t.typtype = 'c' AND EXISTS (
        SELECT 1 FROM pg_catalog.pg_class tc WHERE tc.oid = t.typrelid AND tc.relkind = 'c'
      ))
    )
    AND NOT EXISTS (
      SELECT 1 FROM pg_catalog.pg_depend d
      WHERE d.classid = 'pg_type'::regclass AND d.objid = t.oid AND d.deptype IN ('e', 'i')
    )
) drops
ORDER BY phase, statement`

func requireEmptyValidationDatabase(ctx context.Context, db *sql.DB, allowedSchemas []string) error {
	const query = `
SELECT EXISTS (
  SELECT 1
  FROM pg_catalog.pg_class c
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
    AND n.nspname NOT LIKE 'pg_toast%'
    AND n.nspname NOT LIKE 'pg_temp_%'
	AND NOT (n.nspname = ANY($1::text[]))
    AND c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
	AND NOT EXISTS (
	  SELECT 1 FROM pg_catalog.pg_depend d
	  WHERE d.classid = 'pg_class'::regclass
	    AND d.objid = c.oid
	    AND d.refclassid = 'pg_extension'::regclass
	    AND d.deptype = 'e'
	)
  UNION ALL
  SELECT 1
  FROM pg_catalog.pg_proc p
  JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
  WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
    AND n.nspname NOT LIKE 'pg_temp_%'
	AND NOT (n.nspname = ANY($1::text[]))
	AND NOT EXISTS (
	  SELECT 1 FROM pg_catalog.pg_depend d
	  WHERE d.classid = 'pg_proc'::regclass
	    AND d.objid = p.oid
	    AND d.refclassid = 'pg_extension'::regclass
	    AND d.deptype = 'e'
	)
  UNION ALL
  SELECT 1
  FROM pg_catalog.pg_type t
  JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
  WHERE n.nspname NOT IN ('pg_catalog', 'information_schema')
    AND n.nspname NOT LIKE 'pg_temp_%'
    AND t.typtype IN ('e', 'd', 'r')
	AND NOT (n.nspname = ANY($1::text[]))
	AND NOT EXISTS (
	  SELECT 1 FROM pg_catalog.pg_depend d
	  WHERE d.classid = 'pg_type'::regclass
	    AND d.objid = t.oid
	    AND d.refclassid = 'pg_extension'::regclass
	    AND d.deptype = 'e'
	)
  UNION ALL
  SELECT 1
  FROM pg_catalog.pg_namespace n
  WHERE n.nspname NOT IN ('public', 'pg_catalog', 'information_schema')
    AND n.nspname NOT LIKE 'pg_toast%'
    AND n.nspname NOT LIKE 'pg_temp_%'
	AND NOT (n.nspname = ANY($1::text[]))
	AND NOT EXISTS (
	  SELECT 1 FROM pg_catalog.pg_depend d
	  WHERE d.classid = 'pg_namespace'::regclass
	    AND d.objid = n.oid
	    AND d.refclassid = 'pg_extension'::regclass
	    AND d.deptype = 'e'
	)
)`

	var hasUserObjects bool
	if err := db.QueryRowContext(ctx, query, pq.Array(allowedSchemas)).Scan(&hasUserObjects); err != nil {
		return fmt.Errorf("inspect external validation database: %w", err)
	}
	if hasUserObjects {
		return fmt.Errorf("external validation database is not empty; refusing to apply migrations")
	}
	return nil
}

func normalizeAllowedSchemas(schemas []string) []string {
	seen := make(map[string]struct{}, len(schemas))
	result := make([]string, 0, len(schemas))
	for _, schema := range schemas {
		schema = strings.TrimSpace(schema)
		if schema == "" {
			continue
		}
		if _, ok := seen[schema]; ok {
			continue
		}
		seen[schema] = struct{}{}
		result = append(result, schema)
	}
	return result
}
