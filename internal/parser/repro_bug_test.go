package parser

import (
	"testing"

	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/stretchr/testify/assert"
)

func TestDOBlockWithDDLIsPreservedVerbatim(t *testing.T) {
	sql := `
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM information_schema.columns
                 WHERE table_name = 'collection_items' AND column_name = 'command_id') THEN
    ALTER TABLE collection_items ADD COLUMN command_id TEXT REFERENCES commands(id) ON DELETE CASCADE;
    CREATE INDEX idx_collection_items_command_id ON collection_items(command_id);
  END IF;
END $$;
`

	migration, err := ParseMigration(sql, "test_migration.sql")
	assert.NoError(t, err)
	assert.NotNil(t, migration)

	if assert.Len(t, migration.Statements, 1) {
		statement := migration.Statements[0]
		assert.Equal(t, types.TypeDoBlock, statement.ObjectType)
		assert.Contains(t, statement.SQL, "IF NOT EXISTS")
		assert.Contains(t, statement.SQL, "ALTER TABLE collection_items")
		assert.Contains(t, statement.SQL, "CREATE INDEX idx_collection_items_command_id")
	}
}
