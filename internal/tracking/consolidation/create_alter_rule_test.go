package consolidation

import (
	"testing"

	"github.com/capydatabase/capysquash/internal/parser"
	"github.com/capydatabase/capysquash/internal/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parsedStatements(t *testing.T, sql string) []types.Statement {
	t.Helper()
	migration, err := parser.ParseMigration(sql, "001_test.sql")
	require.NoError(t, err)
	return migration.Statements
}

// ALTERs that cannot move into the CREATE TABLE are replayed after it, in
// history order, from the first one on: ENABLE ROW LEVEL SECURITY used to be
// dropped, which left the table readable by everyone.
func TestIntegrateAlterIntoCreateReplaysWhatItCannotMerge(t *testing.T) {
	t.Parallel()

	statements := parsedStatements(t, `
CREATE TABLE accounts (id int PRIMARY KEY);
ALTER TABLE accounts ADD COLUMN name text;
ALTER TABLE accounts ADD CONSTRAINT name_present CHECK (name <> '');
ALTER TABLE accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE accounts ADD COLUMN email text;
ALTER TABLE accounts FORCE ROW LEVEL SECURITY;
`)
	require.Len(t, statements, 6)

	got := integrateAlterIntoCreate(&statements[0], statements[1:])

	assert.Contains(t, got, "name text")
	assert.Contains(t, got, "CONSTRAINT name_present CHECK (name <> '')")
	assert.NotContains(t, got, "ALTER TABLE accounts ADD COLUMN name text")
	assert.Regexp(t, `(?s)\);\s+ALTER TABLE accounts ENABLE ROW LEVEL SECURITY;\s+ALTER TABLE accounts ADD COLUMN email text;\s+ALTER TABLE accounts FORCE ROW LEVEL SECURITY;$`, got)
}

// A statement that mixes an ADD COLUMN with something else, or adds a column
// without the COLUMN keyword the text extraction looks for, is replayed
// rather than half merged.
func TestIntegrateAlterIntoCreateReplaysStatementsItCannotFullyExtract(t *testing.T) {
	t.Parallel()

	statements := parsedStatements(t, `
CREATE TABLE accounts (id int PRIMARY KEY);
ALTER TABLE accounts ADD note text;
ALTER TABLE accounts ADD COLUMN a int, ALTER COLUMN id SET DEFAULT 1;
`)
	require.Len(t, statements, 3)

	got := integrateAlterIntoCreate(&statements[0], statements[1:])

	assert.Regexp(t, `(?s)^CREATE TABLE accounts \(id int PRIMARY KEY\);\s+ALTER TABLE accounts ADD note text;\s+ALTER TABLE accounts ADD COLUMN a int, ALTER COLUMN id SET DEFAULT 1;$`, got)
}
