// Package transformation provides SQL transformation and rollback management.
// It handles generation of rollback scripts, backup creation, and safe
// transformation operations for PostgreSQL migration squashing.
package transformation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// RollbackPlan represents a complete rollback strategy
type RollbackPlan struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	CreatedAt   time.Time         `json:"created_at"`
	Scripts     []*RollbackScript `json:"scripts"`
	Metadata    *RollbackMetadata `json:"metadata"`
	Status      RollbackStatus    `json:"status"`
}

// RollbackMetadata contains additional information about the rollback
type RollbackMetadata struct {
	SourceMigration   string            `json:"source_migration"`
	DatabaseVersion   string            `json:"database_version"`
	SchemaChecksum    string            `json:"schema_checksum"`
	EstimatedDuration time.Duration     `json:"estimated_duration"`
	Dependencies      []string          `json:"dependencies"`
	Warnings          []string          `json:"warnings"`
	Tags              map[string]string `json:"tags"`
}

// RollbackStatus tracks the execution status of rollback plans
type RollbackStatus int

const (
	RollbackPending RollbackStatus = iota
	RollbackInProgress
	RollbackCompleted
	RollbackFailed
	RollbackCancelled
)

// RollbackExecution tracks the execution of a rollback plan
type RollbackExecution struct {
	PlanID        string                 `json:"plan_id"`
	StartedAt     time.Time              `json:"started_at"`
	CompletedAt   *time.Time             `json:"completed_at,omitempty"`
	Status        RollbackStatus         `json:"status"`
	Progress      int                    `json:"progress"` // Percentage
	CurrentScript int                    `json:"current_script"`
	Results       []*ScriptResult        `json:"results"`
	Error         string                 `json:"error,omitempty"`
	Context       map[string]interface{} `json:"context"`
}

// ScriptResult tracks the result of executing a single rollback script
type ScriptResult struct {
	ScriptID     string        `json:"script_id"`
	StartedAt    time.Time     `json:"started_at"`
	CompletedAt  *time.Time    `json:"completed_at,omitempty"`
	Success      bool          `json:"success"`
	Error        string        `json:"error,omitempty"`
	Duration     time.Duration `json:"duration"`
	RowsAffected int64         `json:"rows_affected"`
	Output       string        `json:"output,omitempty"`
}

// RollbackManager handles rollback plan creation and execution
type RollbackManager struct {
	db          *sql.DB
	workDir     string
	generator   *BackupGenerator
	transformer *SQLTransformer
	plans       map[string]*RollbackPlan
	executions  map[string]*RollbackExecution
}

// NewRollbackManager creates a new rollback manager
func NewRollbackManager(db *sql.DB, workDir string) *RollbackManager {
	return &RollbackManager{
		db:          db,
		workDir:     workDir,
		generator:   NewBackupGenerator(DefaultBackupConfig(), db),
		transformer: NewSQLTransformer(DefaultTransformationConfig()),
		plans:       make(map[string]*RollbackPlan),
		executions:  make(map[string]*RollbackExecution),
	}
}

// CreateRollbackPlan generates a comprehensive rollback plan for migrations
func (rm *RollbackManager) CreateRollbackPlan(ctx context.Context, name string, statements []types.Statement) (*RollbackPlan, error) {
	planID := fmt.Sprintf("rollback_%d_%s", time.Now().Unix(), strings.ReplaceAll(name, " ", "_"))

	plan := &RollbackPlan{
		ID:          planID,
		Name:        name,
		Description: fmt.Sprintf("Rollback plan for %s", name),
		CreatedAt:   time.Now(),
		Status:      RollbackPending,
		Metadata: &RollbackMetadata{
			Dependencies: make([]string, 0),
			Warnings:     make([]string, 0),
			Tags:         make(map[string]string),
		},
	}

	// Generate rollback scripts
	scripts, err := rm.generator.GenerateRollbackScript(ctx, statements)
	if err != nil {
		return nil, errors.NewTransformationError(errors.ErrorCodeRollbackGenerationFailed, "failed to generate rollback scripts").WithInnerError(err)
	}

	plan.Scripts = scripts

	// Analyze dependencies and risks
	err = rm.analyzeRollbackPlan(ctx, plan)
	if err != nil {
		return nil, errors.NewTransformationError(errors.ErrorCodeRollbackAnalysisFailed, "failed to analyze rollback plan").WithInnerError(err)
	}

	// Store the plan
	rm.plans[planID] = plan

	// Persist to file
	err = rm.savePlanToFile(plan)
	if err != nil {
		return nil, errors.NewTransformationError(errors.ErrorCodeRollbackSaveFailed, "failed to save rollback plan").WithInnerError(err)
	}

	return plan, nil
}

