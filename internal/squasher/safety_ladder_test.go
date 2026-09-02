package squasher

import (
	"fmt"
	"testing"
)

// ruleTypeSet returns the set of rule Go type names configured for a safety
// level. Type names are a stable proxy for rule identity here.
func ruleTypeSet(t *testing.T, level SafetyLevel) map[string]struct{} {
	t.Helper()

	engine, err := NewSquasherRuleEngine(level, nil)
	if err != nil {
		t.Fatalf("NewSquasherRuleEngine(%q) error: %v", level, err)
	}

	set := make(map[string]struct{})
	for _, rule := range engine.Rules() {
		set[fmt.Sprintf("%T", rule)] = struct{}{}
	}
	return set
}

func isSubset(sub, super map[string]struct{}) (string, bool) {
	for name := range sub {
		if _, ok := super[name]; !ok {
			return name, false
		}
	}
	return "", true
}

// TestSafetyLadderSubsetProperty proves the invariant:
//
//	paranoid ⊂ conservative ⊂ standard ⊂ aggressive
//
// Each stricter level's rule set must be a subset of the next-looser level.
func TestSafetyLadderSubsetProperty(t *testing.T) {
	t.Parallel()

	paranoid := ruleTypeSet(t, Paranoid)
	conservative := ruleTypeSet(t, Conservative)
	standard := ruleTypeSet(t, Standard)
	aggressive := ruleTypeSet(t, Aggressive)

	pairs := []struct {
		name       string
		sub, super map[string]struct{}
	}{
		{"paranoid ⊂ conservative", paranoid, conservative},
		{"conservative ⊂ standard", conservative, standard},
		{"standard ⊂ aggressive", standard, aggressive},
	}

	for _, p := range pairs {
		if missing, ok := isSubset(p.sub, p.super); !ok {
			t.Errorf("%s violated: rule %q present in stricter set but missing from looser set", p.name, missing)
		}
	}

	// Each looser level must add at least one rule (strict subset), otherwise
	// the ladder would collapse.
	if len(conservative) <= len(paranoid) {
		t.Errorf("conservative (%d rules) must add rules over paranoid (%d rules)", len(conservative), len(paranoid))
	}
	if len(standard) <= len(conservative) {
		t.Errorf("standard (%d rules) must add rules over conservative (%d rules)", len(standard), len(conservative))
	}
	if len(aggressive) <= len(standard) {
		t.Errorf("aggressive (%d rules) must add rules over standard (%d rules)", len(aggressive), len(standard))
	}
}

// TestSafetyLadderNoDeadCodeRule ensures the deleted DeadCodeRemovalRule is not
// reintroduced at any level.
func TestSafetyLadderNoDeadCodeRule(t *testing.T) {
	t.Parallel()

	for _, level := range []SafetyLevel{Paranoid, Conservative, Standard, Aggressive} {
		for name := range ruleTypeSet(t, level) {
			if name == "*consolidation.DeadCodeRemovalRule" {
				t.Errorf("safety level %q must not include DeadCodeRemovalRule", level)
			}
		}
	}
}

// TestParseSafetyLevelRejection verifies ParseSafetyLevel accepts every valid
// level and rejects unknown values (the single choke point wired into the CLI
// flag handler, pkg/engine convertConfig, and NewSquasherRuleEngine).
func TestParseSafetyLevelRejection(t *testing.T) {
	t.Parallel()

	valid := []string{"paranoid", "conservative", "standard", "aggressive"}
	for _, v := range valid {
		if _, err := ParseSafetyLevel(v); err != nil {
			t.Errorf("ParseSafetyLevel(%q) unexpected error: %v", v, err)
		}
	}

	invalid := []string{"", "PARANOID", "yolo", "safe", "none", "standard "}
	for _, v := range invalid {
		if _, err := ParseSafetyLevel(v); err == nil {
			t.Errorf("ParseSafetyLevel(%q) expected error, got nil", v)
		}
	}
}

// TestNewSquasherRuleEngineRejectsInvalid ensures the rule-engine constructor
// (the third ParseSafetyLevel entry point) errors on invalid levels rather
// than silently producing an engine with no consolidation rules.
func TestNewSquasherRuleEngineRejectsInvalid(t *testing.T) {
	t.Parallel()

	if _, err := NewSquasherRuleEngine(SafetyLevel("bogus"), nil); err == nil {
		t.Fatal("NewSquasherRuleEngine(\"bogus\") expected error, got nil")
	}
}

// TestRuleOverrides verifies the per-rule override surface (F-71): named rules
// can be force-enabled/disabled relative to the safety baseline, and unknown
// rule names are rejected rather than silently ignored.
func TestRuleOverrides(t *testing.T) {
	t.Parallel()

	// Unknown rule names must error.
	if _, err := NewSquasherRuleEngine(Standard, map[string]bool{"nonexistent_rule": true}); err == nil {
		t.Fatal("expected error for unknown rule override name")
	}
	if err := ValidateRuleOverrides(map[string]bool{"nonexistent_rule": false}); err == nil {
		t.Fatal("ValidateRuleOverrides expected error for unknown rule name")
	}
	if err := ValidateRuleOverrides(map[string]bool{"create_alter_consolidation": false}); err != nil {
		t.Fatalf("ValidateRuleOverrides rejected a valid rule name: %v", err)
	}

	// Disabling a baseline rule removes it.
	engine, err := NewSquasherRuleEngine(Standard, map[string]bool{"create_alter_consolidation": false})
	if err != nil {
		t.Fatalf("NewSquasherRuleEngine with disable override: %v", err)
	}
	for _, rule := range engine.Rules() {
		if fmt.Sprintf("%T", rule) == "*consolidation.CreateAlterConsolidationRule" {
			t.Fatal("create_alter_consolidation should have been disabled by override")
		}
	}

	// Enabling a rule outside the baseline adds it (function_deduplication is
	// aggressive-only in the ladder; enable it at standard).
	engine, err = NewSquasherRuleEngine(Standard, map[string]bool{"function_deduplication": true})
	if err != nil {
		t.Fatalf("NewSquasherRuleEngine with enable override: %v", err)
	}
	found := false
	rules := engine.Rules()
	for _, rule := range rules {
		if fmt.Sprintf("%T", rule) == "*consolidation.FunctionDeduplicationRule" {
			found = true
		}
	}
	if !found {
		t.Fatal("function_deduplication should have been enabled by override")
	}

	// The error-recovery rule must remain last.
	if len(rules) == 0 || fmt.Sprintf("%T", rules[len(rules)-1]) != "*consolidation.ErrorRecoveryRule" {
		t.Fatalf("error recovery rule must remain the final ladder entry, got %T", rules[len(rules)-1])
	}
}
