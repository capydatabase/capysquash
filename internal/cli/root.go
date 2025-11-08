package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/ai"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/config"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/errors"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/parser"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/squasher"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/tracking"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/transformation"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/types"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/utils"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/validation"
)

var (
	configPath    string
	safetyLevel   string
	outputDir     string
	dryRun        bool
	explainMode   bool
	verbose       bool
	showProgress  bool
	streaming     bool
	memoryLimitMB int
	batchSize     int
	workerCount   int

	// Transformation options
	enableBackup         bool
	enableRollback       bool
	enableTransformation bool
	backupPath           string
	rollbackPath         string

	// DDL cycle detection options
	enableCycleDetection bool
	cycleDetectionDepth  int
	showCycleDetails     bool

	// AI fix options
	maxFixAttempts int
	autoApplyFixes bool

	// Validation options
	validationMode    string
	workflowOutputDir string
	noValidate        bool
	failOnDiff        bool
	openReport        bool

	// Transaction planning options
	showPlan         bool
	explainLocks     bool
	splitBySchema    string
	branchCheck      bool
	iKnowWhatImDoing bool

	// Init-config options
	forceOverwrite bool

	// Output options
	quietMode bool
	noEmoji   bool

	// TUI mode
	tuiMode bool
)

var rootCmd = &cobra.Command{
	Use:     "pgsquash",
	Short:   "pgsquash-engine - Intelligent PostgreSQL migration consolidation",
	Version: "0.9.5",
	Long: `pgsquash-engine intelligently consolidates PostgreSQL migration files into
clean, production-ready SQL while preserving data integrity, respecting
dependencies, and validating safety at every step.

The pgsquash-engine is the core consolidation engine that powers
CAPYSQUASH and provides parser-grade accuracy for migration optimization.`,
	// UX FIX: Silence duplicate error messages - errors are already logged in main()
	SilenceErrors: true,
	// UX FIX: Don't show usage on every error - only when explicitly requested with --help
	SilenceUsage: true,
}

var analyzeCmd = &cobra.Command{
	Use:   "analyze [migration files...]",
	Short: "Analyze migrations without modifications",
	Long: `Analyze migration files to identify redundancies and optimization
opportunities without making any changes to your files.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runAnalyze,
}

var squashCmd = &cobra.Command{
	Use:   "squash [migration files...]",
	Short: "Consolidate and optimize migrations intelligently",
	Long: `Intelligently consolidate migration files into clean, production-ready SQL.
Automatically resolves dependencies, removes redundancies, and reorganizes
operations while preserving schema integrity and respecting safety constraints.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSquash,
}

var validateCmd = &cobra.Command{
	Use:   "validate [original dir] [squashed dir]",
	Short: "Validate squashed migrations match original schema",
	Long: `Compare the final schema state between original and squashed
migrations to ensure they produce identical results.`,
	Args: cobra.ExactArgs(2),
	RunE: runValidate,
}

var initConfigCmd = &cobra.Command{
	Use:   "init-config",
	Short: "Generate default configuration file",
	Long:  `Create a default pgsquash.config.json file with all available options.`,
	RunE:  runInitConfig,
}

var aiTestCmd = &cobra.Command{
	Use:   "ai-test",
	Short: "Test AI provider integrations",
	Long: `Test the AI provider system including Claude, OpenAI, and other
configured providers. Shows available capabilities and runs integration tests.`,
	RunE: runAITest,
}

var aiDemoCmd = &cobra.Command{
	Use:   "ai-demo",
	Short: "Demonstrate AI analysis capabilities",
	Long: `Run demonstration of AI analysis capabilities with sample PostgreSQL
functions and schemas to showcase semantic analysis, dead code detection,
and other AI-powered features.`,
	RunE: runAIDemo,
}

var aiFixCmd = &cobra.Command{
	Use:   "ai-fix [migration directory]",
	Short: "AI-assisted migration fixing",
	Long: `Use AI to automatically analyze and fix broken migrations.
This command runs validation, analyzes errors, and uses AI to suggest and apply fixes
in an interactive loop until migrations validate successfully.

Requires: ANTHROPIC_API_KEY, OPENAI_API_KEY, or AZURE_OPENAI_ENDPOINT

Example:
  pgsquash ai-fix migrations/
  pgsquash ai-fix migrations/ --max-attempts 10 --auto-apply`,
	Args: cobra.ExactArgs(1),
	RunE: runAIFix,
}

// Standardized workflow commands
var safeCmd = &cobra.Command{
	Use:   "safe [migration files...]",
	Short: "SAFE workflow: Production-ready squashing with full validation",
	Long: `SAFE workflow combines Conservative safety level with comprehensive Docker validation.
Perfect for production deployments requiring maximum safety and confidence.

Features enabled:
- Conservative safety level (minimal changes, maximum safety)
- Full Docker validation with TWO_CONTAINERS approach
- Pre-squash backup generation
- Rollback script creation
- Schema diff analysis with risk assessment
- Extension auto-detection and installation
- Comprehensive progress reporting

This workflow prioritizes data safety over optimization.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runSafeWorkflow,
}

var fastCmd = &cobra.Command{
	Use:   "fast [migration files...]",
	Short: "FAST workflow: Development-optimized squashing with minimal validation",
	Long: `FAST workflow optimizes for development speed with Standard safety level.
Best for development environments where speed matters more than extensive validation.

Features enabled:
- Standard safety level (balanced optimization)
- SCHEMA_DIFF validation approach (fastest)
- Streaming mode for large datasets
- DDL cycle detection and resolution
- SQL transformation and modernization
- Progress tracking with minimal overhead

This workflow balances speed with reasonable safety measures.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runFastWorkflow,
}

var analyzeDeepCmd = &cobra.Command{
	Use:   "analyze-deep [migration files...]",
	Short: "ANALYZE workflow: Comprehensive analysis without modifications",
	Long: `ANALYZE workflow performs deep analysis without making any changes.
Ideal for understanding migration complexity and planning consolidation strategy.

Features enabled:
- Complete dependency graph analysis
- Advanced DDL cycle detection with all algorithm types
- AI-powered semantic analysis (if configured)
- Authentication pattern detection
- Dead code identification
- Performance optimization suggestions
- Risk assessment with detailed warnings
- Comprehensive reporting with categorized findings

This workflow provides maximum insight without any data modifications.`,
	Args: cobra.MinimumNArgs(1),
	RunE: runAnalyzeWorkflow,
}

func init() {
	rootCmd.SetVersionTemplate(`{{.Version}}
`)

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&configPath, "config", "c", "",
		"Config file (default: pgsquash.config.json)")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false,
		"Verbose output")

	rootCmd.PersistentFlags().BoolVarP(&quietMode, "quiet", "q", false,
		"Quiet mode - only show errors and final results (ideal for CI/CD)")
	rootCmd.PersistentFlags().BoolVar(&noEmoji, "no-emoji", false,
		"Disable emoji characters in output (improves terminal compatibility)")

	// Analyze command flags
	analyzeCmd.Flags().BoolVar(&tuiMode, "tui", false,
		"Launch interactive TUI for analysis")
	analyzeCmd.Flags().BoolVar(&showProgress, "progress", true,
		"Show progress during analysis")
	analyzeCmd.Flags().BoolVar(&streaming, "streaming", false,
		"Use streaming mode for memory-efficient analysis of large migration sets")
	analyzeCmd.Flags().IntVar(&memoryLimitMB, "memory-limit", 256,
		"Memory limit in MB for streaming mode (default: 256)")

	// Squash command flags
	squashCmd.Flags().BoolVar(&tuiMode, "tui", false,
		"Launch interactive TUI for squashing")
	squashCmd.Flags().StringVarP(&safetyLevel, "safety", "s", "",
		"Safety level: conservative, standard, aggressive (overrides config)")
	squashCmd.Flags().StringVarP(&outputDir, "output", "o", "",
		"Output directory (overrides config)")
	squashCmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Show what would be done without writing files")
	squashCmd.Flags().BoolVar(&explainMode, "explain", false,
		"Show detailed consolidation plan with reasoning (implies --dry-run)")
	squashCmd.Flags().BoolVar(&showProgress, "progress", true,
		"Show progress during squashing")

	// Performance and streaming flags
	squashCmd.Flags().BoolVar(&streaming, "streaming", false,
		"Use streaming mode for memory-efficient processing of large migration sets")
	squashCmd.Flags().IntVar(&memoryLimitMB, "memory-limit", 256,
		"Memory limit in MB for streaming mode (default: 256)")
	squashCmd.Flags().IntVar(&batchSize, "batch-size", 50,
		"Batch size for streaming processing (default: 50)")
	squashCmd.Flags().IntVar(&workerCount, "workers", 0,
		"Number of worker goroutines (default: auto-detect based on CPU cores)")

	// Transformation and safety flags
	squashCmd.Flags().BoolVar(&enableBackup, "backup", false,
		"Generate backup before squashing (requires database connection)")
	squashCmd.Flags().BoolVar(&enableRollback, "rollback", false,
		"Generate rollback scripts for squash operations")
	squashCmd.Flags().BoolVar(&enableTransformation, "transform", true,
		"Apply SQL transformations and modernization (default: enabled)")
	squashCmd.Flags().StringVar(&backupPath, "backup-path", "",
		"Custom backup file path (default: auto-generated)")
	squashCmd.Flags().StringVar(&rollbackPath, "rollback-path", "rollbacks",
		"Directory for rollback scripts (default: ./rollbacks)")

	// DDL cycle detection flags
	squashCmd.Flags().BoolVar(&enableCycleDetection, "detect-cycles", true,
		"Enable advanced DDL cycle detection (default: enabled)")
	squashCmd.Flags().IntVar(&cycleDetectionDepth, "cycle-depth", 10,
		"Maximum cycle detection depth (default: 10)")
	squashCmd.Flags().BoolVar(&showCycleDetails, "cycle-details", false,
		"Show detailed information about detected cycles")

	// Validation flags
	squashCmd.Flags().BoolVar(&noValidate, "no-validate", false,
		"Skip automatic validation after squashing")
	squashCmd.Flags().BoolVar(&failOnDiff, "fail-on-diff", false,
		"Exit with error code 1 if schema differences are detected during validation")
	squashCmd.Flags().BoolVar(&openReport, "open-report", false,
		"Open validation report in $EDITOR after validation")

	// Transaction planning and lock analysis flags
	squashCmd.Flags().BoolVar(&showPlan, "plan", false,
		"Show transaction batching plan without executing")
	squashCmd.Flags().BoolVar(&explainLocks, "explain-locks", false,
		"Show detailed lock-level analysis for all statements")
	squashCmd.Flags().StringVar(&splitBySchema, "split-by", "",
		"Split output by: schema, object, or none (default: none)")

	// Branch safety flags
	squashCmd.Flags().BoolVar(&branchCheck, "branch-check", false,
		"Enforce protected branch requirement (fails if not on main/master)")
	squashCmd.Flags().BoolVar(&iKnowWhatImDoing, "i-know-what-im-doing", false,
		"Override all safety warnings (use with caution)")

	// AI fix command flags
	aiFixCmd.Flags().IntVar(&maxFixAttempts, "max-attempts", 5,
		"Maximum number of fix attempts (default: 5)")
	aiFixCmd.Flags().BoolVar(&autoApplyFixes, "auto-apply", false,
		"Automatically apply fixes without confirmation (default: false)")
	aiFixCmd.Flags().BoolVar(&verbose, "verbose", false,
		"Enable verbose output")

	// Validate command flags
	validateCmd.Flags().StringVar(&validationMode, "validation-mode", "",
		"Validation approach: TWO_CONTAINERS, TWO_DATABASES, or SCHEMA_DIFF (default: from config or TWO_DATABASES)")

	// Init-config command flags
	initConfigCmd.Flags().BoolVarP(&forceOverwrite, "force", "f", false,
		"Overwrite existing config file if it exists")

	// Workflow command flags (safe, fast, analyze-deep)
	safeCmd.Flags().StringVarP(&workflowOutputDir, "output", "o", "",
		"Output directory for squashed migrations (overrides config)")
	fastCmd.Flags().StringVarP(&workflowOutputDir, "output", "o", "",
		"Output directory for squashed migrations (overrides config)")
	analyzeDeepCmd.Flags().StringVarP(&workflowOutputDir, "output", "o", "",
		"Output directory for analysis results (overrides config)")

	// Add commands to root
	rootCmd.AddCommand(analyzeCmd, squashCmd, validateCmd, initConfigCmd, aiTestCmd, aiDemoCmd, aiFixCmd, safeCmd, fastCmd, analyzeDeepCmd)
}

