// Package tui is the bubbletea terminal UI behind `capysquash tui` and the
// --tui flags.
package tui

import (
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
	"github.com/capydatabase/capysquash/internal/utils"
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

	return run(NewModel(migrationDir, configPath))
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
	model.startAt(view)

	return run(model)
}

// run starts the program. The analysis, dependency and squash views call
// into packages that log through the default logger, which writes to stdout
// and would draw over the screen, so it is silenced until the program exits.
func run(model *Model) error {
	prev := utils.GetDefaultLogger()
	utils.SetDefaultLogger(utils.NewLogger(utils.LogLevelInfo, io.Discard))
	defer utils.SetDefaultLogger(prev)

	if _, err := tea.NewProgram(model).Run(); err != nil {
		return fmt.Errorf("TUI error: %w", err)
	}
	return nil
}
