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

// ErrorRecoveryRule provides enhanced error recovery and validation for consolidation failures
type ErrorRecoveryRule struct {
	MaxRetries     int
	RecoveryMode   string // "conservative", "aggressive", "fallback"
	ValidateSQL    bool
	LogFailures    bool
	FailureMetrics map[string]int
}

// NewErrorRecoveryRule creates a new error recovery rule with specified configuration
func NewErrorRecoveryRule(maxRetries int, recoveryMode string, validateSQL bool) *ErrorRecoveryRule {
	return &ErrorRecoveryRule{
		MaxRetries:     maxRetries,
		RecoveryMode:   recoveryMode,
		ValidateSQL:    validateSQL,
		LogFailures:    true,
		FailureMetrics: make(map[string]int),
	}
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
	objectKey := lifecycle.Key
	failureCount, hasFailures := rule.FailureMetrics[objectKey]

	// If we've exceeded max retries, apply fallback strategy
	if hasFailures && failureCount >= rule.MaxRetries {
		return rule.applyFallbackStrategy(lifecycle, engine)
	}

	// Attempt normal consolidation with validation
	result, err := rule.attemptConsolidation(lifecycle, engine)
	if err != nil {
		// Record failure and attempt recovery
		rule.FailureMetrics[objectKey] = failureCount + 1
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
			rule.FailureMetrics[objectKey] = failureCount + 1
			return rule.attemptErrorRecovery(lifecycle, engine, validationErr)
		}
	}

	return result, nil
}

// Risk returns the risk level for error recovery rule
func (rule *ErrorRecoveryRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow // Error recovery is inherently low risk
}

// attemptConsolidation tries to apply normal consolidation rules
func (rule *ErrorRecoveryRule) attemptConsolidation(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	// This would delegate to other consolidation rules
	// For now, return a basic result to allow testing of error recovery
	if len(lifecycle.History) == 0 {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "no operations to consolidate", map[string]interface{}{"object": lifecycle.Name})
	}

	// Try to create a basic consolidated statement
	finalState := lifecycle.GetFinalState()
	if finalState == nil {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "no final state available for consolidation", map[string]interface{}{"object": lifecycle.Name})
	}

	// DEBUG: Log SQL for cleanup_expired_memory_cards and current_clerk_org_id
	if strings.Contains(strings.ToLower(lifecycle.Name), "cleanup_expired") || strings.Contains(strings.ToLower(lifecycle.Name), "current_clerk_org_id") {
		utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY-DEBUG").Info("%s finalState.SQL length=%d", lifecycle.Name, len(finalState.SQL))
		utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY-DEBUG").Info("  SQL preview (first 300): %s", strings.ReplaceAll(finalState.SQL[:min(300, len(finalState.SQL))], "\n", "\\n"))
		utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY-DEBUG").Info("  History count: %d", len(lifecycle.History))
		for i, event := range lifecycle.History {
			utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY-DEBUG").Info("    Event %d: Op=%s, SQL length=%d, SQL preview: %s",
				i, event.Operation, len(event.Statement.SQL),
				strings.ReplaceAll(event.Statement.SQL[:min(100, len(event.Statement.SQL))], "\n", "\\n"))
		}
	}

	// BUG-001 fix: For indexes without explicit access method, remove "USING btree" from SQL
	// to prevent spatial index errors. pg_query may have added it during parsing.
	consolidatedSQL := finalState.SQL
	if lifecycle.Type == types.TypeIndex && !finalState.IndexHadExplicitAccessMethod {
		utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY").Info("INDEX %s: IndexHadExplicitAccessMethod=%v, checking for USING btree",
			lifecycle.Name, finalState.IndexHadExplicitAccessMethod)
		utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY-DEBUG").Info("INDEX %s: SQL = %s", lifecycle.Name, consolidatedSQL)
		// Check if SQL contains "USING btree" (case-insensitive)
		if strings.Contains(strings.ToUpper(consolidatedSQL), " USING BTREE") {
			// Remove "USING btree" - use regex to handle spacing variations
			re := regexp.MustCompile(`(?i)\s+USING\s+btree\s*`)
			consolidatedSQL = re.ReplaceAllString(consolidatedSQL, " ")
			utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY").Info("Removed implicit USING btree from index %s", lifecycle.Name)
		} else {
			utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY").Info("Index %s has no USING btree in SQL (length=%d)",
				lifecycle.Name, len(consolidatedSQL))
		}
		utils.GetDefaultLogger().WithPrefix("ERROR-RECOVERY-DEBUG").Info("INDEX %s: After check, consolidated SQL = %s", lifecycle.Name, consolidatedSQL)
	}

	return &tracking.ConsolidationResult{
		ConsolidatedSQL:    consolidatedSQL,
		OriginalStatements: rule.extractStatements(lifecycle.History),
		Optimizations:      []string{"error_recovery_applied"},
		Warnings:           []string{},
	}, nil
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
		return nil, nil // Skip empty objects
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

	return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "no valid operations found for fallback", map[string]interface{}{"object": lifecycle.Name})
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
			return errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "potentially dangerous SQL detected", map[string]interface{}{"pattern": pattern})
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

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
