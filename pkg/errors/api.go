// Package errors provides structured error handling for pgsquash operations.
//
// This package exposes the unified error system that categorizes, enriches, and
// formats errors with contextual information for better debugging and reporting.
//
// # Basic Usage
//
// Create structured errors:
//
//	err := errors.NewParseError(
//	    errors.ErrorCodeSyntaxError,
//	    "Invalid CREATE TABLE syntax",
//	).WithFile("001_init.sql").WithLine(42)
//
//	if err != nil {
//	    fmt.Println(err.Error())
//	    // [ERROR:PARSING] code:SYNTAX_ERROR file:001_init.sql line:42 Invalid CREATE TABLE syntax
//	}
//
// # Error Categories
//
// Errors are organized by category:
//
//	errors.CategorySyntax       - SQL parsing errors
//	errors.CategorySemantic     - Logical errors
//	errors.CategoryDependency   - Dependency issues
//	errors.CategoryValidation   - Validation failures
//	errors.CategoryConsolidation - Consolidation errors
//
// # Severity Levels
//
// Four severity levels:
//
//	errors.SeverityInfo     - Informational
//	errors.SeverityWarning  - Non-critical issues
//	errors.SeverityError    - Errors requiring attention
//	errors.SeverityCritical - Fatal errors
//
// # Error Collection
//
// Collect multiple errors:
//
//	collector := errors.NewErrorCollector(ctx)
//
//	for _, migration := range migrations {
//	    if err := validate(migration); err != nil {
//	        collector.AddError(err)
//	    }
//	}
//
//	if collector.HasErrors() {
//	    summary := collector.Summary()
//	    fmt.Printf("Found %d errors\n", summary.TotalErrors)
//	}
//
// # Fluent Error Building
//
// Build rich errors fluently:
//
//	err := errors.NewError(
//	    errors.ErrorCodeConsolidationFailed,
//	    "Failed to consolidate table",
//	    errors.SeverityError,
//	    errors.CategoryConsolidation,
//	).WithObject("users", "table").
//	  WithFile("003_users.sql").
//	  WithLine(15).
//	  WithSuggestion("Check for circular dependencies").
//	  WithInnerError(originalErr)
package errors

import (
	"context"

	internal_errors "github.com/capysquash/pgsquash-engine/internal/errors"
)

// Re-export types from internal package
type (
	// StructuredError is a unified error type for all pgsquash errors
	StructuredError = internal_errors.StructuredError

	// ErrorContext provides context information for errors
	ErrorContext = internal_errors.ErrorContext

	// ErrorCollector collects and manages errors
	ErrorCollector = internal_errors.ErrorCollector

	// ErrorSummary provides a summary of collected errors
	ErrorSummary = internal_errors.ErrorSummary

	// ErrorFormatter formats errors for different outputs
	ErrorFormatter = internal_errors.ErrorFormatter

	// Severity represents the severity level of errors
	Severity = internal_errors.Severity

	// Category represents the category of errors/warnings
	Category = internal_errors.Category

	// ErrorCode represents specific error codes
	ErrorCode = internal_errors.ErrorCode
)

// Severity levels
const (
	// SeverityInfo is informational, processing continues
	SeverityInfo Severity = internal_errors.SeverityInfo

	// SeverityWarning is non-critical, processing continues
	SeverityWarning Severity = internal_errors.SeverityWarning

	// SeverityError requires attention, may stop processing
	SeverityError Severity = internal_errors.SeverityError

	// SeverityCritical is fatal, stops processing
	SeverityCritical Severity = internal_errors.SeverityCritical
)