// analyzeRollbackPlan analyzes a rollback plan for dependencies and risks
func (rm *RollbackManager) analyzeRollbackPlan(ctx context.Context, plan *RollbackPlan) error {
	// Estimate execution duration
	plan.Metadata.EstimatedDuration = rm.estimateRollbackDuration(plan.Scripts)

	// Check for potential issues
	for _, script := range plan.Scripts {
		if strings.Contains(strings.ToUpper(script.SQL), "DROP ") {
			plan.Metadata.Warnings = append(plan.Metadata.Warnings,
				fmt.Sprintf("Script %s contains destructive DROP operation", script.ID))
		}

		if strings.Contains(strings.ToUpper(script.SQL), "DELETE ") {
			plan.Metadata.Warnings = append(plan.Metadata.Warnings,
				fmt.Sprintf("Script %s contains data deletion", script.ID))
		}

		if strings.Contains(script.SQL, "-- Cannot automatically rollback") {
			plan.Metadata.Warnings = append(plan.Metadata.Warnings,
				fmt.Sprintf("Script %s requires manual intervention", script.ID))
		}
	}

	// Check current database state
	if rm.db != nil {
		checksum, err := rm.generateSchemaChecksum(ctx)
		if err == nil {
			plan.Metadata.SchemaChecksum = checksum
		}

		version, err := rm.getDatabaseVersion(ctx)
		if err == nil {
			plan.Metadata.DatabaseVersion = version
		}
	}

	return nil
}

// estimateRollbackDuration estimates how long a rollback will take
func (rm *RollbackManager) estimateRollbackDuration(scripts []*RollbackScript) time.Duration {
	// Simple estimation based on script complexity
	totalDuration := time.Duration(0)

	for _, script := range scripts {
		var scriptDuration time.Duration

		// Base time per script
		scriptDuration = 1 * time.Second

		// Add time based on operation type
		sql := strings.ToUpper(script.SQL)
		if strings.Contains(sql, "DROP TABLE") {
			scriptDuration += 2 * time.Second
		}
		if strings.Contains(sql, "ALTER TABLE") {
			scriptDuration += 5 * time.Second
		}
		if strings.Contains(sql, "DELETE") {
			scriptDuration += 10 * time.Second // Data operations take longer
		}

		totalDuration += scriptDuration
	}

	return totalDuration
}

// generateSchemaChecksum creates a checksum of the current database schema
func (rm *RollbackManager) generateSchemaChecksum(ctx context.Context) (string, error) {
	query := `
		SELECT md5(string_agg(
			schemaname || '.' || tablename || ':' ||
			coalesce(string_agg(column_name || ':' || data_type, ',' ORDER BY ordinal_position), ''),
			'|' ORDER BY schemaname, tablename
		))
		FROM pg_tables pt
		LEFT JOIN information_schema.columns isc ON
			pt.schemaname = isc.table_schema AND pt.tablename = isc.table_name
		WHERE pt.schemaname NOT IN ('information_schema', 'pg_catalog', 'pg_toast')
		GROUP BY pt.schemaname, pt.tablename
	`

	var checksum string
	err := rm.db.QueryRowContext(ctx, query).Scan(&checksum)
	if err != nil {
		return "", err
	}

	return checksum, nil
}

// getDatabaseVersion gets the PostgreSQL version
func (rm *RollbackManager) getDatabaseVersion(ctx context.Context) (string, error) {
	var version string
	err := rm.db.QueryRowContext(ctx, "SELECT version()").Scan(&version)
	return version, err
}

