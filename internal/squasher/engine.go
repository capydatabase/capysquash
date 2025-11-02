package squasher

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/ai"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/builder"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/config"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/errors"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/metadata"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/parser"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/performance"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/plugins/auth"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/postprocessing"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/tracking"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/tracking/consolidation"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/transformation"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/types"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/utils"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// SquashResult represents the result of a squash operation with multiple output files
type SquashResult struct {
	BaselineSQL       string     // DDL-only SQL (000_baseline.sql)
	DataOperationsSQL string     // Data operations SQL (010_data.sql)
	Warnings          []string   // Warnings generated during squash
	ProvenanceMap     *SquashMap // Provenance tracking information
	Extensions        []string   // Extensions detected/required

	// Enhanced metrics for partner integrations
	DetailedMetrics    *DetailedMetrics    `json:"detailed_metrics,omitempty"`
	RecommendedActions []RecommendedAction `json:"recommended_actions,omitempty"`
}

// DetailedMetrics provides comprehensive analysis metrics
type DetailedMetrics struct {
	TotalMigrations     int     `json:"total_migrations"`
	OptimizedMigrations int     `json:"optimized_migrations"`
	ReductionPercentage float64 `json:"reduction_percentage"`

	Operations OperationBreakdown `json:"operations"`

	EstimatedTimeSavingsSeconds int     `json:"estimated_time_savings_seconds"`
	FileSizeReductionBytes      int64   `json:"file_size_reduction_bytes"`
	FileSizeReductionPercent    float64 `json:"file_size_reduction_percent"`

	RedundanciesFound []RedundancyDetail `json:"redundancies_found"`
}

// OperationBreakdown categorizes SQL operations
type OperationBreakdown struct {
	Creates      int `json:"creates"`
	Alters       int `json:"alters"`
	Drops        int `json:"drops"`
	Inserts      int `json:"inserts"`
	Updates      int `json:"updates"`
	Deletes      int `json:"deletes"`
	Consolidated int `json:"consolidated"`
}

// RedundancyDetail describes a specific redundancy found
type RedundancyDetail struct {
	Type        string `json:"type"`         // "drop_create_cycle", "duplicate_alter", etc.
	ObjectName  string `json:"object_name"`  // "users", "posts", etc.
	ObjectType  string `json:"object_type"`  // "table", "index", "function"
	Severity    string `json:"severity"`     // "low", "medium", "high", "critical"
	Description string `json:"description"`
	FileNumbers []int  `json:"file_numbers"` // Which migration files involved
	Savings     string `json:"savings"`      // "2 operations consolidated"
}

// RecommendedAction suggests next steps
type RecommendedAction struct {
	Action      string `json:"action"`       // "auto_cleanup", "manual_review", "guarded_apply"
	Reason      string `json:"reason"`
	Priority    string `json:"priority"`     // "high", "medium", "low"
	AutomateURL string `json:"automate_url"` // Deep link to platform action
	RiskLevel   string `json:"risk_level"`   // "safe", "moderate", "high"
}

type SafetyLevel string

const (
	Conservative SafetyLevel = "conservative"
	Standard     SafetyLevel = "standard"
	Aggressive   SafetyLevel = "aggressive"
	Paranoid     SafetyLevel = "paranoid"
)

// Engine provides comprehensive PostgreSQL migration squashing with modern patterns and streaming support
type Engine struct {
	// Core configuration and state
	config     *config.Config
	lifecycles map[string]*tracking.ObjectLifecycle
	warnings   []string
	aiAnalyzer *ai.Analyzer
	prodDB     *sql.DB

	// Enhanced components
	metadataManager      *metadata.MetadataManager
	tracker              *tracking.Tracker
	dataOperationTracker *tracking.DataOperationTracker // Separate tracker for data operations (INSERT/UPDATE/DELETE)
	sqlBuilder           *builder.SQLBuilder
	ruleEngine           *consolidation.ConsolidationRuleEngine
	logger               *utils.Logger // Centralized logger with ENGINE prefix

	// Transformation components
	backupGenerator *transformation.BackupGenerator
	rollbackManager *transformation.RollbackManager
	sqlTransformer  *transformation.SQLTransformer
	rollbackPath    string

	// Processing state
	processedFiles       map[string]bool
	consolidationResults map[string]*tracking.ConsolidationResult

	// Streaming components (optional)
	streamingTracker *tracking.StreamingTracker
	memManager       *performance.MemoryManager
	batchProcessor   *performance.BatchProcessor

	// Streaming configuration
	batchSize           int
	workerCount         int
	memoryLimitMB       int
	enableProgressTrack bool
	enableStreaming     bool

	enableCycleDetection bool
	showCycleDetails     bool
	cycleDetectionDepth  int

	// Extension analysis and auth compatibility
	authCompatibilitySQL string

	// Processing statistics
	stats      *SquashStats
	mu         sync.RWMutex
	progressCb func(processed, total int64, phase string)
}

// SquashStats tracks squashing statistics (both regular and streaming)
type SquashStats struct {
	Phase                 string        `json:"current_phase"`
	MigrationsProcessed   int64         `json:"migrations_processed"`
	TotalMigrations       int64         `json:"total_migrations"`
	ObjectsTracked        int64         `json:"objects_tracked"`
	ConsolidationsApplied int64         `json:"consolidations_applied"`
	ProcessingTime        time.Duration `json:"processing_time"`
	PeakMemoryUsage       int64         `json:"peak_memory_usage"`
	ThroughputMPS         float64       `json:"throughput_migrations_per_sec"`
}

// EngineConfig configures the engine with optional streaming capabilities
type EngineConfig struct {
	Config              *config.Config
	EnableStreaming     bool
	BatchSize           int
	WorkerCount         int
	MemoryLimitMB       int
	EnableProgressTrack bool
	ProgressCallback    func(processed, total int64, phase string)

	// Transformation options
	EnableBackup         bool
	EnableRollback       bool
	EnableTransformation bool
	BackupConfig         *transformation.BackupConfig
	TransformationConfig *transformation.TransformationConfig
	RollbackPath         string // Directory for rollback scripts

	EnableCycleDetection bool
	ShowCycleDetails     bool
	CycleDetectionDepth  int
}

// NewEngine creates an enhanced engine with the provided configuration
func NewEngine(cfg EngineConfig) *Engine {
	return newEngineInternal(cfg)
}

// newEngineInternal is the internal implementation
func newEngineInternal(engineCfg EngineConfig) *Engine {
	// Initialize logger for this engine instance
	logger := utils.GetDefaultLogger().WithPrefix("ENGINE")

	// Extract configuration
	cfg := engineCfg.Config
	enableStreaming := engineCfg.EnableStreaming
	batchSize := engineCfg.BatchSize
	workerCount := engineCfg.WorkerCount
	memoryLimitMB := engineCfg.MemoryLimitMB
	enableProgressTrack := engineCfg.EnableProgressTrack
	progressCallback := engineCfg.ProgressCallback

	// Set defaults for streaming config
	if batchSize == 0 {
		batchSize = 50
	}
	if workerCount == 0 {
		workerCount = 4
	}
	if memoryLimitMB == 0 {
		memoryLimitMB = 256
	}

	// Database connection for paranoid mode or backup feature
	var db *sql.DB
	var err error
	needsDB := cfg.SafetyLevel == string(Paranoid) || engineCfg.EnableBackup

	if needsDB {
		if cfg.ProdDBDSN == "" {
			if cfg.SafetyLevel == string(Paranoid) {
				// TODO: Implement production DSN support for paranoid mode live dead code analysis
				// For E2E testing, paranoid mode uses mock data without production DSN
				// Full implementation requires: database connection pooling, query analysis, and runtime statistics
				logger.Warn("Paranoid safety level selected, but no production database DSN provided. Dead code analysis will be skipped.")
			}
			if engineCfg.EnableBackup {
				logger.Warn("Backup generation requested, but no production database DSN provided (prod_db_dsn in config). Backup will be skipped.")
			}
		} else {
			db, err = sql.Open("postgres", cfg.ProdDBDSN)
			if err != nil {
				logger.Fatal("Failed to connect to production database: %v", err)
			}
			if err := db.Ping(); err != nil {
				logger.Fatal("Failed to ping production database: %v", err)
			}
			if cfg.SafetyLevel == string(Paranoid) {
				logger.Info("Successfully connected to production database for dead code analysis.")
			}
			if engineCfg.EnableBackup {
				logger.Info("Successfully connected to production database for backup generation.")
			}
		}
	}

	// Initialize enhanced components
	var metaMgr *metadata.MetadataManager
	if db != nil {
		metaMgr = metadata.NewMetadataManager(db, 15*time.Minute)
	}

	tracker := tracking.NewTrackerWithMetadata(metaMgr)
	dataOperationTracker := tracking.NewDataOperationTracker() // Separate tracker for data operations
	sqlBuilder := builder.NewSQLBuilder(builder.DefaultBuildOptions())
	ruleEngine := NewSquasherRuleEngine(SafetyLevel(cfg.SafetyLevel))

	// Initialize streaming components if enabled
	var memManager *performance.MemoryManager
	var streamingTracker *tracking.StreamingTracker
	var batchProcessor *performance.BatchProcessor

	if enableStreaming {
		memManager = performance.NewMemoryManager(memoryLimitMB)
		streamingTracker = tracking.NewStreamingTracker(batchSize, workerCount, memManager)
		batchProcessor = performance.NewBatchProcessor(batchSize, memoryLimitMB/4, memManager)
	}

	// Initialize transformation components if enabled
	var backupGenerator *transformation.BackupGenerator
	var rollbackManager *transformation.RollbackManager
	var sqlTransformer *transformation.SQLTransformer
	var rollbackPath string

	if engineCfg.EnableBackup {
		backupConfig := engineCfg.BackupConfig
		if backupConfig == nil {
			backupConfig = transformation.DefaultBackupConfig()
		}
		backupGenerator = transformation.NewBackupGenerator(backupConfig, db)
	}

	if engineCfg.EnableRollback {
		rollbackPath = engineCfg.RollbackPath
		if rollbackPath == "" {
			rollbackPath = "rollbacks" // Default rollback directory
		}
		rollbackManager = transformation.NewRollbackManager(db, rollbackPath)
	}

	if engineCfg.EnableTransformation {
		transformConfig := engineCfg.TransformationConfig
		if transformConfig == nil {
			transformConfig = transformation.DefaultTransformationConfig()
		}
		sqlTransformer = transformation.NewSQLTransformer(transformConfig)
	}

	// Initialize AI analyzer with proper error handling
	var aiAnalyzer *ai.Analyzer
	if analyzer, err := ai.NewAnalyzer(); err != nil {
		logger.Warn("AI analyzer initialization failed: %v", err)
		logger.Warn("AI-powered features will be unavailable (semantic analysis, dead code detection, etc.)")
		logger.Warn("To enable AI features, set ANTHROPIC_API_KEY, OPENAI_API_KEY, or AZURE_OPENAI_ENDPOINT")
		aiAnalyzer = nil // Explicitly set to nil instead of returning error
	} else {
		aiAnalyzer = analyzer
		providers := analyzer.GetAvailableProviders()
		logger.Info("☑ AI analyzer initialized with providers: %v", providers)
	}

	return &Engine{
		// Core components
		config:     cfg,
		lifecycles: make(map[string]*tracking.ObjectLifecycle),
		warnings:   []string{},
		aiAnalyzer: aiAnalyzer,
		prodDB:     db,

		// Enhanced components
		metadataManager:      metaMgr,
		tracker:              tracker,
		dataOperationTracker: dataOperationTracker,
		sqlBuilder:           sqlBuilder,
		ruleEngine:           ruleEngine,
		logger:               logger,

		// Transformation components
		backupGenerator: backupGenerator,
		rollbackManager: rollbackManager,
		sqlTransformer:  sqlTransformer,
		rollbackPath:    rollbackPath,

		// Processing state
		processedFiles:       make(map[string]bool),
		consolidationResults: make(map[string]*tracking.ConsolidationResult),

		// Streaming components
		streamingTracker:    streamingTracker,
		memManager:          memManager,
		batchProcessor:      batchProcessor,
		batchSize:           batchSize,
		workerCount:         workerCount,
		memoryLimitMB:       memoryLimitMB,
		enableProgressTrack: enableProgressTrack,
		enableStreaming:     enableStreaming,
		progressCb:          progressCallback,
		stats:               &SquashStats{},

		enableCycleDetection: engineCfg.EnableCycleDetection,
		showCycleDetails:     engineCfg.ShowCycleDetails,
		cycleDetectionDepth:  engineCfg.CycleDetectionDepth,
	}
}

