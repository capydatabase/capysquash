package squasher

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/builder"
	"github.com/capysquash/pgsquash-engine/internal/config"
	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/metadata"
	"github.com/capysquash/pgsquash-engine/internal/parser"
	"github.com/capysquash/pgsquash-engine/internal/performance"
	"github.com/capysquash/pgsquash-engine/internal/plugins"
	"github.com/capysquash/pgsquash-engine/internal/plugins/auth"
	"github.com/capysquash/pgsquash-engine/internal/postprocessing"

	// Enable for selected non-destructive fixes
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/tracking/consolidation"
	"github.com/capysquash/pgsquash-engine/internal/transformation"
	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/capysquash/pgsquash-engine/internal/utils"
	pg_query "github.com/pganalyze/pg_query_go/v6"

	"github.com/capysquash/pgsquash-engine/pkg/validation"
)

// SquashResult represents the result of a squash operation with multiple output files
//
// NOTE: detailed partner-integration metrics (DetailedMetrics/RecommendedActions)
// live exclusively in pkg/engine (detailed_metrics.go); the internal result
// carries only what the engine itself computes.
type SquashResult struct {
	BaselineSQL       string     // DDL-only SQL (000_baseline.sql)
	DataOperationsSQL string     // Data operations SQL (010_data.sql)
	Warnings          []string   // Warnings generated during squash
	ProvenanceMap     *SquashMap // Provenance tracking information
	Extensions        []string   // Extensions detected/required

	// Auth compatibility for validation
	AuthCompatibilitySQL string `json:"auth_compatibility_sql,omitempty"`
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
	version    string // Tool version stamped into provenance (fallback: "dev")
	lifecycles map[string]*tracking.ObjectLifecycle
	warnings   []string
	prodDB     *sql.DB
	ctx        context.Context

	// Validators
	preFlightValidator  *validation.StaticValidator
	postFlightValidator *validation.StaticValidator

	// Enhanced components
	metadataManager      *metadata.MetadataManager
	tracker              *tracking.Tracker
	dataOperationTracker *tracking.DataOperationTracker // Separate tracker for data operations (INSERT/UPDATE/DELETE)
	sqlBuilder           *builder.SQLBuilder
	ruleEngine           *consolidation.ConsolidationRuleEngine
	logger               *utils.Logger // Centralized logger with ENGINE prefix

	// Transformation components
	backupGenerator     *transformation.BackupGenerator
	rollbackManager     *transformation.RollbackManager
	sqlTransformer      *transformation.SQLTransformer
	rollbackPath        string
	backupRetentionDays int

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

	// Output options
	excludeDataFromBaseline bool

	// Extension analysis and auth compatibility
	authCompatibilitySQL string
	extAnalysis          *ExtensionAnalysis
	envPrepared          bool

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
	Config  *config.Config
	Context context.Context

	// Version is the tool version stamped into provenance metadata
	// (.squashmap.json). Callers set it from their release metadata (e.g. the
	// CLI passes its rootCmd version). Empty falls back to "dev" - never a
	// hardcoded release string.
	Version string

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
	BackupPath           string // Directory for pg_dump backups (default: <output dir>/.backups)
	BackupRetentionDays  int    // Retention window for old backups (0 = keep forever)

	// Output options
	ExcludeDataFromBaseline bool // If true, filter data operations from baseline SQL (default: false, meaning include)

	EnableCycleDetection bool
	ShowCycleDetails     bool
	CycleDetectionDepth  int
}

// NewEngine creates an enhanced engine with the provided configuration.
//
// Breaking change: constructor failures now return explicit errors instead of
// silently returning nil engine pointers.
func NewEngine(cfg EngineConfig) (*Engine, error) {
	return newEngineInternal(cfg)
}

