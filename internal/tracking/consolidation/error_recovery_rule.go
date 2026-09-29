package consolidation

import (
	"fmt"
	"slices"
	"strings"

	"github.com/capydatabase/capysquash/internal/utils"

	"github.com/capydatabase/capysquash/internal/tracking"
	"github.com/capydatabase/capysquash/internal/types"

	"sync"

	"github.com/capydatabase/capysquash/internal/errors"
)

// ErrorRecoveryRule runs the consolidation rules it wraps and recovers when
// one of them fails: the first delegate that applies and produces a result
// wins, and an error from a delegate (or an invalid result) is handled by the
// configured recovery mode instead of aborting the object.
type ErrorRecoveryRule struct {
	MaxRetries     int
	RecoveryMode   string // "conservative", "aggressive", "fallback"
	ValidateSQL    bool
	LogFailures    bool
	FailureMetrics map[string]int
	mutex          sync.RWMutex
	delegates      []ConsolidationRule
}

// NewErrorRecoveryRule creates an error recovery rule that wraps delegates,
// tried in order. With no delegates it consolidates every object to its
// default form (see createDefaultConsolidation).
func NewErrorRecoveryRule(maxRetries int, recoveryMode string, validateSQL bool, delegates ...ConsolidationRule) *ErrorRecoveryRule {
	return &ErrorRecoveryRule{
		MaxRetries:     maxRetries,
		RecoveryMode:   recoveryMode,
		ValidateSQL:    validateSQL,
		LogFailures:    true,
		FailureMetrics: make(map[string]int),
		delegates:      slices.Clone(delegates),
	}
}

// Delegates returns the rules this rule runs, in order. The returned slice
// is a copy.
func (rule *ErrorRecoveryRule) Delegates() []ConsolidationRule {
	return slices.Clone(rule.delegates)
}

// CanApply returns true for all objects to provide universal error recovery
func (rule *ErrorRecoveryRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	return true // Universal error recovery
}

// Apply implements the ConsolidationRule interface with error recovery
func (rule *ErrorRecoveryRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if rule.FailureMetrics == nil {
		rule.FailureMetrics = make(map[string]int)
	}

	// Track if this object has had failures before
	// Track if this object has had failures before
	objectKey := lifecycle.Key

	rule.mutex.RLock()
	failureCount, hasFailures := rule.FailureMetrics[objectKey]
	rule.mutex.RUnlock()

	// If we've exceeded max retries, apply fallback strategy
	if hasFailures && failureCount >= rule.MaxRetries {
		return rule.applyFallbackStrategy(lifecycle, engine)
	}

	// Attempt normal consolidation with validation
	result, err := rule.attemptConsolidation(lifecycle, engine)
	if err != nil {
		// Record failure and attempt recovery
		rule.mutex.Lock()
		rule.FailureMetrics[objectKey] = failureCount + 1
		rule.mutex.Unlock()

		if rule.LogFailures {
			utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY").Info("Consolidation failed for %s (attempt %d/%d): %v",
				objectKey, failureCount+1, rule.MaxRetries, err)
		}

		// Try error recovery
		return rule.attemptErrorRecovery(lifecycle, engine, err)
	}

	// Validate the result if enabled
	if rule.ValidateSQL && result != nil {
		if validationErr := rule.validateConsolidatedSQL(result); validationErr != nil {
			// Mark as failed and try recovery
			rule.mutex.Lock()
			rule.FailureMetrics[objectKey] = failureCount + 1
			rule.mutex.Unlock()
			return rule.attemptErrorRecovery(lifecycle, engine, validationErr)
		}
	}

	return result, nil
}

// Risk returns the risk level for error recovery rule
func (rule *ErrorRecoveryRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow // Error recovery is inherently low risk
}

