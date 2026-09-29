package validation

import (
	"errors"
	"testing"

	"github.com/capydatabase/capysquash/internal/config"
	pg_query "github.com/pganalyze/pg_query_go/v6"
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

func TestApplyFixes_ConstraintNotValid(t *testing.T) {
	validator := NewStaticValidator(&config.StaticValidatorConfig{EnabledRules: []string{RuleCodeSafetyConstraintNotValid}})

	tests := []struct {
		name     string
		sql      string
		expected string
	}{
		{
			name:     "foreign key with actions",
			sql:      "ALTER TABLE posts ADD CONSTRAINT posts_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE;",
			expected: "ALTER TABLE posts ADD CONSTRAINT posts_user_fk FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE NOT VALID;",
		},
		{
			name:     "check with nested parentheses and a string",
			sql:      "ALTER TABLE t ADD CONSTRAINT c CHECK ((a > 0) AND (b <> ',')); SELECT 1;",
			expected: "ALTER TABLE t ADD CONSTRAINT c CHECK ((a > 0) AND (b <> ',')) NOT VALID; SELECT 1;",
		},
		{
			name:     "several commands in one statement",
			sql:      "ALTER TABLE t ADD CONSTRAINT c1 CHECK (a > 0), ADD CONSTRAINT c2 CHECK (b > 0)",
			expected: "ALTER TABLE t ADD CONSTRAINT c1 CHECK (a > 0) NOT VALID, ADD CONSTRAINT c2 CHECK (b > 0) NOT VALID",
		},
		{
			name:     "trailing comment stays after the insertion",
			sql:      "ALTER TABLE t ADD CHECK (a > 0) -- positive\n;",
			expected: "ALTER TABLE t ADD CHECK (a > 0) NOT VALID -- positive\n;",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := validator.Check(tc.sql)
			assert.NoError(t, err)
			assert.NotEmpty(t, violations)

			fixed, err := validator.ApplyFixes(tc.sql, violations)
			assert.NoError(t, err)
			assert.Equal(t, tc.expected, fixed)

			// The fixed SQL no longer violates the rule.
			again, err := validator.Check(fixed)
			assert.NoError(t, err)
			assert.Empty(t, again)
		})
	}
}

func TestPreferBigIntFlagsAlterColumnType(t *testing.T) {
	validator := NewStaticValidator(&config.StaticValidatorConfig{EnabledRules: []string{RuleCodeHygienePreferBigInt}})

	sql := "ALTER TABLE t ALTER COLUMN hits TYPE integer;"
	violations, err := validator.Check(sql)
	assert.NoError(t, err)
	if assert.Len(t, violations, 1) {
		assert.Contains(t, violations[0].Message, "'hits'")
	}

	fixed, err := validator.ApplyFixes(sql, violations)
	assert.NoError(t, err)
	assert.Equal(t, "ALTER TABLE t ALTER COLUMN hits TYPE BIGINT;", fixed)
}

type failingRule struct{}

func (failingRule) Code() string                { return "CSQ.TEST.FAILING" }
func (failingRule) Name() string                { return "always fails" }
func (failingRule) Category() ViolationCategory { return CategorySafety }
func (failingRule) Check(string, *pg_query.ParseResult) ([]Violation, error) {
	return nil, errors.New("boom")
}

// One failing rule does not hide the findings of the others.
func TestCheckKeepsOtherRulesFindingsWhenARuleFails(t *testing.T) {
	validator := &StaticValidator{
		config: config.DefaultStaticValidatorConfig(),
		rules:  []ValidationRule{failingRule{}, &PreferBigInt{}},
	}

	violations, err := validator.Check("CREATE TABLE t (id INT);")
	assert.ErrorContains(t, err, "CSQ.TEST.FAILING")
	if assert.Len(t, violations, 1) {
		assert.Equal(t, RuleCodeHygienePreferBigInt, violations[0].Code)
	}
}
