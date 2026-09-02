// Package validation provides comprehensive schema validation and safety checking.
//
// This package exposes validation functionality for external tools to validate
// migration squashing results and ensure schema equivalence.
//
// # Basic Usage
//
// Validate squashed migrations against originals:
//
//	config := validation.DefaultConfig()
//	config.DockerApproach = validation.ApproachTwoDatabases
//	config.PostgreSQLVersion = "17"
//
//	validator, err := validation.NewValidator(config)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	result, err := validator.ValidateWithDocker(ctx, originalDir, squashedDir)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	if result.Success {
//	    fmt.Println("✓ Validation passed!")
//	} else {
//	    for _, err := range result.Errors {
//	        fmt.Printf("Error: %s\n", err.Message)
//	    }
//	}
//
// # Validation Approaches
//
// Three Docker-based strategies:
//
//	ApproachTwoContainers  - Most accurate, separate containers
//	ApproachTwoDatabases   - Balanced, one container two databases (recommended)
//	ApproachSchemaDiff     - Fastest, sequential application
//
// # Extension Detection
//
// Automatic detection and installation of PostgreSQL extensions:
//
//	config := validation.DefaultConfig()
//	config.EnableExtensionDetection = true
//	config.AutoInstallExtensions = true
//
//	validator, err := validation.NewValidator(config)
//	// Extensions like pgcrypto, uuid-ossp will be auto-detected and installed
//
// # Custom Validation
//
// Validate specific aspects:
//
//	config := validation.DefaultConfig()
//	config.Level = validation.LevelThorough
//	config.ValidateConstraints = true
//	config.ValidateDependencies = true
//	config.ValidatePerformance = true
package validation

import (
	"context"
	"maps"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/config"
	internal_validation "github.com/capysquash/pgsquash-engine/internal/validation"
)

// Re-export types from internal package
type (
	// SchemaValidator performs schema validation and safety checking
	SchemaValidator = internal_validation.SchemaValidator

	// ValidationConfig configures validation behavior
	ValidationConfig = internal_validation.ValidationConfig

	// ValidationResult represents the result of schema validation
	ValidationResult = internal_validation.ValidationResult

	// ValidationError represents a validation error
	ValidationError = internal_validation.ValidationError

	// ValidationWarning represents a validation warning
	ValidationWarning = internal_validation.ValidationWarning

	// ValidationLevel represents the level of validation to perform
	ValidationLevel = internal_validation.ValidationLevel

	// ValidationApproach defines Docker-based validation strategies
	ValidationApproach = internal_validation.ValidationApproach

	// SchemaComparisonResult represents a comparison between two database schemas
	SchemaComparisonResult = internal_validation.SchemaComparisonResult

	// SchemaDifference represents a difference between schemas
	SchemaDifference = internal_validation.SchemaDifference

	// DockerValidationResult represents the result of Docker-based validation
	DockerValidationResult = internal_validation.DockerValidationResult

	// CatalogSnapshot is a portable schema signature captured from a caller-owned database.
	CatalogSnapshot = internal_validation.CatalogSnapshot

	// ExternalValidationOptions identifies platform-owned schemas allowed in an otherwise empty database.
	ExternalValidationOptions = internal_validation.ExternalValidationOptions

	// ValidationFix represents a fix applied during validation
	ValidationFix = internal_validation.ValidationFix

	// StaticValidator performs AST-based checking of SQL rules
	StaticValidator = internal_validation.StaticValidator

	// StaticValidatorConfig configures the static validator
	StaticValidatorConfig = config.StaticValidatorConfig

	// Fix represents a suggested code change
	Fix = internal_validation.Fix

	// Violation represents a rule violation found during static analysis
	Violation = internal_validation.Violation

	// ViolationCategory represents the severity/type of a violation
	ViolationCategory = internal_validation.ViolationCategory

	// SchemaDiff represents differences between two schemas (normalized)
	SchemaDiff = internal_validation.SchemaDiff
)

const CatalogSnapshotContractVersion = internal_validation.CatalogSnapshotContractVersion

