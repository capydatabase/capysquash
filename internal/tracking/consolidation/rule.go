package consolidation

import (
	"fmt"
	"strings"

	"github.com/capydatabase/capysquash/internal/config"
	"github.com/capydatabase/capysquash/internal/tracking"
	"github.com/capydatabase/capysquash/internal/types"
)

// ConsolidationRule interface for consolidation rules used by the squasher
type ConsolidationRule interface {
	CanApply(lifecycle *tracking.ObjectLifecycle) bool
	Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error)
	Risk() tracking.RiskLevel
}

// ConsolidationEngine interface for the engine that applies consolidation rules
type ConsolidationEngine interface {
	GetTracker() *tracking.Tracker
	GetConfig() *config.Config
	GetSafetyLevel() string // Returns the current safety level (paranoid, conservative, standard, aggressive)
}

// ConsolidationRuleEngine manages and applies consolidation rules
type ConsolidationRuleEngine struct {
	rules    []ConsolidationRule
	registry *RuleRegistry
	initErr  error
}

// NewConsolidationRuleEngine creates a new rule engine with all extracted rules
func NewConsolidationRuleEngine() *ConsolidationRuleEngine {
	engine := &ConsolidationRuleEngine{
		rules:    []ConsolidationRule{},
		registry: GetRegistry(),
	}

	// Register core rules in the global registry if not already registered
	if err := RegisterCoreRules(engine.registry); err != nil {
		engine.initErr = fmt.Errorf("failed to register core consolidation rules: %w", err)
	}

	return engine
}

// AddRule adds an explicitly ordered rule to the engine.
func (cre *ConsolidationRuleEngine) AddRule(rule ConsolidationRule) {
	cre.rules = append(cre.rules, rule)
}

// ruleWrapper is a rule that runs other rules itself (ErrorRecoveryRule).
type ruleWrapper interface {
	Delegates() []ConsolidationRule
}

// Rules returns the effective, ordered rules of this engine: a wrapping rule
// is listed right after the rules it runs. The returned slice is a copy;
// mutating it does not affect the engine.
func (cre *ConsolidationRuleEngine) Rules() []ConsolidationRule {
	var rules []ConsolidationRule
	for _, rule := range cre.rules {
		if wrapper, ok := rule.(ruleWrapper); ok {
			rules = append(rules, wrapper.Delegates()...)
		}
		rules = append(rules, rule)
	}
	return rules
}

// ApplyRules applies all applicable rules to a lifecycle
// If no rules apply, returns a default consolidation that preserves the original SQL
func (cre *ConsolidationRuleEngine) ApplyRules(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if cre.initErr != nil {
		return nil, cre.initErr
	}

	if len(cre.rules) > 0 {
		for _, rule := range cre.rules {
			if !rule.CanApply(lifecycle) {
				continue
			}

			result, err := rule.Apply(lifecycle, engine)
			if err != nil || result != nil {
				return result, err
			}
		}
		return createDefaultConsolidation(lifecycle), nil
	}

	registeredRules := cre.registry.GetApplicableRules(lifecycle)
	for _, registered := range registeredRules {
		result, err := registered.Rule.Apply(lifecycle, engine)
		if err != nil || result != nil {
			return result, err
		}
	}

	// No rules applied - return default preservation
	return createDefaultConsolidation(lifecycle), nil
}

// createDefaultConsolidation creates a default consolidation result that preserves the original SQL
// This ensures objects without matching consolidation rules are still included in output
// Single-version objects bypass ALL processing to preserve original SQL exactly
func createDefaultConsolidation(lifecycle *tracking.ObjectLifecycle) *tracking.ConsolidationResult {
	if lifecycle == nil || len(lifecycle.History) == 0 {
		return nil
	}

	// For single-version objects, use original SQL directly
	// This bypasses ALL AST round-tripping (parsing -> deparsing) that can corrupt:
	// - LANGUAGE placement (trailing -> leading)
	// - LANGUAGE type (sql <-> plpgsql)
	// - Volatility/security markers
	// - Function bodies with complex quoting
	if len(lifecycle.History) == 1 {
		originalStmt := lifecycle.History[0].Statement
		return &tracking.ConsolidationResult{
			OriginalStatements: []types.Statement{originalStmt},
			ConsolidatedSQL:    originalStmt.SQL, // Use ORIGINAL SQL directly, no processing
			Optimizations:      []string{"preserved_single_version_object"},
			RiskLevel:          tracking.RiskLevelLow, // Low risk - no changes made
			Warnings:           []string{},
		}
	}

	// For multi-version objects, get the final state (may involve processing)
	finalState := lifecycle.GetFinalState()
	if finalState == nil {
		return nil
	}

	// No rule merged the history, so the ALTERs made after the final CREATE
	// are replayed as written; the final state alone would lose them (a
	// column added later, a default changed later). The replay starts from
	// that CREATE: for objects other than tables the final state is the last
	// ALTER itself (ALTER FUNCTION ... RENAME), which would lose the CREATE.
	lastCreate := -1
	for i, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			lastCreate = i
		}
	}
	if lastCreate >= 0 {
		finalState = &lifecycle.History[lastCreate].Statement
	}
	statements := []types.Statement{*finalState}
	sql := strings.TrimRight(strings.TrimSpace(finalState.SQL), ";") + ";"
	if lastCreate >= 0 {
		for _, event := range lifecycle.History[lastCreate+1:] {
			if event.Operation != types.OpAlter || strings.TrimSpace(event.Statement.SQL) == "" {
				continue
			}
			statements = append(statements, event.Statement)
			sql += "\n\n" + strings.TrimRight(strings.TrimSpace(event.Statement.SQL), ";") + ";"
		}
	}

	return &tracking.ConsolidationResult{
		OriginalStatements: statements,
		ConsolidatedSQL:    sql,
		Optimizations:      []string{"preserved_as_is"},
		RiskLevel:          tracking.RiskLevelLow,
		Warnings:           []string{},
	}
}

// GetApplicableRules returns all rules that can be applied to a lifecycle
func (cre *ConsolidationRuleEngine) GetApplicableRules(lifecycle *tracking.ObjectLifecycle) []ConsolidationRule {
	if len(cre.rules) > 0 {
		var applicable []ConsolidationRule
		for _, rule := range cre.Rules() {
			if rule.CanApply(lifecycle) {
				applicable = append(applicable, rule)
			}
		}
		return applicable
	}

	registeredRules := cre.registry.GetApplicableRules(lifecycle)
	rules := make([]ConsolidationRule, len(registeredRules))
	for i, registered := range registeredRules {
		rules[i] = registered.Rule
	}
	return rules
}

// GetRegistry returns the rule registry.
func (cre *ConsolidationRuleEngine) GetRegistry() *RuleRegistry {
	return cre.registry
}
