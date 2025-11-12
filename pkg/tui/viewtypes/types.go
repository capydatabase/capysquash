package viewtypes

import (
    tea "github.com/charmbracelet/bubbletea"
)

// ViewType represents different views in the TUI
type ViewType int

const (
    ViewDashboard ViewType = iota
    ViewAnalysis
    ViewConfig
    ViewDependencyGraph
    ViewProgress
    ViewHelp
)

// Messages for inter-view communication

// NavigateMsg requests navigation to a different view
type NavigateMsg struct {
    View ViewType
    Data interface{}
}

// ErrorMsg represents an error message
type ErrorMsg struct {
    Err error
}

// SuccessMsg represents a success message
type SuccessMsg struct {
    Message string
}

// LoadingMsg indicates loading state
type LoadingMsg struct {
    Message string
}

// LoadedMsg indicates loading completed
type LoadedMsg struct {
    Data interface{}
}

// AnalysisCompleteMsg sent when analysis completes
type AnalysisCompleteMsg struct {
    Stats *AnalysisStats
}

// SquashCompleteMsg sent when squashing completes
type SquashCompleteMsg struct {
    OutputPath string
    Stats      *SquashStats
}

// ConfigSavedMsg sent when configuration is saved
type ConfigSavedMsg struct {
    Path string
}

// AnalysisStats contains analysis results
type AnalysisStats struct {
    TotalMigrations   int
    ObjectsCreated    int
    TablesCreated     int
    FunctionsCreated  int
    IndexesCreated    int
    PotentialSavings  float64
    FileReduction     int
    LifecyclePatterns []LifecyclePattern
    Dependencies      []DependencyInfo
    Warnings          []string
    Errors            []string
}

// LifecyclePattern represents an object's lifecycle
type LifecyclePattern struct {
    ObjectName  string
    ObjectType  string
    Operations  []string
    Optimizable bool
    Warning     string
}

// DependencyInfo represents a dependency relationship
type DependencyInfo struct {
    Source string
    Target string
    Type   string
}

// SquashStats contains squashing operation statistics
type SquashStats struct {
    OriginalFiles    int
    SquashedFiles    int
    BytesSaved       int64
    ObjectsOptimized int
    Duration         float64
}

// View interface that all views must implement
type View interface {
    // Init initializes the view
    Init() tea.Cmd

    // Update handles messages and updates view state
    Update(msg tea.Msg) (View, tea.Cmd)

    // View renders the view
    View() string

    // Type returns the view type
    Type() ViewType

    // SetSize sets the view dimensions
    SetSize(width, height int)

    // OnEnter is called when the view becomes active
    OnEnter() tea.Cmd

    // OnExit is called when leaving the view
    OnExit() tea.Cmd
}

// BaseView provides common functionality for all views
type BaseView struct {
    Width  int
    Height int
}

func (b *BaseView) SetSize(width, height int) {
    b.Width = width
    b.Height = height
}

func (b *BaseView) OnEnter() tea.Cmd {
    return nil
}

func (b *BaseView) OnExit() tea.Cmd {
    return nil
}
