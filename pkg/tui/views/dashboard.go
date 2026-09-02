package views

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/config"
	"github.com/capy-base/pgsquash-engine/pkg/tui/styles"
	"github.com/capy-base/pgsquash-engine/pkg/tui/viewtypes"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DashboardView is the main dashboard view
type DashboardView struct {
	viewtypes.BaseView
	migrationDir string
	configPath   string
	config       *config.Config
	stats        *DashboardStats
	selectedIdx  int
	menuItems    []MenuItem
	loading      bool
}

// MenuItem represents a menu item
type MenuItem struct {
	Label       string
	Description string
	Action      ViewAction
}

// ViewAction represents an action to perform
type ViewAction int

const (
	ActionAnalyze ViewAction = iota
	ActionConfigure
	ActionSquash
	ActionDependencyGraph
	ActionHelp
)

// DashboardStats contains dashboard statistics
type DashboardStats struct {
	TotalFiles      int
	TotalSize       int64
	ConfigExists    bool
	ConfigSafety    string
	DetectedPlugins []string
}

// NewDashboardView creates a new dashboard view
func NewDashboardView(migrationDir, configPath string) *DashboardView {
	d := &DashboardView{
		migrationDir: migrationDir,
		configPath:   configPath,
		selectedIdx:  0,
	}

	d.menuItems = []MenuItem{
		{
			Label:       "Analyze Migrations",
			Description: "Explore migration patterns and optimization opportunities",
			Action:      ActionAnalyze,
		},
		{
			Label:       "Configure Settings",
			Description: "Adjust safety levels and consolidation rules",
			Action:      ActionConfigure,
		},
		{
			Label:       "View Dependency Graph",
			Description: "Visualize object dependencies and relationships",
			Action:      ActionDependencyGraph,
		},
		{
			Label:       "Squash Migrations",
			Description: "Consolidate and optimize migration files",
			Action:      ActionSquash,
		},
		{
			Label:       "Help",
			Description: "View keyboard shortcuts and usage information",
			Action:      ActionHelp,
		},
	}

	return d
}

// Init initializes the dashboard
func (d *DashboardView) Init() tea.Cmd {
	return d.loadStats
}

// Update handles messages
func (d *DashboardView) Update(msg tea.Msg) (viewtypes.View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if d.selectedIdx > 0 {
				d.selectedIdx--
			}
			return d, nil

		case "down", "j":
			if d.selectedIdx < len(d.menuItems)-1 {
				d.selectedIdx++
			}
			return d, nil

		case "enter", " ":
			return d, d.executeAction()
		}

	case DashboardStats:
		d.stats = &msg
		d.loading = false
		return d, nil
	}

	return d, nil
}

// View renders the dashboard
func (d *DashboardView) View() string {
	if d.loading {
		return styles.TextStyle.Render("Loading dashboard...")
	}

	var sections []string

	// Title
	title := styles.TitleStyle.Render("pgsquash Interactive Dashboard")
	sections = append(sections, title)

	// Stats section
	if d.stats != nil {
		statsSection := d.renderStats()
		sections = append(sections, statsSection)
	}

	// Menu section
	menuSection := d.renderMenu()
	sections = append(sections, menuSection)

	// Help hint
	helpHint := styles.HelpStyle.Render("Use ↑/↓ or j/k to navigate, Enter to select")
	sections = append(sections, helpHint)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		sections...,
	)
}

// Type returns the view type
func (d *DashboardView) Type() viewtypes.ViewType {
	return viewtypes.ViewDashboard
}

// OnEnter is called when entering the view
func (d *DashboardView) OnEnter() tea.Cmd {
	return d.loadStats
}

