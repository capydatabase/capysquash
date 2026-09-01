// Package transaction provides transaction planning and lock analysis for migration squashing.
package transaction

import (
	"fmt"
	"sort"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/parser"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// TransactionBatch represents a group of statements that can run in a single transaction
type TransactionBatch struct {
	Statements       []types.Statement
	RequiresNoTxn    bool
	MaxLockLevel     types.LockLevel
	EstimatedTime    types.ExecutionTimeCategory
	BatchNumber      int
	CanRunInParallel bool
}

// TransactionPlan represents the complete execution plan for migrations
type TransactionPlan struct {
	Batches         []TransactionBatch
	TotalStatements int
	TotalBatches    int
	ConcurrentOps   int
	LockConflicts   []LockConflict
	Warnings        []string
}

// LockConflict represents a potential lock conflict between statements
type LockConflict struct {
	Statement1     types.Statement
	Statement2     types.Statement
	ConflictType   string
	Severity       string
	Recommendation string
}

// TransactionPlanner plans transaction boundaries and analyzes lock conflicts
type TransactionPlanner struct {
	pgVersion string
}

// NewTransactionPlanner creates a new transaction planner
func NewTransactionPlanner(pgVersion string) *TransactionPlanner {
	return &TransactionPlanner{
		pgVersion: pgVersion,
	}
}

// PlanTransactions creates an execution plan for a set of statements
func (tp *TransactionPlanner) PlanTransactions(statements []types.Statement) *TransactionPlan {
	plan := &TransactionPlan{
		Batches:         make([]TransactionBatch, 0),
		TotalStatements: len(statements),
		LockConflicts:   make([]LockConflict, 0),
		Warnings:        make([]string, 0),
	}

	currentBatch := TransactionBatch{
		Statements:    make([]types.Statement, 0),
		RequiresNoTxn: false,
		MaxLockLevel:  types.LockNone,
		BatchNumber:   1,
	}

	for _, stmt := range statements {
		// Check if statement requires separate transaction
		if stmt.Metadata.RequiresNoTransaction {
			// Flush current batch if it has statements
			if len(currentBatch.Statements) > 0 {
				plan.Batches = append(plan.Batches, currentBatch)
				currentBatch = TransactionBatch{
					Statements:  make([]types.Statement, 0),
					BatchNumber: len(plan.Batches) + 1,
				}
			}

			// Add as standalone batch
			plan.Batches = append(plan.Batches, TransactionBatch{
				Statements:    []types.Statement{stmt},
				RequiresNoTxn: true,
				MaxLockLevel:  stmt.Metadata.LockLevel,
				EstimatedTime: stmt.Metadata.ExecutionTime,
				BatchNumber:   len(plan.Batches) + 1,
			})
			plan.ConcurrentOps++

			// Start new batch
			currentBatch = TransactionBatch{
				Statements:  make([]types.Statement, 0),
				BatchNumber: len(plan.Batches) + 1,
			}
			continue
		}

		// Add to current batch
		currentBatch.Statements = append(currentBatch.Statements, stmt)

		// Update batch metadata
		if stmt.Metadata.LockLevel > currentBatch.MaxLockLevel {
			currentBatch.MaxLockLevel = stmt.Metadata.LockLevel
		}
		if stmt.Metadata.ExecutionTime > currentBatch.EstimatedTime {
			currentBatch.EstimatedTime = stmt.Metadata.ExecutionTime
		}
	}

	// Add final batch if it has statements
	if len(currentBatch.Statements) > 0 {
		plan.Batches = append(plan.Batches, currentBatch)
	}

	plan.TotalBatches = len(plan.Batches)

	// Analyze lock conflicts
	tp.analyzeLockConflicts(plan)

	// Generate warnings
	tp.generateWarnings(plan)

	return plan
}

// analyzeLockConflicts identifies potential lock conflicts
func (tp *TransactionPlanner) analyzeLockConflicts(plan *TransactionPlan) {
	// Check for statements that might conflict
	for i, batch := range plan.Batches {
		for j, stmt1 := range batch.Statements {
			for k := j + 1; k < len(batch.Statements); k++ {
				stmt2 := batch.Statements[k]

				// Check if both statements operate on the same object
				if stmt1.ObjectName == stmt2.ObjectName && stmt1.ObjectType == stmt2.ObjectType {
					conflict := tp.detectConflict(stmt1, stmt2)
					if conflict != nil {
						conflict.Statement1 = stmt1
						conflict.Statement2 = stmt2
						plan.LockConflicts = append(plan.LockConflicts, *conflict)
					}
				}
			}
		}

		// Check for conflicts across batches (less critical)
		if i < len(plan.Batches)-1 {
			nextBatch := plan.Batches[i+1]
			for _, stmt1 := range batch.Statements {
				for _, stmt2 := range nextBatch.Statements {
					if stmt1.ObjectName == stmt2.ObjectName && stmt1.ObjectType == stmt2.ObjectType {
						conflict := tp.detectConflict(stmt1, stmt2)
						if conflict != nil {
							conflict.Severity = "LOW" // Cross-batch conflicts are less critical
							conflict.Statement1 = stmt1
							conflict.Statement2 = stmt2
							plan.LockConflicts = append(plan.LockConflicts, *conflict)
						}
					}
				}
			}
		}
	}
}

// detectConflict checks if two statements conflict
func (tp *TransactionPlanner) detectConflict(stmt1, stmt2 types.Statement) *LockConflict {
	// Both take ACCESS EXCLUSIVE locks
	if stmt1.Metadata.LockLevel == types.LockAccessExclusive &&
		stmt2.Metadata.LockLevel == types.LockAccessExclusive {
		return &LockConflict{
			ConflictType:   "EXCLUSIVE_LOCK_CONFLICT",
			Severity:       "HIGH",
			Recommendation: "Consider separating these operations or using IF NOT EXISTS",
		}
	}

	// One is concurrent, one is not
	if stmt1.Metadata.Concurrent != stmt2.Metadata.Concurrent {
		return &LockConflict{
			ConflictType:   "CONCURRENT_MISMATCH",
			Severity:       "MEDIUM",
			Recommendation: "Concurrent and non-concurrent operations on same object may cause issues",
		}
	}

	return nil
}

// generateWarnings generates warnings based on the plan
func (tp *TransactionPlanner) generateWarnings(plan *TransactionPlan) {
	for _, batch := range plan.Batches {
		// Warn about large batches
		if len(batch.Statements) > 50 {
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("Batch %d has %d statements - consider splitting for better error handling",
					batch.BatchNumber, len(batch.Statements)))
		}

		// Warn about slow operations
		if batch.EstimatedTime == types.ExecutionSlow {
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("Batch %d contains slow operations - may take significant time",
					batch.BatchNumber))
		}

		// Warn about ACCESS EXCLUSIVE locks
		if batch.MaxLockLevel == types.LockAccessExclusive {
			plan.Warnings = append(plan.Warnings,
				fmt.Sprintf("Batch %d requires ACCESS EXCLUSIVE lock - will block all table access",
					batch.BatchNumber))
		}

		// pgvet-inspired: multiple ALTER TABLE targets in one transaction can increase deadlock risk.
		if !batch.RequiresNoTxn {
			targets := uniqueAlterTableTargets(batch.Statements)
			if len(targets) > 1 {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("Batch %d alters %d different tables in one transaction (%s) - consider splitting to reduce deadlock risk",
						batch.BatchNumber,
						len(targets),
						strings.Join(targets, ", ")))
			}
		}

		// pgvet-inspired: ADD CONSTRAINT without NOT VALID can hold heavier locks during validation.
		for _, stmt := range batch.Statements {
			if isAddConstraintWithoutNotValid(stmt) {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("Batch %d adds constraint on %s without NOT VALID - consider ADD CONSTRAINT ... NOT VALID then VALIDATE CONSTRAINT in a later transaction",
						batch.BatchNumber, stmt.ObjectName))
			}

			if isNonConcurrentIndexOperation(stmt) {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("Batch %d performs %s on %s without CONCURRENTLY - this can block writes; consider CONCURRENTLY (outside a transaction)",
						batch.BatchNumber, stmt.Operation, stmt.ObjectName))
			}
		}
	}

	// Warn about many concurrent operations
	if plan.ConcurrentOps > 5 {
		plan.Warnings = append(plan.Warnings,
			fmt.Sprintf("%d concurrent operations detected - ensure sufficient system resources",
				plan.ConcurrentOps))
	}
}

