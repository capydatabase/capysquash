// Package parser provides statement metadata analysis for lock levels and transaction requirements.
package parser

import (
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/types"
)

// StatementAnalyzer analyzes SQL statements to determine metadata like lock levels,
// transaction requirements, and execution characteristics.
type StatementAnalyzer struct {
	pgVersion string // PostgreSQL version for version-specific behavior
}

// NewStatementAnalyzer creates a new statement analyzer
func NewStatementAnalyzer(pgVersion string) *StatementAnalyzer {
	return &StatementAnalyzer{
		pgVersion: pgVersion,
	}
}

// AnalyzeStatement analyzes a statement and populates its metadata
func (sa *StatementAnalyzer) AnalyzeStatement(stmt *types.Statement) {
	stmt.Metadata = sa.analyzeMetadata(stmt)
}

// analyzeMetadata determines the complete metadata for a statement
func (sa *StatementAnalyzer) analyzeMetadata(stmt *types.Statement) types.StatementMetadata {
	metadata := types.StatementMetadata{
		LockLevel:     sa.determineLockLevel(stmt),
		Concurrent:    sa.isConcurrent(stmt),
		Idempotent:    sa.isIdempotent(stmt),
		ExecutionTime: sa.estimateExecutionTime(stmt),
	}

	// Determine if statement requires no transaction
	metadata.RequiresNoTransaction = metadata.Concurrent || sa.requiresNoTransaction(stmt)

	// Determine version gates
	metadata.VersionGate = sa.determineVersionGate(stmt)

	return metadata
}

// determineLockLevel determines the PostgreSQL lock level required by a statement
func (sa *StatementAnalyzer) determineLockLevel(stmt *types.Statement) types.LockLevel {
	sql := strings.ToUpper(stmt.SQL)

	// Data operations
	if stmt.IsDataOp {
		switch stmt.Operation {
		case types.OpInsert, types.OpUpdate, types.OpDelete:
			return types.LockRowExclusive
		}
	}

	// Concurrent operations take lower locks
	if sa.isConcurrent(stmt) {
		if strings.Contains(sql, "CREATE INDEX CONCURRENTLY") ||
			strings.Contains(sql, "CREATE UNIQUE INDEX CONCURRENTLY") {
			return types.LockShareUpdateExclusive
		}
		if strings.Contains(sql, "REFRESH MATERIALIZED VIEW CONCURRENTLY") {
			return types.LockExclusive
		}
		if strings.Contains(sql, "DROP INDEX CONCURRENTLY") {
			return types.LockShareUpdateExclusive
		}
	}

	// DDL operations by type
	switch stmt.Operation {
	case types.OpCreate:
		return sa.determineCreateLockLevel(stmt, sql)
	case types.OpAlter:
		return sa.determineAlterLockLevel(stmt, sql)
	case types.OpDrop:
		return sa.determineDropLockLevel(stmt, sql)
	case types.OpGrant, types.OpRevoke:
		return types.LockAccessExclusive
	}

	// TRUNCATE, VACUUM FULL
	if strings.Contains(sql, "TRUNCATE") {
		return types.LockAccessExclusive
	}
	if strings.Contains(sql, "VACUUM FULL") {
		return types.LockAccessExclusive
	}
	if strings.Contains(sql, "VACUUM") && !strings.Contains(sql, "FULL") {
		return types.LockShareUpdateExclusive
	}

	// Default for unknown statements
	return types.LockAccessExclusive
}

// determineCreateLockLevel determines lock level for CREATE statements
func (sa *StatementAnalyzer) determineCreateLockLevel(stmt *types.Statement, sql string) types.LockLevel {
	switch stmt.ObjectType {
	case types.TypeIndex:
		if sa.isConcurrent(stmt) {
			return types.LockShareUpdateExclusive
		}
		return types.LockShare
	case types.TypeTable, types.TypeView, types.TypeFunction, types.TypeTrigger,
		types.TypeSequence, types.TypeSchema, types.TypeExtension, types.TypeType,
		types.TypeEnum, types.TypeDomain, types.TypeComposite:
		// Most CREATE statements on new objects don't lock existing tables
		return types.LockNone
	case types.TypePolicy:
		// RLS policies lock the table
		return types.LockAccessExclusive
	default:
		return types.LockAccessExclusive
	}
}

