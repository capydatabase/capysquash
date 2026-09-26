package views

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/capydatabase/capysquash/internal/config"
	"github.com/capydatabase/capysquash/internal/tui/styles"
	"github.com/capydatabase/capysquash/internal/tui/viewtypes"
	"github.com/capydatabase/capysquash/internal/validation"
)

// ValidationView lints the migrations with the static rules `capysquash
// lint` uses and renders the result.
type ValidationView struct {
	viewtypes.BaseView
	migrationDir string
	configPath   string
	result       *viewtypes.ValidationResultMsg
	loading      bool
}

// NewValidationView creates a new validation view
func NewValidationView(migrationDir, configPath string) *ValidationView {
	return &ValidationView{
		migrationDir: migrationDir,
		configPath:   configPath,
	}
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
	sections := []string{styles.TitleStyle.Render("Migration Validation")}

	switch {
	case v.loading:
		sections = append(sections, styles.MutedStyle.Render("Validating migrations..."))
	case v.result == nil:
		sections = append(sections, styles.MutedStyle.Render("No validation results yet."))
	default:
		sections = append(sections, v.renderResult())
	}

	help := styles.HelpStyle.Render("Static lint rules, as in capysquash lint  ►  v: Back  ►  ESC: Back")
	sections = append(sections, help)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (v *ValidationView) renderResult() string {
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

	// Findings are long; size the box to the view so they wrap inside it
	// instead of running off the right edge.
	box := styles.BoxStyle
	if v.Width > 0 {
		box = box.Width(v.Width)
	}
	return box.Render(sb.String())
}

func (v *ValidationView) Type() viewtypes.ViewType {
	return viewtypes.ViewValidation
}

// OnEnter re-runs the lint every time the view is opened.
func (v *ValidationView) OnEnter() tea.Cmd {
	v.loading = true
	v.result = nil
	return v.runValidation
}

// runValidation lints every migration the way `capysquash lint` does: the
// configured static rules, safety and breaking violations as errors (they
// fail lint), hygiene violations as warnings. Files that cannot be read or
// parsed are errors too.
func (v *ValidationView) runValidation() tea.Msg {
	res := viewtypes.ValidationResultMsg{Errors: []string{}, Warnings: []string{}}
	fail := func(format string, args ...any) tea.Msg {
		res.Errors = append(res.Errors, fmt.Sprintf(format, args...))
		return res
	}

	cfg, err := config.LoadConfig(v.configPath)
	if err != nil {
		return fail("load configuration: %v", err)
	}
	staticCfg := cfg.StaticValidation
	if staticCfg.EnabledRules, err = validation.ResolveEnabledRules(staticCfg.EnabledRules, nil, nil); err != nil {
		return fail("static_validation: %v", err)
	}
	validator := validation.NewStaticValidator(&staticCfg)

	files, err := filepath.Glob(filepath.Join(v.migrationDir, "*.sql"))
	if err != nil {
		return fail("read migration directory: %v", err)
	}
	sort.Strings(files)

	for _, file := range files {
		name := filepath.Base(file)
		content, err := os.ReadFile(file)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		violations, err := validator.Check(string(content))
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		for _, vl := range violations {
			line := fmt.Sprintf("%s:%d [%s] %s: %s", name, vl.Line, vl.Category, vl.Code, vl.Message)
			if vl.Category == validation.CategoryHygiene {
				res.Warnings = append(res.Warnings, line)
			} else {
				res.Errors = append(res.Errors, line)
			}
		}
	}

	res.Success = len(res.Errors) == 0
	return res
}