func uniqueAlterTableTargets(statements []types.Statement) []string {
	seen := make(map[string]struct{})
	for _, stmt := range statements {
		if stmt.ObjectType != types.TypeTable || stmt.Operation != types.OpAlter {
			continue
		}
		if stmt.ObjectName == "" {
			continue
		}
		seen[stmt.ObjectName] = struct{}{}
	}

	targets := make([]string, 0, len(seen))
	for t := range seen {
		targets = append(targets, t)
	}
	sort.Strings(targets)

	return targets
}

func isAddConstraintWithoutNotValid(stmt types.Statement) bool {
	if stmt.ObjectType != types.TypeTable || stmt.Operation != types.OpAlter {
		return false
	}

	sql := strings.ToUpper(stmt.SQL)
	if !strings.Contains(sql, "ALTER TABLE") {
		return false
	}
	if !strings.Contains(sql, "ADD CONSTRAINT") {
		return false
	}
	if strings.Contains(sql, "NOT VALID") {
		return false
	}

	return true
}

func isNonConcurrentIndexOperation(stmt types.Statement) bool {
	if stmt.ObjectType != types.TypeIndex {
		return false
	}

	if stmt.Operation != types.OpCreate && stmt.Operation != types.OpDrop {
		return false
	}

	if stmt.Metadata.Concurrent {
		return false
	}

	sql := strings.ToUpper(stmt.SQL)
	if stmt.Operation == types.OpCreate && strings.Contains(sql, "CREATE INDEX") {
		return true
	}
	if stmt.Operation == types.OpDrop && strings.Contains(sql, "DROP INDEX") {
		return true
	}

	return false
}