// Validation levels
const (
	// LevelBasic performs basic SQL parsing and syntax validation
	LevelBasic ValidationLevel = internal_validation.ValidationLevelBasic

	// LevelStandard includes basic checks plus dependency validation (recommended)
	LevelStandard ValidationLevel = internal_validation.ValidationLevelStandard

	// LevelThorough includes standard checks plus performance analysis
	LevelThorough ValidationLevel = internal_validation.ValidationLevelThorough

	// LevelComprehensive performs the complete deterministic check suite
	// (all thorough checks plus full schema comparison)
	LevelComprehensive ValidationLevel = internal_validation.ValidationLevelComprehensive
)

// Violation categories
const (
	CategorySafety   ViolationCategory = internal_validation.CategorySafety
	CategoryBreaking ViolationCategory = internal_validation.CategoryBreaking
	CategoryHygiene  ViolationCategory = internal_validation.CategoryHygiene
)

// Rule codes (stable cross-surface identifiers)
const (
	RuleCodeBreakingDropColumn        = internal_validation.RuleCodeBreakingDropColumn
	RuleCodeBreakingDropTable         = internal_validation.RuleCodeBreakingDropTable
	RuleCodeBreakingRenameColumn      = internal_validation.RuleCodeBreakingRenameColumn
	RuleCodeBreakingRenameTable       = internal_validation.RuleCodeBreakingRenameTable
	RuleCodeBreakingTypeChange        = internal_validation.RuleCodeBreakingTypeChange
	RuleCodeSafetyConcurrentIndex     = internal_validation.RuleCodeSafetyConcurrentIndex
	RuleCodeSafetyMissingWhere        = internal_validation.RuleCodeSafetyMissingWhere
	RuleCodeSafetyConstraintNotValid  = internal_validation.RuleCodeSafetyConstraintNotValid
	RuleCodeSafetyConstraintFlow      = internal_validation.RuleCodeSafetyConstraintFlow
	RuleCodeHygienePreferText         = internal_validation.RuleCodeHygienePreferText
	RuleCodeHygienePreferBigInt       = internal_validation.RuleCodeHygienePreferBigInt
	RuleCodeMetaUnusedIgnoreDirective = internal_validation.RuleCodeMetaUnusedIgnoreDirective
)

// Validation approaches
const (
	// ApproachTwoContainers uses separate containers for original and squashed migrations (most accurate)
	ApproachTwoContainers ValidationApproach = internal_validation.ApproachTwoContainers

	// ApproachTwoDatabases uses one container with two databases (best balance)
	ApproachTwoDatabases ValidationApproach = internal_validation.ApproachTwoDatabases

	// ApproachSchemaDiff uses sequential application and schema diff (fastest)
	ApproachSchemaDiff ValidationApproach = internal_validation.ApproachSchemaDiff
)

// DefaultConfig returns a recommended configuration for validation
func DefaultConfig() *ValidationConfig {
	return internal_validation.DefaultValidationConfig()
}

// NewValidator creates a new schema validator with the given configuration
//
// Example:
//
//	config := validation.DefaultConfig()
//	config.DockerApproach = validation.ApproachTwoDatabases
//	validator := validation.NewValidator(config)
func NewValidator(config *ValidationConfig) *SchemaValidator {
	return internal_validation.NewSchemaValidator(config, nil, nil)
}

// NewStaticValidator creates a new static validator with configured rules
func NewStaticValidator(config *StaticValidatorConfig) *StaticValidator {
	return internal_validation.NewStaticValidator(config)
}

// NewPreFlightValidator returns a validator optimized for initial checks (fast, hygiene focused)
func NewPreFlightValidator() *StaticValidator {
	return internal_validation.NewStaticValidator(nil) // Default config
}

// NewPostFlightValidator returns a validator optimized for final verification (strict)
func NewPostFlightValidator() *StaticValidator {
	conf := &config.StaticValidatorConfig{
		TreatWarningsAsErrors: true,
	}
	return internal_validation.NewStaticValidator(conf)
}

// ListRuleCodes returns all registered static validation rule codes.
func ListRuleCodes() []string {
	return internal_validation.ListRuleCodes()
}

// ResolveEnabledRules resolves a final enabled rule set from base config values
// plus runtime enable/disable overrides.
func ResolveEnabledRules(baseEnabled, enableRules, disableRules []string) ([]string, error) {
	return internal_validation.ResolveEnabledRules(baseEnabled, enableRules, disableRules)
}

