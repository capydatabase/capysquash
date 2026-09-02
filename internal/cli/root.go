package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"

	"github.com/capysquash/pgsquash-engine/internal/config"
	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/parser"
	"github.com/capysquash/pgsquash-engine/internal/squasher"
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/transformation"
	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/capysquash/pgsquash-engine/internal/utils"
	"github.com/capysquash/pgsquash-engine/internal/validation"
	engineapi "github.com/capysquash/pgsquash-engine/pkg/engine"
	harnesscontract "github.com/capysquash/pgsquash-engine/pkg/harness"
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

	// Validation options
	validationMode         string
	workflowOutputDir      string
	noValidate             bool
	failOnDiff             bool
	strictParse            bool
	openReport             bool
	customDockerImage      string
	emitHarnessReport      bool
	harnessReportPath      string
	externalDSN            string
	externalDSNEnv         string
	snapshotOutput         string
	againstSnapshot        string
	externalJSON           bool
	externalAllowedSchemas []string

	// Branch safety options
	branchCheck      bool
	iKnowWhatImDoing bool

	// Init-config options
	forceOverwrite bool

	// Output options
	quietMode  bool
	noEmoji    bool
	squashJSON bool

	// TUI mode
	tuiMode bool
)

var rootCmd = &cobra.Command{
	Use:     "pgsquash",
	Short:   "pgsquash-engine - Intelligent PostgreSQL migration consolidation",
	Version: "0.9.7",
	Long: `pgsquash-engine intelligently consolidates PostgreSQL migration files into
clean, production-ready SQL while preserving data integrity, respecting
dependencies, and validating safety at every step.

The pgsquash-engine can be used directly or embedded through its public Go API.`,
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

var validateExternalCmd = &cobra.Command{
	Use:   "validate-external [migrations path]",
	Short: "Apply migrations to an empty external database and capture or compare its catalog",
	Long: `Apply migrations only after confirming that the caller-owned database is empty.

Use --snapshot-output to capture the original catalog, then reset or replace the
database and use --against-snapshot to validate a generated baseline. Connection
details are never included in the snapshot or JSON result.`,
	Args: cobra.ExactArgs(1),
	RunE: runValidateExternal,
}

var lintCmd = &cobra.Command{
	Use:   "lint [migration files or directories...]",
	Short: "Run static SQL lint rules on migrations",
	Long: `Run static AST-based lint checks against migration SQL files.

Examples:
  pgsquash lint ./migrations
  pgsquash lint ./migrations/*.sql --enable-rule CSQ.SAFETY.CONCURRENT_INDEX
  pgsquash lint ./migrations --fix --write
`,
	Args: cobra.MinimumNArgs(1),
	RunE: runLint,
}

var initConfigCmd = &cobra.Command{
	Use:   "init-config",
	Short: "Generate default configuration file",
	Long:  `Create a default pgsquash.config.json file with all available options.`,
	RunE:  runInitConfig,
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
		"Safety level: paranoid, conservative, standard, aggressive (overrides config)")
	squashCmd.Flags().StringVarP(&outputDir, "output", "o", "",
		"Output directory (overrides config)")
	squashCmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Show what would be done without writing files")
	squashCmd.Flags().BoolVar(&squashJSON, "json", false,
		"Emit machine-readable dry-run output as JSON (requires --dry-run)")
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
		"Directory for pg_dump backups, created if missing (default: <output>/.backups; requires --backup)")
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
	squashCmd.Flags().BoolVar(&failOnDiff, "fail-on-diff", true,
		"Exit non-zero when post-squash validation detects real schema differences (default true; pass --fail-on-diff=false to downgrade to a warning)")
	squashCmd.Flags().BoolVar(&strictParse, "strict-parse", false,
		"Fail if any migration file has partial parse errors")
	squashCmd.Flags().BoolVar(&openReport, "open-report", false,
		"Open validation report in $EDITOR after validation")
	squashCmd.Flags().StringVar(&customDockerImage, "docker-image", "",
		"Custom Docker image with pre-installed extensions (e.g., 'myregistry/postgres-postgis:17')")
	squashCmd.Flags().BoolVar(&emitHarnessReport, "emit-context", false,
		"Emit deterministic AI harness context artifact after squash")
	squashCmd.Flags().StringVar(&harnessReportPath, "context-output", "",
		"Path to write deterministic context artifact (default: <output>/.capysquash.context.v1.json)")

	// Branch safety flags
	squashCmd.Flags().BoolVar(&branchCheck, "branch-check", false,
		"Enforce protected branch requirement (fails if not on main/master)")
	squashCmd.Flags().BoolVar(&iKnowWhatImDoing, "i-know-what-im-doing", false,
		"Override all safety warnings (use with caution)")

	// Validate command flags
	validateCmd.Flags().StringVar(&validationMode, "validation-mode", "",
		"Validation approach: TWO_CONTAINERS, TWO_DATABASES, or SCHEMA_DIFF (default: from config or TWO_DATABASES)")
	validateExternalCmd.Flags().StringVar(&externalDSN, "dsn", "",
		"Connection URL for a caller-owned empty validation database")
	validateExternalCmd.Flags().StringVar(&externalDSNEnv, "dsn-env", "",
		"Read the validation database connection URL from this environment variable")
	validateExternalCmd.Flags().StringVar(&snapshotOutput, "snapshot-output", "",
		"Write the captured catalog snapshot to this file")
	validateExternalCmd.Flags().StringVar(&againstSnapshot, "against-snapshot", "",
		"Compare the captured catalog against this snapshot file")
	validateExternalCmd.Flags().BoolVar(&externalJSON, "json", false,
		"Emit the stable external-validation result as JSON")
	validateExternalCmd.Flags().StringSliceVar(&externalAllowedSchemas, "allow-existing-schema", nil,
		"Platform-owned schema allowed in the otherwise empty validation database (repeatable)")
	squashCmd.Flags().StringVar(&validationMode, "validation-mode", "",
		"Validation approach for post-squash validation: TWO_CONTAINERS, TWO_DATABASES, or SCHEMA_DIFF (default: from config)")

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

	// Static validation flags
	validateCmd.Flags().StringSlice("enable-rule", nil, "Enable specific static validation rules")
	validateCmd.Flags().StringSlice("disable-rule", nil, "Disable specific static validation rules")
	validateCmd.Flags().Bool("strict", false, "Treat all validation warnings as errors")
	lintCmd.Flags().StringSlice("enable-rule", nil, "Enable specific static validation rules")
	lintCmd.Flags().StringSlice("disable-rule", nil, "Disable specific static validation rules")
	lintCmd.Flags().Bool("strict", false, "Treat all lint violations as errors")
	lintCmd.Flags().Bool("fix", false, "Apply available autofixes in memory")
	lintCmd.Flags().Bool("write", false, "Write autofix output back to files (requires --fix)")
	lintCmd.Flags().Bool("json", false, "Emit lint output as JSON")
	rootCmd.AddCommand(analyzeCmd, squashCmd, validateCmd, validateExternalCmd, lintCmd, initConfigCmd, safeCmd, fastCmd, analyzeDeepCmd)
}