func Execute() error {
	// Configure global logging based on verbose flag
	// This is called before any command runs via PersistentPreRun
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if quietMode {
			// Quiet mode: suppress all non-error output
			verbose = false
			showProgress = false
		}
		// If verbose mode is enabled, all logs are shown (default behavior)

		if noEmoji || quietMode {
			color.NoColor = true
		}
	}

	return rootCmd.Execute()
}

func runAnalyze(cmd *cobra.Command, args []string) error {
	// Check if TUI mode is requested
	if tuiMode {
		// Get migration directory from args
		migrationDir := "."
		if len(args) > 0 {
			migrationDir = args[0]
		}
		return runTUIAnalyze(cmd, []string{migrationDir})
	}

	startTime := time.Now()

	// BUG #7 FIX: Config loading moved to line ~500 where it's actually used
	// (Removed duplicate config load here - it was shadowing the one below)

	if verbose {
		fmt.Printf("Loading migrations from %d files...\n", len(args))
	}

	var t *tracking.Tracker
	var migrations []*MigrationWithContent

	// Use streaming for large datasets or when explicitly requested
	if streaming || len(args) > 100 {
		// Show streaming mode indicator
		if streaming {
			color.Cyan("🚀 Streaming mode: enabled (memory limit: %dMB)\n", memoryLimitMB)
		} else {
			color.Cyan("🚀 Auto-enabling streaming mode for %d files (threshold: 100)\n", len(args))
		}
		// Create memory-optimized tracker for large datasets
		memTracker := tracking.NewMemoryOptimizedTracker(memoryLimitMB, 50)

		if showProgress {
			memTracker.SetProgressCallback(func(processed, total int64, throughput float64) {
				if total > 0 {
					progress := float64(processed) / float64(total) * 100
					fmt.Printf("\rAnalyzing: %.1f%% (%d/%d) - %.1f files/sec",
						progress, processed, total, throughput)
				} else {
					fmt.Printf("\rAnalyzing: %d files processed - %.1f files/sec",
						processed, throughput)
				}
			})
		}

		// Process only the specified files using streaming
		for _, migrationFile := range args {
			migration, err := loadSingleMigration(migrationFile)
			if err != nil {
				return errors.NewError(
					errors.ErrorCodeSyntaxError,
					"Failed to load migration file",
					errors.SeverityError,
					errors.CategoryParsing,
				).WithFile(migrationFile).WithInnerError(err).WithSuggestion("Verify the SQL file exists and contains valid PostgreSQL syntax")
			}
			memTracker.GetTracker().ProcessMigration(migration.Migration, len(migrations))
			migrations = append(migrations, migration)
		}

		if showProgress {
			fmt.Printf("\n")
		}

		// Get the underlying tracker
		t = memTracker.GetTracker()
		defer func() {
			if err := memTracker.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: Failed to stop memory tracker: %v\n", err)
			}
		}()
	} else {
		// Use traditional approach for smaller datasets
		var err error
		migrations, err = loadMigrations(args, showProgress)
		if err != nil {
			return err
		}

		// Create tracker
		t = tracking.NewTracker()
		for i, m := range migrations {
			t.ProcessMigration(m.Migration, i)
		}
	}

	// Get analysis results
	redundancies := t.GetRedundantObjects()
	stats := t.GetStatistics()
	warnings := t.ValidateConsistency()

	// Convert to parser migrations for print function
	parserMigrations := make([]*types.Migration, len(migrations))
	for i, m := range migrations {
		parserMigrations[i] = m.Migration
	}

	// Print analysis report
	printAnalysisReport(parserMigrations, redundancies, stats, warnings)

	if verbose {
		fmt.Printf("\nAnalysis completed in %v\n", time.Since(startTime))
	}

	return nil
}