// NewSquasherRuleEngine creates a rule engine for the given safety level
func NewSquasherRuleEngine(safetyLevel SafetyLevel) *consolidation.ConsolidationRuleEngine {
	engine := consolidation.NewConsolidationRuleEngine()

	// Always add external dependency filter (reduces noise)
	engine.AddRule(consolidation.NewExternalDependencyFilterRule())

	// Add safety-appropriate rules based on level
	switch safetyLevel {
	case Conservative:
		engine.AddRule(&consolidation.DOBlockEnumTypeRule{})
		engine.AddRule(&consolidation.DOBlockAlterTableRule{})
		engine.AddRule(&consolidation.EnumDeduplicationRule{})
		engine.AddRule(&consolidation.PublicationDeduplicationRule{})

		// Standard rules
		engine.AddRule(&consolidation.MultipleCreateConsolidationRule{})
		engine.AddRule(&consolidation.CreateAlterConsolidationRule{})
		engine.AddRule(&consolidation.ColumnEvolutionRule{})
		engine.AddRule(&consolidation.ConditionalSchemaRule{})
		engine.AddRule(&consolidation.AdvancedColumnLifecycleRule{})
	case Standard:
		engine.AddRule(&consolidation.DOBlockEnumTypeRule{})
		engine.AddRule(&consolidation.DOBlockAlterTableRule{})
		engine.AddRule(&consolidation.EnumDeduplicationRule{})
		engine.AddRule(&consolidation.PublicationDeduplicationRule{})

		// Standard rules
		engine.AddRule(&consolidation.MultipleCreateConsolidationRule{})
		engine.AddRule(&consolidation.CreateAlterConsolidationRule{})
		engine.AddRule(&consolidation.ColumnEvolutionRule{})
		engine.AddRule(&consolidation.ConditionalSchemaRule{})
		engine.AddRule(&consolidation.AdvancedColumnLifecycleRule{})
		engine.AddRule(&consolidation.DropCreateCycleRule{}) // Now handles VIEWs
		engine.AddRule(&consolidation.RLSConsolidationRule{})
		engine.AddRule(&consolidation.TransactionBoundaryRule{})
	case Aggressive:
		engine.AddRule(&consolidation.DOBlockEnumTypeRule{})
		engine.AddRule(&consolidation.DOBlockAlterTableRule{})
		engine.AddRule(&consolidation.EnumDeduplicationRule{})
		engine.AddRule(&consolidation.PublicationDeduplicationRule{})

		// Standard rules
		engine.AddRule(&consolidation.MultipleCreateConsolidationRule{})
		engine.AddRule(&consolidation.CreateAlterConsolidationRule{})
		engine.AddRule(&consolidation.ColumnEvolutionRule{})
		engine.AddRule(&consolidation.ConditionalSchemaRule{})
		engine.AddRule(&consolidation.AdvancedColumnLifecycleRule{})
		engine.AddRule(&consolidation.DropCreateCycleRule{}) // Now handles VIEWs
		engine.AddRule(&consolidation.RLSConsolidationRule{})
		engine.AddRule(&consolidation.TransactionBoundaryRule{})
		engine.AddRule(&consolidation.FunctionDeduplicationRule{})
	case Paranoid:
		engine.AddRule(&consolidation.DOBlockEnumTypeRule{})
		engine.AddRule(&consolidation.DOBlockAlterTableRule{})
		engine.AddRule(&consolidation.EnumDeduplicationRule{})
		engine.AddRule(&consolidation.PublicationDeduplicationRule{})

		// Standard rules
		engine.AddRule(&consolidation.MultipleCreateConsolidationRule{})
		engine.AddRule(&consolidation.CreateAlterConsolidationRule{})
		engine.AddRule(&consolidation.ColumnEvolutionRule{})
		engine.AddRule(&consolidation.ConditionalSchemaRule{})
		engine.AddRule(&consolidation.AdvancedColumnLifecycleRule{})
		engine.AddRule(&consolidation.DropCreateCycleRule{}) // Now handles VIEWs
		engine.AddRule(&consolidation.RLSConsolidationRule{})
		engine.AddRule(&consolidation.TransactionBoundaryRule{})
		engine.AddRule(&consolidation.FunctionDeduplicationRule{})
		engine.AddRule(&consolidation.DeadCodeRemovalRule{})
	}

	// Add error recovery as the last rule to catch any failures from primary consolidation rules
	recoveryMode := "conservative"
	if safetyLevel == Aggressive || safetyLevel == Paranoid {
		recoveryMode = "aggressive"
	}
	errorRecovery := consolidation.NewErrorRecoveryRule(3, recoveryMode, true)
	engine.AddRule(errorRecovery)

	return engine
}

// GetTracker returns the tracker for use by consolidation rules
func (e *Engine) GetTracker() *tracking.Tracker {
	return e.tracker
}

// GetSafetyLevel returns the current safety level
func (e *Engine) GetSafetyLevel() string {
	if e.config == nil {
		return "standard"
	}
	return e.config.SafetyLevel
}

// GetConfig returns the configuration for use by consolidation rules
func (e *Engine) GetConfig() interface{} {
	return e.config
}

// GetAIAnalyzer returns the AI analyzer if available, nil otherwise
func (e *Engine) GetAIAnalyzer() interface{} {
	return e.aiAnalyzer
}

// GetAuthCompatibilitySQL returns the auth compatibility SQL for Docker validation
func (e *Engine) GetAuthCompatibilitySQL() string {
	return e.authCompatibilitySQL
}

// Close gracefully shuts down the engine
func (e *Engine) Close() {
	if e.prodDB != nil {
		_ = e.prodDB.Close()
	}
	if e.enableStreaming && e.streamingTracker != nil {
		if err := e.streamingTracker.Stop(); err != nil {
			e.logger.Info("Warning: failed to stop streaming tracker: %v", err)
		}
	}
}