// ExecuteRollbackPlan executes a rollback plan
func (rm *RollbackManager) ExecuteRollbackPlan(ctx context.Context, planID string, dryRun bool) (*RollbackExecution, error) {
	plan, exists := rm.plans[planID]
	if !exists {
		return nil, errors.NewError(
			errors.ErrorCodeTransformationFailed,
			fmt.Sprintf("rollback plan %s not found", planID),
			errors.SeverityError,
			errors.CategoryTransformation,
		)
	}

	executionID := fmt.Sprintf("exec_%s_%d", planID, time.Now().Unix())
	execution := &RollbackExecution{
		PlanID:    planID,
		StartedAt: time.Now(),
		Status:    RollbackInProgress,
		Progress:  0,
		Results:   make([]*ScriptResult, 0, len(plan.Scripts)),
		Context:   map[string]interface{}{"dry_run": dryRun},
	}

	rm.executions[executionID] = execution

	// Execute scripts in order
	for i, script := range plan.Scripts {
		execution.CurrentScript = i
		execution.Progress = (i * 100) / len(plan.Scripts)

		result, err := rm.executeScript(ctx, script, dryRun)
		execution.Results = append(execution.Results, result)

		if err != nil && !dryRun {
			execution.Status = RollbackFailed
			execution.Error = err.Error()
			now := time.Now()
			execution.CompletedAt = &now
			return execution, err
		}

		// Check for cancellation
		select {
		case <-ctx.Done():
			execution.Status = RollbackCancelled
			execution.Error = "Rollback cancelled"
			now := time.Now()
			execution.CompletedAt = &now
			return execution, ctx.Err()
		default:
		}
	}

	// Mark as completed
	execution.Status = RollbackCompleted
	execution.Progress = 100
	now := time.Now()
	execution.CompletedAt = &now

	return execution, nil
}

// executeScript executes a single rollback script
func (rm *RollbackManager) executeScript(ctx context.Context, script *RollbackScript, dryRun bool) (*ScriptResult, error) {
	result := &ScriptResult{
		ScriptID:  script.ID,
		StartedAt: time.Now(),
		Success:   false,
	}

	if dryRun {
		// For dry run, just validate the SQL
		_, err := rm.transformer.Transform(ctx, script.SQL)
		if err != nil {
			result.Error = fmt.Sprintf("SQL validation failed: %v", err)
		} else {
			result.Success = true
			result.Output = "Dry run - SQL validated successfully"
		}
	} else {
		// Execute the actual SQL
		if rm.db != nil {
			startTime := time.Now()
			sqlResult, err := rm.db.ExecContext(ctx, script.SQL)
			result.Duration = time.Since(startTime)

			if err != nil {
				result.Error = err.Error()
			} else {
				result.Success = true
				if sqlResult != nil {
					rowsAffected, _ := sqlResult.RowsAffected()
					result.RowsAffected = rowsAffected
				}
			}
		} else {
			result.Error = "No database connection available"
		}
	}

	now := time.Now()
	result.CompletedAt = &now

	return result, nil
}

// ListRollbackPlans returns all available rollback plans
func (rm *RollbackManager) ListRollbackPlans() []*RollbackPlan {
	plans := make([]*RollbackPlan, 0, len(rm.plans))
	for _, plan := range rm.plans {
		plans = append(plans, plan)
	}

	// Sort by creation time (newest first)
	sort.Slice(plans, func(i, j int) bool {
		return plans[i].CreatedAt.After(plans[j].CreatedAt)
	})

	return plans
}

// GetRollbackPlan retrieves a specific rollback plan
func (rm *RollbackManager) GetRollbackPlan(planID string) (*RollbackPlan, error) {
	plan, exists := rm.plans[planID]
	if !exists {
		return nil, errors.NewError(
			errors.ErrorCodeTransformationFailed,
			fmt.Sprintf("rollback plan %s not found", planID),
			errors.SeverityError,
			errors.CategoryTransformation,
		)
	}
	return plan, nil
}

// GetRollbackExecution retrieves a specific rollback execution
func (rm *RollbackManager) GetRollbackExecution(executionID string) (*RollbackExecution, error) {
	execution, exists := rm.executions[executionID]
	if !exists {
		return nil, errors.NewTransformationError(errors.ErrorCodeExecutionNotFound, "rollback execution not found").WithAdditional("execution_id", executionID)
	}
	return execution, nil
}