// renderStats renders the statistics section
func (d *DashboardView) renderStats() string {
	lines := []string{
		styles.SubtitleStyle.Render("Migration Overview"),
		"",
	}

	// File count
	fileCountStr := fmt.Sprintf("%d files", d.stats.TotalFiles)
	lines = append(lines, styles.StatInfo("Total Migrations", fileCountStr))

	// Size
	sizeStr := formatBytes(d.stats.TotalSize)
	lines = append(lines, styles.StatInfo("Total Size", sizeStr))

	// Config status
	if d.stats.ConfigExists {
		configStr := fmt.Sprintf("Loaded (%s mode)", d.stats.ConfigSafety)
		lines = append(lines, styles.StatSuccess("Configuration", configStr))
	} else {
		lines = append(lines, styles.StatWarning("Configuration", "Not found (using defaults)"))
	}

	// Detected plugins
	if len(d.stats.DetectedPlugins) > 0 {
		pluginsStr := strings.Join(d.stats.DetectedPlugins, ", ")
		lines = append(lines, styles.StatInfo("Detected Plugins", pluginsStr))
	}

	lines = append(lines, "")

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderMenu renders the menu section
func (d *DashboardView) renderMenu() string {
	lines := []string{
		styles.SubtitleStyle.Render("Actions"),
		"",
	}

	for i, item := range d.menuItems {
		var line string
		if i == d.selectedIdx {
			// Selected item
			label := styles.SelectedItemStyle.Render("") + styles.TextStyle.Bold(true).Foreground(styles.Primary).Render(item.Label)
			desc := styles.MutedStyle.Render("  " + item.Description)
			line = label + "\n" + desc
		} else {
			// Unselected item
			label := styles.UnselectedItemStyle.Render("") + styles.TextStyle.Render(item.Label)
			desc := styles.MutedStyle.Render("  " + item.Description)
			line = label + "\n" + desc
		}
		lines = append(lines, line)
		lines = append(lines, "")
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// executeAction executes the selected menu action
func (d *DashboardView) executeAction() tea.Cmd {
	if d.selectedIdx < 0 || d.selectedIdx >= len(d.menuItems) {
		return nil
	}

	action := d.menuItems[d.selectedIdx].Action

	switch action {
	case ActionAnalyze:
		return func() tea.Msg {
			return viewtypes.NavigateMsg{View: viewtypes.ViewAnalysis}
		}
	case ActionConfigure:
		return func() tea.Msg {
			return viewtypes.NavigateMsg{View: viewtypes.ViewConfig}
		}
	case ActionDependencyGraph:
		return func() tea.Msg {
			return viewtypes.NavigateMsg{View: viewtypes.ViewDependencyGraph}
		}
	case ActionSquash:
		return func() tea.Msg {
			return viewtypes.NavigateMsg{View: viewtypes.ViewProgress}
		}
	case ActionHelp:
		return func() tea.Msg {
			return viewtypes.NavigateMsg{View: viewtypes.ViewHelp}
		}
	}

	return nil
}

// loadStats loads dashboard statistics
func (d *DashboardView) loadStats() tea.Msg {
	stats := DashboardStats{}

	// Count migration files
	if d.migrationDir != "" {
		files, err := filepath.Glob(filepath.Join(d.migrationDir, "*.sql"))
		if err == nil {
			stats.TotalFiles = len(files)

			// Calculate total size
			var totalSize int64
			for _, file := range files {
				if info, err := os.Stat(file); err == nil {
					totalSize += info.Size()
				}
			}
			stats.TotalSize = totalSize
		}
	}

	// Check config
	cfg, err := config.LoadConfig(d.configPath)
	if err == nil && cfg != nil {
		stats.ConfigExists = true
		stats.ConfigSafety = string(cfg.SafetyLevel)
		d.config = cfg

		// Detect plugins from config
		if cfg.ThirdPartyIntegrations.SupabaseIntegration.Enabled {
			stats.DetectedPlugins = append(stats.DetectedPlugins, "Supabase")
		}
		if cfg.ThirdPartyIntegrations.ClerkIntegration.Enabled {
			stats.DetectedPlugins = append(stats.DetectedPlugins, "Clerk")
		}
	}

	return stats
}
