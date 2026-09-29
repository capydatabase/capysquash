//go:build integration

package validation

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

func TestExternalCatalogValidationAgainstPostgres(t *testing.T) {
	baseDSN := os.Getenv("DATABASE_URL")
	if baseDSN == "" {
		t.Skip("DATABASE_URL is required for integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	parsed, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	adminURL := *parsed
	adminURL.Path = "/postgres"
	admin, err := sql.Open("postgres", adminURL.String())
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}
	defer func() { _ = admin.Close() }()

	databaseName := fmt.Sprintf("capysquash_external_%d", time.Now().UnixNano())
	databaseURL := *parsed
	databaseURL.Path = "/" + databaseName
	dsn := databaseURL.String()

	dropDatabase := func() {
		_, _ = admin.ExecContext(context.Background(),
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			databaseName,
		)
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(databaseName))
	}
	createDatabase := func() {
		dropDatabase()
		if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(databaseName)); err != nil {
			t.Fatalf("create validation database: %v", err)
		}
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatalf("open validation database: %v", err)
		}
		defer func() { _ = db.Close() }()
		if _, err := db.ExecContext(ctx, "CREATE EXTENSION IF NOT EXISTS pgcrypto"); err != nil {
			t.Fatalf("install baseline extension: %v", err)
		}
	}
	defer dropDatabase()

	migrations := t.TempDir()
	content := `
CREATE TYPE public.account_state AS ENUM ('active', 'disabled');
CREATE DOMAIN public.email_address AS text CHECK (VALUE LIKE '%@%');
CREATE SEQUENCE public.accounts_id_seq START 10 INCREMENT 2;
CREATE TABLE public.accounts (
  id bigint PRIMARY KEY DEFAULT nextval('public.accounts_id_seq'),
  email public.email_address NOT NULL,
  state public.account_state NOT NULL DEFAULT 'active'
);
ALTER SEQUENCE public.accounts_id_seq OWNED BY public.accounts.id;
CREATE INDEX accounts_email_idx ON public.accounts (email);
COMMENT ON TABLE public.accounts IS 'Account records';
COMMENT ON COLUMN public.accounts.email IS 'Normalized email';
CREATE FUNCTION public.account_visible(public.accounts) RETURNS boolean
LANGUAGE sql STABLE AS $$ SELECT true $$;
COMMENT ON FUNCTION public.account_visible(public.accounts) IS 'Visibility predicate';
ALTER TABLE public.accounts ENABLE ROW LEVEL SECURITY;
CREATE POLICY accounts_read ON public.accounts FOR SELECT TO PUBLIC USING (public.account_visible(accounts));
GRANT SELECT ON public.accounts TO PUBLIC;
GRANT USAGE ON SEQUENCE public.accounts_id_seq TO PUBLIC;
`
	if err := os.WriteFile(filepath.Join(migrations, "001_schema.sql"), []byte(content), 0o644); err != nil {
		t.Fatalf("write migration: %v", err)
	}

	config := DefaultValidationConfig()
	config.Verbose = false
	validator := NewSchemaValidator(config, nil, nil)
	defer func() { _ = validator.Close() }()

	createDatabase()
	original, err := validator.ApplyAndSnapshot(ctx, migrations, dsn)
	if err != nil {
		t.Fatalf("capture original snapshot: %v", err)
	}
	if _, err := validator.ApplyAndSnapshot(ctx, migrations, dsn); err == nil || !strings.Contains(err.Error(), "not empty") {
		t.Fatalf("expected non-empty database refusal, got %v", err)
	}

	createDatabase()
	candidate, err := validator.ApplyAndSnapshot(ctx, migrations, dsn)
	if err != nil {
		t.Fatalf("capture candidate snapshot: %v", err)
	}
	diff, err := CompareCatalogSnapshots(original, candidate)
	if err != nil {
		t.Fatalf("compare snapshots: %v", err)
	}
	if diff.HasDifferences {
		t.Fatalf("identical migrations produced catalog differences: %v", diff.Differences)
	}

	wantedKinds := []string{"sequence|", "type|", "relation|", "policy_roles|", "privileges|", "comment|"}
	for _, kind := range wantedKinds {
		found := false
		for _, signature := range original.Signature {
			if strings.HasPrefix(signature, kind) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("snapshot has no %s signature", kind)
		}
	}
}