func runSquash(cmd *cobra.Command, args []string) error {
	// Check if TUI mode is requested
	if tuiMode {
		// Get migration directory from args
		migrationDir := "."
		if len(args) > 0 {
			migrationDir = args[0]
		}
		return runTUI(cmd, []string{migrationDir})
	}

	startTime := time.Now()

	// If explain mode is enabled, imply dry-run
	if explainMode {
		dryRun = true
	}

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"Failed to load configuration",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithFile(configPath).WithInnerError(err).WithSuggestion("Run 'pgsquash init-config' to generate a valid configuration file")
	}

	// Override config with command line flags
	if safetyLevel != "" {
		cfg.SafetyLevel = safetyLevel
	}
	if outputDir != "" {
		cfg.Output.Directory = outputDir
	} else if cfg.Output.Directory == "squashed" {
		if !verbose {
			color.Cyan("ℹ️  Output directory not specified, using default: ./squashed\n")
		}
	}

	// Auto-detect worker count if not specified
	if workerCount == 0 {
		workerCount = runtime.NumCPU()
	}

	if verbose {
		fmt.Printf("Loading migrations from %d files...\n", len(args))
		fmt.Printf("Safety level: %s\n", cfg.SafetyLevel)
		fmt.Printf("Output directory: %s\n", cfg.Output.Directory)
	}

	// Show streaming mode info whenever streaming is explicitly enabled
	if streaming {
		color.Cyan("🚀 Streaming mode: enabled (memory limit: %dMB, batch size: %d, workers: %d)\n",
			memoryLimitMB, batchSize, workerCount)
	}

	var finalSQL string
	var warnings []string
	var migrationCount int

	// Use streaming engine for large datasets or when explicitly requested
	if streaming || len(args) > 100 {
		if !streaming {
			color.Cyan("🚀 Auto-enabling streaming mode for %d files (threshold: 100)\n", len(args))
			color.Cyan("   Streaming: batch=%d, workers=%d, memory=%dMB\n", batchSize, workerCount, memoryLimitMB)
		}

		// Use streaming engine with optimized settings
		if len(args) > 500 {
			// For very large datasets, use high-performance settings
			finalSQL, warnings, err = squasher.OptimizedSquashForLargeDatasets(cfg, nil, memoryLimitMB)
			if err != nil {
				return errors.NewError(
					errors.ErrorCodeConsolidationFailed,
					"Failed to squash large dataset",
					errors.SeverityError,
					errors.CategoryConsolidation,
				).WithInnerError(err).WithAdditional("file_count", len(args)).WithSuggestion("Try reducing memory limit or batch size, or use standard mode for smaller datasets")
			}
			migrationCount = len(args)
		} else {
			// Create engine with streaming configuration
			engineConfig := squasher.EngineConfig{
				Config:              cfg,
				BatchSize:           batchSize,
				WorkerCount:         workerCount,
				MemoryLimitMB:       memoryLimitMB,
				EnableStreaming:     true,
				EnableProgressTrack: showProgress,

				// Transformation options
				EnableBackup:         enableBackup,
				EnableRollback:       enableRollback,
				EnableTransformation: enableTransformation,
				BackupConfig:         createBackupConfig(),
				TransformationConfig: createTransformationConfig(),

				EnableCycleDetection: enableCycleDetection,
				ShowCycleDetails:     showCycleDetails,
				CycleDetectionDepth:  cycleDetectionDepth,
			}

			if showProgress {
				engineConfig.ProgressCallback = func(processed, total int64, phase string) {
					if total > 0 {
						progress := float64(processed) / float64(total) * 100
						fmt.Printf("\r%s: %.1f%% (%d/%d)", phase, progress, processed, total)
					} else {
						fmt.Printf("\r%s: %d processed", phase, processed)
					}
				}
			}

			engine := squasher.NewEngine(engineConfig)

			// Load migrations and convert to map
			migrations, err := loadMigrations(args, showProgress)
			if err != nil {
				return err
			}

			migrationMap := make(map[int]string)
			for i, m := range migrations {
				migrationMap[i] = m.Content
			}

			finalSQL, warnings, err = engine.Squash(migrationMap)
			if err != nil {
				return errors.NewError(
					errors.ErrorCodeConsolidationFailed,
					"Failed to squash migrations in streaming mode",
					errors.SeverityError,
					errors.CategoryConsolidation,
				).WithInnerError(err).WithAdditional("streaming", true).WithSuggestion("Try disabling streaming mode or check for syntax errors in migration files")
			}

			migrationCount = len(migrations)

			// Print final progress line
			if showProgress {
				fmt.Printf("\n")
			}
		}
	} else {
		// Use traditional engine for smaller datasets with transformation support
		migrations, err := loadMigrations(args, showProgress)
		if err != nil {
			return err
		}

		// Create engine configuration for non-streaming mode
		engineConfig := squasher.EngineConfig{
			Config:              cfg,
			EnableStreaming:     false,
			EnableProgressTrack: showProgress,

			// Transformation options
			EnableBackup:         enableBackup,
			EnableRollback:       enableRollback,
			EnableTransformation: enableTransformation,
			BackupConfig:         createBackupConfig(),
			TransformationConfig: createTransformationConfig(),
			RollbackPath:         rollbackPath,

			EnableCycleDetection: enableCycleDetection,
			ShowCycleDetails:     showCycleDetails,
			CycleDetectionDepth:  cycleDetectionDepth,
		}

		engine := squasher.NewEngine(engineConfig)
		defer engine.Close()

		if showProgress {
			fmt.Printf("Processing %d migrations...\n", len(migrations))
		}

		// Convert []*MigrationWithContent to map[int]string
		migrationMap := make(map[int]string)
		for i, m := range migrations {
			migrationMap[i] = m.Content
		}

		// Process migrations with separate files
		squashResult, err := engine.SquashWithSeparateFiles(migrationMap)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeConsolidationFailed,
				"Failed to process and consolidate migrations",
				errors.SeverityError,
				errors.CategoryConsolidation,
			).WithInnerError(err).WithAdditional("migration_count", len(migrations)).WithSuggestion("Review migration files for syntax errors or complex dependencies")
		}

		// Detect extensions for provenance tracking
		extDetector := squasher.NewExtensionDetector()
		analysis := extDetector.AnalyzeMigrations(migrationMap)

		finalSQL = squashResult.BaselineSQL
		warnings = squashResult.Warnings
		migrationCount = len(migrations)

		// Create output directory before writing files
		if err := os.MkdirAll(cfg.Output.Directory, 0755); err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				fmt.Sprintf("Failed to create output directory '%s'", cfg.Output.Directory),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithFile(cfg.Output.Directory).WithInnerError(err).WithSuggestion("Check directory permissions and ensure parent directory exists")
		}

		// Write data operations file if present
		if squashResult.DataOperationsSQL != "" {
			dataPath := filepath.Join(cfg.Output.Directory, "010_data.sql")
			if err := os.WriteFile(dataPath, []byte(squashResult.DataOperationsSQL), 0644); err != nil {
				return errors.NewError(
					errors.ErrorCodeSQLGenerationFailed,
					fmt.Sprintf("Failed to write data operations file '%s'", dataPath),
					errors.SeverityError,
					errors.CategoryConsolidation,
				).WithFile(dataPath).WithInnerError(err).WithSuggestion("Ensure sufficient disk space and write permissions")
			}
			fmt.Println(color.GreenString("✓ Data operations written to: %s", dataPath))
		}

		// Write provenance map
		if squashResult.ProvenanceMap != nil {
			provenance := squasher.NewProvenanceTracker(
				"0.9.5",
				cfg.SafetyLevel,
				cfg.PostgreSQLFeatures.TargetVersion,
				analysis.RequiredExtensions,
			)
			provenance.GetSquashMap().Inputs = squashResult.ProvenanceMap.Inputs
			provenance.GetSquashMap().Outputs = squashResult.ProvenanceMap.Outputs
			provenance.GetSquashMap().Warnings = squashResult.ProvenanceMap.Warnings
			provenance.GetSquashMap().ContentHash = squashResult.ProvenanceMap.ContentHash

			if err := provenance.WriteSquashMap(cfg.Output.Directory); err != nil {
				fmt.Println(color.YellowString("⚠️  Warning: Could not write .squashmap.json: %v", err))
			} else {
				fmt.Println(color.GreenString("✓ Provenance map written to: %s", filepath.Join(cfg.Output.Directory, ".squashmap.json")))
			}
		}
	}

	// Handle explain mode - show detailed consolidation plan
	if explainMode {
		// Create a new engine just for generating the plan
		migrations, err := loadMigrations(args, false)
		if err != nil {
			return err
		}

		engineConfig := squasher.EngineConfig{
			Config:          cfg,
			EnableStreaming: false,
		}
		engine := squasher.NewEngine(engineConfig)
		defer engine.Close()

		// Convert to migration map
		migrationMap := make(map[int]string)
		for i, m := range migrations {
			migrationMap[i] = m.Content
		}

		// Generate detailed plan
		plan, err := engine.GenerateConsolidationPlan(migrationMap)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeConsolidationFailed,
				"Failed to generate consolidation plan",
				errors.SeverityError,
				errors.CategoryConsolidation,
			).WithInnerError(err).WithSuggestion("Run without --explain flag to see more detailed error information")
		}

		// Print the formatted plan
		fmt.Print(color.CyanString(plan.FormatPlan()))
		return nil
	}

	// Regular dry-run mode (without detailed explanation)
	if dryRun {
		fmt.Println("\n" + color.BlueString("=== Dry Run: Final SQL Output ==="))
		fmt.Println(finalSQL)
		if len(warnings) > 0 {
			fmt.Println("\n" + color.YellowString("Warnings:"))
			for _, w := range warnings {
				fmt.Printf("  - %s\n", w)
			}
		}
		fmt.Print("\n" + color.YellowString("Run without --dry-run to apply changes") + "\n")
		return nil
	}

	// Write output - create directory first
	if err := os.MkdirAll(cfg.Output.Directory, 0755); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("Failed to create output directory '%s'", cfg.Output.Directory),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithFile(cfg.Output.Directory).WithInnerError(err).WithSuggestion("Check directory permissions and ensure parent directory exists")
	}

	// Verify directory was created
	if _, err := os.Stat(cfg.Output.Directory); os.IsNotExist(err) {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("Output directory '%s' does not exist after creation attempt", cfg.Output.Directory),
			errors.SeverityCritical,
			errors.CategoryValidation,
		).WithFile(cfg.Output.Directory).WithSuggestion("Check filesystem permissions and available disk space")
	}

	outputPath := filepath.Join(cfg.Output.Directory, "000_baseline.sql")
	if err := os.WriteFile(outputPath, []byte(finalSQL), 0644); err != nil {
		return errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			fmt.Sprintf("Failed to write output file '%s'", outputPath),
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithFile(outputPath).WithInnerError(err).WithSuggestion("Ensure sufficient disk space and write permissions")
	}

	// Verify file was written
	if info, err := os.Stat(outputPath); err != nil {
		return errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			fmt.Sprintf("Output file '%s' was not created", outputPath),
			errors.SeverityCritical,
			errors.CategoryConsolidation,
		).WithFile(outputPath).WithInnerError(err).WithSuggestion("Check filesystem state and available inodes")
	} else if info.Size() == 0 {
		return errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			fmt.Sprintf("Output file '%s' is empty (0 bytes)", outputPath),
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithFile(outputPath).WithSuggestion("This may indicate all migrations were filtered out - check safety level and input files")
	}

	// Print success report
	printSquashSummary(migrationCount, len(strings.Split(finalSQL, "\n")), time.Since(startTime), warnings, outputPath)

	// Run automatic validation unless --no-validate is specified
	if !noValidate {
		fmt.Println("\n" + color.CyanString("🔍 Running automatic validation..."))

		// Get original migrations path
		var originalPath string
		if len(args) > 0 {
			// Check if it's a directory or file
			info, err := os.Stat(args[0])
			if err == nil {
				if info.IsDir() {
					originalPath = args[0]
				} else {
					originalPath = filepath.Dir(args[0])
				}
			}
		}

		if originalPath == "" {
			fmt.Println(color.YellowString("⚠️  Could not determine original migrations path, skipping validation"))
			fmt.Println(color.YellowString("    Run 'pgsquash validate <original> <squashed>' manually if needed"))
		} else {
			// Run validation
			valResult, valErr := runValidationCheck(cfg, originalPath, cfg.Output.Directory)

			if valErr != nil {
				// Print the main error
				fmt.Println(color.RedString("❌ Validation failed: %v", valErr))

				// If it's a structured error with an inner error, print that too
				if structErr, ok := valErr.(*errors.StructuredError); ok && structErr.InnerError != nil {
					fmt.Println(color.RedString("   Inner error: %v", structErr.InnerError))

					// If the inner error is also structured, print its details
					if innerStruct, ok := structErr.InnerError.(*errors.StructuredError); ok && innerStruct.InnerError != nil {
						fmt.Println(color.RedString("   PostgreSQL error: %v", innerStruct.InnerError))
					}
				}

				if failOnDiff {
					return errors.NewError(
						errors.ErrorCodeValidationFailed,
						"Schema validation detected differences",
						errors.SeverityError,
						errors.CategoryValidation,
					).WithInnerError(valErr).WithSuggestion("Review validation report for details")
				}
				fmt.Println(color.YellowString("⚠️  Warning: Validation failed but continuing (use --fail-on-diff to exit on validation errors)"))
			} else if valResult != nil && !valResult.Success {
				// BUG #3 FIX: Check if comparison is valid before reporting differences as errors
				if valResult.DockerValidation.ComparisonValid {
					// Valid comparison - these are real consolidation issues
					fmt.Println(color.RedString("❌ Schema differences detected!"))
					fmt.Println(valResult.DockerValidation.Differences)

					if openReport {
						reportPath := filepath.Join(cfg.Output.Directory, "validation-report.md")
						if err := os.WriteFile(reportPath, []byte(valResult.DockerValidation.Differences), 0644); err == nil {
							fmt.Println(color.CyanString("📝 Validation report saved to: %s", reportPath))
							openInEditor(reportPath)
						}
					}

					if failOnDiff {
						return errors.NewError(
							errors.ErrorCodeValidationFailed,
							"Schema differences detected between original and squashed migrations",
							errors.SeverityError,
							errors.CategoryValidation,
						).WithSuggestion("Review the differences above and ensure squashing is correct")
					}
				} else {
					// Invalid comparison - original migrations failed
					fmt.Println(color.YellowString("⚠️  Original migrations have errors (this is expected - pgsquash fixes broken migrations)"))
					if valResult.DockerValidation.OriginalMigrationsError != "" {
						fmt.Println(color.YellowString("    Error: %s", valResult.DockerValidation.OriginalMigrationsError))
					}
					fmt.Println(color.GreenString("✅ Squashed migrations applied successfully"))
					if valResult.DockerValidation.Differences != "" {
						fmt.Println(color.CyanString("ℹ️  Schema differences (informational only - due to original migration failures):"))
						// Print first 20 differences only
						lines := strings.Split(valResult.DockerValidation.Differences, "\n")
						maxLines := 25
						if len(lines) > maxLines {
							for i := 0; i < maxLines && i < len(lines); i++ {
								fmt.Println(color.CyanString("    %s", lines[i]))
							}
							fmt.Println(color.CyanString("    ... (%d more differences not shown)", len(lines)-maxLines))
						} else {
							fmt.Println(color.CyanString("    %s", valResult.DockerValidation.Differences))
						}
					}
				}
			} else {
				fmt.Println(color.GreenString("✅ Validation passed - schemas are identical"))
			}
		}
	}

	return nil
}

// runValidationCheck performs validation and returns the result
func runValidationCheck(cfg *config.Config, originalPath, squashedPath string) (*validation.ValidationResult, error) {
	// Create validator with config
	// BUG #6 FIX: Extract PostgreSQL version from docker_image config instead of hardcoding
	postgresVersion := "17" // Default to 17 if not specified
	if cfg.Validation.DockerImage != "" {
		// Parse version from docker image (e.g., "postgres:17" -> "17")
		parts := strings.Split(cfg.Validation.DockerImage, ":")
		if len(parts) == 2 {
			postgresVersion = parts[1]
		}
	}

	valConfig := &validation.ValidationConfig{
		Level:                    validation.ValidationLevelStandard,
		ValidateExpressions:      true,
		ValidateConstraints:      true,
		ValidateDependencies:     true,
		DockerApproach:           validation.ApproachTwoDatabases,
		PostgreSQLVersion:        postgresVersion,
		EnableExtensionDetection: true,
		AutoInstallExtensions:    true,
		Verbose:                  verbose,
	}

	if cfg.Validation.Mode != "" {
		valConfig.DockerApproach = validation.ValidationApproach(cfg.Validation.Mode)
	}

	// Apply container ready timeout from config (convert int seconds to time.Duration)
	if cfg.Validation.ContainerReadyTimeout > 0 {
		valConfig.ContainerReadyTimeout = time.Duration(cfg.Validation.ContainerReadyTimeout) * time.Second
	}

	validator := validation.NewSchemaValidator(valConfig, nil, nil)
	defer validator.Close()

	ctx := context.Background()
	result, err := validator.ValidateWithDocker(ctx, originalPath, squashedPath)

	return result, err
}

// openInEditor opens a file in the user's preferred editor
func openInEditor(path string) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim" // fallback
	}

	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Printf("Failed to open editor: %v\n", err)
	}
}