// DefaultStaticValidatorConfig returns a copy of the engine default static-validator config.
func DefaultStaticValidatorConfig() *StaticValidatorConfig {
	return cloneStaticValidatorConfig(config.DefaultStaticValidatorConfig())
}

// CloneStaticValidatorConfig returns a deep copy of the provided static-validator config.
// If conf is nil, it returns a copy of the engine defaults.
func CloneStaticValidatorConfig(conf *StaticValidatorConfig) *StaticValidatorConfig {
	if conf == nil {
		return DefaultStaticValidatorConfig()
	}

	return cloneStaticValidatorConfig(conf)
}

// MergeStaticValidatorConfig overlays `overlay` onto `base` using engine semantics:
// - EnabledRules: if overlay has values, they replace base enabled rules.
// - RuleOptions: deep-merged (base first, then overlay keys overwrite).
// - TreatWarningsAsErrors: overlay value replaces base value.
//
// If base is nil, engine defaults are used.
func MergeStaticValidatorConfig(base, overlay *StaticValidatorConfig) *StaticValidatorConfig {
	merged := CloneStaticValidatorConfig(base)
	if overlay == nil {
		return merged
	}

	if len(overlay.EnabledRules) > 0 {
		merged.EnabledRules = append([]string(nil), overlay.EnabledRules...)
	}

	if merged.RuleOptions == nil {
		merged.RuleOptions = map[string]map[string]any{}
	}
	for ruleCode, ruleOptions := range overlay.RuleOptions {
		if _, ok := merged.RuleOptions[ruleCode]; !ok {
			merged.RuleOptions[ruleCode] = map[string]any{}
		}
		maps.Copy(merged.RuleOptions[ruleCode], ruleOptions)
	}

	merged.TreatWarningsAsErrors = overlay.TreatWarningsAsErrors

	return merged
}

// BuildStaticValidatorConfig applies runtime overrides (enable/disable/strict)
// to a base static-validator config and returns the resolved validator config.
func BuildStaticValidatorConfig(base *StaticValidatorConfig, enableRules, disableRules []string, strict bool) (*StaticValidatorConfig, error) {
	resolvedBase := CloneStaticValidatorConfig(base)

	resolvedRules, err := ResolveEnabledRules(resolvedBase.EnabledRules, enableRules, disableRules)
	if err != nil {
		return nil, err
	}

	resolvedBase.EnabledRules = resolvedRules
	if strict {
		resolvedBase.TreatWarningsAsErrors = true
	}

	return resolvedBase, nil
}

func cloneStaticValidatorConfig(conf *StaticValidatorConfig) *StaticValidatorConfig {
	if conf == nil {
		return &StaticValidatorConfig{
			EnabledRules: nil,
			RuleOptions:  map[string]map[string]any{},
		}
	}

	copyConf := &StaticValidatorConfig{
		EnabledRules:          append([]string(nil), conf.EnabledRules...),
		RuleOptions:           make(map[string]map[string]any, len(conf.RuleOptions)),
		TreatWarningsAsErrors: conf.TreatWarningsAsErrors,
	}

	for ruleCode, options := range conf.RuleOptions {
		if options == nil {
			copyConf.RuleOptions[ruleCode] = map[string]any{}
			continue
		}

		optionCopy := make(map[string]any, len(options))
		maps.Copy(optionCopy, options)
		copyConf.RuleOptions[ruleCode] = optionCopy
	}

	if copyConf.RuleOptions == nil {
		copyConf.RuleOptions = map[string]map[string]any{}
	}

	return copyConf
}

// SchemaValidator methods
// These are available on *SchemaValidator instances:
//
//   ValidateWithDocker(ctx, originalDir, squashedDir) (*ValidationResult, error)
//   ValidateMigrations(ctx, migrations) (*ValidationResult, error)
//   Close() error

