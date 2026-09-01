// Example: Using pgsquash as a Go library
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/capysquash/pgsquash-engine/pkg/engine"
)

func main() {
	fmt.Println("=== pgsquash Library Usage Examples ===")

	// Example 1: Basic squashing with defaults
	example1()

	// Example 2: Custom configuration
	example2()

	// Example 3: Analysis only
	example3()

	// Example 4: Specific files
	example4()
}

// Example 1: Basic squashing with defaults
func example1() {
	fmt.Println("1️⃣  Basic Squashing (Default Config)")
	fmt.Println("-----------------------------------")

	// Create test migrations directory
	if err := setupTestMigrations(); err != nil {
		log.Printf("Setup failed: %v\n", err)
		return
	}
	defer cleanup()

	// Squash with defaults
	result, err := engine.SquashDirectory("./example_migrations", nil)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("☑ Success!\n")
	fmt.Printf("   Files processed: %d\n", result.FilesProcessed)
	fmt.Printf("   Warnings: %d\n", len(result.Warnings))
	fmt.Printf("   SQL length: %d bytes\n", len(result.BaselineSQL))
	fmt.Println()
}

// Example 2: Custom configuration
func example2() {
	fmt.Println("2️⃣  Custom Configuration")
	fmt.Println("------------------------")

	if err := setupTestMigrations(); err != nil {
		log.Printf("Setup failed: %v\n", err)
		return
	}
	defer cleanup()

	// Custom config with conservative safety
	config := &engine.Config{
		SafetyLevel:  engine.Conservative,
		OutputFormat: engine.FormatSingle,
		Verbose:      false,
	}

	result, err := engine.SquashDirectory("./example_migrations", config)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("☑ Success with conservative mode!\n")
	fmt.Printf("   Files processed: %d\n", result.FilesProcessed)
	fmt.Println()
}

// Example 3: Analysis only (no squashing)
func example3() {
	fmt.Println("3️⃣  Analysis Only")
	fmt.Println("-----------------")

	if err := setupTestMigrations(); err != nil {
		log.Printf("Setup failed: %v\n", err)
		return
	}
	defer cleanup()

	analysis, err := engine.AnalyzeDirectory("./example_migrations", nil)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("📊 Analysis Results:\n")
	fmt.Printf("   Total files: %d\n", analysis.TotalFiles)
	fmt.Printf("   Total statements: %d\n", analysis.TotalStatements)
	fmt.Printf("   Total objects: %d\n", analysis.TotalObjects)
	fmt.Printf("   Redundancies: %d\n", len(analysis.Redundancies))

	if len(analysis.ObjectsByType) > 0 {
		fmt.Println("\n   Objects by type:")
		for objType, count := range analysis.ObjectsByType {
			fmt.Printf("     - %s: %d\n", objType, count)
		}
	}
	fmt.Println()
}

// Example 4: Squash specific files
func example4() {
	fmt.Println("4️⃣  Specific Files")
	fmt.Println("------------------")

	if err := setupTestMigrations(); err != nil {
		log.Printf("Setup failed: %v\n", err)
		return
	}
	defer cleanup()

	// Specify exact files to squash
	migrations := map[int]string{
		1: "./example_migrations/001_create_users.sql",
		2: "./example_migrations/002_create_posts.sql",
	}

	result, err := engine.SquashFiles(migrations, nil)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	fmt.Printf("☑ Squashed specific files!\n")
	fmt.Printf("   Files: %d\n", result.FilesProcessed)
	fmt.Println()
}

// Helper: Setup test migrations
func setupTestMigrations() error {
	// Create directory
	if err := os.MkdirAll("./example_migrations", 0755); err != nil {
		return err
	}

	// Migration 1: Create users table
	migration1 := `-- Create users table
CREATE TABLE users (
    id SERIAL PRIMARY KEY,
    email TEXT NOT NULL UNIQUE,
    name TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_users_email ON users(email);
`
	if err := os.WriteFile("./example_migrations/001_create_users.sql", []byte(migration1), 0644); err != nil {
		return err
	}

	// Migration 2: Create posts table
	migration2 := `-- Create posts table
CREATE TABLE posts (
    id SERIAL PRIMARY KEY,
    user_id INTEGER REFERENCES users(id),
    title TEXT NOT NULL,
    content TEXT,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE INDEX idx_posts_user_id ON posts(user_id);
`
	if err := os.WriteFile("./example_migrations/002_create_posts.sql", []byte(migration2), 0644); err != nil {
		return err
	}

	// Migration 3: Add comments table
	migration3 := `-- Create comments table
CREATE TABLE comments (
    id SERIAL PRIMARY KEY,
    post_id INTEGER REFERENCES posts(id),
    user_id INTEGER REFERENCES users(id),
    content TEXT NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);
`
	if err := os.WriteFile("./example_migrations/003_create_comments.sql", []byte(migration3), 0644); err != nil {
		return err
	}

	return nil
}

// Helper: Cleanup test files
func cleanup() {
	os.RemoveAll("./example_migrations")
}