func runValidate(cmd *cobra.Command, args []string) error {
	originalDir := args[0]
	squashedDir := args[1]

	fmt.Printf("Validating migrations...\n")
	fmt.Printf("Original: %s\n", originalDir)
	fmt.Printf("Squashed: %s\n", squashedDir)

	// Load config for validation settings (runValidate is a separate function from runSquash)
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"Failed to load configuration for validation",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithFile(configPath).WithInnerError(err).WithSuggestion("Ensure pgsquash.config.json is valid or use default configuration")
	}

	// Load migrations to detect auth patterns
	migrations, err := filepath.Glob(filepath.Join(originalDir, "*.sql"))
	if err == nil && len(migrations) > 0 {
		// Read migrations to detect auth service
		migrationContent := make(map[int]string)
		for i, migFile := range migrations {
			content, readErr := os.ReadFile(migFile)
			if readErr == nil {
				migrationContent[i] = string(content)
			}
		}

		// Detect auth service and generate compatibility SQL
		extDetector := squasher.NewExtensionDetector()
		extAnalysis := extDetector.AnalyzeMigrations(migrationContent)
		if extAnalysis.AuthCompatibilitySQL != "" {
			color.Cyan("🔐 Detected %s authentication - injecting compatibility layer\n", extAnalysis.AuthService)
		}

		// Create validation config with Docker support
		valConfig := validation.DefaultValidationConfig()

		// BUG #6 FIX: Extract PostgreSQL version from docker_image config
		postgresVersion := "17" // Default to 17 if not specified
		if cfg.Validation.DockerImage != "" {
			// Parse version from docker image (e.g., "postgres:17" -> "17")
			parts := strings.Split(cfg.Validation.DockerImage, ":")
			if len(parts) == 2 {
				postgresVersion = parts[1]
			}
		}
		valConfig.PostgreSQLVersion = postgresVersion

		// Use validation mode from flag, config, or default
		mode := cfg.Validation.Mode
		if validationMode != "" {
			mode = validationMode
		}

		// Set Docker approach based on mode
		switch strings.ToUpper(mode) {
		case "TWO_CONTAINERS":
			valConfig.DockerApproach = validation.ApproachTwoContainers
		case "TWO_DATABASES":
			valConfig.DockerApproach = validation.ApproachTwoDatabases
		case "SCHEMA_DIFF":
			valConfig.DockerApproach = validation.ApproachSchemaDiff
		default:
			valConfig.DockerApproach = validation.ApproachTwoDatabases // Default
		}

		valConfig.EnableExtensionDetection = cfg.Validation.EnableExtensionDetection
		valConfig.EnableSQLFixes = cfg.Validation.EnableSQLFixes
		valConfig.Verbose = cfg.Validation.Verbose
		valConfig.AuthCompatibilitySQL = extAnalysis.AuthCompatibilitySQL // Inject auth compatibility

		// Apply container ready timeout from config (convert int seconds to time.Duration)
		if cfg.Validation.ContainerReadyTimeout > 0 {
			valConfig.ContainerReadyTimeout = time.Duration(cfg.Validation.ContainerReadyTimeout) * time.Second
		}

		if mode != "" {
			color.Cyan("🔍 Using validation mode: %s\n", strings.ToUpper(mode))
		}

		validator := validation.NewSchemaValidator(valConfig, nil, nil)
		defer func() { _ = validator.Close() }()

		result, err := validator.ValidateWithDocker(cmd.Context(), originalDir, squashedDir)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Docker validation failed",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithFile(originalDir).WithAdditional("squashed_dir", squashedDir).WithInnerError(err).WithSuggestion("Ensure Docker is running and accessible, or try a different validation mode")
		}

		if result.Success {
			color.Green("☑ Validation successful: Schemas are equivalent.\n")
			fmt.Printf("Validation completed in %v\n", result.Duration)
			if len(result.ExtensionsDetected) > 0 {
				fmt.Printf("Extensions detected: %v\n", result.ExtensionsDetected)
			}
			if len(result.FixesApplied) > 0 {
				fmt.Printf("SQL fixes applied: %d\n", len(result.FixesApplied))
			}
		} else {
			color.Red("✗ Validation failed: Schemas are different.\n")
			if result.DockerValidation != nil && result.DockerValidation.Differences != "" {
				fmt.Println("Differences found:")
				fmt.Println(result.DockerValidation.Differences)
			}
			if len(result.Errors) > 0 {
				fmt.Printf("Errors: %d\n", len(result.Errors))
				for _, err := range result.Errors {
					fmt.Printf("  - %s: %s\n", err.Code, err.Message)
				}
			}
		}

		return nil
	}

	// If we couldn't load migrations, fall back to basic validation
	return errors.NewError(
		errors.ErrorCodeValidationFailed,
		fmt.Sprintf("Failed to load migrations from %s", originalDir),
		errors.SeverityError,
		errors.CategoryValidation,
	).WithFile(originalDir).WithSuggestion("Ensure directory exists and contains .sql files")
}

func runInitConfig(cmd *cobra.Command, args []string) error {
	configFile := "pgsquash.config.json"
	if configPath != "" {
		configFile = configPath
	}

	// Resolve to absolute path to ensure it's created in current working directory
	// If path is relative, resolve it relative to current working directory
	if !filepath.IsAbs(configFile) {
		cwd, err := os.Getwd()
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Failed to get current working directory",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithSuggestion("Try using an absolute path with --config flag")
		}
		configFile = filepath.Join(cwd, configFile)
	}

	if _, err := os.Stat(configFile); err == nil {
		if !forceOverwrite {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				fmt.Sprintf("Config file already exists: %s", configFile),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithFile(configFile).WithSuggestion("Use --force or -f to overwrite the existing configuration")
		}
		// File exists but force flag is set - show warning and continue
		color.Yellow("⚠️  Overwriting existing config file: %s\n", configFile)
	}

	// Create default config
	cfg := config.DefaultConfig()

	// Save to file
	if err := cfg.SaveToFile(configFile); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"Failed to save configuration file",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithFile(configFile).WithInnerError(err).WithSuggestion("Check write permissions in the current directory")
	}

	if forceOverwrite {
		color.Green("☑ Configuration file overwritten: %s\n", configFile)
	} else {
		color.Green("☑ Generated default configuration: %s\n", configFile)
	}
	fmt.Printf("Edit this file to customize pgsquash-engine behavior\n")

	return nil
}

func runAITest(cmd *cobra.Command, args []string) error {
	color.Cyan("🤖 Testing AI Provider Integrations\n")

	// Run the integration test
	ai.RunAIIntegrationTest()

	return nil
}

func runAIDemo(cmd *cobra.Command, args []string) error {
	color.Cyan("🎯 Demonstrating AI Capabilities\n")

	// Run the AI demonstration
	if err := ai.DemoAICapabilities(); err != nil {
		color.Red("☒ AI demonstration failed: %v\n", err)
		return err
	}

	color.Green("✨ AI demonstration completed successfully!\n")
	return nil
}

func runAIFix(cmd *cobra.Command, args []string) error {
	migrationPath := args[0]

	color.Cyan("🤖 AI-Assisted Migration Fixing\n")
	color.Cyan("   Migration path: %s\n", migrationPath)
	color.Cyan("   Max attempts: %d\n", maxFixAttempts)
	color.Cyan("   Auto-apply: %v\n\n", autoApplyFixes)

	// Create provider manager with default config
	providerManager, err := ai.NewProviderManager(nil)
	if err != nil {
		color.Red("☒ Failed to initialize AI providers: %v\n", err)
		color.Yellow("\nℹ️  AI fixing requires API keys. Set one of:\n")
		color.Yellow("   ► ANTHROPIC_API_KEY for Claude\n")
		color.Yellow("   ► OPENAI_API_KEY for OpenAI\n")
		color.Yellow("   ► AZURE_OPENAI_ENDPOINT + AZURE_OPENAI_DEPLOYMENT for Azure\n")
		return err
	}

	// Get default provider
	provider, err := providerManager.GetDefaultProvider()
	if err != nil {
		color.Red("☒ No AI provider available: %v\n", err)
		return err
	}

	// Create migration fixer with validation function
	fixer := ai.NewMigrationFixer(provider, maxFixAttempts, verbose)

	// Create validation function that uses Docker validation
	validationFunc := func(ctx context.Context, path string) error {
		// Load configuration
		_, err := config.LoadConfig(configPath)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Failed to load configuration for AI fix validation",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithFile(configPath).WithInnerError(err)
		}

		// Create temporary output directory for validation
		tmpOutput := filepath.Join(os.TempDir(), fmt.Sprintf("pgsquash_validate_%d", time.Now().Unix()))
		if err := os.MkdirAll(tmpOutput, 0755); err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Failed to create temporary output directory for validation",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithFile(tmpOutput).WithInnerError(err).WithSuggestion("Check temp directory permissions")
		}
		defer func() {
			if err := os.RemoveAll(tmpOutput); err != nil {
				// Log but don't fail on cleanup error
				_ = err
			}
		}()

		// Load and process migrations
		migrations, err := filepath.Glob(filepath.Join(path, "*.sql"))
		if err != nil || len(migrations) == 0 {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				fmt.Sprintf("No SQL files found in %s", path),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithFile(path).WithSuggestion("Ensure the directory contains .sql migration files")
		}

		// Create validation config
		valConfig := validation.DefaultValidationConfig()
		valConfig.DockerApproach = validation.ApproachSchemaDiff // Fast validation for fixing
		valConfig.EnableExtensionDetection = true
		valConfig.EnableSQLFixes = false // Don't auto-fix during validation
		valConfig.Verbose = false        // Quiet during fixing loop

		validator := validation.NewSchemaValidator(valConfig, nil, nil)
		defer func() {
			if err := validator.Close(); err != nil {
				// Log but don't fail on cleanup error
				_ = err
			}
		}()

		// Validate migrations
		result, err := validator.ValidateWithDocker(ctx, path, path)
		if err != nil {
			return err
		}

		if !result.Success || len(result.Errors) > 0 {
			// Return first error
			if len(result.Errors) > 0 {
				return errors.NewError(
					errors.ErrorCodeValidationFailed,
					fmt.Sprintf("%s: %s", result.Errors[0].Code, result.Errors[0].Message),
					errors.SeverityError,
					errors.CategoryValidation,
				).WithFile(path)
			}
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Validation failed",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithFile(path)
		}

		return nil
	}

	// Attach validation function to fixer
	fixer.WithValidation(validationFunc)

	// Run initial validation
	color.Cyan("🔍 Running initial validation...\n")
	ctx := context.Background()
	initialError := validationFunc(ctx, migrationPath)

	if initialError == nil {
		color.Green("☑ Migrations are already valid! No fixes needed.\n")
		return nil
	}

	color.Yellow("⚠️  Validation failed: %v\n", initialError)
	color.Cyan("   Starting AI-powered fixing...\n\n")

	// Run the fixer with automatic validation re-runs
	result, err := fixer.FixMigrationsUntilValid(ctx, migrationPath, initialError)
	if err != nil {
		color.Red("☒ AI fixing failed: %v\n", err)
		return err
	}

	// Display results
	color.Cyan("\n📊 Fix Results:\n")
	color.Cyan("   Total attempts: %d\n", len(result.Attempts))
	color.Cyan("   Successful fixes: %d\n", result.TotalFixes)
	color.Cyan("   Files modified: %d\n\n", len(result.FilesModified))

	if result.Success {
		color.Green("☑ Migrations fixed successfully!\n")
		color.Green("\nℹ️  Modified files:\n")
		for _, file := range result.FilesModified {
			color.Green("   ► %s (backup created)\n", file)
		}
	} else {
		color.Red("☒ Could not fix all migration errors\n")
		color.Red("   Last error: %s\n", result.FinalError)
	}

	return nil
}

