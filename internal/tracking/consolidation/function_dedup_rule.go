package consolidation

import (
	"fmt"

	"github.com/capy-base/pgsquash-engine/internal/utils"

	"github.com/capy-base/pgsquash-engine/internal/tracking"
	"github.com/capy-base/pgsquash-engine/internal/types"

	"github.com/capy-base/pgsquash-engine/internal/errors"
)

// FunctionDeduplicationRule consolidates duplicate function definitions
type FunctionDeduplicationRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *FunctionDeduplicationRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	if lifecycle.Type != types.TypeFunction {
		return false
	}

	// Check for multiple CREATE operations with identical function signatures
	createCount := 0
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			createCount++
		}
	}

	return createCount > 1
}

// Apply applies the consolidation rule to the given lifecycle
func (r *FunctionDeduplicationRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]any{"rule": "FunctionDedupRule"})
	}

	var latestCreate *types.Statement
	var duplicateStmts []types.Statement

	// Find the latest CREATE statement and collect duplicates
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			if latestCreate != nil {
				duplicateStmts = append(duplicateStmts, *latestCreate)
			}
			latestCreate = &event.Statement
		}
	}

	if latestCreate == nil {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "no CREATE statement found", map[string]any{"object": lifecycle.Name})
	}

	// Use original SQL directly from latest CREATE statement
	// Multi-version functions should preserve the exact SQL from the latest definition
	// without any extraction, reconstruction, or deparser round-trips that can corrupt:
	// - LANGUAGE placement (before AS vs after body)
	// - LANGUAGE type (sql vs plpgsql)
	// - Volatility markers (STABLE, IMMUTABLE, VOLATILE)
	// - Security markers (SECURITY DEFINER)
	// - Function body quoting
	consolidatedSQL := latestCreate.SQL

	utils.GetDefaultLogger().WithPrefix("FUNCTION-DEDUP").Info("Preserving original SQL for multi-version function: %s (length=%d)",
		lifecycle.Name, len(consolidatedSQL))

	result := &tracking.ConsolidationResult{
		OriginalStatements: append(duplicateStmts, *latestCreate),
		ConsolidatedSQL:    consolidatedSQL,
		Optimizations: []string{
			fmt.Sprintf("Removed %d duplicate function definitions", len(duplicateStmts)),
		},
		RiskLevel: tracking.RiskLevelLow,
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(duplicateStmts),
			FilesAffected:     len(duplicateStmts) + 1,
			LinesReduced:      len(duplicateStmts) * 5, // Estimate based on function size
		},
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *FunctionDeduplicationRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow
}
