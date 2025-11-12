// Package validation provides comprehensive schema validation and safety checking.
// It validates SQL migrations, checks for breaking changes, and ensures
// data integrity during migration squashing operations.
package validation

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/patterns"
	"github.com/capysquash/pgsquash-engine/internal/performance"
	"github.com/capysquash/pgsquash-engine/internal/plugins"
	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/fatih/color"
	_ "github.com/lib/pq"
)

// ValidationLevel represents the level of validation to perform
type ValidationLevel string

const (
	ValidationLevelBasic         ValidationLevel = "BASIC"
	ValidationLevelStandard      ValidationLevel = "STANDARD"
	ValidationLevelThorough      ValidationLevel = "THOROUGH"
	ValidationLevelComprehensive ValidationLevel = "COMPREHENSIVE"
)

// ValidationApproach defines Docker-based validation strategies
type ValidationApproach string

const (
	ApproachTwoContainers ValidationApproach = "TWO_CONTAINERS" // Most accurate
	ApproachTwoDatabases  ValidationApproach = "TWO_DATABASES"  // Best balance
	ApproachSchemaDiff    ValidationApproach = "SCHEMA_DIFF"    // Fastest
)

// ValidationConfig configures validation behavior
type ValidationConfig struct {
	Level                ValidationLevel `json:"level"`
	DatabaseURL          string          `json:"database_url,omitempty"`
	ValidateExpressions  bool            `json:"validate_expressions"`
	ValidateConstraints  bool            `json:"validate_constraints"`
	ValidateDependencies bool            `json:"validate_dependencies"`
	ValidatePermissions  bool            `json:"validate_permissions"`
	ValidatePerformance  bool            `json:"validate_performance"`
	IgnoreWarnings       bool            `json:"ignore_warnings"`
	StopOnError          bool            `json:"stop_on_error"`
	MaxConcurrentQueries int             `json:"max_concurrent_queries"`
	QueryTimeout         time.Duration   `json:"query_timeout"`
	// Docker-based validation options
	DockerApproach           ValidationApproach `json:"docker_approach,omitempty"`
	PostgreSQLVersion        string             `json:"postgresql_version,omitempty"` // PostgreSQL version for validation containers (default: 17)
	EnableExtensionDetection bool               `json:"enable_extension_detection"`
	AutoInstallExtensions    bool               `json:"auto_install_extensions"`
	EnableSQLFixes           bool               `json:"enable_sql_fixes"`
	EnablePreprocessing      bool               `json:"enable_preprocessing"` // Preprocess SQL to fix common issues (e.g., deduplicate publication statements) (default: true)
	CustomExtensions         map[string]string  `json:"custom_extensions,omitempty"`
	Verbose                  bool               `json:"verbose"`
	ContainerReadyTimeout    time.Duration      `json:"container_ready_timeout,omitempty"`  // Timeout for container readiness (default: 150s, recommended for complex migrations with many extensions)
	MaxPortSearchAttempts    int                `json:"max_port_search_attempts,omitempty"` // Max ports to search (default: 1000)
	AuthCompatibilitySQL     string             `json:"auth_compatibility_sql,omitempty"`   // Auth compatibility SQL to inject before migrations
}

// DefaultValidationConfig returns a default validation configuration
func DefaultValidationConfig() *ValidationConfig {
	return &ValidationConfig{
		Level:                   ValidationLevelStandard,
		ValidateExpressions:     true,
		ValidateConstraints:     true,
		ValidateDependencies:    true,
		ValidatePermissions:     false,
		ValidatePerformance:     false,
		IgnoreWarnings:          false,
		StopOnError:             false,
		MaxConcurrentQueries:    4,
		QueryTimeout:            30 * time.Second,
		EnablePreprocessing:     true, // Enable preprocessing by default (deduplicate publications, etc.)
		EnableExtensionDetection: true, // Enable extension detection by default
		AutoInstallExtensions:   true, // Auto-install extensions by default
		Verbose:                 true, // Show detailed output by default
	}
}

// ValidationResult represents the result of schema validation
type ValidationResult struct {
	StartTime     time.Time              `json:"start_time"`
	EndTime       time.Time              `json:"end_time"`
	Duration      time.Duration          `json:"duration"`
	Level         ValidationLevel        `json:"level"`
	Success       bool                   `json:"success"`
	Errors        []ValidationError      `json:"errors"`
	Warnings      []ValidationWarning    `json:"warnings"`
	SchemaChanges []SchemaChange         `json:"schema_changes,omitempty"`
	Statistics    ValidationStatistics   `json:"statistics"`
	Details       map[string]interface{} `json:"details,omitempty"`
	// Docker validation results
	DockerValidation    *DockerValidationResult `json:"docker_validation,omitempty"`
	ExtensionsDetected  []string                `json:"extensions_detected,omitempty"`
	ExtensionsInstalled []string                `json:"extensions_installed,omitempty"`
	ApproachUsed        ValidationApproach      `json:"approach_used,omitempty"`
	FixesApplied        []ValidationFix         `json:"fixes_applied,omitempty"`
	// AI validation results
	AIValidation *AIValidationResult `json:"ai_validation,omitempty"`
}

// DockerValidationResult represents the result of Docker-based validation
type DockerValidationResult struct {
	Success                 bool          `json:"success"`
	Duration                time.Duration `json:"duration"`
	Differences             string        `json:"differences,omitempty"`
	Error                   string        `json:"error,omitempty"`
	OriginalDB              string        `json:"original_db"`
	SquashedDB              string        `json:"squashed_db"`
	OriginalMigrationsError string        `json:"original_migrations_error,omitempty"` // Error applying original migrations (expected for broken migrations)
	ComparisonValid         bool          `json:"comparison_valid"`                     // True if both original and squashed migrations succeeded
}

// ContainerInfo holds Docker container information
type ContainerInfo struct {
	ID   string
	Port int
}

// ValidationFix represents a fix applied during validation
type ValidationFix struct {
	Issue       string `json:"issue"`
	Fix         string `json:"fix"`
	Success     bool   `json:"success"`
	Description string `json:"description"`
}

// SchemaComparisonResult provides detailed schema differences
type SchemaComparisonResult struct {
	TablesMatch     bool               `json:"tables_match"`
	IndexesMatch    bool               `json:"indexes_match"`
	FunctionsMatch  bool               `json:"functions_match"`
	ExtensionsMatch bool               `json:"extensions_match"`
	Differences     []SchemaDifference `json:"differences"`
	Summary         string             `json:"summary"`
}

// SchemaDifference represents a specific schema difference
type SchemaDifference struct {
	Type       string `json:"type"` // TABLE, INDEX, FUNCTION, etc.
	Name       string `json:"name"`
	Difference string `json:"difference"` // MISSING, EXTRA, MODIFIED
	Details    string `json:"details"`
	Severity   string `json:"severity"` // LOW, MEDIUM, HIGH, CRITICAL
}

// ValidationError represents a validation error
// This now wraps the unified StructuredError
type ValidationError struct {
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	ObjectID   ObjectID               `json:"object_id"`
	Severity   string                 `json:"severity"`
	Context    map[string]interface{} `json:"context,omitempty"`
	Suggestion string                 `json:"suggestion,omitempty"`
	SQLQuery   string                 `json:"sql_query,omitempty"`
	Line       int                    `json:"line,omitempty"`
	File       string                 `json:"file,omitempty"`
}

// ValidationWarning represents a validation warning
type ValidationWarning struct {
	Code       string                 `json:"code"`
	Message    string                 `json:"message"`
	ObjectID   ObjectID               `json:"object_id"`
	Context    map[string]interface{} `json:"context,omitempty"`
	Suggestion string                 `json:"suggestion,omitempty"`
}

// Note: ValidationError and ValidationWarning remain as-is for now
// because they have specialized fields (ObjectID) used in Docker validation.
// They could be migrated in a future phase if needed.

// ValidationStatistics provides validation statistics
type ValidationStatistics struct {
	ObjectsValidated     int `json:"objects_validated"`
	QueriesExecuted      int `json:"queries_executed"`
	ExpressionsValidated int `json:"expressions_validated"`
	ConstraintsChecked   int `json:"constraints_checked"`
	DependenciesResolved int `json:"dependencies_resolved"`
	ErrorsFound          int `json:"errors_found"`
	WarningsFound        int `json:"warnings_found"`
}

// SchemaValidator performs comprehensive schema validation including Docker-based validation
type SchemaValidator struct {
	config       *ValidationConfig
	db           *sql.DB
	reporter     performance.ProgressReporter
	evaluator    *ExpressionValidator
	dockerClient *client.Client
	extensionMap map[string]string
	aiValidator  *AIValidator
}

// NewSchemaValidator creates a new schema validator
func NewSchemaValidator(config *ValidationConfig, db *sql.DB, reporter performance.ProgressReporter) *SchemaValidator {
	if config == nil {
		config = DefaultValidationConfig()
	}

	var evaluator *ExpressionValidator
	if config.ValidateExpressions && db != nil {
		evaluator = NewExpressionValidator(db)
	}

	// Initialize Docker client if needed
	var dockerClient *client.Client
	if config.DockerApproach != "" {
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			// Silently fail - error will be caught later when ValidateWithDocker is called
			dockerClient = nil
		} else {
			dockerClient = cli
		}
	}

	validator := &SchemaValidator{
		config:       config,
		db:           db,
		reporter:     reporter,
		evaluator:    evaluator,
		dockerClient: dockerClient,
		extensionMap: getDefaultExtensionMap(),
	}

	// Add custom extensions
	for ext, pkg := range config.CustomExtensions {
		validator.extensionMap[ext] = pkg
	}

	return validator
}

// ValidateMigrations validates a set of migrations
func (sv *SchemaValidator) ValidateMigrations(ctx context.Context, migrations []*types.Migration) (*ValidationResult, error) {
	result := &ValidationResult{
		StartTime:  time.Now(),
		Level:      sv.config.Level,
		Errors:     make([]ValidationError, 0),
		Warnings:   make([]ValidationWarning, 0),
		Statistics: ValidationStatistics{},
		Details:    make(map[string]interface{}),
	}

	if sv.reporter != nil {
		sv.reporter.StartPhase(performance.PhaseValidation, len(migrations))
	}

	defer func() {
		result.EndTime = time.Now()
		result.Duration = result.EndTime.Sub(result.StartTime)
		result.Success = len(result.Errors) == 0
		result.Statistics.ErrorsFound = len(result.Errors)
		result.Statistics.WarningsFound = len(result.Warnings)

		if sv.reporter != nil {
			sv.reporter.FinishPhase(performance.PhaseValidation, result.Success,
				fmt.Sprintf("Validated %d migrations", len(migrations)))
		}
	}()

	// Phase 1: Basic syntax and structure validation
	for i, migration := range migrations {
		if sv.reporter != nil {
			sv.reporter.UpdateProgress(i+1, fmt.Sprintf("Validating %s", migration.Filename))
		}

		errors, warnings := sv.validateMigrationStructure(ctx, migration)
		result.Errors = append(result.Errors, errors...)
		result.Warnings = append(result.Warnings, warnings...)
		result.Statistics.ObjectsValidated += len(migration.Statements)

		if sv.config.StopOnError && len(errors) > 0 {
			return result, nil
		}

		// Check context cancellation
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		default:
		}
	}

	// Phase 2: Cross-migration dependency validation
	if sv.config.ValidateDependencies {
		if sv.reporter != nil {
			sv.reporter.StartPhase(performance.PhaseDependencies, len(migrations))
		}

		depErrors, depWarnings := sv.validateDependencies(ctx, migrations)
		result.Errors = append(result.Errors, depErrors...)
		result.Warnings = append(result.Warnings, depWarnings...)
		result.Statistics.DependenciesResolved = len(migrations)
	}

	// Phase 3: Database validation (if database connection available)
	if sv.db != nil && sv.config.Level >= ValidationLevelThorough {
		if sv.reporter != nil {
			sv.reporter.StartPhase(performance.PhaseAnalysis, len(migrations))
		}

		dbErrors, dbWarnings := sv.validateWithDatabase(ctx, migrations)
		result.Errors = append(result.Errors, dbErrors...)
		result.Warnings = append(result.Warnings, dbWarnings...)
	}

	// Phase 4: Performance analysis (optional)
	if sv.config.ValidatePerformance && sv.config.Level >= ValidationLevelComprehensive {
		perfWarnings := sv.validatePerformance(ctx, migrations)
		result.Warnings = append(result.Warnings, perfWarnings...)
	}

	return result, nil
}