func Execute() error {
	// Configure global logging based on verbose flag
	// This is called before any command runs via PersistentPreRun
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if quietMode || squashJSON || externalJSON {
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

	// Load configuration to honor performance settings (streaming threshold,
	// show_progress) during analysis.
	cfg, err := config.LoadConfig(resolveConfigPath())
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"Failed to load configuration",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithFile(configPath).WithInnerError(err).WithSuggestion("Run 'pgsquash init-config' to generate a valid configuration file")
	}

	if !cmd.Flags().Changed("progress") {
		showProgress = cfg.Performance.ShowProgress
	}

	if verbose {
		fmt.Printf("Loading migrations from %d files...\n", len(args))
	}

	var t *tracking.Tracker
	var migrations []*MigrationWithContent

	// Use streaming for large datasets or when explicitly requested
	useStreaming, autoStreamReason := streaming, ""
	if !useStreaming {
		useStreaming, autoStreamReason = shouldAutoStream(args, cfg)
	}
	if useStreaming {
		// Show streaming mode indicator
		if streaming {
			color.Cyan("🚀 Streaming mode: enabled (memory limit: %dMB)\n", memoryLimitMB)
		} else {
			color.Cyan("🚀 Auto-enabling streaming mode: %s\n", autoStreamReason)
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
	if squashJSON && !dryRun {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"--json requires --dry-run",
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}
	if squashJSON && explainMode {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"--json cannot be combined with --explain",
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}
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
	validationStatus := "skipped"
	validationModeUsed := ""
	objectsConsolidated := 0

	// If explain mode is enabled, imply dry-run
	if explainMode {
		dryRun = true
		validationStatus = "dry_run"
	}

	// Enforce branch safety checks for non-dry-run squashing.
	// Dry-run/explain modes do not mutate output files and skip branch gating.
	if !dryRun {
		branchSafety := NewBranchSafetyChecker()
		safeToProceed, branchErr := branchSafety.CheckBranchSafety(branchCheck, iKnowWhatImDoing)
		if branchErr != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Branch safety check failed",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(branchErr).WithSuggestion("Use --i-know-what-im-doing only when you intentionally want to bypass branch safety prompts")
		}

		if !safeToProceed {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Squash cancelled due to branch safety policy",
				errors.SeverityWarning,
				errors.CategoryValidation,
			)
		}
	}

	// Load configuration
	cfg, err := config.LoadConfig(resolveConfigPath())
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"Failed to load configuration",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithFile(configPath).WithInnerError(err).WithSuggestion("Run 'pgsquash init-config' to generate a valid configuration file")
	}

	// Override config with command line flags.
	// The --safety flag is validated here: an unknown value must be rejected,
	// never silently accepted (which used to disable all consolidation rules).
	if err := applySafetyOverride(cfg, safetyLevel); err != nil {
		return err
	}
	if outputDir != "" {
		cfg.Output.Directory = outputDir
	} else if cfg.Output.Directory == "squashed" {
		if !verbose && !squashJSON {
			color.Cyan("ℹ️  Output directory not specified, using default: ./squashed\n")
		}
	}

	// CLI --validation-mode overrides the config validation mode.
	if err := applyValidationModeOverride(cfg, validationMode); err != nil {
		return err
	}

	// Config show_progress wins when --progress was not explicitly set.
	if !cmd.Flags().Changed("progress") {
		showProgress = cfg.Performance.ShowProgress
	}

	validationModeUsed = strings.ToUpper(strings.TrimSpace(cfg.Validation.Mode))
	if validationModeUsed == "" {
		validationModeUsed = "TWO_DATABASES"
	}

	// Auto-detect worker count if not specified
	if workerCount == 0 {
		workerCount = runtime.NumCPU()
	}

	// Dry-run must not write anything anywhere: disable every write-producing
	// side feature (pg_dump backups and rollback plan files).
	if dryRun {
		if (enableBackup || enableRollback) && !squashJSON {
			color.Yellow("ℹ️  --dry-run: skipping backup and rollback generation (no files are written)\n")
		}
		enableBackup = false
		enableRollback = false
	}

	// --backup-path only has an effect together with --backup; a silently
	// ignored flag is a footgun, so reject the inconsistent combination.
	if backupPath != "" && !enableBackup && !dryRun {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"--backup-path requires --backup",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Add --backup to enable backup generation, or drop --backup-path")
	}

	if verbose {
		fmt.Printf("Loading migrations from %d files...\n", len(args))
		fmt.Printf("Safety level: %s\n", cfg.SafetyLevel)
		fmt.Printf("Output directory: %s\n", cfg.Output.Directory)
	}

	// Streaming is used when explicitly requested, or auto-enabled by file
	// count / total input size (performance.streaming_threshold_mb).
	useStreaming, autoStreamReason := streaming, ""
	if !useStreaming {
		useStreaming, autoStreamReason = shouldAutoStream(args, cfg)
	}

	// Streaming mode does not execute backup/rollback generation or paranoid
	// database validation; silently dropping them is not acceptable.
	if useStreaming && (enableBackup || enableRollback) {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"--backup and --rollback are not supported in streaming mode",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Drop --streaming (and stay under the auto-streaming thresholds), or run without --backup/--rollback")
	}

	// Streaming also skips the SQL transformation phase. If the user asked for
	// --transform explicitly, reject the combination; if transformation is
	// merely the flag default, disable it and record a visible warning instead
	// of silently shipping untransformed output.
	var streamingWarnings []string
	if useStreaming && enableTransformation {
		if cmd.Flags().Changed("transform") {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"--transform is not supported in streaming mode",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithSuggestion("Drop --streaming (and stay under the auto-streaming thresholds), or run without --transform")
		}
		enableTransformation = false
		streamingWarnings = append(streamingWarnings,
			"Streaming mode: SQL transformation (default --transform) skipped - not supported in streaming mode")
	}

	// Show streaming mode info whenever streaming is explicitly enabled
	if streaming && !squashJSON {
		color.Cyan("🚀 Streaming mode: enabled (memory limit: %dMB, batch size: %d, workers: %d)\n",
			memoryLimitMB, batchSize, workerCount)
	}

	var finalSQL string
	var warnings []string
	var migrationCount int
	var authCompatibilitySQL string
	var dataOperationsSQL string
	var provenanceMap *squasher.SquashMap
	var requiredExtensions []string
	var harnessMigrations []engineapi.DeterministicHarnessMigration

	// Use streaming engine for large datasets or when explicitly requested
	if useStreaming {
		if !streaming && !squashJSON {
			color.Cyan("🚀 Auto-enabling streaming mode: %s\n", autoStreamReason)
			color.Cyan("   Streaming: batch=%d, workers=%d, memory=%dMB\n", batchSize, workerCount, memoryLimitMB)
		}

		migrations, err := loadMigrations(args, showProgress)
		if err != nil {
			return err
		}
		harnessMigrations = deterministicHarnessMigrations(migrations)

		parsePolicyWarnings, err := enforcePartialParsePolicy(migrations, strictParse)
		if err != nil {
			return err
		}

		migrationMap := make(map[int]string)
		for i, m := range migrations {
			migrationMap[i] = m.Content
		}

		// Use streaming engine with optimized settings
		if len(args) > 500 {
			// For very large datasets, use high-performance settings
			res, err := squasher.OptimizedSquashForLargeDatasets(cfg, migrationMap, memoryLimitMB)
			if err != nil {
				return errors.NewError(
					errors.ErrorCodeConsolidationFailed,
					"Failed to squash large dataset",
					errors.SeverityError,
					errors.CategoryConsolidation,
				).WithInnerError(err).WithAdditional("file_count", len(args)).WithSuggestion("Try reducing memory limit or batch size, or use standard mode for smaller datasets")
			}
			finalSQL = res.BaselineSQL
			warnings = append(append(res.Warnings, parsePolicyWarnings...), streamingWarnings...)
			authCompatibilitySQL = res.AuthCompatibilitySQL
			requiredExtensions = res.Extensions
			migrationCount = len(migrations)
		} else {
			// Create engine with streaming configuration
			engineConfig := squasher.EngineConfig{
				Config:              cfg,
				Version:             rootCmd.Version,
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
				BackupPath:           backupPath,

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

			engine, err := squasher.NewEngine(engineConfig)
			if err != nil {
				return errors.NewError(
					errors.ErrorCodeConsolidationFailed,
					"failed to initialize squashing engine for streaming mode",
					errors.SeverityError,
					errors.CategoryConsolidation,
				).WithInnerError(err)
			}
			defer engine.Close()

			squashResult, err := engine.Squash(migrationMap)
			if err != nil {
				return errors.NewError(
					errors.ErrorCodeConsolidationFailed,
					"Failed to squash migrations in streaming mode",
					errors.SeverityError,
					errors.CategoryConsolidation,
				).WithInnerError(err).WithAdditional("streaming", true).WithSuggestion("Try disabling streaming mode or check for syntax errors in migration files")
			}

			finalSQL = squashResult.BaselineSQL
			warnings = append(append(squashResult.Warnings, parsePolicyWarnings...), streamingWarnings...)
			// Capture auth compatibility SQL for validation
			authCompatibilitySQL = squashResult.AuthCompatibilitySQL
			requiredExtensions = squashResult.Extensions

			migrationCount = len(migrations)
			objectsConsolidated = int(engine.GetStats().ConsolidationsApplied)

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
		harnessMigrations = deterministicHarnessMigrations(migrations)

		parsePolicyWarnings, err := enforcePartialParsePolicy(migrations, strictParse)
		if err != nil {
			return err
		}

		// Create engine configuration for non-streaming mode
		engineConfig := squasher.EngineConfig{
			Config:              cfg,
			Version:             rootCmd.Version,
			EnableStreaming:     false,
			EnableProgressTrack: showProgress,

			// Transformation options
			EnableBackup:         enableBackup,
			EnableRollback:       enableRollback,
			EnableTransformation: enableTransformation,
			BackupConfig:         createBackupConfig(),
			TransformationConfig: createTransformationConfig(),
			RollbackPath:         rollbackPath,
			BackupPath:           backupPath,

			EnableCycleDetection: enableCycleDetection,
			ShowCycleDetails:     showCycleDetails,
			CycleDetectionDepth:  cycleDetectionDepth,
		}

		engine, err := squasher.NewEngine(engineConfig)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeConsolidationFailed,
				"failed to initialize squashing engine",
				errors.SeverityError,
				errors.CategoryConsolidation,
			).WithInnerError(err)
		}
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

		finalSQL = squashResult.BaselineSQL
		warnings = append(squashResult.Warnings, parsePolicyWarnings...)
		authCompatibilitySQL = squashResult.AuthCompatibilitySQL
		migrationCount = len(migrations)
		objectsConsolidated = int(engine.GetStats().ConsolidationsApplied)
		dataOperationsSQL = squashResult.DataOperationsSQL
		provenanceMap = squashResult.ProvenanceMap
		requiredExtensions = squashResult.Extensions

		// NOTE: nothing is written to disk here. All output files are staged
		// after the dry-run guard and promoted only after validation (F-05/F-06).
	}

	// Handle explain mode - show detailed consolidation plan
	if explainMode {
		// Create a new engine just for generating the plan
		migrations, err := loadMigrations(args, false)
		if err != nil {
			return err
		}

		if _, err := enforcePartialParsePolicy(migrations, strictParse); err != nil {
			return err
		}

		engineConfig := squasher.EngineConfig{
			Config:          cfg,
			EnableStreaming: false,
		}
		engine, err := squasher.NewEngine(engineConfig)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeConsolidationFailed,
				"failed to initialize squashing engine for explain mode",
				errors.SeverityError,
				errors.CategoryConsolidation,
			).WithInnerError(err)
		}
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
		if squashJSON {
			result := &engineapi.SquashResult{
				BaselineSQL:         finalSQL,
				DataOperationsSQL:   dataOperationsSQL,
				Warnings:            warnings,
				FilesProcessed:      migrationCount,
				ObjectsConsolidated: objectsConsolidated,
				ProcessingTime:      time.Since(startTime).String(),
				Extensions:          requiredExtensions,
				ProvenanceInfo: &engineapi.ProvenanceInfo{
					Version:     rootCmd.Version,
					SafetyLevel: string(cfg.SafetyLevel),
				},
			}
			report, err := engineapi.BuildDeterministicHarnessReport(result, engineapi.DeterministicHarnessReportOptions{
				OutputSQLPath:          "generated_schema.sql",
				EngineVersion:          rootCmd.Version,
				OriginalMigrationFiles: migrationCount,
				ValidationStatus:       "engine_basic_passed",
				ValidationMode:         "engine_basic",
				AnalysisWarnings:       warnings,
			})
			if err != nil {
				return err
			}
			contextArtifact, err := engineapi.BuildDeterministicHarnessContext(result, engineapi.DeterministicHarnessContextOptions{
				OutputSQLPath:          "generated_schema.sql",
				EngineVersion:          rootCmd.Version,
				OriginalMigrationFiles: migrationCount,
				ValidationStatus:       report.Validation.Status,
				ValidationMode:         report.Validation.Mode,
				AnalysisWarnings:       warnings,
				OriginalMigrations:     harnessMigrations,
			})
			if err != nil {
				return err
			}
			artifact, err := engineapi.BuildDeterministicHarnessArtifact(result, report)
			if err == nil {
				_, err = engineapi.ValidateDeterministicHarnessArtifact(cmd.Context(), artifact, contextArtifact)
			}
			if err != nil {
				return err
			}
			return json.NewEncoder(cmd.OutOrStdout()).Encode(map[string]any{
				"auth_compatibility_sql": authCompatibilitySQL,
				"baseline_sql":           finalSQL,
				"data_operations_sql":    dataOperationsSQL,
				"deterministic_artifact": artifact,
				"deterministic_report":   report,
				"harness_context":        contextArtifact,
				"safety_level":           string(cfg.SafetyLevel),
				"warnings":               warnings,
			})
		}
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

	// Guard: refuse to write output into the directory that contains the
	// input migrations. Doing so would let a subsequent run consume its own
	// output and can destroy the originals.
	if err := ensureOutputNotInputDirectory(cfg.Output.Directory, args); err != nil {
		return err
	}

	if strings.TrimSpace(finalSQL) == "" {
		return errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			"generated baseline SQL is empty",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithSuggestion("This may indicate all migrations were filtered out - check safety level and input files")
	}

	// Stage every output file (baseline, data operations, provenance map) in a
	// temporary directory next to the output directory. The staged files are
	// promoted into place only after validation; a failed validation removes
	// the staging directory and leaves the output directory untouched.
	stagingDir, cleanupStaging, err := stageSquashOutput(cfg, finalSQL, dataOperationsSQL, provenanceMap, requiredExtensions)
	if err != nil {
		return err
	}
	promoted := false
	defer func() {
		if !promoted {
			cleanupStaging()
		}
	}()

	outputPath := filepath.Join(cfg.Output.Directory, "000_baseline.sql")

	// Run automatic validation against the STAGED output unless --no-validate
	// is specified. Nothing has been written to the output directory yet.
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
			valResult, valErr := runValidationCheck(cfg, originalPath, stagingDir, authCompatibilitySQL)

			status, outcomeErr := reportSquashValidationOutcome(valResult, valErr)
			validationStatus = status
			if outcomeErr != nil {
				// Staged output is discarded by the deferred cleanup; the
				// output directory stays untouched.
				return outcomeErr
			}
		}
	}

	// Promote staged output files into the output directory.
	if err := promoteStagedOutput(stagingDir, cfg.Output.Directory); err != nil {
		return err
	}
	promoted = true

	if dataOperationsSQL != "" {
		fmt.Println(color.GreenString("✓ Data operations written to: %s", filepath.Join(cfg.Output.Directory, "010_data.sql")))
	}
	if provenanceMap != nil {
		fmt.Println(color.GreenString("✓ Provenance map written to: %s", filepath.Join(cfg.Output.Directory, ".squashmap.json")))
	}

	// Print success report
	printSquashSummary(migrationCount, len(strings.Split(finalSQL, "\n")), time.Since(startTime), warnings, outputPath)

	if emitHarnessReport {
		if dryRun {
			fmt.Println(color.YellowString("⚠️  Skipping context emission in --dry-run mode"))
		} else {
			reportOutputPath := strings.TrimSpace(harnessReportPath)
			if reportOutputPath == "" {
				reportOutputPath = filepath.Join(cfg.Output.Directory, ".capysquash.context.v1.json")
			}

			contextArtifact, reportErr := engineapi.BuildDeterministicHarnessContext(&engineapi.SquashResult{
				BaselineSQL:         finalSQL,
				DataOperationsSQL:   "",
				Warnings:            warnings,
				FilesProcessed:      migrationCount,
				ObjectsConsolidated: objectsConsolidated,
				ProcessingTime:      time.Since(startTime).String(),
				ProvenanceInfo: &engineapi.ProvenanceInfo{
					Version:     rootCmd.Version,
					SafetyLevel: string(cfg.SafetyLevel),
				},
			}, engineapi.DeterministicHarnessContextOptions{
				OutputSQLPath:          outputPath,
				EngineVersion:          rootCmd.Version,
				OriginalMigrationFiles: migrationCount,
				ValidationStatus:       validationStatus,
				ValidationMode:         validationModeUsed,
				AnalysisWarnings:       warnings,
			})
			if reportErr != nil {
				return errors.NewError(
					errors.ErrorCodeSQLGenerationFailed,
					"Failed to build deterministic context artifact",
					errors.SeverityError,
					errors.CategoryConsolidation,
				).WithInnerError(reportErr)
			}

			if reportErr := harnesscontract.WriteHarnessContextV1(reportOutputPath, contextArtifact); reportErr != nil {
				return errors.NewError(
					errors.ErrorCodeSQLGenerationFailed,
					"Failed to write deterministic context artifact",
					errors.SeverityError,
					errors.CategoryConsolidation,
				).WithFile(reportOutputPath).WithInnerError(reportErr)
			}

			fmt.Println(color.GreenString("✓ Deterministic context artifact written to: %s", reportOutputPath))
		}
	}

	return nil
}

