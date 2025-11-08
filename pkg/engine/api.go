// Package engine provides a programmatic API for the pgsquash migration consolidation engine.
//
// This package allows Go applications to use pgsquash as a library for custom migration
// workflows, batch processing, or integration into existing tools.
//
// # Basic Usage
//
//	config := engine.DefaultConfig()
//	config.SafetyLevel = engine.Standard
//
//	result, err := engine.SquashDirectory("./migrations", config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Printf("Squashed %d migrations\n", result.FilesProcessed)
//	fmt.Printf("Output: %s\n", result.SQL)
//
// # Advanced Usage
//
//	// Squash specific files with custom config
//	files := map[int]string{
//	    1: "001_create_users.sql",
//	    2: "002_create_posts.sql",
//	}
//
//	config := &engine.Config{
//	    SafetyLevel:     engine.Conservative,
//	    OutputFormat:    engine.FormatSingle,
//	    EnableStreaming: true,
//	    MemoryLimitMB:   512,
//	}
//
//	result, err := engine.SquashFiles(files, config)
//
// # Analyzing Without Squashing
//
//	analysis, err := engine.AnalyzeDirectory("./migrations", nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Printf("Found %d redundancies\n", len(analysis.Redundancies))
//	fmt.Printf("Total objects: %d\n", analysis.TotalObjects)
package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	internal_config "github.com/capysquash/pgsquash-engine/internal/config"
	internal_parser "github.com/capysquash/pgsquash-engine/internal/parser"
	internal_squasher "github.com/capysquash/pgsquash-engine/internal/squasher"
	internal_tracking "github.com/capysquash/pgsquash-engine/internal/tracking"
	internal_types "github.com/capysquash/pgsquash-engine/internal/types"
	internal_utils "github.com/capysquash/pgsquash-engine/internal/utils"
)

// SafetyLevel determines how aggressively migrations are consolidated.
type SafetyLevel string

const (
	// Conservative applies minimal consolidation, preserving most operations
	Conservative SafetyLevel = "conservative"

	// Standard balances consolidation and safety (recommended)
	Standard SafetyLevel = "standard"

	// Aggressive maximizes consolidation, suitable for development
	Aggressive SafetyLevel = "aggressive"

	// Paranoid preserves everything, minimal changes
	Paranoid SafetyLevel = "paranoid"
)

// OutputFormat determines how squashed SQL is formatted.
type OutputFormat string

const (
	// FormatSingle outputs all migrations in a single file
	FormatSingle OutputFormat = "single"

	// FormatSplit outputs migrations in multiple files by category
	FormatSplit OutputFormat = "split"
)

// Config contains configuration for the squashing engine.
type Config struct {
	// SafetyLevel determines consolidation aggressiveness (default: Standard)
	SafetyLevel SafetyLevel

	// OutputFormat determines file organization (default: FormatSingle)
	OutputFormat OutputFormat

	// SeparateDataOps when true, returns separate DDL and data operations files
	SeparateDataOps bool

	// EnableStreaming enables memory-efficient processing for large datasets (default: false)
	EnableStreaming bool

	// MemoryLimitMB sets memory limit for streaming mode (default: 256)
	MemoryLimitMB int

	// BatchSize controls how many migrations are processed in each batch (streaming mode)
	// Default: 50. Increase for better throughput, decrease to reduce memory usage.
	BatchSize int

	// WorkerCount sets the number of parallel workers (streaming mode)
	// Default: 4. Should match available CPU cores for optimal performance.
	WorkerCount int

	// ProgressCallback is called during processing to report progress
	// The callback receives: (processed, total, phase)
	//
	// Example:
	//   config.ProgressCallback = func(processed, total int64, phase string) {
	//       percent := float64(processed) / float64(total) * 100
	//       fmt.Printf("[%s] %.1f%% complete\n", phase, percent)
	//   }
	ProgressCallback ProgressCallback

	// EnableBackup enables backup generation before squashing (default: false)
	// Creates timestamped backups of original migrations for safety
	EnableBackup bool

	// BackupPath specifies where to store backups (default: "./backups")
	// Only used if EnableBackup is true
	BackupPath string

	// BackupRetentionDays specifies how long to keep backups (default: 30)
	// Older backups are automatically cleaned up
	BackupRetentionDays int

	// EnableRollback enables rollback script generation (default: false)
	// Creates scripts to undo the squashing operation
	EnableRollback bool

	// RollbackPath specifies where to store rollback scripts (default: "./rollbacks")
	// Only used if EnableRollback is true
	RollbackPath string

	// EnableCycleDetection enables DDL cycle detection (default: false)
	// Detects circular dependencies in migrations
	EnableCycleDetection bool

	// ShowCycleDetails shows detailed cycle information (default: false)
	// Only used if EnableCycleDetection is true
	ShowCycleDetails bool

	// CycleDetectionDepth sets maximum depth for cycle detection (default: 10)
	// Higher values detect deeper cycles but use more memory
	CycleDetectionDepth int

	// EnableAI enables AI-powered analysis and optimization (default: false)
	// Requires ANTHROPIC_API_KEY, OPENAI_API_KEY, or AZURE_OPENAI_ENDPOINT
	EnableAI bool

	// Verbose enables detailed logging (default: false)
	Verbose bool
}