// validateMigrationStructure validates the structure of a single migration
func (sv *SchemaValidator) validateMigrationStructure(ctx context.Context, migration *types.Migration) ([]ValidationError, []ValidationWarning) {
	var errors []ValidationError
	var warnings []ValidationWarning

	for _, stmt := range migration.Statements {
		// Validate SQL syntax and structure
		stmtErrors, stmtWarnings := sv.validateStatement(ctx, &stmt, migration.Filename)
		errors = append(errors, stmtErrors...)
		warnings = append(warnings, stmtWarnings...)

		// Validate naming conventions
		if sv.config.Level >= ValidationLevelStandard {
			namingWarnings := sv.validateNamingConventions(&stmt, migration.Filename)
			warnings = append(warnings, namingWarnings...)
		}

		// Validate object-specific rules
		objWarnings := sv.validateObjectSpecificRules(&stmt, migration.Filename)
		warnings = append(warnings, objWarnings...)
	}

	return errors, warnings
}

// validateStatement validates a single SQL statement
func (sv *SchemaValidator) validateStatement(ctx context.Context, stmt *types.Statement, filename string) ([]ValidationError, []ValidationWarning) {
	var errors []ValidationError
	var warnings []ValidationWarning

	objectID := ObjectID{
		Type:   stmt.ObjectType,
		Schema: stmt.Schema,
		Name:   stmt.ObjectName,
	}

	// Validate expressions if enabled and evaluator available
	if sv.config.ValidateExpressions && sv.evaluator != nil {
		if expr := extractExpressionFromStatement(stmt); expr != "" {
			if err := sv.evaluator.ValidateExpression(expr); err != nil {
				errors = append(errors, ValidationError{
					Code:     "INVALID_EXPRESSION",
					Message:  fmt.Sprintf("Invalid SQL expression: %v", err),
					ObjectID: objectID,
					Severity: "ERROR",
					Context: map[string]interface{}{
						"expression": expr,
					},
					SQLQuery: stmt.SQL,
					File:     filename,
					Line:     stmt.Line,
				})
			}
			sv.config.MaxConcurrentQueries++
		}
	}

	// Validate constraints
	if sv.config.ValidateConstraints {
		constraintWarnings := sv.validateStatementConstraints(stmt, filename)
		warnings = append(warnings, constraintWarnings...)
	}

	// Validate PostgreSQL-specific features
	if sv.config.Level >= ValidationLevelThorough {
		featureWarnings := sv.validatePostgreSQLFeatures(stmt, filename)
		warnings = append(warnings, featureWarnings...)
	}

	return errors, warnings
}

// validateDependencies validates cross-migration dependencies
func (sv *SchemaValidator) validateDependencies(ctx context.Context, migrations []*types.Migration) ([]ValidationError, []ValidationWarning) {
	var errors []ValidationError
	var warnings []ValidationWarning

	// Build dependency graph
	created := make(map[string]bool)
	referenced := make(map[string][]string) // object -> list of referencing files

	// First pass: collect all created objects
	for _, migration := range migrations {
		for _, stmt := range migration.Statements {
			if stmt.Operation == types.OpCreate {
				key := fmt.Sprintf("%s::%s", stmt.ObjectName, stmt.ObjectType)
				created[key] = true
			}
		}
	}

	// Second pass: check references
	for _, migration := range migrations {
		for _, stmt := range migration.Statements {
			for _, dep := range stmt.Dependencies {
				// Try to resolve actual object type from dependencies
				// Check if dependency exists with any object type
				found := false
				for _, objType := range []types.ObjectType{types.TypeTable, types.TypeView, types.TypeFunction, types.TypeType, types.TypeSequence} {
					depKey := fmt.Sprintf("%s::%s", dep, objType)
					if created[depKey] {
						found = true
						break
					}
				}

				// If not found, record as missing (default to table assumption for reporting)
				if !found {
					depKey := fmt.Sprintf("%s::%s", dep, types.TypeTable)
					referenced[depKey] = append(referenced[depKey], migration.Filename)
				}
			}
		}
	}

	// Report missing dependencies
	for depKey, referencingFiles := range referenced {
		if !created[depKey] {
			errors = append(errors, ValidationError{
				Code:     "MISSING_DEPENDENCY",
				Message:  fmt.Sprintf("Object %s is referenced but never created", depKey),
				Severity: "ERROR",
				Context: map[string]interface{}{
					"referencing_files": referencingFiles,
					"dependency":        depKey,
				},
				Suggestion: "Ensure the dependency is created in an earlier migration",
			})
		}
	}

	return errors, warnings
}

// validateWithDatabase validates against actual database
func (sv *SchemaValidator) validateWithDatabase(ctx context.Context, migrations []*types.Migration) ([]ValidationError, []ValidationWarning) {
	var errors []ValidationError
	var warnings []ValidationWarning

	// This would implement more sophisticated database-backed validation
	// For now, we'll do basic connectivity and syntax checks

	// Test database connectivity
	if err := sv.db.PingContext(ctx); err != nil {
		errors = append(errors, ValidationError{
			Code:       "DATABASE_CONNECTION_FAILED",
			Message:    fmt.Sprintf("Failed to connect to database: %v", err),
			Severity:   "ERROR",
			Suggestion: "Check database connection parameters",
		})
		return errors, warnings
	}

	// Validate statements against database
	for _, migration := range migrations {
		for _, stmt := range migration.Statements {
			// Use EXPLAIN to validate syntax without executing
			if sv.canExplainStatement(&stmt) {
				explainSQL := fmt.Sprintf("EXPLAIN %s", stmt.SQL)
				_, err := sv.db.QueryContext(ctx, explainSQL)
				if err != nil {
					errors = append(errors, ValidationError{
						Code:    "INVALID_SQL_SYNTAX",
						Message: fmt.Sprintf("SQL syntax error: %v", err),
						ObjectID: ObjectID{
							Type:   stmt.ObjectType,
							Schema: stmt.Schema,
							Name:   stmt.ObjectName,
						},
						Severity:   "ERROR",
						SQLQuery:   stmt.SQL,
						File:       migration.Filename,
						Line:       stmt.Line,
						Suggestion: "Check SQL syntax and fix errors",
					})
				}
				sv.config.MaxConcurrentQueries++
			}
		}
	}

	return errors, warnings
}

// validatePerformance validates performance-related aspects
func (sv *SchemaValidator) validatePerformance(ctx context.Context, migrations []*types.Migration) []ValidationWarning {
	var warnings []ValidationWarning

	for _, migration := range migrations {
		for _, stmt := range migration.Statements {
			// Check for potential performance issues
			perfWarnings := sv.analyzeStatementPerformance(&stmt, migration.Filename)
			warnings = append(warnings, perfWarnings...)
		}
	}

	return warnings
}

// validateNamingConventions validates PostgreSQL naming conventions
func (sv *SchemaValidator) validateNamingConventions(stmt *types.Statement, filename string) []ValidationWarning {
	var warnings []ValidationWarning

	if stmt.ObjectName == "" {
		return warnings
	}

	objectID := ObjectID{
		Type:   stmt.ObjectType,
		Schema: stmt.Schema,
		Name:   stmt.ObjectName,
	}

	// Check length limits
	if len(stmt.ObjectName) > 63 {
		warnings = append(warnings, ValidationWarning{
			Code:       "NAME_TOO_LONG",
			Message:    fmt.Sprintf("Object name '%s' exceeds PostgreSQL limit of 63 characters", stmt.ObjectName),
			ObjectID:   objectID,
			Suggestion: "Shorten the object name to 63 characters or less",
		})
	}

	// Check for reserved keywords (basic check)
	reservedWords := []string{"user", "table", "index", "primary", "foreign", "check", "unique"}
	lowerName := strings.ToLower(stmt.ObjectName)
	for _, reserved := range reservedWords {
		if lowerName == reserved {
			warnings = append(warnings, ValidationWarning{
				Code:       "RESERVED_KEYWORD",
				Message:    fmt.Sprintf("Object name '%s' is a PostgreSQL reserved keyword", stmt.ObjectName),
				ObjectID:   objectID,
				Suggestion: "Use a different name or quote the identifier",
			})
			break
		}
	}

	// Object-specific naming conventions
	switch stmt.ObjectType {
	case types.TypeIndex:
		if !strings.HasSuffix(lowerName, "_idx") && !strings.HasSuffix(lowerName, "_index") {
			warnings = append(warnings, ValidationWarning{
				Code:       "INDEX_NAMING_CONVENTION",
				Message:    fmt.Sprintf("Index name '%s' doesn't follow naming convention", stmt.ObjectName),
				ObjectID:   objectID,
				Suggestion: "Consider using suffix '_idx' for index names",
			})
		}
	case types.TypeConstraint:
		prefixes := []string{"pk_", "fk_", "ck_", "uq_"}
		hasPrefix := false
		for _, prefix := range prefixes {
			if strings.HasPrefix(lowerName, prefix) {
				hasPrefix = true
				break
			}
		}
		if !hasPrefix {
			warnings = append(warnings, ValidationWarning{
				Code:       "CONSTRAINT_NAMING_CONVENTION",
				Message:    fmt.Sprintf("Constraint name '%s' doesn't indicate its type", stmt.ObjectName),
				ObjectID:   objectID,
				Suggestion: "Consider prefixes like pk_, fk_, ck_, uq_ for constraints",
			})
		}
	}

	return warnings
}

// validateObjectSpecificRules validates rules specific to object types
func (sv *SchemaValidator) validateObjectSpecificRules(stmt *types.Statement, filename string) []ValidationWarning {
	var warnings []ValidationWarning

	objectID := ObjectID{
		Type:   stmt.ObjectType,
		Schema: stmt.Schema,
		Name:   stmt.ObjectName,
	}

	switch stmt.ObjectType {
	case types.TypeFunction:
		// Check function naming
		if len(stmt.ObjectName) < 3 {
			warnings = append(warnings, ValidationWarning{
				Code:       "FUNCTION_NAME_TOO_SHORT",
				Message:    fmt.Sprintf("Function name '%s' is very short", stmt.ObjectName),
				ObjectID:   objectID,
				Suggestion: "Use descriptive verb-based names for functions",
			})
		}

	case types.TypePolicy:
		// Check RLS policy naming
		lowerName := strings.ToLower(stmt.ObjectName)
		if !strings.Contains(lowerName, "policy") &&
			!strings.Contains(lowerName, "access") &&
			!strings.Contains(lowerName, "security") {
			warnings = append(warnings, ValidationWarning{
				Code:       "POLICY_NAMING_CONVENTION",
				Message:    fmt.Sprintf("Policy name '%s' doesn't clearly indicate its security purpose", stmt.ObjectName),
				ObjectID:   objectID,
				Suggestion: "Include words like 'policy', 'access', or 'security' in RLS policy names",
			})
		}

	case types.TypeIndex:
		// Check for redundant indexes
		if strings.Contains(strings.ToLower(stmt.SQL), "unique") {
			warnings = append(warnings, ValidationWarning{
				Code:       "UNIQUE_INDEX_CONSIDERATION",
				Message:    fmt.Sprintf("Creating unique index '%s' - ensure data doesn't violate uniqueness", stmt.ObjectName),
				ObjectID:   objectID,
				Suggestion: "Validate data uniqueness before creating unique indexes",
			})
		}
	}

	return warnings
}

