package validation_test

import (
	"testing"

	"github.com/capysquash/pgsquash-engine/pkg/validation"
	"github.com/stretchr/testify/assert"
)

func TestNewStaticValidator_Config(t *testing.T) {
	// Test Default
	v1 := validation.NewStaticValidator(nil)
	assert.NotNil(t, v1)

	// Test with Config
	config := &validation.StaticValidatorConfig{
		EnabledRules: []string{"CSQ.BREAKING.DROP_COLUMN"},
	}
	v2 := validation.NewStaticValidator(config)
	assert.NotNil(t, v2)
}

func TestNewPreFlightValidator(t *testing.T) {
	v := validation.NewPreFlightValidator()
	assert.NotNil(t, v)
}

func TestNewPostFlightValidator(t *testing.T) {
	v := validation.NewPostFlightValidator()
	assert.NotNil(t, v)
}

func TestValidateSchemaDiff(t *testing.T) {
	s1 := "CREATE TABLE t1 (id INT);"
	s2 := "CREATE TABLE t1 (id INT);"
	s3 := "CREATE TABLE t1 (id BIGINT);"

	// Identical
	diff, err := validation.ValidateSchemaDiff(s1, s2)
	assert.NoError(t, err)
	assert.False(t, diff.HasDifferences)

	// Different
	diff2, err := validation.ValidateSchemaDiff(s1, s3)
	assert.NoError(t, err)
	assert.True(t, diff2.HasDifferences)
}

func TestMergeStaticValidatorConfig(t *testing.T) {
	base := &validation.StaticValidatorConfig{
		EnabledRules: []string{"CSQ.SAFETY.MISSING_WHERE"},
		RuleOptions: map[string]map[string]any{
			"CSQ.SAFETY.MISSING_WHERE": {
				"allow_full_table": false,
			},
		},
		TreatWarningsAsErrors: false,
	}

	overlay := &validation.StaticValidatorConfig{
		EnabledRules: []string{"CSQ.HYGIENE.PREFER_TEXT"},
		RuleOptions: map[string]map[string]any{
			"CSQ.SAFETY.MISSING_WHERE": {
				"allow_full_table": true,
			},
			"CSQ.HYGIENE.PREFER_TEXT": {
				"enabled": true,
			},
		},
		TreatWarningsAsErrors: true,
	}

	merged := validation.MergeStaticValidatorConfig(base, overlay)
	assert.Equal(t, []string{"CSQ.HYGIENE.PREFER_TEXT"}, merged.EnabledRules)
	assert.Equal(t, true, merged.TreatWarningsAsErrors)
	assert.Equal(t, true, merged.RuleOptions["CSQ.SAFETY.MISSING_WHERE"]["allow_full_table"])
	assert.Equal(t, true, merged.RuleOptions["CSQ.HYGIENE.PREFER_TEXT"]["enabled"])
}

func TestBuildStaticValidatorConfig(t *testing.T) {
	base := &validation.StaticValidatorConfig{
		EnabledRules: []string{"CSQ.SAFETY.MISSING_WHERE", "CSQ.HYGIENE.PREFER_TEXT"},
		RuleOptions: map[string]map[string]any{
			"CSQ.HYGIENE.PREFER_TEXT": {
				"style": "text",
			},
		},
		TreatWarningsAsErrors: false,
	}

	built, err := validation.BuildStaticValidatorConfig(
		base,
		[]string{"CSQ.BREAKING.DROP_COLUMN"},
		[]string{"CSQ.HYGIENE.PREFER_TEXT"},
		true,
	)
	assert.NoError(t, err)

	assert.Contains(t, built.EnabledRules, "CSQ.SAFETY.MISSING_WHERE")
	assert.Contains(t, built.EnabledRules, "CSQ.BREAKING.DROP_COLUMN")
	assert.NotContains(t, built.EnabledRules, "CSQ.HYGIENE.PREFER_TEXT")
	assert.Equal(t, true, built.TreatWarningsAsErrors)
	assert.Equal(t, "text", built.RuleOptions["CSQ.HYGIENE.PREFER_TEXT"]["style"])
}