// SquashResult contains the results of a squashing operation.
type SquashResult struct {
	// SQL contains the consolidated migration SQL (BaselineSQL for backwards compatibility)
	SQL string

	// BaselineSQL contains DDL-only SQL (same as SQL for single-file mode)
	BaselineSQL string

	// DataOperationsSQL contains data operations SQL (INSERT, UPDATE, DELETE)
	DataOperationsSQL string

	// Warnings contains any warnings generated during squashing
	Warnings []string

	// FilesProcessed is the number of migration files processed
	FilesProcessed int

	// ObjectsConsolidated is the number of database objects consolidated
	ObjectsConsolidated int

	// ProcessingTime is the duration of the squashing operation
	ProcessingTime string

	// Extensions contains detected/required PostgreSQL extensions
	Extensions []string

	// ProvenanceInfo contains metadata about the squashing operation
	ProvenanceInfo *ProvenanceInfo

	// DetailedMetrics provides comprehensive analysis metrics for partner integrations
	DetailedMetrics *DetailedMetrics `json:"detailed_metrics,omitempty"`

	// RecommendedActions suggests next steps based on analysis
	RecommendedActions []RecommendedAction `json:"recommended_actions,omitempty"`
}

// ProvenanceInfo contains metadata about the squashing operation
type ProvenanceInfo struct {
	// Version of the squasher used
	Version string

	// SafetyLevel applied during squashing
	SafetyLevel string

	// InputFiles lists source migration files
	InputFiles []string

	// OutputFiles lists generated files
	OutputFiles []string

	// ContentHash for integrity verification
	ContentHash string
}

// AnalysisResult contains the results of migration analysis.
type AnalysisResult struct {
	// TotalFiles is the number of migration files analyzed
	TotalFiles int

	// TotalStatements is the total number of SQL statements
	TotalStatements int

	// TotalObjects is the number of database objects
	TotalObjects int

	// Redundancies lists redundant operations that can be consolidated
	Redundancies []Redundancy

	// ObjectsByType maps object types to counts
	ObjectsByType map[string]int

	// Warnings contains validation warnings
	Warnings []string
}

// Redundancy represents a redundant database operation.
type Redundancy struct {
	// Type is the redundancy type (e.g., "duplicate_create", "overridden_alter")
	Type string

	// ObjectName is the affected database object
	ObjectName string

	// Description explains the redundancy
	Description string

	// Severity indicates importance (low, medium, high)
	Severity string
}

// DefaultConfig returns a configuration with sensible defaults.
func DefaultConfig() *Config {
	return &Config{
		SafetyLevel:         Standard,
		OutputFormat:        FormatSingle,
		EnableStreaming:     false,
		MemoryLimitMB:       256,
		BatchSize:           50,
		WorkerCount:         4,
		EnableBackup:        false,
		BackupPath:          "./backups",
		BackupRetentionDays:     30,
		EnableRollback:          false,
		RollbackPath:            "./rollbacks",
		EnableCycleDetection:    false,
		ShowCycleDetails:        false,
		CycleDetectionDepth:     10,
		EnableAI:                false,
		Verbose:                 false,
	}
}

// SquashDirectory consolidates all migration files in a directory.
//
// If config is nil, DefaultConfig() is used.
//
// Example:
//
//	result, err := engine.SquashDirectory("./migrations", nil)
//	if err != nil {
//	    return err
//	}
//	fmt.Println(result.SQL)
func SquashDirectory(directory string, config *Config) (*SquashResult, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Setup logger
	logLevel := internal_utils.LogLevelInfo
	if config.Verbose {
		logLevel = internal_utils.LogLevelDebug
	}
	logger := internal_utils.NewLogger(logLevel, os.Stdout)
	internal_utils.SetDefaultLogger(logger)

	// Convert to internal config
	internalCfg := convertConfig(config)

	// Use optimized directory squashing if streaming enabled
	if config.EnableStreaming {
		sql, warnings, err := internal_squasher.OptimizedSquashFromDirectory(
			internalCfg,
			directory,
			config.MemoryLimitMB,
		)
		if err != nil {
			return nil, err
		}

		// Note: Streaming mode doesn't use engine struct, so stats are limited
		return &SquashResult{
			SQL:                sql,
			Warnings:           warnings,
			FilesProcessed:     countFilesInDir(directory),
			ObjectsConsolidated: 0, // Streaming mode doesn't track this yet
			ProcessingTime:     "N/A", // Streaming mode doesn't track this yet
		}, nil
	}

	// Load migrations from directory
	migrations, err := loadMigrationsFromDir(directory)
	if err != nil {
		return nil, fmt.Errorf("failed to load migrations: %w", err)
	}

	return SquashFiles(migrations, config)
}

