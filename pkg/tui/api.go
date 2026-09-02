// Package tui provides a public API for the Terminal User Interface (TUI).
// This package exposes the interactive terminal interface for pgsquash-engine,
// allowing external CLI wrappers to integrate the TUI functionality.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// Launch is a convenience function that creates and runs a TUI in one step.
// This is the simplest way to start the TUI with default settings.
//
// Example:
//
//	if err := tui.Launch("./migrations", "pgsquash.config.json"); err != nil {
//		log.Fatal(err)
//	}
func Launch(migrationDir, configPath string) error {
	if migrationDir == "" {
		migrationDir = "."
	}
	if configPath == "" {
		configPath = "pgsquash.config.json"
	}

	model := NewModel(migrationDir, configPath)
	p := tea.NewProgram(model, tea.WithAltScreen())
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
		configPath = "pgsquash.config.json"
	}

	model := NewModel(migrationDir, configPath)

	// Set the initial view before starting the program
	if v, exists := model.views[view]; exists {
		model.currentView = v
	}

	p := tea.NewProgram(model, tea.WithAltScreen())

	if _, err := p.Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}
