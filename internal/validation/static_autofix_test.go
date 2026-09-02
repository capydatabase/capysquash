package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplyFixes_PreferBigInt(t *testing.T) {
	validator := NewStaticValidator(nil)

	tests := []struct {
		name     string
		sql      string
		expected string
	}{
		{
			name:     "Replace INT with BIGINT",
			sql:      "CREATE TABLE users (id INT, count INTEGER);",
			expected: "CREATE TABLE users (id BIGINT, count BIGINT);",
		},
		{
			name:     "Replace INT4 with BIGINT",
			sql:      "CREATE TABLE items (id INT4);",
			expected: "CREATE TABLE items (id BIGINT);",
		},
		{
			name:     "Mixed casing",
			sql:      "CREATE TABLE t (col1 int, col2 InTeGeR);",
			expected: "CREATE TABLE t (col1 BIGINT, col2 BIGINT);",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := validator.Check(tc.sql)
			assert.NoError(t, err)

			fixedSQL, err := validator.ApplyFixes(tc.sql, violations)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, fixedSQL)
		})
	}
}

func TestApplyFixes_MultipleRules(t *testing.T) {
	validator := NewStaticValidator(nil)

	sql := `CREATE TABLE t1 (id INT, name VARCHAR(255)); CREATE TABLE t2 (val INTEGER, note character varying(32));`
	expected := `CREATE TABLE t1 (id BIGINT, name TEXT); CREATE TABLE t2 (val BIGINT, note TEXT);`

	violations, err := validator.Check(sql)
	assert.NoError(t, err)
	assert.NotEmpty(t, violations)

	fixed, err := validator.ApplyFixes(sql, violations)
	assert.NoError(t, err)
	assert.Equal(t, expected, fixed)
}
