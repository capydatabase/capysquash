package validation

import (
	"testing"

	pgquery "github.com/pganalyze/pg_query_go/v6"
)

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

func TestCompareCatalogSnapshots(t *testing.T) {
	t.Parallel()

	original := &CatalogSnapshot{
		ContractVersion:   CatalogSnapshotContractVersion,
		PostgreSQLVersion: "17.6",
		Signature:         []string{"column|public.users|0001|id|bigint|notnull=true|default="},
	}
	candidate := &CatalogSnapshot{
		ContractVersion:   CatalogSnapshotContractVersion,
		PostgreSQLVersion: "17.6",
		Signature:         append([]string(nil), original.Signature...),
	}

	diff, err := CompareCatalogSnapshots(original, candidate)
	if err != nil {
		t.Fatalf("CompareCatalogSnapshots() error = %v", err)
	}
	if diff.HasDifferences {
		t.Fatalf("expected matching snapshots, got %v", diff.Differences)
	}

	candidate.Signature = append(candidate.Signature, "sequence|public.users_id_seq|sha256:abc")
	diff, err = CompareCatalogSnapshots(original, candidate)
	if err != nil {
		t.Fatalf("CompareCatalogSnapshots() error = %v", err)
	}
	if !diff.HasDifferences {
		t.Fatal("expected added sequence to be detected")
	}
}

func TestCompareCatalogSnapshotsRejectsIncompatibleInputs(t *testing.T) {
	t.Parallel()

	valid := &CatalogSnapshot{ContractVersion: CatalogSnapshotContractVersion, PostgreSQLVersion: "17.6"}
	if _, err := CompareCatalogSnapshots(
		&CatalogSnapshot{ContractVersion: "unknown", PostgreSQLVersion: "17.6"},
		valid,
	); err == nil {
		t.Fatal("expected unsupported contract to be rejected")
	}
	if _, err := CompareCatalogSnapshots(
		valid,
		&CatalogSnapshot{ContractVersion: CatalogSnapshotContractVersion, PostgreSQLVersion: "16.10"},
	); err == nil {
		t.Fatal("expected PostgreSQL version mismatch to be rejected")
	}
}

func TestCatalogSignatureQueriesParse(t *testing.T) {
	t.Parallel()

	queries := map[string]string{
		"sequences":    signatureSequencesQuery,
		"types":        signatureTypesQuery,
		"relations":    signatureRelationsQuery,
		"ownership":    signatureOwnershipQuery,
		"policy_roles": signaturePolicyRolesQuery,
		"grants":       signatureGrantsQuery,
		"comments":     signatureCommentsQuery,
	}
	for name, query := range queries {
		name, query := name, query
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := pgquery.Parse(query); err != nil {
				t.Fatalf("catalog query does not parse: %v", err)
			}
		})
	}
}

func TestNormalizeAllowedSchemas(t *testing.T) {
	t.Parallel()

	got := normalizeAllowedSchemas([]string{" extensions ", "", "capydb", "extensions"})
	want := []string{"extensions", "capydb"}
	if len(got) != len(want) {
		t.Fatalf("normalizeAllowedSchemas() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeAllowedSchemas() = %v, want %v", got, want)
		}
	}
}