// savePlanToFile persists a rollback plan to disk
func (rm *RollbackManager) savePlanToFile(plan *RollbackPlan) error {
	planDir := filepath.Join(rm.workDir, "rollback_plans")
	if err := os.MkdirAll(planDir, 0755); err != nil {
		return errors.NewError(
			errors.ErrorCodeTransformationFailed,
			fmt.Sprintf("create rollback plans directory '%s'", planDir),
			errors.SeverityError,
			errors.CategoryTransformation,
		).WithInnerError(err)
	}

	// Verify directory was created
	if _, err := os.Stat(planDir); os.IsNotExist(err) {
		return errors.NewError(
			errors.ErrorCodeTransformationFailed,
			fmt.Sprintf("rollback plans directory '%s' does not exist after creation attempt", planDir),
			errors.SeverityError,
			errors.CategoryTransformation,
		)
	}

	filename := fmt.Sprintf("%s.json", plan.ID)
	planFilePath := filepath.Join(planDir, filename)

	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeTransformationFailed,
			"marshal rollback plan to JSON",
			errors.SeverityError,
			errors.CategoryTransformation,
		).WithInnerError(err)
	}

	if err := os.WriteFile(planFilePath, data, 0644); err != nil {
		return errors.NewError(
			errors.ErrorCodeTransformationFailed,
			fmt.Sprintf("write rollback plan file '%s'", planFilePath),
			errors.SeverityError,
			errors.CategoryTransformation,
		).WithInnerError(err)
	}

	// Verify file was written successfully
	if info, err := os.Stat(planFilePath); err != nil {
		return errors.NewError(
			errors.ErrorCodeTransformationFailed,
			fmt.Sprintf("rollback plan file '%s' was not created", planFilePath),
			errors.SeverityError,
			errors.CategoryTransformation,
		).WithInnerError(err)
	} else if info.Size() == 0 {
		return errors.NewError(
			errors.ErrorCodeTransformationFailed,
			fmt.Sprintf("rollback plan file '%s' is empty (0 bytes)", planFilePath),
			errors.SeverityError,
			errors.CategoryTransformation,
		)
	}

	return nil
}

// loadPlanFromFile loads a rollback plan from disk
func (rm *RollbackManager) loadPlanFromFile(planID string) (*RollbackPlan, error) {
	planDir := filepath.Join(rm.workDir, "rollback_plans")
	filename := fmt.Sprintf("%s.json", planID)
	filepath := filepath.Join(planDir, filename)

	data, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	var plan RollbackPlan
	err = json.Unmarshal(data, &plan)
	if err != nil {
		return nil, err
	}

	return &plan, nil
}

// LoadAllPlans loads all rollback plans from disk
func (rm *RollbackManager) LoadAllPlans() error {
	planDir := filepath.Join(rm.workDir, "rollback_plans")

	files, err := filepath.Glob(filepath.Join(planDir, "*.json"))
	if err != nil {
		return err
	}

	for _, file := range files {
		// Extract plan ID from filename
		basename := filepath.Base(file)
		planID := strings.TrimSuffix(basename, ".json")

		plan, err := rm.loadPlanFromFile(planID)
		if err != nil {
			continue // Skip invalid files
		}

		rm.plans[planID] = plan
	}

	return nil
}

// DeleteRollbackPlan removes a rollback plan
func (rm *RollbackManager) DeleteRollbackPlan(planID string) error {
	// Remove from memory
	delete(rm.plans, planID)

	// Remove from disk
	planDir := filepath.Join(rm.workDir, "rollback_plans")
	filename := fmt.Sprintf("%s.json", planID)
	filepath := filepath.Join(planDir, filename)

	return os.Remove(filepath)
}

// ValidateRollbackPlan validates that a rollback plan is executable
func (rm *RollbackManager) ValidateRollbackPlan(ctx context.Context, planID string) error {
	plan, exists := rm.plans[planID]
	if !exists {
		return errors.NewTransformationError(errors.ErrorCodeRollbackNotFound, "rollback plan not found").WithAdditional("plan_id", planID)
	}

	// Check database connectivity
	if rm.db != nil {
		err := rm.db.PingContext(ctx)
		if err != nil {
			return errors.NewTransformationError(errors.ErrorCodeDatabaseNotAccessible, "database not accessible").WithInnerError(err)
		}
	}

	// Validate each script
	for _, script := range plan.Scripts {
		if strings.Contains(script.SQL, "-- Cannot automatically rollback") {
			return errors.NewTransformationError(errors.ErrorCodeManualInterventionRequired, "script requires manual intervention").WithAdditional("script_id", script.ID)
		}

		// Use transformer to validate SQL syntax
		_, err := rm.transformer.Transform(ctx, script.SQL)
		if err != nil {
			return errors.NewTransformationError(errors.ErrorCodeInvalidSQL, "script has invalid SQL").WithAdditional("script_id", script.ID).WithInnerError(err)
		}
	}

	return nil
}
