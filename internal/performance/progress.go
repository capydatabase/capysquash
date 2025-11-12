package performance

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ProcessingPhase represents different phases of migration processing
type ProcessingPhase string

const (
	PhaseDiscovery    ProcessingPhase = "DISCOVERY"
	PhaseValidation   ProcessingPhase = "VALIDATION"
	PhaseParsing      ProcessingPhase = "PARSING"
	PhaseAnalysis     ProcessingPhase = "ANALYSIS"
	PhaseDependencies ProcessingPhase = "DEPENDENCIES"
	PhaseSquashing    ProcessingPhase = "SQUASHING"
	PhaseOptimization ProcessingPhase = "OPTIMIZATION"
	PhaseGeneration   ProcessingPhase = "GENERATION"
	PhaseOutput       ProcessingPhase = "OUTPUT"
	PhaseCompletion   ProcessingPhase = "COMPLETION"
)

// ProgressReporter interface for reporting progress
type ProgressReporter interface {
	StartPhase(phase ProcessingPhase, total int)
	UpdateProgress(current int, message string)
	FinishPhase(phase ProcessingPhase, success bool, message string)
	SetOverallProgress(current, total int)
	AddWarning(message string)
	AddError(err error)
	GetSummary() ProgressSummary
}

// ProgressSummary provides a summary of the progress
type ProgressSummary struct {
	StartTime       time.Time                     `json:"start_time"`
	EndTime         time.Time                     `json:"end_time"`
	Duration        time.Duration                 `json:"duration"`
	CurrentPhase    ProcessingPhase               `json:"current_phase"`
	OverallProgress float64                       `json:"overall_progress"`
	PhaseProgress   map[ProcessingPhase]float64   `json:"phase_progress"`
	PhaseDetails    map[ProcessingPhase]PhaseInfo `json:"phase_details"`
	Warnings        []string                      `json:"warnings"`
	Errors          []string                      `json:"errors"`
	IsComplete      bool                          `json:"is_complete"`
	Success         bool                          `json:"success"`
}

// PhaseInfo contains information about a processing phase
type PhaseInfo struct {
	StartTime  time.Time     `json:"start_time"`
	EndTime    time.Time     `json:"end_time"`
	Duration   time.Duration `json:"duration"`
	Total      int           `json:"total"`
	Current    int           `json:"current"`
	Progress   float64       `json:"progress"`
	Message    string        `json:"message"`
	Success    bool          `json:"success"`
	IsActive   bool          `json:"is_active"`
	IsComplete bool          `json:"is_complete"`
}

// DefaultProgressReporter is a concrete implementation of ProgressReporter
type DefaultProgressReporter struct {
	mu             sync.RWMutex
	startTime      time.Time
	endTime        time.Time
	currentPhase   ProcessingPhase
	overallCurrent int
	overallTotal   int
	phaseDetails   map[ProcessingPhase]*PhaseInfo
	warnings       []string
	errors         []string
	isComplete     bool
	success        bool
	updateCallback func(ProgressSummary)
	ctx            context.Context
}

// NewProgressReporter creates a new progress reporter
func NewProgressReporter(ctx context.Context, updateCallback func(ProgressSummary)) *DefaultProgressReporter {
	return &DefaultProgressReporter{
		startTime:      time.Now(),
		phaseDetails:   make(map[ProcessingPhase]*PhaseInfo),
		warnings:       make([]string, 0),
		errors:         make([]string, 0),
		updateCallback: updateCallback,
		ctx:            ctx,
	}
}

// StartPhase starts a new processing phase
func (pr *DefaultProgressReporter) StartPhase(phase ProcessingPhase, total int) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	// End previous phase if active
	if pr.currentPhase != "" {
		if phaseInfo := pr.phaseDetails[pr.currentPhase]; phaseInfo != nil && phaseInfo.IsActive {
			phaseInfo.IsActive = false
			phaseInfo.IsComplete = true
			phaseInfo.EndTime = time.Now()
			phaseInfo.Duration = phaseInfo.EndTime.Sub(phaseInfo.StartTime)
		}
	}

	pr.currentPhase = phase
	pr.phaseDetails[phase] = &PhaseInfo{
		StartTime: time.Now(),
		Total:     total,
		Current:   0,
		Progress:  0.0,
		IsActive:  true,
	}

	pr.notifyUpdate()
}

