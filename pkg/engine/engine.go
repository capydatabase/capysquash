// Package engine - Engine wrapper with advanced features
package engine

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	internal_squasher "github.com/capy-base/pgsquash-engine/internal/squasher"
	internal_transformation "github.com/capy-base/pgsquash-engine/internal/transformation"
	internal_utils "github.com/capy-base/pgsquash-engine/internal/utils"
)

// Engine wraps the internal squashing engine and provides access to
// advanced features like statistics, progress tracking, and resource management.
//
// Use NewEngine to create an engine instance, then call Squash methods.
// Always call Close() when done to free resources.
//
// Example:
//
//	config := engine.DefaultConfig()
//	config.ProgressCallback = func(processed, total int64, phase string) {
//	    fmt.Printf("Progress: %d/%d - %s\n", processed, total, phase)
//	}
//
//	eng, err := engine.NewEngine(config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer eng.Close()
//
//	result, err := eng.SquashDirectory("./migrations")
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	stats := eng.GetStats()
//	fmt.Printf("Processed %d objects in %s\n", stats.ObjectsTracked, stats.ProcessingTime)
type Engine struct {
	internal *internal_squasher.Engine
	config   *Config
	result   *SquashResult
}

// NewEngine creates a new engine instance with the provided configuration.
//
// The engine must be closed when done using Close() to free resources.
//
// Example:
//
//	eng, err := engine.NewEngine(config)
//	if err != nil {
//	    return err
//	}
//	defer eng.Close()
func NewEngine(config *Config) (*Engine, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Library callers must get the same plugin surface (Supabase, Clerk,
	// Prisma, Drizzle rules) as the CLI binary. Registration is idempotent,
	// and a failure to register is an error, never a silent degradation.
	if err := ensureDefaultPlugins(); err != nil {
		return nil, err
	}

	// Setup logger
	logLevel := internal_utils.LogLevelInfo
	if config.Verbose {
		logLevel = internal_utils.LogLevelDebug
	}
	logger := internal_utils.NewLogger(logLevel, nil)
	internal_utils.SetDefaultLogger(logger)

	// Convert to internal config (rejects invalid safety levels and formats)
	internalCfg, err := convertConfig(config)
	if err != nil {
		return nil, err
	}

	// DryRun suppresses every write the engine can perform (pg_dump backups
	// and rollback plan files). The engine itself never writes squashed output
	// files - callers receive SQL strings - so disabling these two features
	// makes a dry run write nothing end to end.
	enableBackup := config.EnableBackup
	enableRollback := config.EnableRollback
	if config.DryRun {
		enableBackup = false
		enableRollback = false
	}

	// Build internal engine config
	engineConfig := internal_squasher.EngineConfig{
		Config:              internalCfg,
		Context:             config.Context,
		Version:             config.Version,
		EnableStreaming:     config.EnableStreaming,
		BatchSize:           config.BatchSize,
		WorkerCount:         config.WorkerCount,
		MemoryLimitMB:       config.MemoryLimitMB,
		EnableProgressTrack: config.ProgressCallback != nil,
		ProgressCallback:    config.ProgressCallback,

		// Transformation options
		EnableBackup:        enableBackup,
		EnableRollback:      enableRollback,
		RollbackPath:        config.RollbackPath,
		BackupPath:          config.BackupPath,
		BackupRetentionDays: config.BackupRetentionDays,

		// Cycle detection
		EnableCycleDetection: config.EnableCycleDetection,
		ShowCycleDetails:     config.ShowCycleDetails,
		CycleDetectionDepth:  config.CycleDetectionDepth,
	}

	// Set up transformation config if enabled
	if enableBackup {
		engineConfig.BackupConfig = &internal_transformation.BackupConfig{
			Type:          internal_transformation.SchemaAndData,
			Format:        internal_transformation.SQLFormat,
			Compression:   true,
			VerboseOutput: config.Verbose,
			InsertFormat:  true,
			ColumnInserts: true,
			IfExists:      true,
			Encoding:      "UTF8",
		}
	}

	// Create internal engine
	internalEngine, err := internal_squasher.NewEngine(engineConfig)
	if err != nil {
		return nil, err
	}

	return &Engine{
		internal: internalEngine,
		config:   config,
	}, nil
}

// SquashDirectory consolidates all migration files in a directory.
//
// Example:
//
//	eng, err := engine.NewEngine(config)
//	defer eng.Close()
//
//	result, err := eng.SquashDirectory("./migrations")
func (e *Engine) SquashDirectory(directory string) (*SquashResult, error) {
	ctx := e.config.Context
	if ctx == nil {
		ctx = context.Background()
	}
	// Load migrations
	migrations, err := loadMigrationsFromDirContext(ctx, directory)
	if err != nil {
		return nil, err
	}

	return e.SquashFiles(migrations)
}

// SquashFiles consolidates specific migration files.
//
// The migrations map can contain either file paths OR SQL content.
// If a value looks like a file path (.sql extension), it will be read.
// Otherwise, it's treated as SQL content directly.
//
// Example with file paths:
//
//	migrations := map[int]string{
//	    1: "001_create_users.sql",
//	    2: "002_create_posts.sql",
//	}
//	result, err := eng.SquashFiles(migrations)
//
// Example with SQL content:
//
//	migrations := map[int]string{
//	    1: "CREATE TABLE users (id INT);",
//	    2: "CREATE TABLE posts (id INT);",
//	}
//	result, err := eng.SquashFiles(migrations)
func (e *Engine) SquashFiles(migrations map[int]string) (*SquashResult, error) {
	startTime := time.Now()

	// Convert file paths to SQL content if needed
	contentMap := make(map[int]string)
	for id, value := range migrations {
		// Check if this looks like a file path
		if strings.HasSuffix(value, ".sql") && !strings.Contains(value, "\n") && !strings.Contains(value, ";") {
			// Likely a file path; an unreadable path is an error, never
			// silently treated as SQL content.
			content, err := os.ReadFile(value)
			if err != nil {
				return nil, fmt.Errorf("failed to read migration file %s: %w", value, err)
			}
			contentMap[id] = string(content)
		} else {
			// Already SQL content
			contentMap[id] = value
		}
	}

	// Use separate files mode if requested
	if e.config.SeparateDataOps {
		internalResult, err := e.internal.SquashWithSeparateFiles(contentMap)
		if err != nil {
			return nil, err
		}

		// Convert to public result
		result := convertSquashResult(internalResult, len(migrations), e.internal.GetStats())
		result.ProcessingTime = formatDuration(time.Since(startTime))
		e.result = result
		return result, nil
	}

	// Standard single-file mode
	internalResult, err := e.internal.Squash(contentMap)
	if err != nil {
		return nil, err
	}

	stats := e.internal.GetStats()
	result := &SquashResult{
		BaselineSQL:          internalResult.BaselineSQL,
		DataOperationsSQL:    "",
		Warnings:             internalResult.Warnings,
		FilesProcessed:       len(migrations),
		ObjectsConsolidated:  int(stats.ConsolidationsApplied),
		ProcessingTime:       formatDuration(time.Since(startTime)),
		Extensions:           internalResult.Extensions,
		AuthCompatibilitySQL: internalResult.AuthCompatibilitySQL,
	}

	e.result = result
	return result, nil
}

// GetStats returns current squashing statistics.
//
// This can be called during or after squashing operations.
//
// Example:
//
//	stats := eng.GetStats()
//	fmt.Printf("Phase: %s\n", stats.Phase)
//	fmt.Printf("Progress: %d/%d\n", stats.MigrationsProcessed, stats.TotalMigrations)
func (e *Engine) GetStats() SquashStats {
	internal := e.internal.GetStats()

	return SquashStats{
		Phase:                 internal.Phase,
		MigrationsProcessed:   internal.MigrationsProcessed,
		TotalMigrations:       internal.TotalMigrations,
		ObjectsTracked:        internal.ObjectsTracked,
		ConsolidationsApplied: internal.ConsolidationsApplied,
		ProcessingTime:        internal.ProcessingTime,
		PeakMemoryUsage:       internal.PeakMemoryUsage,
		ThroughputMPS:         internal.ThroughputMPS,
	}
}

// GetMemoryStats returns memory usage statistics (streaming mode only).
//
// Example:
//
//	if config.EnableStreaming {
//	    memStats := eng.GetMemoryStats()
//	    fmt.Printf("Memory: %dMB / %dMB\n",
//	        memStats.CurrentUsageMB, memStats.LimitMB)
//	}
func (e *Engine) GetMemoryStats() MemoryStats {
	internal := e.internal.GetMemoryStats()

	currentMB := internal.CurrentMemoryBytes / (1024 * 1024)
	maxMB := internal.MaxMemoryBytes / (1024 * 1024)
	usagePercent := 0.0
	if maxMB > 0 {
		usagePercent = float64(currentMB) / float64(maxMB) * 100.0
	}

	return MemoryStats{
		CurrentUsageMB: currentMB,
		PeakUsageMB:    currentMB, // Use current as peak for now
		LimitMB:        maxMB,
		UsagePercent:   usagePercent,
	}
} // GetResult returns the most recent squashing result.
// Returns nil if no squashing operation has been performed yet.
func (e *Engine) GetResult() *SquashResult {
	return e.result
}

// GetSafetyLevel returns the configured safety level.
//
// Example:
//
//	level := eng.GetSafetyLevel()
//	fmt.Printf("Using safety level: %s\n", level)
func (e *Engine) GetSafetyLevel() SafetyLevel {
	return e.config.SafetyLevel
}

// GetExtensions returns the list of PostgreSQL extensions detected in migrations.
//
// Example:
//
//	exts := eng.GetExtensions()
//	if len(exts) > 0 {
//	    fmt.Printf("Required extensions: %v\n", exts)
//	}
func (e *Engine) GetExtensions() []string {
	if e.result != nil && len(e.result.Extensions) > 0 {
		return e.result.Extensions
	}
	return []string{}
}

// GetAuthCompatibilitySQL returns authentication compatibility SQL for Docker validation.
//
// This SQL is used to set up auth_users compatibility when using Supabase-style
// authentication with vanilla PostgreSQL.
//
// Example:
//
//	authSQL := eng.GetAuthCompatibilitySQL()
//	if authSQL != "" {
//	    fmt.Println("Auth compatibility required")
//	}
func (e *Engine) GetAuthCompatibilitySQL() string {
	if e.internal != nil {
		return e.internal.GetAuthCompatibilitySQL()
	}
	return ""
}

// GetWarnings returns accumulated warnings from the squashing operation.
//
// Example:
//
//	warnings := eng.GetWarnings()
//	for _, w := range warnings {
//	    log.Printf("Warning: %s\n", w)
//	}
func (e *Engine) GetWarnings() []string {
	if e.result != nil {
		return e.result.Warnings
	}
	return []string{}
}

// Close releases resources used by the engine.
//
// Always call this when done with the engine, preferably using defer.
// A failure to release resources (e.g. closing the production database
// connection) is returned, never swallowed.
//
// Example:
//
//	eng, err := engine.NewEngine(config)
//	if err != nil {
//	    return err
//	}
//	defer eng.Close()
func (e *Engine) Close() error {
	if e.internal != nil {
		return e.internal.Close()
	}
	return nil
}

// Helper function to convert internal SquashResult to public SquashResult
func convertSquashResult(internal *internal_squasher.SquashResult, filesProcessed int, stats internal_squasher.SquashStats) *SquashResult {
	var provenanceInfo *ProvenanceInfo
	if internal.ProvenanceMap != nil {
		provenanceInfo = &ProvenanceInfo{
			Version:     internal.ProvenanceMap.Version,
			SafetyLevel: internal.ProvenanceMap.SafetyMode,
			InputFiles:  internal.ProvenanceMap.Inputs,
			OutputFiles: internal.ProvenanceMap.Outputs,
			ContentHash: internal.ProvenanceMap.ContentHash,
		}
	}

	return &SquashResult{
		BaselineSQL:          internal.BaselineSQL,
		DataOperationsSQL:    internal.DataOperationsSQL,
		Warnings:             internal.Warnings,
		FilesProcessed:       filesProcessed,
		ObjectsConsolidated:  int(stats.ConsolidationsApplied),
		ProcessingTime:       formatDuration(stats.ProcessingTime),
		Extensions:           internal.Extensions,
		AuthCompatibilitySQL: internal.AuthCompatibilitySQL,
		ProvenanceInfo:       provenanceInfo,
	}
}
