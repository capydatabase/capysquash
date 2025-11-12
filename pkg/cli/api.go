// Package cli provides a public API for the pgsquash CLI functionality.
//
// This package exports the internal CLI implementation for use by external tools
// like capysquash-cli while keeping the internal implementation details private.
package cli

import (
	internal_cli "github.com/capysquash/pgsquash-engine/internal/cli"
)

// Execute runs the pgsquash CLI.
// This is the main entry point for CLI applications using pgsquash as a library.
//
// Returns an error if command execution fails.
func Execute() error {
	return internal_cli.Execute()
}

// SetVersionInfo updates the version information displayed in the CLI.
// This should be called during application initialization, typically in init().
//
// Parameters:
//   - version: The semantic version string (e.g., "0.9.7")
//   - buildDate: The build timestamp in ISO 8601 format
//   - gitCommit: The git commit hash (short or full)
//
// Example:
//
//	cli.SetVersionInfo("0.9.7", "2024-10-21T10:00:00Z", "abc123")
func SetVersionInfo(version, buildDate, gitCommit string) {
	internal_cli.SetVersionInfo(version, buildDate, gitCommit)
}

// SetBrandName customizes the CLI branding for different distributions.
// This allows different binaries (e.g., pgsquash vs capysquash) to have
// distinct branding while using the same underlying functionality.
//
// Supported brands:
//   - "pgsquash" (default) - Technical, engine-focused messaging
//   - "capysquash" - Platform-focused, commercial messaging
//
// This should be called during application initialization, after SetVersionInfo.
//
// Example:
//
//	cli.SetBrandName("capysquash")
func SetBrandName(brandName string) {
	internal_cli.SetBrandName(brandName)
}