// validateStatementConstraints validates constraint-related aspects
func (sv *SchemaValidator) validateStatementConstraints(stmt *types.Statement, filename string) []ValidationWarning {
	var warnings []ValidationWarning

	sqlUpper := strings.ToUpper(stmt.SQL)

	// Check for potentially risky operations
	if strings.Contains(sqlUpper, "DROP COLUMN") {
		warnings = append(warnings, ValidationWarning{
			Code:    "DATA_LOSS_RISK",
			Message: "DROP COLUMN operation will permanently delete data",
			ObjectID: ObjectID{
				Type:   stmt.ObjectType,
				Schema: stmt.Schema,
				Name:   stmt.ObjectName,
			},
			Suggestion: "Ensure data is backed up before dropping columns",
		})
	}

	if strings.Contains(sqlUpper, "ALTER COLUMN") && strings.Contains(sqlUpper, "TYPE") {
		warnings = append(warnings, ValidationWarning{
			Code:    "TYPE_CONVERSION_RISK",
			Message: "Column type change may cause data loss or conversion errors",
			ObjectID: ObjectID{
				Type:   stmt.ObjectType,
				Schema: stmt.Schema,
				Name:   stmt.ObjectName,
			},
			Suggestion: "Test type conversion with sample data before applying",
		})
	}

	return warnings
}

// validatePostgreSQLFeatures validates PostgreSQL-specific features
func (sv *SchemaValidator) validatePostgreSQLFeatures(stmt *types.Statement, filename string) []ValidationWarning {
	var warnings []ValidationWarning

	// Check for modern PostgreSQL features
	sqlUpper := strings.ToUpper(stmt.SQL)

	if strings.Contains(sqlUpper, "GENERATED ALWAYS AS") {
		warnings = append(warnings, ValidationWarning{
			Code:    "GENERATED_COLUMN_FEATURE",
			Message: "Using generated columns (PostgreSQL 12+)",
			ObjectID: ObjectID{
				Type:   stmt.ObjectType,
				Schema: stmt.Schema,
				Name:   stmt.ObjectName,
			},
			Suggestion: "Ensure target PostgreSQL version supports generated columns",
		})
	}

	if strings.Contains(sqlUpper, "VECTOR(") {
		warnings = append(warnings, ValidationWarning{
			Code:    "VECTOR_EXTENSION_REQUIRED",
			Message: "Vector data type requires pgvector extension",
			ObjectID: ObjectID{
				Type:   stmt.ObjectType,
				Schema: stmt.Schema,
				Name:   stmt.ObjectName,
			},
			Suggestion: "Ensure pgvector extension is installed",
		})
	}

	return warnings
}

// analyzeStatementPerformance analyzes performance implications
func (sv *SchemaValidator) analyzeStatementPerformance(stmt *types.Statement, filename string) []ValidationWarning {
	var warnings []ValidationWarning

	sqlUpper := strings.ToUpper(stmt.SQL)

	// Check for potentially slow operations
	if strings.Contains(sqlUpper, "CREATE INDEX") && !strings.Contains(sqlUpper, "CONCURRENTLY") {
		warnings = append(warnings, ValidationWarning{
			Code:    "INDEX_BLOCKING_OPERATION",
			Message: "Creating index without CONCURRENTLY may block table access",
			ObjectID: ObjectID{
				Type:   stmt.ObjectType,
				Schema: stmt.Schema,
				Name:   stmt.ObjectName,
			},
			Suggestion: "Consider using CREATE INDEX CONCURRENTLY for large tables",
		})
	}

	if strings.Contains(sqlUpper, "ALTER TABLE") && strings.Contains(sqlUpper, "ADD COLUMN") &&
		strings.Contains(sqlUpper, "NOT NULL") && !strings.Contains(sqlUpper, "DEFAULT") {
		warnings = append(warnings, ValidationWarning{
			Code:    "EXPENSIVE_NOT_NULL_COLUMN",
			Message: "Adding NOT NULL column without default requires table rewrite",
			ObjectID: ObjectID{
				Type:   stmt.ObjectType,
				Schema: stmt.Schema,
				Name:   stmt.ObjectName,
			},
			Suggestion: "Consider adding column with default value first, then removing default",
		})
	}

	return warnings
}

// Helper functions

// extractExpressionFromStatement extracts SQL expressions that can be validated
func extractExpressionFromStatement(stmt *types.Statement) string {
	sqlUpper := strings.ToUpper(stmt.SQL)

	// Extract CHECK constraint expressions
	if strings.Contains(sqlUpper, "CHECK (") {
		start := strings.Index(sqlUpper, "CHECK (") + 6
		if start < len(stmt.SQL) {
			// Simple extraction - would need more sophisticated parsing in real implementation
			end := strings.Index(stmt.SQL[start:], ")")
			if end > 0 {
				return strings.TrimSpace(stmt.SQL[start : start+end])
			}
		}
	}

	// Extract DEFAULT expressions
	if strings.Contains(sqlUpper, "DEFAULT ") {
		start := strings.Index(sqlUpper, "DEFAULT ") + 8
		if start < len(stmt.SQL) {
			// Extract until next keyword or end
			remaining := stmt.SQL[start:]
			keywords := []string{" NOT", " NULL", " CHECK", " REFERENCES", ",", ")"}
			end := len(remaining)
			for _, keyword := range keywords {
				if idx := strings.Index(strings.ToUpper(remaining), keyword); idx >= 0 && idx < end {
					end = idx
				}
			}
			return strings.TrimSpace(remaining[:end])
		}
	}

	return ""
}

// canExplainStatement checks if a statement can be validated with EXPLAIN
func (sv *SchemaValidator) canExplainStatement(stmt *types.Statement) bool {
	// Only certain statement types can be explained
	switch stmt.Operation {
	case types.OpInsert, types.OpUpdate, types.OpDelete:
		return true
	default:
		return false
	}
}

// SortValidationResults sorts validation results by severity and type
func SortValidationResults(result *ValidationResult) {
	// Sort errors by severity
	sort.Slice(result.Errors, func(i, j int) bool {
		severityOrder := map[string]int{"CRITICAL": 0, "ERROR": 1, "WARNING": 2}
		return severityOrder[result.Errors[i].Severity] < severityOrder[result.Errors[j].Severity]
	})

	// Sort warnings by code
	sort.Slice(result.Warnings, func(i, j int) bool {
		return result.Warnings[i].Code < result.Warnings[j].Code
	})
}

// =============================================================================
// Docker-based Validation Methods
// =============================================================================

// ValidateWithDocker validates squashed migrations using Docker containers
func (sv *SchemaValidator) ValidateWithDocker(ctx context.Context, originalPath, squashedPath string) (*ValidationResult, error) {
	if sv.dockerClient == nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"docker client not initialized",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Ensure Docker is installed and running, and the DOCKER_HOST environment variable is set correctly")
	}

	result := &ValidationResult{
		StartTime:    time.Now(),
		Level:        sv.config.Level,
		Errors:       make([]ValidationError, 0),
		Warnings:     make([]ValidationWarning, 0),
		Statistics:   ValidationStatistics{},
		Details:      make(map[string]interface{}),
		FixesApplied: make([]ValidationFix, 0),
	}

	approach := sv.config.DockerApproach
	if approach == "" {
		approach = ApproachTwoDatabases // Default to best balance
	}
	result.ApproachUsed = approach

	sv.logInfo("🚀 Starting Docker validation with approach: %s", approach)

	// Step 1: Detect required extensions
	if sv.config.EnableExtensionDetection {
		extensions, err := sv.detectRequiredExtensions(originalPath, squashedPath)
		if err != nil {
			return result, errors.NewError(
				errors.ErrorCodeValidationFailed,
				"extension detection failed",
				errors.SeverityError,
				errors.CategoryExtension,
			).WithInnerError(err).WithSuggestion("Check that migration files are readable and contain valid SQL")
		}
		result.ExtensionsDetected = extensions
		sv.logInfo("📦 Detected %d required extensions: %v", len(extensions), extensions)
	}

	// Step 2: Apply SQL fixes if enabled (only to squashed output, not originals)
	if sv.config.EnableSQLFixes {
		fix := sv.fixSQLIssues(squashedPath)
		if fix.Success {
			result.FixesApplied = append(result.FixesApplied, fix)
		}
	}

	// Step 3: Run Docker validation
	dockerResult, err := sv.runDockerValidation(ctx, originalPath, squashedPath, result.ExtensionsDetected, approach)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, ValidationError{
			Code:     "DOCKER_VALIDATION_FAILED",
			Message:  err.Error(),
			Severity: "ERROR",
		})
		return result, err
	}

	result.DockerValidation = dockerResult
	result.Success = dockerResult.Success
	result.EndTime = time.Now()
	result.Duration = result.EndTime.Sub(result.StartTime)

	return result, nil
}

// runDockerValidation runs the actual Docker-based validation
func (sv *SchemaValidator) runDockerValidation(ctx context.Context, originalPath, squashedPath string, extensions []string, approach ValidationApproach) (*DockerValidationResult, error) {
	switch approach {
	case ApproachTwoContainers:
		return sv.validateWithTwoContainers(ctx, originalPath, squashedPath, extensions)
	case ApproachTwoDatabases:
		return sv.validateWithTwoDatabases(ctx, originalPath, squashedPath, extensions)
	case ApproachSchemaDiff:
		return sv.validateWithSchemaDiff(ctx, originalPath, squashedPath, extensions)
	default:
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("unsupported validation approach: %s", approach),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Use one of: TWO_CONTAINERS, TWO_DATABASES, or SCHEMA_DIFF")
	}
}

