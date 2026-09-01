package parser

import (
	"context"
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// ParseError represents a structured parsing error with context
type ParseError struct {
	*errors.StructuredError
	Statement *types.Statement
}

// ParseContext scopes errors.ErrorContext to parser operations.
type ParseContext = errors.ErrorContext

// NewParseError creates a new ParseError wrapping StructuredError
func NewParseError(message string, severity errors.Severity, category errors.Category) *ParseError {
	return &ParseError{
		StructuredError: errors.NewError(
			errors.ErrorCode(""),
			message,
			severity,
			category,
		),
	}
}

// ErrorCollector now wraps errors.ErrorCollector with ParseError compatibility
type ErrorCollector struct {
	*errors.ErrorCollector
}

// NewErrorCollector creates a new error collector
func NewErrorCollector(ctx context.Context) *ErrorCollector {
	return &ErrorCollector{
		ErrorCollector: errors.NewErrorCollector(ctx),
	}
}

// AddError adds an error to the collector
func (ec *ErrorCollector) AddError(err *ParseError) {
	ec.ErrorCollector.AddError(err.StructuredError)
}

// AddSyntaxError adds a syntax error
func (ec *ErrorCollector) AddSyntaxError(message string, ctx *ParseContext, innerErr error) {
	structuredErr := errors.NewError(
		errors.ErrorCodeSyntaxError,
		message,
		errors.SeverityError,
		errors.CategorySyntax,
	).WithContext(ctx).WithInnerError(innerErr).WithCanContinue(false)

	ec.ErrorCollector.AddError(structuredErr)
}

// AddSemanticError adds a semantic error
func (ec *ErrorCollector) AddSemanticError(message, suggestion string, ctx *ParseContext) {
	structuredErr := errors.NewError(
		errors.ErrorCodeSemanticError,
		message,
		errors.SeverityError,
		errors.CategorySemantic,
	).WithContext(ctx).WithSuggestion(suggestion).WithCanContinue(true)

	ec.ErrorCollector.AddError(structuredErr)
}

// AddDependencyError adds a dependency error
func (ec *ErrorCollector) AddDependencyError(message string, ctx *ParseContext) {
	structuredErr := errors.NewError(
		errors.ErrorCodeDependencyError,
		message,
		errors.SeverityError,
		errors.CategoryDependency,
	).WithContext(ctx).WithSuggestion("Check dependency order and ensure referenced objects exist").WithCanContinue(true)

	ec.ErrorCollector.AddError(structuredErr)
}

// AddNamingWarning adds a naming convention warning
func (ec *ErrorCollector) AddNamingWarning(message, suggestion string, ctx *ParseContext) {
	structuredErr := errors.NewError(
		errors.ErrorCode(""),
		message,
		errors.SeverityWarning,
		errors.CategoryNaming,
	).WithContext(ctx).WithSuggestion(suggestion).WithCanContinue(true)

	ec.ErrorCollector.AddError(structuredErr)
}

// AddPerformanceWarning adds a performance warning
func (ec *ErrorCollector) AddPerformanceWarning(message, suggestion string, ctx *ParseContext) {
	structuredErr := errors.NewError(
		errors.ErrorCode(""),
		message,
		errors.SeverityWarning,
		errors.CategoryPerformance,
	).WithContext(ctx).WithSuggestion(suggestion).WithCanContinue(true)

	ec.ErrorCollector.AddError(structuredErr)
}

// GetErrors returns all errors as ParseErrors
func (ec *ErrorCollector) GetErrors() []*ParseError {
	structuredErrors := ec.ErrorCollector.GetErrors()
	parseErrors := make([]*ParseError, len(structuredErrors))
	for i, err := range structuredErrors {
		parseErrors[i] = &ParseError{StructuredError: err}
	}
	return parseErrors
}

// GetWarnings returns all warnings as ParseErrors
func (ec *ErrorCollector) GetWarnings() []*ParseError {
	structuredWarnings := ec.ErrorCollector.GetWarnings()
	parseWarnings := make([]*ParseError, len(structuredWarnings))
	for i, warning := range structuredWarnings {
		parseWarnings[i] = &ParseError{StructuredError: warning}
	}
	return parseWarnings
}

// GetAllIssues returns both errors and warnings as ParseErrors
func (ec *ErrorCollector) GetAllIssues() []*ParseError {
	structuredIssues := ec.ErrorCollector.GetAllIssues()
	parseIssues := make([]*ParseError, len(structuredIssues))
	for i, issue := range structuredIssues {
		parseIssues[i] = &ParseError{StructuredError: issue}
	}
	return parseIssues
}

// ErrorSummary is now an alias to errors.ErrorSummary
type ErrorSummary = errors.ErrorSummary

// ErrorReporter handles error reporting and formatting
type ErrorReporter struct {
	collector *ErrorCollector
	formatter *ErrorFormatter
}

// NewErrorReporter creates a new error reporter
func NewErrorReporter(collector *ErrorCollector) *ErrorReporter {
	return &ErrorReporter{
		collector: collector,
		formatter: NewErrorFormatter(),
	}
}

// ErrorFormatter wraps errors.ErrorFormatter
type ErrorFormatter struct {
	*errors.ErrorFormatter
}

// NewErrorFormatter creates a new error formatter
func NewErrorFormatter() *ErrorFormatter {
	return &ErrorFormatter{
		ErrorFormatter: errors.NewErrorFormatter(),
	}
}

// FormatError formats a single error
func (ef *ErrorFormatter) FormatError(err *ParseError) string {
	return ef.ErrorFormatter.FormatError(err.StructuredError)
}

// FormatErrorList formats a list of errors
func (ef *ErrorFormatter) FormatErrorList(errorList []*ParseError) string {
	structuredErrors := make([]*errors.StructuredError, len(errorList))
	for i, err := range errorList {
		structuredErrors[i] = err.StructuredError
	}
	return ef.ErrorFormatter.FormatErrorList(structuredErrors)
}

// ErrorHandler provides centralized error handling
type ErrorHandler struct {
	collector *ErrorCollector
	reporter  *ErrorReporter
	context   context.Context
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(ctx context.Context) *ErrorHandler {
	collector := NewErrorCollector(ctx)
	reporter := NewErrorReporter(collector)

	return &ErrorHandler{
		collector: collector,
		reporter:  reporter,
		context:   ctx,
	}
}

// HandleParseError handles a parse error with appropriate logging
func (eh *ErrorHandler) HandleParseError(err error, ctx *ParseContext) *ParseError {
	// Detect missing semicolon and provide clearer error message
	errorMsg := err.Error()
	enhancedMsg := errorMsg
	suggestion := ""

	if detectsMissingSemicolon(errorMsg, ctx) {
		enhancedMsg = "Missing semicolon at end of SQL statement"
		suggestion = "Add a semicolon (;) at the end of the statement. PostgreSQL requires semicolons to separate statements in migration files."
	}

	parseErr := &ParseError{
		StructuredError: errors.NewError(
			errors.ErrorCodeSyntaxError,
			enhancedMsg,
			errors.SeverityError,
			errors.CategorySyntax,
		).WithContext(ctx).WithInnerError(err).WithCanContinue(false).WithSuggestion(suggestion),
	}

	eh.collector.AddError(parseErr)
	return parseErr
}

// detectsMissingSemicolon analyzes parse errors to detect missing semicolons
func detectsMissingSemicolon(errorMsg string, ctx *ParseContext) bool {
	// Common pg_query error patterns that indicate missing semicolons:
	// 1. "syntax error at or near" followed by a keyword that starts a new statement
	// 2. "syntax error at end of input"
	// 3. Statement text doesn't end with semicolon

	errorLower := strings.ToLower(errorMsg)

	// Pattern 1: "syntax error at or near" followed by statement-starting keywords
	if strings.Contains(errorLower, "syntax error at or near") {
		statementStarters := []string{"create", "alter", "drop", "insert", "update", "delete", "grant", "revoke", "do"}
		for _, starter := range statementStarters {
			if strings.Contains(errorLower, "\""+starter+"\"") || strings.Contains(errorLower, "'"+starter+"'") {
				return true
			}
		}
	}

	// Pattern 2: "syntax error at end of input"
	if strings.Contains(errorLower, "syntax error at end of input") {
		return true
	}

	// Pattern 3: Check if statement text exists and doesn't end with semicolon
	if ctx != nil && ctx.StatementText != "" {
		trimmed := strings.TrimSpace(ctx.StatementText)
		if !strings.HasSuffix(trimmed, ";") {
			// Additional check: ensure it's not just a comment or empty line
			if len(trimmed) > 0 && !strings.HasPrefix(trimmed, "--") {
				return true
			}
		}
	}

	return false
}

// HandleValidationError handles validation errors
func (eh *ErrorHandler) HandleValidationError(message string, ctx *ParseContext) *ParseError {
	parseErr := &ParseError{
		StructuredError: errors.NewError(
			errors.ErrorCodeSemanticError,
			message,
			errors.SeverityError,
			errors.CategorySemantic,
		).WithContext(ctx).WithCanContinue(true),
	}

	eh.collector.AddError(parseErr)
	return parseErr
}

// CreateContext creates a parse context from available information
func (eh *ErrorHandler) CreateContext(filename string, line int, stmt *types.Statement) *ParseContext {
	ctx := &ParseContext{
		Filename: filename,
		Line:     line,
	}

	if stmt != nil {
		ctx.StatementText = stmt.SQL
		ctx.ObjectName = stmt.ObjectName
		ctx.ObjectType = string(stmt.ObjectType)
		ctx.Schema = stmt.Schema
	}

	return ctx
}

// GetCollector returns the error collector
func (eh *ErrorHandler) GetCollector() *ErrorCollector {
	return eh.collector
}

// GetReporter returns the error reporter
func (eh *ErrorHandler) GetReporter() *ErrorReporter {
	return eh.reporter
}

// Recovery handles panic recovery during parsing
func (eh *ErrorHandler) Recovery(filename string, line int) {
	if r := recover(); r != nil {
		ctx := &ParseContext{
			Filename: filename,
			Line:     line,
		}

		recoveredErr, ok := r.(error)
		if !ok {
			recoveredErr = fmt.Errorf("%v", r)
		}

		parseErr := &ParseError{
			StructuredError: errors.NewCriticalError(
				errors.ErrorCodeSyntaxError,
				fmt.Sprintf("panic during parsing: %v", recoveredErr),
				errors.CategorySyntax,
			).WithContext(ctx),
		}

		eh.collector.AddError(parseErr)
	}
}

// ShouldContinue determines if processing should continue after an error
func (eh *ErrorHandler) ShouldContinue() bool {
	for _, err := range eh.collector.GetErrors() {
		if !err.CanContinue {
			return false
		}
	}
	return true
}

// LogSummary logs a summary of all collected errors and warnings
func (eh *ErrorHandler) LogSummary() {
	summary := eh.collector.Summary()
	if summary.TotalErrors > 0 || summary.TotalWarnings > 0 {
		println("Parse Summary: " + eh.reporter.formatter.FormatSummary(summary))
	}
}
