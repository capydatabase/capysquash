package postprocessing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReplaceWholeWord(t *testing.T) {
	t.Parallel()

	input := "status verification_status_enum, backup verification_status_enum_backup, arr verification_status_enum[]"
	got, count := replaceWholeWord(input, "verification_status_enum", "primary_status_enum")

	assert.Equal(t, 2, count)
	assert.Contains(t, got, "status primary_status_enum")
	assert.Contains(t, got, "backup verification_status_enum_backup")
	assert.Contains(t, got, "arr primary_status_enum[]")
}

func TestExtractFunctionSignatureSnippet(t *testing.T) {
	t.Parallel()

	sql := `CREATE OR REPLACE FUNCTION auth.jwt() RETURNS jsonb LANGUAGE sql AS $$ SELECT '{}'::jsonb; $$;`
	match := extractFunctionSignatureSnippet(sql, "auth.jwt()")

	assert.Contains(t, match, "CREATE OR REPLACE FUNCTION auth.jwt()")
	assert.Contains(t, match, "AS $$")
}

func TestFixEliminatedEnumReferences(t *testing.T) {
	t.Parallel()

	sql := "CREATE TABLE t (status verification_status_enum, backup verification_status_enum_backup);"
	replacements := map[string]string{"verification_status_enum": "primary_status_enum"}

	got := fixEliminatedEnumReferences(sql, replacements)
	assert.Contains(t, got, "status primary_status_enum")
	assert.Contains(t, got, "backup verification_status_enum_backup")
}
