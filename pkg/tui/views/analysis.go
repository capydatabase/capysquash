package views

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/config"
	"github.com/capy-base/pgsquash-engine/internal/parser"
	"github.com/capy-base/pgsquash-engine/internal/tracking"
	"github.com/capy-base/pgsquash-engine/pkg/tui/styles"
	"github.com/capy-base/pgsquash-engine/pkg/tui/viewtypes"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// AnalysisView displays detailed migration analysis
type AnalysisView struct {
	viewtypes.BaseView
	migrationDir string
	stats        *viewtypes.AnalysisStats
	loading      bool
	selectedTab  int
	tabs         []string
	scrollOffset int
	err          error
}

// NewAnalysisView creates a new analysis view
func NewAnalysisView(migrationDir string) *AnalysisView {
	return &AnalysisView{
		migrationDir: migrationDir,
		loading:      true,
		selectedTab:  0,
		tabs:         []string{"Overview", "Lifecycle Patterns", "Dependencies", "Issues"},
		scrollOffset: 0,
	}
}

// Init initializes the analysis view
func (a *AnalysisView) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (a *AnalysisView) Update(msg tea.Msg) (viewtypes.View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			if a.selectedTab > 0 {
				a.selectedTab--
				a.scrollOffset = 0
			}
			return a, nil

		case "right", "l":
			if a.selectedTab < len(a.tabs)-1 {
				a.selectedTab++
				a.scrollOffset = 0
			}
			return a, nil

		case "up", "k":
			if a.scrollOffset > 0 {
				a.scrollOffset--
			}
			return a, nil

		case "down", "j":
			a.scrollOffset++
			return a, nil

		case "r":
			// Refresh analysis
			a.loading = true
			return a, a.runAnalysis
		}

	case viewtypes.AnalysisCompleteMsg:
		a.stats = msg.Stats
		a.loading = false
		return a, nil

	case viewtypes.ErrorMsg:
		a.err = msg.Err
		a.loading = false
		return a, nil
	}

	return a, nil
}

// View renders the analysis view
func (a *AnalysisView) View() string {
	if a.loading {
		return styles.TextStyle.Render("Analyzing migrations...\n\nThis may take a few moments for large migration sets.")
	}

	if a.err != nil {
		return styles.ErrorStyle.Render(fmt.Sprintf("Analysis failed: %v\n\nPress 'r' to retry, ESC to return to dashboard.", a.err))
	}

	if a.stats == nil {
		return styles.TextStyle.Render("No analysis data available. Press 'r' to run analysis.")
	}

	var sections []string

	// Title
	title := styles.TitleStyle.Render("Migration Analysis")
	sections = append(sections, title)

	// Tabs
	tabBar := a.renderTabs()
	sections = append(sections, tabBar)

	// Content based on selected tab
	var content string
	switch a.selectedTab {
	case 0:
		content = a.renderOverview()
	case 1:
		content = a.renderLifecyclePatterns()
	case 2:
		content = a.renderDependencies()
	case 3:
		content = a.renderIssues()
	}
	sections = append(sections, content)

	// Help
	help := styles.HelpStyle.Render("←/→: Switch tabs  ►  ↑/↓: Scroll  ►  r: Refresh  ►  ESC: Back")
	sections = append(sections, help)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		sections...,
	)
}

// Type returns the view type
func (a *AnalysisView) Type() viewtypes.ViewType {
	return viewtypes.ViewAnalysis
}

// OnEnter is called when entering the view
func (a *AnalysisView) OnEnter() tea.Cmd {
	if a.stats == nil {
		a.loading = true
		return a.runAnalysis
	}
	return nil
}

// renderTabs renders the tab bar
func (a *AnalysisView) renderTabs() string {
	var tabs []string

	for i, tab := range a.tabs {
		var style lipgloss.Style
		if i == a.selectedTab {
			style = lipgloss.NewStyle().
				Bold(true).
				Foreground(styles.Primary).
				Background(lipgloss.Color("#2D3748")).
				Padding(0, 2)
		} else {
			style = lipgloss.NewStyle().
				Foreground(styles.Muted).
				Padding(0, 2)
		}
		tabs = append(tabs, style.Render(tab))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...) + "\n"
}