// SquashFiles consolidates specific migration files.
//
// The migrations map can contain either:
// - File paths: values ending in .sql will be read from disk
// - SQL content: multi-line SQL strings will be used directly
//
// Example with file paths:
//
//	migrations := map[int]string{
//	    1: "001_create_users.sql",
//	    2: "002_create_posts.sql",
//	}
//	result, err := engine.SquashFiles(migrations, nil)
//
// Example with SQL content:
//
//	migrations := map[int]string{
//	    1: "CREATE TABLE users (id INT);\nCREATE TABLE posts (id INT);",
//	}
//	result, err := engine.SquashFiles(migrations, nil)
func SquashFiles(migrations map[int]string, config *Config) (*SquashResult, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Setup logger
	logLevel := internal_utils.LogLevelInfo
	if config.Verbose {
		logLevel = internal_utils.LogLevelDebug
	}
	logger := internal_utils.NewLogger(logLevel, os.Stdout)
	internal_utils.SetDefaultLogger(logger)

	// Convert to internal config
	internalCfg := convertConfig(config)

	// Convert file paths to SQL content if needed
	contentMap := make(map[int]string)
	for id, value := range migrations {
		// Check if this looks like a file path
		if strings.HasSuffix(value, ".sql") && !strings.Contains(value, "\n") && !strings.Contains(value, ";") {
			// Likely a file path, try to read it
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

	// Create squasher engine
	engineConfig := internal_squasher.EngineConfig{
		Config:              internalCfg,
		EnableStreaming:     config.EnableStreaming,
		MemoryLimitMB:       config.MemoryLimitMB,
		EnableProgressTrack: config.Verbose,
	}

	engine := internal_squasher.NewEngine(engineConfig)

	// Get statistics from engine
	stats := engine.GetStats()

	// Use separate files mode if requested
	if config.SeparateDataOps {
		internalResult, err := engine.SquashWithSeparateFiles(contentMap)
		if err != nil {
			return nil, err
		}

		// Build provenance info
		var provenanceInfo *ProvenanceInfo
		if internalResult.ProvenanceMap != nil {
			provenanceInfo = &ProvenanceInfo{
				Version:      internalResult.ProvenanceMap.Version,
				SafetyLevel:  internalResult.ProvenanceMap.SafetyMode,
				InputFiles:   internalResult.ProvenanceMap.Inputs,
				OutputFiles:  internalResult.ProvenanceMap.Outputs,
				ContentHash:  internalResult.ProvenanceMap.ContentHash,
			}
		}

		return &SquashResult{
			SQL:                 internalResult.BaselineSQL, // For backwards compatibility
			BaselineSQL:         internalResult.BaselineSQL,
			DataOperationsSQL:   internalResult.DataOperationsSQL,
			Warnings:            internalResult.Warnings,
			FilesProcessed:      len(migrations),
			ObjectsConsolidated: int(stats.ConsolidationsApplied),
			ProcessingTime:      formatDuration(stats.ProcessingTime),
			Extensions:          internalResult.Extensions,
			ProvenanceInfo:      provenanceInfo,
		}, nil
	}

	// Standard single-file mode
	sql, warnings, err := engine.Squash(contentMap)
	if err != nil {
		return nil, err
	}

	return &SquashResult{
		SQL:                 sql,
		BaselineSQL:         sql, // Same as SQL in single-file mode
		DataOperationsSQL:   "",
		Warnings:            warnings,
		FilesProcessed:      len(migrations),
		ObjectsConsolidated: int(stats.ConsolidationsApplied),
		ProcessingTime:      formatDuration(stats.ProcessingTime),
		Extensions:          []string{},
		ProvenanceInfo:      nil,
	}, nil
}

// AnalyzeDirectory analyzes migration files without making modifications.
//
// If config is nil, DefaultConfig() is used.
//
// Example:
//
//	analysis, err := engine.AnalyzeDirectory("./migrations", nil)
//	if err != nil {
//	    return err
//	}
//	fmt.Printf("Found %d redundancies\n", len(analysis.Redundancies))
func AnalyzeDirectory(directory string, config *Config) (*AnalysisResult, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Setup logger
	logLevel := internal_utils.LogLevelInfo
	if config.Verbose {
		logLevel = internal_utils.LogLevelDebug
	}
	logger := internal_utils.NewLogger(logLevel, os.Stdout)
	internal_utils.SetDefaultLogger(logger)

	// Load and parse migrations
	migrations, err := loadAndParseMigrations(directory)
	if err != nil {
		return nil, err
	}

	// Create tracker for analysis
	tracker := internal_tracking.NewTracker()
	for i, migration := range migrations {
		tracker.ProcessMigration(migration, i)
	}

	// Get analysis results
	redundancies := tracker.GetRedundantObjects()
	stats := tracker.GetStatistics()
	warnings := tracker.ValidateConsistency()

	// Convert to public types
	// Convert ObjectsByType map to string keys
	objectsByTypeStr := make(map[string]int)
	for objType, count := range stats.ObjectsByType {
		objectsByTypeStr[string(objType)] = count
	}

	result := &AnalysisResult{
		TotalFiles:      len(migrations),
		TotalStatements: stats.TotalStatements,
		TotalObjects:    stats.TotalObjects,
		Redundancies:    convertRedundancies(redundancies),
		ObjectsByType:   objectsByTypeStr,
		Warnings:        warnings,
	}

	return result, nil
}

// Helper functions

func convertConfig(config *Config) *internal_config.Config {
	// Convert safety level
	var safetyLevel internal_squasher.SafetyLevel
	switch config.SafetyLevel {
	case Conservative:
		safetyLevel = internal_squasher.Conservative
	case Standard:
		safetyLevel = internal_squasher.Standard
	case Aggressive:
		safetyLevel = internal_squasher.Aggressive
	case Paranoid:
		safetyLevel = internal_squasher.Paranoid
	default:
		safetyLevel = internal_squasher.Standard
	}

	// Create internal config with defaults
	internalCfg := internal_config.DefaultConfig()
	internalCfg.SafetyLevel = string(safetyLevel)
	internalCfg.Output.Format = string(config.OutputFormat)

	return internalCfg
}

func loadMigrationsFromDir(directory string) (map[int]string, error) {
	files, err := filepath.Glob(filepath.Join(directory, "*.sql"))
	if err != nil {
		return nil, err
	}

	migrations := make(map[int]string)
	for i, file := range files {
		// Read file content (internal engine expects SQL content, not paths)
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}
		migrations[i+1] = string(content)
	}

	return migrations, nil
}

func loadAndParseMigrations(directory string) ([]*internal_types.Migration, error) {
	files, err := filepath.Glob(filepath.Join(directory, "*.sql"))
	if err != nil {
		return nil, err
	}

	var migrations []*internal_types.Migration
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", file, err)
		}

		// Parse migration file
		migration, err := internal_parser.ParseMigration(string(content), file)
		if err != nil {
			return nil, fmt.Errorf("failed to parse %s: %w", file, err)
		}

		migrations = append(migrations, migration)
	}

	return migrations, nil
}

