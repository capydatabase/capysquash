package views

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/config"
	"github.com/capysquash/pgsquash-engine/internal/squasher"
	"github.com/capysquash/pgsquash-engine/pkg/tui/styles"
	"github.com/capysquash/pgsquash-engine/pkg/tui/viewtypes"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ProgressView displays real-time progress of squashing operation
type ProgressView struct {
	viewtypes.BaseView
	migrationDir string
	outputDir    string
	configPath   string
	progress     float64
	currentPhase string
	message      string
	complete     bool
	stats        *viewtypes.SquashStats
	startTime    time.Time
	err          error
	logs         []string
}

// NewProgressView creates a new progress view
func NewProgressView(migrationDir, configPath string) *ProgressView {
	return &ProgressView{
		migrationDir: migrationDir,
		configPath:   configPath,
		progress:     0,
		currentPhase: "Initializing",
		logs:         []string{},
	}
}

// Init initializes the progress view
func (p *ProgressView) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (p *ProgressView) Update(msg tea.Msg) (viewtypes.View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if p.complete {
				// Return to dashboard
				return p, func() tea.Msg {
					return viewtypes.NavigateMsg{View: viewtypes.ViewDashboard}
				}
			}
		}

	case ProgressUpdate:
		p.progress = msg.Progress
		p.currentPhase = msg.Phase
		p.message = msg.Message
		return p, nil

	case LogMessage:
		p.logs = append(p.logs, msg.Message)
		// Keep only last 10 logs
		if len(p.logs) > 10 {
			p.logs = p.logs[len(p.logs)-10:]
		}
		return p, nil

	case viewtypes.SquashCompleteMsg:
		p.complete = true
		p.progress = 1.0
		p.currentPhase = "Complete"
		p.stats = msg.Stats
		return p, nil

	case viewtypes.ErrorMsg:
		p.err = msg.Err
		p.complete = true
		return p, nil

	case SquashConfig:
		p.migrationDir = msg.MigrationDir
		p.outputDir = msg.OutputDir
		p.configPath = msg.ConfigPath
		p.startTime = time.Now()
		return p, p.runSquash
	}

	return p, nil
}

// View renders the progress view
func (p *ProgressView) View() string {
	var sections []string

	// Title
	title := styles.TitleStyle.Render("Migration Squashing Progress")
	sections = append(sections, title)

	if p.err != nil {
		// Error state
		errorMsg := styles.ErrorStyle.Render(fmt.Sprintf("✗ Error: %v", p.err))
		sections = append(sections, "")
		sections = append(sections, errorMsg)
		sections = append(sections, "")
		sections = append(sections, styles.HelpStyle.Render("Press ESC to return to dashboard"))
	} else if p.complete {
		// Complete state
		sections = append(sections, p.renderComplete())
	} else {
		// In progress state
		sections = append(sections, p.renderProgress())
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		sections...,
	)
}

// Type returns the view type
func (p *ProgressView) Type() viewtypes.ViewType {
	return viewtypes.ViewProgress
}

// OnEnter is called when entering the view
func (p *ProgressView) OnEnter() tea.Cmd {
	// Start squashing when entering the view
	// Reset state for new squashing operation
	p.complete = false
	p.err = nil
	p.progress = 0
	p.logs = []string{}
	p.startTime = time.Now()

	return func() tea.Msg {
		return SquashConfig{
			MigrationDir: p.migrationDir,
			OutputDir:    "squashed",
			ConfigPath:   p.configPath,
		}
	}
}

