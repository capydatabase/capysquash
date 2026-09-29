package parser

import (
	"testing"

	"github.com/capydatabase/capysquash/internal/types"
)

// ALTER FUNCTION ... RENAME names the overload it renames, so the tracker
// files it under that function's lifecycle instead of dropping a statement
// without a name.
func TestRenameFunctionNamesTheOverload(t *testing.T) {
	migration, err := ParseMigration("ALTER FUNCTION public.secret_count(int) RENAME TO hidden_count;", "002_rename.sql")
	if err != nil {
		t.Fatal(err)
	}
	stmt := migration.Statements[0]
	if stmt.ObjectType != types.TypeFunction || stmt.Operation != types.OpAlter {
		t.Fatalf("ObjectType=%s Operation=%s", stmt.ObjectType, stmt.Operation)
	}
	if stmt.ObjectName != "public.secret_count" || stmt.FunctionSignature != "(integer)" {
		t.Fatalf("ObjectName=%q FunctionSignature=%q", stmt.ObjectName, stmt.FunctionSignature)
	}
}