// UpdateProgress updates the progress of the current phase
func (pr *DefaultProgressReporter) UpdateProgress(current int, message string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if phaseInfo := pr.phaseDetails[pr.currentPhase]; phaseInfo != nil {
		phaseInfo.Current = current
		phaseInfo.Message = message

		if phaseInfo.Total > 0 {
			phaseInfo.Progress = float64(current) / float64(phaseInfo.Total) * 100
		}
	}

	pr.notifyUpdate()
}

// FinishPhase completes the current phase
func (pr *DefaultProgressReporter) FinishPhase(phase ProcessingPhase, success bool, message string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	if phaseInfo := pr.phaseDetails[phase]; phaseInfo != nil {
		phaseInfo.IsActive = false
		phaseInfo.IsComplete = true
		phaseInfo.Success = success
		phaseInfo.Message = message
		phaseInfo.EndTime = time.Now()
		phaseInfo.Duration = phaseInfo.EndTime.Sub(phaseInfo.StartTime)

		if success && phaseInfo.Total > 0 {
			phaseInfo.Current = phaseInfo.Total
			phaseInfo.Progress = 100.0
		}
	}

	pr.notifyUpdate()
}

// SetOverallProgress sets the overall progress
func (pr *DefaultProgressReporter) SetOverallProgress(current, total int) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	pr.overallCurrent = current
	pr.overallTotal = total

	pr.notifyUpdate()
}

// AddWarning adds a warning message
func (pr *DefaultProgressReporter) AddWarning(message string) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	pr.warnings = append(pr.warnings, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), message))
	pr.notifyUpdate()
}

// AddError adds an error
func (pr *DefaultProgressReporter) AddError(err error) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	pr.errors = append(pr.errors, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), err.Error()))
	pr.notifyUpdate()
}

// Complete marks the overall process as complete
func (pr *DefaultProgressReporter) Complete(success bool) {
	pr.mu.Lock()
	defer pr.mu.Unlock()

	pr.isComplete = true
	pr.success = success
	pr.endTime = time.Now()

	// Complete current phase if still active
	if pr.currentPhase != "" {
		if phaseInfo := pr.phaseDetails[pr.currentPhase]; phaseInfo != nil && phaseInfo.IsActive {
			phaseInfo.IsActive = false
			phaseInfo.IsComplete = true
			phaseInfo.Success = success
			phaseInfo.EndTime = time.Now()
			phaseInfo.Duration = phaseInfo.EndTime.Sub(phaseInfo.StartTime)
		}
	}

	pr.notifyUpdate()
}

// GetSummary returns the current progress summary
func (pr *DefaultProgressReporter) GetSummary() ProgressSummary {
	pr.mu.RLock()
	defer pr.mu.RUnlock()

	summary := ProgressSummary{
		StartTime:     pr.startTime,
		EndTime:       pr.endTime,
		CurrentPhase:  pr.currentPhase,
		PhaseProgress: make(map[ProcessingPhase]float64),
		PhaseDetails:  make(map[ProcessingPhase]PhaseInfo),
		Warnings:      make([]string, len(pr.warnings)),
		Errors:        make([]string, len(pr.errors)),
		IsComplete:    pr.isComplete,
		Success:       pr.success,
	}

	// Calculate duration
	if pr.isComplete {
		summary.Duration = pr.endTime.Sub(pr.startTime)
	} else {
		summary.Duration = time.Since(pr.startTime)
	}

	// Calculate overall progress
	if pr.overallTotal > 0 {
		summary.OverallProgress = float64(pr.overallCurrent) / float64(pr.overallTotal) * 100
	}

	// Copy phase details
	for phase, info := range pr.phaseDetails {
		summary.PhaseProgress[phase] = info.Progress
		summary.PhaseDetails[phase] = *info
	}

	// Copy warnings and errors
	copy(summary.Warnings, pr.warnings)
	copy(summary.Errors, pr.errors)

	return summary
}

// notifyUpdate calls the update callback if provided
func (pr *DefaultProgressReporter) notifyUpdate() {
	if pr.updateCallback != nil {
		go pr.updateCallback(pr.GetSummary())
	}
}

// ConsoleProgressReporter reports progress to console output
type ConsoleProgressReporter struct {
	*DefaultProgressReporter
	lastPrintTime time.Time
	printInterval time.Duration
}

