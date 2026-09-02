package consolidation

import (
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/transaction"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// TransactionBoundaryRule optimizes transaction boundaries for better performance
type TransactionBoundaryRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *TransactionBoundaryRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	// If object is ultimately dropped, do not optimize transactions. Let it fall back to Drop.
	if len(lifecycle.History) > 0 {
		if lifecycle.History[len(lifecycle.History)-1].Operation == types.OpDrop {
			return false
		}
	}
	// Apply to lifecycles with multiple operations that can be grouped
	return len(lifecycle.History) > 2
}

// Apply applies the consolidation rule to the given lifecycle
// Delegates to squasher.TransactionPlanner for transaction grouping (single source of truth)
func (r *TransactionBoundaryRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]any{"rule": "TransactionRule"})
	}

	// Collect all statements from lifecycle
	var statements []types.Statement
	for _, event := range lifecycle.History {
		statements = append(statements, event.Statement)
	}

	// Use TransactionPlanner for grouping (single source of truth)
	planner := transaction.NewTransactionPlanner("17") // PostgreSQL 17 default baseline
	plan := planner.PlanTransactions(statements)

	// Generate optimized SQL from the plan
	consolidatedSQL := r.generateOptimizedTransactionsFromPlan(plan)

	result := &tracking.ConsolidationResult{
		OriginalStatements: statements,
		ConsolidatedSQL:    consolidatedSQL,
		Optimizations: []string{
			fmt.Sprintf("Grouped %d operations into %d optimized transactions",
				len(statements), plan.TotalBatches),
			"Optimized transaction boundaries for better performance",
		},
		RiskLevel: tracking.RiskLevelLow, // Transaction optimization is generally safe
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: 0, // No statements removed, just reorganized
			FilesAffected:     len(statements),
			LinesReduced:      plan.TotalBatches * 2, // Transaction overhead reduction
		},
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *TransactionBoundaryRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow
}

// generateOptimizedTransactionsFromPlan generates SQL from a TransactionPlan
// Delegates to TransactionPlanner for grouping logic (single source of truth)
func (r *TransactionBoundaryRule) generateOptimizedTransactionsFromPlan(plan *transaction.TransactionPlan) string {
	var sqlParts []string

	for i, batch := range plan.Batches {
		if batch.RequiresNoTxn {
			// Single operation that requires separate transaction (concurrent ops, etc.)
			sqlParts = append(sqlParts,
				fmt.Sprintf("-- Operation %d: Requires separate transaction", i+1))
			for _, stmt := range batch.Statements {
				sql := stmt.SQL
				if !strings.HasSuffix(strings.TrimSpace(sql), ";") {
					sql += ";"
				}
				sqlParts = append(sqlParts, sql)
			}
		} else if len(batch.Statements) > 1 {
			// Multiple operations in transaction block
			sqlParts = append(sqlParts,
				fmt.Sprintf("-- Transaction %d: %d operations", i+1, len(batch.Statements)))
			sqlParts = append(sqlParts, "BEGIN;")

			for _, stmt := range batch.Statements {
				sql := stmt.SQL
				if !strings.HasSuffix(strings.TrimSpace(sql), ";") {
					sql += ";"
				}
				sqlParts = append(sqlParts, "  "+sql)
			}

			sqlParts = append(sqlParts, "COMMIT;")
		} else {
			// Single operation, no transaction needed
			sql := batch.Statements[0].SQL
			if !strings.HasSuffix(strings.TrimSpace(sql), ";") {
				sql += ";"
			}
			sqlParts = append(sqlParts, sql)
		}

		sqlParts = append(sqlParts, "")
	}

	return strings.Join(sqlParts, "\n")
}
