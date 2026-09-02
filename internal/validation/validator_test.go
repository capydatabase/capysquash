package validation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractExtensionNamesFromSQL(t *testing.T) {
	t.Parallel()

	sql := `CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
DROP EXTENSION IF EXISTS postgis;
CREATE EXTENSION pg_trgm;`

	names := extractExtensionNamesFromSQL(sql)
	assert.ElementsMatch(t, []string{"uuid-ossp", "postgis", "pg_trgm"}, names)
}

func TestApplySQLFixesAndFunctionNormalization(t *testing.T) {
	t.Parallel()

	sv := NewSchemaValidator(DefaultValidationConfig(), nil, nil)
	sv.config.Verbose = false

	input := `ALTER PUBLICATION app_pub ADD TABLE users
CREATE POLICY users_policy ON users FOR SELECT USING (true)
CREATE FUNCTION test_fn() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql;
CREATE EXTENSION IF NOT EXISTS hstore;
CREATE EXTENSION IF NOT EXISTS hstore;`

	got := sv.applySQLFixes(input)

	assert.Contains(t, got, "ALTER PUBLICATION app_pub ADD TABLE users;")
	assert.Contains(t, got, "CREATE POLICY users_policy ON users FOR SELECT USING (true);")
	assert.Contains(t, got, "CREATE OR REPLACE FUNCTION test_fn()")
	assert.Equal(t, 1, countLinesContaining(got, "CREATE EXTENSION IF NOT EXISTS hstore;"))
}

func countLinesContaining(sql, needle string) int {
	count := 0
	for line := range strings.SplitSeq(sql, "\n") {
		if strings.TrimSpace(line) == needle {
			count++
		}
	}
	return count
}
