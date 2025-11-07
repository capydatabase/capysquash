package consolidation

import (
	"fmt"
	"strings"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/errors"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/tracking"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/types"
)

// RLSConsolidationRule consolidates Row Level Security operations
type RLSConsolidationRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *RLSConsolidationRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	if lifecycle.Type != types.TypeTable {
		return false
	}

	// Check if there are RLS operations
	hasRLS := false
	for _, event := range lifecycle.History {
		if event.Operation == types.OpAlter {
			alterSQL := strings.ToUpper(event.Statement.SQL)
			if strings.Contains(alterSQL, "ENABLE ROW LEVEL SECURITY") ||
				strings.Contains(alterSQL, "DISABLE ROW LEVEL SECURITY") ||
				strings.Contains(alterSQL, "FORCE ROW LEVEL SECURITY") {
				hasRLS = true
				break
			}
		}
	}

	return hasRLS
}

// Apply applies the consolidation rule to the given lifecycle
func (r *RLSConsolidationRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]interface{}{"rule": "RLSConsolidationRule"})
	}

	// BUG FIX #6: When consolidating RLS operations, we must also handle other ALTER operations
	// (like ADD COLUMN) that may exist in the same lifecycle. Otherwise, we lose those ALTER statements.
	//
	// Example: collection_items has:
	//   1. CREATE TABLE
	//   2. ALTER TABLE ADD COLUMN command_id
	//   3. ALTER TABLE ADD COLUMN item_type
	//   4. ALTER TABLE ADD CONSTRAINT ...
	//   5. ALTER TABLE ENABLE ROW LEVEL SECURITY
	//
	// If we only consolidate RLS (step 5), we lose steps 2-4!
	//
	// Solution: Use the same integration logic as CreateAlterConsolidationRule - integrate ALL ALTERs

	// Find CREATE TABLE statement and ALL ALTER statements (not just RLS)
	var createStmt *types.Statement
	var alterStmts []types.Statement
	var rlsAlterStmts []types.Statement // Track RLS separately for optimization message

	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			createStmt = &event.Statement
		} else if event.Operation == types.OpAlter && !event.HasDataOps {
			alterStmts = append(alterStmts, event.Statement)
			if r.isRLSOperation(event.Statement.SQL) {
				rlsAlterStmts = append(rlsAlterStmts, event.Statement)
			}
		}
	}

	// Generate consolidated SQL by integrating ALL ALTER operations into CREATE
	// This uses the same proven logic as CreateAlterConsolidationRule
	var consolidatedSQL string
	if createStmt != nil && len(alterStmts) > 0 {
		// Use integrateAlterIntoCreate to properly merge ALL ALTER operations
		consolidatedSQL = integrateAlterIntoCreate(createStmt, alterStmts)
	} else if createStmt != nil {
		// No ALTER statements, just the CREATE
		consolidatedSQL = strings.TrimSpace(createStmt.SQL)
		if !strings.HasSuffix(consolidatedSQL, ";") {
			consolidatedSQL += ";"
		}
	} else {
		// Fallback: no CREATE found (shouldn't happen for tables, but be safe)
		finalRLSState := r.determineFinalRLSState(lifecycle)
		if finalRLSState != "" {
			tableName := lifecycle.Name
			consolidatedSQL = fmt.Sprintf("ALTER TABLE %s %s;", tableName, finalRLSState)
		}
	}

	// Build optimizations list
	optimizations := []string{}
	if len(rlsAlterStmts) > 0 {
		finalRLSState := r.determineFinalRLSState(lifecycle)
		optimizations = append(optimizations, fmt.Sprintf("Consolidated %d RLS operations into final state: %s", len(rlsAlterStmts), finalRLSState))
	}
	if len(alterStmts) > len(rlsAlterStmts) {
		nonRLSCount := len(alterStmts) - len(rlsAlterStmts)
		optimizations = append(optimizations, fmt.Sprintf("Integrated %d non-RLS ALTER operations (ADD COLUMN, ADD CONSTRAINT, etc.)", nonRLSCount))
	}

	// Collect all original statements for tracking
	allStmts := []types.Statement{}
	if createStmt != nil {
		allStmts = append(allStmts, *createStmt)
	}
	allStmts = append(allStmts, alterStmts...)

	result := &tracking.ConsolidationResult{
		OriginalStatements: allStmts,
		ConsolidatedSQL:    consolidatedSQL,
		Optimizations:      optimizations,
		RiskLevel:          tracking.RiskLevelLow, // RLS is usually safe to consolidate
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(alterStmts), // All ALTERs get consolidated
			FilesAffected:     len(allStmts),
			LinesReduced:      len(alterStmts) * 2, // Estimate
		},
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *RLSConsolidationRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow
}

func (r *RLSConsolidationRule) determineFinalRLSState(lifecycle *tracking.ObjectLifecycle) string {
	// Track RLS state changes through lifecycle
	currentState := "DISABLE ROW LEVEL SECURITY" // Default state

	for _, event := range lifecycle.History {
		if event.Operation == types.OpAlter {
			alterSQL := strings.ToUpper(event.Statement.SQL)

			if strings.Contains(alterSQL, "ENABLE ROW LEVEL SECURITY") {
				currentState = "ENABLE ROW LEVEL SECURITY"
			} else if strings.Contains(alterSQL, "DISABLE ROW LEVEL SECURITY") {
				currentState = "DISABLE ROW LEVEL SECURITY"
			} else if strings.Contains(alterSQL, "FORCE ROW LEVEL SECURITY") {
				currentState = "FORCE ROW LEVEL SECURITY"
			}
		}
	}

	return currentState
}

func (r *RLSConsolidationRule) isRLSOperation(sql string) bool {
	upperSQL := strings.ToUpper(sql)
	return strings.Contains(upperSQL, "ENABLE ROW LEVEL SECURITY") ||
		strings.Contains(upperSQL, "DISABLE ROW LEVEL SECURITY") ||
		strings.Contains(upperSQL, "FORCE ROW LEVEL SECURITY")
}
