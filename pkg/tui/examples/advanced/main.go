// Package main demonstrates advanced TUI usage with Cobra CLI integration.
// This example shows how to integrate the TUI into a CLI application with
// multiple commands and flags.
package main

import (
	"fmt"
	"os"

	"github.com/capy-base/pgsquash-engine/pkg/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	// Global flags
	configPath string
	verbose    bool

	// Root command
	rootCmd = &cobra.Command{
		Use:   "tui-demo",
		Short: "Demo application showing TUI integration",
		Long: `A demonstration of how to integrate the pgsquash TUI
into a Cobra-based CLI application with multiple commands.`,
	}

	// TUI command
	tuiCmd = &cobra.Command{
		Use:   "tui [migrations-dir]",
		Short: "Launch interactive TUI",
		Long: `Launch the interactive terminal user interface.

The TUI provides a visual interface for:
  • Analyzing migrations and viewing lifecycle patterns
  • Configuring squashing settings interactively
  • Visualizing dependency graphs
  • Monitoring squashing progress in real-time

Examples:
  tui-demo tui migrations/
  tui-demo tui --config=custom.json
  tui-demo tui analyze migrations/`,
		Args: cobra.MaximumNArgs(1),
		RunE: runTUI,
	}

	// TUI analyze subcommand
	tuiAnalyzeCmd = &cobra.Command{
		Use:   "analyze [migrations-dir]",
		Short: "Launch TUI in analysis view",
		Long:  "Launch the TUI and immediately navigate to the analysis view.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runTUIAnalyze,
	}

	// TUI config subcommand
	tuiConfigCmd = &cobra.Command{
		Use:   "config [config-path]",
		Short: "Launch TUI in configuration wizard",
		Long:  "Launch the TUI and immediately navigate to the configuration wizard.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runTUIConfig,
	}

	// TUI deps subcommand
	tuiDepGraphCmd = &cobra.Command{
		Use:   "deps [migrations-dir]",
		Short: "Launch TUI in dependency graph view",
		Long:  "Launch the TUI and immediately navigate to the dependency graph visualization.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runTUIDepGraph,
	}
)

func init() {
	// Global flags
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "pgsquash.config.json",
		"Path to configuration file")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false,
		"Enable verbose output")

	// Add subcommands to tui command
	tuiCmd.AddCommand(tuiAnalyzeCmd)
	tuiCmd.AddCommand(tuiConfigCmd)
	tuiCmd.AddCommand(tuiDepGraphCmd)

	// Add tui command to root
	rootCmd.AddCommand(tuiCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

// runTUI launches the main TUI dashboard
func runTUI(cmd *cobra.Command, args []string) error {
	migrationDir := getMigrationDir(args)

	if err := validateMigrationDir(migrationDir); err != nil {
		return err
	}

	if verbose {
		fmt.Printf("Launching TUI with migrations from: %s\n", migrationDir)
		fmt.Printf("Using config file: %s\n", configPath)
	}

	// Launch the TUI using the simple API
	return tui.Launch(migrationDir, configPath)
}

// runTUIAnalyze launches the TUI directly in analysis view
func runTUIAnalyze(cmd *cobra.Command, args []string) error {
	migrationDir := getMigrationDir(args)

	if err := validateMigrationDir(migrationDir); err != nil {
		return err
	}

	if verbose {
		fmt.Printf("Launching TUI in analysis view: %s\n", migrationDir)
	}

	// Launch directly into analysis view
	return tui.LaunchWithView(migrationDir, configPath, tui.ViewAnalysis)
}

// runTUIConfig launches the TUI directly in configuration wizard
func runTUIConfig(cmd *cobra.Command, args []string) error {
	// For config view, we can use any directory or current directory
	configFile := configPath
	if len(args) > 0 {
		configFile = args[0]
	}

	if verbose {
		fmt.Printf("Launching TUI configuration wizard: %s\n", configFile)
	}

	// Launch directly into config view
	return tui.LaunchWithView(".", configFile, tui.ViewConfig)
}

// runTUIDepGraph launches the TUI directly in dependency graph view
func runTUIDepGraph(cmd *cobra.Command, args []string) error {
	migrationDir := getMigrationDir(args)

	if err := validateMigrationDir(migrationDir); err != nil {
		return err
	}

	if verbose {
		fmt.Printf("Launching TUI dependency graph view: %s\n", migrationDir)
	}

	// For advanced control, you can create the model directly and use custom bubbletea options
	model := tui.NewModel(migrationDir, configPath)

	opts := []tea.ProgramOption{
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(), // Enable mouse support for graph
	}

	p := tea.NewProgram(model, opts...)

	// Send navigation message after program starts
	go func() {
		p.Send(tui.NavigateMsg{View: tui.ViewDependencyGraph})
	}()

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}

// Helper functions

func getMigrationDir(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "."
}

func validateMigrationDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("migration directory does not exist: %s", dir)
		}
		return fmt.Errorf("failed to access migration directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("path is not a directory: %s", dir)
	}

	return nil
}
