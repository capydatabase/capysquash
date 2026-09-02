// Package tui provides a public API for the pgsquash Terminal User Interface (TUI).
//
// The TUI package offers an interactive terminal interface for analyzing and squashing
// PostgreSQL migrations with real-time feedback and visualization.
//
// # Features
//
//   - Interactive dashboard for migration analysis
//   - Real-time dependency graph visualization
//   - Configuration wizard with validation
//   - Progress tracking for long-running operations
//   - Keyboard-driven navigation with vim-like bindings
//   - Multiple themed views with syntax highlighting
//
// # Basic Usage
//
// The simplest way to launch the TUI is using the Launch function:
//
//	package main
//
//	import (
//		"log"
//		"github.com/capy-base/pgsquash-engine/pkg/tui"
//	)
//
//	func main() {
//		if err := tui.Launch("./migrations", "pgsquash.config.json"); err != nil {
//			log.Fatal(err)
//		}
//	}
//
// # Advanced Usage
//
// For more control, create a Model directly and use custom Bubbletea options:
//
//	package main
//
//	import (
//		"log"
//		"github.com/capy-base/pgsquash-engine/pkg/tui"
//		tea "github.com/charmbracelet/bubbletea"
//	)
//
//	func main() {
//		// Create TUI model
//		model := tui.NewModel("./migrations", "pgsquash.config.json")
//
//		// Custom Bubbletea options
//		opts := []tea.ProgramOption{
//			tea.WithAltScreen(),
//			tea.WithMouseCellMotion(),
//		}
//
//		// Run the TUI with custom options
//		p := tea.NewProgram(model, opts...)
//		if _, err := p.Run(); err != nil {
//			log.Fatalf("TUI error: %v", err)
//		}
//	}
//
// # Direct View Navigation
//
// Launch directly into a specific view for focused workflows:
//
//	package main
//
//	import (
//		"log"
//		"github.com/capy-base/pgsquash-engine/pkg/tui"
//	)
//
//	func main() {
//		// Launch directly into analysis view
//		err := tui.LaunchWithView(
//			"./migrations",
//			"",
//			tui.ViewAnalysis,
//		)
//		if err != nil {
//			log.Fatal(err)
//		}
//	}
//
// # Available Views
//
//   - ViewDashboard: Main dashboard with quick actions
//   - ViewAnalysis: Detailed migration analysis with lifecycle patterns
//   - ViewConfig: Interactive configuration wizard
//   - ViewDependencyGraph: Visual dependency graph
//   - ViewProgress: Real-time operation progress
//   - ViewValidation: Schema validation results
//   - ViewHelp: Keyboard shortcuts and help
//
// # Integration with CLI Tools
//
// The TUI can be easily integrated into CLI tools using Cobra or other frameworks:
//
//	package main
//
//	import (
//		"fmt"
//		"os"
//		"github.com/capy-base/pgsquash-engine/pkg/tui"
//		"github.com/spf13/cobra"
//	)
//
//	var tuiCmd = &cobra.Command{
//		Use:   "tui [migrations-dir]",
//		Short: "Launch interactive TUI",
//		RunE:  runTUI,
//	}
//
//	func runTUI(cmd *cobra.Command, args []string) error {
//		migrationDir := "."
//		if len(args) > 0 {
//			migrationDir = args[0]
//		}
//
//		// Verify directory exists
//		if _, err := os.Stat(migrationDir); err != nil {
//			if os.IsNotExist(err) {
//				return fmt.Errorf("migration directory does not exist: %s", migrationDir)
//			}
//			return fmt.Errorf("failed to access migration directory: %w", err)
//		}
//
//		configPath, _ := cmd.Flags().GetString("config")
//		return tui.Launch(migrationDir, configPath)
//	}
//
// # Error Handling
//
// The TUI returns errors for:
//   - Missing or inaccessible migration directories
//   - Invalid configuration files
//   - Terminal compatibility issues
//   - Internal TUI failures
//
// Always check and handle errors appropriately:
//
//	if err := tui.Launch("./migrations", ""); err != nil {
//		log.Printf("TUI failed: %v", err)
//		// Fallback to non-interactive mode
//		runNonInteractive()
//	}
//
// # Keyboard Shortcuts
//
// Global shortcuts available in all views:
//   - q, Ctrl+C: Quit the TUI
//   - ?: Toggle help view
//   - ESC: Return to dashboard
//   - Tab: Cycle focus between elements
//   - ↑/↓ or j/k: Navigate up/down
//   - Enter: Select/confirm
//
// View-specific shortcuts are shown in the help view (press ?).
//
// # Configuration
//
// The TUI reads configuration from the specified config file (JSON format).
// If no config file exists, the TUI will use sensible defaults and offer
// to create one through the configuration wizard.
//
// Example configuration structure:
//
//	{
//		"safetyLevel": "standard",
//		"autoValidate": true,
//		"excludePatterns": ["*_test.sql"],
//		"customRules": []
//	}
//
// # Terminal Requirements
//
// The TUI requires:
//   - Terminal with ANSI color support
//   - Minimum terminal size: 80x24
//   - UTF-8 encoding support
//
// The TUI automatically detects terminal capabilities and adjusts rendering
// accordingly for the best experience.
//
// # Thread Safety
//
// The TUI Model is not thread-safe. Each instance should be used from a
// single goroutine. The underlying Bubbletea framework handles concurrency
// internally through its message passing system.
//
// # Dependencies
//
// This package uses:
//   - github.com/charmbracelet/bubbletea: TUI framework
//   - github.com/charmbracelet/lipgloss: Styling and layout
//
// These dependencies are handled automatically through the internal
// implementation and are not exposed in the public API.
package tui