func countFilesInDir(directory string) int {
	files, err := filepath.Glob(filepath.Join(directory, "*.sql"))
	if err != nil {
		return 0
	}
	return len(files)
}

func convertRedundancies(internal []internal_tracking.RedundancyReport) []Redundancy {
	var redundancies []Redundancy
	for _, r := range internal {
		// Determine severity based on pattern
		severity := "medium"
		if r.Pattern == internal_tracking.PatternDropCreateSequence ||
		   r.Pattern == internal_tracking.PatternDuplicateOperations {
			severity = "high"
		}

		redundancies = append(redundancies, Redundancy{
			Type:        string(r.Pattern),
			ObjectName:  r.Object,
			Description: r.Explanation,
			Severity:    severity,
		})
	}
	return redundancies
}

// formatDuration converts time.Duration to human-readable string
func formatDuration(d time.Duration) string {
	if d == 0 {
		return "N/A"
	}

	// Round to milliseconds for display
	ms := d.Milliseconds()

	if ms < 1000 {
		return fmt.Sprintf("%dms", ms)
	}

	if ms < 60000 {
		return fmt.Sprintf("%.2fs", float64(ms)/1000.0)
	}

	minutes := ms / 60000
	seconds := (ms % 60000) / 1000
	return fmt.Sprintf("%dm%ds", minutes, seconds)
}