func (e *Engine) generateCycleResolutionSuggestions(cycle tracking.DDLCycle) []string {
	suggestions := []string{}

	switch cycle.Type {
	case tracking.DependencyCycle:
		suggestions = append(suggestions, "Consider reordering migrations to place dependent objects after their dependencies")
		suggestions = append(suggestions, "Use temporary tables or deferred constraints if possible")

	case tracking.ConstraintCycle:
		suggestions = append(suggestions, "Use DEFERRABLE constraints to allow cyclic foreign keys")
		suggestions = append(suggestions, "Example: ADD CONSTRAINT fk_name FOREIGN KEY (...) REFERENCES ... DEFERRABLE INITIALLY DEFERRED")
		suggestions = append(suggestions, "Consider splitting foreign keys into a separate migration applied after all tables exist")

	case tracking.SimpleCycle:
		// Make suggestions context-aware - only suggest what's actually in the cycle
		hasCreate := false
		hasAlter := false
		hasDrop := false
		for _, op := range cycle.Operations {
			switch strings.ToUpper(op.Operation) {
			case "CREATE":
				hasCreate = true
			case "ALTER":
				hasAlter = true
			case "DROP":
				hasDrop = true
			}
		}

		// Only suggest relevant actions based on actual operations
		if hasCreate {
			if hasDrop {
				suggestions = append(suggestions, "Object is created and dropped - consider using the final version only")
			} else {
				suggestions = append(suggestions, "Merge CREATE statements into a single operation")
			}
		}
		if hasAlter {
			suggestions = append(suggestions, "Combine related ALTER statements")
		}

	case tracking.ComplexCycle:
		suggestions = append(suggestions, "Break cycle by removing non-critical dependencies")
		suggestions = append(suggestions, "Consider using views or materialized views to reduce coupling")
		suggestions = append(suggestions, "Split into multiple migrations with explicit ordering")

	case tracking.TransientCycle:
		suggestions = append(suggestions, "This cycle is temporary and can be safely resolved during consolidation")
		suggestions = append(suggestions, "The engine will handle this automatically")

	case tracking.VersioningCycle:
		suggestions = append(suggestions, "Keep all versions if rollback capability is needed")
		suggestions = append(suggestions, "Otherwise, use only the latest version in production")
	}

	// Add severity-specific suggestions
	if cycle.Severity == tracking.SeverityCritical {
		suggestions = append(suggestions, "⚠ CRITICAL: Manual review required before applying optimizations")
		suggestions = append(suggestions, "Consider using --safety conservative mode for this migration set")
	}

	return suggestions
}

// Squash processes migrations using enhanced patterns and modern PostgreSQL conventions
func (e *Engine) Squash(migrations map[int]string) (string, []string, error) {
	// Use streaming approach if enabled
	if e.enableStreaming {
		return e.SquashStreaming(migrations)
	}

	ctx := context.Background()
	startTime := time.Now()

	e.logger.Info("Starting enhanced squashing process with %d migration files", len(migrations))

	// Initialize stats
	e.stats.TotalMigrations = int64(len(migrations))
	e.stats.MigrationsProcessed = 0

	// Invoke progress callback at start
	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(0, e.stats.TotalMigrations, "Initializing")
	}

	// PHASE 0: Initialize Plugin System
	// This must happen BEFORE parsing to enable plugin enrichment
	// NOTE: Plugin initialization has been moved to CLI/API layer
	// if err := e.initializePlugins(ctx, migrations); err != nil {
	// 	e.logger.Info("Warning: Plugin initialization failed: %v", err)
	// 	e.warnings = append(e.warnings, fmt.Sprintf("Plugin initialization warning: %v", err))
	// }

	// Analyze extensions required for validation
	extDetector := NewExtensionDetector()
	extAnalysis := extDetector.AnalyzeMigrations(migrations)
	if len(extAnalysis.RequiredExtensions) > 0 {
		e.logger.Info("Detected extensions: %v", extAnalysis.RequiredExtensions)
		e.logger.Info("Recommended Docker image: %s", extAnalysis.RecommendedDockerBase)
		e.warnings = append(e.warnings, fmt.Sprintf("Required extensions: %v", extAnalysis.RequiredExtensions))
	}

	// Store auth compatibility SQL for validation
	if extAnalysis.AuthCompatibilitySQL != "" {
		e.authCompatibilitySQL = extAnalysis.AuthCompatibilitySQL
		e.logger.Info("Generated auth compatibility layer for: %s", extAnalysis.AuthService)
	}

	// Pre-processing: Generate backup if enabled
	if e.backupGenerator != nil && e.prodDB != nil {
		e.logger.Info("Generating backup before squashing...")
		// Use database connection string from config
		backupResult, err := e.backupGenerator.GeneratePreMigrationBackup(ctx, e.config.ProdDBDSN)
		if err != nil {
			return "", nil, errors.NewError(
				errors.ErrorCodeBackupGenerationFailed,
				"failed to generate backup",
				errors.SeverityError,
				errors.CategoryBackup,
			).WithInnerError(err)
		}
		e.logger.Info("Backup created at: %s", backupResult.BackupPath)
		e.warnings = append(e.warnings, fmt.Sprintf("Backup created: %s", backupResult.BackupPath))
	}

	// Initialize rollback manager if enabled
	if e.rollbackManager != nil {
		// Parse migrations to extract statements for rollback planning
		var allStatements []types.Statement
		for id, migrationSQL := range migrations {
			filename := fmt.Sprintf("migration_%d.sql", id)
			migration, err := parser.ParseMigration(migrationSQL, filename)
			if err == nil {
				allStatements = append(allStatements, migration.Statements...)
			}
		}

		// Create a rollback plan
		planName := fmt.Sprintf("squash_%d", startTime.Unix())
		plan, err := e.rollbackManager.CreateRollbackPlan(ctx, planName, allStatements)
		if err != nil {
			e.logger.Info("Warning: Failed to create rollback plan: %v", err)
			e.warnings = append(e.warnings, fmt.Sprintf("Rollback plan creation failed: %v", err))
		} else {
			planPath := filepath.Join(e.rollbackPath, "rollback_plans", fmt.Sprintf("%s.json", plan.ID))
			e.logger.Info("Rollback plan '%s' created successfully at %s", planName, planPath)
			e.warnings = append(e.warnings, fmt.Sprintf("Rollback script generated: %s (%d operations)", planPath, len(plan.Scripts)))
		}
	}

	// Phase 1: Parse and build object lifecycles
	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(0, e.stats.TotalMigrations, "Parsing migrations")
	}

	if err := e.parseAndTrackMigrations(ctx, migrations); err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeAnalysisError,
			"parse and track migrations",
			errors.SeverityError,
			errors.CategoryParsing,
		).WithInnerError(err)
	}

	e.stats.MigrationsProcessed = e.stats.TotalMigrations
	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(e.stats.MigrationsProcessed, e.stats.TotalMigrations, "Analyzing dependencies")
	}

	// Phase 2: Analyze dependencies and detect issues
	if err := e.analyzeDependenciesAndRisks(ctx); err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeDependencyError,
			"analyze dependencies",
			errors.SeverityError,
			errors.CategoryDependency,
		).WithInnerError(err)
	}

	// Phase 3: Apply consolidation rules
	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(e.stats.MigrationsProcessed, e.stats.TotalMigrations, "Applying consolidation rules")
	}

	consolidatedObjects, err := e.applyConsolidationRules(ctx)
	if err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"apply consolidation rules",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}

	// Phase 4: Generate optimized SQL with modern formatting
	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(e.stats.MigrationsProcessed, e.stats.TotalMigrations, "Generating SQL")
	}

	finalSQL, err := e.generateOptimizedSQL(ctx, consolidatedObjects)
	if err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			"generate optimized SQL",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}

	// Phase 4.5: Apply SQL transformations if enabled
	if e.sqlTransformer != nil {
		e.logger.Info("Applying SQL transformations...")
		transformResult, err := e.sqlTransformer.Transform(ctx, finalSQL)
		if err != nil {
			e.logger.Info("Warning: SQL transformation failed: %v", err)
			e.warnings = append(e.warnings, fmt.Sprintf("SQL transformation warning: %v", err))
		} else {
			finalSQL = transformResult.TransformedSQL
			if len(transformResult.Transformations) > 0 {
				e.logger.Info("Applied %d SQL transformations", len(transformResult.Transformations))
				for _, transformation := range transformResult.Transformations {
					e.warnings = append(e.warnings, fmt.Sprintf("Transformation: %s", transformation.Description))
				}
			}
		}
	}

	// Phase 5: Perform database validation if in paranoid mode
	if e.config.SafetyLevel == string(Paranoid) && e.prodDB != nil {
		if err := e.validateAgainstDatabase(ctx, finalSQL); err != nil {
			e.warnings = append(e.warnings, fmt.Sprintf("Database validation warning: %v", err))
		}
	}

	processingTime := time.Since(startTime)
	e.stats.ProcessingTime = processingTime
	e.logger.Info("Enhanced squashing completed in %v", processingTime)

	// Final progress callback
	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(e.stats.MigrationsProcessed, e.stats.TotalMigrations, "Completed")
	}

	return finalSQL, e.warnings, nil
}

// SquashWithSeparateFiles performs squashing and returns separate files for DDL and data operations
func (e *Engine) SquashWithSeparateFiles(migrations map[int]string) (*SquashResult, error) {
	// Perform regular squashing to get baseline DDL
	baselineSQL, warnings, err := e.Squash(migrations)
	if err != nil {
		return nil, err
	}

	// Generate separate data operations SQL
	dataSQL, err := e.generateDataOperationsSQL()
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			"failed to generate data operations SQL",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}

	// Detect extensions
	detector := NewExtensionDetector()
	analysis := detector.AnalyzeMigrations(migrations)

	// Create provenance tracker
	provenance := NewProvenanceTracker(
		"0.9.0", // TODO: Get from version constant
		e.config.SafetyLevel,
		e.config.PostgreSQLFeatures.TargetVersion,
		analysis.RequiredExtensions,
	)

	// Add input files
	for i := range migrations {
		provenance.AddInputFile(fmt.Sprintf("migration_%d.sql", i))
	}

	// Add output files
	provenance.AddOutputFile("000_baseline.sql")
	if dataSQL != "" {
		provenance.AddOutputFile("010_data.sql")
	}

	// Compute content hash
	provenance.ComputeContentHash(baselineSQL + dataSQL)

	// Add warnings
	for _, warning := range warnings {
		provenance.AddWarning(warning)
	}

	// Build result
	result := &SquashResult{
		BaselineSQL:       baselineSQL,
		DataOperationsSQL: dataSQL,
		Warnings:          warnings,
		ProvenanceMap:     provenance.GetSquashMap(),
		Extensions:        analysis.RequiredExtensions,
	}

	return result, nil
}