func deterministicHarnessMigrations(migrations []*MigrationWithContent) []engineapi.DeterministicHarnessMigration {
	result := make([]engineapi.DeterministicHarnessMigration, 0, len(migrations))
	for index, migration := range migrations {
		migrationID := filepath.Base(migration.FullPath)
		if migrationID == "." || migrationID == "" {
			migrationID = migration.Filename
		}
		result = append(result, engineapi.DeterministicHarnessMigration{
			MigrationID: migrationID,
			Sequence:    index + 1,
			SQL:         migration.Content,
		})
	}
	return result
}

// runValidationCheck performs validation and returns the result
func runValidationCheck(cfg *config.Config, originalPath, squashedPath, authCompatibilitySQL string) (*validation.ValidationResult, error) {
	// Create validator with config
	postgresVersion := "17" // Default to 17 if not specified
	if cfg.Validation.DockerImage != "" {
		// Parse version from docker image (e.g., "postgres:17" -> "17")
		parts := strings.Split(cfg.Validation.DockerImage, ":")
		if len(parts) == 2 {
			postgresVersion = parts[1]
		}
	}

	// Honor the config validation section instead of hardcoding behavior.
	valConfig := &validation.ValidationConfig{
		Level:                    validation.ValidationLevelStandard,
		ValidateExpressions:      true,
		ValidateConstraints:      true,
		ValidateDependencies:     true,
		DockerApproach:           validation.ApproachTwoDatabases,
		PostgreSQLVersion:        postgresVersion,
		CustomDockerImage:        customDockerImage,
		EnableExtensionDetection: cfg.Validation.EnableExtensionDetection,
		AutoInstallExtensions:    cfg.Validation.AutoInstallExtensions,
		EnableSQLFixes:           cfg.Validation.EnableSQLFixes,
		EnablePreprocessing:      cfg.Validation.EnablePreprocessing,
		Verbose:                  verbose || cfg.Validation.Verbose,
		AuthCompatibilitySQL:     authCompatibilitySQL,
	}

	if cfg.Validation.Mode != "" {
		valConfig.DockerApproach = validation.ValidationApproach(strings.ToUpper(strings.TrimSpace(cfg.Validation.Mode)))
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

// applySafetyOverride validates and applies a --safety flag override.
// An unknown value is rejected instead of silently disabling consolidation.
// Parsing goes through the public API's case/whitespace-normalizing parser so
// CLI semantics never diverge from library consumers.
func applySafetyOverride(cfg *config.Config, level string) error {
	if level == "" {
		return nil
	}

	parsed, err := engineapi.ParseSafetyLevel(level)
	if err != nil {
		return err
	}
	cfg.SafetyLevel = string(parsed)
	return nil
}

// applyValidationModeOverride validates and applies a --validation-mode flag override.
func applyValidationModeOverride(cfg *config.Config, mode string) error {
	if mode == "" {
		return nil
	}

	normalized := strings.ToUpper(strings.TrimSpace(mode))
	switch normalized {
	case "TWO_CONTAINERS", "TWO_DATABASES", "SCHEMA_DIFF":
		cfg.Validation.Mode = normalized
		return nil
	default:
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("invalid validation mode '%s'", mode),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Use one of: TWO_CONTAINERS, TWO_DATABASES, SCHEMA_DIFF")
	}
}

// shouldAutoStream reports whether streaming mode should be auto-enabled based
// on file count (>100 files) or total input size exceeding
// performance.streaming_threshold_mb. The reason string describes the trigger.
func shouldAutoStream(files []string, cfg *config.Config) (bool, string) {
	if len(files) > 100 {
		return true, fmt.Sprintf("%d files (threshold: 100 files)", len(files))
	}

	thresholdMB := cfg.Performance.StreamingThresholdMB
	if thresholdMB <= 0 {
		return false, ""
	}

	var totalBytes int64
	for _, file := range files {
		if info, err := os.Stat(file); err == nil && !info.IsDir() {
			totalBytes += info.Size()
		}
	}

	thresholdBytes := int64(thresholdMB) * 1024 * 1024
	if totalBytes > thresholdBytes {
		return true, fmt.Sprintf("%.1fMB total input (threshold: %dMB from performance.streaming_threshold_mb)",
			float64(totalBytes)/(1024*1024), thresholdMB)
	}

	return false, ""
}

// ensureOutputNotInputDirectory rejects configurations where the output
// directory is the same directory that holds the input migration files.
func ensureOutputNotInputDirectory(outputDir string, inputs []string) error {
	absOut, err := filepath.Abs(outputDir)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("failed to resolve output directory '%s'", outputDir),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	for _, input := range inputs {
		absIn, err := filepath.Abs(input)
		if err != nil {
			continue
		}

		inputDir := absIn
		if info, statErr := os.Stat(absIn); statErr == nil && !info.IsDir() {
			inputDir = filepath.Dir(absIn)
		} else if statErr == nil && info.IsDir() {
			inputDir = absIn
		} else {
			inputDir = filepath.Dir(absIn)
		}

		if inputDir == absOut {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				fmt.Sprintf("output directory '%s' is the same as the input migrations directory", outputDir),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithSuggestion("Choose a different --output directory so the original migrations are never overwritten")
		}
	}

	return nil
}

// stageSquashOutput writes all squash output files (000_baseline.sql,
// 010_data.sql, .squashmap.json) into a temporary staging directory created
// next to the output directory. It returns the staging path and a cleanup
// function that removes it.
func stageSquashOutput(cfg *config.Config, baselineSQL, dataOperationsSQL string, provenanceMap *squasher.SquashMap, extensions []string) (string, func(), error) {
	parent := filepath.Dir(filepath.Clean(cfg.Output.Directory))
	if err := os.MkdirAll(parent, 0755); err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("Failed to create parent directory for output '%s'", cfg.Output.Directory),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check directory permissions and ensure parent directory exists")
	}

	stagingDir, err := os.MkdirTemp(parent, ".pgsquash-staging-")
	if err != nil {
		return "", nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"Failed to create staging directory for squash output",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check write permissions next to the output directory")
	}

	cleanup := func() {
		if err := os.RemoveAll(stagingDir); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove staging directory %s: %v\n", stagingDir, err)
		}
	}

	baselinePath := filepath.Join(stagingDir, "000_baseline.sql")
	if err := os.WriteFile(baselinePath, []byte(baselineSQL), 0644); err != nil {
		cleanup()
		return "", nil, errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			fmt.Sprintf("Failed to write staged output file '%s'", baselinePath),
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithFile(baselinePath).WithInnerError(err).WithSuggestion("Ensure sufficient disk space and write permissions")
	}

	if dataOperationsSQL != "" {
		dataPath := filepath.Join(stagingDir, "010_data.sql")
		if err := os.WriteFile(dataPath, []byte(dataOperationsSQL), 0644); err != nil {
			cleanup()
			return "", nil, errors.NewError(
				errors.ErrorCodeSQLGenerationFailed,
				fmt.Sprintf("Failed to write staged data operations file '%s'", dataPath),
				errors.SeverityError,
				errors.CategoryConsolidation,
			).WithFile(dataPath).WithInnerError(err).WithSuggestion("Ensure sufficient disk space and write permissions")
		}
	}

	if provenanceMap != nil {
		provenance := squasher.NewProvenanceTracker(
			rootCmd.Version,
			cfg.SafetyLevel,
			cfg.PostgreSQLFeatures.TargetVersion,
			extensions,
		)
		provenance.GetSquashMap().Inputs = provenanceMap.Inputs
		provenance.GetSquashMap().Outputs = provenanceMap.Outputs
		provenance.GetSquashMap().Warnings = provenanceMap.Warnings
		provenance.GetSquashMap().ContentHash = provenanceMap.ContentHash

		// The squashmap is the provenance contract for the squashed output; a
		// baseline without it is incomplete, so a write failure fails the run.
		if err := provenance.WriteSquashMap(stagingDir); err != nil {
			cleanup()
			return "", nil, errors.NewError(
				errors.ErrorCodeSQLGenerationFailed,
				"Failed to write staged .squashmap.json provenance file",
				errors.SeverityError,
				errors.CategoryConsolidation,
			).WithFile(filepath.Join(stagingDir, ".squashmap.json")).WithInnerError(err).WithSuggestion("Ensure sufficient disk space and write permissions")
		}
	}

	return stagingDir, cleanup, nil
}

