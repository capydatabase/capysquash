package parser

import (
	"testing"

	"github.com/capydatabase/capysquash/internal/types"
)

// REFRESH MATERIALIZED VIEW fills a view with data, so it travels with the
// data operations: in history order and after every index exists. As a
// verbatim schema statement it was sorted ahead of the unique index and of the
// plain REFRESH that CONCURRENTLY needs.
func TestRefreshMaterializedViewIsADataOperation(t *testing.T) {
	migration, err := ParseMigration("REFRESH MATERIALIZED VIEW CONCURRENTLY reports.summary;", "004_refresh.sql")
	if err != nil {
		t.Fatal(err)
	}
	if len(migration.Statements) != 1 {
		t.Fatalf("got %d statements", len(migration.Statements))
	}
	stmt := migration.Statements[0]
	if !stmt.IsDataOp || stmt.Operation != types.OpRefresh || stmt.ObjectType != types.TypeData {
		t.Fatalf("IsDataOp=%t Operation=%s ObjectType=%s", stmt.IsDataOp, stmt.Operation, stmt.ObjectType)
	}
	if stmt.ObjectName != "reports.summary" {
		t.Fatalf("ObjectName = %q", stmt.ObjectName)
	}
}
