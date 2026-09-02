package views

import (
	"strings"

	"github.com/capy-base/pgsquash-engine/pkg/tui/styles"
	"github.com/capy-base/pgsquash-engine/pkg/tui/viewtypes"
	tea "github.com/charmbracelet/bubbletea"
)

// ValidationView renders validation results
type ValidationView struct {
	viewtypes.BaseView
	result  *viewtypes.ValidationResultMsg
	loading bool
}

// NewValidationView creates a new validation view
func NewValidationView() *ValidationView {
	return &ValidationView{}
}

func (v *ValidationView) Init() tea.Cmd {
	return nil
}

func (v *ValidationView) Update(msg tea.Msg) (viewtypes.View, tea.Cmd) {
	switch msg := msg.(type) {
	case viewtypes.ValidationResultMsg:
		v.result = &msg
		v.loading = false
		return v, nil
	case viewtypes.LoadingMsg:
		v.loading = true
		v.result = nil
		return v, nil
	}
	return v, nil
}

func (v *ValidationView) View() string {
	if v.loading {
		return styles.MutedStyle.Render("Validating...")
	}

	if v.result == nil {
		return styles.MutedStyle.Render("No validation results yet.")
	}

	var sb strings.Builder

	// Header
	if v.result.Success {
		sb.WriteString(styles.SuccessStyle.Render("✓ Validation Passed") + "\n\n")
	} else {
		sb.WriteString(styles.ErrorStyle.Render("✗ Validation Failed") + "\n\n")
	}

	// Errors
	if len(v.result.Errors) > 0 {
		sb.WriteString(styles.SubtitleStyle.Render("Errors:") + "\n")
		for _, err := range v.result.Errors {
			sb.WriteString(styles.ErrorStyle.Render("  • "+err) + "\n")
		}
		sb.WriteString("\n")
	}

	// Warnings
	if len(v.result.Warnings) > 0 {
		sb.WriteString(styles.SubtitleStyle.Render("Warnings:") + "\n")
		for _, warn := range v.result.Warnings {
			sb.WriteString(styles.WarningStyle.Render("  • "+warn) + "\n")
		}
	}

	return styles.BoxStyle.Render(sb.String())
}

func (v *ValidationView) Type() viewtypes.ViewType {
	return viewtypes.ViewValidation
}