// SquashStreaming processes migrations with streaming approach for large datasets
func (e *Engine) SquashStreaming(migrations map[int]string) (string, []string, error) {
	if !e.enableStreaming {
		return "", nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"streaming not enabled for this engine instance",
			errors.SeverityError,
			errors.CategoryConsolidation,
		)
	}

	startTime := time.Now()
	ctx := context.Background()

	e.updatePhase("Initializing")
	e.stats.TotalMigrations = int64(len(migrations))

	e.logger.Info("Starting streaming squash process with %d migrations", len(migrations))

	// Phase 1: Stream process migrations using batching
	e.updatePhase("Parsing and Tracking")
	if err := e.streamProcessMigrations(ctx, migrations); err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"stream process migrations",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}

	// Get the underlying tracker
	tracker := e.streamingTracker.GetTracker()
	e.tracker = tracker

	// Get lifecycles from tracker
	lifecycles := e.tracker.GetObjectsByCategory()
	for _, categoryObjects := range lifecycles {
		for _, lifecycle := range categoryObjects {
			e.lifecycles[lifecycle.Key] = lifecycle
		}
	}

	e.stats.ObjectsTracked = int64(len(e.lifecycles))

	// Continue with existing engine phases
	e.updatePhase("Analyzing Dependencies")
	if err := e.analyzeDependenciesAndRisks(ctx); err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeDependencyError,
			"analyze dependencies",
			errors.SeverityError,
			errors.CategoryDependency,
		).WithInnerError(err)
	}

	e.updatePhase("Applying Consolidations")
	consolidatedObjects, err := e.applyConsolidationRules(ctx)
	if err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"apply consolidation rules",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}

	e.stats.ConsolidationsApplied = int64(len(consolidatedObjects))

	e.updatePhase("Generating SQL")
	finalSQL, err := e.generateOptimizedSQL(ctx, consolidatedObjects)
	if err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			"generate final SQL",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}

	// Update final statistics
	e.stats.ProcessingTime = time.Since(startTime)
	if e.stats.ProcessingTime.Seconds() > 0 {
		e.stats.ThroughputMPS = float64(e.stats.MigrationsProcessed) / e.stats.ProcessingTime.Seconds()
	}
	e.updatePhase("Completed")

	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(e.stats.MigrationsProcessed, e.stats.TotalMigrations, "Completed")
	}

	e.logger.Info("Streaming squash completed in %v (%.2f migrations/sec)",
		e.stats.ProcessingTime, e.stats.ThroughputMPS)

	return finalSQL, e.warnings, nil
}

// SquashFromDirectory processes migrations from a directory using streaming
func (e *Engine) SquashFromDirectory(dir string) (string, []string, error) {
	if !e.enableStreaming {
		return "", nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"streaming not enabled for this engine instance",
			errors.SeverityError,
			errors.CategoryConsolidation,
		)
	}

	startTime := time.Now()
	ctx := context.Background()

	e.updatePhase("Initializing")
	e.logger.Info("Starting streaming squash process from directory: %s", dir)

	// Phase 1: Stream parse and track migrations
	e.updatePhase("Parsing and Tracking")
	if err := e.streamParseAndTrack(ctx, dir); err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"stream parse and track",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}

	// Get the underlying tracker from streaming tracker
	tracker := e.streamingTracker.GetTracker()

	// Update engine's tracker to use the streaming tracker's results
	e.tracker = tracker

	// Phase 2: Analyze dependencies (using existing engine logic)
	e.updatePhase("Analyzing Dependencies")
	if err := e.analyzeDependenciesAndRisks(ctx); err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeDependencyError,
			"analyze dependencies",
			errors.SeverityError,
			errors.CategoryDependency,
		).WithInnerError(err)
	}

	// Phase 3: Apply consolidation rules (using existing engine logic)
	e.updatePhase("Applying Consolidations")
	consolidatedObjects, err := e.applyConsolidationRules(ctx)
	if err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"apply consolidation rules",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}

	e.stats.ConsolidationsApplied = int64(len(consolidatedObjects))

	// Phase 4: Generate final SQL (using existing engine logic)
	e.updatePhase("Generating SQL")
	finalSQL, err := e.generateOptimizedSQL(ctx, consolidatedObjects)
	if err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			"generate final SQL",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}

	// Update final statistics
	e.stats.ProcessingTime = time.Since(startTime)
	e.updatePhase("Completed")

	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(e.stats.MigrationsProcessed, e.stats.TotalMigrations, "Completed")
	}

	e.logger.Info("Streaming squash completed in %v", e.stats.ProcessingTime)
	return finalSQL, e.warnings, nil
}

// parseAndTrackMigrations parses migrations and builds object lifecycles
func (e *Engine) parseAndTrackMigrations(ctx context.Context, migrations map[int]string) error {
	e.logger.Info("Parsing %d migration files with enhanced tracking", len(migrations))

	for sequence, migrationContent := range migrations {
		migration, err := parser.ParseMigration(migrationContent, fmt.Sprintf("migration_%d", sequence))
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeSyntaxError,
				fmt.Sprintf("parse migration %d", sequence),
				errors.SeverityError,
				errors.CategoryParsing,
			).WithInnerError(err)
		}

		// Process with enhanced tracker
		e.tracker.ProcessMigration(migration, sequence)

		// Collect data operations separately (INSERT/UPDATE/DELETE)
		for stmtIndex, stmt := range migration.Statements {
			if stmt.IsDataOp {
				// Analyze pragmas for data operations too
				sa := parser.NewStatementAnalyzer(e.config.PostgreSQLFeatures.TargetVersion)
				sa.AnalyzeStatement(&stmt)
				sa.AnalyzePragmas(&stmt)

				if err := e.dataOperationTracker.AddOperation(stmt, sequence, stmtIndex); err != nil {
					e.logger.Info("Warning: failed to track data operation at migration %d, statement %d: %v", sequence, stmtIndex, err)
				}
			}
		}

		// Mark as processed
		e.processedFiles[migration.Filename] = true
	}

	// Get object lifecycles from tracker
	lifecycles := e.tracker.GetObjectsByCategory()
	for _, categoryObjects := range lifecycles {
		for _, lifecycle := range categoryObjects {
			e.lifecycles[lifecycle.Key] = lifecycle
		}
	}

	e.logger.Info("Tracked %d database objects across %d categories", len(e.lifecycles), len(lifecycles))
	return nil
}

