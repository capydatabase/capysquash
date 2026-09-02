package cli

import (
	"bytes"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setTestVersionInfo stamps a distinctive version through the same seam the
// real binary uses (SetVersionInfo from main via ldflags) and restores the
// previous values when the test finishes.
func setTestVersionInfo(t *testing.T, version, buildDate, gitCommit string) {
	t.Helper()
	prevVersion := versionInfo.version
	prevBuildDate := versionInfo.buildDate
	prevGitCommit := versionInfo.gitCommit
	prevRootVersion := rootCmd.Version

	SetVersionInfo(version, buildDate, gitCommit)

	t.Cleanup(func() {
		versionInfo.version = prevVersion
		versionInfo.buildDate = prevBuildDate
		versionInfo.gitCommit = prevGitCommit
		rootCmd.Version = prevRootVersion
	})
}

// TestSquashDryRunIsPure proves --dry-run purity end-to-end: a squash run
// with --dry-run (even with --backup/--rollback requested) must create ZERO
// files or directories anywhere - no output dir, no .squashmap.json, no
// backup dir, no rollback plans, no staging leftovers.
func TestSquashDryRunIsPure(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("PROD_DB_DSN", "")

	migDir := filepath.Join(root, "migrations")
	files := writeFixtureMigrations(t, migDir)
	outDir := filepath.Join(root, "out")

	before := snapshotTree(t, root)

	args := append([]string{"squash"}, files...)
	args = append(args,
		"--output", outDir,
		"--dry-run",
		// Deliberately request every write-producing side feature: dry-run
		// must disable all of them instead of writing anything.
		"--backup",
		"--rollback",
		"--rollback-path", filepath.Join(root, "rollbacks"),
	)

	if err := executeCLI(t, args...); err != nil {
		t.Fatalf("squash --dry-run failed: %v", err)
	}

	after := snapshotTree(t, root)
	if !maps.Equal(before, after) {
		t.Errorf("--dry-run modified the filesystem:\n before: %v\n after:  %v", before, after)
	}

	for _, path := range []string{
		outDir,
		filepath.Join(outDir, ".squashmap.json"),
		filepath.Join(root, "rollbacks"),
		filepath.Join(root, "squashed"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("--dry-run must not create %s (stat err: %v)", path, err)
		}
	}

	// No staging directory may be left behind either.
	staging, err := filepath.Glob(filepath.Join(root, ".pgsquash-staging-*"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(staging) != 0 {
		t.Errorf("--dry-run left staging directories behind: %v", staging)
	}
}

func TestSquashDryRunJSONReturnsOnlyGeneratedSQLArtifact(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("PROD_DB_DSN", "")
	files := writeFixtureMigrations(t, filepath.Join(root, "migrations"))

	var output bytes.Buffer
	previousOutput := rootCmd.OutOrStdout()
	rootCmd.SetOut(&output)
	t.Cleanup(func() { rootCmd.SetOut(previousOutput) })

	args := append([]string{"squash"}, files...)
	args = append(args, "--dry-run", "--json", "--quiet", "--output", filepath.Join(root, "out"))
	if err := executeCLI(t, args...); err != nil {
		t.Fatalf("squash --dry-run --json failed: %v", err)
	}

	var payload struct {
		BaselineSQL string   `json:"baseline_sql"`
		Warnings    []string `json:"warnings"`
	}
	if err := json.Unmarshal(output.Bytes(), &payload); err != nil {
		t.Fatalf("decode dry-run JSON %q: %v", output.String(), err)
	}
	if strings.TrimSpace(payload.BaselineSQL) == "" {
		t.Fatal("dry-run JSON baseline_sql is empty")
	}
	if strings.Contains(payload.BaselineSQL, "Dry Run: Final SQL Output") {
		t.Fatal("baseline_sql contains human-readable CLI framing")
	}
}

// TestSquashSmokeEndToEnd runs a real (non-dry-run) squash over the fixture
// migration set through the full cobra path and verifies the staged+promoted
// output contract: baseline file present, .squashmap.json present with the
// version stamp threaded from SetVersionInfo, input files untouched, and no
// staging leftovers.
func TestSquashSmokeEndToEnd(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("PROD_DB_DSN", "")

	// A distinctive version proves the stamp is threaded from SetVersionInfo
	// rather than being the hardcoded "0.9.7" fallback.
	const testVersion = "9.9.9-clitest"
	setTestVersionInfo(t, testVersion, "2026-07-07", "cafebabe")

	migDir := filepath.Join(root, "migrations")
	files := writeFixtureMigrations(t, migDir)
	outDir := filepath.Join(root, "out")

	inputHashes := make(map[string]string, len(files))
	for _, f := range files {
		inputHashes[f] = fileSHA256(t, f)
	}

	args := append([]string{"squash"}, files...)
	args = append(args,
		"--output", outDir,
		"--no-validate",          // Docker-based validation is out of scope for -short
		"--i-know-what-im-doing", // never prompt on stdin, regardless of git state
	)

	if err := executeCLI(t, args...); err != nil {
		t.Fatalf("squash failed: %v", err)
	}

	// Baseline promoted into the output directory.
	baselinePath := filepath.Join(outDir, "000_baseline.sql")
	baseline, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("expected baseline at %s: %v", baselinePath, err)
	}
	if strings.TrimSpace(string(baseline)) == "" {
		t.Error("baseline file is empty")
	}
	if !strings.Contains(string(baseline), "users") {
		t.Errorf("baseline does not mention the fixture table 'users':\n%s", baseline)
	}

	// Provenance map present with the threaded version stamp.
	squashmapPath := filepath.Join(outDir, ".squashmap.json")
	squashmapData, err := os.ReadFile(squashmapPath)
	if err != nil {
		t.Fatalf("expected provenance map at %s: %v", squashmapPath, err)
	}
	var squashmap struct {
		Version    string `json:"version"`
		SafetyMode string `json:"safety_mode"`
	}
	if err := json.Unmarshal(squashmapData, &squashmap); err != nil {
		t.Fatalf("failed to parse %s: %v", squashmapPath, err)
	}
	if squashmap.Version != testVersion {
		t.Errorf("squashmap version = %q, want %q (version must be threaded from SetVersionInfo)", squashmap.Version, testVersion)
	}
	if squashmap.Version == "0.9.7" {
		t.Error("squashmap version is the hardcoded fallback \"0.9.7\" despite SetVersionInfo being called")
	}
	if squashmap.SafetyMode != "standard" {
		t.Errorf("squashmap safety_mode = %q, want %q (default config)", squashmap.SafetyMode, "standard")
	}

	// Input migrations must be byte-for-byte unmodified.
	for _, f := range files {
		if got := fileSHA256(t, f); got != inputHashes[f] {
			t.Errorf("input migration %s was modified by squash", f)
		}
	}

	// Staging directories are created next to the output dir and must be
	// gone after promotion.
	staging, err := filepath.Glob(filepath.Join(root, ".pgsquash-staging-*"))
	if err != nil {
		t.Fatalf("glob failed: %v", err)
	}
	if len(staging) != 0 {
		t.Errorf("staging directories left behind after promotion: %v", staging)
	}
}

// TestSquashBackupPathHonored verifies that --backup-path together with
// --backup places the backup working directory at the given path (and not at
// the default <output>/.backups). Without a PROD_DB_DSN no pg_dump runs, but
// the engine creates the backup directory at construction time, which is the
// wiring under test.
func TestSquashBackupPathHonored(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("PROD_DB_DSN", "")

	migDir := filepath.Join(root, "migrations")
	files := writeFixtureMigrations(t, migDir)
	outDir := filepath.Join(root, "out")
	customBackups := filepath.Join(root, "custom-backups")

	args := append([]string{"squash"}, files...)
	args = append(args,
		"--output", outDir,
		"--no-validate",
		"--i-know-what-im-doing",
		"--backup",
		"--backup-path", customBackups,
	)

	if err := executeCLI(t, args...); err != nil {
		t.Fatalf("squash --backup --backup-path failed: %v", err)
	}

	info, err := os.Stat(customBackups)
	if err != nil {
		t.Fatalf("expected backup directory at --backup-path %s: %v", customBackups, err)
	}
	if !info.IsDir() {
		t.Fatalf("--backup-path %s exists but is not a directory", customBackups)
	}

	defaultBackups := filepath.Join(outDir, ".backups")
	if _, err := os.Stat(defaultBackups); !os.IsNotExist(err) {
		t.Errorf("default backup dir %s must not be created when --backup-path is set (stat err: %v)", defaultBackups, err)
	}
}
