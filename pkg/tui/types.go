package tui

import (
	"github.com/capy-base/pgsquash-engine/pkg/tui/viewtypes"
)

// Re-export types for convenience
type ViewType = viewtypes.ViewType

const (
	ViewDashboard       = viewtypes.ViewDashboard
	ViewAnalysis        = viewtypes.ViewAnalysis
	ViewConfig          = viewtypes.ViewConfig
	ViewDependencyGraph = viewtypes.ViewDependencyGraph
	ViewProgress        = viewtypes.ViewProgress
	ViewHelp            = viewtypes.ViewHelp
	ViewValidation      = viewtypes.ViewValidation
)

// Re-export View for convenience
type View = viewtypes.View

// Re-export message types for convenience
type (
	NavigateMsg         = viewtypes.NavigateMsg
	ErrorMsg            = viewtypes.ErrorMsg
	SuccessMsg          = viewtypes.SuccessMsg
	LoadingMsg          = viewtypes.LoadingMsg
	LoadedMsg           = viewtypes.LoadedMsg
	AnalysisCompleteMsg = viewtypes.AnalysisCompleteMsg
	SquashCompleteMsg   = viewtypes.SquashCompleteMsg
	ConfigSavedMsg      = viewtypes.ConfigSavedMsg
	AnalysisStats       = viewtypes.AnalysisStats
	LifecyclePattern    = viewtypes.LifecyclePattern
	DependencyInfo      = viewtypes.DependencyInfo
	SquashStats         = viewtypes.SquashStats
)