// analyzeDependenciesAndRisks analyzes object dependencies and assesses risks
func (e *Engine) analyzeDependenciesAndRisks(ctx context.Context) error {
	e.logger.Info("Analyzing dependencies and risks for %d objects", len(e.lifecycles))

	// Use unified dependency resolver for complex scenarios
	unifiedResolver := NewUnifiedDependencyResolver()

	sortedObjects, err := unifiedResolver.ResolveLifecycleDependencies(
		e.tracker.GetActualDependencyGraph(),
		e.lifecycles,
	)
	if err != nil {
		e.warnings = append(e.warnings, fmt.Sprintf("Unified dependency resolution warnings: %v", err))

		// Fallback to basic topological sort
		if basicSorted, basicErr := e.tracker.GetActualDependencyGraph().TopologicalSort(); basicErr != nil {
			e.warnings = append(e.warnings, fmt.Sprintf("Circular dependencies detected: %v", basicErr))
		} else {
			e.logger.Info("Fallback dependency analysis complete: %d objects in correct order", len(basicSorted))
		}
	} else {
		e.logger.Info("Enhanced dependency analysis complete: %d objects in optimized order", len(sortedObjects))
	}

	// Detect specific dependency cycles
	if cycles := e.tracker.GetActualDependencyGraph().DetectCycles(); len(cycles) > 0 {
		for _, cycle := range cycles {
			cycleStr := make([]string, len(cycle))
			for i, obj := range cycle {
				cycleStr[i] = obj.String()
			}
			e.warnings = append(e.warnings, fmt.Sprintf("Circular dependency: %s", strings.Join(cycleStr, " -> ")))
		}
	}

	if e.enableCycleDetection {
		unifiedTracker := e.tracker // Tracker is an alias for UnifiedTracker
		e.logger.Info("Running advanced DDL cycle detection...")
		if err := unifiedTracker.DetectDDLCycles(); err != nil {
			e.logger.Info("Warning: DDL cycle detection failed: %v", err)
			e.warnings = append(e.warnings, fmt.Sprintf("DDL cycle detection warning: %v", err))
		} else {
			detectedCycles := unifiedTracker.GetDetectedCycles()
			if len(detectedCycles) > 0 {
				e.logger.Info("Detected %d DDL cycles", len(detectedCycles))
				for _, cycle := range detectedCycles {
					severity := string(cycle.Severity)
					objectsStr := strings.Join(cycle.Objects, ", ")

					// Basic cycle warning (always shown)
					basicWarning := fmt.Sprintf("DDL Cycle [%s] %s: %s", severity, cycle.Type, objectsStr)
					e.warnings = append(e.warnings, basicWarning)

					if e.showCycleDetails {
						// Add cycle description
						if cycle.Description != "" {
							e.warnings = append(e.warnings, fmt.Sprintf("  Description: %s", cycle.Description))
						}

						// Add cycle path visualization
						if len(cycle.Objects) > 1 {
							cyclePath := strings.Join(cycle.Objects, " → ")
							if cycle.Type == tracking.DependencyCycle || cycle.Type == tracking.ConstraintCycle {
								cyclePath += " → " + cycle.Objects[0] // Show circular path
							}
							e.warnings = append(e.warnings, fmt.Sprintf("  Cycle path: %s", cyclePath))
						}

						// Add operation details
						if len(cycle.Operations) > 0 && len(cycle.Operations) <= 10 {
							e.warnings = append(e.warnings, fmt.Sprintf("  Operations involved: %d", len(cycle.Operations)))
							for i, op := range cycle.Operations {
								if i < 5 { // Show first 5 operations
									// Use loop index for sequential numbering (0, 1, 2...) instead of op.Sequence
									e.warnings = append(e.warnings, fmt.Sprintf("    %d. %s on %s", i, op.Operation, op.Object))
								}
							}
							if len(cycle.Operations) > 5 {
								e.warnings = append(e.warnings, fmt.Sprintf("    ... and %d more operations", len(cycle.Operations)-5))
							}

							// Add resolution suggestions AFTER operations for better readability
							suggestions := e.generateCycleResolutionSuggestions(cycle)
							if len(suggestions) > 0 {
								e.warnings = append(e.warnings, "  Suggested resolutions:")
								for _, suggestion := range suggestions {
									e.warnings = append(e.warnings, fmt.Sprintf("    → %s", suggestion))
								}
							}
						} else if len(cycle.Operations) > 10 {
							// For large cycles, show summary only
							e.warnings = append(e.warnings, fmt.Sprintf("  Operations involved: %d (too many to display)", len(cycle.Operations)))
							suggestions := e.generateCycleResolutionSuggestions(cycle)
							if len(suggestions) > 0 {
								e.warnings = append(e.warnings, "  Suggested resolutions:")
								for _, suggestion := range suggestions {
									e.warnings = append(e.warnings, fmt.Sprintf("    → %s", suggestion))
								}
							}
						}

						// Add optimization safety notice
						if cycle.CanOptimize {
							e.warnings = append(e.warnings, "  ☑ This cycle can be safely optimized")
						} else {
							e.warnings = append(e.warnings, "  ⚠ This cycle should be preserved as-is for safety")
						}
					}

					// Log additional details for critical cycles
					if cycle.Severity == tracking.SeverityCritical {
						e.logger.Info("CRITICAL DDL cycle detected: %s involving objects: %s", cycle.Description, objectsStr)
					}
				}

				// Report critical cycles count
				criticalCycles := unifiedTracker.GetCriticalCycles()
				if len(criticalCycles) > 0 {
					e.warnings = append(e.warnings, fmt.Sprintf("WARNING: %d critical DDL cycles detected - some optimizations may be unsafe", len(criticalCycles)))
				}
			} else {
				e.logger.Info("No DDL cycles detected - all optimizations are safe")
			}
		}
	}

	// Validate consistency
	warnings := e.tracker.ValidateConsistency()
	e.warnings = append(e.warnings, warnings...)

	return nil
}

// applyConsolidationRules applies safety-appropriate consolidation rules
func (e *Engine) applyConsolidationRules(ctx context.Context) (map[string]*tracking.ConsolidationResult, error) {
	e.logger.Info("Applying %s safety level consolidation rules", e.config.SafetyLevel)

	consolidatedObjects := make(map[string]*tracking.ConsolidationResult)

	for key, lifecycle := range e.lifecycles {
		// Skip if already processed
		if _, exists := e.consolidationResults[key]; exists {
			continue
		}

		// Apply consolidation rules
		result, err := e.ruleEngine.ApplyRules(lifecycle, e)
		if err != nil {
			e.warnings = append(e.warnings, fmt.Sprintf("Rule application failed for %s: %v", key, err))
			// Don't drop objects on error - preserve them as-is
			if lifecycle.GetFinalState() != nil {
				result = &tracking.ConsolidationResult{
					OriginalStatements: []types.Statement{*lifecycle.GetFinalState()},
					ConsolidatedSQL:    lifecycle.GetFinalState().SQL,
					Optimizations:      []string{"preserved_after_rule_failure"},
					Warnings:           []string{fmt.Sprintf("Rule application failed: %v", err)},
					RiskLevel:          tracking.RiskLevelLow,
				}
			}
		}

		if result != nil {
			// Validate ConsolidatedSQL is not empty
			if strings.TrimSpace(result.ConsolidatedSQL) == "" {
				e.warnings = append(e.warnings, fmt.Sprintf("Object %s has empty ConsolidatedSQL, skipping", key))
				continue
			}

			// For tables, check if there are ALTER statements that must remain separate
			// Some ALTERs (like RLS) cannot be integrated into CREATE TABLE
			if lifecycle.Type == types.TypeTable {
				allAlterStmts := lifecycle.GetAlterStatements()

				// Check which ALTER types must remain as separate statements
				var separateAlters []types.Statement
				for _, alterStmt := range allAlterStmts {
					alterSQL := strings.ToUpper(alterStmt.SQL)
					// These ALTER operations cannot be integrated into CREATE TABLE
					mustBeSeparate := strings.Contains(alterSQL, "ENABLE ROW LEVEL SECURITY") ||
						strings.Contains(alterSQL, "DISABLE ROW LEVEL SECURITY") ||
						strings.Contains(alterSQL, "FORCE ROW LEVEL SECURITY") ||
						strings.Contains(alterSQL, "NO FORCE ROW LEVEL SECURITY") ||
						strings.Contains(alterSQL, "ALTER COLUMN") || // Column modifications
						strings.Contains(alterSQL, "DROP COLUMN") || // Column drops
						strings.Contains(alterSQL, "RENAME COLUMN") || // Column renames
						strings.Contains(alterSQL, "RENAME TO") || // Table renames
						strings.Contains(alterSQL, "OWNER TO") || // Owner changes
						strings.Contains(alterSQL, "SET SCHEMA") // Schema changes

					if mustBeSeparate {
						separateAlters = append(separateAlters, alterStmt)
					}
				}

				if len(separateAlters) > 0 {
					// Ensure CREATE TABLE ends with semicolon before appending ALTERs
					result.ConsolidatedSQL = strings.TrimSpace(result.ConsolidatedSQL)
					if !strings.HasSuffix(result.ConsolidatedSQL, ";") {
						result.ConsolidatedSQL += ";"
					}
					// Append ALTER statements that must be separate
					for _, alterStmt := range separateAlters {
						result.ConsolidatedSQL += "\n\n" + alterStmt.SQL
						// Only add to OriginalStatements if not already there
						alreadyIncluded := false
						for _, existing := range result.OriginalStatements {
							if existing.SQL == alterStmt.SQL {
								alreadyIncluded = true
								break
							}
						}
						if !alreadyIncluded {
							result.OriginalStatements = append(result.OriginalStatements, alterStmt)
						}
					}
					e.logger.Info("Added %d ALTER statements that must remain separate for table %s", len(separateAlters), key)
				}
			}

			consolidatedObjects[key] = result
			e.consolidationResults[key] = result
			e.logger.Info("Applied consolidation to %s: %v", key, result.Optimizations)
		} else if lifecycle.GetFinalState() != nil {
			// No consolidation rules applied - preserve object as-is
			finalState := lifecycle.GetFinalState()
			if strings.TrimSpace(finalState.SQL) == "" {
				e.warnings = append(e.warnings, fmt.Sprintf("Object %s has empty SQL, skipping", key))
				continue
			}

			consolidatedSQL := finalState.SQL

			// BUGFIX Bug #5: Ensure SQL always ends with semicolon before appending separate statements
			consolidatedSQL = strings.TrimRight(consolidatedSQL, " \t\n")
			if !strings.HasSuffix(consolidatedSQL, ";") {
				consolidatedSQL += ";"
			}

			allStatements := []types.Statement{*finalState}

			// For tables, include ALTER statements as separate statements (not consolidated)
			// When no consolidation rules apply, we preserve original migration structure
			if finalState.ObjectType == types.TypeTable {
				alterStmts := lifecycle.GetAlterStatements()
				if len(alterStmts) > 0 {
					// Append ALTER statements after CREATE TABLE (original migration structure)
					// No need to add semicolon again - already added above
					for _, alterStmt := range alterStmts {
						consolidatedSQL += "\n\n" + alterStmt.SQL
						allStatements = append(allStatements, alterStmt)
					}
					e.logger.Info("Preserved %d ALTER statements for table %s (no consolidation rules applied)", len(alterStmts), key)
				}
			}

			result = &tracking.ConsolidationResult{
				OriginalStatements: allStatements,
				ConsolidatedSQL:    consolidatedSQL,
				Optimizations:      []string{"preserved_no_applicable_rules"},
				Warnings:           []string{},
				RiskLevel:          tracking.RiskLevelLow,
			}
			consolidatedObjects[key] = result
			e.consolidationResults[key] = result
			e.logger.Info("Preserved object without consolidation: %s", key)
		}
	}

	e.logger.Info("Successfully consolidated %d objects", len(consolidatedObjects))
	return consolidatedObjects, nil
}