// promoteStagedOutput moves staged output files into the output directory and
// removes the (now empty) staging directory.
func promoteStagedOutput(stagingDir, outputDir string) error {
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("Failed to create output directory '%s'", outputDir),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithFile(outputDir).WithInnerError(err).WithSuggestion("Check directory permissions and ensure parent directory exists")
	}

	entries, err := os.ReadDir(stagingDir)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("Failed to read staging directory '%s'", stagingDir),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	for _, entry := range entries {
		src := filepath.Join(stagingDir, entry.Name())
		dst := filepath.Join(outputDir, entry.Name())
		if err := os.Rename(src, dst); err != nil {
			return errors.NewError(
				errors.ErrorCodeSQLGenerationFailed,
				fmt.Sprintf("Failed to move staged file '%s' into output directory", entry.Name()),
				errors.SeverityError,
				errors.CategoryConsolidation,
			).WithFile(dst).WithInnerError(err).WithSuggestion("Ensure the output directory is writable")
		}
	}

	if err := os.Remove(stagingDir); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to remove staging directory %s: %v\n", stagingDir, err)
	}

	return nil
}

// reportSquashValidationOutcome interprets the post-squash validation result,
// prints a truthful report, and decides whether the squash must fail.
//
// Status values:
//   - "passed":     a real comparison ran (both migration sets applied) and matched
//   - "failed":     a real comparison found differences, or validation errored
//   - "unverified": the original migrations failed to apply, so schema
//     equivalence is unproven (never reported as passed)
func reportSquashValidationOutcome(valResult *validation.ValidationResult, valErr error) (string, error) {
	if valErr != nil {
		fmt.Println(color.RedString("❌ Validation failed: %v", valErr))

		// If it's a structured error with an inner error, print that too
		if structErr, ok := valErr.(*errors.StructuredError); ok && structErr.InnerError != nil {
			fmt.Println(color.RedString("   Inner error: %v", structErr.InnerError))

			if innerStruct, ok := structErr.InnerError.(*errors.StructuredError); ok && innerStruct.InnerError != nil {
				fmt.Println(color.RedString("   PostgreSQL error: %v", innerStruct.InnerError))
			}
		}

		if failOnDiff {
			return "failed", errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Post-squash validation failed",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(valErr).WithSuggestion("Fix the validation environment (e.g. Docker) or rerun with --no-validate / --fail-on-diff=false")
		}
		fmt.Println(color.YellowString("⚠️  Warning: Validation failed but continuing (--fail-on-diff=false)"))
		return "failed", nil
	}

	if valResult == nil {
		// No error but also no result: validation never produced evidence.
		// Fail closed like the workflow path - never report "passed" without
		// a real comparison result backing it.
		fmt.Println(color.RedString("❌ Validation returned no result - schema equivalence is unverified"))
		if failOnDiff {
			return "unverified", errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Post-squash validation returned no result",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithSuggestion("Fix the validation environment (e.g. Docker) or rerun with --no-validate / --fail-on-diff=false")
		}
		fmt.Println(color.YellowString("⚠️  Warning: continuing without validation evidence (--fail-on-diff=false)"))
		return "unverified", nil
	}

	if valResult.Success {
		fmt.Println(color.GreenString("✅ Validation passed - schemas are identical"))
		return "passed", nil
	}

	docker := valResult.DockerValidation

	if docker != nil && docker.OriginalApplyFailed {
		// Distinct outcome: the original migrations could not be applied, so
		// no real comparison ran. Equivalence is unproven - never "passed".
		fmt.Println(color.YellowString("⚠️  Original migrations failed to apply - schema equivalence is UNPROVEN"))
		if docker.OriginalMigrationsError != "" {
			fmt.Println(color.YellowString("    Error: %s", docker.OriginalMigrationsError))
		}
		fmt.Println(color.GreenString("✓ Squashed migrations applied successfully"))
		if docker.Differences != "" {
			fmt.Println(color.CyanString("ℹ️  Differences against the partially-applied original schema:"))
			lines := strings.Split(docker.Differences, "\n")
			maxLines := 25
			for i, line := range lines {
				if i >= maxLines {
					fmt.Println(color.CyanString("    ... (%d more differences not shown)", len(lines)-maxLines))
					break
				}
				fmt.Println(color.CyanString("    %s", line))
			}
		}
		return "unverified", nil
	}

	// A real comparison ran and found differences.
	fmt.Println(color.RedString("❌ Schema differences detected!"))
	if docker != nil && docker.Differences != "" {
		fmt.Println(docker.Differences)

		if openReport {
			reportPath := "pgsquash-validation-report.md"
			if err := os.WriteFile(reportPath, []byte(docker.Differences), 0644); err == nil {
				fmt.Println(color.CyanString("📝 Validation report saved to: %s", reportPath))
				openInEditor(reportPath)
			}
		}
	}

	if failOnDiff {
		return "failed", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"Schema differences detected between original and squashed migrations",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Review the differences above and ensure squashing is correct")
	}
	fmt.Println(color.YellowString("⚠️  Warning: Schema differences detected but continuing (--fail-on-diff=false)"))
	return "failed", nil
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
	cfg, err := config.LoadConfig(resolveConfigPath())
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
		valConfig.AutoInstallExtensions = cfg.Validation.AutoInstallExtensions
		valConfig.EnableSQLFixes = cfg.Validation.EnableSQLFixes
		valConfig.Verbose = cfg.Validation.Verbose
		valConfig.AuthCompatibilitySQL = extAnalysis.AuthCompatibilitySQL // Inject auth compatibility

		// Use custom Docker image if specified (CLI flag takes precedence)
		if customDockerImage != "" {
			valConfig.CustomDockerImage = customDockerImage
			color.Cyan("🐳 Using custom Docker image for validation: %s\n", customDockerImage)
		}

		// Apply container ready timeout from config (convert int seconds to time.Duration)
		if cfg.Validation.ContainerReadyTimeout > 0 {
			valConfig.ContainerReadyTimeout = time.Duration(cfg.Validation.ContainerReadyTimeout) * time.Second
		}

		if mode != "" {
			color.Cyan("🔍 Using validation mode: %s\n", strings.ToUpper(mode))
		}

		validator := validation.NewSchemaValidator(valConfig, nil, nil)
		defer func() {
			if err := validator.Close(); err != nil {
				utils.GetDefaultLogger().Warn("Failed to close validator: %v", err)
			}
		}()

		result, err := validator.ValidateWithDocker(cmd.Context(), originalDir, squashedDir)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Docker validation failed",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithFile(originalDir).WithAdditional("squashed_dir", squashedDir).WithInnerError(err).WithSuggestion("Ensure Docker is running and accessible, or try a different validation mode")
		}

		// Run Static Validation (Post-Flight)
		staticConfig, err := buildStaticValidationConfig(cmd, cfg)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"Invalid static validation rule configuration",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}

		// Instantiate Static Validator
		staticValidator := validation.NewStaticValidator(staticConfig)

		// Validate structure of squashed migrations
		// (We validate the *result*, so we read the squashed SQL)
		squashedSQL, err := readDirToSQL(squashedDir)
		if err == nil {
			violations, err := staticValidator.Check(squashedSQL)
			if err != nil {
				color.Red("❌ Static validation execution failed: %v", err)
			} else if len(violations) > 0 {
				color.Yellow("\nStatic Analysis Findings:")
				for _, v := range violations {
					symbol := "⚠️"
					if v.Category == validation.CategoryBreaking || staticConfig.TreatWarningsAsErrors {
						symbol = "❌"
					}
					fmt.Printf("  %s [%s] %s: %s\n", symbol, v.Category, v.Code, v.Message)
				}
				if staticConfig.TreatWarningsAsErrors {
					// We should fail the command if there are errors
					// But we also want to report Docker validation status.
					// Let's combine success flags.
					result.Success = false // Mark overall result as failed
				}
			}
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
			// Fail closed: a validation gate that always exits 0 is not a gate.
			// Returning an error makes `pgsquash validate` exit non-zero so CI can
			// rely on it to block non-equivalent squashed migrations.
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"schema validation failed: original and squashed schemas are not equivalent",
				errors.SeverityError,
				errors.CategoryValidation,
			)
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

