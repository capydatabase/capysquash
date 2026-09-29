//go:build integration

package validation

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"slices"
	"testing"
	"time"
)

// SCHEMA_DIFF applies the squashed baseline and then the original history
// to the same cluster; the roles the first one created must be gone before
// the second runs its CREATE ROLE.
func TestDropRolesExceptLeavesTheClusterAsItWas(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is required for integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer func() { _ = db.Close() }()

	before, err := listRoles(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	suffix := time.Now().UnixNano()
	reader, writer := fmt.Sprintf("csq_roles_reader_%d", suffix), fmt.Sprintf("csq_roles_writer_%d", suffix)
	for _, statement := range []string{
		"CREATE ROLE " + reader,
		"CREATE ROLE " + writer + " LOGIN",
		"GRANT " + reader + " TO " + writer,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}

	if err := dropRolesExcept(ctx, db, before); err != nil {
		t.Fatalf("dropRolesExcept: %v", err)
	}
	after, err := listRoles(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(after, before) {
		t.Fatalf("roles after the reset: %v, want %v", after, before)
	}
	// The original history can now create the same roles again.
	if _, err := db.ExecContext(ctx, "CREATE ROLE "+reader); err != nil {
		t.Fatalf("recreate %s: %v", reader, err)
	}
	if _, err := db.ExecContext(ctx, "DROP ROLE "+reader); err != nil {
		t.Fatalf("clean up %s: %v", reader, err)
	}
}