// Error categories
const (
	// CategorySyntax for SQL parsing and syntax errors
	CategorySyntax Category = internal_errors.CategorySyntax

	// CategorySemantic for logical and semantic errors
	CategorySemantic Category = internal_errors.CategorySemantic

	// CategoryDependency for dependency resolution errors
	CategoryDependency Category = internal_errors.CategoryDependency

	// CategoryNaming for naming convention errors
	CategoryNaming Category = internal_errors.CategoryNaming

	// CategoryPermission for permission and security errors
	CategoryPermission Category = internal_errors.CategoryPermission

	// CategoryConstraint for constraint validation errors
	CategoryConstraint Category = internal_errors.CategoryConstraint

	// CategoryDataType for data type errors
	CategoryDataType Category = internal_errors.CategoryDataType

	// CategoryFunction for function-related errors
	CategoryFunction Category = internal_errors.CategoryFunction

	// CategoryIndex for index-related errors
	CategoryIndex Category = internal_errors.CategoryIndex

	// CategoryPolicy for RLS policy errors
	CategoryPolicy Category = internal_errors.CategoryPolicy

	// CategoryExtension for extension-related errors
	CategoryExtension Category = internal_errors.CategoryExtension

	// CategoryPerformance for performance issues
	CategoryPerformance Category = internal_errors.CategoryPerformance

	// CategoryValidation for validation failures
	CategoryValidation Category = internal_errors.CategoryValidation

	// CategoryTransformation for transformation errors
	CategoryTransformation Category = internal_errors.CategoryTransformation

	// CategoryType for type system errors
	CategoryType Category = internal_errors.CategoryType

	// CategoryNormalization for normalization errors
	CategoryNormalization Category = internal_errors.CategoryNormalization

	// CategoryParsing for parsing errors
	CategoryParsing Category = internal_errors.CategoryParsing

	// CategoryConsolidation for consolidation errors
	CategoryConsolidation Category = internal_errors.CategoryConsolidation

	// CategoryCycle for circular dependency errors
	CategoryCycle Category = internal_errors.CategoryCycle

	// CategoryOptimization for optimization issues
	CategoryOptimization Category = internal_errors.CategoryOptimization

	// CategoryRisk for risk assessment warnings
	CategoryRisk Category = internal_errors.CategoryRisk

	// CategoryBackup for backup operation errors
	CategoryBackup Category = internal_errors.CategoryBackup

	// CategoryRollback for rollback operation errors
	CategoryRollback Category = internal_errors.CategoryRollback

	// CategoryInfo for informational messages
	CategoryInfo Category = internal_errors.CategoryInfo

	// CategoryGeneral for general errors
	CategoryGeneral Category = internal_errors.CategoryGeneral
)

// Error codes
const (
	// Parse errors
	ErrorCodeSyntaxError    ErrorCode = internal_errors.ErrorCodeSyntaxError
	ErrorCodeSemanticError  ErrorCode = internal_errors.ErrorCodeSemanticError
	ErrorCodeDependencyError ErrorCode = internal_errors.ErrorCodeDependencyError

	// Validation errors
	ErrorCodeValidationFailed ErrorCode = internal_errors.ErrorCodeValidationFailed
	ErrorCodeSchemaNotFound   ErrorCode = internal_errors.ErrorCodeSchemaNotFound
	ErrorCodeInvalidObject    ErrorCode = internal_errors.ErrorCodeInvalidObject

	// Transformation errors
	ErrorCodeRollbackGenerationFailed   ErrorCode = internal_errors.ErrorCodeRollbackGenerationFailed
	ErrorCodeBackupGenerationFailed     ErrorCode = internal_errors.ErrorCodeBackupGenerationFailed
	ErrorCodeTransformationFailed       ErrorCode = internal_errors.ErrorCodeTransformationFailed

	// Type errors
	ErrorCodeInvalidType       ErrorCode = internal_errors.ErrorCodeInvalidType
	ErrorCodeTypeNotFound      ErrorCode = internal_errors.ErrorCodeTypeNotFound
	ErrorCodeIncompatibleTypes ErrorCode = internal_errors.ErrorCodeIncompatibleTypes

	// Consolidation errors
	ErrorCodeConsolidationFailed ErrorCode = internal_errors.ErrorCodeConsolidationFailed
	ErrorCodeSQLGenerationFailed ErrorCode = internal_errors.ErrorCodeSQLGenerationFailed

	// Normalization errors
	ErrorCodeNormalizationFailed ErrorCode = internal_errors.ErrorCodeNormalizationFailed
	ErrorCodeInvalidIdentifier   ErrorCode = internal_errors.ErrorCodeInvalidIdentifier
)