// attemptConsolidation runs the delegate rules in order: the first one that
// applies and returns a result wins, and a delegate error is returned so
// Apply can recover. When no delegate produces a result the object gets its
// default consolidation (the original SQL for a single-version object, the
// final state otherwise); a dropped object yields no result.
func (rule *ErrorRecoveryRule) attemptConsolidation(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if len(lifecycle.History) == 0 {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "no operations to consolidate", map[string]any{"object": lifecycle.Name})
	}

	for _, delegate := range rule.delegates {
		if !delegate.CanApply(lifecycle) {
			continue
		}
		result, err := delegate.Apply(lifecycle, engine)
		if err != nil {
			return nil, fmt.Errorf("%T: %w", delegate, err)
		}
		if result != nil {
			return result, nil
		}
	}

	if lifecycle.GetFinalState() == nil {
		// Dropped as the last operation: nothing to emit, which is not an error.
		if lifecycle.History[len(lifecycle.History)-1].Operation == types.OpDrop {
			return nil, nil
		}
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "no final state available for consolidation", map[string]any{"object": lifecycle.Name})
	}

	result := createDefaultConsolidation(lifecycle)

	// An index written without an access method must not gain "USING btree"
	// from an AST round trip: spatial columns would then fail to index.
	if lifecycle.Type == types.TypeIndex && !lifecycle.GetFinalState().IndexHadExplicitAccessMethod &&
		strings.Contains(strings.ToUpper(result.ConsolidatedSQL), " USING BTREE") {
		result.ConsolidatedSQL = stripErrorRecoveryUsingBtreeClause(result.ConsolidatedSQL)
		utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY").Info("Removed implicit USING btree from index %s", lifecycle.Name)
	}

	return result, nil
}

// attemptErrorRecovery tries various recovery strategies when consolidation fails
func (rule *ErrorRecoveryRule) attemptErrorRecovery(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine, originalErr error) (*tracking.ConsolidationResult, error) {
	switch rule.RecoveryMode {
	case "conservative":
		return rule.conservativeRecovery(lifecycle, originalErr)
	case "aggressive":
		return rule.aggressiveRecovery(lifecycle, originalErr)
	case "fallback":
		return rule.applyFallbackStrategy(lifecycle, engine)
	default:
		return rule.conservativeRecovery(lifecycle, originalErr)
	}
}

// conservativeRecovery uses the original statements without modification
func (rule *ErrorRecoveryRule) conservativeRecovery(lifecycle *tracking.ObjectLifecycle, originalErr error) (*tracking.ConsolidationResult, error) {
	utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY").Info("Applying conservative recovery for %s", lifecycle.Key)

	// Use all original statements unchanged
	var sqlParts []string
	for _, event := range lifecycle.History {
		if event.Statement.SQL != "" {
			sqlParts = append(sqlParts, event.Statement.SQL)
		}
	}

	return &tracking.ConsolidationResult{
		ConsolidatedSQL:    strings.Join(sqlParts, ";\n") + ";",
		OriginalStatements: rule.extractStatements(lifecycle.History),
		Optimizations:      []string{"conservative_recovery"},
		Warnings:           []string{fmt.Sprintf("Original error: %v", originalErr)},
	}, nil
}

// aggressiveRecovery tries to salvage parts of the consolidation
func (rule *ErrorRecoveryRule) aggressiveRecovery(lifecycle *tracking.ObjectLifecycle, originalErr error) (*tracking.ConsolidationResult, error) {
	utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY").Info("Applying aggressive recovery for %s", lifecycle.Key)

	// Try to use the final state, fall back to conservative if that fails
	finalState := lifecycle.GetFinalState()
	if finalState != nil {
		return &tracking.ConsolidationResult{
			ConsolidatedSQL:    finalState.SQL,
			OriginalStatements: rule.extractStatements(lifecycle.History),
			Optimizations:      []string{"aggressive_recovery"},
			Warnings:           []string{fmt.Sprintf("Recovered from: %v", originalErr)},
		}, nil
	}

	// Fall back to conservative recovery
	return rule.conservativeRecovery(lifecycle, originalErr)
}

