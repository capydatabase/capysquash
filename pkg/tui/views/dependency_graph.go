package views

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/capysquash/pgsquash-engine/internal/parser"
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/pkg/tui/styles"
	"github.com/capysquash/pgsquash-engine/pkg/tui/viewtypes"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DependencyGraphView displays object dependency visualization
type DependencyGraphView struct {
	viewtypes.BaseView
	migrationDir string
	dependencies map[string][]string
	objects      []string
	selectedIdx  int
	scrollOffset int
	loading      bool
	err          error
	showReverse  bool
}

// NewDependencyGraphView creates a new dependency graph view
func NewDependencyGraphView(migrationDir string) *DependencyGraphView {
	return &DependencyGraphView{
		migrationDir: migrationDir,
		loading:      true,
		selectedIdx:  0,
		scrollOffset: 0,
		showReverse:  false,
	}
}

// Init initializes the dependency graph view
func (d *DependencyGraphView) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (d *DependencyGraphView) Update(msg tea.Msg) (viewtypes.View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if d.selectedIdx > 0 {
				d.selectedIdx--
				if d.selectedIdx < d.scrollOffset {
					d.scrollOffset--
				}
			}
			return d, nil

		case "down", "j":
			if d.selectedIdx < len(d.objects)-1 {
				d.selectedIdx++
				maxVisible := 15
				if d.selectedIdx >= d.scrollOffset+maxVisible {
					d.scrollOffset++
				}
			}
			return d, nil

		case "r":
			// Toggle reverse dependencies
			d.showReverse = !d.showReverse
			return d, nil

		case "f":
			// Refresh
			d.loading = true
			return d, d.loadDependencies
		}

	case DependencyData:
		d.dependencies = msg.Dependencies
		d.objects = msg.Objects
		d.loading = false
		return d, nil

	case viewtypes.ErrorMsg:
		d.err = msg.Err
		d.loading = false
		return d, nil
	}

	return d, nil
}

// View renders the dependency graph view
func (d *DependencyGraphView) View() string {
	if d.loading {
		return styles.TextStyle.Render("Loading dependency graph...\n\nThis may take a moment for large migration sets.")
	}

	if d.err != nil {
		return styles.ErrorStyle.Render(fmt.Sprintf("Failed to load dependencies: %v\n\nPress 'f' to retry, ESC to return to dashboard.", d.err))
	}

	if len(d.objects) == 0 {
		return styles.MutedStyle.Render("No objects found in migrations.\n\nPress ESC to return to dashboard.")
	}

	var sections []string

	// Title
	mode := "Forward Dependencies"
	if d.showReverse {
		mode = "Reverse Dependencies"
	}
	title := styles.TitleStyle.Render(fmt.Sprintf("Dependency Graph - %s", mode))
	sections = append(sections, title)

	// Object list
	objectList := d.renderObjectList()
	sections = append(sections, objectList)

	// Dependency details
	if d.selectedIdx >= 0 && d.selectedIdx < len(d.objects) {
		details := d.renderDependencyDetails()
		sections = append(sections, details)
	}

	// Help
	help := styles.HelpStyle.Render("↑/↓: Navigate  ►  r: Toggle reverse  ►  f: Refresh  ►  ESC: Back")
	sections = append(sections, help)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		sections...,
	)
}

// Type returns the view type
func (d *DependencyGraphView) Type() viewtypes.ViewType {
	return viewtypes.ViewDependencyGraph
}

// OnEnter is called when entering the view
func (d *DependencyGraphView) OnEnter() tea.Cmd {
	if len(d.objects) == 0 {
		d.loading = true
		return d.loadDependencies
	}
	return nil
}

