package metadata

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// DatabaseOwnerRole is how privilege signatures name the role that owns the
// database being compared.
//
// Role names are compared literally - PUBLIC, the predefined pg_* roles and
// every application role - with one exception: the role that owns the
// database. The history and its squashed baseline are usually applied by
// that role, but under a different name in each place (neondb_owner in
// production, postgres in a validation container), so on each side it is
// replaced by this token before comparing. Every other role a history grants
// to must exist under the same name wherever it is validated: validation
// does not create roles. The history's own CREATE ROLE statements do, and
// the baseline creates each role only when it is missing, because roles are
// shared by every database of a cluster.
const DatabaseOwnerRole = "<database owner>"

// privilegeRole renders a role OID for a privilege signature.
func privilegeRole(expr string) string {
	return fmt.Sprintf(`CASE
    WHEN %[1]s = 0 THEN 'PUBLIC'
    WHEN %[1]s = (SELECT d.datdba FROM pg_catalog.pg_database d WHERE d.datname = pg_catalog.current_database()) THEN '%[2]s'
    ELSE pg_catalog.pg_get_userbyid(%[1]s)
  END`, expr, DatabaseOwnerRole)
}

// PrivilegeSignatureQuery lists every object's owner and effective
// privileges, and every ALTER DEFAULT PRIVILEGES entry, outside the system
// schemas: relations and sequences (relacl), columns with privileges of their
// own (attacl), routines by identity signature (proacl), schemas (nspacl),
// types and domains (typacl) and pg_default_acl. A NULL ACL is replaced by
// acldefault(), the privileges it stands for, so an ACL PostgreSQL has
// materialized compares equal to the untouched default it matches. Each ACL
// renders as its sorted aclexplode() entries, grantee=privilege[*]/grantor,
// with roles named as DatabaseOwnerRole describes.
var PrivilegeSignatureQuery = fmt.Sprintf(`
WITH objects AS (
  SELECT 'relation' AS kind, n.nspname AS schema_name, c.relname AS object_name, c.relowner AS owner,
    COALESCE(c.relacl, pg_catalog.acldefault((CASE WHEN c.relkind = 'S' THEN 's' ELSE 'r' END)::"char", c.relowner)) AS acl
  FROM pg_catalog.pg_class c
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relkind IN ('r', 'p', 'v', 'm', 'S', 'f')
  UNION ALL
  SELECT 'column', n.nspname, c.relname || '.' || a.attname, NULL, a.attacl
  FROM pg_catalog.pg_attribute a
  JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
  JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
  WHERE c.relkind IN ('r', 'p', 'v', 'm', 'f')
    AND a.attnum > 0
    AND NOT a.attisdropped
    AND pg_catalog.cardinality(a.attacl) > 0
  UNION ALL
  SELECT 'routine', n.nspname, p.proname || '(' || pg_catalog.pg_get_function_identity_arguments(p.oid) || ')', p.proowner,
    COALESCE(p.proacl, pg_catalog.acldefault('f'::"char", p.proowner))
  FROM pg_catalog.pg_proc p
  JOIN pg_catalog.pg_namespace n ON n.oid = p.pronamespace
  UNION ALL
  SELECT 'schema', n.nspname, n.nspname, n.nspowner, COALESCE(n.nspacl, pg_catalog.acldefault('n'::"char", n.nspowner))
  FROM pg_catalog.pg_namespace n
  UNION ALL
  SELECT 'type', n.nspname, t.typname, t.typowner, COALESCE(t.typacl, pg_catalog.acldefault('T'::"char", t.typowner))
  FROM pg_catalog.pg_type t
  JOIN pg_catalog.pg_namespace n ON n.oid = t.typnamespace
  WHERE t.typtype IN ('c', 'd', 'e', 'r', 'm')
    AND (t.typtype <> 'c' OR EXISTS (
      SELECT 1 FROM pg_catalog.pg_class tc WHERE tc.oid = t.typrelid AND tc.relkind = 'c'
    ))
  UNION ALL
  SELECT 'default privileges', COALESCE(n.nspname, ''),
    %[1]s || ' on ' || CASE d.defaclobjtype
      WHEN 'r' THEN 'tables' WHEN 'S' THEN 'sequences' WHEN 'f' THEN 'functions'
      WHEN 'T' THEN 'types' WHEN 'n' THEN 'schemas' WHEN 'L' THEN 'large objects'
      ELSE d.defaclobjtype::text END,
    NULL, d.defaclacl
  FROM pg_catalog.pg_default_acl d
  LEFT JOIN pg_catalog.pg_namespace n ON n.oid = d.defaclnamespace
)
SELECT
  o.kind,
  o.schema_name,
  o.object_name,
  COALESCE(%[2]s, '') AS owner,
  COALESCE((
    SELECT pg_catalog.string_agg(entries.entry, ',' ORDER BY entries.entry)
    FROM (
      SELECT %[3]s || '=' || a.privilege_type || CASE WHEN a.is_grantable THEN '*' ELSE '' END || '/' || %[4]s AS entry
      FROM pg_catalog.aclexplode(o.acl) a
    ) entries
  ), '') AS acl
FROM objects o
WHERE o.schema_name NOT IN ('pg_catalog', 'information_schema')
  AND o.schema_name NOT LIKE 'pg_toast%%'
  AND o.schema_name NOT LIKE 'pg_temp_%%'
ORDER BY 1, 2, 3`,
	privilegeRole("d.defaclrole"), privilegeRole("o.owner"), privilegeRole("a.grantee"), privilegeRole("a.grantor"))

// PrivilegeMetadata is one row of PrivilegeSignatureQuery.
type PrivilegeMetadata struct {
	Kind   string `json:"kind"`   // relation, column, routine, schema, type or default privileges
	Schema string `json:"schema"` // "" for database-wide default privileges
	Name   string `json:"name"`   // object name within the schema; for default privileges "<role> on <objects>"
	Owner  string `json:"owner"`  // "" for columns and default privileges
	ACL    string `json:"acl"`    // sorted grantee=privilege[*]/grantor entries
}

// Identifier names the row uniquely.
func (p PrivilegeMetadata) Identifier() string {
	return p.Kind + "|" + p.Schema + "|" + p.Name
}

// Definition renders what is compared: owner and privileges.
func (p PrivilegeMetadata) Definition() string {
	parts := make([]string, 0, 2)
	if p.Owner != "" {
		parts = append(parts, "owner="+p.Owner)
	}
	parts = append(parts, "acl="+p.ACL)
	return strings.Join(parts, " ")
}

// rowQuerier is satisfied by *sql.DB and *sql.Tx.
type rowQuerier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// LoadPrivileges runs PrivilegeSignatureQuery.
func LoadPrivileges(ctx context.Context, db rowQuerier) ([]PrivilegeMetadata, error) {
	rows, err := db.QueryContext(ctx, PrivilegeSignatureQuery)
	if err != nil {
		return nil, fmt.Errorf("list privileges: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var privileges []PrivilegeMetadata
	for rows.Next() {
		var p PrivilegeMetadata
		if err := rows.Scan(&p.Kind, &p.Schema, &p.Name, &p.Owner, &p.ACL); err != nil {
			return nil, fmt.Errorf("scan privileges: %w", err)
		}
		privileges = append(privileges, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate privileges: %w", err)
	}
	return privileges, nil
}
