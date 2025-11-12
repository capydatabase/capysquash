// Package main demonstrates the simplest way to use the pgsquash TUI.
package main

import (
	"log"
	"os"

	"github.com/capysquash/pgsquash-engine/pkg/tui"
)

func main() {
	// Get migration directory from command line args, or use current directory
	migrationDir := "."
	if len(os.Args) > 1 {
		migrationDir = os.Args[1]
	}

	// Verify the migration directory exists
	if _, err := os.Stat(migrationDir); err != nil {
		if os.IsNotExist(err) {
			log.Fatalf("Migration directory does not exist: %s", migrationDir)
		}
		log.Fatalf("Failed to access migration directory: %v", err)
	}

	// Launch the TUI with default settings
	// This will use "pgsquash.config.json" as the config file if it exists
	if err := tui.Launch(migrationDir, "pgsquash.config.json"); err != nil {
		log.Fatalf("TUI error: %v", err)
	}
}
