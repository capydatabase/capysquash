package validation

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

const CatalogSnapshotContractVersion = "pgsquash.catalog-snapshot.v1"

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
	defer db.Close()

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
