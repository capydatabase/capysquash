package consolidation

import (
	"testing"

	"github.com/capysquash/pgsquash-engine/internal/types"
)

func TestMultipleCreateConsolidationRule_Apply(t *testing.T) {

	// We can't easily test Apply() without a mock Engine, but we can verify the internal logic
	// by testing mergeCumulativeCreateStatements indirectly via a new exported function or by
	// duplicating the logic in the test if the function is private.
	// Or, we can use the private access since we are in the same package.

	// Test case 1: IF NOT EXISTS should be ignored
	t.Run("IgnoreIfNotExists", func(t *testing.T) {
		stmts := []types.Statement{
			{SQL: "CREATE TABLE foo (col1 text)"},
			{SQL: "CREATE TABLE IF NOT EXISTS foo (col2 text)"},
		}

		result := mergeCumulativeCreateStatements(stmts, "foo")
		expected := "CREATE TABLE foo (col1 text)"

		if result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}
	})

	// Test case 2: Hard CREATE should override
	t.Run("HardCreateOverrides", func(t *testing.T) {
		stmts := []types.Statement{
			{SQL: "CREATE TABLE foo (col1 text)"},
			{SQL: "CREATE TABLE foo (col2 text)"},
		}

		result := mergeCumulativeCreateStatements(stmts, "foo")
		expected := "CREATE TABLE foo (col2 text)"

		if result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}
	})

	// Test case 3: Deep chain
	t.Run("DeepChain", func(t *testing.T) {
		stmts := []types.Statement{
			{SQL: "CREATE TABLE foo (col1 text)"},
			{SQL: "CREATE TABLE IF NOT EXISTS foo (col2 text)"}, // Skipped
			{SQL: "CREATE TABLE IF NOT EXISTS foo (col3 text)"}, // Skipped
			{SQL: "CREATE TABLE foo (col4 text)"},               // Overrides
			{SQL: "CREATE TABLE IF NOT EXISTS foo (col5 text)"}, // Skipped
		}

		result := mergeCumulativeCreateStatements(stmts, "foo")
		expected := "CREATE TABLE foo (col4 text)"

		if result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}
	})

	// Test case 4: Whitespace and case insensitivity
	t.Run("RegexRobustness", func(t *testing.T) {
		stmts := []types.Statement{
			{SQL: "CREATE TABLE foo (col1 text)"},
			{SQL: "create table if not exists foo (col2 text)"},
			{SQL: "CREATE  TEMP  TABLE  IF  NOT  EXISTS foo (col3 text)"},
		}

		result := mergeCumulativeCreateStatements(stmts, "foo")
		expected := "CREATE TABLE foo (col1 text)"

		if result != expected {
			t.Errorf("Expected '%s', got '%s'", expected, result)
		}
	})

	// Test case 5: Type mismatch robustness (though not logic of this function)
	// Checking complex SQL
	t.Run("ComplexSQL", func(t *testing.T) {
		sql1 := `CREATE TABLE public.market_configs (
          market_code TEXT PRIMARY KEY,
          name TEXT NOT NULL,
          currency TEXT NOT NULL
         )`
		sql2 := `CREATE TABLE IF NOT EXISTS market_configs (
          market_code TEXT PRIMARY KEY,
          market_name TEXT NOT NULL,
          currency TEXT NOT NULL
         )`

		stmts := []types.Statement{{SQL: sql1}, {SQL: sql2}}
		result := mergeCumulativeCreateStatements(stmts, "market_configs")

		if result != sql1 {
			t.Errorf("Expected sql1, got \n%s", result)
		}
	})
}
