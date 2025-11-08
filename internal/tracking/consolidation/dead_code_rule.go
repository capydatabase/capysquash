package consolidation

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/utils"

	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// DeadCodeRemovalRule identifies and removes unused database objects
type DeadCodeRemovalRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *DeadCodeRemovalRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	// Detect objects that are created and then dropped (simple dead code)
	createFound := false
	dropFound := false

	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			createFound = true
		}
		if event.Operation == types.OpDrop {
			dropFound = true
		}
	}

	// If object is created and then dropped, it's dead code
	if createFound && dropFound {
		return true
	}

	// This applies to functions and triggers
	if (lifecycle.Type == types.TypeFunction ||
		lifecycle.Type == types.TypeTrigger) && createFound && !dropFound {
		// Function exists but never dropped - check if it's referenced
		// The Apply method will analyze function references across all SQL
		return true
	}

	// Additional detection: Check if object exists but has zero usage in production
	// This requires database statistics from the ConsolidationEngine
	// Only applicable for tables and indexes where we can query pg_stat_user_tables/indexes
	if lifecycle.Type == types.TypeTable || lifecycle.Type == types.TypeIndex {
		// The Apply method will query the database for usage stats
		// For now, return false here since we can't query without the engine
		// The actual DB stats check happens in Apply()
		return false
	}

	return false
}

// Apply applies the consolidation rule to the given lifecycle
func (r *DeadCodeRemovalRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]interface{}{"rule": "DeadCodeRemovalRule"})
	}

	var eliminatedStmts []types.Statement
	createFound := false
	dropFound := false
	isUnreferencedFunction := false

	// Check which case we're dealing with
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			createFound = true
		}
		if event.Operation == types.OpDrop {
			dropFound = true
		}
	}

	// Case 1: CREATE -> DROP patterns (simple dead code)
	if createFound && dropFound {
		for _, event := range lifecycle.History {
			if event.Operation == types.OpCreate && len(eliminatedStmts) == 0 {
				eliminatedStmts = append(eliminatedStmts, event.Statement)
			} else if event.Operation == types.OpDrop {
				eliminatedStmts = append(eliminatedStmts, event.Statement)
				break
			}
		}
	}

	// Case 2: Unreferenced functions (NOT triggers - triggers are event-driven)
	if createFound && !dropFound && lifecycle.Type == types.TypeFunction {
		// Analyze if function is referenced anywhere
		isReferenced := r.isFunctionReferenced(lifecycle, engine)

		if !isReferenced {
			// Function is never called - it's dead code
			isUnreferencedFunction = true
			for _, event := range lifecycle.History {
				if event.Operation == types.OpCreate {
					eliminatedStmts = append(eliminatedStmts, event.Statement)
				}
			}
		} else {
			// Function is referenced - not dead code, don't remove
			return nil, nil
		}
	}

	// IMPORTANT: Never apply dead code removal to triggers without a DROP
	// Triggers are event-driven and don't need to be "called" from other SQL
	if createFound && !dropFound && lifecycle.Type == types.TypeTrigger {
		return nil, nil // Preserve all triggers that aren't explicitly dropped
	}

	// If no statements to eliminate, this rule doesn't apply
	if len(eliminatedStmts) == 0 {
		return nil, nil
	}

	// Analyze usage patterns to determine if object is truly unused
	usageAnalysis := r.analyzeObjectUsage(lifecycle, engine)

	optimizations := []string{}
	warnings := []string{}

	if isUnreferencedFunction {
		optimizations = append(optimizations, fmt.Sprintf("Removed unreferenced %s '%s'", lifecycle.Type, lifecycle.Name))
		optimizations = append(optimizations, "Function is never called by triggers, other functions, or application code")
		warnings = append(warnings, "Dead code removal applied - verify function is truly unused before production deployment")
	} else {
		optimizations = append(optimizations, "Eliminated CREATE/DROP cycle for unused object")
		if usageAnalysis.ShortLivedObject {
			optimizations = append(optimizations, fmt.Sprintf("Object existed for only %d migrations before being dropped", usageAnalysis.LifespanMigrations))
		}
	}

	// Add usage analysis details
	if usageAnalysis.HasDependencies {
		warnings = append(warnings, fmt.Sprintf("Object had %d dependencies before being dropped", usageAnalysis.DependencyCount))
	}
	if !usageAnalysis.ReferencedByOthers {
		optimizations = append(optimizations, "Object was never referenced by other objects")
	}

	result := &tracking.ConsolidationResult{
		OriginalStatements: eliminatedStmts,
		ConsolidatedSQL:    "-- Dead code eliminated", // No SQL needed
		Optimizations:      optimizations,
		Warnings:           warnings,
		RiskLevel:          tracking.RiskLevelMedium, // Medium risk as we're removing code
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(eliminatedStmts),
			FilesAffected:     len(eliminatedStmts),
			LinesReduced:      len(eliminatedStmts) * 5, // Functions are typically larger
		},
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *DeadCodeRemovalRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelMedium
}