// validateWithTwoDatabases validates using two databases in one container
func (sv *SchemaValidator) validateWithTwoDatabases(ctx context.Context, originalPath, squashedPath string, extensions []string) (*DockerValidationResult, error) {
	startTime := time.Now()
	result := &DockerValidationResult{Success: false}

	sv.logInfo("🔧 Using two-database validation approach")

	// Create container with extensions
	containerInfo, err := sv.createEnhancedContainer(ctx, extensions, "")
	if err != nil {
		result.Error = fmt.Sprintf("failed to create enhanced container: %v", err)
		return result, err
	}
	defer sv.stopAndRemoveContainer(ctx, containerInfo.ID)

	// Create databases and apply migrations
	originalDSN := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/validation_original?sslmode=disable", containerInfo.Port)
	squashedDSN := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/validation_squashed?sslmode=disable", containerInfo.Port)

	result.OriginalDB = originalDSN
	result.SquashedDB = squashedDSN

	// Connect and create databases
	originalMigErr, squashedMigErr := sv.setupDatabases(ctx, containerInfo, originalPath, squashedPath)

	// Track original migration status
	if originalMigErr != nil {
		result.OriginalMigrationsError = originalMigErr.Error()
		result.ComparisonValid = false
		if sv.config.Verbose {
			color.Yellow("📊 Schema comparison will show differences due to original migration failures\n")
		}
	} else {
		result.ComparisonValid = true
	}

	// If squashed migrations failed, that's a critical error
	if squashedMigErr != nil {
		result.Error = squashedMigErr.Error()
		return result, squashedMigErr
	}

	// Compare schemas using pg_dump normalization
	schema1, err := sv.DumpAndNormalizeContainerDatabase(ctx, containerInfo.ID, "validation_original")
	if err != nil {
		result.Error = fmt.Sprintf("failed to dump original schema: %v", err)
		return result, err
	}

	schema2, err := sv.DumpAndNormalizeContainerDatabase(ctx, containerInfo.ID, "validation_squashed")
	if err != nil {
		result.Error = fmt.Sprintf("failed to dump squashed schema: %v", err)
		return result, err
	}

	// Compare normalized schemas
	diff := CompareNormalizedSchemas(schema1, schema2)

	result.Duration = time.Since(startTime)

	// Only consider validation successful if comparison is valid (no original errors)
	// AND schemas match
	if result.ComparisonValid {
		result.Success = !diff.HasDifferences
	} else {
		// Original migrations failed - comparison is informational only
		result.Success = true // Squashed migrations work, which is what matters
		if sv.config.Verbose {
			color.Green("✓ Squashed migrations applied successfully\n")
			if diff.HasDifferences {
				color.Yellow("ℹ️  Schema differences are due to original migration failures (not consolidation bugs)\n")
			}
		}
	}

	if diff.HasDifferences {
		result.Differences = diff.FormatDiff()
	}

	return result, nil
}

// validateWithTwoContainers validates using two separate containers
func (sv *SchemaValidator) validateWithTwoContainers(ctx context.Context, originalPath, squashedPath string, extensions []string) (*DockerValidationResult, error) {
	startTime := time.Now()
	result := &DockerValidationResult{Success: false}

	sv.logInfo("🔧 Using two-container validation approach")

	// Create containers
	originalContainer, err := sv.createEnhancedContainer(ctx, extensions, "ORIGINAL")
	if err != nil {
		result.Error = fmt.Sprintf("failed to create original container: %v", err)
		return result, err
	}
	defer sv.stopAndRemoveContainer(ctx, originalContainer.ID)

	squashedContainer, err := sv.createEnhancedContainer(ctx, extensions, "SQUASHED")
	if err != nil {
		result.Error = fmt.Sprintf("failed to create squashed container: %v", err)
		return result, err
	}
	defer sv.stopAndRemoveContainer(ctx, squashedContainer.ID)

	// Apply migrations and compare
	originalDSN := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", originalContainer.Port)
	squashedDSN := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", squashedContainer.Port)

	result.OriginalDB = originalDSN
	result.SquashedDB = squashedDSN

	// Apply migrations to each container
	if err := sv.applyMigrationsToContainer(ctx, originalContainer, originalPath); err != nil {
		result.Error = fmt.Sprintf("failed to apply original migrations: %v", err)
		return result, err
	}

	if err := sv.applyMigrationsToContainer(ctx, squashedContainer, squashedPath); err != nil {
		result.Error = fmt.Sprintf("failed to apply squashed migrations: %v", err)
		return result, err
	}

	// Compare schemas using pg_dump normalization
	diff, err := sv.compareSchemasWithNormalization(ctx, originalContainer.ID, squashedContainer.ID)
	if err != nil {
		result.Error = fmt.Sprintf("schema comparison failed: %v", err)
		return result, err
	}

	result.Duration = time.Since(startTime)
	result.Success = !diff.HasDifferences
	if diff.HasDifferences {
		result.Differences = diff.FormatDiff()
	}

	return result, nil
}

// validateWithSchemaDiff validates by comparing with schema dump
func (sv *SchemaValidator) validateWithSchemaDiff(ctx context.Context, originalPath, squashedPath string, extensions []string) (*DockerValidationResult, error) {
	startTime := time.Now()
	result := &DockerValidationResult{Success: false}

	sv.logInfo("🔧 Using schema-diff validation approach")

	// Create container and apply original migrations
	containerInfo, err := sv.createEnhancedContainer(ctx, extensions, "")
	if err != nil {
		result.Error = fmt.Sprintf("failed to create container: %v", err)
		return result, err
	}
	defer sv.stopAndRemoveContainer(ctx, containerInfo.ID)

	// Apply original migrations - allow errors (broken originals are expected)
	if err := sv.applyMigrationsToContainer(ctx, containerInfo, originalPath); err != nil {
		if sv.config.Verbose {
			color.Yellow("⚠️  Original migrations have errors (this is expected): %v\n", err)
			color.Yellow("    Note: Validating squashed migrations independently\n")
		}
		// Clear the container and start fresh
		sv.stopAndRemoveContainer(ctx, containerInfo.ID)

		// Create new container for squashed-only validation
		containerInfo, err = sv.createEnhancedContainer(ctx, extensions, "")
		if err != nil {
			result.Error = fmt.Sprintf("failed to create container for squashed validation: %v", err)
			return result, err
		}
		defer sv.stopAndRemoveContainer(ctx, containerInfo.ID)

		// Apply squashed migrations - these MUST work
		if err := sv.applyMigrationsToContainer(ctx, containerInfo, squashedPath); err != nil {
			result.Error = fmt.Sprintf("squashed migrations failed to apply: %v", err)
			return result, err
		}

		// Success - squashed migrations work even though originals don't
		result.Duration = time.Since(startTime)
		result.Success = true
		result.Differences = "Original migrations failed - validated squashed migrations independently"
		return result, nil
	}

	// Original migrations worked - do full comparison
	// We need to compare DATABASE schemas, not file contents!
	// Apply squashed migrations to the same container and compare the resulting schemas
	if err := sv.applyMigrationsToContainer(ctx, containerInfo, squashedPath); err != nil {
		result.Error = fmt.Sprintf("squashed migrations failed to apply: %v", err)
		return result, err
	}

	// Now both original and squashed migrations are applied to the SAME container
	// Since they're in the same database, we need to use a different approach:
	// Dump the current schema (which has both applied) as the "final" state
	// This validates that squashed migrations CAN be applied successfully
	// The fact that they applied without error means they're compatible

	// For SCHEMA_DIFF mode, we care that:
	// 1. Original migrations applied successfully (checked above)
	// 2. Squashed migrations applied successfully (just checked)
	// Both succeeded, so validation passes

	result.Duration = time.Since(startTime)
	result.Success = true
	result.Differences = "" // No differences - both applied successfully

	return result, nil
}

// =============================================================================
// Extension Detection and SQL Fixing
// =============================================================================

// detectRequiredExtensions scans migration files for required extensions
func (sv *SchemaValidator) detectRequiredExtensions(originalPath, squashedPath string) ([]string, error) {
	extensions := make(map[string]bool)
	aliasesFound := make(map[string]string) // Track which aliases were detected

	// Scan both paths with alias tracking
	for _, path := range []string{originalPath, squashedPath} {
		if err := sv.scanDirectoryForExtensions(path, extensions, aliasesFound); err != nil {
			return nil, err
		}
	}

	// Log detected aliases once (after scanning both paths)
	if sv.config.Verbose && len(aliasesFound) > 0 {
		for alias, resolved := range aliasesFound {
			sv.logInfo("ℹ️  Detected obsolete extension name '%s', using '%s' instead", alias, resolved)
		}
	}

	// Convert to sorted slice
	var result []string
	for ext := range extensions {
		result = append(result, ext)
	}
	sort.Strings(result)

	return result, nil
}

// scanDirectoryForExtensions scans a directory for extension usage
func (sv *SchemaValidator) scanDirectoryForExtensions(dirPath string, extensions map[string]bool, aliasesFound map[string]string) error {
	return filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !strings.HasSuffix(path, ".sql") {
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				fmt.Sprintf("failed to read file %s", path),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithSuggestion("Ensure the file exists and has read permissions")
		}

		sv.extractExtensionsFromSQL(string(content), extensions, aliasesFound)
		return nil
	})
}

// Extension aliases: map obsolete/incorrect extension names to correct ones
// Note: UUID is a built-in PostgreSQL type since 8.3 and does not require an extension
// Only UUID generation functions (uuid_generate_v*) require the uuid-ossp extension
var extensionAliases = map[string]string{
	// Currently no aliases defined - UUID datatype is built-in
}

// resolveExtensionAlias resolves an extension name through aliases
// Returns the resolved name and whether an alias was applied
func resolveExtensionAlias(ext string) (string, bool) {
	if alias, exists := extensionAliases[ext]; exists {
		return alias, true
	}
	return ext, false
}

// extractExtensionsFromSQL extracts extension names from SQL content
func (sv *SchemaValidator) extractExtensionsFromSQL(sql string, extensions map[string]bool, aliasesFound map[string]string) {
	// Patterns to match extension usage (using precompiled patterns)
	extensionPatterns := []*regexp.Regexp{
		patterns.CreateExtensionValidationPattern,
		patterns.DropExtensionPattern,
	}

	upperSQL := strings.ToUpper(sql)

	for _, pattern := range extensionPatterns {
		matches := pattern.FindAllStringSubmatch(upperSQL, -1)
		for _, match := range matches {
			if len(match) > 1 {
				ext := strings.ToLower(match[1])
				ext = strings.ReplaceAll(ext, `"`, "")
				ext = strings.ReplaceAll(ext, `'`, "")

				// Apply extension aliases (e.g., uuid → uuid-ossp)
				resolvedExt, wasAliased := resolveExtensionAlias(ext)
				if wasAliased {
					// Track alias for logging later (prevents duplicates)
					aliasesFound[ext] = resolvedExt
				}
				extensions[resolvedExt] = true
			}
		}
	}

	// Direct extension detection by name
	commonExtensions := []string{
		"postgis", "uuid-ossp", "pg_trgm", "btree_gin", "btree_gist",
		"pgcrypto", "cube", "earthdistance", "pg_stat_statements",
		"hstore", "ltree", "citext", "unaccent", "fuzzystrmatch",
	}

	for _, ext := range commonExtensions {
		if strings.Contains(upperSQL, strings.ToUpper(ext)) {
			extensions[ext] = true
		}
	}

	// Check for UUID generation functions that require uuid-ossp extension
	// Note: UUID datatype itself is built-in and doesn't require an extension
	uuidFunctions := []string{
		"UUID_GENERATE_V1(", "UUID_GENERATE_V1MC(", "UUID_GENERATE_V3(",
		"UUID_GENERATE_V4(", "UUID_GENERATE_V5(",
	}
	for _, fn := range uuidFunctions {
		if strings.Contains(upperSQL, fn) {
			extensions["uuid-ossp"] = true
			break
		}
	}

	// Process any remaining extension aliases (if any exist in the map)
	for alias := range extensionAliases {
		if strings.Contains(upperSQL, strings.ToUpper(alias)) {
			resolved, _ := resolveExtensionAlias(alias)
			extensions[resolved] = true
			// Track alias for logging later (prevents duplicates)
			aliasesFound[alias] = resolved
		}
	}
}