// determineAlterLockLevel determines lock level for ALTER statements
func (sa *StatementAnalyzer) determineAlterLockLevel(stmt *types.Statement, sql string) types.LockLevel {
	// ALTER TYPE ... ADD VALUE
	if stmt.ObjectType == types.TypeEnum || stmt.ObjectType == types.TypeType {
		if strings.Contains(sql, "ADD VALUE") {
			// PG >= 12 can run in transaction, PG < 12 cannot
			if sa.pgVersion >= "12" {
				return types.LockShareUpdateExclusive
			}
			return types.LockAccessExclusive
		}
	}

	// ALTER TABLE operations vary widely
	if stmt.ObjectType == types.TypeTable {
		// Adding columns with defaults can be slow
		if strings.Contains(sql, "ADD COLUMN") && strings.Contains(sql, "DEFAULT") {
			return types.LockAccessExclusive
		}
		// Adding NOT NULL requires table scan
		if strings.Contains(sql, "SET NOT NULL") {
			return types.LockAccessExclusive
		}
		// Adding constraints
		if strings.Contains(sql, "ADD CONSTRAINT") {
			if strings.Contains(sql, "NOT VALID") {
				// NOT VALID constraints are fast
				return types.LockShareUpdateExclusive
			}
			return types.LockAccessExclusive
		}
		// Most other ALTER TABLE operations
		return types.LockAccessExclusive
	}

	// Default for ALTER
	return types.LockAccessExclusive
}

// determineDropLockLevel determines lock level for DROP statements
func (sa *StatementAnalyzer) determineDropLockLevel(stmt *types.Statement, sql string) types.LockLevel {
	if stmt.ObjectType == types.TypeIndex && sa.isConcurrent(stmt) {
		return types.LockShareUpdateExclusive
	}
	// Most DROP operations require ACCESS EXCLUSIVE
	return types.LockAccessExclusive
}

// isConcurrent checks if a statement uses CONCURRENTLY
func (sa *StatementAnalyzer) isConcurrent(stmt *types.Statement) bool {
	return strings.Contains(strings.ToUpper(stmt.SQL), "CONCURRENTLY")
}

// requiresNoTransaction checks if statement must run outside a transaction
func (sa *StatementAnalyzer) requiresNoTransaction(stmt *types.Statement) bool {
	sql := strings.ToUpper(stmt.SQL)

	// CONCURRENTLY operations cannot run in transactions
	if sa.isConcurrent(stmt) {
		return true
	}

	// VACUUM cannot run in transaction
	if strings.Contains(sql, "VACUUM") {
		return true
	}

	// CREATE DATABASE cannot run in transaction
	if strings.Contains(sql, "CREATE DATABASE") {
		return true
	}

	// ALTER TYPE ... ADD VALUE on PG < 12
	if stmt.ObjectType == types.TypeEnum || stmt.ObjectType == types.TypeType {
		if strings.Contains(sql, "ADD VALUE") && sa.pgVersion < "12" {
			return true
		}
	}

	return false
}

// determineVersionGate determines if statement has PostgreSQL version requirements
func (sa *StatementAnalyzer) determineVersionGate(stmt *types.Statement) string {
	sql := strings.ToUpper(stmt.SQL)

	// ALTER TYPE ... ADD VALUE requires special handling on PG < 12
	if (stmt.ObjectType == types.TypeEnum || stmt.ObjectType == types.TypeType) &&
		strings.Contains(sql, "ADD VALUE") {
		return "PG<12: Cannot run in transaction"
	}

	// Generated columns (PG >= 12)
	if strings.Contains(sql, "GENERATED ALWAYS") {
		return "PG>=12"
	}

	// Stored generated columns (PG >= 12)
	if strings.Contains(sql, "STORED") && strings.Contains(sql, "GENERATED") {
		return "PG>=12"
	}

	// Multirange types (PG >= 14)
	if stmt.ObjectType == types.TypeMultirangeType {
		return "PG>=14"
	}

	// Subscriptions (PG >= 10)
	if stmt.ObjectType == types.TypeSubscription {
		return "PG>=10"
	}

	// Statistics objects (PG >= 10)
	if stmt.ObjectType == types.TypeStatistic {
		return "PG>=10"
	}

	return ""
}