// Standardized workflow functions
func runSafeWorkflow(cmd *cobra.Command, args []string) error {
	color.Cyan("🛡️  SAFE Workflow: Production-Ready Migration Squashing\n")

	// Override settings for SAFE workflow
	safetyLevel = "conservative"
	enableBackup = true
	enableRollback = true
	enableTransformation = false // Conservative approach

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		color.Red("☒ Failed to load configuration: %v\n", err)
		return err
	}

	// Apply SAFE workflow settings
	cfg.SafetyLevel = safetyLevel
	cfg.Performance.ShowProgress = true

	// Override output directory if flag is provided
	if workflowOutputDir != "" {
		cfg.Output.Directory = workflowOutputDir
	}

	color.Yellow("📋 SAFE Workflow Configuration:\n")
	color.Yellow("   ► Safety Level: %s (minimal changes)\n", cfg.SafetyLevel)
	color.Yellow("   ► Docker Validation: TWO_CONTAINERS (maximum accuracy)\n")
	color.Yellow("   ► Output Directory: %s\n", cfg.Output.Directory)
	color.Yellow("   ► Backup: %v (pre-squash safety)\n", enableBackup)
	color.Yellow("   ► Rollback: %v (recovery planning)\n", enableRollback)
	color.Yellow("   ► Auto SQL Fix: disabled (manual review required)\n")
	fmt.Println()

	// Execute squash with AI-enhanced validation
	return executeSquashWithAIValidation(args, cfg, "TWO_CONTAINERS")
}

func runFastWorkflow(cmd *cobra.Command, args []string) error {
	color.Cyan("⚡ FAST Workflow: Development-Optimized Migration Squashing\n")

	// Override settings for FAST workflow
	safetyLevel = "standard"
	enableBackup = false
	enableRollback = false
	enableTransformation = true
	streaming = true // Enable for performance
	enableCycleDetection = true

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		color.Red("☒ Failed to load configuration: %v\n", err)
		return err
	}

	// Apply FAST workflow settings
	cfg.SafetyLevel = safetyLevel
	cfg.Performance.ShowProgress = true
	cfg.Performance.ParallelProcessing = true

	// Override output directory if flag is provided
	if workflowOutputDir != "" {
		cfg.Output.Directory = workflowOutputDir
	}

	color.Yellow("📋 FAST Workflow Configuration:\n")
	color.Yellow("   ► Safety Level: %s (balanced optimization)\n", cfg.SafetyLevel)
	color.Yellow("   ► Docker Validation: SCHEMA_DIFF (fastest approach)\n")
	color.Yellow("   ► Output Directory: %s\n", cfg.Output.Directory)
	color.Yellow("   ► Streaming: %v (memory efficient)\n", streaming)
	color.Yellow("   ► DDL Cycle Detection: %v (resolves conflicts)\n", enableCycleDetection)
	color.Yellow("   ► SQL Transformation: %v (modern syntax)\n", enableTransformation)
	color.Yellow("   ► Auto SQL Fix: enabled (automatic corrections)\n")
	fmt.Println()

	// Execute squash with AI-enhanced fast processing
	return executeSquashWithAIOptimization(args, cfg, "SCHEMA_DIFF")
}

func runAnalyzeWorkflow(cmd *cobra.Command, args []string) error {
	color.Cyan("🔍 ANALYZE Workflow: Comprehensive Migration Analysis\n")

	// Override settings for ANALYZE workflow
	enableCycleDetection = true
	cycleDetectionDepth = 10 // Deep analysis
	showCycleDetails = true

	// Load configuration
	cfg, err := config.LoadConfig(configPath)
	if err != nil {
		color.Red("☒ Failed to load configuration: %v\n", err)
		return err
	}

	// Apply ANALYZE workflow settings - no actual modifications
	cfg.Performance.ShowProgress = true

	color.Yellow("📋 ANALYZE Workflow Configuration:\n")
	color.Yellow("   ► DDL Cycle Detection: %v (all algorithm types)\n", enableCycleDetection)
	color.Yellow("   ► Analysis Depth: %d levels\n", cycleDetectionDepth)
	color.Yellow("   ► AI Analysis: enabled if configured (semantic insights)\n")
	color.Yellow("   ► Detailed Reporting: %v (comprehensive findings)\n", showCycleDetails)
	color.Yellow("   ► Mode: Analysis only (no file modifications)\n")
	fmt.Println()

	// Execute AI-powered comprehensive analysis
	return executeAIComprehensiveAnalysis(args, cfg)
}

// AI-Enhanced Helper Functions

func executeSquashWithAIValidation(args []string, cfg *config.Config, validationApproach string) error {
	startTime := time.Now()

	// Load migrations
	migrations, err := loadMigrations(args, cfg.Performance.ShowProgress)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeSyntaxError,
			"Failed to load migration files for SAFE workflow",
			errors.SeverityError,
			errors.CategoryParsing,
		).WithInnerError(err).WithSuggestion("Check migration file syntax and permissions")
	}

	// Initialize AI analyzer
	analyzer, aiErr := ai.NewAnalyzer()
	if aiErr != nil {
		color.Yellow("⚠️  AI analyzer unavailable: %v\n", aiErr)
		color.Yellow("   Proceeding without AI enhancements\n")
		// Fall back to regular validation
		return executeSquashWithValidation(args, cfg, validationApproach)
	}

	color.Cyan("🧠 AI-Enhanced SAFE Processing\n")

	// AI Pre-squash Analysis
	combinedSQL := ""
	for _, mig := range migrations {
		combinedSQL += mig.Content + "\n"
	}

	// Track if AI is available - fail fast on first error
	aiAvailable := true

	// 1. Detect authentication patterns for extra safety (with timeout to avoid hanging)
	authCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	authPatternsResp, err := analyzer.DetectAuthPatterns(authCtx, combinedSQL)
	if err != nil {
		color.Yellow("⚠️  AI pre-analysis unavailable: %v\n", err)
		color.Yellow("   Skipping AI pre-analysis - proceeding with squashing\n")
		aiAvailable = false
	} else if len(authPatternsResp.Patterns) > 0 {
		color.Yellow("🔐 AI detected authentication patterns:\n")
		for _, pattern := range authPatternsResp.Patterns {
			color.Yellow("   ► %s\n", pattern)
		}
		color.Yellow("   Extra validation recommended for auth-related changes\n")
	}

	// Create squasher engine
	engineConfig := squasher.EngineConfig{
		Config:               cfg,
		EnableStreaming:      false,
		EnableTransformation: true,
	}
	engine := squasher.NewEngine(engineConfig)

	// Convert migrations to format expected by engine
	migrationMap := make(map[int]string)
	for i, mig := range migrations {
		migrationMap[i+1] = mig.Content
	}

	// Execute squashing
	finalSQL, warnings, err := engine.Squash(migrationMap)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"Failed to squash migrations in AI-enhanced SAFE workflow",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err).WithSuggestion("Review migration syntax and dependencies")
	}

	// AI Post-squash Safety Analysis (NON-BLOCKING - warnings only)
	// Docker validation is the source of truth. AI provides additional insights but doesn't block deployment.

	// Only run AI validation if AI was available in pre-analysis
	if !aiAvailable {
		color.Yellow("⚠️  Skipping AI post-validation (AI unavailable from pre-analysis)\n")
	} else {
		color.Cyan("🔍 AI Safety Validation...\n")
	}

	aiWarningCount := 0

	// 2. Schema consistency validation (warnings only - only if AI is available)
	if aiAvailable {
		consistencyResp, err := analyzer.ValidateSchemaConsistency(context.Background(), combinedSQL, finalSQL)
		if err != nil {
			// AI validation failed - skip remaining AI calls and continue with Docker validation
			color.Yellow("⚠️  AI validation unavailable: %v\n", err)
			color.Yellow("   Skipping AI safety checks - Docker validation will be the sole validation\n")
			aiAvailable = false
		} else if len(consistencyResp.Differences) > 0 {
			color.Yellow("⚠️  AI detected %d potential schema inconsistencies (review recommended):\n", len(consistencyResp.Differences))
			for i, issue := range consistencyResp.Differences {
				if i < 3 { // Show first 3 to avoid overwhelming output
					color.Yellow("   ► %s\n", issue)
				}
			}
			if len(consistencyResp.Differences) > 3 {
				color.Yellow("   ... and %d more issues\n", len(consistencyResp.Differences)-3)
			}
			color.Yellow("   Note: These are AI suggestions - Docker validation is authoritative\n")
			aiWarningCount += len(consistencyResp.Differences)
		}
	}

	// 3. Conservative dead code detection (warnings only in SAFE mode, and only if AI is available)
	if aiAvailable {
		functions := extractFunctionsFromSQL(finalSQL)
		deadCodeCount := 0
		for _, function := range functions {
			isDead, _, err := analyzer.IsDeadCode(context.Background(), finalSQL, function)
			if err == nil && isDead {
				deadCodeCount++
			}
		}
		if deadCodeCount > 0 {
			color.Yellow("💡 AI detected %d potentially unused functions\n", deadCodeCount)
			color.Yellow("   Manual review recommended before production deployment\n")
			aiWarningCount += deadCodeCount
		}
	}

	// Write output files (same as original)
	outputDir := cfg.Output.Directory
	if outputDir == "" {
		outputDir = "clean_migrations"
	}

	if !dryRun {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Failed to create output directory",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithFile(outputDir).WithInnerError(err).WithSuggestion("Check directory permissions")
		}

		outputFile := filepath.Join(outputDir, "001_consolidated_migration.sql")
		if err := os.WriteFile(outputFile, []byte(finalSQL), 0644); err != nil {
			return errors.NewError(
				errors.ErrorCodeSQLGenerationFailed,
				"Failed to write consolidated migration file",
				errors.SeverityError,
				errors.CategoryConsolidation,
			).WithFile(outputFile).WithInnerError(err).WithSuggestion("Ensure sufficient disk space")
		}

		color.Green("☑ Squashed migrations written to: %s\n", outputFile)
	}

	// Run Docker validation
	if !dryRun {
		color.Cyan("🔍 Running Docker validation with %s approach...\n", validationApproach)

		validationConfig := validation.DefaultValidationConfig()
		validationConfig.EnableExtensionDetection = true
		validationConfig.EnableSQLFixes = false                                  // No auto-fix in SAFE mode
		validationConfig.DockerApproach = validation.ApproachTwoContainers       // SAFE uses TWO_CONTAINERS
		validationConfig.AuthCompatibilitySQL = engine.GetAuthCompatibilitySQL() // Inject auth compatibility
		validationConfig.Verbose = true                                          // Show auth layer creation
		validator := validation.NewSchemaValidator(validationConfig, nil, nil)

		ctx := context.Background()
		result, err := validator.ValidateWithDocker(ctx, filepath.Dir(args[0]), outputDir)
		if err != nil {
			color.Red("☒ Docker validation failed: %v\n", err)
		} else if result != nil && len(result.Errors) == 0 {
			color.Green("☑ Docker validation passed!\n")
		} else {
			color.Yellow("⚠️  Schema differences detected - see validation report\n")
		}
	}

	if aiWarningCount > 0 {
		color.Yellow("🛡️  AI Safety Validation: %d warnings (review recommended, not blocking)\n", aiWarningCount)
	} else {
		color.Green("🛡️  AI Safety Validation: No issues detected\n")
	}

	// Print summary
	sqlLines := strings.Count(finalSQL, "\n")
	outputFile := filepath.Join(outputDir, "001_consolidated_migration.sql")
	printSquashSummary(len(migrations), sqlLines, time.Since(startTime), warnings, outputFile)
	return nil
}

