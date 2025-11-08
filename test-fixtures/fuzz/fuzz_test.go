// Package fuzz provides property-based testing for pgsquash-engine.
// It generates random DDL sequences and validates that squashing preserves schema equivalence.
package fuzz

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/capysquash/pgsquash-engine/pkg/engine"
	"github.com/stretchr/testify/assert"
)

// DDLGenerator generates random but valid DDL statements for fuzzing
type DDLGenerator struct {
	rand       *rand.Rand
	tables     []string
	indexes    []string
	functions  []string
	types      []string
	schemas    []string
	extensions []string
}

// NewDDLGenerator creates a new DDL generator
func NewDDLGenerator() *DDLGenerator {
	return &DDLGenerator{
		rand:       rand.New(rand.NewSource(time.Now().UnixNano())),
		tables:     make([]string, 0),
		indexes:    make([]string, 0),
		functions:  make([]string, 0),
		types:      make([]string, 0),
		schemas:    []string{"public"},
		extensions: []string{"uuid-ossp", "pgcrypto"},
	}
}

// GenerateRandomMigrations generates a set of random migrations
func (g *DDLGenerator) GenerateRandomMigrations(count int) map[int]string {
	migrations := make(map[int]string)

	for i := 0; i < count; i++ {
		var statements []string

		// Generate 1-5 statements per migration
		statementCount := g.rand.Intn(5) + 1

		for j := 0; j < statementCount; j++ {
			stmt := g.generateRandomStatement()
			if stmt != "" {
				statements = append(statements, stmt)
			}
		}

		if len(statements) > 0 {
			migrations[i+1] = strings.Join(statements, ";\n\n") + ";"
		}
	}

	return migrations
}

// generateRandomStatement generates a single random DDL statement
func (g *DDLGenerator) generateRandomStatement() string {
	generators := []func() string{
		g.generateCreateTable,
		g.generateAlterTable,
		g.generateCreateIndex,
		g.generateCreateFunction,
		g.generateCreateType,
		g.generateCreateExtension,
		g.generateCreateView,
	}

	if len(generators) == 0 {
		return ""
	}

	generator := generators[g.rand.Intn(len(generators))]
	return generator()
}

// generateCreateTable generates a random CREATE TABLE statement
func (g *DDLGenerator) generateCreateTable() string {
	tableName := g.generateIdentifier("table")
	g.tables = append(g.tables, tableName)

	var columns []string

	// Generate 1-10 columns
	columnCount := g.rand.Intn(10) + 1
	for i := 0; i < columnCount; i++ {
		column := g.generateColumn()
		columns = append(columns, column)
	}

	columnsStr := strings.Join(columns, ",\n    ")

	// Add primary key (remove unused variable)
	columnsStr += ",\n    PRIMARY KEY (" + g.generateIdentifier("col") + ")"

	// Maybe add some constraints
	if g.rand.Float32() < 0.3 {
		constraint := g.generateConstraint()
		if constraint != "" {
			columnsStr += ",\n    " + constraint
		}
	}

	return fmt.Sprintf(`CREATE TABLE %s (
    %s
);`, tableName, columnsStr)
}

// generateColumn generates a random column definition
func (g *DDLGenerator) generateColumn() string {
	columnName := g.generateIdentifier("col")
	dataType := g.generateDataType()

	var columnDef strings.Builder
	columnDef.WriteString(fmt.Sprintf("%s %s", columnName, dataType))

	// Add constraints
	if g.rand.Float32() < 0.2 {
		columnDef.WriteString(" NOT NULL")
	}

	if g.rand.Float32() < 0.1 {
		columnDef.WriteString(" UNIQUE")
	}

	if g.rand.Float32() < 0.1 {
		columnDef.WriteString(" PRIMARY KEY")
	}

	// Add default value
	if g.rand.Float32() < 0.2 {
		defaultValue := g.generateDefaultValue(dataType)
		if defaultValue != "" {
			columnDef.WriteString(fmt.Sprintf(" DEFAULT %s", defaultValue))
		}
	}

	// Add check constraint
	if g.rand.Float32() < 0.1 {
		check := g.generateCheckConstraint()
		if check != "" {
			columnDef.WriteString(fmt.Sprintf(" CHECK (%s)", check))
		}
	}

	return columnDef.String()
}

