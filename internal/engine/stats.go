// Package engine - Statistics and progress tracking types
package engine

import "time"

// SquashStats tracks detailed squashing statistics for monitoring and observability.
//
// This type provides real-time metrics during squashing operations, useful for
// progress tracking, performance monitoring, and debugging.
//
// Example:
//
//	eng, err := engine.NewEngine(config)
//	defer eng.Close()
//
//	// Get stats during or after operation
//	stats := eng.GetStats()
//	fmt.Printf("Phase: %s\n", stats.Phase)
//	fmt.Printf("Progress: %d/%d migrations\n", stats.MigrationsProcessed, stats.TotalMigrations)
//	fmt.Printf("Throughput: %.2f migrations/sec\n", stats.ThroughputMPS)
type SquashStats struct {
	// Phase is the current processing phase
	// Examples: "Initializing", "Parsing", "Analyzing", "Consolidating", "Generating SQL"
	Phase string `json:"current_phase"`

	// MigrationsProcessed is the number of migrations processed so far
	MigrationsProcessed int64 `json:"migrations_processed"`

	// TotalMigrations is the total number of migrations to process
	TotalMigrations int64 `json:"total_migrations"`

	// ObjectsTracked is the total number of database objects being tracked
	ObjectsTracked int64 `json:"objects_tracked"`

	// ConsolidationsApplied is the number of consolidations successfully applied
	ConsolidationsApplied int64 `json:"consolidations_applied"`

	// ProcessingTime is the total time spent processing
	ProcessingTime time.Duration `json:"processing_time"`

	// PeakMemoryUsage is the peak memory usage in bytes (streaming mode only)
	PeakMemoryUsage int64 `json:"peak_memory_usage"`

	// ThroughputMPS is the throughput in migrations per second
	ThroughputMPS float64 `json:"throughput_migrations_per_sec"`
}

// ProgressPhase represents a processing phase with progress information
type ProgressPhase string

const (
	PhaseInitializing  ProgressPhase = "Initializing"
	PhaseParsing       ProgressPhase = "Parsing"
	PhaseTracking      ProgressPhase = "Tracking"
	PhaseAnalyzing     ProgressPhase = "Analyzing"
	PhaseConsolidating ProgressPhase = "Consolidating"
	PhaseGenerating    ProgressPhase = "Generating SQL"
	PhaseValidating    ProgressPhase = "Validating"
	PhaseComplete      ProgressPhase = "Complete"
)

// ProgressCallback is called during squashing to report progress.
//
// Parameters:
//   - processed: Number of items processed so far
//   - total: Total number of items to process
//   - phase: Current processing phase (see ProgressPhase constants)
//
// Example:
//
//	config.ProgressCallback = func(processed, total int64, phase string) {
//	    if total > 0 {
//	        percent := float64(processed) / float64(total) * 100
//	        fmt.Printf("[%s] Progress: %.1f%% (%d/%d)\n", phase, percent, processed, total)
//	    }
//	}
type ProgressCallback func(processed, total int64, phase string)

// MemoryStats provides memory usage statistics for streaming operations
type MemoryStats struct {
	// CurrentUsageMB is the current memory usage in megabytes
	CurrentUsageMB int64 `json:"current_usage_mb"`

	// PeakUsageMB is the peak memory usage in megabytes
	PeakUsageMB int64 `json:"peak_usage_mb"`

	// LimitMB is the configured memory limit in megabytes
	LimitMB int64 `json:"limit_mb"`

	// UsagePercent is the percentage of limit currently used
	UsagePercent float64 `json:"usage_percent"`
}
