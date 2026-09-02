package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	pkgplugins "github.com/capy-base/pgsquash-engine/pkg/plugins"
)

// TestMain registers the default plugin set once for the whole package.
// Plugin registration is idempotent, so this is safe even if another
// package in the same binary already registered them.
func TestMain(m *testing.M) {
	if err := pkgplugins.RegisterDefault(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to register default plugins: %v\n", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// resetCLIState resets every flag on the package-global cobra command tree to
// its declared default and clears the Changed marker.
//
// This is required because internal/cli uses package-global commands and
// package-global flag variables: command handlers mutate the bound variables
// directly (e.g. runSafeWorkflow overwrites safetyLevel, runSquash disables
// enableBackup under --dry-run), and pflag keeps both values and Changed state
// across Execute calls within one process. Without this reset, sequential
// tests would leak state into each other. Tests in this package therefore
// must NOT use t.Parallel().
func resetCLIState(t *testing.T) {
	t.Helper()

	resetFlagSet := func(fs *pflag.FlagSet) {
		fs.VisitAll(func(f *pflag.Flag) {
			if sv, ok := f.Value.(pflag.SliceValue); ok {
				if err := sv.Replace(nil); err != nil {
					t.Fatalf("failed to reset slice flag %q: %v", f.Name, err)
				}
			} else if err := f.Value.Set(f.DefValue); err != nil {
				t.Fatalf("failed to reset flag %q to default %q: %v", f.Name, f.DefValue, err)
			}
			f.Changed = false
		})
	}

	var resetCommand func(c *cobra.Command)
	resetCommand = func(c *cobra.Command) {
		resetFlagSet(c.Flags())
		resetFlagSet(c.PersistentFlags())
		for _, sub := range c.Commands() {
			resetCommand(sub)
		}
	}
	resetCommand(rootCmd)
}

// executeCLI runs the real cobra command tree end-to-end (the same path the
// pgsquash binary uses via cli.Execute) with a clean flag state.
func executeCLI(t *testing.T, args ...string) error {
	t.Helper()
	resetCLIState(t)
	rootCmd.SetArgs(args)
	err := Execute()
	rootCmd.SetArgs(nil)
	return err
}

// fixtureMigration is one small migration file for end-to-end tests.
type fixtureMigration struct {
	name    string
	content string
}

// fixtureMigrations is a minimal, dependency-ordered migration set that
// exercises CREATE + ALTER consolidation without needing Docker or a database.
var fixtureMigrations = []fixtureMigration{
	{
		name: "001_create_users.sql",
		content: `CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    created_at TIMESTAMPTZ DEFAULT now()
);
`,
	},
	{
		name:    "002_add_full_name.sql",
		content: "ALTER TABLE users ADD COLUMN full_name TEXT;\n",
	},
	{
		name:    "003_add_email_index.sql",
		content: "CREATE INDEX idx_users_email ON users (email);\n",
	},
}

// writeFixtureMigrations writes the fixture migration set into dir and
// returns the file paths in migration order.
func writeFixtureMigrations(t *testing.T, dir string) []string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create migrations dir: %v", err)
	}
	paths := make([]string, 0, len(fixtureMigrations))
	for _, m := range fixtureMigrations {
		path := filepath.Join(dir, m.name)
		if err := os.WriteFile(path, []byte(m.content), 0o644); err != nil {
			t.Fatalf("failed to write fixture migration %s: %v", m.name, err)
		}
		paths = append(paths, path)
	}
	return paths
}

// snapshotTree captures every entry under root: directories map to "dir",
// files map to the SHA-256 of their content. Used to prove --dry-run purity.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	snap := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			snap[rel] = "dir"
			return nil
		}
		snap[rel] = fileSHA256(t, path)
		return nil
	})
	if err != nil {
		t.Fatalf("failed to snapshot tree at %s: %v", root, err)
	}
	return snap
}

// fileSHA256 returns the hex SHA-256 of a file's content.
func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// captureStderr runs fn while capturing everything written to os.Stderr.
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("failed to close pipe writer: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("failed to read captured stderr: %v", err)
	}
	return string(data)
}