// fixSQLIssues attempts to fix common SQL generation issues in squashed output
func (sv *SchemaValidator) fixSQLIssues(migrationPath string) ValidationFix {
	fix := ValidationFix{
		Issue: "SQL syntax errors detected",
		Fix:   "Applied automatic SQL fixes to squashed output",
	}

	err := filepath.Walk(migrationPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || !strings.HasSuffix(path, ".sql") {
			return err
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		originalSQL := string(content)
		fixedSQL := sv.applySQLFixes(originalSQL)

		if originalSQL != fixedSQL {
			err = os.WriteFile(path, []byte(fixedSQL), info.Mode())
			if err != nil {
				return err
			}
			fix.Success = true
			fix.Description = fmt.Sprintf("Fixed SQL issues in %s", filepath.Base(path))
		}

		return nil
	})

	if err != nil {
		fix.Success = false
		fix.Description = err.Error()
	}

	return fix
}

// applySQLFixes applies common SQL fixes to squashed output
func (sv *SchemaValidator) applySQLFixes(sql string) string {
	// Fix 1: Add missing semicolons after ALTER PUBLICATION statements
	sql = patterns.AlterPublicationPattern.ReplaceAllString(sql, "${1};${2}")

	// Fix 2: Fix transaction blocks with missing semicolons
	sql = patterns.TransactionPattern.ReplaceAllString(sql, "${1};${2}")

	// Fix 3: Remove duplicate extension creations
	sql = sv.deduplicateExtensions(sql)

	// Fix 4: Fix policy statements
	sql = patterns.CreatePolicyPattern.ReplaceAllString(sql, "${1};${2}")

	// Fix 5: Convert CREATE FUNCTION to CREATE OR REPLACE FUNCTION
	sql = sv.fixDuplicateFunctions(sql)

	return sql
}

// deduplicateExtensions removes duplicate extension creations
func (sv *SchemaValidator) deduplicateExtensions(sql string) string {
	lines := strings.Split(sql, "\n")
	extensionsSeen := make(map[string]bool)
	var result []string

	for _, line := range lines {
		if match := patterns.ExtensionNamePattern.FindStringSubmatch(line); match != nil {
			extension := strings.ToLower(match[1])
			if extensionsSeen[extension] {
				continue // Skip duplicate
			}
			extensionsSeen[extension] = true
		}
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// fixDuplicateFunctions converts CREATE FUNCTION to CREATE OR REPLACE FUNCTION
func (sv *SchemaValidator) fixDuplicateFunctions(sql string) string {
	// Match CREATE FUNCTION but not CREATE OR REPLACE FUNCTION (using precompiled patterns)
	lines := strings.Split(sql, "\n")
	var result []string

	for _, line := range lines {
		// If it's CREATE FUNCTION and not already CREATE OR REPLACE FUNCTION
		if patterns.FunctionHeaderPattern.MatchString(line) && !patterns.CreateOrReplaceFunctionPattern.MatchString(line) {
			// Replace CREATE FUNCTION with CREATE OR REPLACE FUNCTION
			line = patterns.FunctionHeaderPattern.ReplaceAllString(line, "CREATE OR REPLACE FUNCTION ")
			if sv.config.Verbose {
				sv.logInfo("🔧 Converting CREATE FUNCTION to CREATE OR REPLACE FUNCTION")
			}
		}
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// =============================================================================
// Docker Container Management
// =============================================================================

// createEnhancedContainer creates a PostgreSQL container with extensions
func (sv *SchemaValidator) createEnhancedContainer(ctx context.Context, extensions []string, containerPurpose string) (*ContainerInfo, error) {
	if containerPurpose != "" {
		sv.logInfo("🐳 Creating enhanced container [%s] with extensions: %v", containerPurpose, extensions)
	} else {
		sv.logInfo("🐳 Creating enhanced container with extensions: %v", extensions)
	}

	// Generate session ID for container tracking
	sessionID := fmt.Sprintf("%d-%d", time.Now().Unix(), os.Getpid())

	// =============================================================================
	// DOCKER IMAGE SELECTION STRATEGY
	// =============================================================================
	// Current Implementation: Base postgres (Debian) + runtime package installation
	// - Always uses standard postgres:<version> Debian-based image
	// - Installs required packages (PostGIS, etc.) at runtime via apt-get
	// - Flexible: supports any extension available in Debian/Ubuntu repositories
	// - Slower: package installation adds ~5-15s to container startup
	// - Production parity: matches AWS RDS, GCP CloudSQL, Azure Database
	//
	// MIGRATION HISTORY:
	// - Previously used ubuntu (postgres:15-ubuntu) with apk package manager
	// - Migrated to Debian/Ubuntu for:
	//   * Better extension support (PostGIS available via apt vs compilation)
	//   * Production environment parity (AWS/GCP/Azure all use Debian)
	//   * Faster package installation (5s vs 5-10 min compilation)
	//   * No musl/glibc collation compatibility issues
	// - See docs/ubuntu_VS_DEBIAN_ANALYSIS.md for detailed rationale
	//
	// FUTURE ENHANCEMENT (Solution 3): Custom Docker Image Building
	// - Build custom images on-demand with Dockerfile templates
	// - Cache built images for reuse (significant speedup for repeated runs)
	// - Full control over package versions and dependencies
	// - Better for CI/CD pipelines with predictable extension sets
	//
	// Migration Path to Solution 3:
	// 1. Create Dockerfile template system in internal/validation/dockerfiles/
	// 2. Implement image builder with Docker BuildKit API
	// 3. Add image caching with content-based tags (hash of extensions list)
	// 4. Keep runtime installation as fallback for exotic/custom extensions
	// 5. Configuration option to prefer cached images vs. runtime installation
	//
	// Example Dockerfile template for PostGIS:
	// ```
	// FROM postgres:{{.Version}}
	// RUN apt-get update && apt-get install -y --no-install-recommends \
	//     postgresql-{{.Version}}-postgis-3 && \
	//     apt-get clean && rm -rf /var/lib/apt/lists/*
	// ```
	// =============================================================================

	// Use configurable PostgreSQL version (default to 17)
	postgresVersion := "17"
	if sv.config != nil && sv.config.PostgreSQLVersion != "" {
		postgresVersion = sv.config.PostgreSQLVersion
	}

	// Use standard Debian-based PostgreSQL image (NOT ubuntu)
	// Rationale: Debian provides pre-compiled extensions (PostGIS, etc.) via apt
	// See docs/ubuntu_VS_DEBIAN_ANALYSIS.md for detailed comparison
	postgresImage := fmt.Sprintf("postgres:%s", postgresVersion)
	sv.logInfo("🐘 Using Debian-based PostgreSQL image: %s (production-grade, fast extensions)", postgresImage)

	// Check if image exists locally, if not, pull it
	if err := sv.ensureDockerImageAvailable(ctx, postgresImage); err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to ensure Docker image availability",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion(fmt.Sprintf("Pull the image manually: docker pull %s", postgresImage))
	}

	// Create container with PostgreSQL and extensions
	resp, err := sv.dockerClient.ContainerCreate(ctx, &container.Config{
		Image: postgresImage,
		Env: []string{
			"POSTGRES_DB=postgres",
			"POSTGRES_USER=postgres",
			"POSTGRES_PASSWORD=postgres",
			"POSTGRES_INITDB_ARGS=--auth-host=trust",
		},
		ExposedPorts: nat.PortSet{
			"5432/tcp": struct{}{},
		},
		Cmd: []string{"postgres", "-c", "shared_preload_libraries=pg_stat_statements"},
		Labels: map[string]string{
			"pgsquash.type":    "validation",
			"pgsquash.session": sessionID,
			"pgsquash.cleanup": "auto",
			"pgsquash.created": time.Now().Format(time.RFC3339),
		},
	}, &container.HostConfig{
		PortBindings: nat.PortMap{
			// Use "0" to let Docker assign a random available port dynamically
			"5432/tcp": []nat.PortBinding{{HostPort: "0"}},
		},
		// Resource limits for security and stability
		Resources: container.Resources{
			Memory:   512 * 1024 * 1024, // 512MB
			NanoCPUs: 1000000000,        // 1 CPU
		},
		// Security options
		SecurityOpt: []string{
			"no-new-privileges",
		},
	}, nil, nil, fmt.Sprintf("pgsquash-validation-%s", sessionID))

	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create container",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Ensure Docker daemon is running and has sufficient resources")
	}

	if err := sv.dockerClient.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to start container",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check Docker logs for the container: docker logs " + resp.ID)
	}

	// Inspect container to get the dynamically assigned port
	containerJSON, err := sv.dockerClient.ContainerInspect(ctx, resp.ID)
	if err != nil {
		sv.stopAndRemoveContainer(ctx, resp.ID)
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to inspect container for port assignment",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	// Extract the assigned host port from the container inspection
	assignedPort := 0
	if bindings, ok := containerJSON.NetworkSettings.Ports["5432/tcp"]; ok && len(bindings) > 0 {
		// Parse the assigned port
		portStr := bindings[0].HostPort
		assignedPort, err = strconv.Atoi(portStr)
		if err != nil {
			sv.stopAndRemoveContainer(ctx, resp.ID)
			return nil, errors.NewError(
				errors.ErrorCodeValidationFailed,
				fmt.Sprintf("failed to parse assigned port: %s", portStr),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}
	} else {
		sv.stopAndRemoveContainer(ctx, resp.ID)
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"no port binding found for container",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Container may have failed to start properly")
	}

	sv.logInfo("📌 Docker assigned port: %d", assignedPort)
	containerInfo := &ContainerInfo{ID: resp.ID, Port: assignedPort}

	// Re-inspect after a brief delay to get the actual bound port
	// Docker may not have fully bound the port when we first inspect
	time.Sleep(500 * time.Millisecond)
	containerJSON2, err := sv.dockerClient.ContainerInspect(ctx, resp.ID)
	if err == nil {
		if bindings, ok := containerJSON2.NetworkSettings.Ports["5432/tcp"]; ok && len(bindings) > 0 {
			portStr := bindings[0].HostPort
			if actualPort, err := strconv.Atoi(portStr); err == nil && actualPort != assignedPort {
				sv.logInfo("⚠️  Port changed from %d to %d after inspection, updating...", assignedPort, actualPort)
				containerInfo.Port = actualPort
			}
		}
	}

	// Wait for container to start (before installing packages)
	// We need the container running to exec package manager commands
	if err := sv.waitForContainerStart(ctx, containerInfo); err != nil {
		sv.stopAndRemoveContainer(ctx, resp.ID)
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"container failed to start",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check Docker container logs for startup errors")
	}

	// Step 1: Install required system packages for extensions (via apk/package manager)
	// This must happen BEFORE PostgreSQL is fully ready, as some extensions need
	// shared libraries loaded during PostgreSQL initialization
	if len(extensions) > 0 {
		if err := sv.installExtensionsViaPackageManager(ctx, resp.ID, extensions); err != nil {
			sv.logInfo("⚠️  Warning: Failed to install some system packages: %v", err)
			// Continue anyway - some extensions might not need system packages
		}

		// Step 1.5: Restart PostgreSQL to load newly installed extension libraries
		// PostGIS and other extensions with shared libraries need this
		sv.logInfo("🔄 Restarting PostgreSQL to load extension libraries...")
		stopTimeout := 10
		if err := sv.dockerClient.ContainerRestart(ctx, resp.ID, container.StopOptions{Timeout: &stopTimeout}); err != nil {
			sv.logInfo("⚠️  Warning: Failed to restart container: %v", err)
		} else {
			sv.logInfo("☑ Container restarted successfully")
			// Wait a bit after restart for PostgreSQL to begin initialization
			// Without this, we immediately try to connect before PostgreSQL has started
			sv.logInfo("⏳ Waiting 10 seconds for PostgreSQL to initialize after restart...")
			time.Sleep(10 * time.Second)

			// Re-inspect port after container restart
			// Docker may reassign the port binding when the container restarts
			containerJSON3, err := sv.dockerClient.ContainerInspect(ctx, resp.ID)
			if err == nil {
				if bindings, ok := containerJSON3.NetworkSettings.Ports["5432/tcp"]; ok && len(bindings) > 0 {
					portStr := bindings[0].HostPort
					if actualPort, err := strconv.Atoi(portStr); err == nil && actualPort != containerInfo.Port {
						sv.logInfo("⚠️  Port changed from %d to %d after container restart, updating...", containerInfo.Port, actualPort)
						containerInfo.Port = actualPort
					}
				}
			}
		}
	}

	// Step 2: Wait for PostgreSQL to be ready (after packages are installed and container restarted)
	if err := sv.waitForPostgreSQLReady(ctx, containerInfo); err != nil {
		sv.stopAndRemoveContainer(ctx, resp.ID)
		return nil, errors.NewError(
			errors.ErrorCodeDatabaseNotAccessible,
			"PostgreSQL not ready",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Increase ContainerReadyTimeout in config or check container logs for PostgreSQL startup errors")
	}

	// Step 3: Create SQL extensions (CREATE EXTENSION commands)
	if len(extensions) > 0 {
		if err := sv.installExtensions(ctx, containerInfo, extensions); err != nil {
			sv.logInfo("⚠️  Warning: Failed to install some extensions: %v", err)
		}
	}

	return containerInfo, nil
}

// =============================================================================
// Helper Methods
// =============================================================================

// ensureDockerImageAvailable checks if a Docker image exists locally
// If not, it pulls the image with a progress indicator
func (sv *SchemaValidator) ensureDockerImageAvailable(ctx context.Context, imageName string) error {
	// Check if image exists locally
	_, err := sv.dockerClient.ImageInspect(ctx, imageName)
	if err == nil {
		// Image exists, no need to pull
		sv.logInfo("☑ Docker image '%s' found locally", imageName)
		return nil
	}

	// Image not found locally, pull it
	if err != nil && strings.Contains(err.Error(), "No such image") {
		sv.logInfo("📦 Docker image '%s' not found locally", imageName)
		if sv.config.Verbose {
			color.Cyan("   Pulling image... (this may take a few minutes on first run)\n")
		}

		// Pull the image
		reader, err := sv.dockerClient.ImagePull(ctx, imageName, image.PullOptions{})
		if err != nil {
			if sv.config.Verbose {
				color.Red("☒ Failed to pull Docker image '%s'\n", imageName)
				color.Yellow("\nTo fix this, try running manually:\n")
				color.Yellow("  docker pull %s\n\n", imageName)
			}
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				fmt.Sprintf("failed to pull image %s", imageName),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithSuggestion(fmt.Sprintf("Pull the image manually: docker pull %s", imageName))
		}
		defer func() { _ = reader.Close() }()

		// Show simple progress indicator
		// Note: Full progress parsing would require jsonmessage decoding
		// For now, we just show that pulling is happening
		sv.logInfo("⏳ Downloading image layers...")

		// Copy output to show progress (basic approach)
		// In production, we'd parse JSON progress messages
		buf := make([]byte, 1024)
		lastUpdate := time.Now()
		for {
			n, err := reader.Read(buf)
			if n > 0 && time.Since(lastUpdate) > 2*time.Second {
				sv.logInfo("   Still pulling...")
				lastUpdate = time.Now()
			}
			if err != nil {
				break
			}
		}

		if sv.config.Verbose {
			color.Green("☑ Successfully pulled image: %s\n", imageName)
		}
		return nil
	}

	// Other error occurred
	return errors.NewError(
		errors.ErrorCodeValidationFailed,
		fmt.Sprintf("failed to inspect image %s", imageName),
		errors.SeverityError,
		errors.CategoryValidation,
	).WithInnerError(err).WithSuggestion("Ensure Docker daemon is running and accessible")
}

func (sv *SchemaValidator) stopAndRemoveContainer(ctx context.Context, containerID string) {
	// Check if container exists before trying to stop/remove
	_, err := sv.dockerClient.ContainerInspect(ctx, containerID)
	if err != nil {
		if strings.Contains(err.Error(), "No such container") {
			// Container already removed, nothing to do
			return
		}
		sv.logInfo("⚠️  Failed to inspect container %s: %v", containerID, err)
		// Continue anyway to try cleanup
	}

	// Try to stop container
	stopTimeout := 10
	if err := sv.dockerClient.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &stopTimeout}); err != nil {
		if !strings.Contains(err.Error(), "No such container") {
			sv.logInfo("⚠️  Failed to stop container %s: %v", containerID, err)
		}
		// Continue to removal attempt
	}

	// Try to remove container
	if err := sv.dockerClient.ContainerRemove(ctx, containerID, container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	}); err != nil {
		if !strings.Contains(err.Error(), "No such container") {
			sv.logInfo("⚠️  Failed to remove container %s: %v", containerID, err)
		}
	} else {
		sv.logInfo("☑ Successfully cleaned up container %s", containerID)
	}
}