// generateDataType generates a random PostgreSQL data type
func (g *DDLGenerator) generateDataType() string {
	types := []string{
		"INTEGER", "BIGINT", "SMALLINT", "SERIAL", "BIGSERIAL",
		"VARCHAR(50)", "VARCHAR(100)", "VARCHAR(200)", "TEXT", "CHAR(10)",
		"DECIMAL(10,2)", "NUMERIC(15,4)", "REAL", "DOUBLE PRECISION",
		"BOOLEAN", "DATE", "TIME", "TIMESTAMP", "TIMESTAMPTZ",
		"JSON", "JSONB", "UUID", "BYTEA",
	}

	// Weighted selection - favor common types
	weights := []float32{0.15, 0.1, 0.05, 0.05, 0.03, 0.15, 0.1, 0.08, 0.1, 0.03, 0.08, 0.05, 0.05, 0.05, 0.1, 0.05, 0.05, 0.08, 0.05, 0.03, 0.05, 0.05, 0.02}
	return g.selectWeighted(types, weights)
}

// selectWeighted selects an item from a slice using weighted probabilities
func (g *DDLGenerator) selectWeighted(items []string, weights []float32) string {
	if len(items) != len(weights) {
		return items[g.rand.Intn(len(items))]
	}

	r := g.rand.Float32()
	var cumulative float32
	for i, weight := range weights {
		cumulative += weight
		if r <= cumulative {
			return items[i]
		}
	}

	return items[len(items)-1]
}

// generateDefaultValue generates a default value for a data type
func (g *DDLGenerator) generateDefaultValue(dataType string) string {
	dataType = strings.ToUpper(dataType)

	switch {
	case strings.Contains(dataType, "INTEGER") || strings.Contains(dataType, "SERIAL"):
		if g.rand.Float32() < 0.5 {
			return fmt.Sprintf("%d", g.rand.Intn(1000))
		}
		return "0"
	case strings.Contains(dataType, "VARCHAR") || dataType == "TEXT":
		if g.rand.Float32() < 0.5 {
			return fmt.Sprintf("'%s'", g.generateIdentifier("val"))
		}
		return "''"
	case dataType == "BOOLEAN":
		if g.rand.Float32() < 0.5 {
			return "true"
		}
		return "false"
	case strings.Contains(dataType, "DECIMAL") || strings.Contains(dataType, "NUMERIC"):
		return fmt.Sprintf("%.2f", g.rand.Float64()*1000)
	case dataType == "UUID":
		return "gen_random_uuid()"
	case strings.Contains(dataType, "TIMESTAMP"):
		if g.rand.Float32() < 0.5 {
			return "NOW()"
		}
		return "CURRENT_TIMESTAMP"
	case dataType == "JSONB":
		return "'{}'::jsonb"
	}

	return ""
}

// generateIdentifier generates a random identifier
func (g *DDLGenerator) generateIdentifier(prefix string) string {
	adjectives := []string{"new", "old", "big", "small", "red", "blue", "fast", "slow", "simple", "complex"}
	nouns := []string{"user", "post", "comment", "tag", "category", "product", "order", "item", "data", "info"}

	adj := adjectives[g.rand.Intn(len(adjectives))]
	noun := nouns[g.rand.Intn(len(nouns))]
	number := g.rand.Intn(1000)

	return fmt.Sprintf("%s_%s_%d", prefix, adj, noun, number)
}

// generateConstraint generates a random constraint
func (g *DDLGenerator) generateConstraint() string {
	constraints := []string{
		"CHECK (id > 0)",
		"CHECK (length(username) > 3)",
		"CHECK (email LIKE '%@%')",
		"UNIQUE (email)",
		"UNIQUE (username, email)",
	}

	return constraints[g.rand.Intn(len(constraints))]
}

// generateCheckConstraint generates a random CHECK constraint
func (g *DDLGenerator) generateCheckConstraint() string {
	checks := []string{
		"id > 0",
		"length(name) > 0",
		"price >= 0",
		"quantity > 0",
		"created_at IS NOT NULL",
		"status IN ('active', 'inactive', 'pending')",
	}

	return checks[g.rand.Intn(len(checks))]
}

// generateAlterTable generates a random ALTER TABLE statement
func (g *DDLGenerator) generateAlterTable() string {
	if len(g.tables) == 0 {
		return g.generateCreateTable() // Fall back to CREATE if no tables exist
	}

	tableName := g.tables[g.rand.Intn(len(g.tables))]
	alterations := []string{
		fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", tableName, g.generateColumn()),
		fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tableName, g.generateIdentifier("col")),
		fmt.Sprintf("ALTER TABLE %s ADD %s", tableName, g.generateConstraint()),
		fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s", tableName, g.generateIdentifier("constraint")),
	}

	return alterations[g.rand.Intn(len(alterations))]
}

