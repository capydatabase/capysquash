// Package tui is the bubbletea terminal UI behind `capysquash tui` and the
// --tui flags.
package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// Launch is a convenience function that creates and runs a TUI in one step.
// This is the simplest way to start the TUI with default settings.
//
// Example:
//
//	if err := tui.Launch("./migrations", "capysquash.config.json"); err != nil {
//		log.Fatal(err)
//	}
func Launch(migrationDir, configPath string) error {
	if migrationDir == "" {
		migrationDir = "."
	}
	if configPath == "" {
		configPath = "capysquash.config.json"
	}

	model := NewModel(migrationDir, configPath)
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}

// LaunchWithView is a convenience function that creates and runs a TUI,
// immediately navigating to the specified view.
//
// Example:
//
//	// Launch directly into analysis view
//	if err := tui.LaunchWithView("./migrations", "", tui.ViewAnalysis); err != nil {
//		log.Fatal(err)
//	}
func LaunchWithView(migrationDir, configPath string, view ViewType) error {
	if migrationDir == "" {
		migrationDir = "."
	}
	if configPath == "" {
		configPath = "capysquash.config.json"
	}

	model := NewModel(migrationDir, configPath)

	// Set the initial view before starting the program
	if v, exists := model.views[view]; exists {
		model.currentView = v
	}

	p := tea.NewProgram(model)

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}
