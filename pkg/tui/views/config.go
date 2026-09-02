package views

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/capy-base/pgsquash-engine/internal/config"
	"github.com/capy-base/pgsquash-engine/pkg/tui/styles"
	"github.com/capy-base/pgsquash-engine/pkg/tui/viewtypes"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ConfigView displays and allows editing configuration
type ConfigView struct {
	viewtypes.BaseView
	configPath  string
	config      *config.Config
	selectedIdx int
	editingIdx  int
	fields      []ConfigField
	dirty       bool
	saveStatus  string
}

// ConfigField represents a configurable field
type ConfigField struct {
	Label        string
	Description  string
	CurrentValue string
	Options      []string
	Type         FieldType
	ConfigPath   []string
}

// FieldType represents the type of field
type FieldType int

const (
	FieldTypeSelect FieldType = iota
	FieldTypeToggle
	FieldTypeText
)

// NewConfigView creates a new config view
func NewConfigView(configPath string) *ConfigView {
	return &ConfigView{
		configPath:  configPath,
		selectedIdx: 0,
		editingIdx:  -1,
	}
}

// Init initializes the config view
func (c *ConfigView) Init() tea.Cmd {
	return c.loadConfig
}

// Update handles messages
func (c *ConfigView) Update(msg tea.Msg) (viewtypes.View, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if c.editingIdx >= 0 {
			return c.handleEditMode(msg)
		}
		return c.handleNavigationMode(msg)

	case config.Config:
		c.config = &msg
		c.buildFields()
		return c, nil

	case viewtypes.ConfigSavedMsg:
		c.saveStatus = "Configuration saved successfully!"
		c.dirty = false
		return c, nil
	}

	return c, nil
}

// handleNavigationMode handles input in navigation mode
func (c *ConfigView) handleNavigationMode(msg tea.KeyMsg) (viewtypes.View, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if c.selectedIdx > 0 {
			c.selectedIdx--
		}
		return c, nil

	case "down", "j":
		if c.selectedIdx < len(c.fields)-1 {
			c.selectedIdx++
		}
		return c, nil

	case "enter", " ":
		// Enter edit mode
		c.editingIdx = c.selectedIdx
		return c, nil

	case "s", "ctrl+s":
		if c.dirty {
			return c, c.saveConfig
		}
		return c, nil

	case "d":
		// Reset to defaults
		return c, c.resetToDefaults
	}

	return c, nil
}

// handleEditMode handles input in edit mode
func (c *ConfigView) handleEditMode(msg tea.KeyMsg) (viewtypes.View, tea.Cmd) {
	field := c.fields[c.editingIdx]

	switch msg.String() {
	case "esc":
		c.editingIdx = -1
		return c, nil

	case "left", "h":
		switch field.Type {
		case FieldTypeSelect:
			c.cyclePrevious(c.editingIdx)
			c.dirty = true
		case FieldTypeToggle:
			c.toggleField(c.editingIdx)
			c.dirty = true
		}
		return c, nil

	case "right", "l", "enter", " ":
		switch field.Type {
		case FieldTypeSelect:
			c.cycleNext(c.editingIdx)
			c.dirty = true
		case FieldTypeToggle:
			c.toggleField(c.editingIdx)
			c.dirty = true
		}
		c.editingIdx = -1
		return c, nil
	}

	return c, nil
}

// View renders the config view
func (c *ConfigView) View() string {
	if c.config == nil {
		return styles.TextStyle.Render("Loading configuration...")
	}

	var sections []string

	// Title
	title := styles.TitleStyle.Render("Configuration Wizard")
	sections = append(sections, title)

	// Status
	if c.dirty {
		status := styles.WarningStyle.Render("● Unsaved changes")
		sections = append(sections, status)
	} else if c.saveStatus != "" {
		status := styles.SuccessStyle.Render(c.saveStatus)
		sections = append(sections, status)
	}
	sections = append(sections, "")

	// Fields
	for i, field := range c.fields {
		fieldView := c.renderField(i, field)
		sections = append(sections, fieldView)
	}

	sections = append(sections, "")

	// Help
	if c.editingIdx >= 0 {
		help := styles.HelpStyle.Render("←/→: Change value  ►  Enter: Confirm  ►  ESC: Cancel")
		sections = append(sections, help)
	} else {
		help := styles.HelpStyle.Render("↑/↓: Navigate  ►  Enter: Edit  ►  s: Save  ►  d: Reset to defaults  ►  ESC: Back")
		sections = append(sections, help)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		sections...,
	)
}

