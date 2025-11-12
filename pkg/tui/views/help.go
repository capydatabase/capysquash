package views

import (
	"github.com/capysquash/pgsquash-engine/pkg/tui/styles"
	"github.com/capysquash/pgsquash-engine/pkg/tui/viewtypes"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpView displays keyboard shortcuts and usage information
type HelpView struct {
	viewtypes.BaseView
	scrollOffset int
}

// KeyBinding represents a keyboard shortcut
type KeyBinding struct {
	Key         string
	Description string
	Context     string
}

// NewHelpView creates a new help view
func NewHelpView() *HelpView {
	return &HelpView{
		scrollOffset: 0,
	}
}

// Init initializes the help view
func (h *HelpView) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (h *HelpView) Update(msg tea.Msg) (viewtypes.View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if h.scrollOffset > 0 {
				h.scrollOffset--
			}
			return h, nil

		case "down", "j":
			h.scrollOffset++
			return h, nil
		}
	}

	return h, nil
}

// View renders the help view
func (h *HelpView) View() string {
	var sections []string

	// Title
	title := styles.TitleStyle.Render("Help & Keyboard Shortcuts")
	sections = append(sections, title)

	// Global shortcuts
	sections = append(sections, h.renderSection("Global", []KeyBinding{
		{"q, Ctrl+C", "Quit the application", ""},
		{"ESC", "Return to dashboard", ""},
		{"?", "Toggle this help screen", ""},
	}))

	// Dashboard shortcuts
	sections = append(sections, h.renderSection("Dashboard", []KeyBinding{
		{"↑/↓, j/k", "Navigate menu items", ""},
		{"Enter, Space", "Select menu item", ""},
	}))

	// Analysis shortcuts
	sections = append(sections, h.renderSection("Analysis View", []KeyBinding{
		{"←/→, h/l", "Switch between tabs", ""},
		{"↑/↓, j/k", "Scroll content", ""},
		{"r", "Refresh analysis", ""},
	}))

	// Configuration shortcuts
	sections = append(sections, h.renderSection("Configuration", []KeyBinding{
		{"↑/↓, j/k", "Navigate fields", ""},
		{"Enter", "Edit field", ""},
		{"←/→, h/l", "Change value (while editing)", ""},
		{"ESC", "Cancel editing", ""},
		{"s, Ctrl+S", "Save configuration", ""},
		{"d", "Reset to defaults", ""},
	}))

	// Dependency Graph shortcuts
	sections = append(sections, h.renderSection("Dependency Graph", []KeyBinding{
		{"↑/↓, j/k", "Navigate objects", ""},
		{"r", "Toggle reverse dependencies", ""},
		{"f", "Refresh graph", ""},
	}))

	// About section
	sections = append(sections, h.renderAbout())

	// Footer help
	help := styles.HelpStyle.Render("↑/↓: Scroll  ►  ESC: Back")
	sections = append(sections, help)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		sections...,
	)
}

// Type returns the view type
func (h *HelpView) Type() viewtypes.ViewType {
	return viewtypes.ViewHelp
}

// renderSection renders a section of key bindings
func (h *HelpView) renderSection(title string, bindings []KeyBinding) string {
	lines := []string{
		"",
		styles.SubtitleStyle.Render(title),
		"",
	}

	for _, binding := range bindings {
		key := styles.KeyStyle.Render(binding.Key)
		desc := styles.DescStyle.Render(binding.Description)

		line := "  " + key
		// Pad to align descriptions
		padding := 25 - lipgloss.Width(key)
		if padding > 0 {
			line += lipgloss.NewStyle().Width(padding).Render("")
		}
		line += desc

		if binding.Context != "" {
			context := styles.MutedStyle.Render(" (" + binding.Context + ")")
			line += context
		}

		lines = append(lines, line)
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderAbout renders the about section
func (h *HelpView) renderAbout() string {
	lines := []string{
		"",
		styles.SubtitleStyle.Render("About"),
		"",
		styles.TextStyle.Render("  pgsquash v0.9.7"),
		styles.MutedStyle.Render("  PostgreSQL Migration Consolidation Tool"),
		"",
		styles.TextStyle.Render("  Features:"),
		styles.MutedStyle.Render("    ► AST-based SQL parsing with pg_query_go"),
		styles.MutedStyle.Render("    ► Intelligent object lifecycle tracking"),
		styles.MutedStyle.Render("    ► Safety-level aware consolidation"),
		styles.MutedStyle.Render("    ► Plugin system for third-party integrations"),
		styles.MutedStyle.Render("    ► Docker-based schema validation"),
		"",
		styles.TextStyle.Render("  For more information:"),
		styles.MutedStyle.Render("    ► GitHub: github.com/capysquash/pgsquash-engine"),
		styles.MutedStyle.Render("    ► Docs: See docs/ directory"),
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}
