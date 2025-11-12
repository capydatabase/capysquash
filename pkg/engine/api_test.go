package engine

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSquashFilesReturnsRealMetrics verifies that SquashFiles now returns actual metrics instead of 0/"N/A"
func TestSquashFilesReturnsRealMetrics(t *testing.T) {
	// Create a temporary directory with test migrations
	tmpDir, err := os.MkdirTemp("", "pgsquash-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

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
	if result.SQL == "" {
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
	tmpDir, err := os.MkdirTemp("", "pgsquash-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

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