// applyFallbackStrategy applies when max retries exceeded
func (rule *ErrorRecoveryRule) applyFallbackStrategy(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY").Info("Applying fallback strategy for %s (max retries exceeded)", lifecycle.Key)

	// Skip the object entirely or use minimal processing
	if len(lifecycle.History) == 0 {
		// Skip empty objects by returning empty result
		return &tracking.ConsolidationResult{
			ConsolidatedSQL:    "",
			OriginalStatements: []types.Statement{},
			Optimizations:      []string{"skipped_empty"},
			Warnings:           []string{"Skipped empty object in fallback"},
		}, nil
	}

	// Return first valid operation
	for _, event := range lifecycle.History {
		if event.Statement.SQL != "" {
			return &tracking.ConsolidationResult{
				ConsolidatedSQL:    event.Statement.SQL,
				OriginalStatements: []types.Statement{event.Statement},
				Optimizations:      []string{"fallback_strategy"},
				Warnings:           []string{"Applied fallback due to repeated failures"},
			}, nil
		}
	}

	return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "no valid operations found for fallback", map[string]any{"object": lifecycle.Name})
}

// validateConsolidatedSQL performs basic SQL validation
func (rule *ErrorRecoveryRule) validateConsolidatedSQL(result *tracking.ConsolidationResult) error {
	sql := strings.TrimSpace(result.ConsolidatedSQL)

	// Basic validation checks
	if sql == "" {
		return errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "empty SQL generated", nil)
	}

	// Check for obvious syntax issues
	if !strings.HasSuffix(sql, ";") {
		result.ConsolidatedSQL = sql + ";"
		result.Warnings = append(result.Warnings, "Added missing semicolon")
	}

	// Check for dangerous patterns that might indicate consolidation errors
	dangerousPatterns := []string{
		"DROP DATABASE",
		"DROP SCHEMA",
		"TRUNCATE",
	}

	for _, pattern := range dangerousPatterns {
		if strings.Contains(strings.ToUpper(sql), pattern) {
			return errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "potentially dangerous SQL detected", map[string]any{"pattern": pattern})
		}
	}

	return nil
}

// extractStatements extracts statements from lifecycle events
func (rule *ErrorRecoveryRule) extractStatements(history []tracking.LifecycleEvent) []types.Statement {
	var statements []types.Statement
	for _, event := range history {
		statements = append(statements, event.Statement)
	}
	return statements
}

func stripErrorRecoveryUsingBtreeClause(sql string) string {
	if strings.TrimSpace(sql) == "" {
		return sql
	}

	lower := strings.ToLower(sql)
	for i := 0; i < len(lower); i++ {
		if !hasErrorRecoveryKeywordAt(lower, i, "using") {
			continue
		}

		j := skipErrorRecoveryWhitespace(lower, i+len("using"))
		if !hasErrorRecoveryKeywordAt(lower, j, "btree") {
			continue
		}

		start := i
		for start > 0 && isErrorRecoveryWhitespaceByte(lower[start-1]) {
			start--
		}

		end := j + len("btree")
		for end < len(lower) && isErrorRecoveryWhitespaceByte(lower[end]) {
			end++
		}

		prefix := sql[:start]
		suffix := sql[end:]

		if prefix != "" && suffix != "" && !isErrorRecoveryWhitespaceByte(prefix[len(prefix)-1]) && !isErrorRecoveryWhitespaceByte(suffix[0]) {
			return prefix + " " + suffix
		}

		return prefix + suffix
	}

	return sql
}

func hasErrorRecoveryKeywordAt(value string, pos int, keyword string) bool {
	if pos < 0 || pos+len(keyword) > len(value) {
		return false
	}

	if value[pos:pos+len(keyword)] != keyword {
		return false
	}

	if pos > 0 && isErrorRecoveryIdentifierByte(value[pos-1]) {
		return false
	}

	end := pos + len(keyword)
	if end < len(value) && isErrorRecoveryIdentifierByte(value[end]) {
		return false
	}

	return true
}

func skipErrorRecoveryWhitespace(value string, pos int) int {
	for pos < len(value) && isErrorRecoveryWhitespaceByte(value[pos]) {
		pos++
	}
	return pos
}

func isErrorRecoveryIdentifierByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func isErrorRecoveryWhitespaceByte(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}
