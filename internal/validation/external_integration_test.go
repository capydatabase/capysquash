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
	defer admin.Close()

	databaseName := fmt.Sprintf("pgsquash_external_%d", time.Now().UnixNano())
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
		defer db.Close()
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
	defer validator.Close()

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

	wantedKinds := []string{"sequence|", "type|", "relation|", "policy_roles|", "grant|", "comment|"}
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
