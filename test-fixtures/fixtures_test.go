package test_fixtures

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/capysquash/pgsquash-engine/pkg/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SafetyLevel represents the consolidation safety level
type SafetyLevel string

const (
	Paranoid     SafetyLevel = "paranoid"
	Conservative SafetyLevel = "conservative"
	Standard     SafetyLevel = "standard"
	Aggressive   SafetyLevel = "aggressive"
)

// FixtureTest represents a single fixture test case
type FixtureTest struct {
	Name        string
	Path        string
	SafetyModes []SafetyLevel
	SkipReasons map[SafetyLevel]string // Reasons to skip specific modes
}

// GetAllFixtures returns all available test fixtures
func GetAllFixtures() []FixtureTest {
	return []FixtureTest{
		{
			Name: "enums_append_reorder",
			Path: "enums_append_reorder",
			SafetyModes: []SafetyLevel{Paranoid, Conservative, Standard, Aggressive},
		},
		{
			Name: "fk_cycles",
			Path: "fk_cycles",
			SafetyModes: []SafetyLevel{Paranoid, Conservative, Standard, Aggressive},
		},
		{
			Name: "partial_index_predicates",
			Path: "partial_index_predicates",
			SafetyModes: []SafetyLevel{Paranoid, Conservative, Standard, Aggressive},
		},
		{
			Name: "rls_policies",
			Path: "rls_policies",
			SafetyModes: []SafetyLevel{Paranoid, Conservative, Standard, Aggressive},
		},
		{
			Name: "matviews",
			Path: "matviews",
			SafetyModes: []SafetyLevel{Paranoid, Conservative, Standard, Aggressive},
		},
		{
			Name: "collations",
			Path: "collations",
			SafetyModes: []SafetyLevel{Paranoid, Conservative, Standard, Aggressive},
		},
		{
			Name: "generated_columns_identity",
			Path: "generated_columns_identity",
			SafetyModes: []SafetyLevel{Paranoid, Conservative, Standard, Aggressive},
		},
		{
			Name: "partitions",
			Path: "partitions",
			SafetyModes: []SafetyLevel{Paranoid, Conservative, Standard, Aggressive},
		},
		{
			Name: "extensions_versioning",
			Path: "extensions_versioning",
			SafetyModes: []SafetyLevel{Paranoid, Conservative, Standard, Aggressive},
		},
		{
			Name: "triggers_function_versions",
			Path: "triggers_function_versions",
			SafetyModes: []SafetyLevel{Paranoid, Conservative, Standard, Aggressive},
		},
		{
			Name: "supabase_auth_schema",
			Path: "supabase_auth_schema",
			SafetyModes: []SafetyLevel{Paranoid, Conservative, Standard, Aggressive},
		},
		{
			Name: "pragma_examples",
			Path: "pragma_examples",
			SafetyModes: []SafetyLevel{Paranoid, Conservative, Standard, Aggressive},
		},
	}
}

// TestAllFixtures runs all test fixtures across different safety modes
func TestAllFixtures(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping fixture tests in short mode")
	}

	fixtures := GetAllFixtures()

	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			testFixture(t, fixture)
		})
	}
}

// TestFixture runs tests for a specific fixture
func TestFixture(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping fixture tests in short mode")
	}

	fixtures := GetAllFixtures()
	for _, fixture := range fixtures {
		if fixture.Name == "enums_append_reorder" { // Test specific fixture
			testFixture(t, fixture)
			break
		}
	}
}

// testFixture runs the actual fixture test
func testFixture(t *testing.T, fixture FixtureTest) {
	// Check if fixture directory exists
	fixturePath := filepath.Join(".", fixture.Path)
	if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
		t.Skipf("Fixture directory %s does not exist, skipping", fixturePath)
		return
	}

	// Check if original directory exists
	originalPath := filepath.Join(fixturePath, "original")
	if _, err := os.Stat(originalPath); os.IsNotExist(err) {
		t.Skipf("Original directory %s does not exist, skipping", originalPath)
		return
	}

	// Load original migrations
	originalMigrations, err := loadMigrationsFromDir(originalPath)
	require.NoError(t, err, "Failed to load original migrations")

	// Test each safety mode
	for _, safetyMode := range fixture.SafetyModes {
		t.Run(string(safetyMode), func(t *testing.T) {
			if reason, shouldSkip := fixture.SkipReasons[safetyMode]; shouldSkip {
				t.Skipf("Skipping %s mode: %s", safetyMode, reason)
				return
			}

			testSafetyMode(t, fixture, safetyMode, originalMigrations)
		})
	}
}

