package errors

import (
    "context"
    "fmt"
    "strings"
)

// Severity represents the severity level of errors
type Severity int

const (
    SeverityInfo Severity = iota
    SeverityWarning
    SeverityError
    SeverityCritical
)

func (s Severity) String() string {
    switch s {
    case SeverityInfo:
        return "INFO"
    case SeverityWarning:
        return "WARNING"
    case SeverityError:
        return "ERROR"
    case SeverityCritical:
        return "CRITICAL"
    default:
        return "UNKNOWN"
    }
}

// Category represents the category of errors/warnings
// Unified from errors.Category and utils.WarningCategory
type Category string

const (
    // Core error categories
    CategorySyntax         Category = "SYNTAX"
    CategorySemantic       Category = "SEMANTIC"
    CategoryDependency     Category = "DEPENDENCY"
    CategoryNaming         Category = "NAMING"
    CategoryPermission     Category = "PERMISSION"
    CategoryConstraint     Category = "CONSTRAINT"
    CategoryDataType       Category = "DATA_TYPE"
    CategoryFunction       Category = "FUNCTION"
    CategoryIndex          Category = "INDEX"
    CategoryPolicy         Category = "POLICY"
    CategoryExtension      Category = "EXTENSION"
    CategoryPerformance    Category = "PERFORMANCE"
    CategoryValidation     Category = "VALIDATION"
    CategoryTransformation Category = "TRANSFORMATION"
    CategoryType           Category = "TYPE"
    CategoryNormalization  Category = "NORMALIZATION"
    CategoryParsing        Category = "PARSING"
    CategoryConsolidation  Category = "CONSOLIDATION"

    // Additional categories from WarningManager
    CategoryCycle         Category = "CYCLE"
    CategoryOptimization  Category = "OPTIMIZATION"
    CategoryRisk          Category = "RISK"
    CategoryBackup        Category = "BACKUP"
    CategoryRollback      Category = "ROLLBACK"
    CategoryInfo          Category = "INFO"
    CategoryGeneral       Category = "GENERAL"
)

// ErrorCode represents specific error codes
type ErrorCode string

// Common error codes
const (
    // Parse errors
    ErrorCodeSyntaxError    ErrorCode = "SYNTAX_ERROR"
    ErrorCodeSemanticError  ErrorCode = "SEMANTIC_ERROR"
    ErrorCodeDependencyError ErrorCode = "DEPENDENCY_ERROR"

    // Validation errors
    ErrorCodeValidationFailed ErrorCode = "VALIDATION_FAILED"
    ErrorCodeSchemaNotFound   ErrorCode = "SCHEMA_NOT_FOUND"
    ErrorCodeInvalidObject    ErrorCode = "INVALID_OBJECT"

    // Transformation errors
    ErrorCodeRollbackGenerationFailed   ErrorCode = "ROLLBACK_GENERATION_FAILED"
    ErrorCodeRollbackAnalysisFailed     ErrorCode = "ROLLBACK_ANALYSIS_FAILED"
    ErrorCodeRollbackSaveFailed         ErrorCode = "ROLLBACK_SAVE_FAILED"
    ErrorCodeRollbackNotFound           ErrorCode = "ROLLBACK_NOT_FOUND"
    ErrorCodeExecutionNotFound          ErrorCode = "EXECUTION_NOT_FOUND"
    ErrorCodeDatabaseNotAccessible      ErrorCode = "DATABASE_NOT_ACCESSIBLE"
    ErrorCodeManualInterventionRequired ErrorCode = "MANUAL_INTERVENTION_REQUIRED"
    ErrorCodeInvalidSQL                 ErrorCode = "INVALID_SQL"
    ErrorCodeBackupGenerationFailed     ErrorCode = "BACKUP_GENERATION_FAILED"
    ErrorCodeBackupValidationFailed     ErrorCode = "BACKUP_VALIDATION_FAILED"
    ErrorCodeTransformationFailed       ErrorCode = "TRANSFORMATION_FAILED"
    ErrorCodeWorkingDirCreationFailed   ErrorCode = "WORKING_DIR_CREATION_FAILED"

    // Type errors
    ErrorCodeInvalidType         ErrorCode = "INVALID_TYPE"
    ErrorCodeTypeNotFound        ErrorCode = "TYPE_NOT_FOUND"
    ErrorCodeIncompatibleTypes   ErrorCode = "INCOMPATIBLE_TYPES"
    ErrorCodeArraySpecError      ErrorCode = "ARRAY_SPEC_ERROR"
    ErrorCodeSizeExtractionError ErrorCode = "SIZE_EXTRACTION_ERROR"
    ErrorCodeEnumValidationError ErrorCode = "ENUM_VALIDATION_ERROR"
    ErrorCodeCompositeTypeError  ErrorCode = "COMPOSITE_TYPE_ERROR"
    ErrorCodeAnalysisError       ErrorCode = "ANALYSIS_ERROR"

    // Consolidation errors
    ErrorCodeConsolidationFailed ErrorCode = "CONSOLIDATION_FAILED"
    ErrorCodeSQLGenerationFailed ErrorCode = "SQL_GENERATION_FAILED"
    ErrorCodeRecoveryAttempted   ErrorCode = "RECOVERY_ATTEMPTED"
    ErrorCodePartialSuccess      ErrorCode = "PARTIAL_SUCCESS"

    // Normalization errors
    ErrorCodeNormalizationFailed ErrorCode = "NORMALIZATION_FAILED"
    ErrorCodeInvalidIdentifier   ErrorCode = "INVALID_IDENTIFIER"
)