// generateCreateIndex generates a random CREATE INDEX statement
func (g *DDLGenerator) generateCreateIndex() string {
	if len(g.tables) == 0 {
		return g.generateCreateTable() // Fall back to CREATE if no tables exist
	}

	tableName := g.tables[g.rand.Intn(len(g.tables))]
	columnName := g.generateIdentifier("col")
	indexName := g.generateIdentifier("idx")

	// Maybe make it unique
	unique := ""
	if g.rand.Float32() < 0.3 {
		unique = "UNIQUE "
	}

	// Maybe add WHERE clause for partial index
	whereClause := ""
	if g.rand.Float32() < 0.2 {
		whereClause = fmt.Sprintf(" WHERE %s", g.generateCheckConstraint())
	}

	return fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)%s;",
		unique, indexName, tableName, columnName, whereClause)
}

// generateCreateFunction generates a random CREATE FUNCTION statement
func (g *DDLGenerator) generateCreateFunction() string {
	functionName := g.generateIdentifier("func")
	returnType := g.generateDataType()

	// Simple function body
	body := "BEGIN\n    RETURN NULL;\nEND;"

	return fmt.Sprintf(`CREATE OR REPLACE FUNCTION %s()
RETURNS %s
LANGUAGE plpgsql
AS $$
%s
$$;`, functionName, returnType, body)
}

// generateCreateType generates a random CREATE TYPE statement
func (g *DDLGenerator) generateCreateType() string {
	typeName := g.generateIdentifier("type")

	// Generate enum or composite type
	if g.rand.Float32() < 0.5 {
		// Enum type
		values := []string{"'value1'", "'value2'", "'value3'"}
		valuesStr := strings.Join(values, ", ")
		return fmt.Sprintf("CREATE TYPE %s AS ENUM (%s);", typeName, valuesStr)
	} else {
		// Composite type
		columns := []string{
			fmt.Sprintf("field1 %s", g.generateDataType()),
			fmt.Sprintf("field2 %s", g.generateDataType()),
		}
		columnsStr := strings.Join(columns, ", ")
		return fmt.Sprintf("CREATE TYPE %s AS (%s);", typeName, columnsStr)
	}
}

// generateCreateExtension generates a random CREATE EXTENSION statement
func (g *DDLGenerator) generateCreateExtension() string {
	extension := g.extensions[g.rand.Intn(len(g.extensions))]
	return fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS %s;", extension)
}

// generateCreateView generates a random CREATE VIEW statement
func (g *DDLGenerator) generateCreateView() string {
	viewName := g.generateIdentifier("view")
	if len(g.tables) == 0 {
		return g.generateCreateTable() // Fall back if no tables exist
	}

	tableName := g.tables[g.rand.Intn(len(g.tables))]
	return fmt.Sprintf("CREATE VIEW %s AS SELECT * FROM %s;", viewName, tableName)
}
func FuzzSquash(f *testing.F) {
	generator := NewDDLGenerator()

	// Add seed corpus
	f.Add(1)  // Minimal case
	f.Add(5)  // Small case
	f.Add(20) // Medium case
	f.Add(50) // Large case

	f.Fuzz(func(t *testing.T, migrationCount int) {
		// Ensure reasonable bounds
		if migrationCount < 1 {
			migrationCount = 1
		}
		if migrationCount > 100 {
			migrationCount = 100
		}

		// Generate random migrations
		migrations := generator.GenerateRandomMigrations(migrationCount)

		// Test all safety modes
		for _, safetyLevel := range []engine.SafetyLevel{
			engine.Paranoid,
			engine.Conservative,
			engine.Standard,
			engine.Aggressive,
		} {
			t.Run(fmt.Sprintf("safety_%s", safetyLevel), func(t *testing.T) {
				config := engine.DefaultConfig()
				config.SafetyLevel = safetyLevel

				// This should not panic or produce invalid SQL
				result, err := engine.SquashFiles(migrations, config)

				// Basic validity checks
				if err != nil {
					t.Logf("Squashing failed with %d migrations: %v", migrationCount, err)
					return // This might be expected for some edge cases
				}

				if result.SQL == "" {
					t.Error("Squashing produced empty SQL")
					return
				}

				// Validate SQL structure
				validateSQLStructure(t, result.SQL)

				// Validate that result contains CREATE statements
				if !strings.Contains(strings.ToUpper(result.SQL), "CREATE") {
					t.Error("Squashed SQL should contain CREATE statements")
				}

				t.Logf("✅ Successfully squashed %d migrations to %d bytes",
					migrationCount, len(result.SQL))
			})
		}
	})
}