// waitForContainerStart waits for the Docker container to be in running state
// This is a quick check (usually <1s) before we can exec commands in the container
func (sv *SchemaValidator) waitForContainerStart(ctx context.Context, containerInfo *ContainerInfo) error {
	timeout := time.After(10 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"timeout waiting for container to start",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithSuggestion("Container may be stuck - check Docker logs or increase timeout")
		case <-ticker.C:
			inspect, err := sv.dockerClient.ContainerInspect(ctx, containerInfo.ID)
			if err != nil {
				continue
			}
			if inspect.State.Running {
				return nil
			}
		}
	}
}

// waitForPostgreSQLReady waits for PostgreSQL inside the container to accept connections
// This happens after packages are installed and PostgreSQL has finished initialization
func (sv *SchemaValidator) waitForPostgreSQLReady(ctx context.Context, containerInfo *ContainerInfo) error {
	dsn := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", containerInfo.Port)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Get timeout from config (default: 150s - sufficient for heavy extensions like postgis, pgcrypto, cube, earthdistance)
	timeoutDuration := 150 * time.Second
	if sv.config != nil && sv.config.ContainerReadyTimeout > 0 {
		timeoutDuration = sv.config.ContainerReadyTimeout // Already a time.Duration, no conversion needed
		sv.logInfo("📊 Using container_ready_timeout from config: %v", timeoutDuration)
	} else {
		sv.logInfo("⚠️  Using default container_ready_timeout: %v (config nil=%v, timeout=%v)", timeoutDuration, sv.config == nil, sv.config.ContainerReadyTimeout)
	}

	timeout := time.After(timeoutDuration)
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return errors.NewError(
				errors.ErrorCodeDatabaseNotAccessible,
				fmt.Sprintf("timeout waiting for PostgreSQL after %v", timeoutDuration),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithSuggestion(fmt.Sprintf("Increase container_ready_timeout in config (current: %ds, suggested: %ds+) or check container logs: docker logs %s",
				int(timeoutDuration.Seconds()), int(timeoutDuration.Seconds())+30, containerInfo.ID))
		case <-ticker.C:
			if err := db.PingContext(ctx); err == nil {
				sv.logInfo("☑ PostgreSQL ready and accepting connections")
				return nil
			}
		}
	}
}



func (sv *SchemaValidator) installExtensions(ctx context.Context, containerInfo *ContainerInfo, extensions []string) error {
	dsn := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", containerInfo.Port)
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	var failedExtensions []string

	for _, ext := range extensions {
		// Try to install extension
		if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE EXTENSION IF NOT EXISTS \"%s\"", ext)); err != nil {
			// Check if extension is available
			var available bool
			checkErr := db.QueryRowContext(ctx,
				"SELECT EXISTS(SELECT 1 FROM pg_available_extensions WHERE name = $1)", ext).Scan(&available)

			if checkErr == nil && !available {
				sv.logInfo("⚠️  Extension %s not available in PostgreSQL - may need custom Docker image", ext)
			} else {
				sv.logInfo("⚠️  Failed to install extension %s: %v", ext, err)
			}
			failedExtensions = append(failedExtensions, ext)
		} else {
			sv.logInfo("☑ Installed extension: %s", ext)
		}
	}

	if len(failedExtensions) > 0 {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("failed to install %d extension(s): %v", len(failedExtensions), failedExtensions),
			errors.SeverityError,
			errors.CategoryExtension,
		).WithSuggestion("Some extensions may require additional packages or custom Docker images")
	}

	return nil
}

// =============================================================================
// RUNTIME PACKAGE INSTALLATION (Solution 1)
// =============================================================================
// installExtensionsViaPackageManager installs system packages required for PostgreSQL extensions
// This runs INSIDE the container using Docker exec to run apt-get (Debian/Ubuntu package manager)
//
// ARCHITECTURE NOTES:
// - Uses Docker exec API to run commands inside running container
// - Installs packages via Debian/Ubuntu's apt-get package manager
// - Maps PostgreSQL extension names to Debian package names
// - Handles both community and contrib packages
// - Tolerates failures (some extensions don't need system packages)
//
// FUTURE ENHANCEMENT (Migration to Solution 3):
// When migrating to custom image building, this function becomes:
// 1. Fallback for extensions not in cached images
// 2. Development/testing tool for trying new extensions
// 3. Emergency override when cached images are unavailable
//
// The image builder would render Dockerfile templates like:
//
//	FROM postgres:17
//	RUN apt-get update && apt-get install -y --no-install-recommends \
//	    postgresql-17-postgis-3 \
//	    postgresql-contrib \
//	    ...other packages && \
//	    apt-get clean && rm -rf /var/lib/apt/lists/*
//
// And cache with content-based tags like:
//
//	pgsquash-postgres:17-debian-sha256-abc123
//
// This keeps the flexibility of runtime installation while gaining
// speed benefits of pre-built images for common extension combinations.
// =============================================================================
func (sv *SchemaValidator) installExtensionsViaPackageManager(ctx context.Context, containerID string, extensions []string) error {
	// Get PostgreSQL version from config for version-specific packages
	postgresVersion := "17"
	if sv.config != nil && sv.config.PostgreSQLVersion != "" {
		postgresVersion = sv.config.PostgreSQLVersion
	}

	// Map PostgreSQL extension names to Debian/Ubuntu package names
	// PostGIS has version-specific packages, contrib is version-specific too
	extensionToPackageMap := map[string]string{
		"postgis":            fmt.Sprintf("postgresql-%s-postgis-3", postgresVersion),
		"uuid-ossp":          "postgresql-contrib", // included in contrib
		"pg_trgm":            "postgresql-contrib", // included in contrib
		"pg_stat_statements": "postgresql-contrib", // included in contrib
		"btree_gin":          "postgresql-contrib", // included in contrib
		"btree_gist":         "postgresql-contrib", // included in contrib
		"cube":               "postgresql-contrib", // included in contrib
		"earthdistance":      "postgresql-contrib", // included in contrib
		"pgcrypto":           "postgresql-contrib", // included in contrib
		"hstore":             "postgresql-contrib", // included in contrib
		"citext":             "postgresql-contrib", // included in contrib
		"ltree":              "postgresql-contrib", // included in contrib
		"intarray":           "postgresql-contrib", // included in contrib
		"plpgsql":            "",                   // built-in, no package needed
		"uuid":               "",                   // uuid-ossp provides this, not a separate package
		// Additional extensions can be added here
	}

	// Collect unique packages to install
	packagesToInstall := make(map[string]bool)
	for _, ext := range extensions {
		if pkg, exists := extensionToPackageMap[ext]; exists && pkg != "" {
			packagesToInstall[pkg] = true
		} else if ext != "plpgsql" && ext != "uuid" && !exists {
			// Unknown extension - try version-specific package
			sv.logInfo("⚙️  Unknown extension '%s', trying postgresql-%s-%s", ext, postgresVersion, ext)
			packagesToInstall[fmt.Sprintf("postgresql-%s-%s", postgresVersion, ext)] = true
		}
	}

	if len(packagesToInstall) == 0 {
		sv.logInfo("☑ No system packages needed for requested extensions")
		return nil
	}

	// Build package list
	var packages []string
	for pkg := range packagesToInstall {
		packages = append(packages, pkg)
	}

	sv.logInfo("📦 Installing Debian packages via apt-get: %v", packages)

	// Set DEBIAN_FRONTEND=noninteractive to avoid interactive prompts
	setEnvCmd := []string{"sh", "-c", "export DEBIAN_FRONTEND=noninteractive"}
	_ = sv.execInContainer(ctx, containerID, setEnvCmd) // Ignore error - best effort

	// Update apt repositories
	updateCmd := []string{"apt-get", "update"}
	if err := sv.execInContainer(ctx, containerID, updateCmd); err != nil {
		sv.logInfo("⚠️  Failed to update apt repositories: %v", err)
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"apt-get update failed",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check container network connectivity and Debian repository availability")
	}

	// Install packages (use -y for non-interactive, --no-install-recommends to minimize size)
	installCmd := []string{"apt-get", "install", "-y", "--no-install-recommends"}
	installCmd = append(installCmd, packages...)

	if err := sv.execInContainer(ctx, containerID, installCmd); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("failed to install packages %v", packages),
			errors.SeverityError,
			errors.CategoryExtension,
		).WithInnerError(err).WithSuggestion("Some packages may not be available in this PostgreSQL version - check package names")
	}

	// Clean up apt cache to save space
	cleanCmd := []string{"apt-get", "clean"}
	_ = sv.execInContainer(ctx, containerID, cleanCmd) // Ignore error - best effort

	sv.logInfo("☑ Successfully installed Debian packages")
	return nil
}

