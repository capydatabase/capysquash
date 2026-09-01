package validation

import (
	"os"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

type staticFixtureCase struct {
	Name          string   `yaml:"name"`
	SQL           string   `yaml:"sql"`
	ExpectedCodes []string `yaml:"expected_codes"`
}

type staticFixtureSuite struct {
	Cases []staticFixtureCase `yaml:"cases"`
}

func TestStaticValidatorYAMLFixtures(t *testing.T) {
	rawFixture, err := os.ReadFile("testdata/static_rules_fixtures.yaml")
	assert.NoError(t, err)

	var suite staticFixtureSuite
	err = yaml.Unmarshal(rawFixture, &suite)
	assert.NoError(t, err)
	if !assert.NotEmpty(t, suite.Cases, "fixture suite should contain at least one case") {
		return
	}

	validator := NewStaticValidator(nil)

	for _, fixture := range suite.Cases {
		t.Run(fixture.Name, func(t *testing.T) {
			violations, checkErr := validator.Check(fixture.SQL)
			assert.NoError(t, checkErr)

			actualCodes := make([]string, 0, len(violations))
			for _, violation := range violations {
				actualCodes = append(actualCodes, violation.Code)
			}

			expectedCodes := append([]string(nil), fixture.ExpectedCodes...)
			sort.Strings(actualCodes)
			sort.Strings(expectedCodes)

			assert.ElementsMatch(t, expectedCodes, actualCodes)
		})
	}
}