func executeSquashWithAIOptimization(args []string, cfg *config.Config, validationApproach string) error {
	startTime := time.Now()

	// Load migrations
	migrations, err := loadMigrations(args, cfg.Performance.ShowProgress)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeSyntaxError,
			"Failed to load migration files for AI optimization",
			errors.SeverityError,
			errors.CategoryParsing,
		).WithInnerError(err).WithSuggestion("Verify migration files contain valid SQL")
	}

	// Initialize AI analyzer
	analyzer, aiErr := ai.NewAnalyzer()
	if aiErr != nil {
		color.Yellow("⚠️  AI analyzer unavailable: %v\n", aiErr)
		// Fall back to regular processing
		return executeSquashWithValidation(args, cfg, validationApproach)
	}

	color.Cyan("🧠 AI-Enhanced FAST Processing\n")

	// Create squasher engine
	engineConfig := squasher.EngineConfig{
		Config:               cfg,
		EnableStreaming:      false,
		EnableTransformation: true,
	}
	engine := squasher.NewEngine(engineConfig)

	// Convert migrations to format expected by engine
	migrationMap := make(map[int]string)
	combinedSQL := ""
	for i, mig := range migrations {
		migrationMap[i+1] = mig.Content
		combinedSQL += mig.Content + "\n"
	}

	// Execute squashing
	finalSQL, warnings, err := engine.Squash(migrationMap)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"Failed to squash migrations in FAST workflow",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err).WithSuggestion("Try reducing migration complexity or using SAFE workflow")
	}

	// AI-Powered Optimizations
	color.Cyan("⚡ AI Optimization Engine...\n")

	aiAvailable := true // Track if AI is working

	// 1. Function semantic analysis for deduplication
	functions := extractFunctionsFromSQL(finalSQL)
	equivalentPairs := 0
	for i, func1 := range functions {
		if !aiAvailable {
			break // Stop if AI is unavailable
		}
		for j := i + 1; j < len(functions); j++ {
			func2 := functions[j]
			isEquivalent, _, err := analyzer.AreFunctionsSemanticallyEquivalent(context.Background(), func1, func2)
			if err != nil {
				// AI failed - skip remaining AI calls
				color.Yellow("⚠️  AI optimization unavailable: %v\n", err)
				color.Yellow("   Skipping AI optimizations - proceeding with validation\n")
				aiAvailable = false
				break
			}
			if isEquivalent {
				color.Cyan("🔄 AI found equivalent functions: %s ≡ %s\n",
					extractFunctionName(func1), extractFunctionName(func2))
				equivalentPairs++
			}
		}
	}

	// 2. Performance optimization suggestions (only if AI is available)
	var optimizationsResp *ai.OptimizationsResponse
	if aiAvailable {
		resp, err := analyzer.SuggestOptimizations(context.Background(), finalSQL)
		if err != nil {
			color.Yellow("⚠️  AI optimization suggestions unavailable: %v\n", err)
			aiAvailable = false
		} else {
			optimizationsResp = resp
			if len(optimizationsResp.Optimizations) > 0 {
				color.Green("⚡ AI Performance Suggestions:\n")
				for i, opt := range optimizationsResp.Optimizations {
					if i < 5 { // Show top 5 suggestions
						color.Green("   ► %s\n", opt)
					}
				}
				if len(optimizationsResp.Optimizations) > 5 {
					color.Green("   ... and %d more optimizations\n", len(optimizationsResp.Optimizations)-5)
				}
			}
		}
	}

	// 3. Complexity warnings (only if AI is available)
	complexityWarnings := 0
	if aiAvailable {
		for _, mig := range migrations {
			complexityResp, err := analyzer.AnalyzeFunctionComplexity(context.Background(), mig.Content)
			if err == nil && strings.Contains(strings.ToLower(complexityResp.Reasoning), "high") {
				color.Yellow("⚠️  High complexity in %s - consider refactoring\n", mig.FullPath)
				complexityWarnings++
			}
		}
	}

	// Write output files
	outputDir := cfg.Output.Directory
	if outputDir == "" {
		outputDir = "clean_migrations"
	}

	if !dryRun {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Failed to create output directory",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithFile(outputDir).WithInnerError(err).WithSuggestion("Check directory permissions")
		}

		outputFile := filepath.Join(outputDir, "001_consolidated_migration.sql")
		if err := os.WriteFile(outputFile, []byte(finalSQL), 0644); err != nil {
			return errors.NewError(
				errors.ErrorCodeSQLGenerationFailed,
				"Failed to write optimized migration file",
				errors.SeverityError,
				errors.CategoryConsolidation,
			).WithFile(outputFile).WithInnerError(err).WithSuggestion("Ensure sufficient disk space")
		}

		color.Green("☑ Optimized migrations written to: %s\n", outputFile)
	}

	// Fast Docker validation
	if !dryRun {
		color.Cyan("🔍 Running fast Docker validation...\n")

		validationConfig := validation.DefaultValidationConfig()
		validationConfig.EnableExtensionDetection = true
		validationConfig.EnableSQLFixes = true                                   // Enable auto-fix for FAST mode
		validationConfig.DockerApproach = validation.ApproachSchemaDiff          // FAST uses SCHEMA_DIFF
		validationConfig.AuthCompatibilitySQL = engine.GetAuthCompatibilitySQL() // Inject auth compatibility
		validationConfig.Verbose = true                                          // Show auth layer creation
		validator := validation.NewSchemaValidator(validationConfig, nil, nil)

		ctx := context.Background()
		result, err := validator.ValidateWithDocker(ctx, filepath.Dir(args[0]), outputDir)
		if err != nil {
			color.Yellow("⚠️  Validation completed with warnings: %v\n", err)
		} else if result != nil && len(result.Errors) == 0 {
			color.Green("☑ Fast validation passed!\n")
		}
	}

	// AI Summary
	color.Green("⚡ AI Optimization Summary:\n")
	color.Green("   ► Equivalent function pairs found: %d\n", equivalentPairs)
	if optimizationsResp != nil {
		color.Green("   ► Performance optimizations suggested: %d\n", len(optimizationsResp.Optimizations))
	} else {
		color.Yellow("   ► Performance optimizations: AI unavailable\n")
	}
	if complexityWarnings > 0 {
		color.Yellow("   ► High complexity warnings: %d\n", complexityWarnings)
	}

	// Print summary
	sqlLines := strings.Count(finalSQL, "\n")
	outputFile := filepath.Join(outputDir, "001_consolidated_migration.sql")
	printSquashSummary(len(migrations), sqlLines, time.Since(startTime), warnings, outputFile)
	return nil
}

func executeAIComprehensiveAnalysis(args []string, cfg *config.Config) error {
	startTime := time.Now()

	color.Cyan("🔍 AI-Powered Comprehensive Analysis\n")

	// Load migrations
	migrations, err := loadMigrations(args, cfg.Performance.ShowProgress)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeSyntaxError,
			"Failed to load migration files for AI comprehensive analysis",
			errors.SeverityError,
			errors.CategoryParsing,
		).WithInnerError(err).WithSuggestion("Ensure migration files contain valid SQL")
	}

	// Initialize AI analyzer
	analyzer, aiErr := ai.NewAnalyzer()
	if aiErr != nil {
		color.Red("☒ AI analyzer unavailable: %v\n", aiErr)
		// Fall back to basic analysis
		return executeComprehensiveAnalysis(args, cfg)
	}

	// Create combined SQL for analysis
	combinedSQL := ""
	for _, mig := range migrations {
		combinedSQL += mig.Content + "\n"
	}

	color.Cyan("🧠 Deep AI Analysis in progress...\n")

	// 1. Authentication Security Audit
	authPatternsResp, err := analyzer.DetectAuthPatterns(context.Background(), combinedSQL)
	authAnalysis := "No auth patterns detected"
	if err == nil && len(authPatternsResp.Patterns) > 0 {
		authAnalysis = fmt.Sprintf("%d patterns found: %v", len(authPatternsResp.Patterns), authPatternsResp.Patterns)
	}

	// 2. Dead Code Analysis
	functions := extractFunctionsFromSQL(combinedSQL)
	deadCodeCount := 0
	deadFunctions := []string{}
	for _, function := range functions {
		functionName := extractFunctionName(function)
		isDead, _, err := analyzer.IsDeadCode(context.Background(), combinedSQL, functionName)
		if err == nil && isDead {
			deadCodeCount++
			deadFunctions = append(deadFunctions, functionName)
		}
	}

	// 3. Function Complexity Heatmap
	complexityMap := make(map[string]string)
	highComplexityCount := 0
	for _, mig := range migrations {
		complexityResp, err := analyzer.AnalyzeFunctionComplexity(context.Background(), mig.Content)
		if err == nil {
			complexityMap[mig.FullPath] = complexityResp.Reasoning
			if strings.Contains(strings.ToLower(complexityResp.Reasoning), "high") {
				highComplexityCount++
			}
		}
	}

	// 4. Performance Optimization Opportunities
	optimizationsResp, err := analyzer.SuggestOptimizations(context.Background(), combinedSQL)
	optimizationCount := 0
	if err == nil {
		optimizationCount = len(optimizationsResp.Optimizations)
	}

	// 5. Function Semantic Analysis
	equivalentPairs := 0
	for i, func1 := range functions {
		for j := i + 1; j < len(functions); j++ {
			func2 := functions[j]
			isEquivalent, _, err := analyzer.AreFunctionsSemanticallyEquivalent(context.Background(), func1, func2)
			if err == nil && isEquivalent {
				equivalentPairs++
			}
		}
	}

	// 6. Code Coverage Analysis
	coverageIssues := []string{}
	for _, function := range functions[:min(len(functions), 10)] { // Analyze top 10 functions
		coverageResp, err := analyzer.AnalyzeCodeCoverage(context.Background(), function, combinedSQL)
		if err == nil && strings.Contains(strings.ToLower(coverageResp), "unused") {
			coverageIssues = append(coverageIssues, extractFunctionName(function))
		}
	}

	// Enhanced AI Reporting
	color.Cyan("\n🧠 AI Deep Analysis Results\n")
	color.Cyan("=====================================\n")

	fmt.Printf("📊 Migration Files Analyzed: %d\n", len(migrations))
	fmt.Printf("⏱️  Analysis Duration: %v\n", time.Since(startTime))
	fmt.Printf("🔍 Total Functions Found: %d\n", len(functions))
	fmt.Println()

	color.Cyan("🔐 Security Analysis:\n")
	fmt.Printf("   ► Authentication Patterns: %s\n", authAnalysis)
	fmt.Println()

	color.Cyan("🧹 Code Quality Analysis:\n")
	fmt.Printf("   ► Dead Code Functions: %d\n", deadCodeCount)
	if len(deadFunctions) > 0 && len(deadFunctions) <= 5 {
		for _, fn := range deadFunctions {
			fmt.Printf("     - %s\n", fn)
		}
	} else if len(deadFunctions) > 5 {
		for _, fn := range deadFunctions[:5] {
			fmt.Printf("     - %s\n", fn)
		}
		fmt.Printf("     ... and %d more\n", len(deadFunctions)-5)
	}
	fmt.Printf("   ► High Complexity Migrations: %d\n", highComplexityCount)
	fmt.Printf("   ► Semantically Equivalent Function Pairs: %d\n", equivalentPairs)
	fmt.Println()

	color.Cyan("⚡ Performance Analysis:\n")
	fmt.Printf("   ► Optimization Opportunities: %d\n", optimizationCount)
	if optimizationCount > 0 && optimizationsResp != nil {
		fmt.Println("   Top suggestions:")
		for i, opt := range optimizationsResp.Optimizations[:min(len(optimizationsResp.Optimizations), 3)] {
			fmt.Printf("     %d. %s\n", i+1, opt)
		}
	}
	fmt.Printf("   ► Coverage Issues: %d functions with low usage\n", len(coverageIssues))
	fmt.Println()

	// Strategic Recommendations
	color.Cyan("💡 AI Recommendations:\n")
	if deadCodeCount > 0 {
		color.Yellow("   ► Run FAST workflow to automatically optimize %d functions\n", equivalentPairs)
	}
	if len(authPatternsResp.Patterns) > 0 {
		color.Yellow("   ► Use SAFE workflow for production - auth patterns detected\n")
	}
	if optimizationCount > 10 {
		color.Green("   ► High optimization potential - FAST workflow recommended\n")
	} else if optimizationCount == 0 {
		color.Green("   ► Migrations appear well-optimized\n")
	}

	color.Green("\n✨ AI Analysis Complete!\n")
	return nil
}

