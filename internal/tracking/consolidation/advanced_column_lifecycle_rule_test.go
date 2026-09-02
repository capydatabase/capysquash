package consolidation

import (
	"slices"
	"strings"
	"testing"

	"github.com/capy-base/pgsquash-engine/internal/parser"
	"github.com/capy-base/pgsquash-engine/internal/types"
)

func TestAdvancedColumnLifecycleRule_ParseAlterOperations_ASTAndFallback(t *testing.T) {
	rule := NewAdvancedColumnLifecycleRule()

	stmtAdd := mustParseSingleStatement(t, `ALTER TABLE accounts ADD COLUMN status TEXT NOT NULL;`)
	opsAdd := rule.parseAlterOperations(stmtAdd)
	if len(opsAdd) != 1 {
		t.Fatalf("expected 1 operation for ADD COLUMN, got %d", len(opsAdd))
	}
	if opsAdd[0].Operation != ColumnOpAdd || opsAdd[0].NewValue != "status" {
		t.Fatalf("unexpected ADD op payload: %+v", opsAdd[0])
	}

	stmtRename := mustParseSingleStatement(t, `ALTER TABLE accounts RENAME COLUMN status TO state;`)
	opsRename := rule.parseAlterOperations(stmtRename)
	if len(opsRename) != 1 {
		t.Fatalf("expected 1 operation for RENAME COLUMN, got %d", len(opsRename))
	}
	if opsRename[0].Operation != ColumnOpRename || opsRename[0].OldValue != "status" || opsRename[0].NewValue != "state" {
		t.Fatalf("unexpected RENAME op payload: %+v", opsRename[0])
	}

	// DEFAULT operations may not be surfaced via explicit AST subtype in all parser versions;
	// ensure fallback parsing still captures them deterministically.
	stmtDefault := mustParseSingleStatement(t, `ALTER TABLE accounts ALTER COLUMN status SET DEFAULT 'active';`)
	opsDefault := rule.parseAlterOperations(stmtDefault)
	if len(opsDefault) != 1 {
		t.Fatalf("expected 1 operation for SET DEFAULT, got %d", len(opsDefault))
	}
	if opsDefault[0].Operation != ColumnOpSetDefault || opsDefault[0].OldValue != "status" {
		t.Fatalf("unexpected SET DEFAULT payload: %+v", opsDefault[0])
	}
	if !strings.Contains(opsDefault[0].NewValue, "'active'") {
		t.Fatalf("expected default value to include 'active', got %q", opsDefault[0].NewValue)
	}
}

func TestAdvancedColumnLifecycleRule_ParseConstraintDefinition_AST(t *testing.T) {
	rule := NewAdvancedColumnLifecycleRule()

	info := rule.parseConstraintDefinition(`ALTER TABLE orders ADD CONSTRAINT orders_user_id_fkey FOREIGN KEY (user_id) REFERENCES users(id);`)
	if info == nil {
		t.Fatal("expected constraint info, got nil")
	}

	if info.Type != ConstraintForeignKey {
		t.Fatalf("expected foreign key type, got %s", info.Type)
	}
	if info.Name != "orders_user_id_fkey" {
		t.Fatalf("expected normalized constraint name, got %q", info.Name)
	}
	if !slices.Contains(info.AffectedColumns, "user_id") {
		t.Fatalf("expected affected columns to include user_id, got %v", info.AffectedColumns)
	}
	if !strings.Contains(strings.ToUpper(info.Definition), "FOREIGN KEY") {
		t.Fatalf("expected foreign-key definition, got %q", info.Definition)
	}
}

func TestAdvancedColumnLifecycleRule_ExtractInitialColumns_AST(t *testing.T) {
	rule := NewAdvancedColumnLifecycleRule()
	stmt := mustParseSingleStatement(t, `
		CREATE TABLE accounts (
			id BIGINT PRIMARY KEY,
			email TEXT NOT NULL,
			status TEXT DEFAULT 'active'
		);
	`)

	cols := rule.extractInitialColumns(stmt)
	if len(cols) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(cols))
	}

	if cols[0].Name != "id" || cols[0].IsNullable {
		t.Fatalf("expected id to be non-nullable primary key, got %+v", cols[0])
	}
	if cols[2].Name != "status" || !strings.Contains(cols[2].DefaultValue, "DEFAULT") {
		t.Fatalf("expected status default metadata, got %+v", cols[2])
	}
}

func TestSQLMentionsIdentifier_WordBoundaries(t *testing.T) {
	if sqlMentionsIdentifier("CREATE INDEX idx ON t(status_id)", "id") {
		t.Fatal("expected false for partial identifier match")
	}
	if !sqlMentionsIdentifier("CREATE INDEX idx ON t(status)", "status") {
		t.Fatal("expected true for exact identifier match")
	}
}

func mustParseSingleStatement(t *testing.T, sql string) types.Statement {
	t.Helper()

	migration, err := parser.ParseMigration(sql, "test.sql")
	if err != nil {
		t.Fatalf("failed to parse SQL: %v", err)
	}
	if migration == nil || len(migration.Statements) == 0 {
		t.Fatal("expected at least one parsed statement")
	}

	return migration.Statements[0]
}