// newEngineInternal is the internal implementation
func newEngineInternal(engineCfg EngineConfig) (*Engine, error) {
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
	engineContext := engineCfg.Context
	if engineContext == nil {
		engineContext = context.Background()
	}

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

	// Validate the safety level once at the engine boundary. Every entry point
	// (CLI flag, config file, library config) funnels through here.
	if _, err := ParseSafetyLevel(cfg.SafetyLevel); err != nil {
		return nil, err
	}

	// Streaming mode does not execute the backup, rollback, transformation, or
	// paranoid database-validation phases. Silently dropping explicitly
	// requested safety features is not acceptable, so reject the combination
	// outright. Callers with a soft default (e.g. the CLI's --transform which
	// defaults to true) must disable the feature before constructing a
	// streaming engine and surface a warning to the user.
	if enableStreaming {
		if engineCfg.EnableBackup || engineCfg.EnableRollback {
			return nil, errors.NewError(
				errors.ErrorCodeValidationFailed,
				"backup and rollback generation are not supported in streaming mode",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithSuggestion("Disable streaming mode, or drop --backup/--rollback for streaming runs")
		}
		if engineCfg.EnableTransformation {
			return nil, errors.NewError(
				errors.ErrorCodeValidationFailed,
				"SQL transformation is not supported in streaming mode",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithSuggestion("Disable streaming mode, or drop --transform for streaming runs")
		}
		if cfg.SafetyLevel == string(Paranoid) {
			return nil, errors.NewError(
				errors.ErrorCodeValidationFailed,
				"paranoid safety level requires database validation, which streaming mode does not execute",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithSuggestion("Use conservative safety level with streaming, or disable streaming for paranoid runs")
		}
	}

	// Database connection for paranoid mode or backup feature
	var db *sql.DB
	var err error
	needsDB := cfg.SafetyLevel == string(Paranoid) || engineCfg.EnableBackup

	if needsDB {
		if cfg.ProdDBDSN == "" {
			if cfg.SafetyLevel == string(Paranoid) {
				// Paranoid is documented as "requires DB". Running it without a
				// production connection silently skips the database validation
				// that defines the level, so fail closed instead of degrading.
				return nil, errors.NewError(
					errors.ErrorCodeValidationFailed,
					"paranoid safety level requires a production database connection (PROD_DB_DSN)",
					errors.SeverityError,
					errors.CategoryValidation,
				).WithSuggestion("Set the PROD_DB_DSN environment variable or prod_db_dsn in the config, or use the conservative safety level")
			}
			if engineCfg.EnableBackup {
				logger.Warn("Backup generation requested, but no production database DSN provided. Backup will be skipped.")
			}
		} else {
			db, err = sql.Open("postgres", cfg.ProdDBDSN)
			if err != nil {
				return nil, errors.NewError(
					errors.ErrorCodeValidationFailed,
					"failed to connect to production database",
					errors.SeverityCritical,
					errors.CategoryValidation,
				).WithInnerError(err)
			}
			if err := db.Ping(); err != nil {
				return nil, errors.NewError(
					errors.ErrorCodeValidationFailed,
					"failed to ping production database",
					errors.SeverityCritical,
					errors.CategoryValidation,
				).WithInnerError(err)
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
	ruleEngine, err := NewSquasherRuleEngine(SafetyLevel(cfg.SafetyLevel), cfg.Rules.Overrides)
	if err != nil {
		return nil, err
	}

	// Version stamped into provenance metadata; never a hardcoded release.
	version := strings.TrimSpace(engineCfg.Version)
	if version == "" {
		version = "dev"
	}

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

		// Backups belong under the user's output area, never a shared temp dir.
		backupDir := engineCfg.BackupPath
		if backupDir == "" {
			backupDir = filepath.Join(cfg.Output.Directory, ".backups")
		}
		if err := backupGenerator.SetWorkingDirectory(backupDir); err != nil {
			return nil, err
		}
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

	// Initialize validators
	preFlightValidator := validation.NewPreFlightValidator()
	postFlightValidator := validation.NewPostFlightValidator()

	return &Engine{
		// Core components
		config:     cfg,
		version:    version,
		lifecycles: make(map[string]*tracking.ObjectLifecycle),
		warnings:   []string{},
		prodDB:     db,
		ctx:        engineContext,

		// Validators
		preFlightValidator:  preFlightValidator,
		postFlightValidator: postFlightValidator,

		// Enhanced components
		metadataManager:      metaMgr,
		tracker:              tracker,
		dataOperationTracker: dataOperationTracker,
		sqlBuilder:           sqlBuilder,
		ruleEngine:           ruleEngine,
		logger:               logger,

		// Transformation components
		backupGenerator:     backupGenerator,
		rollbackManager:     rollbackManager,
		sqlTransformer:      sqlTransformer,
		rollbackPath:        rollbackPath,
		backupRetentionDays: engineCfg.BackupRetentionDays,

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

		enableCycleDetection:    engineCfg.EnableCycleDetection,
		showCycleDetails:        engineCfg.ShowCycleDetails,
		cycleDetectionDepth:     engineCfg.CycleDetectionDepth,
		excludeDataFromBaseline: engineCfg.ExcludeDataFromBaseline,
	}, nil
}

// NewSquasherRuleEngine creates a rule engine for the given safety level.
//
// The rule sets form a strict subset ladder (stricter level = fewer rules):
//
//	paranoid ⊂ conservative ⊂ standard ⊂ aggressive
//
// ruleOverrides force-enables (true) or force-disables (false) specific named
// rules relative to that baseline. Names must match the consolidation rule
// registry (see ValidateRuleOverrides); unknown names are rejected. A nil map
// applies the baseline unchanged.
//
// Invalid safety levels are rejected with an error instead of silently
// producing an engine with no consolidation rules.
func NewSquasherRuleEngine(safetyLevel SafetyLevel, ruleOverrides map[string]bool) (*consolidation.ConsolidationRuleEngine, error) {
	if err := ValidateSafetyLevel(safetyLevel); err != nil {
		return nil, err
	}

	// Build the safety-level baseline ladder as an ordered list first, so rule
	// overrides can be applied before the rules are frozen into the engine.

	// Always add external dependency filter (reduces noise)
	ladder := []consolidation.ConsolidationRule{
		consolidation.NewExternalDependencyFilterRule(),
	}

	// Paranoid base set: normalization/deduplication rules that never change
	// the effective schema (DO-block unwrapping, duplicate elimination).
	ladder = append(ladder,
		&consolidation.DOBlockEnumTypeRule{},
		&consolidation.DOBlockAlterTableRule{},
		&consolidation.EnumDeduplicationRule{},
		&consolidation.PublicationDeduplicationRule{},
	)

	// Conservative adds structural consolidation of CREATE/ALTER histories.
	if safetyLevel == Conservative || safetyLevel == Standard || safetyLevel == Aggressive {
		ladder = append(ladder,
			&consolidation.MultipleCreateConsolidationRule{},
			&consolidation.CreateAlterConsolidationRule{},
			&consolidation.ColumnEvolutionRule{},
			&consolidation.ConditionalSchemaRule{},
			&consolidation.AdvancedColumnLifecycleRule{},
		)
	}

	// Standard adds cycle elimination, RLS consolidation and transaction boundaries.
	if safetyLevel == Standard || safetyLevel == Aggressive {
		ladder = append(ladder,
			&consolidation.DropCreateCycleRule{}, // Now handles VIEWs
			&consolidation.RLSConsolidationRule{},
			&consolidation.TransactionBoundaryRule{},
		)
	}

	// Aggressive adds function deduplication.
	if safetyLevel == Aggressive {
		ladder = append(ladder, &consolidation.FunctionDeduplicationRule{})
	}

	// NOTE: DeadCodeRemovalRule was removed entirely. It claimed database-backed
	// usage analysis but ran on static analysis alone (the ConsolidationEngine
	// interface exposes no DB handle). Dead-code removal will only be
	// reintroduced once a real database-statistics check exists.

	ladder, includeErrorRecovery, err := applyRuleOverrides(ladder, ruleOverrides)
	if err != nil {
		return nil, err
	}

	engine := consolidation.NewConsolidationRuleEngine()
	for _, rule := range ladder {
		engine.AddRule(rule)
	}

	// Add error recovery as the last rule to catch any failures from primary
	// consolidation rules. Only Aggressive uses aggressive recovery; stricter
	// levels (including Paranoid) always recover conservatively.
	if includeErrorRecovery {
		recoveryMode := "conservative"
		if safetyLevel == Aggressive {
			recoveryMode = "aggressive"
		}
		engine.AddRule(consolidation.NewErrorRecoveryRule(3, recoveryMode, true))
	}

	return engine, nil
}

// errorRecoveryRuleName is the registry name of the always-last recovery rule;
// it is handled specially because it must remain the final ladder entry.
const errorRecoveryRuleName = "error_recovery"

// ValidateRuleOverrides checks that every override key names a rule known to
// the consolidation rule registry (the same catalog served by
// pkg/rules.GetRegistry). Unknown names are an error, never silently ignored.
func ValidateRuleOverrides(overrides map[string]bool) error {
	if len(overrides) == 0 {
		return nil
	}

	registry := consolidation.GetRegistry()
	// Ensure the core catalog is present even when no rule engine has been
	// constructed yet in this process (registration is idempotent).
	if err := consolidation.RegisterCoreRules(registry); err != nil {
		return err
	}

	var unknown []string
	for name := range overrides {
		if _, err := registry.GetRule(name); err != nil {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)

	valid := make([]string, 0)
	for _, registered := range registry.GetAllRules() {
		valid = append(valid, registered.Metadata.Name)
	}
	sort.Strings(valid)

	return errors.NewError(
		errors.ErrorCodeValidationFailed,
		fmt.Sprintf("unknown consolidation rule name(s): %s", strings.Join(unknown, ", ")),
		errors.SeverityError,
		errors.CategoryValidation,
	).WithSuggestion(fmt.Sprintf("Valid rule names: %s", strings.Join(valid, ", ")))
}

// applyRuleOverrides applies per-rule enable/disable overrides to a safety
// baseline ladder. Rule identity is resolved through the consolidation rule
// registry: a disable removes every ladder rule of the named rule's dynamic
// type, an enable appends the registry's rule instance when the type is not
// already present. The returned bool reports whether the error-recovery rule
// (always appended last by the caller) should be included.
func applyRuleOverrides(
	ladder []consolidation.ConsolidationRule,
	overrides map[string]bool,
) ([]consolidation.ConsolidationRule, bool, error) {
	includeErrorRecovery := true
	if len(overrides) == 0 {
		return ladder, includeErrorRecovery, nil
	}

	if err := ValidateRuleOverrides(overrides); err != nil {
		return nil, false, err
	}

	registry := consolidation.GetRegistry()

	// Deterministic application order regardless of map iteration.
	names := slices.Sorted(maps.Keys(overrides))
	for _, name := range names {
		enabled := overrides[name]

		if name == errorRecoveryRuleName {
			// Error recovery is appended after all other rules; overriding its
			// position would break the recovery-last invariant, so only its
			// presence is configurable.
			includeErrorRecovery = enabled
			continue
		}

		registered, err := registry.GetRule(name)
		if err != nil {
			return nil, false, err // unreachable after validation; keep fail-closed
		}
		ruleType := reflect.TypeOf(registered.Rule)

		if enabled {
			present := false
			for _, rule := range ladder {
				if reflect.TypeOf(rule) == ruleType {
					present = true
					break
				}
			}
			if !present {
				ladder = append(ladder, registered.Rule)
			}
			continue
		}

		filtered := ladder[:0]
		for _, rule := range ladder {
			if reflect.TypeOf(rule) != ruleType {
				filtered = append(filtered, rule)
			}
		}
		ladder = filtered
	}

	return ladder, includeErrorRecovery, nil
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
// GetConfig returns the configuration for use by consolidation rules
func (e *Engine) GetConfig() *config.Config {
	return e.config
}

// GetAuthCompatibilitySQL returns the auth compatibility SQL for Docker validation
func (e *Engine) GetAuthCompatibilitySQL() string {
	return e.authCompatibilitySQL
}

// Close gracefully shuts down the engine
func (e *Engine) Close() error {
	var errs []error
	if e.prodDB != nil {
		if err := e.prodDB.Close(); err != nil {
			e.logger.Error("Failed to close production database connection: %v", err)
			errs = append(errs, err)
		}
	}
	if e.enableStreaming && e.streamingTracker != nil {
		if err := e.streamingTracker.Stop(); err != nil {
			e.logger.Info("Warning: failed to stop streaming tracker: %v", err)
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("engine close errors: %v", errs)
	}
	return nil
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
// Squash processes migrations using enhanced patterns and modern PostgreSQL conventions
func (e *Engine) Squash(migrations map[int]string) (*SquashResult, error) {
	ctx := e.ctx
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	startTime := time.Now()

	e.logger.Info("Starting enhanced squashing process with %d migration files", len(migrations))

	// Initialize stats
	e.stats.TotalMigrations = int64(len(migrations))
	e.stats.MigrationsProcessed = 0

	// Invoke progress callback at start
	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(0, e.stats.TotalMigrations, "Initializing")
	}

	// PHASE 0: Initialize Plugin System and analyze extensions.
	// This must happen BEFORE parsing to enable plugin enrichment,
	// and runs for both regular and streaming modes.
	extAnalysis := e.prepareMigrationEnvironment(ctx, migrations)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Phase 0.5: Pre-Flight Validation (Static Analysis)
	if e.preFlightValidator != nil {
		e.logger.Info("Running pre-flight validation on %d migrations...", len(migrations))
		if err := e.runPreFlightValidation(ctx, migrations); err != nil {
			// For now, we log warnings but don't abort unless strict mode is enabled (future)
			e.logger.Warn("Pre-flight validation found issues: %v", err)
			e.warnings = append(e.warnings, fmt.Sprintf("Pre-flight validation: %v", err))
		}
	}

	// Use streaming approach if enabled
	if e.enableStreaming {
		return e.SquashStreaming(migrations)
	}

	// Pre-processing: Generate backup if enabled
	if e.backupGenerator != nil && e.prodDB != nil {
		e.logger.Info("Generating backup before squashing...")
		// Use database connection string from config
		backupResult, err := e.backupGenerator.GeneratePreMigrationBackup(ctx, e.config.ProdDBDSN)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeBackupGenerationFailed,
				"failed to generate backup",
				errors.SeverityError,
				errors.CategoryBackup,
			).WithInnerError(err)
		}
		e.logger.Info("Backup created at: %s", backupResult.BackupPath)
		e.warnings = append(e.warnings, fmt.Sprintf("Backup created: %s", backupResult.BackupPath))

		// Apply retention policy inside the dedicated backup directory only.
		if e.backupRetentionDays > 0 {
			maxAge := time.Duration(e.backupRetentionDays) * 24 * time.Hour
			if cleanupErr := e.backupGenerator.CleanupOldBackups(maxAge, 0); cleanupErr != nil {
				e.logger.Warn("Failed to clean up old backups: %v", cleanupErr)
				e.warnings = append(e.warnings, fmt.Sprintf("Backup retention cleanup failed: %v", cleanupErr))
			}
		}
	}

	// Initialize rollback manager if enabled
	if e.rollbackManager != nil {
		// Parse migrations to extract statements for rollback planning.
		// A migration that fails to parse would leave the rollback plan silently
		// incomplete, so surface the failure instead of skipping the file.
		var allStatements []types.Statement
		for id, migrationSQL := range migrations {
			filename := fmt.Sprintf("migration_%d.sql", id)
			migration, err := parser.ParseMigrationWithContext(ctx, migrationSQL, filename)
			if err != nil {
				return nil, errors.NewError(
					errors.ErrorCodeRollbackGenerationFailed,
					fmt.Sprintf("rollback plan requested but migration %d could not be parsed", id),
					errors.SeverityError,
					errors.CategoryRollback,
				).WithInnerError(err).WithSuggestion("Fix the migration SQL or rerun without --rollback")
			}
			allStatements = append(allStatements, migration.Statements...)
		}

		// Create a rollback plan. Rollback was explicitly requested, so a plan
		// that cannot be created is a hard failure, not a warning.
		planName := fmt.Sprintf("squash_%d", startTime.Unix())
		plan, err := e.rollbackManager.CreateRollbackPlan(ctx, planName, allStatements)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeRollbackGenerationFailed,
				"failed to create rollback plan",
				errors.SeverityError,
				errors.CategoryRollback,
			).WithInnerError(err).WithSuggestion("Check rollback path permissions or rerun without --rollback")
		}
		planPath := filepath.Join(e.rollbackPath, "rollback_plans", fmt.Sprintf("%s.json", plan.ID))
		e.logger.Info("Rollback plan '%s' created successfully at %s", planName, planPath)
		e.warnings = append(e.warnings, fmt.Sprintf("Rollback script generated: %s (%d operations)", planPath, len(plan.Scripts)))
	}

	// Phase 1: Parse and build object lifecycles
	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(0, e.stats.TotalMigrations, "Parsing migrations")
	}

	if err := e.parseAndTrackMigrations(ctx, migrations); err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeAnalysisError,
			"parse and track migrations",
			errors.SeverityError,
			errors.CategoryParsing,
		).WithInnerError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	e.stats.MigrationsProcessed = e.stats.TotalMigrations
	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(e.stats.MigrationsProcessed, e.stats.TotalMigrations, "Analyzing dependencies")
	}

	// Phase 2: Analyze dependencies and detect issues
	if err := e.analyzeDependenciesAndRisks(ctx); err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeDependencyError,
			"analyze dependencies",
			errors.SeverityError,
			errors.CategoryDependency,
		).WithInnerError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Phase 3: Apply consolidation rules
	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(e.stats.MigrationsProcessed, e.stats.TotalMigrations, "Applying consolidation rules")
	}

	consolidatedObjects, err := e.applyConsolidationRules(ctx)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"apply consolidation rules",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Phase 4: Generate optimized SQL with modern formatting
	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(e.stats.MigrationsProcessed, e.stats.TotalMigrations, "Generating SQL")
	}

	finalSQL, err := e.generateOptimizedSQL(ctx, consolidatedObjects)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			"generate optimized SQL",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Phase 4.5: Apply SQL transformations if enabled.
	// Transformation was explicitly enabled; shipping untransformed SQL after a
	// silent failure would misrepresent the output, so fail the squash instead.
	if e.sqlTransformer != nil {
		e.logger.Info("Applying SQL transformations...")
		transformResult, err := e.sqlTransformer.Transform(ctx, finalSQL)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeSQLGenerationFailed,
				"SQL transformation failed",
				errors.SeverityError,
				errors.CategoryTransformation,
			).WithInnerError(err).WithSuggestion("Rerun with --transform=false to skip SQL modernization")
		}
		finalSQL = transformResult.TransformedSQL
		if len(transformResult.Transformations) > 0 {
			e.logger.Info("Applied %d SQL transformations", len(transformResult.Transformations))
			for _, tr := range transformResult.Transformations {
				e.warnings = append(e.warnings, fmt.Sprintf("Transformation: %s", tr.Description))
			}
		}
	}

	// Phase 4.6: Post-Flight Validation
	if e.postFlightValidator != nil {
		e.logger.Info("Running post-flight validation on squashed SQL...")
		violations, err := e.runPostFlightValidation(ctx, finalSQL)
		if err != nil {
			e.logger.Warn("Failed to run post-flight validation: %v", err)
		} else if len(violations) > 0 {
			for _, v := range violations {
				msg := fmt.Sprintf("Post-flight Issue [%s]: %s", v.Code, v.Message)
				e.warnings = append(e.warnings, msg)
				e.logger.Warn("%s", msg)
			}
		}
	}

	// Phase 5: Perform database validation if in paranoid mode.
	// This validation is the defining feature of the paranoid level; a failed
	// check must fail the squash rather than degrade to a warning.
	if e.config.SafetyLevel == string(Paranoid) && e.prodDB != nil {
		if err := e.validateAgainstDatabase(ctx, finalSQL); err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeValidationFailed,
				"paranoid database validation failed",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}
	}

	processingTime := time.Since(startTime)
	e.stats.ProcessingTime = processingTime
	e.logger.Info("Enhanced squashing completed in %v", processingTime)

	// Final progress callback
	if e.progressCb != nil && e.enableProgressTrack {
		e.progressCb(e.stats.MigrationsProcessed, e.stats.TotalMigrations, "Completed")
	}

	e.logger.Info("Squashing completed")

	// Construct result
	return &SquashResult{
		BaselineSQL:          finalSQL,
		Warnings:             e.warnings,
		AuthCompatibilitySQL: e.GetAuthCompatibilitySQL(),
		Extensions:           extAnalysis.RequiredExtensions,
	}, nil
}

