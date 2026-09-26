package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/capydatabase/capysquash/internal/tui/styles"
	"github.com/capydatabase/capysquash/internal/tui/views"
)

// Model is the main TUI application model
type Model struct {
	currentView  View
	views        map[ViewType]View
	width        int
	height       int
	err          error
	statusMsg    string
	statusStyle  lipgloss.Style
	migrationDir string
	configPath   string
	ready        bool
}

// NewModel creates a new TUI model
func NewModel(migrationDir, configPath string) *Model {
	m := &Model{
		views:        make(map[ViewType]View),
		migrationDir: migrationDir,
		configPath:   configPath,
		statusStyle:  styles.TextStyle,
	}

	// Initialize all views
	m.views[ViewDashboard] = views.NewDashboardView(migrationDir, configPath)
	m.views[ViewAnalysis] = views.NewAnalysisView(migrationDir)
	m.views[ViewConfig] = views.NewConfigView(configPath)
	m.views[ViewDependencyGraph] = views.NewDependencyGraphView(migrationDir)
	m.views[ViewProgress] = views.NewProgressView(migrationDir, configPath)
	m.views[ViewValidation] = views.NewValidationView(migrationDir, configPath)
	m.views[ViewHelp] = views.NewHelpView()

	// Start with dashboard
	m.currentView = m.views[ViewDashboard]

	return m
}

// Init enters the starting view.
func (m *Model) Init() tea.Cmd {
	return m.currentView.OnEnter()
}

// startAt makes view the one the program starts on. Init enters it.
func (m *Model) startAt(view ViewType) {
	if v, exists := m.views[view]; exists {
		m.currentView = v
	}
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true

		// Update all view sizes
		for _, view := range m.views {
			view.SetSize(m.width, m.height-3) // Reserve space for status bar
		}

		return m, nil

	case tea.KeyPressMsg:
		// Global key bindings
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit

		case "?":
			// Toggle help view
			if m.currentView.Type() == ViewHelp {
				// Return to previous view (dashboard for now)
				return m.navigateTo(ViewDashboard)
			} else {
				return m.navigateTo(ViewHelp)
			}

		case "v":
			// Toggle the validation view
			if m.currentView.Type() == ViewValidation {
				return m.navigateTo(ViewDashboard)
			}
			return m.navigateTo(ViewValidation)

		case "esc":
			// A view using esc itself (the config wizard while editing)
			// gets it; otherwise return to the dashboard.
			if c, ok := m.currentView.(EscCapturer); ok && c.CapturesEsc() {
				break
			}
			if m.currentView.Type() != ViewDashboard {
				return m.navigateTo(ViewDashboard)
			}
		}

	case NavigateMsg:
		return m.navigateTo(msg.View)

	case ErrorMsg:
		m.statusMsg = fmt.Sprintf("Error: %v", msg.Err)
		m.statusStyle = styles.ErrorStyle
		return m, nil

	case SuccessMsg:
		m.statusMsg = msg.Message
		m.statusStyle = styles.SuccessStyle
		return m, nil

	case LoadingMsg:
		m.statusMsg = msg.Message
		m.statusStyle = styles.TextStyle
		return m, nil
	}

	// Delegate to current view
	m.currentView, cmd = m.currentView.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

// View renders the TUI. It always asks for the alternate screen.
func (m *Model) View() tea.View {
	v := tea.NewView("Initializing...")
	v.AltScreen = true
	if !m.ready {
		return v
	}

	// Render current view
	content := m.currentView.View()

	// Render status bar
	statusBar := m.renderStatusBar()

	v.SetContent(lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		statusBar,
	))
	return v
}

// navigateTo switches to a different view
func (m *Model) navigateTo(viewType ViewType) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	// Exit current view
	if exitCmd := m.currentView.OnExit(); exitCmd != nil {
		cmds = append(cmds, exitCmd)
	}

	// Switch view
	newView, exists := m.views[viewType]
	if !exists {
		m.err = fmt.Errorf("view not found: %v", viewType)
		return m, nil
	}

	m.currentView = newView

	// Enter new view
	if enterCmd := m.currentView.OnEnter(); enterCmd != nil {
		cmds = append(cmds, enterCmd)
	}

	return m, tea.Batch(cmds...)
}

// renderStatusBar renders the bottom status bar
func (m *Model) renderStatusBar() string {
	// Current view name
	viewName := m.getViewName(m.currentView.Type())
	viewBadge := styles.PrimaryBadge(viewName)

	// Navigation hints
	hints := styles.MutedStyle.Render("ESC: Dashboard  ►  v: Validation  ►  ?: Help  ►  q: Quit")

	// Status message
	status := ""
	if m.statusMsg != "" {
		status = m.statusStyle.Render(m.statusMsg)
	}

	leftSection := lipgloss.JoinHorizontal(
		lipgloss.Left,
		viewBadge,
		"  ",
		status,
	)

	rightSection := hints

	// Fill the gap between the sections. The bar's own horizontal padding
	// takes columns out of m.width too, or the content wraps.
	totalWidth := lipgloss.Width(leftSection) + lipgloss.Width(rightSection)
	padding := max(m.width-styles.StatusBarStyle.GetHorizontalPadding()-totalWidth, 0)

	statusContent := lipgloss.JoinHorizontal(
		lipgloss.Left,
		leftSection,
		lipgloss.NewStyle().Width(padding).Render(""),
		rightSection,
	)

	return styles.StatusBarStyle.
		Width(m.width).
		Render(statusContent)
}

// getViewName returns the human-readable name for a view type
func (m *Model) getViewName(vt ViewType) string {
	switch vt {
	case ViewDashboard:
		return "Dashboard"
	case ViewAnalysis:
		return "Analysis"
	case ViewConfig:
		return "Configuration"
	case ViewDependencyGraph:
		return "Dependency Graph"
	case ViewProgress:
		return "Progress"
	case ViewValidation:
		return "Validation"
	case ViewHelp:
		return "Help"
	default:
		return "Unknown"
	}
}