// Helper functions for standardized workflows

func executeSquashWithValidation(args []string, cfg *config.Config, validationApproach string) error {
	startTime := time.Now()

	// Load migrations
	migrations, err := loadMigrations(args, cfg.Performance.ShowProgress)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeSyntaxError,
			"Failed to load migration files",
			errors.SeverityError,
			errors.CategoryParsing,
		).WithInnerError(err).WithSuggestion("Check migration file syntax and permissions")
	}

	// Create squasher engine
	engineConfig := squasher.EngineConfig{
		Config:               cfg,
		EnableStreaming:      false,
		EnableTransformation: true,
	}
	engine := squasher.NewEngine(engineConfig)

	// Convert migrations to format expected by engine
	migrationMap := make(map[int]string)
	for i, mig := range migrations {
		migrationMap[i+1] = mig.Content
	}

	// Execute squashing
	finalSQL, warnings, err := engine.Squash(migrationMap)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"Failed to squash migrations",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err).WithSuggestion("Check migration syntax and dependency graph")
	}

	// Write output files
	outputDir := cfg.Output.Directory
	if outputDir == "" {
		outputDir = "clean_migrations"
	}

	if !dryRun {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Failed to create output directory",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithFile(outputDir).WithInnerError(err).WithSuggestion("Check directory permissions")
		}

		outputFile := filepath.Join(outputDir, "001_consolidated_migration.sql")
		if err := os.WriteFile(outputFile, []byte(finalSQL), 0644); err != nil {
			return errors.NewError(
				errors.ErrorCodeSQLGenerationFailed,
				"Failed to write consolidated migration file",
				errors.SeverityError,
				errors.CategoryConsolidation,
			).WithFile(outputFile).WithInnerError(err).WithSuggestion("Ensure sufficient disk space")
		}

		color.Green("☑ Squashed migrations written to: %s\n", outputFile)
	}

	// Run validation if not dry run
	if !dryRun {
		color.Cyan("🔍 Running Docker validation with %s approach...\n", validationApproach)

		validationConfig := validation.DefaultValidationConfig()
		validationConfig.EnableExtensionDetection = true
		validationConfig.EnableSQLFixes = validationApproach == "SCHEMA_DIFF" // Auto-fix only for fast approach

		// Set Docker approach based on workflow
		switch validationApproach {
		case "TWO_CONTAINERS":
			validationConfig.DockerApproach = validation.ApproachTwoContainers
		case "TWO_DATABASES":
			validationConfig.DockerApproach = validation.ApproachTwoDatabases
		case "SCHEMA_DIFF":
			validationConfig.DockerApproach = validation.ApproachSchemaDiff
		}

		validationConfig.AuthCompatibilitySQL = engine.GetAuthCompatibilitySQL() // Inject auth compatibility
		validationConfig.Verbose = true                                          // Show auth layer creation

		validator := validation.NewSchemaValidator(validationConfig, nil, nil)

		// Get the original migrations directory and the output file for validation
		originalDir := filepath.Dir(args[0])
		outputFile := filepath.Join(outputDir, "001_consolidated_migration.sql")

		ctx := context.Background()
		result, err := validator.ValidateWithDocker(ctx, originalDir, outputFile)
		if err != nil {
			color.Red("☒ Validation failed: %v\n", err)
		} else if result != nil && len(result.Errors) == 0 {
			color.Green("☑ Schema validation passed!\n")
		} else {
			color.Yellow("⚠️  Schema differences detected - see validation report\n")
		}
	}

	// Print summary
	sqlLines := strings.Count(finalSQL, "\n")
	outputFile := filepath.Join(outputDir, "001_consolidated_migration.sql")
	printSquashSummary(len(migrations), sqlLines, time.Since(startTime), warnings, outputFile)
	return nil
}

func executeComprehensiveAnalysis(args []string, cfg *config.Config) error {
	startTime := time.Now()

	color.Cyan("🔍 Loading and parsing migrations...\n")

	// Load migrations
	migrations, err := loadMigrations(args, cfg.Performance.ShowProgress)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeSyntaxError,
			"Failed to load migration files for comprehensive analysis",
			errors.SeverityError,
			errors.CategoryParsing,
		).WithInnerError(err).WithSuggestion("Verify migration files are valid SQL")
	}

	// Create squasher engine for analysis (used for dependency analysis)
	engineConfig := squasher.EngineConfig{
		Config:               cfg,
		EnableStreaming:      false,
		EnableTransformation: false, // Analysis only, no transformation
	}
	engine := squasher.NewEngine(engineConfig)

	// Convert migrations to format expected by engine
	migrationMap := make(map[int]string)
	for i, mig := range migrations {
		migrationMap[i+1] = mig.Content
	}

	// Create tracker for analysis
	tracker := engine.GetTracker()

	// Process all migrations through the tracker
	for i, mig := range migrations {
		tracker.ProcessMigration(mig.Migration, i)
	}

	color.Cyan("🔬 Performing comprehensive analysis...\n")

	// Get actual analysis results from the engine/tracker
	var warnings []string
	var analysisResults []string

	// Real DDL cycle detection
	if enableCycleDetection {
		depGraph := tracker.GetActualDependencyGraph()
		cycles := depGraph.DetectCycles()
		if len(cycles) > 0 {
			analysisResults = append(analysisResults, fmt.Sprintf("⚠️  DDL Cycles Detected: %d cycles found", len(cycles)))
			color.Yellow("  ⚠️  Found %d DDL cycles\n", len(cycles))
			for i, cycle := range cycles {
				if i < 5 { // Show first 5 cycles
					warnings = append(warnings, fmt.Sprintf("DDL Cycle %d: %v", i+1, cycle))
				}
			}
		} else {
			analysisResults = append(analysisResults, "☑ DDL Cycle Detection: No harmful cycles detected")
			color.Green("  ☑ DDL cycle detection completed - no cycles found\n")
		}
	}

	// Real dependency analysis
	stats := tracker.GetStatistics()
	analysisResults = append(analysisResults, fmt.Sprintf("☑ Dependency Analysis: %d objects tracked, %d dependencies resolved", stats.TotalObjects, stats.TotalDependencies))
	color.Green("  ☑ Dependency graph analysis completed\n")

	// Real redundancy analysis
	redundancies := tracker.GetRedundantObjects()
	if len(redundancies) > 0 {
		analysisResults = append(analysisResults, fmt.Sprintf("☑ Redundancy Detection: %d redundant operations identified", len(redundancies)))
		color.Green("  ☑ Found %d optimization opportunities\n", len(redundancies))
	} else {
		analysisResults = append(analysisResults, "☑ Redundancy Detection: No redundancies found")
		color.Green("  ☑ Migrations are already optimized\n")
	}

	// Real risk assessment
	consistencyWarnings := tracker.ValidateConsistency()
	if len(consistencyWarnings) > 0 {
		analysisResults = append(analysisResults, fmt.Sprintf("⚠️  Risk Assessment: %d consistency warnings detected", len(consistencyWarnings)))
		color.Yellow("  ⚠️  %d potential risks identified\n", len(consistencyWarnings))
		warnings = append(warnings, consistencyWarnings...)
	} else {
		analysisResults = append(analysisResults, "☑ Risk Assessment: Low risk - migrations are consistent")
		color.Green("  ☑ Risk assessment completed\n")
	}

	// Print detailed analysis report
	color.Cyan("\n📊 Analysis Results:\n")
	color.Cyan("=====================================\n")

	fmt.Printf("Migration Files Analyzed: %d\n", len(migrations))
	fmt.Printf("Analysis Depth: %d levels\n", cycleDetectionDepth)
	fmt.Printf("Analysis Duration: %v\n", time.Since(startTime))
	fmt.Println()

	for _, result := range analysisResults {
		color.Green("%s\n", result)
	}

	if len(warnings) > 0 {
		fmt.Println()
		color.Yellow("⚠️  Warnings:\n")
		for _, warning := range warnings {
			color.Yellow("  ► %s\n", warning)
		}
	} else {
		fmt.Println()
		color.Green("✨ No warnings detected - migrations appear well-structured\n")
	}

	color.Cyan("\n💡 Recommendations:\n")
	color.Cyan("► Consider using 'fast' workflow for development environments\n")
	color.Cyan("► Use 'safe' workflow for production deployments\n")
	color.Cyan("► Review any warnings before proceeding with consolidation\n")

	return nil
}

// Helper functions for AI integration

func extractFunctionsFromSQL(sql string) []string {
	// Simple function extraction - could be enhanced with proper parsing
	functions := []string{}
	lines := strings.Split(sql, "\n")
	var currentFunction strings.Builder
	inFunction := false

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToUpper(line), "CREATE OR REPLACE FUNCTION") ||
			strings.HasPrefix(strings.ToUpper(line), "CREATE FUNCTION") {
			inFunction = true
			currentFunction.Reset()
			currentFunction.WriteString(line + "\n")
		} else if inFunction {
			currentFunction.WriteString(line + "\n")
			if strings.HasSuffix(strings.ToUpper(line), "$$;") ||
				strings.HasSuffix(strings.ToUpper(line), "$BODY$;") {
				functions = append(functions, currentFunction.String())
				inFunction = false
			}
		}
	}

	return functions
}

func extractFunctionName(functionSQL string) string {
	// Extract function name from CREATE FUNCTION statement
	lines := strings.Split(functionSQL, "\n")
	if len(lines) > 0 {
		firstLine := strings.TrimSpace(lines[0])
		// Parse "CREATE [OR REPLACE] FUNCTION function_name("
		parts := strings.Fields(firstLine)
		for i, part := range parts {
			if strings.ToUpper(part) == "FUNCTION" && i+1 < len(parts) {
				funcNamePart := parts[i+1]
				if idx := strings.Index(funcNamePart, "("); idx > 0 {
					return funcNamePart[:idx]
				}
				return funcNamePart
			}
		}
	}
	return "unknown_function"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// MigrationWithContent contains both parsed migration and original file content
type MigrationWithContent struct {
	*types.Migration
	Content  string
	FullPath string
}

func loadSingleMigration(file string) (*MigrationWithContent, error) {
	content, err := os.ReadFile(file)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("Failed to read migration file: %s", file),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithFile(file).WithInnerError(err).WithSuggestion("Check file exists and is readable")
	}

	m, err := parser.ParseMigration(string(content), filepath.Base(file))
	if err != nil {
		// Check if this is a catastrophic failure (no statements parsed)
		if m != nil && len(m.ParseErrors) > 0 && len(m.Statements) == 0 {
			// Catastrophic failure - all statements lost
			color.Red("☒ FATAL: %s - All statements failed to parse\n", filepath.Base(file))
			color.Red("   Parse errors: %d, Statements recovered: 0\n", len(m.ParseErrors))
			for i, parseErr := range m.ParseErrors {
				if i < 3 { // Show first 3 errors
					color.Red("   ► %s\n", parseErr)
				}
			}
			if len(m.ParseErrors) > 3 {
				color.Red("   ... and %d more errors\n", len(m.ParseErrors)-3)
			}
			return nil, errors.NewError(
				errors.ErrorCodeSyntaxError,
				fmt.Sprintf("Catastrophic parse failure in %s: all statements lost", file),
				errors.SeverityCritical,
				errors.CategoryParsing,
			).WithFile(file).WithAdditional("parse_errors", len(m.ParseErrors)).WithSuggestion("Review SQL syntax - all statements failed to parse")
		}

		if m != nil && len(m.ParseErrors) > 0 {
			// Migration was partially parsed - include it with errors and show warning
			color.Yellow("⚠️  Warning: %s has %d parse error(s) but %d statements were successfully parsed\n",
				filepath.Base(file), len(m.ParseErrors), len(m.Statements))
			for i, parseErr := range m.ParseErrors {
				if i < 3 { // Show first 3 errors to avoid overwhelming output
					color.Yellow("   ► %s\n", parseErr)
				}
			}
			if len(m.ParseErrors) > 3 {
				color.Yellow("   ... and %d more errors\n", len(m.ParseErrors)-3)
			}
			return &MigrationWithContent{
				Migration: m,
				Content:   string(content),
				FullPath:  file,
			}, nil
		}
		// Fatal error - can't parse at all
		return nil, errors.NewError(
			errors.ErrorCodeSyntaxError,
			fmt.Sprintf("Fatal parse error in %s", file),
			errors.SeverityCritical,
			errors.CategoryParsing,
		).WithFile(file).WithInnerError(err).WithSuggestion("Check SQL syntax using a PostgreSQL validator")
	}

	return &MigrationWithContent{
		Migration: m,
		Content:   string(content),
		FullPath:  file,
	}, nil
}