// SquashWithSeparateFiles performs squashing and returns separate files for DDL and data operations
func (e *Engine) SquashWithSeparateFiles(migrations map[int]string) (*SquashResult, error) {
	// Configure engine to exclude data from baseline for this operation
	// We want the baseline to be DDL-only, and data to be in the separate file
	originalExclude := e.excludeDataFromBaseline
	e.excludeDataFromBaseline = true
	defer func() { e.excludeDataFromBaseline = originalExclude }()

	// Perform regular squashing to get baseline DDL
	squashRes, err := e.Squash(migrations)
	if err != nil {
		return nil, err
	}
	if err := e.ctx.Err(); err != nil {
		return nil, err
	}
	baselineSQL := squashRes.BaselineSQL
	warnings := squashRes.Warnings

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

	// Create provenance tracker. The version comes from the engine config
	// (callers stamp their release metadata); the fallback is "dev".
	provenance := NewProvenanceTracker(
		e.version,
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
		BaselineSQL:          baselineSQL,
		DataOperationsSQL:    dataSQL,
		Warnings:             warnings,
		ProvenanceMap:        provenance.GetSquashMap(),
		Extensions:           analysis.RequiredExtensions,
		AuthCompatibilitySQL: analysis.AuthCompatibilitySQL,
	}

	return result, nil
}

