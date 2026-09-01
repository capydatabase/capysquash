package validation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestStaticValidator(t *testing.T) {
	validator := NewStaticValidator(nil)

	tests := []struct {
		name       string
		sql        string
		violations []Violation
	}{
		{
			name:       "Valid SQL",
			sql:        "CREATE TABLE foo (id BIGINT);",
			violations: nil,
		},
		{
			name: "Ban Drop Column",
			sql:  "ALTER TABLE foo DROP COLUMN bar;",
			violations: []Violation{
				{
					Code:     RuleCodeBreakingDropColumn,
					Category: CategoryBreaking,
				},
			},
		},
		{
			name: "Ban Drop Table",
			sql:  "DROP TABLE users;",
			violations: []Violation{
				{
					Code:     RuleCodeBreakingDropTable,
					Category: CategoryBreaking,
				},
			},
		},
		{
			name: "Ban Rename Column",
			sql:  "ALTER TABLE users RENAME COLUMN name TO display_name;",
			violations: []Violation{
				{
					Code:     RuleCodeBreakingRenameColumn,
					Category: CategoryBreaking,
				},
			},
		},
		{
			name: "Ban Rename Table",
			sql:  "ALTER TABLE users RENAME TO app_users;",
			violations: []Violation{
				{
					Code:     RuleCodeBreakingRenameTable,
					Category: CategoryBreaking,
				},
			},
		},
		{
			name: "Ban Type Change",
			sql:  "ALTER TABLE users ALTER COLUMN age TYPE BIGINT;",
			violations: []Violation{
				{
					Code:     RuleCodeBreakingTypeChange,
					Category: CategoryBreaking,
				},
			},
		},
		{
			name: "Require Concurrent Index - Create",
			sql:  "CREATE INDEX idx_foo ON foo(id);",
			violations: []Violation{
				{
					Code:     RuleCodeSafetyConcurrentIndex,
					Category: CategorySafety,
				},
			},
		},
		{
			name:       "Require Concurrent Index - Create Concurrent (Valid)",
			sql:        "CREATE INDEX CONCURRENTLY idx_foo ON foo(id);",
			violations: nil,
		},
		{
			name: "Require Concurrent Index - Drop",
			sql:  "DROP INDEX idx_foo;",
			violations: []Violation{
				{
					Code:     RuleCodeSafetyConcurrentIndex,
					Category: CategorySafety,
				},
			},
		},
		{
			name:       "Require Concurrent Index - Drop Concurrent (Valid)",
			sql:        "DROP INDEX CONCURRENTLY idx_foo;",
			violations: nil,
		},
		{
			name: "Prefer Text - Varchar(255)",
			sql:  "CREATE TABLE foo (name VARCHAR(255));",
			violations: []Violation{
				{
					Code:     RuleCodeHygienePreferText,
					Category: CategoryHygiene,
				},
			},
		},
		{
			name: "Prefer BigInt violation",
			sql:  "CREATE TABLE t1 (id INT);",
			violations: []Violation{
				{
					Code:     RuleCodeHygienePreferBigInt,
					Category: CategoryHygiene,
				},
			},
		},
		{
			name: "Block Missing Where (DELETE)",
			sql:  "DELETE FROM users;",
			violations: []Violation{
				{
					Code:     RuleCodeSafetyMissingWhere,
					Category: CategorySafety,
				},
			},
		},
		{
			name: "Constraint Missing Not Valid",
			sql:  "ALTER TABLE t1 ADD CONSTRAINT check_pos CHECK (val > 0);",
			violations: []Violation{
				{
					Code:     RuleCodeSafetyConstraintNotValid,
					Category: CategorySafety,
				},
			},
		},
		{
			name: "Constraint Validate Flow - Missing VALIDATE",
			sql:  "ALTER TABLE t1 ADD CONSTRAINT fk_users FOREIGN KEY (user_id) REFERENCES users(id) NOT VALID;",
			violations: []Violation{
				{
					Code:     RuleCodeSafetyConstraintFlow,
					Category: CategorySafety,
				},
			},
		},
		{
			name: "Constraint Validate Flow - Add Then Validate",
			sql: `ALTER TABLE t1 ADD CONSTRAINT fk_users FOREIGN KEY (user_id) REFERENCES users(id) NOT VALID;
ALTER TABLE t1 VALIDATE CONSTRAINT fk_users;`,
			violations: nil,
		},
		{
			name: "Ignore Directive Suppresses Matching Rule",
			sql: `-- capysquash-ignore:CSQ.SAFETY.CONCURRENT_INDEX
CREATE INDEX idx_foo ON foo(id);`,
			violations: nil,
		},
		{
			name: "Ignore Directive Does Not Suppress Other Rules",
			sql: `-- capysquash-ignore:CSQ.BREAKING.DROP_COLUMN
CREATE INDEX idx_foo ON foo(id);`,
			violations: []Violation{
				{
					Code:     RuleCodeSafetyConcurrentIndex,
					Category: CategorySafety,
				},
				{
					Code:     RuleCodeMetaUnusedIgnoreDirective,
					Category: CategoryHygiene,
				},
			},
		},
		{
			name: "File Ignore Suppresses Across Statements",
			sql: `-- capysquash-ignore-file:CSQ.SAFETY.CONCURRENT_INDEX
CREATE INDEX idx_foo ON foo(id);
DROP INDEX idx_foo;`,
			violations: nil,
		},
		{
			name: "Unused Ignore Is Reported",
			sql: `-- capysquash-ignore:CSQ.BREAKING.DROP_COLUMN
CREATE TABLE t1 (id BIGINT);`,
			violations: []Violation{
				{
					Code:     RuleCodeMetaUnusedIgnoreDirective,
					Category: CategoryHygiene,
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := validator.Check(tc.sql)
			assert.NoError(t, err)

			if len(tc.violations) == 0 {
				assert.Empty(t, violations)
			} else {
				assert.Len(t, violations, len(tc.violations))

				expectedByCode := make(map[string]ViolationCategory, len(tc.violations))
				for _, expected := range tc.violations {
					expectedByCode[expected.Code] = expected.Category
				}

				for _, actual := range violations {
					expectedCategory, ok := expectedByCode[actual.Code]
					if assert.True(t, ok, "unexpected rule code returned: %s", actual.Code) {
						assert.Equal(t, expectedCategory, actual.Category)
					}
				}
			}
		})
	}
}

func TestBreakingRuleSignalsAreNonOverlapping(t *testing.T) {
	validator := NewStaticValidator(nil)

	tests := []struct {
		name         string
		sql          string
		expectedCode string
	}{
		{
			name:         "drop column maps only to DROP_COLUMN",
			sql:          "ALTER TABLE users DROP COLUMN legacy_name;",
			expectedCode: RuleCodeBreakingDropColumn,
		},
		{
			name:         "rename column maps only to RENAME_COLUMN",
			sql:          "ALTER TABLE users RENAME COLUMN name TO display_name;",
			expectedCode: RuleCodeBreakingRenameColumn,
		},
		{
			name:         "alter column type maps only to TYPE_CHANGE",
			sql:          "ALTER TABLE users ALTER COLUMN age TYPE BIGINT;",
			expectedCode: RuleCodeBreakingTypeChange,
		},
		{
			name:         "rename table maps only to RENAME_TABLE",
			sql:          "ALTER TABLE users RENAME TO app_users;",
			expectedCode: RuleCodeBreakingRenameTable,
		},
		{
			name:         "drop table maps only to DROP_TABLE",
			sql:          "DROP TABLE users;",
			expectedCode: RuleCodeBreakingDropTable,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			violations, err := validator.Check(tc.sql)
			assert.NoError(t, err)
			if assert.Len(t, violations, 1) {
				assert.Equal(t, tc.expectedCode, violations[0].Code)
				assert.Equal(t, CategoryBreaking, violations[0].Category)
			}
		})
	}
}