const externalValidationContractVersion = "pgsquash.external-validation.v1"

type externalValidationResult struct {
	ContractVersion   string   `json:"contract_version"`
	Success           bool     `json:"success"`
	Phase             string   `json:"phase"`
	ComparisonValid   bool     `json:"comparison_valid"`
	HasDifferences    bool     `json:"has_differences"`
	Differences       []string `json:"differences"`
	SnapshotContract  string   `json:"snapshot_contract"`
	PostgreSQLVersion string   `json:"postgresql_version,omitempty"`
	DurationMS        int64    `json:"duration_ms"`
	Error             string   `json:"error,omitempty"`
}

func runValidateExternal(cmd *cobra.Command, args []string) error {
	started := time.Now()
	result := externalValidationResult{
		ContractVersion:  externalValidationContractVersion,
		Differences:      make([]string, 0),
		SnapshotContract: validation.CatalogSnapshotContractVersion,
	}

	fail := func(err error) error {
		result.DurationMS = time.Since(started).Milliseconds()
		result.Error = err.Error()
		if externalJSON {
			_ = json.NewEncoder(cmd.OutOrStdout()).Encode(result)
		}
		return err
	}

	dsn, err := resolveExternalValidationDSN()
	if err != nil {
		return fail(err)
	}
	if (snapshotOutput == "") == (againstSnapshot == "") {
		return fail(errors.NewError(
			errors.ErrorCodeValidationFailed,
			"exactly one of --snapshot-output or --against-snapshot is required",
			errors.SeverityError,
			errors.CategoryValidation,
		))
	}

	result.Phase = "snapshot"
	if againstSnapshot != "" {
		result.Phase = "compare"
	}

	cfg, err := config.LoadConfig(resolveConfigPath())
	if err != nil {
		return fail(errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to load validation configuration",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err))
	}

	valConfig := validation.DefaultValidationConfig()
	valConfig.EnablePreprocessing = cfg.Validation.EnablePreprocessing
	valConfig.EnableSQLFixes = false
	valConfig.Verbose = verbose && !quietMode && !externalJSON
	valConfig.AuthCompatibilitySQL = detectAuthCompatibilitySQL(args[0])

	validator := validation.NewSchemaValidator(valConfig, nil, nil)
	defer validator.Close()

	snapshot, err := validator.ApplyAndSnapshot(cmd.Context(), args[0], dsn, validation.ExternalValidationOptions{
		AllowedSchemas: externalAllowedSchemas,
	})
	if err != nil {
		return fail(errors.NewError(
			errors.ErrorCodeValidationFailed,
			"external database validation failed",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithFile(args[0]).WithInnerError(err))
	}
	result.PostgreSQLVersion = snapshot.PostgreSQLVersion

	if snapshotOutput != "" {
		if err := writeCatalogSnapshot(snapshotOutput, snapshot); err != nil {
			return fail(err)
		}
		result.Success = true
		result.DurationMS = time.Since(started).Milliseconds()
		return writeExternalValidationResult(cmd, result)
	}

	original, err := readCatalogSnapshot(againstSnapshot)
	if err != nil {
		return fail(err)
	}
	diff, err := validation.CompareCatalogSnapshots(original, snapshot)
	if err != nil {
		return fail(err)
	}
	result.ComparisonValid = true
	result.HasDifferences = diff.HasDifferences
	result.Differences = append(result.Differences, diff.Differences...)
	result.Success = !diff.HasDifferences
	result.DurationMS = time.Since(started).Milliseconds()
	if err := writeExternalValidationResult(cmd, result); err != nil {
		return err
	}
	if diff.HasDifferences {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"external validation found schema differences",
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}
	return nil
}

func resolveExternalValidationDSN() (string, error) {
	if strings.TrimSpace(externalDSN) != "" && strings.TrimSpace(externalDSNEnv) != "" {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"--dsn and --dsn-env cannot be used together",
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}
	if name := strings.TrimSpace(externalDSNEnv); name != "" {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			return "", errors.NewError(
				errors.ErrorCodeValidationFailed,
				fmt.Sprintf("environment variable %s is empty", name),
				errors.SeverityError,
				errors.CategoryValidation,
			)
		}
		return value, nil
	}
	if value := strings.TrimSpace(externalDSN); value != "" {
		return value, nil
	}
	return "", errors.NewError(
		errors.ErrorCodeValidationFailed,
		"one of --dsn or --dsn-env is required",
		errors.SeverityError,
		errors.CategoryValidation,
	)
}

func detectAuthCompatibilitySQL(migrationPath string) string {
	paths := make([]string, 0)
	info, err := os.Stat(migrationPath)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		_ = filepath.WalkDir(migrationPath, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if !entry.IsDir() && strings.HasSuffix(strings.ToLower(entry.Name()), ".sql") {
				paths = append(paths, path)
			}
			return nil
		})
	} else {
		paths = append(paths, migrationPath)
	}

	contents := make(map[int]string, len(paths))
	for i, path := range paths {
		content, readErr := os.ReadFile(path)
		if readErr == nil {
			contents[i] = string(content)
		}
	}
	analysis := squasher.NewExtensionDetector().AnalyzeMigrations(contents)
	return analysis.AuthCompatibilitySQL
}