// generateOptimizedSQL generates the final optimized SQL with modern formatting
func (e *Engine) generateOptimizedSQL(ctx context.Context, consolidatedObjects map[string]*tracking.ConsolidationResult) (string, error) {
	e.logger.Info("Generating optimized SQL for %d consolidated objects", len(consolidatedObjects))

	e.sqlBuilder.Reset()

	// Add header comment
	e.sqlBuilder.Comment("Generated by pgsquash with modern PostgreSQL patterns")
	e.sqlBuilder.Comment(fmt.Sprintf("Safety level: %s", e.config.SafetyLevel))
	e.sqlBuilder.Comment(fmt.Sprintf("Generated at: %s", time.Now().Format(time.RFC3339)))
	e.sqlBuilder.NL()

	// Inject auth compatibility layer if needed (BUG #4 FIX)
	// Check if any migration uses auth.jwt(), Supabase roles, or storage schema
	needsAuthCompat := false
	needsSupabaseCompat := false
	for _, result := range consolidatedObjects {
		sqlLower := strings.ToLower(result.ConsolidatedSQL)
		if strings.Contains(sqlLower, "auth.jwt()") || strings.Contains(sqlLower, "auth.uid()") {
			needsAuthCompat = true
			// Detect if it's Supabase (auth.users, storage.) or Clerk (organization patterns)
			if strings.Contains(sqlLower, "auth.users") || strings.Contains(sqlLower, "storage.") {
				needsSupabaseCompat = true
			}
		}
		if strings.Contains(sqlLower, "role authenticated") || strings.Contains(sqlLower, "role service_role") {
			needsAuthCompat = true
		}
		if strings.Contains(sqlLower, "supabase_realtime") || strings.Contains(sqlLower, "storage.buckets") {
			needsSupabaseCompat = true
		}
	}

	if needsAuthCompat {
		e.sqlBuilder.NL().Comment("=== AUTH COMPATIBILITY LAYER ===")
		e.sqlBuilder.Comment("Auto-injected stubs for Supabase/Clerk authentication")
		e.sqlBuilder.NL()

		// Use the existing compatibility generator
		var compatSQL string
		if needsSupabaseCompat {
			e.logger.Info("Injecting Supabase auth compatibility layer")
			generator := auth.NewCompatibilityGenerator(auth.ServiceSupabase)
			compatSQL = generator.Generate()
		} else {
			e.logger.Info("Injecting Clerk auth compatibility layer")
			generator := auth.NewCompatibilityGenerator(auth.ServiceClerk)
			compatSQL = generator.Generate()
		}

		e.sqlBuilder.Statement(compatSQL)
		e.sqlBuilder.NL().NL()
	}

	// Group by category for organized output - CRITICAL: Order must ensure dependencies are created first
	categories := []types.Category{
		types.CategoryExtensions,  // 1. Extensions first (CREATE EXTENSION)
		types.CategoryFoundation,  // 2. Tables, views, sequences (CREATE TABLE)
		types.CategoryConstraints, // 3. Constraints (ALTER TABLE ADD CONSTRAINT)
		types.CategoryFunctions,   // 4. Functions (CREATE FUNCTION)
		types.CategoryTriggers,    // 5. Triggers (CREATE TRIGGER)
		types.CategoryIndexes,     // 6. Indexes (CREATE INDEX)
		types.CategorySecurity,    // 7. RLS Policies (CREATE POLICY)
		// Data operations are NOT included here - they go to separate file
	}

	// Initialize unified dependency resolver
	unifiedResolver := NewUnifiedDependencyResolver()

	// ================================================================
	// CIRCULAR FK DETECTION AND RESOLUTION
	// ================================================================
	// Before generating Foundation objects (tables), check for circular
	// foreign key dependencies and handle them with 2-phase approach:
	// 1. CREATE TABLE without circular FKs
	// 2. ALTER TABLE ADD CONSTRAINT for circular FKs (after all tables exist)
	// ================================================================
	var circularFKAlterStatements []*types.Statement

	// Extract table statements from Foundation category
	foundationObjects := e.getObjectsByCategoryAsMap(consolidatedObjects, types.CategoryFoundation)
	tableStatements := make(map[string]*types.Statement)
	for key, result := range foundationObjects {
		if lifecycle, exists := e.lifecycles[key]; exists && lifecycle.Type == types.TypeTable {
			// Use consolidated SQL for circular FK detection (includes integrated constraints)
			// This is critical because ALTER TABLE ADD CONSTRAINT may have been integrated into CREATE TABLE
			if result.ConsolidatedSQL != "" {
				// Create a statement from the consolidated SQL
				consolidatedStmt := &types.Statement{
					SQL:        result.ConsolidatedSQL,
					ObjectType: types.TypeTable,
					ObjectName: lifecycle.Name,
					Schema:     lifecycle.Schema,
					Operation:  types.OpCreate,
				}
				// Parse the consolidated SQL to get AST for FK extraction
				if parseResult, err := pg_query.Parse(result.ConsolidatedSQL); err == nil {
					consolidatedStmt.ParseTree = parseResult
				}
				tableStatements[lifecycle.Name] = consolidatedStmt
			}
		}
	}

	if len(tableStatements) > 0 {
		e.logger.Info("Checking %d tables for circular FK dependencies", len(tableStatements))
		circularFKHandler := NewCircularFKHandler()

		// Detect circular dependencies
		cycles := circularFKHandler.DetectCircularDependencies(tableStatements)

		if len(cycles) > 0 {
			e.logger.Info("☑ Detected %d circular FK dependency chains - applying 2-phase constraint generation", len(cycles))

			// Remove circular FKs from tables and generate ALTER statements
			modifiedTables, alterStmts, err := circularFKHandler.RemoveCircularFKsFromTables(tableStatements, cycles)
			if err != nil {
				e.warnings = append(e.warnings, fmt.Sprintf("Circular FK handling warning: %v", err))
			} else {
				// Update consolidation results with modified table statements
				// CRITICAL: Update both foundationObjects AND consolidatedObjects to ensure changes persist
				for tableName, modifiedStmt := range modifiedTables {
					// Find the consolidation result for this table
					for key := range foundationObjects {
						if lifecycle, exists := e.lifecycles[key]; exists && lifecycle.Name == tableName {
							// Update BOTH maps with the modified version (circular FKs removed)
							foundationObjects[key].ConsolidatedSQL = modifiedStmt.SQL
							foundationObjects[key].Warnings = append(foundationObjects[key].Warnings, "Circular FK constraints removed and deferred to ALTER TABLE")
							consolidatedObjects[key].ConsolidatedSQL = modifiedStmt.SQL
							consolidatedObjects[key].Warnings = append(consolidatedObjects[key].Warnings, "Circular FK constraints removed and deferred to ALTER TABLE")
							e.logger.Info("Updated table %s with circular FKs removed", tableName)
							break
						}
					}
				}

				// Store ALTER statements for later insertion
				circularFKAlterStatements = alterStmts
				e.logger.Info("Generated %d ALTER TABLE statements for circular FK constraints", len(alterStmts))
			}
		} else {
			e.logger.Info("☑ No circular FK dependencies detected - all tables can be created directly")
		}
	}

	for _, category := range categories {
		categoryObjectsMap := e.getObjectsByCategoryAsMap(consolidatedObjects, category)

		// Sort objects by dependencies within category
		sortedObjects := unifiedResolver.SortConsolidationResults(categoryObjectsMap, category, e.lifecycles)

		// Check if we have any objects or circular FK statements to add
		hasObjects := len(sortedObjects) > 0
		hasCircularFKs := (category == types.CategoryConstraints && len(circularFKAlterStatements) > 0)

		if !hasObjects && !hasCircularFKs {
			// Skip empty categories completely
			continue
		}

		// Add category header only if we have objects to display
		e.sqlBuilder.NL().Comment(fmt.Sprintf("=== %s OBJECTS ===", strings.ToUpper(string(category)))).NL()

		for _, result := range sortedObjects {
			sql := result.ConsolidatedSQL

			// Apply CASCADE enhancement for extension objects
			if category == types.CategoryExtensions {
				sql = unifiedResolver.EnhanceExtensionSQL(sql)
			}

			// DEBUG: Log what we're writing for analytics tables
			if category == types.CategoryFoundation && strings.Contains(strings.ToLower(sql), "analytics") {
				startsPreview := strings.ReplaceAll(sql[:min(100, len(sql))], "\n", "\\n")
				endsPreview := strings.ReplaceAll(sql[max(0, len(sql)-100):], "\n", "\\n")
				e.logger.Info("ENGINE-OUTPUT-DEBUG: Writing analytics table: len=%d",
					len(sql))
				e.logger.Info("  STARTS: %s", startsPreview)
				e.logger.Info("  ENDS: %s", endsPreview)
			}

			// DEBUG: Log what we're writing for functions
			if category == types.CategoryFunctions && strings.Contains(strings.ToLower(sql), "clerk_user_id") {
				createCount := strings.Count(strings.ToUpper(sql), "CREATE")
				functionCount := strings.Count(strings.ToUpper(sql), "FUNCTION")
				e.logger.Info("Writing clerk_user_id function to output: %d CREATE, %d FUNCTION keywords, len=%d",
					createCount, functionCount, len(sql))
				if createCount > 1 || functionCount > 1 {
					e.logger.Info("  WARNING: ConsolidatedSQL contains multiple functions!")
					e.logger.Info("  SQL preview: %s...", strings.ReplaceAll(sql[:min(400, len(sql))], "\n", " "))
				}
			}

			e.sqlBuilder.Statement(sql)
			e.sqlBuilder.NL().NL()
		}

		// CRITICAL: After Foundation objects, add circular FK ALTER statements to Constraints category
		// This ensures tables are created first, then circular FK constraints are added
		if category == types.CategoryConstraints && len(circularFKAlterStatements) > 0 {
			e.sqlBuilder.NL().Comment("Circular FK constraints (added after all tables exist)").NL()
			for _, alterStmt := range circularFKAlterStatements {
				// Add comment from statement
				for _, comment := range alterStmt.Comments {
					e.sqlBuilder.Comment(strings.TrimPrefix(comment, "-- ")).NL()
				}
				e.sqlBuilder.Statement(alterStmt.SQL)
				e.sqlBuilder.NL().NL()
			}
			e.logger.Info("☑ Added %d circular FK ALTER TABLE statements to Constraints category", len(circularFKAlterStatements))
		}
	}

	// Add any objects not included in categories (fallback)
	addedObjects := make(map[string]bool)
	for _, category := range categories {
		categoryObjects := e.getObjectsByCategory(consolidatedObjects, category)
		for range categoryObjects {
			for key := range consolidatedObjects {
				if lifecycle, exists := e.lifecycles[key]; exists && lifecycle.Category == category {
					addedObjects[key] = true
				}
			}
		}
	}

	// Add objects that weren't included in any category
	uncategorizedCount := 0
	uncategorizedObjects := make(map[string]*tracking.ConsolidationResult)
	for key, result := range consolidatedObjects {
		if !addedObjects[key] {
			uncategorizedObjects[key] = result
			uncategorizedCount++
		}
	}

	// Only add header if there are uncategorized objects
	if uncategorizedCount > 0 {
		e.sqlBuilder.NL().Comment("=== UNCATEGORIZED OBJECTS ===").NL()
		for _, result := range uncategorizedObjects {
			e.sqlBuilder.Statement(result.ConsolidatedSQL)
			e.sqlBuilder.NL().NL()
		}
		e.logger.Info("Added %d uncategorized objects to output", uncategorizedCount)
	}

	// Add any non-consolidated objects (legacy fallback)
	for key, lifecycle := range e.lifecycles {
		if _, consolidated := consolidatedObjects[key]; !consolidated {
			e.sqlBuilder.Comment(fmt.Sprintf("Original object: %s", key))
			if lifecycle.GetFinalState() != nil {
				e.sqlBuilder.FromStatement(*lifecycle.GetFinalState()).NL().NL()
			}
		}
	}

	// ================================================================
	// POST-PROCESSING PHASE
	// ================================================================
	// Apply post-processing fixes to handle edge cases and artifacts
	// from the consolidation phase. These fixes ensure valid PostgreSQL syntax.
	//
	// EXECUTION ORDER (critical - do not reorder):
	//   1. fixMalformedDropTriggers - Fix DROP TRIGGER IF EXISTS syntax issues
	//   2. fixExtensionOrder - Ensure extensions are created before usage
	//   3. removeOrphanedAlterStatements - Remove ALTER for non-existent objects
	//   4. fixMalformedFunctions - Fix function definition syntax issues
	//   5. fixMissingSemicolons - Add missing statement terminators
	//   6. fixEliminatedEnumReferences - Rewrite references to eliminated ENUM types
	//
	// NOTE: Post-processing runs BEFORE SQL transformation (if enabled).
	// Post-processor fixes consolidation bugs, transformer enhances valid SQL.
	// ================================================================

	finalSQL := e.sqlBuilder.String()

	// Build enum replacements map
	enumReplacements := e.buildEnumReplacementsMap()

	// Apply post-processing using AST-based processor (falls back to regex for malformed SQL)
	processor := postprocessing.NewProcessorAST(e.config)
	finalSQL, err := processor.Apply(finalSQL, enumReplacements)
	if err != nil {
		return "", err
	}

	// CRITICAL SAFETY NET: Fix pg_query deparser corruption bugs that slip through post-processing
	// The deparser sometimes duplicates "char_" prefix on char_length() function calls
	// This happens during deparsing and can corrupt CHECK constraints
	if strings.Contains(finalSQL, "char_char_length") {
		e.logger.Info("SAFETY NET: Fixing char_char_length corruption in final SQL")
		finalSQL = strings.ReplaceAll(finalSQL, "char_char_length", "char_length")
	}

	return finalSQL, nil
}