// SquashStreaming processes migrations with streaming approach for large datasets
// SquashStreaming processes migrations with streaming approach for large datasets
func (e *Engine) SquashStreaming(migrations map[int]string) (*SquashResult, error) {
	if !e.enableStreaming {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"streaming not enabled for this engine instance",
			errors.SeverityError,
			errors.CategoryConsolidation,
		)
	}

	startTime := time.Now()
	ctx := e.ctx
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	e.updatePhase("Initializing")
	e.stats.TotalMigrations = int64(len(migrations))

	e.logger.Info("Starting streaming squash process with %d migrations", len(migrations))

	// Streaming does not execute the SQL transformation or post-flight
	// validation phases. Transformation is rejected at construction time
	// (streaming + EnableTransformation is an error); post-flight validation
	// has no user-facing toggle, so its absence is surfaced as a result
	// warning instead of being silently skipped.
	e.warnings = append(e.warnings,
		"Streaming mode: post-flight validation is not executed (run 'pgsquash lint' or a non-streaming squash to validate the output)")

	// Phase 0: Plugin initialization + extension analysis (no-op when already
	// performed by Squash() before delegating to the streaming path).
	extAnalysis := e.prepareMigrationEnvironment(ctx, migrations)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Phase 1: Stream process migrations using batching
	e.updatePhase("Parsing and Tracking")
	if err := e.streamProcessMigrations(ctx, migrations); err != nil {
		return nil, errors.NewError(
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
		return nil, errors.NewError(
			errors.ErrorCodeDependencyError,
			"analyze dependencies",
			errors.SeverityError,
			errors.CategoryDependency,
		).WithInnerError(err)
	}

	e.updatePhase("Applying Consolidations")
	consolidatedObjects, err := e.applyConsolidationRules(ctx)
	if err != nil {
		return nil, errors.NewError(
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
		return nil, errors.NewError(
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

	return &SquashResult{
		BaselineSQL:          finalSQL,
		Warnings:             e.warnings,
		AuthCompatibilitySQL: e.GetAuthCompatibilitySQL(),
		Extensions:           extAnalysis.RequiredExtensions,
	}, nil
}

// SquashFromDirectory processes migrations from a directory using streaming
func (e *Engine) SquashFromDirectory(dir string) (*SquashResult, error) {
	if !e.enableStreaming {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"streaming not enabled for this engine instance",
			errors.SeverityError,
			errors.CategoryConsolidation,
		)
	}

	startTime := time.Now()

	ctx := e.ctx
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	e.updatePhase("Initializing")
	e.logger.Info("Starting streaming squash process from directory: %s", dir)

	// Same contract as SquashStreaming: post-flight validation does not run in
	// streaming mode, so record that in the result instead of skipping silently.
	e.warnings = append(e.warnings,
		"Streaming mode: post-flight validation is not executed (run 'pgsquash lint' or a non-streaming squash to validate the output)")

	// Phase 0: Plugin initialization + extension analysis. Directory streaming
	// must run the same plugin enrichment and extension detection as the
	// in-memory paths, so load file contents once for analysis.
	migrationContents, err := loadMigrationContentsFromDir(dir)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("failed to read migration files from directory %s", dir),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}
	extAnalysis := e.prepareMigrationEnvironment(ctx, migrationContents)
	migrationContents = nil // release for GC before streaming begins

	// Phase 1: Stream parse and track migrations
	e.updatePhase("Parsing and Tracking")
	if err := e.streamParseAndTrack(ctx, dir); err != nil {
		return nil, errors.NewError(
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
		return nil, errors.NewError(
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
		return nil, errors.NewError(
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
		return nil, errors.NewError(
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
	return &SquashResult{
		BaselineSQL:          finalSQL,
		Warnings:             e.warnings,
		AuthCompatibilitySQL: e.GetAuthCompatibilitySQL(),
		Extensions:           extAnalysis.RequiredExtensions,
	}, nil
}

// loadMigrationContentsFromDir reads the contents of all .sql files in a
// directory (sorted by filename) keyed by their sequence position. It is used
// by directory streaming to run plugin detection and extension analysis.
func loadMigrationContentsFromDir(dir string) (map[int]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var files []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".sql") {
			continue
		}
		files = append(files, entry.Name())
	}
	sort.Strings(files)

	contents := make(map[int]string, len(files))
	for i, name := range files {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		contents[i] = string(data)
	}

	return contents, nil
}

// prepareMigrationEnvironment initializes plugins and analyzes required
// extensions exactly once per engine instance. It is shared by the regular and
// streaming squash paths so streaming runs get the same plugin enrichment and
// extension metadata as regular runs.
func (e *Engine) prepareMigrationEnvironment(ctx context.Context, migrations map[int]string) *ExtensionAnalysis {
	if e.envPrepared {
		return e.extAnalysis
	}
	e.envPrepared = true

	// Initialize plugin system. Failures stay warnings (plugins are optional),
	// but they are recorded in the result warnings list.
	if err := e.initializePlugins(ctx, migrations); err != nil {
		e.logger.Warn("Plugin initialization failed: %v", err)
		e.warnings = append(e.warnings, fmt.Sprintf("Plugin initialization warning: %v", err))
	}

	// Analyze extensions required for validation.
	extDetector := NewExtensionDetector()
	extAnalysis := extDetector.AnalyzeMigrations(migrations)
	if len(extAnalysis.RequiredExtensions) > 0 {
		e.logger.Info("Detected extensions: %v", extAnalysis.RequiredExtensions)
		e.logger.Info("Recommended Docker image: %s", extAnalysis.RecommendedDockerBase)
		e.warnings = append(e.warnings, fmt.Sprintf("Required extensions: %v", extAnalysis.RequiredExtensions))
	}

	// Store auth compatibility SQL for validation.
	if extAnalysis.AuthCompatibilitySQL != "" {
		e.authCompatibilitySQL = extAnalysis.AuthCompatibilitySQL
		e.logger.Info("Generated auth compatibility layer for: %s", extAnalysis.AuthService)
	}

	e.extAnalysis = extAnalysis
	return extAnalysis
}

// initializePlugins discovers and initializes plugins from migrations
func (e *Engine) initializePlugins(ctx context.Context, migrations map[int]string) error {
	e.logger.Info("Discovering and initializing plugins...")

	// Convert migrations map to Migration slice for plugin detection
	var migrationSlice []*types.Migration
	for id, content := range migrations {
		migration, err := parser.ParseMigration(content, fmt.Sprintf("migration_%d", id))
		if err != nil {
			// If parsing fails, skip this migration for plugin detection
			continue
		}
		migrationSlice = append(migrationSlice, migration)
	}

	// Call plugin registry to discover and initialize
	registry := plugins.GlobalRegistry()
	if err := registry.DiscoverAndInitialize(ctx, migrationSlice, nil); err != nil {
		return err
	}

	// Log active plugins
	activePlugins := registry.ActivePlugins()
	if len(activePlugins) > 0 {
		pluginNames := make([]string, len(activePlugins))
		for i, p := range activePlugins {
			pluginNames[i] = p.Name()
		}
		e.logger.Info("Activated plugins: %v", pluginNames)
	} else {
		e.logger.Info("No plugins detected/activated")
	}

	return nil
}

// parseAndTrackMigrations parses migrations and builds object lifecycles
func (e *Engine) parseAndTrackMigrations(ctx context.Context, migrations map[int]string) error {
	e.logger.Info("Parsing %d migration files with enhanced tracking", len(migrations))

	var sequences []int
	for seq := range migrations {
		sequences = append(sequences, seq)
	}
	sort.Ints(sequences)

	for _, sequence := range sequences {
		migrationContent := migrations[sequence]
		migration, err := parser.ParseMigration(migrationContent, fmt.Sprintf("migration_%05d", sequence))
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
			if stmt.IsDataOp || stmt.ObjectType == types.TypeDoBlock {
				trackedStmt := stmt
				if !trackedStmt.IsDataOp {
					// Data-category DO blocks are non-idempotent mutation scripts and must
					// be emitted in 010_data.sql instead of baseline DDL.
					trackedStmt.IsDataOp = true
				}

				// Analyze pragmas for data operations too
				sa := parser.NewStatementAnalyzer(e.config.PostgreSQLFeatures.TargetVersion)
				sa.AnalyzeStatement(&trackedStmt)
				sa.AnalyzePragmas(&trackedStmt)

				if err := e.dataOperationTracker.AddOperation(trackedStmt, sequence, stmtIndex); err != nil {
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

	// Build a set of terminally dropped tables so dependent objects (indexes,
	// policies, triggers, etc.) can also be skipped. PostgreSQL cascades these
	// drops implicitly, but lifecycle tracking may still contain create events.
	droppedTables := make(map[string]struct{})
	for _, lifecycle := range e.lifecycles {
		if lifecycle.Type != types.TypeTable || len(lifecycle.History) == 0 {
			continue
		}
		if lifecycle.History[len(lifecycle.History)-1].Operation != types.OpDrop {
			continue
		}

		tableName := strings.ToLower(strings.TrimSpace(lifecycle.Name))
		if tableName == "" {
			continue
		}
		droppedTables[tableName] = struct{}{}
		if !strings.Contains(tableName, ".") {
			droppedTables["public."+tableName] = struct{}{}
		}
	}

	for key, lifecycle := range e.lifecycles {
		// Objects that end in DROP must not appear in baseline output.
		// Guard at engine level so no consolidation rule can accidentally
		// resurrect dropped objects (e.g., CREATE+ALTER rules on eventually-dropped tables).
		if len(lifecycle.History) > 0 && lifecycle.History[len(lifecycle.History)-1].Operation == types.OpDrop {
			e.logger.Info("Skipping dropped object lifecycle: %s", key)
			continue
		}

		// Also skip objects that depend on terminally dropped tables.
		// Example: DROP TABLE ... CASCADE removes indexes implicitly; if we keep
		// index CREATE statements, baseline replay fails with "relation does not exist".
		if lifecycleDependsOnDroppedTables(lifecycle, droppedTables) {
			e.logger.Info("Skipping object lifecycle %s because it depends on a dropped table", key)
			continue
		}

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
		} else if result == nil && (lifecycle.Type == types.TypeData || lifecycle.Type == types.TypeDoBlock) {
			// Force consolidation for Data operations
			// CRITICAL: We must accumulate SQL from ALL history events, not just the final state
			// Data operations across multiple migrations (like profiles inserts in 04 and 54)
			// are grouped into a single lifecycle object. Using GetFinalState() only returns the last one.
			var allSQL strings.Builder
			var allStmts []types.Statement

			for i, event := range lifecycle.History {
				if i > 0 {
					allSQL.WriteString("\n")
				}
				allSQL.WriteString(event.Statement.SQL)
				allStmts = append(allStmts, event.Statement)
			}

			if allSQL.Len() > 0 {
				result = &tracking.ConsolidationResult{
					OriginalStatements: allStmts,
					ConsolidatedSQL:    allSQL.String(),
					Optimizations:      []string{"data_aggregation"},
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

			// Consolidation rules have handled all ALTER statement logic
			// SeparateAlterRule ensures ALTERs that must be separate are properly handled
			// No need for engine to duplicate this logic

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

			// BUGFIX: For DROP POLICY statements, reconstruct SQL using builder to ensure ON clause is included
			if finalState.Operation == types.OpDrop && finalState.ObjectType == types.TypePolicy {
				sqlBuilder := builder.NewSQLBuilder(builder.DefaultBuildOptions())
				consolidatedSQL = sqlBuilder.FromStatement(*finalState).String()
			}

			// Ensure SQL always ends with semicolon before appending separate statements
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

func lifecycleDependsOnDroppedTables(lifecycle *tracking.ObjectLifecycle, droppedTables map[string]struct{}) bool {
	if len(droppedTables) == 0 || lifecycle == nil {
		return false
	}

	matchesDroppedTable := func(candidate string) bool {
		c := strings.ToLower(strings.TrimSpace(candidate))
		if c == "" {
			return false
		}

		if _, ok := droppedTables[c]; ok {
			return true
		}

		if !strings.Contains(c, ".") {
			_, ok := droppedTables["public."+c]
			return ok
		}

		return false
	}

	// Direct object target check (important for data operations on dropped tables).
	if matchesDroppedTable(lifecycle.Name) {
		return true
	}
	for _, event := range lifecycle.History {
		if matchesDroppedTable(event.Statement.ObjectName) {
			return true
		}
	}

	// SQL text fallback for statements where dependency extraction is incomplete
	// (e.g., UPDATE ... FROM dropped_table inside data lifecycles).
	referencesDroppedTableInSQL := func(sqlText string) bool {
		sqlLower := strings.ToLower(sqlText)
		for dropped := range droppedTables {
			d := strings.ToLower(strings.TrimSpace(dropped))
			if d == "" {
				continue
			}

			bare := d
			if idx := strings.LastIndex(d, "."); idx >= 0 && idx+1 < len(d) {
				bare = d[idx+1:]
			}

			patterns := []string{
				"from " + bare,
				"join " + bare,
				"update " + bare,
				"into " + bare,
				"from " + d,
				"join " + d,
				"update " + d,
				"into " + d,
				"on " + bare + "(",
				"on " + d + "(",
			}

			for _, p := range patterns {
				if strings.Contains(sqlLower, p) {
					return true
				}
			}
		}
		return false
	}

	for _, event := range lifecycle.History {
		if referencesDroppedTableInSQL(event.Statement.SQL) {
			return true
		}
	}

	normalizeDependency := func(dep string) string {
		d := strings.TrimSpace(dep)
		if d == "" {
			return ""
		}
		if idx := strings.Index(d, ":"); idx >= 0 && idx+1 < len(d) {
			d = d[idx+1:]
		}
		return strings.TrimSpace(strings.Trim(d, `"`))
	}

	// Primary check: statement dependency list.
	for _, event := range lifecycle.History {
		for _, dep := range event.Statement.Dependencies {
			if matchesDroppedTable(normalizeDependency(dep)) {
				return true
			}
		}
	}

	// Fallback for index lifecycles: parse `ON <table>` directly from SQL.
	// Some index statements may lack dependency metadata in edge parsing paths.
	if lifecycle.Type == types.TypeIndex {
		for _, event := range lifecycle.History {
			parseTree := event.Statement.ParseTree
			if parseTree == nil {
				var err error
				parseTree, err = pg_query.Parse(event.Statement.SQL)
				if err != nil {
					continue
				}
			}

			if parseTree == nil {
				continue
			}

			for _, rawStmt := range parseTree.Stmts {
				if rawStmt == nil || rawStmt.Stmt == nil {
					continue
				}

				indexStmt := rawStmt.Stmt.GetIndexStmt()
				if indexStmt == nil || indexStmt.Relation == nil {
					continue
				}

				tableRef := strings.TrimSpace(indexStmt.Relation.Relname)
				schemaRef := strings.TrimSpace(indexStmt.Relation.Schemaname)

				if schemaRef != "" && matchesDroppedTable(schemaRef+"."+tableRef) {
					return true
				}

				if matchesDroppedTable(tableRef) {
					return true
				}
			}
		}
	}

	// Policy object names can encode table context as schema.table.policy.
	if lifecycle.Type == types.TypePolicy {
		parts := strings.Split(strings.ToLower(strings.TrimSpace(lifecycle.Name)), ".")
		if len(parts) >= 3 {
			tableRef := strings.Join(parts[:2], ".")
			if matchesDroppedTable(tableRef) {
				return true
			}
		}
	}

	return false
}

// generateOptimizedSQL generates the final optimized SQL with modern formatting
func (e *Engine) generateOptimizedSQL(ctx context.Context, consolidatedObjects map[string]*tracking.ConsolidationResult) (string, error) {
	e.logger.Info("Generating optimized SQL for %d consolidated objects", len(consolidatedObjects))

	e.sqlBuilder.Reset()

	// Track terminally dropped tables so fallback emission does not reintroduce
	// dependent objects (e.g., indexes removed by DROP TABLE ... CASCADE).
	droppedTablesForGeneration := make(map[string]struct{})
	for _, lifecycle := range e.lifecycles {
		if lifecycle == nil || lifecycle.Type != types.TypeTable || len(lifecycle.History) == 0 {
			continue
		}
		if lifecycle.History[len(lifecycle.History)-1].Operation != types.OpDrop {
			continue
		}

		tableName := strings.ToLower(strings.TrimSpace(lifecycle.Name))
		if tableName == "" {
			continue
		}
		droppedTablesForGeneration[tableName] = struct{}{}
		if !strings.Contains(tableName, ".") {
			droppedTablesForGeneration["public."+tableName] = struct{}{}
		}
	}

	// Add header comment
	e.sqlBuilder.Comment("Generated by pgsquash with modern PostgreSQL patterns")
	e.sqlBuilder.Comment(fmt.Sprintf("Safety level: %s", e.config.SafetyLevel))
	e.sqlBuilder.Comment(fmt.Sprintf("Generated at: %s", time.Now().Format(time.RFC3339)))
	e.sqlBuilder.NL()

	// Inject auth compatibility layer if needed
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

	// Detect role references in policies and grants
	// The previous detection only looked for "role authenticated" but policies use "TO authenticated"
	// This caused missing role creation in output, breaking deployments
	needsRoleCreation := false
	referencedRoles := make(map[string]bool) // Track which roles are used

	for _, result := range consolidatedObjects {
		sqlLower := strings.ToLower(result.ConsolidatedSQL)

		// Detect role references in policies: "TO authenticated", "TO anon", "TO service_role"
		if strings.Contains(sqlLower, "to authenticated") {
			needsRoleCreation = true
			referencedRoles["authenticated"] = true
		}
		if strings.Contains(sqlLower, "to anon") {
			needsRoleCreation = true
			referencedRoles["anon"] = true
		}
		if strings.Contains(sqlLower, "to service_role") {
			needsRoleCreation = true
			referencedRoles["service_role"] = true
		}

		// Also check for GRANT/REVOKE statements that reference roles
		if strings.Contains(sqlLower, "grant") && (strings.Contains(sqlLower, "to authenticated") || strings.Contains(sqlLower, "to anon") || strings.Contains(sqlLower, "to service_role")) {
			needsRoleCreation = true
		}
	}

	// If we need roles, inject them BEFORE everything else
	if needsRoleCreation {
		e.logger.Info("☑ Detected %d PostgreSQL role references in policies/grants - injecting role creation", len(referencedRoles))
		e.sqlBuilder.NL().Comment("=== POSTGRESQL ROLES ===")
		e.sqlBuilder.Comment("Roles must be created before policies that reference them")
		e.sqlBuilder.NL()

		// Generate idempotent role creation SQL
		e.sqlBuilder.Statement(`DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'anon') THEN
    CREATE ROLE anon NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'authenticated') THEN
    CREATE ROLE authenticated NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'service_role') THEN
    CREATE ROLE service_role NOLOGIN;
  END IF;
END
$$`)
		e.sqlBuilder.NL().NL()
	}

	// Inject storage schema when storage.objects/buckets are referenced
	// Detect references to storage.objects or storage.buckets and inject schema creation
	needsStorageSchema := false
	for _, result := range consolidatedObjects {
		sqlLower := strings.ToLower(result.ConsolidatedSQL)
		if strings.Contains(sqlLower, "storage.objects") || strings.Contains(sqlLower, "storage.buckets") {
			needsStorageSchema = true
			break
		}
	}

	if needsStorageSchema {
		e.logger.Info("☑ Detected storage schema references (storage.objects/buckets) - injecting schema creation")
		e.sqlBuilder.NL().Comment("=== STORAGE SCHEMA ===")
		e.sqlBuilder.Comment("Storage schema must exist before policies that reference storage.objects/buckets")
		e.sqlBuilder.NL()
		e.sqlBuilder.Statement("CREATE SCHEMA IF NOT EXISTS storage;")
		e.sqlBuilder.NL().NL()
	}

	// Group by category for organized output - CRITICAL: Order must ensure dependencies are created first
	// Standard PostgreSQL DDL order: Extensions -> Types/Functions/Tables -> Constraints -> Triggers -> etc.
	// Functions are in CategoryFoundation and ordered BEFORE tables (CHECK constraints may reference functions)
	categories := []types.Category{
		types.CategoryExtensions,  // 1. Extensions first (CREATE EXTENSION)
		types.CategoryFoundation,  // 2. Types, functions, tables, views, sequences - functions before tables for CHECK constraints
		types.CategoryConstraints, // 3. Constraints (ALTER TABLE ADD CONSTRAINT)
		types.CategoryIndexes,     // 4. Indexes (CREATE INDEX)
		types.CategoryTriggers,    // 5. Triggers (CREATE TRIGGER)
		types.CategorySecurity,    // 6. RLS Policies (CREATE POLICY) (Before comments)
		types.CategoryComments,    // 7. Comments (COMMENT ON)
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

	// Some DROP POLICY statements are occasionally mis-categorized into the foundation bucket
	// by upstream consolidation rules. Running them before tables exist causes failures because
	// PostgreSQL requires the target table to be present even with IF EXISTS. Defer them into
	// the security category to guarantee the underlying tables are created first.
	deferredSecurity := make(map[string]*tracking.ConsolidationResult)

	for _, category := range categories {
		categoryObjectsMap := e.getObjectsByCategoryAsMap(consolidatedObjects, category)

		// Move policy drops out of the foundation bucket so they execute with other security objects.
		if category == types.CategoryFoundation && len(categoryObjectsMap) > 0 {
			for key, result := range categoryObjectsMap {
				if lifecycle, exists := e.lifecycles[key]; exists && lifecycle.Type == types.TypePolicy {
					deferredSecurity[key] = result
					delete(categoryObjectsMap, key)
				}
			}
		}

		if category == types.CategorySecurity && len(deferredSecurity) > 0 {
			maps.Copy(categoryObjectsMap, deferredSecurity)
			// Clear after merging to avoid re-adding in future iterations
			deferredSecurity = make(map[string]*tracking.ConsolidationResult)
		}

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

			// DEBUG: Log idx_profiles_coordinates in categorized output
			if len(result.OriginalStatements) > 0 && strings.Contains(result.OriginalStatements[0].ObjectName, "idx_profiles_coordinates") {
				e.logger.Info("[OUTPUT-DEBUG-CATEGORIZED] Category=%s, ObjectName=%s", category, result.OriginalStatements[0].ObjectName)
				e.logger.Info("[OUTPUT-DEBUG-CATEGORIZED] ConsolidatedSQL = %s", sql)
			}

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
			if e.excludeDataFromBaseline {
				if lifecycle, exists := e.lifecycles[key]; exists && (lifecycle.Type == types.TypeData || lifecycle.Type == types.TypeDoBlock) {
					continue
				}
			}
			uncategorizedObjects[key] = result
			uncategorizedCount++
		}
	}

	// Only add header if there are uncategorized objects
	if uncategorizedCount > 0 {
		e.sqlBuilder.NL().Comment("=== UNCATEGORIZED OBJECTS ===").NL()

		// Sort keys for deterministic output order matching migration sequence
		var keys []string
		for key := range uncategorizedObjects {
			keys = append(keys, key)
		}

		sort.Slice(keys, func(i, j int) bool {
			l1 := e.lifecycles[keys[i]]
			l2 := e.lifecycles[keys[j]]

			// Safety check
			if len(l1.History) == 0 || len(l2.History) == 0 {
				return keys[i] < keys[j]
			}

			h1 := l1.History[0]
			h2 := l2.History[0]

			if h1.Migration != h2.Migration {
				return h1.Migration < h2.Migration
			}
			return h1.Sequence < h2.Sequence
		})

		for _, key := range keys {
			result := uncategorizedObjects[key]
			e.sqlBuilder.Statement(result.ConsolidatedSQL)
			e.sqlBuilder.NL().NL()
		}
	}

	// Add any non-consolidated objects through the default preservation path.
	// CRITICAL FIX: Sort objects deterministically based on their first appearance
	// This ensures that data operations (INSERTs) preserve their original order
	// and satisfy foreign key dependencies (e.g. markets before properties/listings)
	var preservedKeys []string
	for key := range e.lifecycles {
		if _, consolidated := consolidatedObjects[key]; !consolidated {
			preservedKeys = append(preservedKeys, key)
		}
	}

	sort.Slice(preservedKeys, func(i, j int) bool {
		lifecycleI := e.lifecycles[preservedKeys[i]]
		lifecycleJ := e.lifecycles[preservedKeys[j]]

		// Safety check for empty history
		if len(lifecycleI.History) == 0 || len(lifecycleJ.History) == 0 {
			return preservedKeys[i] < preservedKeys[j]
		}

		eventI := lifecycleI.History[0]
		eventJ := lifecycleJ.History[0]

		// Sort by migration file (maintains file order)
		if eventI.Migration != eventJ.Migration {
			return eventI.Migration < eventJ.Migration
		}

		// Sort by sequence within migration (maintains statement order)
		if eventI.Sequence != eventJ.Sequence {
			return eventI.Sequence < eventJ.Sequence
		}

		// Fallback to key
		return preservedKeys[i] < preservedKeys[j]
	})

	for _, key := range preservedKeys {
		lifecycle := e.lifecycles[key]

		if lifecycle == nil {
			continue
		}

		if len(lifecycle.History) > 0 && lifecycle.History[len(lifecycle.History)-1].Operation == types.OpDrop {
			continue
		}

		if lifecycleDependsOnDroppedTables(lifecycle, droppedTablesForGeneration) {
			continue
		}

		// Skip data operations if configured to exclude them (e.g. for separate file output)
		if (lifecycle.Type == types.TypeData || lifecycle.Type == types.TypeDoBlock) && e.excludeDataFromBaseline {
			continue
		}

		if lifecycle.GetFinalState() != nil {
			e.sqlBuilder.Comment(fmt.Sprintf("Original object: %s", key))
			e.sqlBuilder.FromStatement(*lifecycle.GetFinalState()).NL().NL()
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

	// DISABLED - Postprocessor corrupts functions
	// The postprocessor calls FixMissingLanguageClauses() which:
	// - Adds "LANGUAGE plpgsql" when LANGUAGE is already at the end
	// - Changes LANGUAGE type from "sql" to "plpgsql"
	// - Moves LANGUAGE from trailing to leading position
	//
	// Since we now preserve original SQL in consolidation rules, we must not postprocess
	// Original SQL from migrations is correct and should be used as-is
	//
	// processor := postprocessing.NewProcessorAST(e.config)
	// finalSQL, err := processor.Apply(finalSQL, enumReplacements)
	// if err != nil {
	// 	return "", err
	// } // DISABLED

	// CRITICAL FIX: Ensure extension ordering is correct (e.g. cube before earthdistance)
	// This is a safe string-manipulation fix that doesn't involve complex AST processing
	// and is required for correct database initialization.
	finalSQL = postprocessing.FixExtensionOrder(finalSQL)

	// CRITICAL FIX: Fix corrupted DROP POLICY statements from pg_query deparser
	// This cleans up "DROP POLICY schema.table.policy ON schema.table" -> "DROP POLICY policy ON schema.table"
	finalSQL = postprocessing.FixDropPolicyDeparseCorruption(finalSQL)

	// Note: enum replacements are not critical for function preservation,
	// so we can safely skip the entire postprocessor

	// The deparser sometimes duplicates "char_" prefix on char_length() function calls
	// This happens during deparsing and can corrupt CHECK constraints
	if strings.Contains(finalSQL, "char_char_length") {
		e.logger.Info("SAFETY NET: Fixing char_char_length corruption in final SQL")
		finalSQL = strings.ReplaceAll(finalSQL, "char_char_length", "char_length")
	}

	// NOTE: Disabled destructive regex-based index pruning.
	// The heuristic parser for CREATE TABLE/INDEX statements produced widespread
	// false positives and removed valid indexes, causing severe schema drift.
	// Index validity should be validated by PostgreSQL during apply/validation,
	// not by lossy regex pruning at generation time.
	e.logger.Info("SAFETY NET: Skipping regex-based index pruning to preserve schema fidelity")

	finalSQL = e.removeOrphanedFunctionStatements(finalSQL)

	return finalSQL, nil
}

// generateDataOperationsSQL generates SQL for data operations (INSERT/UPDATE/DELETE) as a separate file
func (e *Engine) generateDataOperationsSQL() (string, error) {
	// Get sorted data operations from dedicated tracker
	sortedDataOps := e.dataOperationTracker.GetSortedOperations()

	if len(sortedDataOps) == 0 {
		return "", nil // No data operations
	}

	// Filter out data operations that reference tables which are terminally dropped.
	// These are transitional backfill steps (e.g., UPDATE ... FROM legacy_scores)
	// that become invalid once dropped tables are intentionally omitted from baseline.
	droppedTables := make(map[string]struct{})
	for _, lifecycle := range e.lifecycles {
		if lifecycle == nil || lifecycle.Type != types.TypeTable || len(lifecycle.History) == 0 {
			continue
		}
		if lifecycle.History[len(lifecycle.History)-1].Operation != types.OpDrop {
			continue
		}

		tableName := strings.ToLower(strings.TrimSpace(lifecycle.Name))
		if tableName == "" {
			continue
		}
		droppedTables[tableName] = struct{}{}
		if !strings.Contains(tableName, ".") {
			droppedTables["public."+tableName] = struct{}{}
		}
	}

	matchesDroppedTable := func(candidate string) bool {
		c := strings.ToLower(strings.TrimSpace(candidate))
		if c == "" {
			return false
		}
		if _, ok := droppedTables[c]; ok {
			return true
		}
		if !strings.Contains(c, ".") {
			_, ok := droppedTables["public."+c]
			return ok
		}
		return false
	}

	referencesDroppedTableInSQL := func(sqlText string) bool {
		sqlLower := strings.ToLower(sqlText)
		for dropped := range droppedTables {
			d := strings.ToLower(strings.TrimSpace(dropped))
			if d == "" {
				continue
			}
			bare := d
			if idx := strings.LastIndex(d, "."); idx >= 0 && idx+1 < len(d) {
				bare = d[idx+1:]
			}

			patterns := []string{
				"from " + bare,
				"join " + bare,
				"update " + bare,
				"into " + bare,
				"from " + d,
				"join " + d,
				"update " + d,
				"into " + d,
			}

			for _, p := range patterns {
				if strings.Contains(sqlLower, p) {
					return true
				}
			}
		}
		return false
	}

	if len(droppedTables) > 0 {
		filteredOps := make([]*tracking.DataOperation, 0, len(sortedDataOps))
		skippedOps := 0

		for _, dataOp := range sortedDataOps {
			shouldSkip := false

			if matchesDroppedTable(dataOp.Table) {
				shouldSkip = true
			}

			if !shouldSkip {
				if slices.ContainsFunc(dataOp.DependsOn, matchesDroppedTable) {
					shouldSkip = true
				}
			}

			if !shouldSkip && referencesDroppedTableInSQL(dataOp.Statement.SQL) {
				shouldSkip = true
			}

			if shouldSkip {
				skippedOps++
				e.logger.Info("[DATA-OPS] Skipping %s on %s because it references a dropped table", dataOp.Operation, dataOp.Table)
				continue
			}

			filteredOps = append(filteredOps, dataOp)
		}

		if skippedOps > 0 {
			e.logger.Info("[DATA-OPS] Filtered %d data operation(s) referencing dropped tables", skippedOps)
		}

		sortedDataOps = filteredOps
		if len(sortedDataOps) == 0 {
			return "", nil
		}
	}

	// ARCHITECTURAL DECISION: We do NOT build column evolution map for data operations
	// Data operations should be preserved exactly as written (see comment below)

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

		// ARCHITECTURAL DECISION: Do NOT apply column evolution to data operations
		//
		// Rationale:
		// 1. Data operations (INSERT/UPDATE/DELETE) are non-idempotent - they were written
		//    for the schema at a specific migration point in time
		// 2. INSERT statements have VALUES tied to their original column list - modifying
		//    column lists without adjusting VALUES causes misalignment
		// 3. Column evolution is for DDL objects (CREATE/ALTER) that get consolidated,
		//    not for one-time data mutations
		// 4. Data operations should be preserved exactly as written to maintain correctness
		//
		// This prevents  (INSERT column list mismatch) where:
		// - Column evolution was modifying column lists but not VALUES, causing NULL misalignment
		//
		// Solution: Use the original SQL exactly as written - no column evolution applied
		sql := dataOp.Statement.SQL

		e.logger.Debug("[DATA-OPS] Preserving %s operation on %s exactly as written (no column evolution)",
			dataOp.Operation, dataOp.Table)

		dataBuilder.Statement(sql)
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

func (e *Engine) removeOrphanedFunctionStatements(rawSQL string) string {
	if strings.TrimSpace(rawSQL) == "" {
		return rawSQL
	}

	e.logger.Info("SAFETY NET: Scanning final SQL for orphaned function statements via AST")
	aliveFunctions := make(map[string]bool)

	for key, result := range e.consolidationResults {
		if !strings.HasSuffix(key, "::FUNCTION") {
			continue
		}

		trimmed := strings.TrimSpace(strings.ToUpper(result.ConsolidatedSQL))
		if strings.HasPrefix(trimmed, "DROP") || len(trimmed) == 0 {
			continue
		}

		parts := strings.Split(key, "::")
		if len(parts) == 0 {
			continue
		}

		fullName := normalizeFunctionIdentifier(parts[0])
		if fullName == "" {
			continue
		}

		aliveFunctions[fullName] = true
		if idx := strings.LastIndex(fullName, "."); idx >= 0 && idx+1 < len(fullName) {
			aliveFunctions[fullName[idx+1:]] = true
		}
	}

	parseResult, err := pg_query.Parse(rawSQL)
	if err != nil || parseResult == nil {
		e.logger.Warn("SAFETY NET: Failed to parse final SQL for orphaned-function filtering: %v", err)
		return rawSQL
	}

	type statementRange struct {
		start int
		end   int
	}

	toRemove := make([]statementRange, 0)

	for _, rawStmt := range parseResult.Stmts {
		if rawStmt == nil || rawStmt.Stmt == nil {
			continue
		}

		start := int(rawStmt.StmtLocation)
		end := start + int(rawStmt.StmtLen)
		if start < 0 || start >= len(rawSQL) {
			continue
		}
		if end <= start || end > len(rawSQL) {
			end = len(rawSQL)
		}

		node := rawStmt.Stmt.GetNode()
		shouldRemove := false
		reason := ""

		switch typed := node.(type) {
		case *pg_query.Node_CommentStmt:
			commentStmt := typed.CommentStmt
			if commentStmt != nil && commentStmt.Objtype == pg_query.ObjectType_OBJECT_FUNCTION {
				fn := normalizeFunctionIdentifier(extractFunctionNameFromObjectNode(commentStmt.Object))
				if fn != "" && !aliveFunctions[fn] {
					if idx := strings.LastIndex(fn, "."); idx >= 0 && idx+1 < len(fn) && aliveFunctions[fn[idx+1:]] {
						break
					}
					shouldRemove = true
					reason = fmt.Sprintf("orphaned COMMENT ON FUNCTION '%s'", fn)
				}
			}

		case *pg_query.Node_GrantStmt:
			grantStmt := typed.GrantStmt
			if grantStmt != nil && grantStmt.Objtype == pg_query.ObjectType_OBJECT_FUNCTION {
				for _, obj := range grantStmt.Objects {
					fn := normalizeFunctionIdentifier(extractFunctionNameFromObjectNode(obj))
					if fn == "" {
						continue
					}

					if aliveFunctions[fn] {
						continue
					}
					if idx := strings.LastIndex(fn, "."); idx >= 0 && idx+1 < len(fn) && aliveFunctions[fn[idx+1:]] {
						continue
					}

					shouldRemove = true
					reason = fmt.Sprintf("orphaned GRANT/REVOKE ON FUNCTION '%s'", fn)
					break
				}
			}
		}

		if shouldRemove {
			e.logger.Warn("SAFETY NET: Removing %s", reason)
			toRemove = append(toRemove, statementRange{start: start, end: end})
		}
	}

	if len(toRemove) == 0 {
		return rawSQL
	}

	sort.Slice(toRemove, func(i, j int) bool {
		return toRemove[i].start < toRemove[j].start
	})

	merged := make([]statementRange, 0, len(toRemove))
	for _, r := range toRemove {
		if len(merged) == 0 {
			merged = append(merged, r)
			continue
		}

		last := &merged[len(merged)-1]
		if r.start <= last.end {
			if r.end > last.end {
				last.end = r.end
			}
			continue
		}

		merged = append(merged, r)
	}

	var rebuilt strings.Builder
	cursor := 0
	for _, r := range merged {
		if r.start > cursor {
			rebuilt.WriteString(rawSQL[cursor:r.start])
		}
		if r.end > cursor {
			cursor = r.end
		}
	}
	if cursor < len(rawSQL) {
		rebuilt.WriteString(rawSQL[cursor:])
	}

	return rebuilt.String()
}

func extractFunctionNameFromObjectNode(node *pg_query.Node) string {
	if node == nil {
		return ""
	}

	parts := make([]string, 0)
	if list := node.GetList(); list != nil {
		for _, item := range list.Items {
			if str := item.GetString_(); str != nil {
				parts = append(parts, str.Sval)
			}
		}
	}

	if args := node.GetObjectWithArgs(); args != nil {
		parts = parts[:0]
		for _, item := range args.Objname {
			if str := item.GetString_(); str != nil {
				parts = append(parts, str.Sval)
			}
		}
	}

	return strings.Join(parts, ".")
}

func normalizeFunctionIdentifier(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}

	segments := strings.Split(trimmed, ".")
	for i, segment := range segments {
		clean := strings.TrimSpace(segment)
		clean = strings.Trim(clean, `"`)
		segments[i] = strings.ToLower(clean)
	}

	return strings.Join(segments, ".")
}

// optimizeIndexTypes uses column type information from the tracker to set appropriate index types
// This replaces the broken regex-based "safety net" with proper AST-based type checking
func (e *Engine) optimizeIndexTypes(sql string) string {
	if e.tracker == nil {
		e.logger.Info("[INDEX-OPT] Tracker not available, skipping index type optimization")
		return sql
	}

	// Parse the consolidated SQL to find index statements
	parseResult, err := pg_query.Parse(sql)
	if err != nil {
		e.logger.Warn("[INDEX-OPT] Failed to parse SQL for index optimization: %v", err)
		return sql // Return unmodified on parse error
	}

	modified := false
	optimizationCount := 0

	// Iterate through all statements looking for CREATE INDEX
	for _, stmt := range parseResult.Stmts {
		if stmt.Stmt == nil {
			continue
		}

		// Check if this is an IndexStmt
		indexStmt := stmt.Stmt.GetIndexStmt()
		if indexStmt == nil {
			continue
		}

		// Extract table name
		if indexStmt.Relation == nil {
			continue
		}

		tableName := indexStmt.Relation.Relname
		schemaName := indexStmt.Relation.Schemaname
		if schemaName == "" {
			schemaName = "public"
		}
		fullTableName := schemaName + "." + tableName

		// Extract column names from index parameters
		if len(indexStmt.IndexParams) == 0 {
			continue
		}

		// For now, focus on single-column indexes
		// Multi-column indexes need more complex handling
		if len(indexStmt.IndexParams) > 1 {
			e.logger.Debug("[INDEX-OPT] Skipping multi-column index on %s", fullTableName)
			continue
		}

		// Get the column name from the first index parameter
		indexParam := indexStmt.IndexParams[0]
		if indexParam == nil {
			continue
		}

		// Extract column name and check for operator class
		var columnName string
		var hasOperatorClass bool
		indexElem := indexParam.GetIndexElem()
		if indexElem != nil {
			columnName = indexElem.Name
			// If the index has an explicit operator class, don't modify it
			// Operator classes are access-method specific (e.g., gin_trgm_ops for GIN)
			if len(indexElem.Opclass) > 0 {
				hasOperatorClass = true
			}
		}

		if columnName == "" {
			e.logger.Debug("[INDEX-OPT] Could not extract column name from index parameter")
			continue
		}

		// Skip indexes with explicit operator classes - they're already optimized for their access method
		if hasOperatorClass {
			e.logger.Debug("[INDEX-OPT] Skipping %s.%s: has explicit operator class (already optimized)", fullTableName, columnName)
			continue
		}

		// Get actual column type from tracker
		// Prefer unqualified table name first because migrations commonly define tables
		// without schema qualification (e.g., "profiles"), while index statements are often
		// schema-qualified (e.g., "public.profiles"). Using unqualified metadata first avoids
		// stale/mismatched type lookups across duplicated CREATE TABLE IF NOT EXISTS variants.
		colInfo := e.tracker.GetColumnType(tableName, columnName)
		if colInfo == nil {
			// Fallback to schema-qualified name
			colInfo = e.tracker.GetColumnType(fullTableName, columnName)
		}

		if colInfo == nil {
			e.logger.Debug("[INDEX-OPT] No type info for %s.%s, keeping original access method", fullTableName, columnName)
			continue
		}

		// Get current access method
		currentMethod := indexStmt.AccessMethod
		if currentMethod == "" {
			currentMethod = "btree" // PostgreSQL default
		}

		// Determine appropriate access method based on actual column type
		var newAccessMethod string
		var reason string

		if colInfo.IsArray {
			// Arrays can use GIN for array operations (containment, overlap)
			// Arrays CANNOT use GiST without operator class
			// Keep current access method if it's gin (likely correct for array operations)
			// Change to btree only if it was incorrectly set to gist
			if currentMethod == "gist" {
				newAccessMethod = "btree"
				reason = "array type (fixed from gist)"
			} else {
				// Keep existing gin or btree - both are valid for arrays
				e.logger.Debug("[INDEX-OPT] %s.%s: keeping %s for array type", fullTableName, columnName, currentMethod)
				continue
			}
		} else if strings.Contains(strings.ToLower(colInfo.DataType), "tsvector") {
			// tsvector MUST use gin for full-text search
			if currentMethod != "gin" {
				newAccessMethod = "gin"
				reason = "tsvector type"
			} else {
				e.logger.Debug("[INDEX-OPT] %s.%s: %s already correct for tsvector", fullTableName, columnName, currentMethod)
				continue
			}
		} else if colInfo.IsSpatial {
			// Do NOT force-rewrite spatial indexes from btree -> gist automatically.
			// The source migration may intentionally use btree (or another method), and
			// forcing gist can produce invalid SQL when table schemas drift across legacy
			// CREATE TABLE IF NOT EXISTS variants.
			e.logger.Debug("[INDEX-OPT] %s.%s: keeping %s for spatial type (no forced rewrite)", fullTableName, columnName, currentMethod)
			continue
		} else {
			// Regular types: keep current access method
			// Don't change unless there's a specific reason
			e.logger.Debug("[INDEX-OPT] %s.%s: keeping %s for regular type", fullTableName, columnName, currentMethod)
			continue
		}

		// Update the access method if it differs
		if currentMethod != newAccessMethod {
			indexStmt.AccessMethod = newAccessMethod
			modified = true
			optimizationCount++
			e.logger.Info("[INDEX-OPT] %s.%s (%s %s): %s → %s",
				fullTableName, columnName, reason, colInfo.DataType,
				currentMethod, newAccessMethod)
		} else {
			e.logger.Debug("[INDEX-OPT] %s.%s: %s already optimal",
				fullTableName, columnName, currentMethod)
		}
	}

	// If we modified anything, deparse back to SQL
	if modified {
		deparsedSQL, err := pg_query.Deparse(parseResult)
		if err != nil {
			e.logger.Warn("[INDEX-OPT] Failed to deparse modified AST: %v", err)
			return sql // Return original on deparse error
		}
		e.logger.Info("[INDEX-OPT] Successfully optimized %d index type(s)", optimizationCount)
		return deparsedSQL
	}

	e.logger.Info("[INDEX-OPT] No index optimizations needed - all access methods appropriate")
	return sql
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

	var sequences []int
	for seq := range migrations {
		sequences = append(sequences, seq)
	}
	sort.Ints(sequences)

	// Stream process each migration in sequential order
	for _, sequence := range sequences {
		content := migrations[sequence]
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
	if e == nil {
		return SquashStats{}
	}

	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.stats == nil {
		return SquashStats{}
	}

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
func OptimizedSquashForLargeDatasets(cfg *config.Config, migrations map[int]string, memoryLimitMB int) (*SquashResult, error) {
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

	engine, err := NewEngine(engineConfig)
	if err != nil {
		return nil, err
	}
	defer engine.Close()

	return engine.SquashStreaming(migrations)
}

// OptimizedSquashFromDirectory provides a high-level interface for directory processing
func OptimizedSquashFromDirectory(cfg *config.Config, dir string, memoryLimitMB int) (*SquashResult, error) {
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

	engine, err := NewEngine(engineConfig)
	if err != nil {
		return nil, err
	}
	defer engine.Close()

	return engine.SquashFromDirectory(dir)
}
