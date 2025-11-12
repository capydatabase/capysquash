package cli

import (
	"fmt"
	"os"

	"github.com/capysquash/pgsquash-engine/pkg/tui"
	"github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
	Use:   "tui [migrations-dir]",
	Short: "Launch interactive TUI for migration analysis and squashing",
	Long: `Launch the interactive terminal user interface (TUI) for pgsquash.

The TUI provides a visual interface for:
  ► Analyzing migrations and viewing lifecycle patterns
  ► Configuring squashing settings interactively
  ► Visualizing dependency graphs
  ► Monitoring squashing progress in real-time

Examples:
  pgsquash tui migrations/
  pgsquash tui
`,
	RunE: runTUI,
}

var tuiAnalyzeCmd = &cobra.Command{
	Use:   "analyze [migrations-dir]",
	Short: "Launch TUI directly in analysis view",
	Long:  "Launch the TUI and immediately navigate to the analysis view.",
	RunE:  runTUIAnalyze,
}

var tuiConfigCmd = &cobra.Command{
	Use:   "config [config-path]",
	Short: "Launch TUI directly in configuration wizard",
	Long:  "Launch the TUI and immediately navigate to the configuration wizard.",
	RunE:  runTUIConfig,
}

var tuiDepGraphCmd = &cobra.Command{
	Use:   "deps [migrations-dir]",
	Short: "Launch TUI directly in dependency graph view",
	Long:  "Launch the TUI and immediately navigate to the dependency graph visualization.",
	RunE:  runTUIDepGraph,
}

func init() {
	// Add subcommands
	tuiCmd.AddCommand(tuiAnalyzeCmd)
	tuiCmd.AddCommand(tuiConfigCmd)
	tuiCmd.AddCommand(tuiDepGraphCmd)

	// Add to root command
	rootCmd.AddCommand(tuiCmd)
}

// runTUI runs the main TUI
func runTUI(cmd *cobra.Command, args []string) error {
	migrationDir := "."
	if len(args) > 0 {
		migrationDir = args[0]
	}

	// Verify migration directory exists
	if _, err := os.Stat(migrationDir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("migration directory does not exist: %s", migrationDir)
		}
		return fmt.Errorf("failed to access migration directory: %w", err)
	}

	// Get config path from flag or default
	configPath, _ := cmd.Flags().GetString("config")

	// Launch the TUI using the public API
	return tui.Launch(migrationDir, configPath)
}

// runTUIAnalyze runs the TUI and navigates to analysis view
func runTUIAnalyze(cmd *cobra.Command, args []string) error {
	migrationDir := "."
	if len(args) > 0 {
		migrationDir = args[0]
	}

	// Verify migration directory exists
	if _, err := os.Stat(migrationDir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("migration directory does not exist: %s", migrationDir)
		}
		return fmt.Errorf("failed to access migration directory: %w", err)
	}

	configPath, _ := cmd.Flags().GetString("config")

	// Launch TUI directly into analysis view using the public API
	return tui.LaunchWithView(migrationDir, configPath, tui.ViewAnalysis)
}

// runTUIConfig runs the TUI and navigates to configuration view
func runTUIConfig(cmd *cobra.Command, args []string) error {
	configPath := "pgsquash.config.json"
	if len(args) > 0 {
		configPath = args[0]
	}

	// Launch TUI directly into config view using the public API
	return tui.LaunchWithView(".", configPath, tui.ViewConfig)
}

// runTUIDepGraph runs the TUI and navigates to dependency graph view
func runTUIDepGraph(cmd *cobra.Command, args []string) error {
	migrationDir := "."
	if len(args) > 0 {
		migrationDir = args[0]
	}

	// Verify migration directory exists
	if _, err := os.Stat(migrationDir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("migration directory does not exist: %s", migrationDir)
		}
		return fmt.Errorf("failed to access migration directory: %w", err)
	}

	configPath, _ := cmd.Flags().GetString("config")

	// Launch TUI directly into dependency graph view using the public API
	return tui.LaunchWithView(migrationDir, configPath, tui.ViewDependencyGraph)
}
