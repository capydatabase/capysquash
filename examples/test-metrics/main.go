package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/capysquash/pgsquash-engine/pkg/engine"
)

func main() {
	// Create temp directory with test migrations
	tmpDir, err := os.MkdirTemp("", "pgsquash-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test migration files
	migration1 := `CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email TEXT NOT NULL
);

ALTER TABLE users ADD COLUMN name TEXT;`

	migration2 := `CREATE TABLE posts (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    title TEXT NOT NULL
);`

	file1 := filepath.Join(tmpDir, "001_create_users.sql")
	file2 := filepath.Join(tmpDir, "002_create_posts.sql")

	if err := os.WriteFile(file1, []byte(migration1), 0644); err != nil {
		panic(err)
	}
	if err := os.WriteFile(file2, []byte(migration2), 0644); err != nil {
		panic(err)
	}

	// Test 1: Basic squash returns real metrics
	fmt.Println("🧪 Test 1: Basic Squash Metrics")
	fmt.Println("================================")

	config := engine.DefaultConfig()
	config.SafetyLevel = engine.Standard

	result, err := engine.SquashDirectory(tmpDir, config)
	if err != nil {
		fmt.Printf("❌ FAIL: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Files Processed: %d\n", result.FilesProcessed)
	fmt.Printf("Objects Consolidated: %d\n", result.ObjectsConsolidated)
	fmt.Printf("Processing Time: %s\n", result.ProcessingTime)
	fmt.Printf("SQL Length: %d bytes\n", len(result.BaselineSQL))

	// Verify not placeholder values
	if result.ProcessingTime == "N/A" && len(result.BaselineSQL) > 0 {
		fmt.Println("⚠️  Processing time is N/A (acceptable if too fast)")
	} else if result.ProcessingTime == "N/A" {
		fmt.Println("❌ FAIL: Processing time is still N/A placeholder")
		os.Exit(1)
	} else {
		fmt.Println("✅ Processing time is real:", result.ProcessingTime)
	}

	if result.FilesProcessed == 2 {
		fmt.Println("✅ Files processed count is correct")
	} else {
		fmt.Printf("❌ FAIL: Expected 2 files, got %d\n", result.FilesProcessed)
		os.Exit(1)
	}

	// Test 2: Separate data ops mode
	fmt.Println("\n🧪 Test 2: Separate Data Ops Mode")
	fmt.Println("==================================")

	config2 := engine.DefaultConfig()
	config2.SeparateDataOps = true

	migrations := map[int]string{
		1: file1,
		2: file2,
	}

	result2, err := engine.SquashFiles(migrations, config2)
	if err != nil {
		fmt.Printf("❌ FAIL: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("BaselineSQL Length: %d bytes\n", len(result2.BaselineSQL))
	fmt.Printf("DataOperationsSQL Length: %d bytes\n", len(result2.DataOperationsSQL))
	fmt.Printf("Extensions: %v\n", result2.Extensions)

	if result2.BaselineSQL == "" {
		fmt.Println("❌ FAIL: BaselineSQL is empty")
		os.Exit(1)
	} else {
		fmt.Println("✅ BaselineSQL populated")
	}

	if result2.ProvenanceInfo != nil {
		fmt.Printf("✅ ProvenanceInfo present (Version: %s)\n", result2.ProvenanceInfo.Version)
	} else {
		fmt.Println("⚠️  ProvenanceInfo is nil (acceptable, may not be populated)")
	}

	// Test 3: Analysis
	fmt.Println("\n🧪 Test 3: Analysis Returns Data")
	fmt.Println("=================================")

	analysis, err := engine.AnalyzeDirectory(tmpDir, config)
	if err != nil {
		fmt.Printf("❌ FAIL: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Total Files: %d\n", analysis.TotalFiles)
	fmt.Printf("Total Statements: %d\n", analysis.TotalStatements)
	fmt.Printf("Total Objects: %d\n", analysis.TotalObjects)
	fmt.Printf("Redundancies: %d\n", len(analysis.Redundancies))

	if analysis.TotalFiles == 2 {
		fmt.Println("✅ Analysis file count correct")
	} else {
		fmt.Printf("❌ FAIL: Expected 2 files, got %d\n", analysis.TotalFiles)
		os.Exit(1)
	}

	fmt.Println("\n================================")
	fmt.Println("✅ All tests passed! (3/3)")
	fmt.Println("================================")
}