// renderProgress renders the in-progress state
func (p *ProgressView) renderProgress() string {
	lines := []string{
		"",
		styles.SubtitleStyle.Render("Current Phase"),
		"",
	}

	// Phase indicator
	phaseText := styles.TextStyle.Bold(true).Foreground(styles.Primary).Render(p.currentPhase)
	lines = append(lines, "  "+phaseText)

	if p.message != "" {
		messageText := styles.MutedStyle.Render(p.message)
		lines = append(lines, "  "+messageText)
	}

	lines = append(lines, "")

	// Progress bar
	progressBar := p.renderProgressBar()
	lines = append(lines, progressBar)

	lines = append(lines, "")

	// Elapsed time
	if !p.startTime.IsZero() {
		elapsed := time.Since(p.startTime)
		elapsedText := styles.StatInfo("Elapsed Time", formatDuration(elapsed))
		lines = append(lines, elapsedText)
	}

	// Recent logs
	if len(p.logs) > 0 {
		lines = append(lines, "")
		lines = append(lines, styles.SubtitleStyle.Render("Recent Activity"))
		lines = append(lines, "")

		for _, log := range p.logs {
			lines = append(lines, "  "+styles.MutedStyle.Render("► "+log))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderComplete renders the completion state
func (p *ProgressView) renderComplete() string {
	lines := []string{
		"",
		styles.SuccessStyle.Render("☑ Squashing Complete!"),
		"",
	}

	if p.stats != nil {
		// Statistics
		lines = append(lines, styles.SubtitleStyle.Render("Results"))
		lines = append(lines, "")

		lines = append(lines, styles.StatInfo("Original Files", fmt.Sprintf("%d", p.stats.OriginalFiles)))
		lines = append(lines, styles.StatSuccess("Squashed Files", fmt.Sprintf("%d", p.stats.SquashedFiles)))

		reduction := p.stats.OriginalFiles - p.stats.SquashedFiles
		reductionPercent := float64(reduction) / float64(p.stats.OriginalFiles) * 100
		lines = append(lines, styles.StatSuccess("Reduction", fmt.Sprintf("%d files (%.1f%%)", reduction, reductionPercent)))

		if p.stats.BytesSaved > 0 {
			lines = append(lines, styles.StatInfo("Space Saved", formatBytes(p.stats.BytesSaved)))
		}

		lines = append(lines, styles.StatInfo("Objects Optimized", fmt.Sprintf("%d", p.stats.ObjectsOptimized)))
		lines = append(lines, styles.StatInfo("Duration", formatDuration(time.Duration(p.stats.Duration*float64(time.Second)))))

		lines = append(lines, "")
		lines = append(lines, styles.InfoBadge("Output: "+p.outputDir))
	}

	lines = append(lines, "")
	lines = append(lines, styles.HelpStyle.Render("Press Enter to return to dashboard"))

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// renderProgressBar renders a progress bar
func (p *ProgressView) renderProgressBar() string {
	width := 50
	filled := int(p.progress * float64(width))
	if filled > width {
		filled = width
	}

	percent := fmt.Sprintf("%.0f%%", p.progress*100)

	barStyle := lipgloss.NewStyle().Foreground(styles.Success)
	emptyStyle := lipgloss.NewStyle().Foreground(styles.Muted)

	styledBar := barStyle.Render(strings.Repeat("█", filled)) +
		emptyStyle.Render(strings.Repeat("░", width-filled))

	return "  " + styledBar + "  " + styles.TextStyle.Render(percent)
}

// ProgressUpdate message for progress updates
type ProgressUpdate struct {
	Progress float64
	Phase    string
	Message  string
}

// LogMessage message for log entries
type LogMessage struct {
	Message string
}

// SquashConfig message to start squashing
type SquashConfig struct {
	MigrationDir string
	OutputDir    string
	ConfigPath   string
}

// runSquash runs the squashing operation
func (p *ProgressView) runSquash() tea.Msg {
	// Load config
	cfg, err := config.LoadConfig(p.configPath)
	if err != nil {
		// Use default config
		cfg = &config.Config{
			SafetyLevel: "standard",
			Output: config.OutputConfig{
				Format:                   "organized",
				Directory:                p.outputDir,
				PreserveComments:         true,
				AddConsolidationComments: true,
			},
		}
	} else {
		// Override output directory
		cfg.Output.Directory = p.outputDir
	}

	// Create engine config
	engineCfg := squasher.EngineConfig{
		Config:              cfg,
		EnableStreaming:     false,
		EnableProgressTrack: false,
	}

	// Create engine
	engine := squasher.NewEngine(engineCfg)

	// Get migration files
	files, err := filepath.Glob(filepath.Join(p.migrationDir, "*.sql"))
	if err != nil {
		return viewtypes.ErrorMsg{Err: fmt.Errorf("failed to read migrations: %w", err)}
	}

	sort.Strings(files)

	originalFileCount := len(files)

	// Read migrations
	migrationMap := make(map[int]string)
	for i, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		migrationMap[i] = string(content)
	}

	// Run squashing
	outputSQL, warnings, err := engine.Squash(migrationMap)
	if err != nil {
		return viewtypes.ErrorMsg{Err: fmt.Errorf("squashing failed: %w", err)}
	}

	// Log warnings if any
	if len(warnings) > 0 {
		p.logs = append(p.logs, warnings...)
	}

	// Create output directory
	if err := os.MkdirAll(p.outputDir, 0755); err != nil {
		return viewtypes.ErrorMsg{Err: fmt.Errorf("failed to create output directory: %w", err)}
	}

	// Write output file
	outputPath := filepath.Join(p.outputDir, "squashed.sql")
	if err := os.WriteFile(outputPath, []byte(outputSQL), 0644); err != nil {
		return viewtypes.ErrorMsg{Err: fmt.Errorf("failed to write output: %w", err)}
	}

	// Calculate stats
	duration := time.Since(p.startTime).Seconds()

	// Get stats from engine
	engineStats := engine.GetStats()

	stats := viewtypes.SquashStats{
		OriginalFiles:    originalFileCount,
		SquashedFiles:    1, // Single output file
		ObjectsOptimized: int(engineStats.ConsolidationsApplied),
		Duration:         duration,
		BytesSaved:       int64(len(strings.Join(files, "")) - len(outputSQL)),
	}

	return viewtypes.SquashCompleteMsg{
		OutputPath: outputPath,
		Stats:      &stats,
	}
}

// formatDuration formats a duration into human-readable format
func formatDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	} else if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	} else {
		return fmt.Sprintf("%.1fm", d.Minutes())
	}
}