// isIdempotent checks if a statement is idempotent
func (sa *StatementAnalyzer) isIdempotent(stmt *types.Statement) bool {
	// IF NOT EXISTS / IF EXISTS clauses make statements idempotent
	if stmt.IfNotExists {
		return true
	}

	sql := strings.ToUpper(stmt.SQL)
	if strings.Contains(sql, "IF EXISTS") {
		return true
	}

	// Data operations are generally not idempotent
	if stmt.IsDataOp {
		return false
	}

	// CREATE OR REPLACE is idempotent
	if strings.Contains(sql, "CREATE OR REPLACE") {
		return true
	}

	// Most DDL without IF NOT EXISTS is not idempotent
	return false
}

// estimateExecutionTime estimates how long a statement might take
func (sa *StatementAnalyzer) estimateExecutionTime(stmt *types.Statement) types.ExecutionTimeCategory {
	sql := strings.ToUpper(stmt.SQL)

	// Data operations on large tables can be slow
	if stmt.IsDataOp {
		return types.ExecutionUnknown // Depends on data volume
	}

	// Operations that scan tables
	if strings.Contains(sql, "SET NOT NULL") ||
		(strings.Contains(sql, "ADD CONSTRAINT") && !strings.Contains(sql, "NOT VALID")) {
		return types.ExecutionSlow
	}

	// Adding columns with defaults (PG < 11 requires table rewrite)
	if strings.Contains(sql, "ADD COLUMN") && strings.Contains(sql, "DEFAULT") {
		if sa.pgVersion < "11" {
			return types.ExecutionSlow
		}
		return types.ExecutionFast
	}

	// Index creation
	if stmt.ObjectType == types.TypeIndex && stmt.Operation == types.OpCreate {
		if sa.isConcurrent(stmt) {
			return types.ExecutionSlow // CONCURRENTLY is slower but non-blocking
		}
		return types.ExecutionMedium
	}

	// VACUUM operations
	if strings.Contains(sql, "VACUUM") {
		return types.ExecutionSlow
	}

	// Most DDL is fast on empty/small tables
	return types.ExecutionFast
}

// AnalyzePragmas checks for manual override pragmas in comments
func (sa *StatementAnalyzer) AnalyzePragmas(stmt *types.Statement) {
	for _, comment := range stmt.Comments {
		commentUpper := strings.ToUpper(comment)

		// Check for pgsquash:ignore pragma
		if strings.Contains(commentUpper, "PGSQUASH:IGNORE") ||
			strings.Contains(commentUpper, "PGSQUASH: IGNORE") {
			stmt.Metadata.PreserveVerbatim = true
		}

		// Check for pgsquash:no-merge pragma
		if strings.Contains(commentUpper, "PGSQUASH:NO-MERGE") ||
			strings.Contains(commentUpper, "PGSQUASH: NO-MERGE") {
			stmt.Metadata.PreserveVerbatim = true
		}
	}

	// Also check inline comments in SQL
	if containsPgsquashPragma(stmt.SQL) {
		stmt.Metadata.PreserveVerbatim = true
	}
}

func containsPgsquashPragma(sql string) bool {
	for line := range strings.SplitSeq(sql, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "--") {
			continue
		}

		body := strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(trimmed, "--")))
		if strings.HasPrefix(body, "PGSQUASH:IGNORE") ||
			strings.HasPrefix(body, "PGSQUASH: IGNORE") ||
			strings.HasPrefix(body, "PGSQUASH:NO-MERGE") ||
			strings.HasPrefix(body, "PGSQUASH: NO-MERGE") {
			return true
		}
	}

	return false
}

// FormatLockLevel returns a human-readable description of a lock level
func FormatLockLevel(level types.LockLevel) string {
	descriptions := map[types.LockLevel]string{
		types.LockNone:                 "No lock (safe)",
		types.LockAccessShare:          "ACCESS SHARE (SELECT)",
		types.LockRowShare:             "ROW SHARE (SELECT FOR UPDATE)",
		types.LockRowExclusive:         "ROW EXCLUSIVE (DML)",
		types.LockShareUpdateExclusive: "SHARE UPDATE EXCLUSIVE (VACUUM, CREATE INDEX CONCURRENTLY)",
		types.LockShare:                "SHARE (CREATE INDEX)",
		types.LockShareRowExclusive:    "SHARE ROW EXCLUSIVE (rare)",
		types.LockExclusive:            "EXCLUSIVE (REFRESH MATERIALIZED VIEW CONCURRENTLY)",
		types.LockAccessExclusive:      "ACCESS EXCLUSIVE (most DDL, blocks all access)",
	}

	if desc, ok := descriptions[level]; ok {
		return desc
	}
	return string(level)
}
