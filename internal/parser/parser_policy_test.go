package parser

import (
	"testing"

	"github.com/capy-base/pgsquash-engine/internal/types"
)

func TestParseDropPolicyCategorization(t *testing.T) {
	t.Parallel()

	migration, err := ParseMigration(`DROP POLICY IF EXISTS "rooms_public_read" ON rooms;`, "policy.sql")
	if err != nil {
		t.Fatalf("ParseMigration returned error: %v", err)
	}

	if len(migration.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(migration.Statements))
	}

	stmt := migration.Statements[0]

	if stmt.ObjectType != types.TypePolicy {
		t.Fatalf("expected ObjectType %s, got %s", types.TypePolicy, stmt.ObjectType)
	}

	if stmt.Category != types.CategorySecurity {
		t.Fatalf("expected Category %s, got %s", types.CategorySecurity, stmt.Category)
	}

	if stmt.ObjectName != "rooms_public_read" {
		t.Fatalf("expected ObjectName rooms_public_read, got %s", stmt.ObjectName)
	}

	if stmt.Schema != "public" {
		t.Fatalf("expected Schema public, got %s", stmt.Schema)
	}

	if len(stmt.Dependencies) != 1 || stmt.Dependencies[0] != "rooms" {
		t.Fatalf("expected dependency on rooms, got %v", stmt.Dependencies)
	}
}

func TestParseDropPolicyCategorizationWithSchema(t *testing.T) {
	t.Parallel()

	migration, err := ParseMigration(`DROP POLICY rooms_public_read ON leasing.rooms;`, "policy_with_schema.sql")
	if err != nil {
		t.Fatalf("ParseMigration returned error: %v", err)
	}

	if len(migration.Statements) != 1 {
		t.Fatalf("expected 1 statement, got %d", len(migration.Statements))
	}

	stmt := migration.Statements[0]

	if stmt.ObjectType != types.TypePolicy {
		t.Fatalf("expected ObjectType %s, got %s", types.TypePolicy, stmt.ObjectType)
	}

	if stmt.Category != types.CategorySecurity {
		t.Fatalf("expected Category %s, got %s", types.CategorySecurity, stmt.Category)
	}

	if stmt.ObjectName != "rooms_public_read" {
		t.Fatalf("expected ObjectName rooms_public_read, got %s", stmt.ObjectName)
	}

	if stmt.Schema != "leasing" {
		t.Fatalf("expected Schema leasing, got %s", stmt.Schema)
	}

	if len(stmt.Dependencies) != 1 || stmt.Dependencies[0] != "leasing.rooms" {
		t.Fatalf("expected dependency on leasing.rooms, got %v", stmt.Dependencies)
	}
}