// FormatPlan formats the transaction plan for display
func (tp *TransactionPlanner) FormatPlan(plan *TransactionPlan) string {
	var output strings.Builder

	output.WriteString("=== Transaction Execution Plan ===\n\n")
	output.WriteString(fmt.Sprintf("Total Statements: %d\n", plan.TotalStatements))
	output.WriteString(fmt.Sprintf("Total Batches:    %d\n", plan.TotalBatches))
	output.WriteString(fmt.Sprintf("Concurrent Ops:   %d\n\n", plan.ConcurrentOps))

	for _, batch := range plan.Batches {
		output.WriteString(fmt.Sprintf("--- Batch %d ---\n", batch.BatchNumber))
		if batch.RequiresNoTxn {
			output.WriteString("  Mode: NO TRANSACTION (concurrent operation)\n")
		} else {
			output.WriteString("  Mode: TRANSACTION\n")
		}
		output.WriteString(fmt.Sprintf("  Statements: %d\n", len(batch.Statements)))
		output.WriteString(fmt.Sprintf("  Max Lock:   %s\n", parser.FormatLockLevel(batch.MaxLockLevel)))
		output.WriteString(fmt.Sprintf("  Est. Time:  %s\n", batch.EstimatedTime))

		// Show first few statements
		for i, stmt := range batch.Statements {
			if i >= 3 {
				output.WriteString(fmt.Sprintf("  ... and %d more statements\n", len(batch.Statements)-3))
				break
			}
			sqlPreview := stmt.SQL
			if len(sqlPreview) > 60 {
				sqlPreview = sqlPreview[:60] + "..."
			}
			output.WriteString(fmt.Sprintf("  %d. %s\n", i+1, sqlPreview))
		}
		output.WriteString("\n")
	}

	if len(plan.LockConflicts) > 0 {
		output.WriteString("=== Lock Conflicts ===\n\n")
		for i, conflict := range plan.LockConflicts {
			output.WriteString(fmt.Sprintf("%d. %s [%s]\n", i+1, conflict.ConflictType, conflict.Severity))
			output.WriteString(fmt.Sprintf("   Object: %s.%s\n", conflict.Statement1.ObjectType, conflict.Statement1.ObjectName))
			output.WriteString(fmt.Sprintf("   Recommendation: %s\n\n", conflict.Recommendation))
		}
	}

	if len(plan.Warnings) > 0 {
		output.WriteString("=== Warnings ===\n\n")
		for i, warning := range plan.Warnings {
			output.WriteString(fmt.Sprintf("%d. %s\n", i+1, warning))
		}
		output.WriteString("\n")
	}

	return output.String()
}