// renderOverview renders the overview tab
func (a *AnalysisView) renderOverview() string {
	lines := []string{
		styles.SubtitleStyle.Render("Analysis Summary"),
		"",
	}

	// Migration count
	lines = append(lines, styles.StatInfo("Total Migrations", fmt.Sprintf("%d", a.stats.TotalMigrations)))

	// Objects created
	lines = append(lines, styles.StatInfo("Objects Created", fmt.Sprintf("%d", a.stats.ObjectsCreated)))
	lines = append(lines, styles.StatInfo("  Tables", fmt.Sprintf("%d", a.stats.TablesCreated)))
	lines = append(lines, styles.StatInfo("  Functions", fmt.Sprintf("%d", a.stats.FunctionsCreated)))
	lines = append(lines, styles.StatInfo("  Indexes", fmt.Sprintf("%d", a.stats.IndexesCreated)))

	lines = append(lines, "")

	// Optimization potential
	lines = append(lines, styles.SubtitleStyle.Render("Optimization Potential"))
	lines = append(lines, "")

	savingsPercent := fmt.Sprintf("%.1f%%", a.stats.PotentialSavings*100)
	if a.stats.PotentialSavings > 0.3 {
		lines = append(lines, styles.StatSuccess("Potential Savings", savingsPercent))
	} else if a.stats.PotentialSavings > 0.15 {
		lines = append(lines, styles.StatWarning("Potential Savings", savingsPercent))
	} else {
		lines = append(lines, styles.StatInfo("Potential Savings", savingsPercent))
	}

	fileReduction := fmt.Sprintf("-%d files", a.stats.FileReduction)
	lines = append(lines, styles.StatInfo("File Reduction", fileReduction))

	lines = append(lines, "")

	// Lifecycle patterns summary
	optimizableCount := 0
	for _, lp := range a.stats.LifecyclePatterns {
		if lp.Optimizable {
			optimizableCount++
		}
	}

	lines = append(lines, styles.SubtitleStyle.Render("Lifecycle Analysis"))
	lines = append(lines, "")
	lines = append(lines, styles.StatInfo("Total Patterns", fmt.Sprintf("%d", len(a.stats.LifecyclePatterns))))
	lines = append(lines, styles.StatSuccess("Optimizable", fmt.Sprintf("%d", optimizableCount)))

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderLifecyclePatterns renders the lifecycle patterns tab
func (a *AnalysisView) renderLifecyclePatterns() string {
	lines := []string{
		styles.SubtitleStyle.Render("Object Lifecycle Patterns"),
		"",
	}

	if len(a.stats.LifecyclePatterns) == 0 {
		lines = append(lines, styles.MutedStyle.Render("No lifecycle patterns detected."))
		return lipgloss.JoinVertical(lipgloss.Left, lines...)
	}

	// Apply scroll offset
	visiblePatterns := a.stats.LifecyclePatterns
	if a.scrollOffset > 0 && a.scrollOffset < len(visiblePatterns) {
		visiblePatterns = visiblePatterns[a.scrollOffset:]
	}

	// Limit to visible height
	maxVisible := 15
	if len(visiblePatterns) > maxVisible {
		visiblePatterns = visiblePatterns[:maxVisible]
	}

	for _, pattern := range visiblePatterns {
		// Object name and type
		objectLine := fmt.Sprintf("%s %s",
			styles.TextStyle.Bold(true).Render(pattern.ObjectName),
			styles.MutedStyle.Render(fmt.Sprintf("(%s)", pattern.ObjectType)),
		)
		lines = append(lines, objectLine)

		// Operations
		opsLine := "  Operations: " + styles.MutedStyle.Render(strings.Join(pattern.Operations, " → "))
		lines = append(lines, opsLine)

		// Optimizable status
		if pattern.Optimizable {
			lines = append(lines, "  "+styles.SuccessBadge("OPTIMIZABLE"))
		} else {
			lines = append(lines, "  "+styles.InfoBadge("PRESERVED"))
		}

		// Warning if present
		if pattern.Warning != "" {
			lines = append(lines, "  "+styles.WarningStyle.Render("⚠ "+pattern.Warning))
		}

		lines = append(lines, "")
	}

	// Scroll indicator
	if a.scrollOffset > 0 || len(a.stats.LifecyclePatterns) > maxVisible {
		scrollInfo := fmt.Sprintf("Showing %d-%d of %d patterns",
			a.scrollOffset+1,
			a.scrollOffset+len(visiblePatterns),
			len(a.stats.LifecyclePatterns),
		)
		lines = append(lines, styles.MutedStyle.Render(scrollInfo))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderDependencies renders the dependencies tab
func (a *AnalysisView) renderDependencies() string {
	lines := []string{
		styles.SubtitleStyle.Render("Object Dependencies"),
		"",
	}

	if len(a.stats.Dependencies) == 0 {
		lines = append(lines, styles.MutedStyle.Render("No dependencies detected."))
		return lipgloss.JoinVertical(lipgloss.Left, lines...)
	}

	// Apply scroll offset
	visibleDeps := a.stats.Dependencies
	if a.scrollOffset > 0 && a.scrollOffset < len(visibleDeps) {
		visibleDeps = visibleDeps[a.scrollOffset:]
	}

	// Limit to visible height
	maxVisible := 20
	if len(visibleDeps) > maxVisible {
		visibleDeps = visibleDeps[:maxVisible]
	}

	for _, dep := range visibleDeps {
		depLine := fmt.Sprintf("%s → %s %s",
			styles.TextStyle.Foreground(styles.Secondary).Render(dep.Source),
			styles.TextStyle.Foreground(styles.Primary).Render(dep.Target),
			styles.MutedStyle.Render(fmt.Sprintf("(%s)", dep.Type)),
		)
		lines = append(lines, depLine)
	}

	// Scroll indicator
	if a.scrollOffset > 0 || len(a.stats.Dependencies) > maxVisible {
		scrollInfo := fmt.Sprintf("Showing %d-%d of %d dependencies",
			a.scrollOffset+1,
			a.scrollOffset+len(visibleDeps),
			len(a.stats.Dependencies),
		)
		lines = append(lines, "")
		lines = append(lines, styles.MutedStyle.Render(scrollInfo))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderIssues renders the issues tab
func (a *AnalysisView) renderIssues() string {
	lines := []string{
		styles.SubtitleStyle.Render("Issues & Warnings"),
		"",
	}

	hasIssues := false

	// Errors
	if len(a.stats.Errors) > 0 {
		hasIssues = true
		lines = append(lines, styles.ErrorStyle.Render(fmt.Sprintf("Errors (%d):", len(a.stats.Errors))))
		lines = append(lines, "")

		for _, err := range a.stats.Errors {
			lines = append(lines, "  "+styles.ErrorStyle.Render("✗")+" "+err)
		}
		lines = append(lines, "")
	}

	// Warnings
	if len(a.stats.Warnings) > 0 {
		hasIssues = true
		lines = append(lines, styles.WarningStyle.Render(fmt.Sprintf("Warnings (%d):", len(a.stats.Warnings))))
		lines = append(lines, "")

		for _, warning := range a.stats.Warnings {
			lines = append(lines, "  "+styles.WarningStyle.Render("⚠")+" "+warning)
		}
	}

	if !hasIssues {
		lines = append(lines, styles.SuccessStyle.Render("☑ No issues detected!"))
		lines = append(lines, "")
		lines = append(lines, styles.MutedStyle.Render("Your migrations are ready for squashing."))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// runAnalysis runs the migration analysis
func (a *AnalysisView) runAnalysis() tea.Msg {
	stats := &viewtypes.AnalysisStats{
		Warnings: []string{},
		Errors:   []string{},
	}

	// Get migration files
	files, err := filepath.Glob(filepath.Join(a.migrationDir, "*.sql"))
	if err != nil {
		return viewtypes.ErrorMsg{Err: fmt.Errorf("failed to read migration directory: %w", err)}
	}

	stats.TotalMigrations = len(files)

	// Sort files by name
	sort.Strings(files)

	// Parse migrations and track objects
	tracker := tracking.NewTracker()

	for i, file := range files {
		// Read file
		content, err := os.ReadFile(file)
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("%s: %v", filepath.Base(file), err))
			continue
		}

		// Parse migration
		migration, err := parser.ParseMigration(string(content), filepath.Base(file))
		if err != nil {
			stats.Errors = append(stats.Errors, fmt.Sprintf("%s: %v", filepath.Base(file), err))
			continue
		}

		// Track lifecycle
		tracker.ProcessMigration(migration, i)
	}

	// Get statistics from tracker
	trackerStats := tracker.GetStatistics()

	stats.ObjectsCreated = trackerStats.TotalObjects

	// Calculate counts from ObjectsByType map
	if trackerStats.ObjectsByType != nil {
		for objType, count := range trackerStats.ObjectsByType {
			switch objType {
			case "TABLE":
				stats.TablesCreated += count
			case "FUNCTION":
				stats.FunctionsCreated += count
			case "INDEX":
				stats.IndexesCreated += count
			}
		}
	}

	// Calculate potential savings
	redundantObjs := tracker.GetRedundantObjects()
	if stats.ObjectsCreated > 0 {
		stats.PotentialSavings = float64(len(redundantObjs)) / float64(stats.ObjectsCreated)
	}

	// Estimate file reduction
	stats.FileReduction = int(float64(stats.TotalMigrations) * stats.PotentialSavings)

	// Build lifecycle patterns
	lifecycles := tracker.GetObjects()
	for objName, lifecycle := range lifecycles {
		pattern := viewtypes.LifecyclePattern{
			ObjectName: objName,
			ObjectType: string(lifecycle.Type),
			Operations: []string{},
		}

		// Build operation list from history
		if len(lifecycle.History) > 0 {
			for _, event := range lifecycle.History {
				switch event.Operation {
				case "CREATE":
					pattern.Operations = append(pattern.Operations, "CREATE")
				case "ALTER", "MODIFY":
					if len(pattern.Operations) == 0 || pattern.Operations[len(pattern.Operations)-1] != "ALTER" {
						pattern.Operations = append(pattern.Operations, "ALTER")
					}
				case "DROP":
					pattern.Operations = append(pattern.Operations, "DROP")
				}
			}
		}

		// Determine if optimizable (has multiple ALTERs or CREATE-DROP cycle)
		alterCount := 0
		hasCreate := false
		hasDrop := false
		for _, op := range pattern.Operations {
			if op == "ALTER" {
				alterCount++
			}
			if op == "CREATE" {
				hasCreate = true
			}
			if op == "DROP" {
				hasDrop = true
			}
		}

		pattern.Optimizable = alterCount > 2 || (hasCreate && hasDrop)

		if hasCreate && hasDrop {
			pattern.Warning = "CREATE-DROP cycle detected"
		}

		stats.LifecyclePatterns = append(stats.LifecyclePatterns, pattern)
	}

	// Build dependency info
	depGraph := tracker.GetDependencyGraph()
	for source, targets := range depGraph {
		for _, target := range targets {
			stats.Dependencies = append(stats.Dependencies, viewtypes.DependencyInfo{
				Source: source,
				Target: target,
				Type:   "depends_on",
			})
		}
	}

	// Load config to check for issues
	cfg, _ := config.LoadConfig("")
	if cfg == nil {
		stats.Warnings = append(stats.Warnings, "No configuration file found - using default settings")
	}

	return viewtypes.AnalysisCompleteMsg{Stats: stats}
}
