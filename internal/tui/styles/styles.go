package styles

import (
	"github.com/charmbracelet/lipgloss"
)

// Color palette
var (
	Primary    = lipgloss.Color("#7C3AED") // Purple
	Secondary  = lipgloss.Color("#06B6D4") // Cyan
	Success    = lipgloss.Color("#10B981") // Green
	Warning    = lipgloss.Color("#F59E0B") // Orange
	Danger     = lipgloss.Color("#EF4444") // Red
	Muted      = lipgloss.Color("#6B7280") // Gray
	Text       = lipgloss.Color("#F3F4F6") // Light gray
	Background = lipgloss.Color("#1F2937") // Dark gray
	Border     = lipgloss.Color("#374151") // Medium gray
)

// Base styles
var (
	// Title style for main headings
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Primary).
			MarginBottom(1).
			PaddingLeft(2)

	// Subtitle style for section headings
	SubtitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Secondary).
			MarginTop(1).
			PaddingLeft(2)

	// Regular text style
	TextStyle = lipgloss.NewStyle().
			Foreground(Text)

	// Muted text style for secondary information
	MutedStyle = lipgloss.NewStyle().
			Foreground(Muted).
			Italic(true)

	// Success message style
	SuccessStyle = lipgloss.NewStyle().
			Foreground(Success).
			Bold(true)

	// Warning message style
	WarningStyle = lipgloss.NewStyle().
			Foreground(Warning).
			Bold(true)

	// Error message style
	ErrorStyle = lipgloss.NewStyle().
			Foreground(Danger).
			Bold(true)

	// Box style for containers
	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Border).
			Padding(1, 2)

	// Active box style (highlighted)
	ActiveBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Primary).
			Padding(1, 2)

	// List item style
	ListItemStyle = lipgloss.NewStyle().
			PaddingLeft(2)

	// Selected list item style
	SelectedItemStyle = lipgloss.NewStyle().
				Foreground(Primary).
				Bold(true).
				PaddingLeft(1).
				SetString("► ")

	// Unselected list item style
	UnselectedItemStyle = lipgloss.NewStyle().
				Foreground(Muted).
				PaddingLeft(2).
				SetString("  ")

	// Status bar style
	StatusBarStyle = lipgloss.NewStyle().
			Foreground(Text).
			Background(Background).
			Padding(0, 1)

	// Help style
	HelpStyle = lipgloss.NewStyle().
			Foreground(Muted).
			Padding(1, 2)

	// Key binding style
	KeyStyle = lipgloss.NewStyle().
			Foreground(Primary).
			Bold(true)

	// Description style for key bindings
	DescStyle = lipgloss.NewStyle().
			Foreground(Muted)

	// Progress bar filled style
	ProgressFilledStyle = lipgloss.NewStyle().
				Foreground(Success).
				Background(Success)

	// Progress bar empty style
	ProgressEmptyStyle = lipgloss.NewStyle().
				Foreground(Border).
				Background(Border)

	// Table header style
	TableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(Secondary).
				BorderStyle(lipgloss.NormalBorder()).
				BorderBottom(true).
				BorderForeground(Border)

	// Table cell style
	TableCellStyle = lipgloss.NewStyle().
			Padding(0, 1)

	// Highlighted table row
	HighlightedRowStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#2D3748")).
				Foreground(Primary)
)

// Stat creates a styled statistic display
func Stat(label, value string, color lipgloss.Color) string {
	labelStyle := lipgloss.NewStyle().
		Foreground(Muted).
		Width(20)

	valueStyle := lipgloss.NewStyle().
		Foreground(color).
		Bold(true)

	return labelStyle.Render(label+":") + " " + valueStyle.Render(value)
}

// StatSuccess creates a success-colored statistic
func StatSuccess(label, value string) string {
	return Stat(label, value, Success)
}

// StatWarning creates a warning-colored statistic
func StatWarning(label, value string) string {
	return Stat(label, value, Warning)
}

// StatInfo creates an info-colored statistic
func StatInfo(label, value string) string {
	return Stat(label, value, Secondary)
}

// StatDanger creates a danger-colored statistic
func StatDanger(label, value string) string {
	return Stat(label, value, Danger)
}

// Badge creates a styled badge
func Badge(text string, color lipgloss.Color) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(color).
		Padding(0, 1).
		Bold(true).
		Render(text)
}

// SuccessBadge creates a success badge
func SuccessBadge(text string) string {
	return Badge(text, Success)
}

// WarningBadge creates a warning badge
func WarningBadge(text string) string {
	return Badge(text, Warning)
}

// DangerBadge creates a danger badge
func DangerBadge(text string) string {
	return Badge(text, Danger)
}

// InfoBadge creates an info badge
func InfoBadge(text string) string {
	return Badge(text, Secondary)
}

// PrimaryBadge creates a primary badge
func PrimaryBadge(text string) string {
	return Badge(text, Primary)
}