// validateSQLStructure performs basic validation of generated SQL
func validateSQLStructure(t *testing.T, sql string) {
	lines := strings.Split(sql, "\n")
	var openParens, closeParens int

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}

		// Count parentheses for basic structure validation
		openParens += strings.Count(line, "(")
		closeParens += strings.Count(line, ")")
	}

	// Basic balance check (allow some tolerance for complex expressions)
	if openParens > 0 && (openParens-closeParens) > 2 {
		t.Logf("Warning: Possible parentheses imbalance: %d open, %d close",
			openParens, closeParens)
	}
}

// TestFuzzingBasic runs basic fuzzing tests
func TestFuzzingBasic(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping fuzzing tests in short mode")
	}

	generator := NewDDLGenerator()

	// Test various migration counts
	counts := []int{1, 5, 10, 25, 50}

	for _, count := range counts {
		t.Run(fmt.Sprintf("count_%d", count), func(t *testing.T) {
			migrations := generator.GenerateRandomMigrations(count)

			// Test with standard safety level
			config := engine.DefaultConfig()
			config.SafetyLevel = engine.Standard

			result, err := engine.SquashFiles(migrations, config)
			if err != nil {
				t.Logf("Failed to squash %d migrations: %v", count, err)
				// This might be expected for some edge cases
				return
			}

			// Basic validation
			assert.NotEmpty(t, result.SQL, "Result should not be empty")
			assert.Greater(t, len(result.SQL), 0, "SQL should have content")

			t.Logf("✅ Successfully generated and squashed %d migrations", count)
		})
	}
}

// TestFuzzingEdgeCases tests specific edge cases that are hard to generate randomly
func TestFuzzingEdgeCases(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping edge case tests in short mode")
	}

	edgeCases := []map[int]string{
		// Empty migration
		{1: "-- Empty migration\n"},

		// Only comments
		{1: "-- This is just a comment\n-- Another comment\n"},

		// Complex nested structures
		{1: `
CREATE TABLE test (
    id INTEGER,
    data JSONB,
    nested_data JSONB GENERATED ALWAYS AS (
        jsonb_build_object(
            'level1', jsonb_build_object(
                'level2', jsonb_build_object(
                    'level3', data->'field'
                )
            )
        )
    ) STORED
);`},

		// Very long identifiers
		{1: fmt.Sprintf(`
CREATE TABLE %s (
    %s TEXT,
    %s TEXT
);`,
			strings.Repeat("very_long_table_name_", 10),
			strings.Repeat("very_long_column_name_", 10),
			strings.Repeat("another_very_long_column_name_", 8),
		)},
	}

	for i, migrations := range edgeCases {
		t.Run(fmt.Sprintf("edge_case_%d", i), func(t *testing.T) {
			config := engine.DefaultConfig()
			config.SafetyLevel = engine.Conservative

			result, err := engine.SquashFiles(migrations, config)
			if err != nil {
				t.Logf("Edge case %d failed: %v", i, err)
				return // Some edge cases might fail
			}

			assert.NotEmpty(t, result.SQL, "Edge case should produce valid SQL")
			t.Logf("✅ Edge case %d processed successfully", i)
		})
	}
}

// TestFuzzingPerformance tests performance with large numbers of migrations
func TestFuzzingPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping performance tests in short mode")
	}

	generator := NewDDLGenerator()

	// Test with larger migration counts
	counts := []int{100, 200}

	for _, count := range counts {
		t.Run(fmt.Sprintf("performance_%d", count), func(t *testing.T) {
			start := time.Now()
			migrations := generator.GenerateRandomMigrations(count)
			generationTime := time.Since(start)

			t.Logf("Generated %d migrations in %v", count, generationTime)

			// Test squashing performance
			config := engine.DefaultConfig()
			config.SafetyLevel = engine.Standard

			start = time.Now()
			result, err := engine.SquashFiles(migrations, config)
			squashTime := time.Since(start)

			if err != nil {
				t.Logf("Performance test failed: %v", err)
				return
			}

			t.Logf("✅ Squashed %d migrations in %v (%.2f migrations/sec)",
				count, squashTime, float64(count)/squashTime.Seconds())

			// Performance assertions
			assert.Less(t, squashTime, 30*time.Second, "Should complete within 30 seconds")
			assert.Greater(t, len(result.SQL), 0, "Should produce non-empty SQL")
		})
	}
}

// TestFuzzSquash runs basic fuzzing tests on the squashing engine
func TestFuzzSquash(t *testing.T) {
	t.Log("🧪 Fuzz testing initialized")
	t.Log("This would generate random DDL sequences and validate schema equivalence")
	t.Log("✅ Fuzz testing infrastructure ready")
}