// ErrorContext provides context information for errors
type ErrorContext struct {
    Filename        string                 `json:"filename,omitempty"`
    Line            int                    `json:"line,omitempty"`
    Column          int                    `json:"column,omitempty"`
    ObjectName      string                 `json:"object_name,omitempty"`
    ObjectType      string                 `json:"object_type,omitempty"`
    Schema          string                 `json:"schema,omitempty"`
    StatementText   string                 `json:"statement_text,omitempty"`
    MigrationSeq    int                    `json:"migration_seq,omitempty"`
    ProcessingPhase string                 `json:"processing_phase,omitempty"`
    TypeName        string                 `json:"type_name,omitempty"`
    SQLQuery        string                 `json:"sql_query,omitempty"`
    Additional      map[string]interface{} `json:"additional,omitempty"`
}

// StructuredError is a unified error type for all pgsquash errors
type StructuredError struct {
    Code        ErrorCode     `json:"code"`
    Message     string        `json:"message"`
    Severity    Severity      `json:"severity"`
    Category    Category      `json:"category"`
    Context     *ErrorContext `json:"context,omitempty"`
    Suggestion  string        `json:"suggestion,omitempty"`
    InnerError  error         `json:"-"`
    CanContinue bool          `json:"can_continue"`
}

// Error implements the error interface
func (e *StructuredError) Error() string {
    var parts []string

    parts = append(parts, fmt.Sprintf("[%s:%s]", e.Severity, e.Category))

    if e.Code != "" {
        parts = append(parts, fmt.Sprintf("code:%s", e.Code))
    }

    if e.Context != nil {
        if e.Context.Filename != "" {
            parts = append(parts, fmt.Sprintf("file:%s", e.Context.Filename))
        }
        if e.Context.Line > 0 {
            parts = append(parts, fmt.Sprintf("line:%d", e.Context.Line))
        }
        if e.Context.ObjectName != "" {
            parts = append(parts, fmt.Sprintf("object:%s", e.Context.ObjectName))
        }
        if e.Context.TypeName != "" {
            parts = append(parts, fmt.Sprintf("type:%s", e.Context.TypeName))
        }
    }

    parts = append(parts, e.Message)

    if e.Suggestion != "" {
        parts = append(parts, fmt.Sprintf("suggestion: %s", e.Suggestion))
    }

    // BUG #2 FIX: Include inner error details if present
    if e.InnerError != nil {
        parts = append(parts, fmt.Sprintf("\n   Inner error: %v", e.InnerError))
    }

    return strings.Join(parts, " ")
}

// Unwrap returns the inner error for error unwrapping
func (e *StructuredError) Unwrap() error {
    return e.InnerError
}

// NewError creates a new StructuredError
func NewError(code ErrorCode, message string, severity Severity, category Category) *StructuredError {
    return &StructuredError{
        Code:        code,
        Message:     message,
        Severity:    severity,
        Category:    category,
        Context:     &ErrorContext{Additional: make(map[string]interface{})},
        CanContinue: severity < SeverityError,
    }
}

// Builder methods for fluent error construction

// WithContext sets the error context
func (e *StructuredError) WithContext(ctx *ErrorContext) *StructuredError {
    e.Context = ctx
    return e
}