func TestClaimedDatabaseResetReturnsDatabaseToEmpty(t *testing.T) {
	baseDSN := os.Getenv("DATABASE_URL")
	if baseDSN == "" {
		t.Skip("DATABASE_URL is required for integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	parsed, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	adminURL := *parsed
	adminURL.Path = "/postgres"
	admin, err := sql.Open("postgres", adminURL.String())
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}
	defer func() { _ = admin.Close() }()

	databaseName := fmt.Sprintf("capysquash_reset_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(databaseName)); err != nil {
		t.Fatalf("create validation database: %v", err)
	}
	defer func() {
		if _, err := admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(databaseName)+" WITH (FORCE)"); err != nil {
			t.Errorf("drop validation database: %v", err)
		}
	}()

	databaseURL := *parsed
	databaseURL.Path = "/" + databaseName
	db, err := sql.Open("postgres", databaseURL.String())
	if err != nil {
		t.Fatalf("open validation database: %v", err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, "CREATE SCHEMA platform; CREATE TABLE platform.settings (id int)"); err != nil {
		t.Fatalf("create allowed platform schema: %v", err)
	}
	if _, err := ClaimEmptyDatabase(ctx, db, nil); err == nil {
		t.Fatal("a database with a platform table must not be claimable without allowing its schema")
	}

	claimed, err := ClaimEmptyDatabase(ctx, db, []string{"platform"})
	if err != nil {
		t.Fatalf("claim empty database: %v", err)
	}
	script := `
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE SCHEMA app;
CREATE TYPE public.state AS ENUM ('on', 'off');
CREATE DOMAIN public.positive AS integer CHECK (VALUE > 0);
CREATE TYPE public.span AS RANGE (subtype = float8);
CREATE TYPE public.pair AS (a int, b text);
CREATE TABLE public.items (
  id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  legacy_id serial,
  state public.state,
  amount public.positive,
  token text DEFAULT encode(gen_random_bytes(8), 'hex')
);
CREATE SEQUENCE public.orders_seq;
CREATE VIEW public.item_ids AS SELECT id FROM public.items;
CREATE MATERIALIZED VIEW public.item_count AS SELECT count(*) FROM public.items;
CREATE FUNCTION public.touch() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RETURN NEW; END$$;
CREATE TRIGGER items_touch BEFORE UPDATE ON public.items FOR EACH ROW EXECUTE FUNCTION public.touch();
CREATE PROCEDURE public.noop() LANGUAGE sql AS $$ SELECT 1 $$;
CREATE TABLE app.events (id int);
CREATE TABLE platform.extra (id int);
ALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO PUBLIC;
ALTER DEFAULT PRIVILEGES REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT USAGE ON SEQUENCES TO PUBLIC;
ALTER DEFAULT PRIVILEGES IN SCHEMA app GRANT USAGE ON TYPES TO PUBLIC;
`
	if err := ExecuteSQLScript(ctx, db, script, "reset.sql"); err != nil {
		t.Fatalf("apply script: %v", err)
	}

	if err := claimed.Reset(ctx); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := ClaimEmptyDatabase(ctx, db, []string{"platform"}); err != nil {
		t.Fatalf("reset database cannot be claimed again: %v", err)
	}

	var remaining int
	if err := db.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM pg_extension WHERE extname = 'pgcrypto')
     + (SELECT count(*) FROM pg_namespace WHERE nspname = 'app')
     + (SELECT count(*) FROM pg_type t JOIN pg_namespace n ON n.oid = t.typnamespace
        WHERE n.nspname = 'public' AND t.typname IN ('state', 'positive', 'span', 'pair'))`).Scan(&remaining); err != nil {
		t.Fatalf("count remaining objects: %v", err)
	}
	if remaining != 0 {
		t.Fatalf("%d created objects survived the reset", remaining)
	}
	var platformTables int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_tables WHERE schemaname = 'platform'`).Scan(&platformTables); err != nil {
		t.Fatalf("count platform tables: %v", err)
	}
	if platformTables != 2 {
		t.Fatalf("allowed schema was modified: %d tables, want 2", platformTables)
	}
	var defaultACLs int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM pg_default_acl`).Scan(&defaultACLs); err != nil {
		t.Fatalf("count default privileges: %v", err)
	}
	if defaultACLs != 0 {
		t.Fatalf("%d default privilege entries survived the reset", defaultACLs)
	}
}

// Owners and privileges compare with the database owner normalized: the
// same history applied by the owners of two databases with different names
// matches, and a missing grant does not.
func TestPrivilegeSignaturesNormalizeTheDatabaseOwner(t *testing.T) {
	baseDSN := os.Getenv("DATABASE_URL")
	if baseDSN == "" {
		t.Skip("DATABASE_URL is required for integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	parsed, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	adminURL := *parsed
	adminURL.Path = "/postgres"
	admin, err := sql.Open("postgres", adminURL.String())
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}
	defer func() { _ = admin.Close() }()

	suffix := time.Now().UnixNano()
	owner := fmt.Sprintf("csq_owner_%d", suffix)
	reader := fmt.Sprintf("csq_reader_%d", suffix)
	first := fmt.Sprintf("capysquash_priv_a_%d", suffix)
	second := fmt.Sprintf("capysquash_priv_b_%d", suffix)
	setup := []string{
		"CREATE ROLE " + pq.QuoteIdentifier(owner) + " LOGIN PASSWORD 'owner'",
		"CREATE ROLE " + pq.QuoteIdentifier(reader),
		"CREATE DATABASE " + pq.QuoteIdentifier(first),
		"CREATE DATABASE " + pq.QuoteIdentifier(second) + " OWNER " + pq.QuoteIdentifier(owner),
	}
	for _, statement := range setup {
		if _, err := admin.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	defer func() {
		for _, statement := range []string{
			"DROP DATABASE IF EXISTS " + pq.QuoteIdentifier(first) + " WITH (FORCE)",
			"DROP DATABASE IF EXISTS " + pq.QuoteIdentifier(second) + " WITH (FORCE)",
			"DROP ROLE IF EXISTS " + pq.QuoteIdentifier(reader),
			"DROP ROLE IF EXISTS " + pq.QuoteIdentifier(owner),
		} {
			if _, err := admin.ExecContext(context.Background(), statement); err != nil {
				t.Errorf("%s: %v", statement, err)
			}
		}
	}()

	history := fmt.Sprintf(`
