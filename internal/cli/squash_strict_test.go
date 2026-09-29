package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeLintFindingMigrations writes a migration set whose second file has a
// pre-flight finding: an INT column (CSQ.HYGIENE.PREFER_BIGINT).
func writeLintFindingMigrations(t *testing.T, dir string) []string {
	t.Helper()
	files := writeFixtureMigrations(t, dir)
	finding := filepath.Join(dir, "003_create_counters.sql")
	if err := os.WriteFile(finding, []byte("CREATE TABLE counters (id SERIAL PRIMARY KEY, hits INT);\n"), 0o644); err != nil {
		t.Fatalf("write migration: %v", err)
	}
	return append(files, finding)
}

func TestSquashStrictAbortsOnPreflightFindings(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("PROD_DB_DSN", "")
	files := writeLintFindingMigrations(t, filepath.Join(root, "migrations"))

	args := append([]string{"squash"}, files...)
	args = append(args, "--output", filepath.Join(root, "out"), "--dry-run", "--no-validate", "--i-know-what-im-doing", "--strict")

	err := executeCLI(t, args...)
	if err == nil {
		t.Fatal("squash --strict succeeded despite a pre-flight finding")
	}
	for _, want := range []string{"strict mode", "CSQ.HYGIENE.PREFER_BIGINT"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error does not mention %q: %v", want, err)
		}
	}
}

func TestSquashWithoutStrictKeepsPreflightFindingsAsWarnings(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("PROD_DB_DSN", "")
	files := writeLintFindingMigrations(t, filepath.Join(root, "migrations"))

	args := append([]string{"squash"}, files...)
	args = append(args, "--output", filepath.Join(root, "out"), "--dry-run", "--no-validate", "--i-know-what-im-doing")

	if err := executeCLI(t, args...); err != nil {
		t.Fatalf("squash without --strict failed: %v", err)
	}
}