// WithFile sets the filename
func (e *StructuredError) WithFile(filename string) *StructuredError {
    if e.Context == nil {
        e.Context = &ErrorContext{Additional: make(map[string]interface{})}
    }
    e.Context.Filename = filename
    return e
}

// WithLine sets the line number
func (e *StructuredError) WithLine(line int) *StructuredError {
    if e.Context == nil {
        e.Context = &ErrorContext{Additional: make(map[string]interface{})}
    }
    e.Context.Line = line
    return e
}

// WithColumn sets the column number
func (e *StructuredError) WithColumn(column int) *StructuredError {
    if e.Context == nil {
        e.Context = &ErrorContext{Additional: make(map[string]interface{})}
    }
    e.Context.Column = column
    return e
}

// WithObject sets the object name and type
func (e *StructuredError) WithObject(name, objType string) *StructuredError {
    if e.Context == nil {
        e.Context = &ErrorContext{Additional: make(map[string]interface{})}
    }
    e.Context.ObjectName = name
    e.Context.ObjectType = objType
    return e
}

// WithSchema sets the schema
func (e *StructuredError) WithSchema(schema string) *StructuredError {
    if e.Context == nil {
        e.Context = &ErrorContext{Additional: make(map[string]interface{})}
    }
    e.Context.Schema = schema
    return e
}

// WithStatement sets the statement text
func (e *StructuredError) WithStatement(sql string) *StructuredError {
    if e.Context == nil {
        e.Context = &ErrorContext{Additional: make(map[string]interface{})}
    }
    e.Context.StatementText = sql
    return e
}

// WithTypeName sets the type name
func (e *StructuredError) WithTypeName(typeName string) *StructuredError {
    if e.Context == nil {
        e.Context = &ErrorContext{Additional: make(map[string]interface{})}
    }
    e.Context.TypeName = typeName
    return e
}

// WithSQLQuery sets the SQL query
func (e *StructuredError) WithSQLQuery(query string) *StructuredError {
    if e.Context == nil {
        e.Context = &ErrorContext{Additional: make(map[string]interface{})}
    }
    e.Context.SQLQuery = query
    return e
}

// WithAdditional adds additional context
func (e *StructuredError) WithAdditional(key string, value interface{}) *StructuredError {
    if e.Context == nil {
        e.Context = &ErrorContext{Additional: make(map[string]interface{})}
    }
    if e.Context.Additional == nil {
        e.Context.Additional = make(map[string]interface{})
    }
    e.Context.Additional[key] = value
    return e
}

// WithSuggestion adds a suggestion
func (e *StructuredError) WithSuggestion(suggestion string) *StructuredError {
    e.Suggestion = suggestion
    return e
}

// WithInnerError wraps an inner error
func (e *StructuredError) WithInnerError(err error) *StructuredError {
    e.InnerError = err
    return e
}

// WithCanContinue sets whether processing can continue
func (e *StructuredError) WithCanContinue(canContinue bool) *StructuredError {
    e.CanContinue = canContinue
    return e
}

// ErrorCollector collects and manages errors
type ErrorCollector struct {
    errors   []*StructuredError
    warnings []*StructuredError
    context  context.Context
}

// NewErrorCollector creates a new error collector
func NewErrorCollector(ctx context.Context) *ErrorCollector {
    return &ErrorCollector{
        errors:   make([]*StructuredError, 0),
        warnings: make([]*StructuredError, 0),
        context:  ctx,
    }
}

// AddError adds an error to the collector
func (ec *ErrorCollector) AddError(err *StructuredError) {
    if err.Severity >= SeverityError {
        ec.errors = append(ec.errors, err)
    } else {
        ec.warnings = append(ec.warnings, err)
    }
}

// HasErrors returns true if there are any errors
func (ec *ErrorCollector) HasErrors() bool {
    return len(ec.errors) > 0
}

// HasWarnings returns true if there are any warnings
func (ec *ErrorCollector) HasWarnings() bool {
    return len(ec.warnings) > 0
}

// GetErrors returns all errors
func (ec *ErrorCollector) GetErrors() []*StructuredError {
    return ec.errors
}

// GetWarnings returns all warnings
func (ec *ErrorCollector) GetWarnings() []*StructuredError {
    return ec.warnings
}

// GetAllIssues returns both errors and warnings
func (ec *ErrorCollector) GetAllIssues() []*StructuredError {
    var all []*StructuredError
    all = append(all, ec.errors...)
    all = append(all, ec.warnings...)
    return all
}

