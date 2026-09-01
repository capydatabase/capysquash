package validation

import "testing"

func TestCompareLineSets(t *testing.T) {
	t.Parallel()

	t.Run("identical", func(t *testing.T) {
		t.Parallel()

		source := []string{"a", "b", "c"}
		target := []string{"c", "b", "a"}

		diff := compareLineSets(source, target)
		if diff.HasDifferences {
			t.Fatalf("expected no differences, got: %v", diff.Differences)
		}
	})

	t.Run("detects additions and removals", func(t *testing.T) {
		t.Parallel()

		source := []string{"a", "b"}
		target := []string{"b", "c"}

		diff := compareLineSets(source, target)
		if !diff.HasDifferences {
			t.Fatal("expected differences")
		}

		if len(diff.Differences) != 2 {
			t.Fatalf("expected 2 differences, got %d: %v", len(diff.Differences), diff.Differences)
		}
	})
}

func TestCompareSchemasDirectly(t *testing.T) {
	t.Parallel()

	t.Run("equivalent after normalization", func(t *testing.T) {
		t.Parallel()

		schema1 := `CREATE TABLE users (id BIGINT, name TEXT);`
		schema2 := `
CREATE   TABLE   users(
  id bigint,
  name text
);
`

		diff, err := CompareSchemasDirectly(schema1, schema2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if diff.HasDifferences {
			t.Fatalf("expected no differences, got: %v", diff.Differences)
		}
	})

	t.Run("detects semantic differences", func(t *testing.T) {
		t.Parallel()

		schema1 := `CREATE TABLE users (id BIGINT);`
		schema2 := `CREATE TABLE users (id BIGINT, email TEXT);`

		diff, err := CompareSchemasDirectly(schema1, schema2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !diff.HasDifferences {
			t.Fatal("expected differences")
		}
	})
}