CREATE TABLE public.accounts (id bigint PRIMARY KEY, email text);
GRANT SELECT (email) ON public.accounts TO %[1]s;
REVOKE ALL ON public.accounts FROM PUBLIC;
CREATE FUNCTION public.visible() RETURNS boolean LANGUAGE sql AS $$ SELECT true $$;
REVOKE EXECUTE ON FUNCTION public.visible() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION public.visible() TO %[1]s WITH GRANT OPTION;
ALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO %[1]s;
`, pq.QuoteIdentifier(reader))
	withGrant := t.TempDir()
	if err := os.WriteFile(filepath.Join(withGrant, "001.sql"), []byte(history), 0o600); err != nil {
		t.Fatal(err)
	}
	withoutGrant := t.TempDir()
	missing := strings.Replace(history, "WITH GRANT OPTION", "", 1)
	if err := os.WriteFile(filepath.Join(withoutGrant, "001.sql"), []byte(missing), 0o600); err != nil {
		t.Fatal(err)
	}

	dsnFor := func(database, user, password string) string {
		u := *parsed
		u.Path = "/" + database
		if user != "" {
			u.User = url.UserPassword(user, password)
		}
		return u.String()
	}
	validator := NewSchemaValidator(DefaultValidationConfig(), nil, nil)
	defer func() { _ = validator.Close() }()

	original, err := validator.ApplyAndSnapshot(ctx, withGrant, dsnFor(first, "", ""))
	if err != nil {
		t.Fatalf("snapshot as the superuser: %v", err)
	}
	asOwner, err := validator.ApplyAndSnapshot(ctx, withGrant, dsnFor(second, owner, "owner"))
	if err != nil {
		t.Fatalf("snapshot as the database owner: %v", err)
	}
	diff, err := CompareCatalogSnapshots(original, asOwner)
	if err != nil {
		t.Fatal(err)
	}
	if diff.HasDifferences {
		t.Fatalf("the same history applied by each database's owner differs: %v", diff.Differences)
	}
	found := false
	for _, line := range original.Signature {
		if strings.HasPrefix(line, "privileges|routine|public|visible()|owner=<database owner> ") && strings.Contains(line, reader+"=EXECUTE*/<database owner>") {
			found = true
		}
	}
	if !found {
		t.Errorf("no normalized routine privilege line in %v", original.Signature)
	}

	if _, err := admin.ExecContext(ctx, "DROP DATABASE "+pq.QuoteIdentifier(second)+" WITH (FORCE)"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(second)+" OWNER "+pq.QuoteIdentifier(owner)); err != nil {
		t.Fatal(err)
	}
	withoutOption, err := validator.ApplyAndSnapshot(ctx, withoutGrant, dsnFor(second, owner, "owner"))
	if err != nil {
		t.Fatalf("snapshot without the grant option: %v", err)
	}
	diff, err = CompareCatalogSnapshots(original, withoutOption)
	if err != nil {
		t.Fatal(err)
	}
	if !diff.HasDifferences || !strings.Contains(strings.Join(diff.Differences, "\n"), "visible()") {
		t.Fatalf("a missing grant option must be a difference: %v", diff.Differences)
	}
}