// generateDataOperationsSQL generates SQL for data operations (INSERT/UPDATE/DELETE) as a separate file
func (e *Engine) generateDataOperationsSQL() (string, error) {
	// Get sorted data operations from dedicated tracker
	sortedDataOps := e.dataOperationTracker.GetSortedOperations()

	if len(sortedDataOps) == 0 {
		return "", nil // No data operations
	}

	// Create a new builder for data operations
	dataBuilder := builder.NewSQLBuilder(builder.DefaultBuildOptions())

	// Add header comment
	dataBuilder.Comment("Data Operations - Generated by pgsquash")
	dataBuilder.Comment("IMPORTANT: These are non-idempotent operations (INSERT/UPDATE/DELETE)")
	dataBuilder.Comment("Run these AFTER the baseline schema is applied")
	dataBuilder.Comment(fmt.Sprintf("Safety level: %s", e.config.SafetyLevel))
	dataBuilder.Comment(fmt.Sprintf("Generated at: %s", time.Now().Format(time.RFC3339)))
	dataBuilder.NL()

	dataBuilder.Comment(fmt.Sprintf("Total data operations: %d (sorted by dependencies)", len(sortedDataOps)))
	dataBuilder.NL()

	// Output each data operation in dependency order
	for _, dataOp := range sortedDataOps {
		// Add a comment showing the operation type and table
		dataBuilder.Comment(fmt.Sprintf("%s on %s", dataOp.Operation, dataOp.Table))
		dataBuilder.Statement(dataOp.Statement.SQL)
		dataBuilder.NL().NL()
	}

	// Log statistics
	stats := e.dataOperationTracker.GetStatistics()
	e.logger.Info("☑ Generated separate data operations file: %d operations (%d INSERTs, %d UPDATEs, %d DELETEs)",
		stats["total_operations"], stats["insert_count"], stats["update_count"], stats["delete_count"])

	return dataBuilder.String(), nil
}

func (e *Engine) getObjectsByCategory(consolidatedObjects map[string]*tracking.ConsolidationResult, category types.Category) []*tracking.ConsolidationResult {
	var objects []*tracking.ConsolidationResult

	for key, result := range consolidatedObjects {
		if lifecycle, exists := e.lifecycles[key]; exists && lifecycle.Category == category {
			objects = append(objects, result)
		}
	}

	return objects
}

func (e *Engine) getObjectsByCategoryAsMap(consolidatedObjects map[string]*tracking.ConsolidationResult, category types.Category) map[string]*tracking.ConsolidationResult {
	objects := make(map[string]*tracking.ConsolidationResult)

	for key, result := range consolidatedObjects {
		if lifecycle, exists := e.lifecycles[key]; exists && lifecycle.Category == category {
			objects[key] = result
		}
	}

	return objects
}

// buildEnumReplacementsMap builds a map of eliminated ENUM names to their primary replacements.
// This is used during post-processing to rewrite references to eliminated ENUMs.
func (e *Engine) buildEnumReplacementsMap() map[string]string {
	enumReplacements := make(map[string]string)

	// Iterate through all consolidation results to find eliminated ENUMs
	for key, result := range e.consolidationResults {
		lifecycle, exists := e.lifecycles[key]
		if !exists {
			continue
		}

		// Check if this is an ENUM type
		if lifecycle.Type != types.TypeEnum && lifecycle.Type != types.TypeType {
			continue
		}

		// Check if it was eliminated (ConsolidatedSQL is a comment)
		if strings.HasPrefix(strings.TrimSpace(result.ConsolidatedSQL), "-- Duplicate ENUM eliminated:") {
			// Parse the warning to extract the mapping
			// Warning format: "ENUM <eliminated> is a duplicate of <primary> - eliminated to prevent conflicts"
			for _, warning := range result.Warnings {
				if strings.Contains(warning, "is a duplicate of") && strings.Contains(warning, "eliminated to prevent conflicts") {
					// Extract eliminated and primary names
					// Format: "ENUM verification_status_enum is a duplicate of verification_status - eliminated to prevent conflicts"
					parts := strings.Split(warning, " is a duplicate of ")
					if len(parts) == 2 {
						eliminatedName := strings.TrimPrefix(parts[0], "ENUM ")
						primaryName := strings.Split(parts[1], " - ")[0]

						enumReplacements[eliminatedName] = primaryName
						e.logger.Info("Enum mapping: %s → %s", eliminatedName, primaryName)
					}
				}
			}
		}
	}

	if len(enumReplacements) > 0 {
		e.logger.Info("Built ENUM replacements map with %d entries", len(enumReplacements))
	}

	return enumReplacements
}