func writeCatalogSnapshot(path string, snapshot *validation.CatalogSnapshot) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create catalog snapshot directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".pgsquash-snapshot-*.json")
	if err != nil {
		return fmt.Errorf("create catalog snapshot: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	encoder := json.NewEncoder(temporary)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(snapshot); err != nil {
		temporary.Close()
		return fmt.Errorf("encode catalog snapshot: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close catalog snapshot: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return fmt.Errorf("publish catalog snapshot: %w", err)
	}
	return nil
}

func readCatalogSnapshot(path string) (*validation.CatalogSnapshot, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read catalog snapshot: %w", err)
	}
	var snapshot validation.CatalogSnapshot
	if err := json.Unmarshal(content, &snapshot); err != nil {
		return nil, fmt.Errorf("decode catalog snapshot: %w", err)
	}
	return &snapshot, nil
}

func writeExternalValidationResult(cmd *cobra.Command, result externalValidationResult) error {
	if externalJSON {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}
	if result.Phase == "snapshot" {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "Captured catalog snapshot (%s)\n", result.PostgreSQLVersion)
		return err
	}
	if result.Success {
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "Schemas are equivalent")
		return err
	}
	_, err := fmt.Fprintln(cmd.OutOrStdout(), "Schemas differ")
	return err
}

func runLint(cmd *cobra.Command, args []string) error {
	files, err := collectSQLFilesFromArgs(args)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"Failed to collect SQL files for linting",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	if len(files) == 0 {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"No SQL files found for linting",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Provide at least one SQL file or a directory containing SQL files")
	}

	cfg, err := config.LoadConfig(resolveConfigPath())
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"Failed to load configuration",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithFile(configPath).WithInnerError(err)
	}

	staticConfig, err := buildStaticValidationConfig(cmd, cfg)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"Invalid static validation rule configuration",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	applyFixes, _ := cmd.Flags().GetBool("fix")
	writeFixes, _ := cmd.Flags().GetBool("write")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	if writeFixes && !applyFixes {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"--write requires --fix",
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}

	validator := validation.NewStaticValidator(staticConfig)

	type fileViolation struct {
		File string `json:"file"`
		validation.Violation
	}

	type lintSummary struct {
		FilesScanned int `json:"files_scanned"`
		Violations   int `json:"violations"`
		Safety       int `json:"safety"`
		Breaking     int `json:"breaking"`
		Hygiene      int `json:"hygiene"`
		UnusedIgnore int `json:"unused_ignore_directives"`
		FixedFiles   int `json:"fixed_files"`
	}

	violations := make([]fileViolation, 0)
	summary := lintSummary{FilesScanned: len(files)}
	backupFiles := make([]string, 0)

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				fmt.Sprintf("Failed to read SQL file '%s'", file),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithFile(file).WithInnerError(err)
		}

		fileViolations, err := validator.Check(string(content))
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				fmt.Sprintf("Lint failed for '%s'", file),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithFile(file).WithInnerError(err)
		}

		if applyFixes {
			fixed, fixErr := validator.ApplyFixes(string(content), fileViolations)
			if fixErr != nil {
				return errors.NewError(
					errors.ErrorCodeValidationFailed,
					fmt.Sprintf("Failed to apply fixes for '%s'", file),
					errors.SeverityError,
					errors.CategoryValidation,
				).WithFile(file).WithInnerError(fixErr)
			}

			if writeFixes && fixed != string(content) {
				// Never rewrite an original in place without a recovery copy:
				// write a sibling backup first (collision-safe), then rewrite.
				backupFile, backupErr := writeLintBackup(file, content)
				if backupErr != nil {
					return errors.NewError(
						errors.ErrorCodeValidationFailed,
						fmt.Sprintf("Failed to back up '%s' before writing fixes", file),
						errors.SeverityError,
						errors.CategoryValidation,
					).WithFile(file).WithInnerError(backupErr).WithSuggestion("Ensure the directory is writable, or run --fix without --write to preview fixes")
				}
				if err := os.WriteFile(file, []byte(fixed), 0644); err != nil {
					return errors.NewError(
						errors.ErrorCodeValidationFailed,
						fmt.Sprintf("Failed to write fixes for '%s' (original preserved at '%s')", file, backupFile),
						errors.SeverityError,
						errors.CategoryValidation,
					).WithFile(file).WithInnerError(err)
				}
				summary.FixedFiles++
				backupFiles = append(backupFiles, backupFile)
			}
		}

		for _, violation := range fileViolations {
			violations = append(violations, fileViolation{File: file, Violation: violation})
			summary.Violations++
			if violation.Code == validation.RuleCodeMetaUnusedIgnoreDirective {
				summary.UnusedIgnore++
			}
			switch violation.Category {
			case validation.CategorySafety:
				summary.Safety++
			case validation.CategoryBreaking:
				summary.Breaking++
			case validation.CategoryHygiene:
				summary.Hygiene++
			}
		}
	}

	if jsonOutput {
		payload := map[string]any{
			"summary":    summary,
			"violations": violations,
		}
		if len(backupFiles) > 0 {
			payload["backups"] = backupFiles
		}
		encoder := json.NewEncoder(os.Stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(payload); err != nil {
			return err
		}
	} else {
		if summary.Violations == 0 {
			fmt.Printf("☑ Lint passed: %d file(s) scanned, no violations found.\n", summary.FilesScanned)
		} else {
			fmt.Printf("Lint summary: %d file(s), %d violation(s) [safety=%d, breaking=%d, hygiene=%d, unused_ignores=%d]\n",
				summary.FilesScanned,
				summary.Violations,
				summary.Safety,
				summary.Breaking,
				summary.Hygiene,
				summary.UnusedIgnore,
			)

			for _, violation := range violations {
				fmt.Printf("- %s:%d [%s] %s: %s\n",
					violation.File,
					violation.Line,
					violation.Category,
					violation.Code,
					violation.Message,
				)
			}

			if applyFixes && writeFixes && summary.FixedFiles > 0 {
				fmt.Printf("Applied and wrote autofixes to %d file(s).\n", summary.FixedFiles)
				for _, backupFile := range backupFiles {
					fmt.Printf("Original backed up to: %s\n", backupFile)
				}
			}
		}
	}

	strict, _ := cmd.Flags().GetBool("strict")
	shouldFail := strict
	if !strict {
		shouldFail = summary.Breaking > 0 || summary.Safety > 0
	}

	if shouldFail && summary.Violations > 0 {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("lint failed with %d violation(s)", summary.Violations),
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}

	return nil
}

