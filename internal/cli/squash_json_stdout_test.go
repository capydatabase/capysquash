package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// squash --json writes the JSON document and nothing else to the process
// stdout, even when the config (show_progress defaults to true) asks for
// progress output: callers decode stdout directly.
func TestSquashJSONKeepsProcessStdoutParseable(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("PROD_DB_DSN", "")
	files := writeFixtureMigrations(t, filepath.Join(root, "migrations"))

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	previousStdout := os.Stdout
	os.Stdout = writer
	previousOut := rootCmd.OutOrStdout()
	rootCmd.SetOut(writer)
	t.Cleanup(func() {
		os.Stdout = previousStdout
		rootCmd.SetOut(previousOut)
	})

	captured := make(chan []byte)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		captured <- buf.Bytes()
	}()

	args := append([]string{"squash"}, files...)
	args = append(args, "--dry-run", "--json", "--no-emoji", "--i-know-what-im-doing", "--output", filepath.Join(root, "out"))
	runErr := executeCLI(t, args...)
	_ = writer.Close()
	stdout := <-captured
	if runErr != nil {
		t.Fatalf("squash --dry-run --json failed: %v", runErr)
	}

	var payload map[string]any
	if err := json.Unmarshal(stdout, &payload); err != nil {
		t.Fatalf("stdout is not one JSON document: %v\n%s", err, stdout)
	}
	if _, ok := payload["baseline_sql"]; !ok {
		t.Fatalf("stdout JSON has no baseline_sql: %s", stdout)
	}
}
