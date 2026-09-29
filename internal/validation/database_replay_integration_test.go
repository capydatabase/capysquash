//go:build integration

package validation

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/capydatabase/capysquash/internal/parser"
	"github.com/capydatabase/capysquash/internal/types"
	"github.com/lib/pq"
)

// Thorough validation with a database replays every migration in order inside
// a rolled-back transaction: PostgreSQL reports what is wrong, later
// statements still run, and the database is left empty.
func TestValidateMigrationsReplaysAgainstTheDatabase(t *testing.T) {
	baseDSN := os.Getenv("DATABASE_URL")
	if baseDSN == "" {
		t.Skip("DATABASE_URL is required for integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	parsed, err := url.Parse(baseDSN)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	admin, err := sql.Open("postgres", baseDSN)
	if err != nil {
		t.Fatalf("open admin database: %v", err)
	}
	defer func() { _ = admin.Close() }()

	name := fmt.Sprintf("capysquash_replay_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(name)); err != nil {
		t.Fatalf("create database: %v", err)
	}
	dbURL := *parsed
	dbURL.Path = "/" + name
	db, err := sql.Open("postgres", dbURL.String())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = admin.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(name)+" WITH (FORCE)")
	})

	sources := []struct{ name, sql string }{
		{"001.sql", "CREATE TABLE users (id bigint PRIMARY KEY, email text NOT NULL);"},
		{"002.sql", "ALTER TABLE users ADD COLUMN name text;\nALTER TABLE accounts ADD COLUMN name text;"},
		{"003.sql", "CREATE INDEX CONCURRENTLY users_email_idx ON users (email);\nCREATE INDEX users_name_idx ON users (name);"},
	}
	var migrations []*types.Migration
	for _, src := range sources {
		m, err := parser.ParseMigration(src.sql, src.name)
		if err != nil {
			t.Fatalf("parse %s: %v", src.name, err)
		}
		migrations = append(migrations, m)
	}

	cfg := DefaultValidationConfig()
	cfg.Level = ValidationLevelThorough
	validator := NewSchemaValidator(cfg, db, nil)
	result, err := validator.ValidateMigrations(ctx, migrations)
	if err != nil {
		t.Fatalf("ValidateMigrations: %v", err)
	}

	var failed, notValidated []string
	for _, e := range result.Errors {
		if e.Code == "STATEMENT_FAILED" {
			failed = append(failed, fmt.Sprintf("%s:%d", e.File, e.Line))
		}
	}
	for _, w := range result.Warnings {
		if w.Code == "NOT_VALIDATED_OUTSIDE_TRANSACTION" {
			notValidated = append(notValidated, w.Message)
		}
	}
	if len(failed) != 1 || failed[0] != "002.sql:2" {
		t.Fatalf("failed statements = %v, want [002.sql:2] (the ALTER of the missing table)", failed)
	}
	if len(notValidated) != 1 {
		t.Fatalf("not-validated warnings = %v, want the CONCURRENTLY index only", notValidated)
	}

	var tables int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM pg_tables WHERE schemaname = 'public'").Scan(&tables); err != nil {
		t.Fatalf("count tables: %v", err)
	}
	if tables != 0 {
		t.Fatalf("replay left %d tables behind; it must roll back", tables)
	}
}
