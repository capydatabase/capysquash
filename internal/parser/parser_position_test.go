package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseMigration_StatementPositionMetadata(t *testing.T) {
	t.Parallel()

	sql := `CREATE TABLE rooms (id integer);
ALTER TABLE rooms ADD COLUMN name text;`

	migration, err := ParseMigration(sql, "positions.sql")
	require.NoError(t, err)
	require.Len(t, migration.Statements, 2)

	first := migration.Statements[0]
	if first.Filename != "positions.sql" {
		t.Fatalf("expected first statement filename positions.sql, got %s", first.Filename)
	}
	if first.Line != 1 || first.Column != 1 {
		t.Fatalf("expected first statement at 1:1, got %d:%d", first.Line, first.Column)
	}

	second := migration.Statements[1]
	if second.Filename != "positions.sql" {
		t.Fatalf("expected second statement filename positions.sql, got %s", second.Filename)
	}
	if second.Line != 2 || second.Column != 1 {
		t.Fatalf("expected second statement at 2:1, got %d:%d", second.Line, second.Column)
	}
}