// Type returns the view type
func (c *ConfigView) Type() viewtypes.ViewType {
	return viewtypes.ViewConfig
}

// OnEnter is called when entering the view
func (c *ConfigView) OnEnter() tea.Cmd {
	return c.loadConfig
}

// renderField renders a single config field
func (c *ConfigView) renderField(idx int, field ConfigField) string {
	isSelected := idx == c.selectedIdx
	isEditing := idx == c.editingIdx

	var lines []string

	// Label
	label := field.Label
	if isSelected {
		label = styles.SelectedItemStyle.Render("") + styles.TextStyle.Bold(true).Foreground(styles.Primary).Render(label)
	} else {
		label = styles.UnselectedItemStyle.Render("") + styles.TextStyle.Render(label)
	}
	lines = append(lines, label)

	// Description
	desc := "  " + styles.MutedStyle.Render(field.Description)
	lines = append(lines, desc)

	// Value
	var value string
	if isEditing {
		value = "  " + styles.ActiveBoxStyle.Width(40).Render("◄ "+field.CurrentValue+" ►")
	} else {
		valueStyle := styles.TextStyle.Foreground(styles.Secondary)
		if field.Type == FieldTypeToggle {
			if field.CurrentValue == "true" {
				value = "  " + styles.SuccessBadge("ENABLED")
			} else {
				value = "  " + styles.MutedStyle.Render("[DISABLED]")
			}
		} else {
			value = "  " + valueStyle.Render(field.CurrentValue)
		}
	}
	lines = append(lines, value)

	lines = append(lines, "")

	return lipgloss.JoinVertical(lipgloss.Left, lines...)
}

// buildFields builds the configuration fields from config
func (c *ConfigView) buildFields() {
	c.fields = []ConfigField{
		{
			Label:        "Safety Level",
			Description:  "Controls how aggressive consolidation optimizations are",
			CurrentValue: string(c.config.SafetyLevel),
			Options:      []string{"paranoid", "conservative", "standard", "aggressive"},
			Type:         FieldTypeSelect,
		},
		{
			Label:        "Output Format",
			Description:  "How to organize the squashed migration files",
			CurrentValue: c.config.Output.Format,
			Options:      []string{"organized", "sequential", "minimal"},
			Type:         FieldTypeSelect,
		},
		{
			Label:        "Preserve Comments",
			Description:  "Keep comments from original migrations",
			CurrentValue: fmt.Sprintf("%t", c.config.Output.PreserveComments),
			Type:         FieldTypeToggle,
		},
		{
			Label:        "Add Consolidation Comments",
			Description:  "Add comments explaining consolidation decisions",
			CurrentValue: fmt.Sprintf("%t", c.config.Output.AddConsolidationComments),
			Type:         FieldTypeToggle,
		},
		{
			Label:        "Consolidate CREATE + ALTER",
			Description:  "Merge CREATE statements with subsequent ALTERs",
			CurrentValue: fmt.Sprintf("%t", c.config.Rules.TableOperations.ConsolidateCreateAlter),
			Type:         FieldTypeToggle,
		},
		{
			Label:        "Remove DROP-CREATE Cycles",
			Description:  "Eliminate redundant DROP followed by CREATE",
			CurrentValue: fmt.Sprintf("%t", c.config.Rules.TableOperations.RemoveDropCreateCycles),
			Type:         FieldTypeToggle,
		},
		{
			Label:        "Parallel Processing",
			Description:  "Use multiple CPU cores for parsing",
			CurrentValue: fmt.Sprintf("%t", c.config.Performance.ParallelProcessing),
			Type:         FieldTypeToggle,
		},
		{
			Label:        "Show Progress",
			Description:  "Display progress bars during operations",
			CurrentValue: fmt.Sprintf("%t", c.config.Performance.ShowProgress),
			Type:         FieldTypeToggle,
		},
		{
			Label:        "Supabase Integration",
			Description:  "Enable Supabase-specific features and compatibility",
			CurrentValue: fmt.Sprintf("%t", c.config.ThirdPartyIntegrations.SupabaseIntegration.Enabled),
			Type:         FieldTypeToggle,
		},
		{
			Label:        "Clerk Integration",
			Description:  "Enable Clerk authentication integration",
			CurrentValue: fmt.Sprintf("%t", c.config.ThirdPartyIntegrations.ClerkIntegration.Enabled),
			Type:         FieldTypeToggle,
		},
	}
}