// renderObjectList renders the list of objects
func (d *DependencyGraphView) renderObjectList() string {
	lines := []string{
		styles.SubtitleStyle.Render("Objects"),
		"",
	}

	maxVisible := 15
	startIdx := d.scrollOffset
	endIdx := d.scrollOffset + maxVisible
	if endIdx > len(d.objects) {
		endIdx = len(d.objects)
	}

	visibleObjects := d.objects[startIdx:endIdx]

	for i, obj := range visibleObjects {
		actualIdx := startIdx + i
		isSelected := actualIdx == d.selectedIdx

		// Count dependencies
		depCount := 0
		if d.showReverse {
			// Count how many objects depend on this one
			for _, deps := range d.dependencies {
				for _, dep := range deps {
					if dep == obj {
						depCount++
					}
				}
			}
		} else {
			// Count dependencies of this object
			if deps, exists := d.dependencies[obj]; exists {
				depCount = len(deps)
			}
		}

		var line string
		if isSelected {
			objName := styles.TextStyle.Bold(true).Foreground(styles.Primary).Render(obj)
			depInfo := styles.MutedStyle.Render(fmt.Sprintf(" (%d)", depCount))
			line = styles.SelectedItemStyle.Render("") + objName + depInfo
		} else {
			objName := styles.TextStyle.Render(obj)
			depInfo := styles.MutedStyle.Render(fmt.Sprintf(" (%d)", depCount))
			line = styles.UnselectedItemStyle.Render("") + objName + depInfo
		}

		lines = append(lines, line)
	}

	// Scroll indicator
	if len(d.objects) > maxVisible {
		scrollInfo := fmt.Sprintf("Showing %d-%d of %d objects",
			startIdx+1,
			endIdx,
			len(d.objects),
		)
		lines = append(lines, "")
		lines = append(lines, styles.MutedStyle.Render(scrollInfo))
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderDependencyDetails renders dependency details for selected object
func (d *DependencyGraphView) renderDependencyDetails() string {
	selectedObj := d.objects[d.selectedIdx]

	lines := []string{
		"",
		styles.SubtitleStyle.Render(fmt.Sprintf("Dependencies for %s", selectedObj)),
		"",
	}

	var deps []string

	if d.showReverse {
		// Show what depends on this object
		for obj, objDeps := range d.dependencies {
			for _, dep := range objDeps {
				if dep == selectedObj {
					deps = append(deps, obj)
				}
			}
		}

		if len(deps) == 0 {
			lines = append(lines, styles.MutedStyle.Render("  No objects depend on this object."))
		} else {
			lines = append(lines, styles.TextStyle.Render(fmt.Sprintf("  %d object(s) depend on this:", len(deps))))
			lines = append(lines, "")
			for _, dep := range deps {
				depLine := "    ► " + styles.TextStyle.Foreground(styles.Secondary).Render(dep)
				lines = append(lines, depLine)
			}
		}
	} else {
		// Show what this object depends on
		if objDeps, exists := d.dependencies[selectedObj]; exists {
			deps = objDeps
		}

		if len(deps) == 0 {
			lines = append(lines, styles.MutedStyle.Render("  No dependencies."))
		} else {
			lines = append(lines, styles.TextStyle.Render(fmt.Sprintf("  Depends on %d object(s):", len(deps))))
			lines = append(lines, "")
			for _, dep := range deps {
				depLine := "    ► " + styles.TextStyle.Foreground(styles.Secondary).Render(dep)
				lines = append(lines, depLine)
			}
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// DependencyData message containing loaded dependencies
type DependencyData struct {
	Dependencies map[string][]string
	Objects      []string
}

// loadDependencies loads the dependency graph
func (d *DependencyGraphView) loadDependencies() tea.Msg {
	// Get migration files
	files, err := filepath.Glob(filepath.Join(d.migrationDir, "*.sql"))
	if err != nil {
		return viewtypes.ErrorMsg{Err: fmt.Errorf("failed to read migration directory: %w", err)}
	}

	// Sort files by name
	sort.Strings(files)

	// Parse migrations and build dependency graph
	tracker := tracking.NewTracker()

	for i, file := range files {
		// Read file
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		// Parse migration
		migration, err := parser.ParseMigration(string(content), filepath.Base(file))
		if err != nil {
			continue
		}

		// Track lifecycle
		tracker.ProcessMigration(migration, i)
	}

	// Get dependency graph
	depGraph := tracker.GetDependencyGraph()

	// Extract objects
	objectSet := make(map[string]bool)
	for obj := range depGraph {
		objectSet[obj] = true
	}
	for _, deps := range depGraph {
		for _, dep := range deps {
			objectSet[dep] = true
		}
	}

	var objects []string
	for obj := range objectSet {
		objects = append(objects, obj)
	}
	sort.Strings(objects)

	return DependencyData{
		Dependencies: depGraph,
		Objects:      objects,
	}
}