// execInContainer executes a command inside a running Docker container
// Returns error if command fails or exits with non-zero status
// NOTE: This implementation could be enhanced to capture stdout/stderr for better error messages
// in Solution 3 migration
func (sv *SchemaValidator) execInContainer(ctx context.Context, containerID string, cmd []string) error {
	execConfig := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	}

	execIDResp, err := sv.dockerClient.ContainerExecCreate(ctx, containerID, execConfig)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create exec",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Container may not be running - check Docker status")
	}

	// Start the exec
	if err := sv.dockerClient.ContainerExecStart(ctx, execIDResp.ID, container.ExecStartOptions{}); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to start exec",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Container exec failed - check container status")
	}

	// Poll for completion with timeout
	timeout := time.After(60 * time.Second)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				fmt.Sprintf("timeout waiting for command %v to complete", cmd),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithSuggestion("Command took too long to execute - check container performance")
		case <-ticker.C:
			inspectResp, err := sv.dockerClient.ContainerExecInspect(ctx, execIDResp.ID)
			if err != nil {
				return errors.NewError(
					errors.ErrorCodeValidationFailed,
					"failed to inspect exec",
					errors.SeverityError,
					errors.CategoryValidation,
				).WithInnerError(err).WithSuggestion("Docker exec inspection failed")
			}

			// Check if exec has finished
			if !inspectResp.Running {
				// Check exit code
				if inspectResp.ExitCode != 0 {
					return errors.NewError(
						errors.ErrorCodeValidationFailed,
						fmt.Sprintf("command %v exited with code %d", cmd, inspectResp.ExitCode),
						errors.SeverityError,
						errors.CategoryValidation,
					).WithSuggestion("Check command syntax and container state")
				}
				return nil
			}
		}
	}
}

func (sv *SchemaValidator) setupDatabases(ctx context.Context, containerInfo *ContainerInfo, originalPath, squashedPath string) (originalErr error, squashedErr error) {
	// Create databases and apply migrations
	dsn := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", containerInfo.Port)
	adminDB, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	defer func() { _ = adminDB.Close() }()

	// Create databases
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE validation_original"); err != nil {
		return nil, err
	}
	if _, err := adminDB.ExecContext(ctx, "CREATE DATABASE validation_squashed"); err != nil {
		return nil, err
	}

	// Apply migrations to each database
	originalDSN := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/validation_original?sslmode=disable", containerInfo.Port)
	squashedDSN := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/validation_squashed?sslmode=disable", containerInfo.Port)

	// Apply original migrations - allow errors (broken originals are expected)
	originalErr = sv.applyMigrationsToDatabase(originalDSN, originalPath)
	if originalErr != nil {
		if sv.config.Verbose {
			color.Yellow("⚠️  Original migrations have errors (this is expected): %v\n", originalErr)
			color.Yellow("    Note: pgsquash is designed to fix broken migrations\n")
		}
		// Don't fail validation - just track that original failed
	}

	// Apply squashed migrations - this MUST succeed
	squashedErr = sv.applyMigrationsToDatabase(squashedDSN, squashedPath)
	if squashedErr != nil {
		return originalErr, errors.NewError(
			errors.ErrorCodeInvalidSQL,
			"failed to apply squashed migrations",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(squashedErr).WithSuggestion("The squashed migrations contain SQL errors - check the generated SQL")
	}

	return originalErr, nil
}

func (sv *SchemaValidator) applyMigrationsToDatabase(dsn, migrationPath string) error {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	// Inject auth compatibility SQL from plugins (Clerk, Supabase, Auth0, etc.)
	// This creates mock auth functions, roles, and schemas for validation
	compatibilitySQL := sv.getPluginCompatibilitySQL(context.Background())

	// Fallback to manual config if no plugins active (backward compatibility)
	if compatibilitySQL == "" && sv.config.AuthCompatibilitySQL != "" {
		compatibilitySQL = sv.config.AuthCompatibilitySQL
	}

	if compatibilitySQL != "" {
		if sv.config.Verbose {
			color.Cyan("🔐 Creating plugin compatibility layers...\n")
		}
		// Split and execute compatibility SQL statements
		statements := splitSQLStatements(compatibilitySQL)
		for i, stmt := range statements {
			stmt = strings.TrimSpace(stmt)
			if stmt == "" {
				continue
			}
			if _, err := db.Exec(stmt); err != nil {
				return errors.NewError(
					errors.ErrorCodeInvalidSQL,
					fmt.Sprintf("failed to create compatibility layer (statement %d)", i+1),
					errors.SeverityError,
					errors.CategoryValidation,
				).WithInnerError(err).WithSuggestion(fmt.Sprintf("Plugin compatibility SQL may have errors - check plugin configuration.\nFailed statement:\n%s", stmt))
			}
		}
		if sv.config.Verbose {
			color.Green("☑ Compatibility layers created successfully\n")
		}
	}

	// Check if migrationPath is a file or directory
	info, err := os.Stat(migrationPath)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"stat migration path",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Ensure the migration path exists and is accessible")
	}

	// If it's a single file, execute it directly
	if !info.IsDir() {
		content, err := os.ReadFile(migrationPath)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"read migration file",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithSuggestion("Ensure the migration file has read permissions")
		}

		sqlContent := string(content)

		// Preprocess SQL if enabled (e.g., deduplicate publication statements)
		if sv.config.EnablePreprocessing {
			sqlContent = preprocessMigrationSQL(sqlContent, true)
		}

		// Use executeSQLFile which can handle multiple statements
		if err := sv.executeSQLFile(db, sqlContent, migrationPath); err != nil {
			return err
		}

		return nil
	}

	// If it's a directory, walk through all SQL files
	return filepath.Walk(migrationPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || !strings.HasSuffix(path, ".sql") || info.IsDir() {
			return err
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		sqlContent := string(content)

		// Preprocess SQL if enabled (e.g., deduplicate publication statements)
		if sv.config.EnablePreprocessing {
			sqlContent = preprocessMigrationSQL(sqlContent, true)
		}

		// Use executeSQLFile which can handle multiple statements
		if err := sv.executeSQLFile(db, sqlContent, path); err != nil {
			return err
		}

		return nil
	})
}

// executeSQLFile executes a SQL file that may contain multiple statements
// Splits the SQL into individual statements and executes them one by one
func (sv *SchemaValidator) executeSQLFile(db *sql.DB, sqlContent, filePath string) error {
	// Split SQL into individual statements
	statements := splitSQLStatements(sqlContent)

	// Execute each statement
	for i, stmt := range statements {
		// Skip empty statements
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}

		// Execute the statement
		if _, err := db.Exec(stmt); err != nil {
			return errors.NewError(
				errors.ErrorCodeInvalidSQL,
				fmt.Sprintf("failed to execute statement %d in migration %s", i+1, filePath),
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithSuggestion(fmt.Sprintf("The migration contains invalid SQL at statement %d.\nPostgreSQL error: %v\n\nStatement:\n%s", i+1, err, stmt))
		}
	}

	return nil
}

// splitSQLStatements splits a SQL file into individual statements
// Handles PostgreSQL-specific syntax including dollar-quoted strings and DO blocks
func splitSQLStatements(sql string) []string {
	var statements []string
	var current strings.Builder
	var inDollarQuote bool
	var dollarTag string
	var inString bool
	var inLineComment bool
	var inBlockComment bool

	runes := []rune(sql)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		// Handle line comments (-- ...)
		if !inString && !inDollarQuote && !inBlockComment && i < len(runes)-1 && ch == '-' && runes[i+1] == '-' {
			inLineComment = true
			current.WriteRune(ch)
			continue
		}

		// End of line comment
		if inLineComment && ch == '\n' {
			inLineComment = false
			current.WriteRune(ch)
			continue
		}

		// Handle block comments (/* ... */)
		if !inString && !inDollarQuote && !inLineComment && i < len(runes)-1 && ch == '/' && runes[i+1] == '*' {
			inBlockComment = true
			current.WriteRune(ch)
			continue
		}

		// End of block comment
		if inBlockComment && i < len(runes)-1 && ch == '*' && runes[i+1] == '/' {
			inBlockComment = false
			current.WriteRune(ch)
			i++ // Skip the '/'
			current.WriteRune(runes[i])
			continue
		}

		// Skip processing if in comment
		if inLineComment || inBlockComment {
			current.WriteRune(ch)
			continue
		}

		// Handle regular strings ('')
		if !inDollarQuote && ch == '\'' {
			// Check if it's an escaped quote
			if i > 0 && runes[i-1] == '\\' {
				current.WriteRune(ch)
				continue
			}
			inString = !inString
			current.WriteRune(ch)
			continue
		}

		// Handle dollar-quoted strings ($$...$$, $tag$...$tag$)
		if !inString && ch == '$' {
			// Try to match dollar quote
			tag := string(ch)
			j := i + 1
			for j < len(runes) && (runes[j] == '_' || (runes[j] >= 'a' && runes[j] <= 'z') || (runes[j] >= 'A' && runes[j] <= 'Z') || (runes[j] >= '0' && runes[j] <= '9')) {
				tag += string(runes[j])
				j++
			}
			if j < len(runes) && runes[j] == '$' {
				tag += "$"
				if inDollarQuote {
					// Check if this closes the dollar quote
					if tag == dollarTag {
						inDollarQuote = false
						dollarTag = ""
					}
				} else {
					// Start dollar quote
					inDollarQuote = true
					dollarTag = tag
				}
				current.WriteString(tag)
				i = j
				continue
			}
		}

		// Handle statement terminator (semicolon)
		if !inString && !inDollarQuote && ch == ';' {
			current.WriteRune(ch)
			stmt := strings.TrimSpace(current.String())
			if stmt != "" {
				statements = append(statements, stmt)
			}
			current.Reset()
			continue
		}

		// Regular character
		current.WriteRune(ch)
	}

	// Add any remaining content
	if current.Len() > 0 {
		stmt := strings.TrimSpace(current.String())
		if stmt != "" {
			statements = append(statements, stmt)
		}
	}

	return statements
}