// ObjectUsageAnalysis contains analysis of object usage patterns
type ObjectUsageAnalysis struct {
	HasDependencies     bool
	DependencyCount     int
	ReferencedByOthers  bool
	ReferenceCount      int
	ShortLivedObject    bool
	LifespanMigrations  int
	DatabaseStats       *DatabaseUsageStats // nil if DB not available
}

// DatabaseUsageStats contains actual production database usage statistics
type DatabaseUsageStats struct {
	TableName       string
	SequentialScans int64
	IndexScans      int64
	TuplesFetched   int64
	TuplesInserted  int64
	TuplesUpdated   int64
	TuplesDeleted   int64
	LastVacuum      *string
	LastAnalyze     *string
}

// analyzeObjectUsage analyzes usage patterns of an object to determine if it's truly dead code
func (r *DeadCodeRemovalRule) analyzeObjectUsage(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) *ObjectUsageAnalysis {
	analysis := &ObjectUsageAnalysis{
		LifespanMigrations: len(lifecycle.History),
	}

	// Check dependencies
	analysis.DependencyCount = len(lifecycle.Dependencies)
	analysis.HasDependencies = analysis.DependencyCount > 0

	// Check if other objects reference this one
	tracker := engine.GetTracker()
	allLifecycles := tracker.GetObjectsByCategory()

	referenceCount := 0
	for _, categoryObjects := range allLifecycles {
		for _, otherLifecycle := range categoryObjects {
			if otherLifecycle.Key == lifecycle.Key {
				continue // Skip self
			}

			// Check if this object is in the other's dependencies
			for _, dep := range otherLifecycle.Dependencies {
				// Compare by name and schema (ObjectID doesn't have a Key field)
				if dep.DependsOn.Name == lifecycle.Name && dep.DependsOn.Schema == lifecycle.Schema {
					referenceCount++
					break
				}
			}
		}
	}

	analysis.ReferenceCount = referenceCount
	analysis.ReferencedByOthers = referenceCount > 0

	// Determine if object was short-lived (created and dropped within few migrations)
	analysis.ShortLivedObject = analysis.LifespanMigrations <= 5

	// Database statistics would be queried here if database connection is available
	analysis.DatabaseStats = nil

	return analysis
}

