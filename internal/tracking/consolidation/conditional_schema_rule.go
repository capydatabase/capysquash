package consolidation

import (
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// ConditionalSchemaRule consolidates conditional schema operations (IF NOT EXISTS, CREATE OR REPLACE)
type ConditionalSchemaRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *ConditionalSchemaRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	// Check if there are conditional schema operations that can be consolidated
	// OR if we have multiple CREATE operations that could benefit from conditional logic
	conditionalOps := 0
	createOps := 0

	for _, event := range lifecycle.History {
		sql := strings.ToUpper(event.Statement.SQL)
		if strings.Contains(sql, "IF NOT EXISTS") ||
			strings.Contains(sql, "IF EXISTS") ||
			strings.Contains(sql, "CREATE OR REPLACE") {
			conditionalOps++
		}
		if event.Operation == types.OpCreate {
			createOps++
		}
	}

	// Apply if we have multiple conditional ops OR multiple creates that could use conditional logic
	return conditionalOps > 1 || (createOps > 1 && conditionalOps >= 1)
}

// Apply applies the consolidation rule to the given lifecycle
func (r *ConditionalSchemaRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]interface{}{"rule": "ConditionalSchemaRule"})
	}

	// Analyze conditional operations and determine final desired state
	finalState := r.analyzeFinalConditionalState(lifecycle)

	// Collect all conditional statements
	var originalStmts []types.Statement
	for _, event := range lifecycle.History {
		sql := strings.ToUpper(event.Statement.SQL)
		if strings.Contains(sql, "IF NOT EXISTS") ||
			strings.Contains(sql, "IF EXISTS") ||
			strings.Contains(sql, "CREATE OR REPLACE") {
			originalStmts = append(originalStmts, event.Statement)
		}
	}

	// Generate consolidated conditional statement
	consolidatedSQL := r.generateConditionalSQL(lifecycle, finalState)

	result := &tracking.ConsolidationResult{
		OriginalStatements: originalStmts,
		ConsolidatedSQL:    consolidatedSQL,
		Optimizations: []string{
			fmt.Sprintf("Consolidated %d conditional schema operations", len(originalStmts)),
			"Resolved IF NOT EXISTS/IF EXISTS conflicts",
		},
		RiskLevel: tracking.RiskLevelLow, // Conditional operations are inherently safe
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(originalStmts) - 1,
			FilesAffected:     len(originalStmts),
			LinesReduced:      len(originalStmts) * 2,
		},
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *ConditionalSchemaRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow
}

// ConditionalState represents the final desired state after analyzing conditional operations
type ConditionalState struct {
	ShouldExist  bool
	UseReplace   bool
	FinalSQL     string
	Dependencies []string
}

func (r *ConditionalSchemaRule) analyzeFinalConditionalState(lifecycle *tracking.ObjectLifecycle) *ConditionalState {
	state := &ConditionalState{
		ShouldExist: true, // Default assumption
		UseReplace:  false,
	}

	// Track the final intention through the lifecycle
	for _, event := range lifecycle.History {
		sql := strings.ToUpper(event.Statement.SQL)

		switch event.Operation {
		case types.OpCreate:
			state.ShouldExist = true
			state.FinalSQL = event.Statement.SQL

			// Prefer CREATE OR REPLACE over CREATE IF NOT EXISTS
			if strings.Contains(sql, "CREATE OR REPLACE") {
				state.UseReplace = true
			}

			// Capture dependencies from CREATE statements
			if len(event.Statement.Dependencies) > 0 {
				state.Dependencies = event.Statement.Dependencies
			}
		case types.OpDrop:
			state.ShouldExist = false

			// Capture dependencies from DROP statements (needed for DROP POLICY ON tablename)
			if len(event.Statement.Dependencies) > 0 {
				state.Dependencies = event.Statement.Dependencies
			}
		case types.OpAlter:
			// ALTER operations indicate the object should exist
			state.ShouldExist = true
		}
	}

	return state
}

func (r *ConditionalSchemaRule) generateConditionalSQL(lifecycle *tracking.ObjectLifecycle, state *ConditionalState) string {
	if !state.ShouldExist {
		// If final state is that object shouldn't exist, use DROP IF EXISTS
		// CRITICAL FIX: Check for UNKNOWN type to prevent invalid SQL generation
		if lifecycle.Type == types.TypeUnknown || lifecycle.Type == "" {
			// Cannot generate valid DROP statement for unknown object types
			// Log warning and skip DROP statement
			// This prevents "DROP UNKNOWN IF EXISTS" which is invalid PostgreSQL syntax
			return fmt.Sprintf("-- WARNING: Cannot generate DROP statement for object '%s' with unknown type\n-- Object lifecycle final state: should not exist\n-- Consider manual cleanup if this object exists in your database",
				lifecycle.Name)
		}

		// DROP POLICY requires ON tablename clause
		if lifecycle.Type == types.TypePolicy {
			// Extract table name from dependencies (parser extracts it from DROP POLICY ... ON tablename)
			tableName := ""
			if len(state.Dependencies) > 0 {
				tableName = state.Dependencies[0]
			}
			if tableName != "" {
				return fmt.Sprintf("DROP %s IF EXISTS %s ON %s;",
					strings.ToUpper(string(lifecycle.Type)), lifecycle.Name, tableName)
			}
		}

		// BUG #2 FIX: DROP TRIGGER also requires ON tablename clause
		// Syntax: DROP TRIGGER [IF EXISTS] trigger_name ON table_name
		if lifecycle.Type == types.TypeTrigger {
			// Extract table name from dependencies (parser now extracts it from DROP TRIGGER ... ON tablename)
			tableName := ""
			if len(state.Dependencies) > 0 {
				tableName = state.Dependencies[0]
			}
			if tableName != "" {
				return fmt.Sprintf("DROP %s IF EXISTS %s ON %s;",
					strings.ToUpper(string(lifecycle.Type)), lifecycle.Name, tableName)
			}
		}

		return fmt.Sprintf("DROP %s IF EXISTS %s;",
			strings.ToUpper(string(lifecycle.Type)), lifecycle.Name)
	}

	// Generate appropriate CREATE statement
	if state.UseReplace && (lifecycle.Type == types.TypeFunction || lifecycle.Type == types.TypeView) {
		// Use CREATE OR REPLACE for functions and views
		baseSQL := state.FinalSQL
		if !strings.Contains(strings.ToUpper(baseSQL), "CREATE OR REPLACE") {
			baseSQL = strings.Replace(baseSQL, "CREATE ", "CREATE OR REPLACE ", 1)
		}
		return baseSQL
	} else {
		// Use CREATE IF NOT EXISTS for tables, indexes, etc.
		baseSQL := state.FinalSQL
		if !strings.Contains(strings.ToUpper(baseSQL), "IF NOT EXISTS") {
			// Insert IF NOT EXISTS after CREATE OBJECT_TYPE
			objectTypeUpper := strings.ToUpper(string(lifecycle.Type))
			createPattern := "CREATE " + objectTypeUpper
			replacePattern := "CREATE " + objectTypeUpper + " IF NOT EXISTS"
			baseSQL = strings.Replace(baseSQL, createPattern, replacePattern, 1)
		}
		return baseSQL
	}
}