// cycleNext cycles to the next option
func (c *ConfigView) cycleNext(idx int) {
	field := &c.fields[idx]
	if field.Type != FieldTypeSelect {
		return
	}

	for i, opt := range field.Options {
		if opt == field.CurrentValue {
			nextIdx := (i + 1) % len(field.Options)
			field.CurrentValue = field.Options[nextIdx]
			c.applyFieldChange(*field)
			return
		}
	}
}

// cyclePrevious cycles to the previous option
func (c *ConfigView) cyclePrevious(idx int) {
	field := &c.fields[idx]
	if field.Type != FieldTypeSelect {
		return
	}

	for i, opt := range field.Options {
		if opt == field.CurrentValue {
			prevIdx := (i - 1 + len(field.Options)) % len(field.Options)
			field.CurrentValue = field.Options[prevIdx]
			c.applyFieldChange(*field)
			return
		}
	}
}

// toggleField toggles a boolean field
func (c *ConfigView) toggleField(idx int) {
	field := &c.fields[idx]
	if field.Type != FieldTypeToggle {
		return
	}

	if field.CurrentValue == "true" {
		field.CurrentValue = "false"
	} else {
		field.CurrentValue = "true"
	}

	c.applyFieldChange(*field)
}

// applyFieldChange applies a field change to the config
func (c *ConfigView) applyFieldChange(field ConfigField) {
	switch field.Label {
	case "Safety Level":
		c.config.SafetyLevel = field.CurrentValue
	case "Output Format":
		c.config.Output.Format = field.CurrentValue
	case "Preserve Comments":
		c.config.Output.PreserveComments = field.CurrentValue == "true"
	case "Add Consolidation Comments":
		c.config.Output.AddConsolidationComments = field.CurrentValue == "true"
	case "Consolidate CREATE + ALTER":
		c.config.Rules.TableOperations.ConsolidateCreateAlter = field.CurrentValue == "true"
	case "Remove DROP-CREATE Cycles":
		c.config.Rules.TableOperations.RemoveDropCreateCycles = field.CurrentValue == "true"
	case "Parallel Processing":
		c.config.Performance.ParallelProcessing = field.CurrentValue == "true"
	case "Show Progress":
		c.config.Performance.ShowProgress = field.CurrentValue == "true"
	case "Supabase Integration":
		c.config.ThirdPartyIntegrations.SupabaseIntegration.Enabled = field.CurrentValue == "true"
	case "Clerk Integration":
		c.config.ThirdPartyIntegrations.ClerkIntegration.Enabled = field.CurrentValue == "true"
	}
}

// loadConfig loads the configuration
func (c *ConfigView) loadConfig() tea.Msg {
	cfg, err := config.LoadConfig(c.configPath)
	if err != nil || cfg == nil {
		// Use default config
		cfg = &config.Config{
			SafetyLevel: "standard",
			Output: config.OutputConfig{
				Format:                   "organized",
				Directory:                "squashed",
				PreserveComments:         true,
				AddConsolidationComments: true,
			},
			Rules: config.RulesConfig{
				TableOperations: config.TableRulesConfig{
					ConsolidateCreateAlter: true,
					RemoveDropCreateCycles: true,
					PreserveDataOperations: true,
				},
				FunctionOperations: config.FunctionRulesConfig{
					RemoveDuplicateDefinitions: true,
				},
			},
			Performance: config.PerformanceConfig{
				ParallelProcessing: true,
				ShowProgress:       true,
			},
			ThirdPartyIntegrations: config.ThirdPartyConfig{
				SupabaseIntegration: config.SupabaseConfig{
					Enabled: false,
				},
				ClerkIntegration: config.ClerkConfig{
					Enabled: false,
				},
			},
		}
	}
	return *cfg
}

// saveConfig saves the configuration
func (c *ConfigView) saveConfig() tea.Msg {
	// Determine save path
	savePath := c.configPath
	if savePath == "" {
		savePath = "pgsquash.config.json"
	}

	// Marshal config
	data, err := json.MarshalIndent(c.config, "", "  ")
	if err != nil {
		return viewtypes.ErrorMsg{Err: fmt.Errorf("failed to marshal config: %w", err)}
	}

	// Write file
	if err := os.WriteFile(savePath, data, 0644); err != nil {
		return viewtypes.ErrorMsg{Err: fmt.Errorf("failed to write config: %w", err)}
	}

	return viewtypes.ConfigSavedMsg{Path: savePath}
}

// resetToDefaults resets configuration to defaults
func (c *ConfigView) resetToDefaults() tea.Msg {
	c.config = nil
	c.dirty = true
	return c.loadConfig()
}