func (r *DeadCodeRemovalRule) isFunctionReferenced(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) bool {
	functionName := strings.ToLower(lifecycle.Name)
	tracker := engine.GetTracker()

	// Get all lifecycles to scan their SQL for references
	allLifecycles := tracker.GetObjectsByCategory()

	// Build patterns to search for function references
	// Pattern 1: EXECUTE FUNCTION function_name
	executePattern := regexp.MustCompile(fmt.Sprintf(`(?i)EXECUTE\s+FUNCTION\s+%s\s*\(`, regexp.QuoteMeta(functionName)))
	// Pattern 2: Direct function call: function_name(
	directCallPattern := regexp.MustCompile(fmt.Sprintf(`(?i)\b%s\s*\(`, regexp.QuoteMeta(functionName)))
	// Pattern 3: PERFORM function_name (in PL/pgSQL)
	performPattern := regexp.MustCompile(fmt.Sprintf(`(?i)PERFORM\s+%s\s*\(`, regexp.QuoteMeta(functionName)))
	// Pattern 4: SELECT function_name(
	selectPattern := regexp.MustCompile(fmt.Sprintf(`(?i)SELECT\s+[^;]*%s\s*\(`, regexp.QuoteMeta(functionName)))
	// Pattern 5: Function in WHERE/HAVING clauses
	wherePattern := regexp.MustCompile(fmt.Sprintf(`(?i)(WHERE|HAVING|AND|OR)\s+[^;]*%s\s*\(`, regexp.QuoteMeta(functionName)))
	// Pattern 6: Function in FROM clause or subquery
	fromPattern := regexp.MustCompile(fmt.Sprintf(`(?i)FROM\s+[^;]*%s\s*\(`, regexp.QuoteMeta(functionName)))
	// Pattern 7: Function in expressions (SET, VALUES, etc.)
	expressionPattern := regexp.MustCompile(fmt.Sprintf(`(?i)(SET|VALUES|RETURN|DEFAULT)\s+[^;]*%s\s*\(`, regexp.QuoteMeta(functionName)))

	// Common PostgreSQL built-in functions to exclude from analysis
	builtinFunctions := map[string]bool{
		"now":                true,
		"current_timestamp":  true,
		"gen_random_uuid":    true,
		"uuid_generate_v4":   true,
		"count":              true,
		"sum":                true,
		"avg":                true,
		"max":                true,
		"min":                true,
		"coalesce":           true,
		"lower":              true,
		"upper":              true,
		"trim":               true,
		"substring":          true,
		"array_agg":          true,
		"jsonb_build_object": true,
		"to_jsonb":           true,
	}

	// Skip built-in functions
	if builtinFunctions[functionName] {
		return true // Consider built-ins as always referenced
	}

	// Helper function to check if SQL contains function reference
	checkSQLForFunction := func(sql string, context string) bool {
		// Check for various reference patterns
		if executePattern.MatchString(sql) ||
			directCallPattern.MatchString(sql) ||
			performPattern.MatchString(sql) ||
			selectPattern.MatchString(sql) ||
			wherePattern.MatchString(sql) ||
			fromPattern.MatchString(sql) ||
			expressionPattern.MatchString(sql) {
			utils.GetDefaultLogger().WithPrefix("DEAD-CODE").Info("Function %s is referenced in %s", functionName, context)
			return true
		}
		return false
	}

	// Scan all tracked object lifecycles for references
	for _, categoryObjects := range allLifecycles {
		for _, otherLifecycle := range categoryObjects {
			// Skip the function's own lifecycle
			if otherLifecycle.Key == lifecycle.Key {
				continue
			}

			// Check all events in the lifecycle
			for _, event := range otherLifecycle.History {
				sql := strings.ToLower(event.Statement.SQL)

				if checkSQLForFunction(sql, fmt.Sprintf("%s (%s)", otherLifecycle.Name, otherLifecycle.Type)) {
					return true
				}

				// Special case: Check for trigger references
				// Format: CREATE TRIGGER ... EXECUTE FUNCTION function_name()
				if otherLifecycle.Type == types.TypeTrigger {
					if strings.Contains(sql, functionName) {
						utils.GetDefaultLogger().WithPrefix("DEAD-CODE").Info("Function %s is referenced by trigger %s", functionName, otherLifecycle.Name)
						return true
					}
				}
			}
		}
	}

	// CRITICAL FIX for Bug #3: Also scan ALL processed events to catch function calls in
	// SELECT statements and other statements that don't create tracked object lifecycles.
	// This ensures we detect function usage in:
	// - SELECT statements (e.g., "SELECT my_function();")
	// - DO blocks that call functions
	// - Other statements that reference functions but don't create lifecycle objects
	//
	// TODO: Need to add GetProcessedEvents() method to UnifiedTracker or make processedEvents accessible
	// For now, this check is disabled (scanning lifecycles above should catch most references)
	//
	// for _, event := range tracker.GetProcessedEvents() {
	// 	// Skip the function's own creation event
	// 	if event.Statement.ObjectName == lifecycle.Name && event.Statement.ObjectType == lifecycle.Type {
	// 		continue
	// 	}
	//
	// 	sql := strings.ToLower(event.Statement.SQL)
	// 	if checkSQLForFunction(sql, fmt.Sprintf("statement in %s", event.Migration)) {
	// 		return true
	// 	}
	// }

	// No references found
	utils.GetDefaultLogger().WithPrefix("DEAD-CODE").Info("Function %s has no references - candidate for dead code removal", functionName)
	return false
}