func loadMigrations(files []string, showProgress bool) ([]*MigrationWithContent, error) {
	migrations := make([]*MigrationWithContent, 0, len(files))

	for i, file := range files {
		if showProgress && len(files) > 5 {
			fmt.Printf("\rLoading migrations... %d/%d", i+1, len(files))
		}

		migration, err := loadSingleMigration(file)
		if err != nil {
			return nil, err
		}
		migrations = append(migrations, migration)
	}

	if showProgress && len(files) > 5 {
		fmt.Printf("\rLoaded %d migrations successfully\n", len(files))
	}

	return migrations, nil
}

func printAnalysisReport(
	migrations []*types.Migration,
	redundancies []tracking.RedundancyReport,
	stats tracking.TrackerStats,
	warnings []string,
) {
	fmt.Print("\n" + color.BlueString("=== Migration Analysis Report ===") + "\n\n")

	// Check for parse errors first
	parseErrorCount := 0
	for _, m := range migrations {
		parseErrorCount += len(m.ParseErrors)
	}

	if parseErrorCount > 0 {
		fmt.Print(color.RedString("⚠️  Parse Errors: %d\n\n", parseErrorCount))
		for _, m := range migrations {
			if len(m.ParseErrors) > 0 {
				fmt.Printf("%s", color.RedString("  File: %s\n", m.Filename))
				for _, parseErr := range m.ParseErrors {
					fmt.Printf("    ► %s\n", parseErr)
				}
				fmt.Println()
			}
		}
	}

	// Basic statistics
	fmt.Printf("Files analyzed: %s\n", color.CyanString("%d", len(migrations)))
	fmt.Printf("Total statements: %s\n", color.CyanString("%d", stats.TotalStatements))
	fmt.Printf("Data operations: %s\n", color.CyanString("%d", stats.DataOperations))

	// Objects by type
	fmt.Printf("\nObjects by type:\n")
	for objType, count := range stats.ObjectsByType {
		fmt.Printf("  ► %s: %d\n", objType, count)
	}

	// Redundancies
	fmt.Print("\n" + color.YellowString(fmt.Sprintf("Redundancies found: %d", len(redundancies))) + "\n")

	if len(redundancies) > 0 {
		totalSavings := 0
		for _, r := range redundancies {
			fmt.Printf("  ► %s (%s): %s\n",
				color.WhiteString(r.Object), r.Type, r.Explanation)
			fmt.Printf("    Potential savings: %d statements\n", r.Savings.StatementsReduced)
			totalSavings += r.Savings.StatementsReduced
		}

		fmt.Printf("\nTotal potential statement reduction: %s\n",
			color.GreenString("%d", totalSavings))
	} else {
		color.Green("  No redundancies detected\n")
	}

	// Warnings
	if len(warnings) > 0 {
		fmt.Print("\n" + color.RedString("Warnings:") + "\n")
		for _, warning := range warnings {
			fmt.Printf("  ⚠ %s\n", warning)
		}
	}
}

func printSquashSummary(originalFiles, finalLines int, duration time.Duration, warnings []string, outputPath string) {
	fmt.Print("\n" + color.GreenString("☑ Squashing completed successfully!") + "\n\n")

	fmt.Printf("Results:\n")
	fmt.Printf("  ► Original files processed: %d\n", originalFiles)
	fmt.Printf("  ► Final lines of SQL: %d\n", finalLines)
	fmt.Printf("  ► Processing time: %v\n", duration)

	if len(warnings) > 0 {
		wm := utils.NewWarningManager()
		wm.AddRawWarnings(warnings)

		// Separate informational items (backups, rollbacks, transformations) from actual warnings
		byCategory := wm.GetWarningsByCategory()

		// Show safety and transformation features
		if backupWarns := byCategory[utils.CategoryBackup]; len(backupWarns) > 0 {
			fmt.Print("\n" + color.BlueString("🛡 Safety Features:") + "\n")
			for _, w := range backupWarns {
				cleanMsg := strings.Replace(w.Message, "Backup created: ", "Database backup: ", 1)
				fmt.Printf("  ☑ %s\n", cleanMsg)
			}
		}

		if rollbackWarns := byCategory[utils.CategoryRollback]; len(rollbackWarns) > 0 {
			fmt.Print("\n" + color.BlueString("🔄 Rollback Capabilities:") + "\n")
			for _, w := range rollbackWarns {
				fmt.Printf("  ☑ %s\n", w.Message)
			}
		}

		if transformWarns := byCategory[utils.CategoryTransformation]; len(transformWarns) > 0 {
			fmt.Print("\n" + color.CyanString("⚡ SQL Transformations:") + "\n")
			for _, w := range transformWarns {
				cleanMsg := strings.Replace(w.Message, "Transformation: ", "", 1)
				fmt.Printf("  ☑ %s\n", cleanMsg)
			}
		}

		if optWarns := byCategory[utils.CategoryOptimization]; len(optWarns) > 0 {
			fmt.Print("\n" + color.CyanString("⚡ Optimizations Applied:") + "\n")
			for _, w := range optWarns {
				fmt.Printf("  ☑ %s\n", w.Message)
			}
		}

		// Show cycle detection results
		if cycleWarns := byCategory[utils.CategoryCycle]; len(cycleWarns) > 0 {
			fmt.Print("\n" + color.YellowString("🔍 DDL Cycle Detection:") + "\n")
			for _, w := range cycleWarns {
				switch w.Severity {
				case utils.SeverityCritical:
					fmt.Printf("  "+color.RedString("⚠ %s")+"\n", w.Message)
				case utils.SeverityHigh:
					fmt.Printf("  "+color.YellowString("⚠ %s")+"\n", w.Message)
				default:
					fmt.Printf("  ℹ %s\n", w.Message)
				}
				if w.Suggestion != "" {
					fmt.Printf("    → %s\n", color.CyanString(w.Suggestion))
				}
			}
		}

		// Show actual warnings (deduplicated and categorized)
		var actualWarnings []*errors.StructuredError
		for _, w := range wm.GetWarnings() {
			// Skip categories we already displayed separately
			if w.Category != errors.CategoryBackup &&
				w.Category != errors.CategoryRollback &&
				w.Category != errors.CategoryTransformation &&
				w.Category != errors.CategoryOptimization &&
				w.Category != errors.CategoryCycle {
				actualWarnings = append(actualWarnings, w)
			}
		}

		if len(actualWarnings) > 0 {
			// Group remaining warnings by severity
			bySeverity := make(map[errors.Severity][]*errors.StructuredError)
			for _, w := range actualWarnings {
				bySeverity[w.Severity] = append(bySeverity[w.Severity], w)
			}

			fmt.Print("\n" + color.YellowString("⚠ Warnings:") + "\n")

			// Critical warnings
			if critical := bySeverity[utils.SeverityCritical]; len(critical) > 0 {
				fmt.Print("\n" + color.RedString("  🔴 Critical (%d):", len(critical)) + "\n")
				for _, w := range critical {
					fmt.Printf("    ► %s\n", w.Message)
					if w.Suggestion != "" {
						fmt.Printf("      → %s\n", color.CyanString(w.Suggestion))
					}
				}
			}

			// High severity
			if high := bySeverity[utils.SeverityHigh]; len(high) > 0 {
				fmt.Print("\n" + color.YellowString("  🟠 High Severity (%d):", len(high)) + "\n")
				for _, w := range high {
					fmt.Printf("    ► %s\n", w.Message)
					if w.Suggestion != "" {
						fmt.Printf("      → %s\n", color.CyanString(w.Suggestion))
					}
				}
			}

			// Medium severity
			if medium := bySeverity[utils.SeverityMedium]; len(medium) > 0 {
				fmt.Print("\n" + color.YellowString("  🟡 Medium Severity (%d):", len(medium)) + "\n")
				for _, w := range medium {
					fmt.Printf("    ► %s\n", w.Message)
				}
			}

			// Low/Info
			if low := bySeverity[utils.SeverityLow]; len(low) > 0 {
				fmt.Printf("\n  ℹ️  Info (%d):\n", len(low))
				for _, w := range low {
					fmt.Printf("    ► %s\n", w.Message)
				}
			}
		}
	}

	// Show enabled features
	fmt.Print("\n" + color.MagentaString("🎯 Features Enabled:") + "\n")
	if enableBackup {
		fmt.Printf("  ☑ Pre-squash backup generation (requires prod_db_dsn in config)\n")
	}
	if enableRollback {
		fmt.Printf("  ☑ Rollback script generation\n")
	}
	if enableTransformation {
		fmt.Printf("  ☑ SQL transformation and modernization\n")
	}
	if enableCycleDetection {
		fmt.Printf("  ☑ Advanced DDL cycle detection\n")
	}

	fmt.Printf("\nOutput written to: %s\n", color.CyanString(outputPath))
}

// createBackupConfig creates backup configuration from CLI flags
func createBackupConfig() *transformation.BackupConfig {
	if !enableBackup {
		return nil
	}

	config := transformation.DefaultBackupConfig()

	// Set backup type based on CLI context
	config.Type = transformation.SchemaOnly
	config.IncludeDrops = false // Conservative for safety
	config.VerboseOutput = verbose

	// Note: backupPath is handled by the BackupGenerator itself

	return config
}

// createTransformationConfig creates transformation configuration from CLI flags
func createTransformationConfig() *transformation.TransformationConfig {
	if !enableTransformation {
		return nil
	}

	config := transformation.DefaultTransformationConfig()

	// Enable all safe transformations by default
	config.EnableDMLToSelect = false   // Don't modify DML in migrations
	config.EnableDropToComment = false // Don't convert drops to comments
	config.EnableUnsafeToSafe = true   // Convert unsafe operations to safe ones
	config.EnableModernSyntax = true   // Use modern PostgreSQL syntax
	config.EnablePerformance = true    // Apply performance optimizations

	return config
}