// FormatLockAnalysis formats detailed lock analysis for all statements
func (tp *TransactionPlanner) FormatLockAnalysis(statements []types.Statement) string {
	var output strings.Builder

	output.WriteString("=== Lock Level Analysis ===\n\n")

	// Group by lock level
	byLockLevel := make(map[types.LockLevel][]types.Statement)
	for _, stmt := range statements {
		level := stmt.Metadata.LockLevel
		byLockLevel[level] = append(byLockLevel[level], stmt)
	}

	// Sort lock levels by severity
	lockLevels := []types.LockLevel{
		types.LockAccessExclusive,
		types.LockExclusive,
		types.LockShareRowExclusive,
		types.LockShare,
		types.LockShareUpdateExclusive,
		types.LockRowExclusive,
		types.LockRowShare,
		types.LockAccessShare,
		types.LockNone,
	}

	for _, level := range lockLevels {
		stmts, ok := byLockLevel[level]
		if !ok || len(stmts) == 0 {
			continue
		}

		output.WriteString(fmt.Sprintf("--- %s (%d statements) ---\n", parser.FormatLockLevel(level), len(stmts)))
		for i, stmt := range stmts {
			if i >= 5 {
				output.WriteString(fmt.Sprintf("  ... and %d more\n", len(stmts)-5))
				break
			}
			sqlPreview := stmt.SQL
			if len(sqlPreview) > 70 {
				sqlPreview = sqlPreview[:70] + "..."
			}

			flags := []string{}
			if stmt.Metadata.Concurrent {
				flags = append(flags, "CONCURRENT")
			}
			if stmt.Metadata.RequiresNoTransaction {
				flags = append(flags, "NO_TXN")
			}
			if stmt.Metadata.PreserveVerbatim {
				flags = append(flags, "PRESERVE")
			}

			flagStr := ""
			if len(flags) > 0 {
				flagStr = fmt.Sprintf(" [%s]", strings.Join(flags, ", "))
			}

			output.WriteString(fmt.Sprintf("  • %s%s\n", sqlPreview, flagStr))
		}
		output.WriteString("\n")
	}

	// Summary statistics
	output.WriteString("=== Summary ===\n\n")
	output.WriteString(fmt.Sprintf("Total Statements:        %d\n", len(statements)))
	output.WriteString(fmt.Sprintf("Concurrent Operations:   %d\n", countConcurrent(statements)))
	output.WriteString(fmt.Sprintf("No-Transaction Required: %d\n", countNoTxn(statements)))
	output.WriteString(fmt.Sprintf("Preserved Verbatim:      %d\n", countPreserved(statements)))

	return output.String()
}

func countConcurrent(statements []types.Statement) int {
	count := 0
	for _, stmt := range statements {
		if stmt.Metadata.Concurrent {
			count++
		}
	}
	return count
}

func countNoTxn(statements []types.Statement) int {
	count := 0
	for _, stmt := range statements {
		if stmt.Metadata.RequiresNoTransaction {
			count++
		}
	}
	return count
}

func countPreserved(statements []types.Statement) int {
	count := 0
	for _, stmt := range statements {
		if stmt.Metadata.PreserveVerbatim {
			count++
		}
	}
	return count
}
