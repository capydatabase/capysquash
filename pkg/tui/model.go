package tui

import (
	"fmt"

	"github.com/capysquash/pgsquash-engine/pkg/tui/styles"
	"github.com/capysquash/pgsquash-engine/pkg/tui/views"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	m.views[ViewValidation] = views.NewValidationView()
	m.views[ViewHelp] = views.NewHelpView()

	// Start with dashboard
	m.currentView = m.views[ViewDashboard]

	return m
}

// Init initializes the TUI
func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		m.currentView.Init(),
	)
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

	case tea.KeyMsg:
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

		case "esc":
			// Return to dashboard from any view
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

// View renders the TUI
func (m *Model) View() string {
	if !m.ready {
		return "Initializing..."
	}

	// Render current view
	content := m.currentView.View()

	// Render status bar
	statusBar := m.renderStatusBar()

	return lipgloss.JoinVertical(
		lipgloss.Left,
		content,
		statusBar,
	)
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
	hints := styles.MutedStyle.Render("ESC: Dashboard  ►  ?: Help  ►  q: Quit")

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

	// Calculate padding
	totalWidth := lipgloss.Width(leftSection) + lipgloss.Width(rightSection)
	padding := max(m.width-totalWidth, 0)

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