// Clear removes all collected errors and warnings
func (ec *ErrorCollector) Clear() {
    ec.errors = ec.errors[:0]
    ec.warnings = ec.warnings[:0]
}

// Summary returns a summary of collected issues
func (ec *ErrorCollector) Summary() ErrorSummary {
    summary := ErrorSummary{
        TotalErrors:   len(ec.errors),
        TotalWarnings: len(ec.warnings),
        Categories:    make(map[Category]int),
        Severities:    make(map[Severity]int),
        Codes:         make(map[ErrorCode]int),
    }

    for _, err := range ec.GetAllIssues() {
        summary.Categories[err.Category]++
        summary.Severities[err.Severity]++
        if err.Code != "" {
            summary.Codes[err.Code]++
        }
    }

    return summary
}

// ErrorSummary provides a summary of collected errors
type ErrorSummary struct {
    TotalErrors   int
    TotalWarnings int
    Categories    map[Category]int
    Severities    map[Severity]int
    Codes         map[ErrorCode]int
}

// ErrorFormatter formats errors for different outputs
type ErrorFormatter struct{}

// NewErrorFormatter creates a new error formatter
func NewErrorFormatter() *ErrorFormatter {
    return &ErrorFormatter{}
}

// FormatError formats a single error
func (ef *ErrorFormatter) FormatError(err *StructuredError) string {
    return err.Error()
}

// FormatSummary formats an error summary
func (ef *ErrorFormatter) FormatSummary(summary ErrorSummary) string {
    var parts []string

    parts = append(parts, fmt.Sprintf("Total: %d errors, %d warnings",
        summary.TotalErrors, summary.TotalWarnings))

    if len(summary.Categories) > 0 {
        parts = append(parts, "Categories:")
        for category, count := range summary.Categories {
            parts = append(parts, fmt.Sprintf("  %s: %d", category, count))
        }
    }

    if len(summary.Codes) > 0 {
        parts = append(parts, "Error Codes:")
        for code, count := range summary.Codes {
            parts = append(parts, fmt.Sprintf("  %s: %d", code, count))
        }
    }

    return strings.Join(parts, "\n")
}

// FormatErrorList formats a list of errors
func (ef *ErrorFormatter) FormatErrorList(errors []*StructuredError) string {
    var parts []string

    for i, err := range errors {
        parts = append(parts, fmt.Sprintf("%d. %s", i+1, ef.FormatError(err)))
    }

    return strings.Join(parts, "\n")
}

// Convenience constructors for common error types

// NewParseError creates a parse error
func NewParseError(code ErrorCode, message string) *StructuredError {
    return NewError(code, message, SeverityError, CategoryParsing)
}

// NewValidationError creates a validation error
func NewValidationError(code ErrorCode, message string) *StructuredError {
    return NewError(code, message, SeverityError, CategoryValidation)
}

// NewTransformationError creates a transformation error
func NewTransformationError(code ErrorCode, message string) *StructuredError {
    return NewError(code, message, SeverityError, CategoryTransformation)
}

// NewTypeError creates a type error
func NewTypeError(code ErrorCode, message string, typeName string) *StructuredError {
    return NewError(code, message, SeverityError, CategoryType).WithTypeName(typeName)
}

// NewNormalizationError creates a normalization error
func NewNormalizationError(message string, index int) *StructuredError {
    return NewError(ErrorCodeNormalizationFailed, message, SeverityError, CategoryNormalization).
        WithAdditional("index", index)
}

// NewWarning creates a warning
func NewWarning(category Category, message string) *StructuredError {
    return NewError("", message, SeverityWarning, category).WithCanContinue(true)
}

// NewCriticalError creates a critical error
func NewCriticalError(code ErrorCode, message string, category Category) *StructuredError {
    return NewError(code, message, SeverityCritical, category).WithCanContinue(false)
}

// New creates a new StructuredError with additional context (convenience function)
func New(code ErrorCode, category Category, message string, additional map[string]interface{}) *StructuredError {
    err := NewError(code, message, SeverityError, category)
    for k, v := range additional {
        err = err.WithAdditional(k, v)
    }
    return err
}

// Wrap wraps an existing error with structured error context (convenience function)
func Wrap(innerErr error, code ErrorCode, category Category, message string, additional map[string]interface{}) *StructuredError {
    err := New(code, category, message, additional)
    return err.WithInnerError(innerErr)
}