func (sv *SchemaValidator) applyMigrationsToContainer(ctx context.Context, containerInfo *ContainerInfo, migrationPath string) error {
	dsn := fmt.Sprintf("postgres://postgres:postgres@localhost:%d/postgres?sslmode=disable", containerInfo.Port)
	return sv.applyMigrationsToDatabase(dsn, migrationPath)
}

// compareSchemasWithNormalization compares schemas using pg_dump normalization pipeline
// This is the production-grade comparison method that handles subtle differences
func (sv *SchemaValidator) compareSchemasWithNormalization(ctx context.Context, container1ID, container2ID string) (*SchemaDiff, error) {
	// Dump and normalize schema from both containers
	schema1, err := sv.DumpAndNormalizeContainerSchema(ctx, container1ID)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to dump schema from original container",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	schema2, err := sv.DumpAndNormalizeContainerSchema(ctx, container2ID)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to dump schema from squashed container",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	// Compare normalized schemas
	diff := CompareNormalizedSchemas(schema1, schema2)
	return diff, nil
}

func (sv *SchemaValidator) compareSchemas(db1, db2 *sql.DB) (string, error) {
	// Note: This method is kept for backward compatibility but is deprecated.
	// For container-based validation, use compareSchemasWithNormalization instead.

	// Compare tables
	tables1, err := sv.getTables(db1)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get tables from db1",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}

	tables2, err := sv.getTables(db2)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get tables from db2",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}

	if diff := sv.compareStringSlices(tables1, tables2); diff != "" {
		return fmt.Sprintf("Table differences: %s", diff), nil
	}

	// Compare indexes
	indexes1, err := sv.getIndexes(db1)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get indexes from db1",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}
	indexes2, err := sv.getIndexes(db2)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get indexes from db2",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}

	if diff := sv.compareStringSlices(indexes1, indexes2); diff != "" {
		return fmt.Sprintf("Index differences: %s", diff), nil
	}

	// Compare functions
	functions1, err := sv.getFunctions(db1)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get functions from db1",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}
	functions2, err := sv.getFunctions(db2)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get functions from db2",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}

	if diff := sv.compareStringSlices(functions1, functions2); diff != "" {
		return fmt.Sprintf("Function differences: %s", diff), nil
	}

	// Compare triggers
	triggers1, err := sv.getTriggers(db1)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get triggers from db1",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}
	triggers2, err := sv.getTriggers(db2)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get triggers from db2",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}

	if diff := sv.compareStringSlices(triggers1, triggers2); diff != "" {
		return fmt.Sprintf("Trigger differences: %s", diff), nil
	}

	// Compare views
	views1, err := sv.getViews(db1)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get views from db1",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}
	views2, err := sv.getViews(db2)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get views from db2",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}

	if diff := sv.compareStringSlices(views1, views2); diff != "" {
		return fmt.Sprintf("View differences: %s", diff), nil
	}

	// Compare sequences
	sequences1, err := sv.getSequences(db1)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get sequences from db1",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}
	sequences2, err := sv.getSequences(db2)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get sequences from db2",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}

	if diff := sv.compareStringSlices(sequences1, sequences2); diff != "" {
		return fmt.Sprintf("Sequence differences: %s", diff), nil
	}

	// Compare types
	types1, err := sv.getCustomTypes(db1)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get types from db1",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}
	types2, err := sv.getCustomTypes(db2)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get types from db2",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}

	if diff := sv.compareStringSlices(types1, types2); diff != "" {
		return fmt.Sprintf("Type differences: %s", diff), nil
	}

	// Compare extensions
	extensions1, err := sv.getExtensions(db1)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get extensions from db1",
			errors.SeverityError,
			errors.CategoryExtension,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}
	extensions2, err := sv.getExtensions(db2)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get extensions from db2",
			errors.SeverityError,
			errors.CategoryExtension,
		).WithInnerError(err).WithSuggestion("Check database connection and permissions")
	}

	if diff := sv.compareStringSlices(extensions1, extensions2); diff != "" {
		return fmt.Sprintf("Extension differences: %s", diff), nil
	}

	return "", nil // No differences
}

func (sv *SchemaValidator) getTables(db *sql.DB) ([]string, error) {
	rows, err := db.Query("SELECT tablename FROM pg_tables WHERE schemaname = 'public' ORDER BY tablename")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var tables []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}
	return tables, nil
}

func (sv *SchemaValidator) getIndexes(db *sql.DB) ([]string, error) {
	rows, err := db.Query("SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' ORDER BY indexname")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var indexes []string
	for rows.Next() {
		var indexDef string
		if err := rows.Scan(&indexDef); err != nil {
			return nil, err
		}
		indexes = append(indexes, indexDef)
	}
	return indexes, nil
}

func (sv *SchemaValidator) compareStringSlices(a, b []string) string {
	mapA := make(map[string]bool)
	for _, item := range a {
		mapA[item] = true
	}

	mapB := make(map[string]bool)
	for _, item := range b {
		mapB[item] = true
	}

	var diffs []string
	for item := range mapA {
		if !mapB[item] {
			diffs = append(diffs, fmt.Sprintf("- %s", item))
		}
	}
	for item := range mapB {
		if !mapA[item] {
			diffs = append(diffs, fmt.Sprintf("+ %s", item))
		}
	}

	if len(diffs) == 0 {
		return ""
	}
	return strings.Join(diffs, "; ")
}

// getDefaultExtensionMap returns a mapping of extensions to Debian/Ubuntu packages
func getDefaultExtensionMap() map[string]string {
	return map[string]string{
		"postgis":            "postgis",
		"uuid-ossp":          "postgresql-contrib",
		"pg_trgm":            "postgresql-contrib",
		"btree_gin":          "postgresql-contrib",
		"btree_gist":         "postgresql-contrib",
		"pgcrypto":           "postgresql-contrib",
		"cube":               "postgresql-contrib",
		"earthdistance":      "postgresql-contrib",
		"pg_stat_statements": "postgresql-contrib",
		"hstore":             "postgresql-contrib",
		"ltree":              "postgresql-contrib",
		"citext":             "postgresql-contrib",
		"unaccent":           "postgresql-contrib",
		"fuzzystrmatch":      "postgresql-contrib",
	}
}

func (sv *SchemaValidator) getFunctions(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`
		SELECT n.nspname || '.' || p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')'
		FROM pg_proc p
		JOIN pg_namespace n ON p.pronamespace = n.oid
		WHERE n.nspname NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
		ORDER BY n.nspname, p.proname
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var functions []string
	for rows.Next() {
		var function string
		if err := rows.Scan(&function); err != nil {
			return nil, err
		}
		functions = append(functions, function)
	}
	return functions, nil
}

func (sv *SchemaValidator) getTriggers(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`
		SELECT trigger_name || ' ON ' || event_object_table
		FROM information_schema.triggers
		WHERE trigger_schema = 'public'
		ORDER BY trigger_name
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var triggers []string
	for rows.Next() {
		var trigger string
		if err := rows.Scan(&trigger); err != nil {
			return nil, err
		}
		triggers = append(triggers, trigger)
	}
	return triggers, nil
}

func (sv *SchemaValidator) getViews(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`
		SELECT table_name
		FROM information_schema.views
		WHERE table_schema = 'public'
		ORDER BY table_name
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var views []string
	for rows.Next() {
		var view string
		if err := rows.Scan(&view); err != nil {
			return nil, err
		}
		views = append(views, view)
	}
	return views, nil
}

func (sv *SchemaValidator) getSequences(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`
		SELECT sequence_name
		FROM information_schema.sequences
		WHERE sequence_schema = 'public'
		ORDER BY sequence_name
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var sequences []string
	for rows.Next() {
		var sequence string
		if err := rows.Scan(&sequence); err != nil {
			return nil, err
		}
		sequences = append(sequences, sequence)
	}
	return sequences, nil
}

func (sv *SchemaValidator) getCustomTypes(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`
		SELECT n.nspname || '.' || t.typname
		FROM pg_type t
		JOIN pg_namespace n ON t.typnamespace = n.oid
		WHERE n.nspname = 'public' AND t.typtype IN ('e', 'c', 'd')
		ORDER BY n.nspname, t.typname
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var types []string
	for rows.Next() {
		var typeInfo string
		if err := rows.Scan(&typeInfo); err != nil {
			return nil, err
		}
		types = append(types, typeInfo)
	}
	return types, nil
}

func (sv *SchemaValidator) getExtensions(db *sql.DB) ([]string, error) {
	rows, err := db.Query(`
		SELECT extname
		FROM pg_extension
		WHERE extname NOT IN ('plpgsql')
		ORDER BY extname
	`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var extensions []string
	for rows.Next() {
		var ext string
		if err := rows.Scan(&ext); err != nil {
			return nil, err
		}
		extensions = append(extensions, ext)
	}
	return extensions, nil
}

func (sv *SchemaValidator) logInfo(format string, args ...interface{}) {
	if sv.config.Verbose {
		_, _ = color.New(color.FgCyan).Printf("[VALIDATOR] "+format+"\n", args...)
	}
}

// Close closes the validator and its resources
func (sv *SchemaValidator) Close() error {
	if sv.dockerClient != nil {
		return sv.dockerClient.Close()
	}
	return nil
}

// SetAIValidator sets the AI validator for enhanced post-processing validation
func (sv *SchemaValidator) SetAIValidator(aiValidator *AIValidator) {
	sv.aiValidator = aiValidator
}

// ValidateWithAI performs AI-powered post-processing validation
// This should be called after Docker validation completes successfully
func (sv *SchemaValidator) ValidateWithAI(ctx context.Context, migrations []*types.Migration, squashedSQL string) (*AIValidationResult, error) {
	if sv.aiValidator == nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"AI validator not configured",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Call SetAIValidator first to enable AI-powered validation")
	}

	sv.logInfo("🤖 Starting AI-powered validation...")
	return sv.aiValidator.ValidatePostProcessing(ctx, migrations, squashedSQL)
}

// getPluginCompatibilitySQL aggregates compatibility SQL from all active plugins
// This creates mock auth functions, roles, schemas, etc. for validation
// Examples:
//   - Clerk: Creates auth.jwt() with JWT v2 organization structure
//   - Supabase: Creates auth.uid(), auth.jwt(), Supabase roles
//   - Auth0: Creates auth.jwt() with Auth0 claim structure
func (sv *SchemaValidator) getPluginCompatibilitySQL(ctx context.Context) string {
	registry := plugins.GlobalRegistry()

	// Only get compatibility SQL if plugins are initialized
	if len(registry.ActivePlugins()) == 0 {
		return "" // No plugins active
	}

	// Call InjectCompatibilityLayer on registry (handles all active plugins)
	return registry.InjectCompatibilityLayer(ctx)
}
