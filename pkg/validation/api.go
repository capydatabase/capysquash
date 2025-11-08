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
	"time"

	internal_validation "github.com/CAPYSQUASH/pgsquash-engine/internal/validation"
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

	// ValidationFix represents a fix applied during validation
	ValidationFix = internal_validation.ValidationFix
)

// Validation levels
const (
	// LevelBasic performs basic SQL parsing and syntax validation
	LevelBasic ValidationLevel = internal_validation.ValidationLevelBasic

	// LevelStandard includes basic checks plus dependency validation (recommended)
	LevelStandard ValidationLevel = internal_validation.ValidationLevelStandard

	// LevelThorough includes standard checks plus performance analysis
	LevelThorough ValidationLevel = internal_validation.ValidationLevelThorough

	// LevelComprehensive performs all checks including AI-powered analysis
	LevelComprehensive ValidationLevel = internal_validation.ValidationLevelComprehensive
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
//	validator := validation.NewValidator(config, nil, nil)
func NewValidator(config *ValidationConfig) *SchemaValidator {
	return internal_validation.NewSchemaValidator(config, nil, nil)
}

// SchemaValidator methods
// These are available on *SchemaValidator instances:
//
//   ValidateWithDocker(ctx, originalDir, squashedDir) (*ValidationResult, error)
//   ValidateMigrations(ctx, migrations) (*ValidationResult, error)
//   Close() error

// Example usage patterns

// ExampleBasicValidation demonstrates basic Docker validation
func ExampleBasicValidation() {
	ctx := context.Background()

	config := DefaultConfig()
	config.DockerApproach = ApproachTwoDatabases
	config.PostgreSQLVersion = "17"

	validator := NewValidator(config)
	defer validator.Close()

	result, err := validator.ValidateWithDocker(ctx, "./migrations", "./squashed")
	if err != nil {
		panic(err)
	}

	if result.Success {
		// Validation passed
	} else {
		// Handle errors
		for _, validationErr := range result.Errors {
			_ = validationErr.Message
			_ = validationErr.Severity
		}
	}
}

// ExampleExtensionDetection demonstrates automatic extension detection
func ExampleExtensionDetection() {
	ctx := context.Background()

	config := DefaultConfig()
	config.EnableExtensionDetection = true
	config.AutoInstallExtensions = true
	config.Verbose = true

	validator := NewValidator(config)
	defer validator.Close()

	// Extensions like pgcrypto, uuid-ossp will be auto-detected and installed
	result, err := validator.ValidateWithDocker(ctx, "./migrations", "./squashed")
	_ = result
	_ = err
}

// ExampleCustomValidation demonstrates custom validation levels
func ExampleCustomValidation() {
	ctx := context.Background()

	config := DefaultConfig()
	config.Level = LevelThorough
	config.ValidateConstraints = true
	config.ValidateDependencies = true
	config.ValidatePerformance = true
	config.ContainerReadyTimeout = 180 * time.Second

	validator := NewValidator(config)
	defer validator.Close()

	result, err := validator.ValidateWithDocker(ctx, "./migrations", "./squashed")
	_ = result
	_ = err
}
