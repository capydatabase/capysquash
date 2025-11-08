package consolidation

import (
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"
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
	GetConfig() interface{}     // This would be the actual config type
	GetAIAnalyzer() interface{} // Returns *ai.Analyzer if available, nil otherwise
	GetSafetyLevel() string     // Returns the current safety level (paranoid, conservative, standard, aggressive)
}

// ConsolidationRuleEngine manages and applies consolidation rules
// It now uses the RuleRegistry for dynamic rule management
type ConsolidationRuleEngine struct {
	rules       []ConsolidationRule // Legacy: kept for backward compatibility
	registry    *RuleRegistry       // New: use registry for dynamic rules
	useRegistry bool                // Whether to use registry (true) or legacy (false)
}

// NewConsolidationRuleEngine creates a new rule engine with all extracted rules
// Uses the global rule registry for dynamic rule management
func NewConsolidationRuleEngine() *ConsolidationRuleEngine {
	engine := &ConsolidationRuleEngine{
		rules:       []ConsolidationRule{},
		registry:    GetRegistry(),
		useRegistry: true, // Use registry by default
	}

	// Register core rules in the global registry if not already registered
	_ = RegisterCoreRules(engine.registry)

	return engine
}

// NewLegacyConsolidationRuleEngine creates a rule engine using legacy static rules
// This is kept for backward compatibility
func NewLegacyConsolidationRuleEngine() *ConsolidationRuleEngine {
	engine := &ConsolidationRuleEngine{
		rules:       []ConsolidationRule{},
		useRegistry: false,
	}

	// Register all extracted consolidation rules
	// Order matters: rules are applied in the order they are added
	engine.AddRule(&MultipleCreateConsolidationRule{})
	engine.AddRule(&CreateAlterConsolidationRule{})
	engine.AddRule(&DropCreateCycleRule{})
	engine.AddRule(&FunctionDeduplicationRule{})
	engine.AddRule(&DeadCodeRemovalRule{})
	engine.AddRule(&TransactionBoundaryRule{})
	engine.AddRule(&DOBlockEnumTypeRule{})
	engine.AddRule(NewExternalDependencyFilterRule())
	engine.AddRule(&ConditionalSchemaRule{})
	engine.AddRule(&EnumDeduplicationRule{})
	engine.AddRule(&ColumnEvolutionRule{})
	engine.AddRule(&RLSConsolidationRule{})
	engine.AddRule(&PublicationDeduplicationRule{})
	engine.AddRule(NewErrorRecoveryRule(3, "conservative", true)) // 3 retries, conservative mode, validate SQL

	return engine
}

// AddRule adds a consolidation rule to the engine
func (cre *ConsolidationRuleEngine) AddRule(rule ConsolidationRule) {
	cre.rules = append(cre.rules, rule)
}

// ApplyRules applies all applicable rules to a lifecycle
// Uses registry if enabled, falls back to legacy rules otherwise
// If no rules apply, returns a default consolidation that preserves the original SQL
func (cre *ConsolidationRuleEngine) ApplyRules(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if cre.useRegistry {
		// Get applicable rules from registry (sorted by priority)
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

	// Legacy behavior: iterate through static rules
	for _, rule := range cre.rules {
		if rule.CanApply(lifecycle) {
			return rule.Apply(lifecycle, engine)
		}
	}

	// No rules applied - return default preservation
	return createDefaultConsolidation(lifecycle), nil
}

// createDefaultConsolidation creates a default consolidation result that preserves the original SQL
// This ensures objects without matching consolidation rules are still included in output
// BUG #2 FIX: Single-version objects bypass ALL processing to preserve original SQL exactly
func createDefaultConsolidation(lifecycle *tracking.ObjectLifecycle) *tracking.ConsolidationResult {
	if lifecycle == nil || len(lifecycle.History) == 0 {
		return nil
	}

	// BUG #2 FIX: For single-version objects, use original SQL directly
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

	return &tracking.ConsolidationResult{
		OriginalStatements: []types.Statement{*finalState},
		ConsolidatedSQL:    finalState.SQL,
		Optimizations:      []string{"preserved_as_is"},
		RiskLevel:          tracking.RiskLevelLow,
		Warnings:           []string{},
	}
}

// GetApplicableRules returns all rules that can be applied to a lifecycle
// Uses registry if enabled, falls back to legacy rules otherwise
func (cre *ConsolidationRuleEngine) GetApplicableRules(lifecycle *tracking.ObjectLifecycle) []ConsolidationRule {
	if cre.useRegistry {
		// Get applicable rules from registry
		registeredRules := cre.registry.GetApplicableRules(lifecycle)
		rules := make([]ConsolidationRule, len(registeredRules))
		for i, registered := range registeredRules {
			rules[i] = registered.Rule
		}
		return rules
	}

	// Legacy behavior: check static rules
	var applicable []ConsolidationRule
	for _, rule := range cre.rules {
		if rule.CanApply(lifecycle) {
			applicable = append(applicable, rule)
		}
	}
	return applicable
}

// GetRegistry returns the rule registry if using registry mode
func (cre *ConsolidationRuleEngine) GetRegistry() *RuleRegistry {
	return cre.registry
}

// UseRegistry enables registry mode
func (cre *ConsolidationRuleEngine) UseRegistry() {
	cre.useRegistry = true
	if cre.registry == nil {
		cre.registry = GetRegistry()
	}
}

// UseLegacyRules disables registry mode and uses legacy static rules
func (cre *ConsolidationRuleEngine) UseLegacyRules() {
	cre.useRegistry = false
}