// NewConsoleProgressReporter creates a console progress reporter
func NewConsoleProgressReporter(ctx context.Context, printInterval time.Duration) *ConsoleProgressReporter {
	cpr := &ConsoleProgressReporter{
		printInterval: printInterval,
		lastPrintTime: time.Now(),
	}

	cpr.DefaultProgressReporter = NewProgressReporter(ctx, cpr.printProgress)
	return cpr
}

// printProgress prints progress to console
func (cpr *ConsoleProgressReporter) printProgress(summary ProgressSummary) {
	now := time.Now()
	if now.Sub(cpr.lastPrintTime) < cpr.printInterval {
		return // Don't print too frequently
	}
	cpr.lastPrintTime = now

	// Print current phase progress
	if summary.CurrentPhase != "" {
		phaseInfo := summary.PhaseDetails[summary.CurrentPhase]
		fmt.Printf("[%s] %s: %d/%d (%.1f%%) - %s\n",
			now.Format("15:04:05"),
			summary.CurrentPhase,
			phaseInfo.Current,
			phaseInfo.Total,
			phaseInfo.Progress,
			phaseInfo.Message)
	}

	// Print overall progress
	if summary.OverallProgress > 0 {
		fmt.Printf("Overall Progress: %.1f%%\n", summary.OverallProgress)
	}

	// Print recent warnings/errors
	if len(summary.Warnings) > 0 {
		fmt.Printf("Warnings: %d\n", len(summary.Warnings))
	}
	if len(summary.Errors) > 0 {
		fmt.Printf("Errors: %d\n", len(summary.Errors))
	}

	// Print completion status
	if summary.IsComplete {
		status := "SUCCESS"
		if !summary.Success {
			status = "FAILED"
		}
		fmt.Printf("Process completed: %s (Duration: %v)\n", status, summary.Duration)
	}
}

// ProgressAggregator aggregates progress from multiple sources
type ProgressAggregator struct {
	sources       map[string]ProgressReporter
	weights       map[string]float64
	totalWeight   float64
	aggregatedSum ProgressSummary //nolint:unused // Reserved for future progress aggregation
	mu            sync.RWMutex
}

// NewProgressAggregator creates a new progress aggregator
func NewProgressAggregator() *ProgressAggregator {
	return &ProgressAggregator{
		sources: make(map[string]ProgressReporter),
		weights: make(map[string]float64),
	}
}

// AddSource adds a progress source with a weight
func (pa *ProgressAggregator) AddSource(name string, reporter ProgressReporter, weight float64) {
	pa.mu.Lock()
	defer pa.mu.Unlock()

	pa.sources[name] = reporter
	pa.weights[name] = weight
	pa.totalWeight += weight
}

// GetAggregatedProgress returns the weighted average progress
func (pa *ProgressAggregator) GetAggregatedProgress() ProgressSummary {
	pa.mu.RLock()
	defer pa.mu.RUnlock()

	var totalProgress float64
	var totalWarnings, totalErrors int
	var earliestStart, latestEnd time.Time
	allComplete := true
	allSuccess := true

	for name, reporter := range pa.sources {
		summary := reporter.GetSummary()
		weight := pa.weights[name]

		// Weighted progress
		totalProgress += summary.OverallProgress * weight

		// Aggregate warnings and errors
		totalWarnings += len(summary.Warnings)
		totalErrors += len(summary.Errors)

		// Track timing
		if earliestStart.IsZero() || summary.StartTime.Before(earliestStart) {
			earliestStart = summary.StartTime
		}
		if summary.EndTime.After(latestEnd) {
			latestEnd = summary.EndTime
		}

		// Track completion and success
		if !summary.IsComplete {
			allComplete = false
		}
		if !summary.Success {
			allSuccess = false
		}
	}

	// Calculate weighted average
	if pa.totalWeight > 0 {
		totalProgress /= pa.totalWeight
	}

	return ProgressSummary{
		StartTime:       earliestStart,
		EndTime:         latestEnd,
		Duration:        latestEnd.Sub(earliestStart),
		OverallProgress: totalProgress,
		IsComplete:      allComplete,
		Success:         allSuccess,
		Warnings:        make([]string, totalWarnings),
		Errors:          make([]string, totalErrors),
	}
}