// Constructors
var (
	// NewError creates a new StructuredError
	NewError = internal_errors.NewError

	// NewParseError creates a parse error
	NewParseError = internal_errors.NewParseError

	// NewValidationError creates a validation error
	NewValidationError = internal_errors.NewValidationError

	// NewTransformationError creates a transformation error
	NewTransformationError = internal_errors.NewTransformationError

	// NewTypeError creates a type error
	NewTypeError = internal_errors.NewTypeError

	// NewNormalizationError creates a normalization error
	NewNormalizationError = internal_errors.NewNormalizationError

	// NewWarning creates a warning
	NewWarning = internal_errors.NewWarning

	// NewCriticalError creates a critical error
	NewCriticalError = internal_errors.NewCriticalError

	// NewErrorCollector creates a new error collector
	NewErrorCollector = internal_errors.NewErrorCollector

	// NewErrorFormatter creates a new error formatter
	NewErrorFormatter = internal_errors.NewErrorFormatter
)

// StructuredError methods (fluent builders)
// These are available on *StructuredError instances:
//
//   WithContext(ctx *ErrorContext) *StructuredError
//   WithFile(filename string) *StructuredError
//   WithLine(line int) *StructuredError
//   WithColumn(column int) *StructuredError
//   WithObject(name, objType string) *StructuredError
//   WithSchema(schema string) *StructuredError
//   WithStatement(sql string) *StructuredError
//   WithTypeName(typeName string) *StructuredError
//   WithSQLQuery(query string) *StructuredError
//   WithAdditional(key string, value interface{}) *StructuredError
//   WithSuggestion(suggestion string) *StructuredError
//   WithInnerError(err error) *StructuredError
//   WithCanContinue(canContinue bool) *StructuredError
//   Error() string
//   Unwrap() error

// ErrorCollector methods
// These are available on *ErrorCollector instances:
//
//   AddError(err *StructuredError)
//   HasErrors() bool
//   HasWarnings() bool
//   GetErrors() []*StructuredError
//   GetWarnings() []*StructuredError
//   GetAllIssues() []*StructuredError
//   Clear()
//   Summary() ErrorSummary

// ErrorFormatter methods
// These are available on *ErrorFormatter instances:
//
//   FormatError(err *StructuredError) string
//   FormatSummary(summary ErrorSummary) string
//   FormatErrorList(errors []*StructuredError) string

// Example usage patterns

// ExampleBasicError demonstrates basic error creation
func ExampleBasicError() {
	err := NewParseError(
		ErrorCodeSyntaxError,
		"Invalid CREATE TABLE syntax",
	).WithFile("001_init.sql").WithLine(42)

	// Use error
	_ = err.Error()
	_ = err.Severity
	_ = err.Category
}

// ExampleErrorCollection demonstrates error collection
func ExampleErrorCollection() {
	ctx := context.Background()
	collector := NewErrorCollector(ctx)

	// Add errors
	collector.AddError(NewParseError(ErrorCodeSyntaxError, "Syntax error"))
	collector.AddError(NewWarning(CategoryOptimization, "Consider adding index"))

	if collector.HasErrors() {
		summary := collector.Summary()
		_ = summary.TotalErrors
		_ = summary.TotalWarnings
		_ = summary.Categories
	}
}

// ExampleFluentBuilder demonstrates fluent error building
func ExampleFluentBuilder() {
	err := NewError(
		ErrorCodeConsolidationFailed,
		"Failed to consolidate table",
		SeverityError,
		CategoryConsolidation,
	).WithObject("users", "table").
		WithFile("003_users.sql").
		WithLine(15).
		WithSuggestion("Check for circular dependencies")

	_ = err
}

// ExampleErrorFormatting demonstrates error formatting
func ExampleErrorFormatting() {
	formatter := NewErrorFormatter()
	collector := NewErrorCollector(context.Background())

	collector.AddError(NewParseError(ErrorCodeSyntaxError, "Error 1"))
	collector.AddError(NewValidationError(ErrorCodeValidationFailed, "Error 2"))

	summary := collector.Summary()
	formatted := formatter.FormatSummary(summary)
	_ = formatted
}
