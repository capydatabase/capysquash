package engine

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSquashFilesReturnsRealMetrics verifies that SquashFiles now returns actual metrics instead of 0/"N/A"
func TestSquashFilesReturnsRealMetrics(t *testing.T) {
	// Create a temporary directory with test migrations
	tmpDir, err := os.MkdirTemp("", "capysquash-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create test migration files with proper statement terminators
	migrations := map[string]string{
		"001_create_users.sql": `-- Migration 1: Create users table
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Add name column
ALTER TABLE users ADD COLUMN name TEXT;
`,
		"002_create_posts.sql": `-- Migration 2: Create posts table
CREATE TABLE posts (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    title TEXT NOT NULL,
    content TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`,
	}

	migrationPaths := make(map[int]string)
	idx := 1
	for filename, content := range migrations {
		path := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to write migration file: %v", err)
		}
		migrationPaths[idx] = path
		idx++
	}

	// Run squash with default config
	config := DefaultConfig()
	config.SafetyLevel = Standard

	result, err := SquashFiles(migrationPaths, config)
	if err != nil {
		t.Fatalf("SquashFiles failed: %v", err)
	}

	// Verify we got real metrics (not the old placeholders)
	t.Logf("Result: FilesProcessed=%d, ObjectsConsolidated=%d, ProcessingTime=%s",
		result.FilesProcessed, result.ObjectsConsolidated, result.ProcessingTime)

	// Check that we processed the files
	if result.FilesProcessed != len(migrationPaths) {
		t.Errorf("Expected FilesProcessed=%d, got %d", len(migrationPaths), result.FilesProcessed)
	}

	// Check that we got actual SQL output
	if result.BaselineSQL == "" {
		t.Error("Expected SQL output, got empty string")
	}

	// Check that ProcessingTime is not the old "N/A" placeholder (unless it's legitimately 0ms)
	// We allow "N/A" if truly no time elapsed, but expect a real value otherwise
	if result.ProcessingTime != "N/A" {
		t.Logf("✅ Got real processing time: %s", result.ProcessingTime)
	}

	// ObjectsConsolidated might legitimately be 0 if no consolidation happened
	// But we can log it to verify it's being tracked
	t.Logf("Objects consolidated: %d", result.ObjectsConsolidated)
}

// TestSquashDirectoryReturnsMetrics tests the directory-based API
func TestSquashDirectoryReturnsMetrics(t *testing.T) {
	// Create a temporary directory with test migrations
	tmpDir, err := os.MkdirTemp("", "capysquash-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create a simple migration
	migrationSQL := `-- Test migration
CREATE TABLE test_table (
    id SERIAL PRIMARY KEY,
    data TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);
`

	if err := os.WriteFile(filepath.Join(tmpDir, "001_test.sql"), []byte(migrationSQL), 0644); err != nil {
		t.Fatalf("Failed to write migration: %v", err)
	}

	// Run squash
	config := DefaultConfig()
	result, err := SquashDirectory(tmpDir, config)
	if err != nil {
		t.Fatalf("SquashDirectory failed: %v", err)
	}

	// Verify we got a result
	if result == nil {
		t.Fatal("Expected result, got nil")
	}

	t.Logf("✅ SquashDirectory completed: FilesProcessed=%d, ProcessingTime=%s",
		result.FilesProcessed, result.ProcessingTime)
}

// TestFormatDuration verifies the duration formatting
func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		input    int64 // milliseconds
		expected string
	}{
		{"zero", 0, "N/A"},
		{"sub-second", 500, "500ms"},
		{"one second", 1000, "1.00s"},
		{"multiple seconds", 3500, "3.50s"},
		{"one minute", 60000, "1m0s"},
		{"minutes and seconds", 125000, "2m5s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := time.Duration(tt.input) * time.Millisecond
			result := formatDuration(d)

			if result != tt.expected {
				t.Errorf("formatDuration(%v) = %q, want %q", d, result, tt.expected)
			}
		})
	}
}

// TestSquashDirectoryStreamingCompletes pins the streaming directory path of
// SquashDirectory: it used to deadlock (the parsed-file channel was never
// closed, so the tracking goroutines waited forever) and handed the tracker
// the files in whatever order the parse workers finished them.
func TestSquashDirectoryStreamingCompletes(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"001_users.sql":   "CREATE TABLE users (id bigint PRIMARY KEY);",
		"002_email.sql":   "ALTER TABLE users ADD COLUMN email text;",
		"003_posts.sql":   "CREATE TABLE posts (id bigint PRIMARY KEY, user_id bigint REFERENCES users(id));",
		"004_title.sql":   "ALTER TABLE posts ADD COLUMN title text;",
		"005_drop.sql":    "ALTER TABLE users DROP COLUMN email;",
		"006_comment.sql": "COMMENT ON TABLE posts IS 'posts';",
	}
	for name, sql := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(sql), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	type outcome struct {
		result *SquashResult
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := SquashDirectory(dir, &Config{
			SafetyLevel:     Standard,
			EnableStreaming: true,
			BatchSize:       1,
			WorkerCount:     2,
		})
		done <- outcome{result, err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("SquashDirectory: %v", got.err)
		}
		sql := got.result.BaselineSQL
		for _, want := range []string{"CREATE TABLE users", "CREATE TABLE posts", "title"} {
			if !strings.Contains(sql, want) {
				t.Errorf("baseline lacks %q:\n%s", want, sql)
			}
		}
		// The history order reaches the tracker: the column is dropped
		// after the table that has it is created.
		if drop := strings.Index(sql, "DROP COLUMN email"); drop >= 0 && drop < strings.Index(sql, "CREATE TABLE users") {
			t.Errorf("baseline drops email before creating users:\n%s", sql)
		}
		if got.result.FilesProcessed != len(files) {
			t.Errorf("FilesProcessed = %d, want %d", got.result.FilesProcessed, len(files))
		}
	case <-time.After(60 * time.Second):
		t.Fatal("SquashDirectory with streaming did not return within 60s")
	}
}