// ValidateSchemaEquivalence is a convenience function that validates whether
// squashed migrations produce the same schema as the original migrations.
//
// This is a high-level helper that sets up validation with recommended defaults
// and returns a simple pass/fail result with detailed comparison information.
//
// Parameters:
//   - ctx: Context for cancellation and timeouts
//   - originalDir: Directory containing original migration files
//   - squashedDir: Directory containing squashed migration files
//   - config: Optional validation configuration (uses defaults if nil)
//
// Returns:
//   - ValidationResult with Success, Errors, Warnings, and SchemaComparison
//   - Error if validation infrastructure fails (Docker, parsing, etc.)
//
// Example:
//
//	ctx := context.Background()
//	result, err := validation.ValidateSchemaEquivalence(ctx, "./original", "./squashed", nil)
//	if err != nil {
//	    log.Fatalf("Validation infrastructure error: %v", err)
//	}
//
//	if result.Success {
//	    fmt.Println("✓ Schemas are equivalent!")
//	} else {
//	    fmt.Println("✗ Schema differences found:")
//	    for _, diff := range result.SchemaComparison.Differences {
//	        fmt.Printf("  - %s: %s\n", diff.Type, diff.Description)
//	    }
//	}
func ValidateSchemaEquivalence(
	ctx context.Context,
	originalDir string,
	squashedDir string,
	config *ValidationConfig,
) (*ValidationResult, error) {
	// Use default config if not provided
	if config == nil {
		config = DefaultConfig()
		config.DockerApproach = ApproachTwoDatabases
		config.PostgreSQLVersion = "17"
		config.EnableExtensionDetection = true
		config.AutoInstallExtensions = true
		config.Level = LevelStandard
		config.ValidateConstraints = true
		config.ValidateDependencies = true
	}

	// Create validator
	validator := NewValidator(config)
	defer validator.Close()

	// Run validation
	result, err := validator.ValidateWithDocker(ctx, originalDir, squashedDir)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// ValidateSchemaDiff performs a direct schema difference comparison between two schema strings.
// It normalizes both schemas using the default normalizer and returns the differences.
//
// This is useful for pre-flight checks where you have the raw SQL of two schemas (e.g. pg_dump output)
// and want to see if they are equivalent.
func ValidateSchemaDiff(schema1, schema2 string) (*SchemaDiff, error) {
	return internal_validation.CompareSchemasDirectly(schema1, schema2)
}

// CompareCatalogSnapshots compares portable catalog snapshots without a live database.
func CompareCatalogSnapshots(original, candidate *CatalogSnapshot) (*SchemaDiff, error) {
	return internal_validation.CompareCatalogSnapshots(original, candidate)
}

// QuickValidate is a convenience function for fast validation with minimal setup.
// Uses the fastest validation approach (SchemaDiff) for quick feedback.
//
// This is ideal for CI/CD pipelines or rapid development iteration where
// speed is more important than exhaustive validation.
//
// Example:
//
//	result, err := validation.QuickValidate(context.Background(), "./original", "./squashed")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Validation: %v (took %s)\n", result.Success, result.Duration)
func QuickValidate(ctx context.Context, originalDir, squashedDir string) (*ValidationResult, error) {
	config := DefaultConfig()
	config.DockerApproach = ApproachSchemaDiff
	config.PostgreSQLVersion = "17"
	config.Level = LevelBasic
	config.ContainerReadyTimeout = 60 * time.Second

	return ValidateSchemaEquivalence(ctx, originalDir, squashedDir, config)
}

// ThoroughValidate performs comprehensive validation with all checks enabled.
// Uses the most accurate validation approach (TwoContainers) for maximum confidence.
//
// This is recommended for production releases, final validation before deployment,
// or when schema accuracy is critical.
//
// Example:
//
//	result, err := validation.ThoroughValidate(context.Background(), "./original", "./squashed")
//	if err != nil {
//	    log.Fatal(err)
//	}
//	if !result.Success {
//	    log.Printf("Found %d errors and %d warnings\n", len(result.Errors), len(result.Warnings))
//	}
func ThoroughValidate(ctx context.Context, originalDir, squashedDir string) (*ValidationResult, error) {
	config := DefaultConfig()
	config.DockerApproach = ApproachTwoContainers
	config.PostgreSQLVersion = "17"
	config.Level = LevelThorough
	config.EnableExtensionDetection = true
	config.AutoInstallExtensions = true
	config.ValidateConstraints = true
	config.ValidateDependencies = true
	config.ValidatePerformance = true
	config.Verbose = true
	config.ContainerReadyTimeout = 180 * time.Second

	return ValidateSchemaEquivalence(ctx, originalDir, squashedDir, config)
}