// writeLintBackup writes a sibling backup of a file's original content before
// an autofix rewrite. It uses <file>.bak, falling back to <file>.bak.1,
// <file>.bak.2, ... on collision so earlier backups are never overwritten.
func writeLintBackup(file string, content []byte) (string, error) {
	candidate := file + ".bak"
	for i := 1; ; i++ {
		_, err := os.Stat(candidate)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s.bak.%d", file, i)
	}
	if err := os.WriteFile(candidate, content, 0644); err != nil {
		return "", err
	}
	return candidate, nil
}

func buildStaticValidationConfig(cmd *cobra.Command, cfg *config.Config) (*config.StaticValidatorConfig, error) {
	staticConfig := &config.StaticValidatorConfig{
		EnabledRules:          append([]string(nil), cfg.StaticValidation.EnabledRules...),
		RuleOptions:           map[string]map[string]any{},
		TreatWarningsAsErrors: cfg.StaticValidation.TreatWarningsAsErrors,
	}

	for code, options := range cfg.StaticValidation.RuleOptions {
		optionCopy := make(map[string]any, len(options))
		maps.Copy(optionCopy, options)
		staticConfig.RuleOptions[code] = optionCopy
	}

	enableRules, _ := cmd.Flags().GetStringSlice("enable-rule")
	disableRules, _ := cmd.Flags().GetStringSlice("disable-rule")
	strict, _ := cmd.Flags().GetBool("strict")

	resolvedRules, err := validation.ResolveEnabledRules(staticConfig.EnabledRules, enableRules, disableRules)
	if err != nil {
		return nil, err
	}
	staticConfig.EnabledRules = resolvedRules

	if strict {
		staticConfig.TreatWarningsAsErrors = true
	}

	return staticConfig, nil
}

func collectSQLFilesFromArgs(args []string) ([]string, error) {
	seen := make(map[string]struct{})
	files := make([]string, 0)

	appendFile := func(path string) {
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		files = append(files, path)
	}

	visitPath := func(target string) error {
		info, err := os.Stat(target)
		if err != nil {
			return err
		}

		if !info.IsDir() {
			if strings.EqualFold(filepath.Ext(target), ".sql") {
				appendFile(target)
			}
			return nil
		}

		return filepath.WalkDir(target, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(path), ".sql") {
				appendFile(path)
			}
			return nil
		})
	}

	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			continue
		}

		if strings.ContainsAny(trimmed, "*?[]") {
			matches, err := filepath.Glob(trimmed)
			if err != nil {
				return nil, err
			}
			for _, match := range matches {
				if err := visitPath(match); err != nil {
					return nil, err
				}
			}
			continue
		}

		if err := visitPath(trimmed); err != nil {
			return nil, err
		}
	}

	sort.Strings(files)
	return files, nil
}

func runInitConfig(cmd *cobra.Command, args []string) error {
	configFile := brandDefaultConfigName()
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

// Standardized workflow functions
func runSafeWorkflow(cmd *cobra.Command, args []string) error {
	color.Cyan("🛡️  SAFE Workflow: Production-Ready Migration Squashing\n")

	// Override settings for SAFE workflow
	safetyLevel = "conservative"
	enableBackup = true
	enableRollback = true
	enableTransformation = false // Conservative approach

	// Load configuration
	cfg, err := config.LoadConfig(resolveConfigPath())
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
	color.Yellow("   ► Auto SQL Fix: %s (from validation.enable_sql_fixes)\n", sqlFixStatus(cfg))
	fmt.Println()

	// Execute squash with AI-enhanced validation
	return executeSquashWithValidation(args, cfg, "TWO_CONTAINERS")
}

func runFastWorkflow(cmd *cobra.Command, args []string) error {
	color.Cyan("⚡ FAST Workflow: Development-Optimized Migration Squashing\n")

	// Override settings for FAST workflow
	safetyLevel = "standard"
	enableBackup = false
	enableRollback = false
	enableTransformation = true
	enableCycleDetection = true

	// Load configuration
	cfg, err := config.LoadConfig(resolveConfigPath())
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
	color.Yellow("   ► Validation: SCHEMA_DIFF single-database snapshots (fastest)\n")
	color.Yellow("   ► DDL Cycle Detection: %v (resolves conflicts)\n", enableCycleDetection)
	color.Yellow("   ► SQL Transformation: %v (modern syntax)\n", enableTransformation)
	color.Yellow("   ► Auto SQL Fix: %s (from validation.enable_sql_fixes)\n", sqlFixStatus(cfg))
	fmt.Println()

	// Execute squash with AI-enhanced fast processing
	return executeSquashWithValidation(args, cfg, "SCHEMA_DIFF")
}

func runAnalyzeWorkflow(cmd *cobra.Command, args []string) error {
	color.Cyan("🔍 ANALYZE Workflow: Comprehensive Migration Analysis\n")

	// Override settings for ANALYZE workflow
	enableCycleDetection = true
	cycleDetectionDepth = 10 // Deep analysis
	showCycleDetails = true

	// Load configuration
	cfg, err := config.LoadConfig(resolveConfigPath())
	if err != nil {
		color.Red("☒ Failed to load configuration: %v\n", err)
		return err
	}

	// Apply ANALYZE workflow settings - no actual modifications
	cfg.Performance.ShowProgress = true

	color.Yellow("📋 ANALYZE Workflow Configuration:\n")
	color.Yellow("   ► DDL Cycle Detection: %v (all algorithm types)\n", enableCycleDetection)
	color.Yellow("   ► Analysis Depth: %d levels\n", cycleDetectionDepth)
	color.Yellow("   ► Deterministic Analysis: enabled (parser + dependency tracker)\n")
	color.Yellow("   ► Detailed Reporting: %v (comprehensive findings)\n", showCycleDetails)
	color.Yellow("   ► Mode: Analysis only (no file modifications)\n")
	fmt.Println()

	// Execute deterministic comprehensive analysis
	return executeComprehensiveAnalysis(args, cfg)
}

// Helper functions for standardized workflows

// sqlFixStatus describes the configured SQL auto-fix behavior for workflow banners.
func sqlFixStatus(cfg *config.Config) string {
	if cfg.Validation.EnableSQLFixes {
		return "enabled - output files may be rewritten in place"
	}
	return "disabled"
}

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

	if cfg.Output.Directory == "" {
		cfg.Output.Directory = "clean_migrations"
	}
	outputDir := cfg.Output.Directory

	// Same guard as `squash`: never write output into the input directory.
	if err := ensureOutputNotInputDirectory(outputDir, args); err != nil {
		return err
	}

	// Create squasher engine using the workflow's feature toggles.
	engineConfig := squasher.EngineConfig{
		Config:               cfg,
		Version:              rootCmd.Version,
		EnableStreaming:      false,
		EnableBackup:         enableBackup,
		EnableRollback:       enableRollback,
		EnableTransformation: enableTransformation,
		BackupConfig:         createBackupConfig(),
		TransformationConfig: createTransformationConfig(),
		RollbackPath:         rollbackPath,
		BackupPath:           backupPath,
		EnableCycleDetection: enableCycleDetection,
	}
	engine, err := squasher.NewEngine(engineConfig)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"failed to initialize squashing engine",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err).WithSuggestion("Review engine configuration and database connectivity settings")
	}
	defer engine.Close()

	// Convert migrations to format expected by engine
	migrationMap := make(map[int]string)
	for i, mig := range migrations {
		migrationMap[i+1] = mig.Content
	}

	// Execute squashing
	res, err := engine.Squash(migrationMap)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"Failed to squash migrations",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err).WithSuggestion("Check migration syntax and dependency graph")
	}
	finalSQL, warnings := res.BaselineSQL, res.Warnings

	// Stage output and promote only after validation passes. The workflow
	// output filename matches `squash` (000_baseline.sql) so downstream
	// tooling sees a single naming contract.
	stagingDir, cleanupStaging, err := stageSquashOutput(cfg, finalSQL, "", nil, res.Extensions)
	if err != nil {
		return err
	}
	promoted := false
	defer func() {
		if !promoted {
			cleanupStaging()
		}
	}()

	outputFile := filepath.Join(outputDir, "000_baseline.sql")

	// Workflows promise validation: a validation execution failure (including
	// Docker being unavailable) or real schema differences must fail the run.
	color.Cyan("🔍 Running Docker validation with %s approach...\n", validationApproach)
	valResult, valErr := runWorkflowValidation(cfg, args, stagingDir, engine.GetAuthCompatibilitySQL(), validationApproach)
	if err := evaluateWorkflowValidation(valResult, valErr); err != nil {
		return err
	}

	if err := promoteStagedOutput(stagingDir, outputDir); err != nil {
		return err
	}
	promoted = true
	color.Green("☑ Squashed migrations written to: %s\n", outputFile)

	// Print summary
	sqlLines := strings.Count(finalSQL, "\n")
	printSquashSummary(len(migrations), sqlLines, time.Since(startTime), warnings, outputFile)
	return nil
}