// validateAgainstDatabase validates the generated SQL against the production database
// This provides comprehensive schema comparison in paranoid mode
func (e *Engine) validateAgainstDatabase(ctx context.Context, sql string) error {
	if e.metadataManager == nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"no metadata manager available for validation",
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}

	e.logger.Info("Performing comprehensive database validation in paranoid mode")

	// Create schema comparator
	comparator := metadata.NewSchemaComparator(e.metadataManager)

	// Perform comprehensive schema comparison
	result, err := comparator.CompareSchema(ctx, sql)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"schema comparison failed",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	// Process comparison results
	if len(result.MissingExtensions) > 0 {
		e.logger.Info("⚠ Missing extensions: %v", result.MissingExtensions)
		for _, ext := range result.MissingExtensions {
			e.warnings = append(e.warnings,
				fmt.Sprintf("Extension '%s' required but not installed in database", ext))
		}
	}

	if len(result.MissingDependencies) > 0 {
		e.logger.Info("⚠ Missing dependencies detected: %d", len(result.MissingDependencies))
		for _, dep := range result.MissingDependencies {
			msg := fmt.Sprintf("%s dependency '%s' not found in database (referenced by: %s)",
				dep.ObjectType, dep.ObjectName, dep.ReferencedBy)

			if dep.Severity == "error" {
				e.warnings = append(e.warnings, "ERROR: "+msg)
			} else {
				e.warnings = append(e.warnings, "WARNING: "+msg)
			}
		}
	}

	if len(result.TypeMismatches) > 0 {
		e.logger.Info("⚠ Type mismatches detected: %d", len(result.TypeMismatches))
		for _, mismatch := range result.TypeMismatches {
			msg := fmt.Sprintf("Type mismatch in %s.%s: migration expects %s but database has %s",
				mismatch.Object, mismatch.Column, mismatch.ExpectedType, mismatch.ActualType)

			if mismatch.IsBreaking {
				e.warnings = append(e.warnings, "ERROR: "+msg+" (BREAKING CHANGE)")
			} else {
				e.warnings = append(e.warnings, "WARNING: "+msg)
			}
		}
	}

	if len(result.ConstraintConflicts) > 0 {
		e.logger.Info("⚠ Constraint conflicts detected: %d", len(result.ConstraintConflicts))
		for _, conflict := range result.ConstraintConflicts {
			e.warnings = append(e.warnings,
				fmt.Sprintf("Constraint conflict in %s.%s: expected '%s' but found '%s' (%s)",
					conflict.Table, conflict.ConstraintName,
					conflict.ExpectedDef, conflict.ActualDef, conflict.ConflictType))
		}
	}

	if len(result.BreakingChanges) > 0 {
		e.logger.Info("☒ Breaking changes detected: %d", len(result.BreakingChanges))
		for _, breaking := range result.BreakingChanges {
			e.warnings = append(e.warnings,
				fmt.Sprintf("BREAKING: %s | Impact: %s | Mitigation: %s",
					breaking.Description, breaking.Impact, breaking.Mitigation))
		}
	}

	if len(result.SchemaDrift) > 0 {
		e.logger.Info("⚠ Schema drift detected: %d instances", len(result.SchemaDrift))
		for _, drift := range result.SchemaDrift {
			e.warnings = append(e.warnings,
				fmt.Sprintf("Schema drift (%s): %s %s - %s",
					drift.DriftType, drift.ObjectType, drift.Object, drift.Description))
		}
	}

	// Add warnings from comparison
	e.warnings = append(e.warnings, result.Warnings...)

	// Log summary
	if result.IsValid {
		e.logger.Info("☑ Database validation passed: schema is compatible")
	} else {
		e.logger.Info("☒ Database validation failed: schema incompatibilities detected")
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("schema validation failed: found %d errors, %d warnings, %d breaking changes",
				len(result.MissingDependencies)+len(result.TypeMismatches),
				len(result.Warnings),
				len(result.BreakingChanges)),
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}

	e.logger.Info("Database validation completed: Extensions=%d, Dependencies=%d, TypeMismatches=%d, Constraints=%d, Breaking=%d, Drift=%d",
		len(result.MissingExtensions),
		len(result.MissingDependencies),
		len(result.TypeMismatches),
		len(result.ConstraintConflicts),
		len(result.BreakingChanges),
		len(result.SchemaDrift))

	return nil
}

// streamParseAndTrack streams parsing and tracking from directory
func (e *Engine) streamParseAndTrack(ctx context.Context, dir string) error {
	// Set up progress tracking
	if e.enableProgressTrack {
		e.streamingTracker.SetProgressCallback(func(processed, total int64, throughput float64) {
			e.mu.Lock()
			e.stats.MigrationsProcessed = processed
			e.mu.Unlock()

			if e.progressCb != nil {
				e.progressCb(processed, total, e.stats.Phase)
			}
		})
	}

	// Process directory with streaming
	if err := e.streamingTracker.ProcessDirectory(dir); err != nil {
		return errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"streaming tracker failed",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}

	// Update statistics
	streamStats, _ := e.streamingTracker.GetCombinedStats()
	e.mu.Lock()
	e.stats.MigrationsProcessed = streamStats.MigrationsProcessed
	e.stats.ObjectsTracked = streamStats.ObjectsTracked
	e.stats.PeakMemoryUsage = e.memManager.GetMemoryStats().CurrentMemoryBytes
	e.mu.Unlock()

	return nil
}

// streamProcessMigrations processes migrations using batching for memory efficiency
func (e *Engine) streamProcessMigrations(ctx context.Context, migrations map[int]string) error {
	migrationFiles := make([]*performance.MigrationFile, 0, len(migrations))

	// Convert migrations to MigrationFile format
	for sequence, content := range migrations {
		migrationFile := &performance.MigrationFile{
			Path:     fmt.Sprintf("migration_%d.sql", sequence),
			Content:  []byte(content),
			Sequence: sequence,
			Size:     int64(len(content)),
		}
		migrationFiles = append(migrationFiles, migrationFile)
	}

	// Process in batches to manage memory
	processedCount := 0
	for _, migrationFile := range migrationFiles {
		// Check memory constraints
		if !e.memManager.TrackMemoryUsage(migrationFile.Size) {
			// Force memory cleanup if needed
			e.streamingTracker.GetTracker().ClearProcessedMigrations()
			if !e.memManager.TrackMemoryUsage(migrationFile.Size) {
				return errors.NewError(
					errors.ErrorCodeValidationFailed,
					fmt.Sprintf("migration too large for memory constraints: %s", migrationFile.Path),
					errors.SeverityError,
					errors.CategoryPerformance,
				)
			}
		}

		// Parse migration
		migration, err := parser.ParseMigration(string(migrationFile.Content), migrationFile.Path)
		if err != nil {
			e.memManager.ReleaseMemory(migrationFile.Size)
			return errors.NewError(
				errors.ErrorCodeSyntaxError,
				fmt.Sprintf("parse migration %s", migrationFile.Path),
				errors.SeverityError,
				errors.CategoryParsing,
			).WithInnerError(err)
		}

		// Process through tracker
		e.streamingTracker.GetTracker().ProcessMigration(migration, migrationFile.Sequence)

		// Release memory
		e.memManager.ReleaseMemory(migrationFile.Size)

		processedCount++
		e.mu.Lock()
		e.stats.MigrationsProcessed = int64(processedCount)
		e.mu.Unlock()

		// Update progress
		if e.progressCb != nil {
			e.progressCb(int64(processedCount), e.stats.TotalMigrations, e.stats.Phase)
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}

	return nil
}

// updatePhase updates the current processing phase
func (e *Engine) updatePhase(phase string) {
	e.mu.Lock()
	e.stats.Phase = phase
	e.mu.Unlock()

	e.logger.Info("Squash phase: %s", phase)
}

// GetStats returns current squashing statistics
func (e *Engine) GetStats() SquashStats {
	e.mu.RLock()
	defer e.mu.RUnlock()

	stats := *e.stats

	// Update peak memory from memory manager if streaming is enabled
	if e.enableStreaming && e.memManager != nil {
		memStats := e.memManager.GetMemoryStats()
		if memStats.CurrentMemoryBytes > stats.PeakMemoryUsage {
			stats.PeakMemoryUsage = memStats.CurrentMemoryBytes
		}
	}

	return stats
}

// SetProgressCallback sets a progress callback for the engine
func (e *Engine) SetProgressCallback(callback func(processed, total int64, phase string)) {
	e.progressCb = callback
	e.enableProgressTrack = true
}

// GetMemoryStats returns current memory usage statistics (streaming mode only)
func (e *Engine) GetMemoryStats() performance.MemoryStats {
	if e.enableStreaming && e.memManager != nil {
		return e.memManager.GetMemoryStats()
	}
	return performance.MemoryStats{}
}

// OptimizedSquashForLargeDatasets provides a high-level interface for large migration sets
func OptimizedSquashForLargeDatasets(cfg *config.Config, migrations map[int]string, memoryLimitMB int) (string, []string, error) {
	engineConfig := EngineConfig{
		Config:              cfg,
		EnableStreaming:     true,
		BatchSize:           100, // Larger batches for better throughput
		WorkerCount:         4,
		MemoryLimitMB:       memoryLimitMB,
		EnableProgressTrack: true,
		ProgressCallback: func(processed, total int64, phase string) {
			if total > 0 {
				progress := float64(processed) / float64(total) * 100
				utils.GetDefaultLogger().WithPrefix("ENGINE").Info("Progress: %.1f%% (%d/%d) - %s", progress, processed, total, phase)
			}
		},
	}

	engine := NewEngine(engineConfig)
	defer engine.Close()

	return engine.SquashStreaming(migrations)
}

// OptimizedSquashFromDirectory provides a high-level interface for directory processing
func OptimizedSquashFromDirectory(cfg *config.Config, dir string, memoryLimitMB int) (string, []string, error) {
	engineConfig := EngineConfig{
		Config:              cfg,
		EnableStreaming:     true,
		BatchSize:           50,
		WorkerCount:         2, // Conservative for directory processing
		MemoryLimitMB:       memoryLimitMB,
		EnableProgressTrack: true,
		ProgressCallback: func(processed, total int64, phase string) {
			utils.GetDefaultLogger().WithPrefix("ENGINE").Info("Processing: %d files - %s", processed, phase)
		},
	}

	engine := NewEngine(engineConfig)
	defer engine.Close()

	return engine.SquashFromDirectory(dir)
}

