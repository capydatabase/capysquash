//go:build integration

package squasher

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/capydatabase/capysquash/internal/config"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// createTestDatabase creates a throwaway database on DATABASE_URL's server and
// returns its DSN; it is dropped when the test ends.
func createTestDatabase(t *testing.T, admin *sql.DB, base *url.URL, prefix string) string {
	t.Helper()
	name := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	_, err := admin.Exec("CREATE DATABASE " + pq.QuoteIdentifier(name))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE IF EXISTS " + pq.QuoteIdentifier(name) + " WITH (FORCE)")
	})
	dsn := *base
	dsn.Path = "/" + name
	return dsn.String()
}

// Paranoid squashes apply the baseline to the validation database and compare
// its catalog with production's: a baseline that matches passes, one that
// differs fails, and the validation database is left empty either way.
func TestParanoidComparesTheBaselineWithProduction(t *testing.T) {
	baseDSN := os.Getenv("DATABASE_URL")
	if baseDSN == "" {
		t.Skip("DATABASE_URL is required for integration test")
	}
	base, err := url.Parse(baseDSN)
	require.NoError(t, err)
	admin, err := sql.Open("postgres", baseDSN)
	require.NoError(t, err)
	defer func() { _ = admin.Close() }()

	migrations := map[int]string{
		1: `CREATE TYPE mood AS ENUM ('ok', 'meh');
CREATE TABLE users (id bigint PRIMARY KEY, email text NOT NULL, state mood DEFAULT 'ok');
CREATE FUNCTION label(v bigint) RETURNS text LANGUAGE sql AS $$ SELECT 'n' || v::text $$;`,
		2: `ALTER TABLE users ADD COLUMN name text;
CREATE INDEX users_email_idx ON users (email);
CREATE FUNCTION label(v text) RETURNS text LANGUAGE sql AS $$ SELECT v $$;`,
	}

	prodSchema := migrations[1] + "\n" + migrations[2]

	for _, tc := range []struct {
		name      string
		prodExtra string
		wantValid bool
	}{
		{name: "matching production", wantValid: true},
		{name: "production has a column the migrations lack", prodExtra: "ALTER TABLE users ADD COLUMN legacy int;", wantValid: false},
		{name: "production has a different default", prodExtra: "ALTER TABLE users ALTER COLUMN state SET DEFAULT 'meh';", wantValid: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prodDSN := createTestDatabase(t, admin, base, "capysquash_prod")
			validationDSN := createTestDatabase(t, admin, base, "capysquash_validation")

			prod, err := sql.Open("postgres", prodDSN)
			require.NoError(t, err)
			defer func() { _ = prod.Close() }()
			_, err = prod.Exec(prodSchema + "\n" + tc.prodExtra)
			require.NoError(t, err)

			cfg := config.DefaultConfig()
			cfg.SafetyLevel = "paranoid"
			cfg.ProdDBDSN = prodDSN
			cfg.ValidationDSN = validationDSN
			e, err := NewEngine(EngineConfig{Config: cfg, Context: context.Background()})
			require.NoError(t, err)
			defer func() { _ = e.Close() }()

			_, err = e.SquashWithSeparateFiles(migrations)
			if tc.wantValid {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
				require.Contains(t, err.Error(), "paranoid database validation failed")
			}

			validation, err := sql.Open("postgres", validationDSN)
			require.NoError(t, err)
			defer func() { _ = validation.Close() }()
			var objects int
			require.NoError(t, validation.QueryRow(`SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = 'public'`).Scan(&objects))
			require.Zero(t, objects, "the validation database is reset")
		})
	}
}

func TestParanoidRequiresAValidationDatabase(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.SafetyLevel = "paranoid"
	cfg.ProdDBDSN = "postgres://unused"
	cfg.ValidationDSN = ""
	_, err := NewEngine(EngineConfig{Config: cfg})
	require.Error(t, err)
}