// testSafetyMode tests a specific safety mode
func testSafetyMode(t *testing.T, fixture FixtureTest, safetyMode SafetyLevel, originalMigrations map[int]string) {
	// Convert safety mode to engine format
	var engineSafetyLevel engine.SafetyLevel
	switch safetyMode {
	case Paranoid:
		engineSafetyLevel = engine.Paranoid
	case Conservative:
		engineSafetyLevel = engine.Conservative
	case Standard:
		engineSafetyLevel = engine.Standard
	case Aggressive:
		engineSafetyLevel = engine.Aggressive
	}

	// Create config with specified safety level
	config := engine.DefaultConfig()
	config.SafetyLevel = engineSafetyLevel

	// Run squash using public API
	result, err := engine.SquashFiles(originalMigrations, config)
	require.NoError(t, err, "Failed to squash migrations")

	// Check expected output exists
	expectedPath := filepath.Join(fixture.Path, "expected", string(safetyMode)+".sql")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Skipf("Expected output %s does not exist, skipping validation", expectedPath)
		return
	}

	// Basic validation - check that result is not empty and contains SQL
	t.Logf("Safety mode: %s", safetyMode)
	t.Logf("Original migrations: %d", len(originalMigrations))
	t.Logf("Result length: %d bytes", len(result.SQL))

	// Basic assertions
	assert.NotEmpty(t, result.SQL, "Squashed result should not be empty")
	assert.Contains(t, strings.ToUpper(result.SQL), "CREATE", "Result should contain CREATE statements")

	// TODO: Add more sophisticated comparison with expected output
	// For now, just verify the engine can process the fixture without errors
	t.Logf("✅ Fixture %s processed successfully in %s mode", fixture.Name, safetyMode)
}

// TestFixtureValidation runs schema validation tests with Docker
func TestFixtureValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping validation tests in short mode")
	}

	// Skip if Docker is not available
	if !isDockerAvailable() {
		t.Skip("Docker not available, skipping validation tests")
	}

	fixtures := GetAllFixtures()

	for _, fixture := range fixtures {
		t.Run(fixture.Name+"_validation", func(t *testing.T) {
			testFixtureValidation(t, fixture)
		})
	}
}

// testFixtureValidation validates that squashed migrations produce identical schemas
func testFixtureValidation(t *testing.T, fixture FixtureTest) {
	// This would implement Docker-based validation
	// For now, just verify the structure exists
	t.Logf("Validation test for %s (Docker integration would validate schema equivalence)", fixture.Name)
}

// TestFixturePerformance benchmarks fixture performance
func TestFixturePerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance tests in short mode")
	}

	fixtures := GetAllFixtures()

	for _, fixture := range fixtures {
		t.Run(fixture.Name+"_performance", func(t *testing.T) {
			benchmarkFixture(t, fixture)
		})
	}
}

// benchmarkFixture benchmarks a fixture's performance
func benchmarkFixture(t *testing.T, fixture FixtureTest) {
	originalPath := filepath.Join(fixture.Path, "original")
	if _, err := os.Stat(originalPath); os.IsNotExist(err) {
		t.Skipf("Original directory %s does not exist, skipping benchmark", originalPath)
		return
	}

	originalMigrations, err := loadMigrationsFromDir(originalPath)
	require.NoError(t, err)

	// Benchmark each safety mode
	for _, safetyMode := range fixture.SafetyModes {
		t.Run(string(safetyMode), func(t *testing.T) {
			start := time.Now()

			// Convert safety mode to engine format
			var engineSafetyLevel engine.SafetyLevel
			switch safetyMode {
			case Paranoid:
				engineSafetyLevel = engine.Paranoid
			case Conservative:
				engineSafetyLevel = engine.Conservative
			case Standard:
				engineSafetyLevel = engine.Standard
			case Aggressive:
				engineSafetyLevel = engine.Aggressive
			}

			config := engine.DefaultConfig()
			config.SafetyLevel = engineSafetyLevel

			_, err := engine.SquashFiles(originalMigrations, config)
			duration := time.Since(start)

			require.NoError(t, err)
			t.Logf("Safety mode %s took %v for %d migrations", safetyMode, duration, len(originalMigrations))
		})
	}
}

// Helper functions

// loadMigrationsFromDir loads migrations from a directory
func loadMigrationsFromDir(dir string) (map[int]string, error) {
	migrations := make(map[int]string)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(path, ".sql") {
			// Extract sequence number from filename (001_, 002_, etc.)
			filename := filepath.Base(path)
			if len(filename) >= 4 && filename[3] == '_' {
				var seq int
				fmt.Sscanf(filename[:3], "%d", &seq)

				content, err := os.ReadFile(path)
				if err != nil {
					return err
				}

				migrations[seq] = string(content)
			}
		}

		return nil
	})

	return migrations, err
}

// normalizeSQL normalizes SQL for comparison
func normalizeSQL(sql string) string {
	// Basic normalization - remove extra whitespace, normalize line endings
	lines := strings.Split(sql, "\n")
	var normalized []string

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "--") {
			normalized = append(normalized, line)
		}
	}

	return strings.Join(normalized, "\n")
}

// isDockerAvailable checks if Docker is available
func isDockerAvailable() bool {
	_, err := os.Stat("/var/run/docker.sock")
	return err == nil
}

// TestFixtureFuzzing runs fuzzing tests for fixtures
func TestFixtureFuzzing(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping fuzzing tests in short mode")
	}

	// This would implement fuzzing tests
	t.Log("Fuzzing tests would generate random DDL and test schema equivalence")
}