// runWorkflowValidation runs Docker validation for the safe/fast workflows,
// honoring the config validation section (extension detection, SQL fixes,
// container timeout) instead of hardcoded defaults. Only the Docker approach
// is fixed by the workflow definition.
func runWorkflowValidation(cfg *config.Config, args []string, squashedPath, authCompatibilitySQL, validationApproach string) (*validation.ValidationResult, error) {
	validationConfig := validation.DefaultValidationConfig()
	validationConfig.EnableExtensionDetection = cfg.Validation.EnableExtensionDetection
	validationConfig.AutoInstallExtensions = cfg.Validation.AutoInstallExtensions
	validationConfig.EnableSQLFixes = cfg.Validation.EnableSQLFixes // opt-in only: never silently rewrite output
	validationConfig.EnablePreprocessing = cfg.Validation.EnablePreprocessing
	validationConfig.Verbose = true // Show auth layer creation

	if cfg.Validation.DockerImage != "" {
		parts := strings.Split(cfg.Validation.DockerImage, ":")
		if len(parts) == 2 {
			validationConfig.PostgreSQLVersion = parts[1]
		}
	}
	if cfg.Validation.ContainerReadyTimeout > 0 {
		validationConfig.ContainerReadyTimeout = time.Duration(cfg.Validation.ContainerReadyTimeout) * time.Second
	}

	// Set Docker approach based on workflow
	switch validationApproach {
	case "TWO_CONTAINERS":
		validationConfig.DockerApproach = validation.ApproachTwoContainers
	case "TWO_DATABASES":
		validationConfig.DockerApproach = validation.ApproachTwoDatabases
	case "SCHEMA_DIFF":
		validationConfig.DockerApproach = validation.ApproachSchemaDiff
	}

	validationConfig.AuthCompatibilitySQL = authCompatibilitySQL

	validator := validation.NewSchemaValidator(validationConfig, nil, nil)
	defer func() {
		if err := validator.Close(); err != nil {
			utils.GetDefaultLogger().Warn("Failed to close validator: %v", err)
		}
	}()

	// Determine the original migrations directory.
	originalDir := filepath.Dir(args[0])
	if info, err := os.Stat(args[0]); err == nil && info.IsDir() {
		originalDir = args[0]
	}

	return validator.ValidateWithDocker(context.Background(), originalDir, squashedPath)
}

// evaluateWorkflowValidation decides whether a safe/fast workflow run must
// fail based on the validation outcome:
//
//   - validation execution errors (e.g. Docker unavailable) fail the run
//   - real schema differences (a valid comparison with diffs) fail the run
//   - an unproven comparison (original migrations failed to apply) is
//     reported loudly but does not fail the run, since fixing broken
//     migration histories is the tool's primary use case
func evaluateWorkflowValidation(result *validation.ValidationResult, valErr error) error {
	if valErr != nil {
		color.Red("☒ Validation failed: %v\n", valErr)
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"workflow validation failed",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(valErr).WithSuggestion("Ensure Docker is running - the safe/fast workflows require validation to complete")
	}

	if result == nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"workflow validation returned no result",
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}

	if result.Success {
		color.Green("☑ Schema validation passed!\n")
		return nil
	}

	if result.DockerValidation != nil && result.DockerValidation.OriginalApplyFailed {
		color.Yellow("⚠️  Original migrations failed to apply - schema equivalence is UNPROVEN\n")
		if result.DockerValidation.OriginalMigrationsError != "" {
			color.Yellow("    Error: %s\n", result.DockerValidation.OriginalMigrationsError)
		}
		color.Green("✓ Squashed migrations applied successfully\n")
		return nil
	}

	color.Red("☒ Schema differences detected between original and squashed migrations\n")
	if result.DockerValidation != nil && result.DockerValidation.Differences != "" {
		fmt.Println(result.DockerValidation.Differences)
	}
	return errors.NewError(
		errors.ErrorCodeValidationFailed,
		"schema differences detected between original and squashed migrations",
		errors.SeverityError,
		errors.CategoryValidation,
	).WithSuggestion("Review the differences above and ensure squashing is correct")
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
	engine, err := squasher.NewEngine(engineConfig)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"failed to initialize analysis engine",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}
	defer engine.Close()

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

func enforcePartialParsePolicy(migrations []*MigrationWithContent, strict bool) ([]string, error) {
	parseWarnings := make([]string, 0)
	filesWithParseErrors := 0
	totalParseErrors := 0

	for _, migration := range migrations {
		if migration == nil || migration.Migration == nil || len(migration.ParseErrors) == 0 {
			continue
		}

		filesWithParseErrors++
		totalParseErrors += len(migration.ParseErrors)

		fileName := filepath.Base(migration.FullPath)
		if fileName == "." || fileName == "" {
			fileName = migration.Filename
		}

		parseWarnings = append(parseWarnings, fmt.Sprintf(
			"Partial parse in %s: %d parse error(s), %d statement(s) recovered",
			fileName,
			len(migration.ParseErrors),
			len(migration.Statements),
		))
	}

	if strict && filesWithParseErrors > 0 {
		return parseWarnings, errors.NewError(
			errors.ErrorCodeSyntaxError,
			"Strict parse policy violation: one or more migrations were only partially parsed",
			errors.SeverityError,
			errors.CategoryParsing,
		).WithAdditional("files_with_parse_errors", filesWithParseErrors).
			WithAdditional("total_parse_errors", totalParseErrors).
			WithSuggestion("Fix parse errors in migration SQL or rerun without --strict-parse")
	}

	return parseWarnings, nil
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
		if backupWarns := byCategory[errors.CategoryBackup]; len(backupWarns) > 0 {
			fmt.Print("\n" + color.BlueString("🛡 Safety Features:") + "\n")
			for _, w := range backupWarns {
				cleanMsg := strings.Replace(w.Message, "Backup created: ", "Database backup: ", 1)
				fmt.Printf("  ☑ %s\n", cleanMsg)
			}
		}

		if rollbackWarns := byCategory[errors.CategoryRollback]; len(rollbackWarns) > 0 {
			fmt.Print("\n" + color.BlueString("🔄 Rollback Capabilities:") + "\n")
			for _, w := range rollbackWarns {
				fmt.Printf("  ☑ %s\n", w.Message)
			}
		}

		if transformWarns := byCategory[errors.CategoryTransformation]; len(transformWarns) > 0 {
			fmt.Print("\n" + color.CyanString("⚡ SQL Transformations:") + "\n")
			for _, w := range transformWarns {
				cleanMsg := strings.Replace(w.Message, "Transformation: ", "", 1)
				fmt.Printf("  ☑ %s\n", cleanMsg)
			}
		}

		if optWarns := byCategory[errors.CategoryOptimization]; len(optWarns) > 0 {
			fmt.Print("\n" + color.CyanString("⚡ Optimizations Applied:") + "\n")
			for _, w := range optWarns {
				fmt.Printf("  ☑ %s\n", w.Message)
			}
		}

		// Show cycle detection results
		if cycleWarns := byCategory[errors.CategoryCycle]; len(cycleWarns) > 0 {
			fmt.Print("\n" + color.YellowString("🔍 DDL Cycle Detection:") + "\n")
			for _, w := range cycleWarns {
				switch w.Severity {
				case errors.SeverityCritical:
					fmt.Printf("  "+color.RedString("⚠ %s")+"\n", w.Message)
				case errors.SeverityError:
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
			if critical := bySeverity[errors.SeverityCritical]; len(critical) > 0 {
				fmt.Print("\n" + color.RedString("  🔴 Critical (%d):", len(critical)) + "\n")
				for _, w := range critical {
					fmt.Printf("    ► %s\n", w.Message)
					if w.Suggestion != "" {
						fmt.Printf("      → %s\n", color.CyanString(w.Suggestion))
					}
				}
			}

			// Error severity warnings
			if errSev := bySeverity[errors.SeverityError]; len(errSev) > 0 {
				fmt.Print("\n" + color.YellowString("  🟠 Error (%d):", len(errSev)) + "\n")
				for _, w := range errSev {
					fmt.Printf("    ► %s\n", w.Message)
					if w.Suggestion != "" {
						fmt.Printf("      → %s\n", color.CyanString(w.Suggestion))
					}
				}
			}

			if warn := bySeverity[errors.SeverityWarning]; len(warn) > 0 {
				fmt.Print("\n" + color.YellowString("  🟡 Warning (%d):", len(warn)) + "\n")
				for _, w := range warn {
					fmt.Printf("    ► %s\n", w.Message)
				}
			}

			if info := bySeverity[errors.SeverityInfo]; len(info) > 0 {
				fmt.Printf("\n  ℹ️  Info (%d):\n", len(info))
				for _, w := range info {
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

	// Note: the backup directory itself is wired via EngineConfig.BackupPath
	// (--backup-path); the engine creates it or fails at construction time.

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

func readDirToSQL(dir string) (string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return "", err
	}

	// Sort files to ensure deterministic order (important for concatenation)
	sort.Strings(files)

	var sb strings.Builder
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		sb.Write(content)
		sb.WriteString("\n")
	}
	return sb.String(), nil
}
