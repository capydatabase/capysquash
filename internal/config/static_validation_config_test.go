package config

import "testing"

func TestMergeStaticValidatorConfig_OverlayRuleOptions(t *testing.T) {
	defaults := StaticValidatorConfig{
		EnabledRules: []string{"CSQ.SAFETY.CONCURRENT_INDEX", "CSQ.BREAKING.DROP_COLUMN"},
		RuleOptions: map[string]map[string]any{
			"CSQ.SAFETY.CONCURRENT_INDEX": {
				"allowDropConcurrently": false,
			},
			"CSQ.HYGIENE.PREFER_BIGINT": {
				"autofix": true,
			},
		},
	}

	loaded := StaticValidatorConfig{
		RuleOptions: map[string]map[string]any{
			"CSQ.SAFETY.CONCURRENT_INDEX": {
				"allowDropConcurrently": true,
			},
			"CSQ.HYGIENE.PREFER_TEXT": {
				"enabled": false,
			},
		},
	}

	merged := mergeStaticValidatorConfig(loaded, defaults)

	if got := merged.RuleOptions["CSQ.SAFETY.CONCURRENT_INDEX"]["allowDropConcurrently"]; got != true {
		t.Fatalf("expected overridden value true, got %v", got)
	}
	if got := merged.RuleOptions["CSQ.HYGIENE.PREFER_BIGINT"]["autofix"]; got != true {
		t.Fatalf("expected default value true to be preserved, got %v", got)
	}
	if got := merged.RuleOptions["CSQ.HYGIENE.PREFER_TEXT"]["enabled"]; got != false {
		t.Fatalf("expected new overlay value false, got %v", got)
	}
}

func TestMergeStaticValidatorConfig_EnabledRulesFallback(t *testing.T) {
	defaults := StaticValidatorConfig{EnabledRules: []string{"rule-a", "rule-b"}}
	loaded := StaticValidatorConfig{}

	merged := mergeStaticValidatorConfig(loaded, defaults)
	if len(merged.EnabledRules) != 2 || merged.EnabledRules[0] != "rule-a" || merged.EnabledRules[1] != "rule-b" {
		t.Fatalf("expected defaults enabled rules to be copied, got %v", merged.EnabledRules)
	}

	// Ensure copy semantics (no aliasing)
	defaults.EnabledRules[0] = "changed"
	if merged.EnabledRules[0] != "rule-a" {
		t.Fatalf("expected merged rules to be independent copy, got %v", merged.EnabledRules)
	}
}

func TestMergeStaticValidatorConfig_DoesNotMutateDefaults(t *testing.T) {
	defaults := StaticValidatorConfig{
		RuleOptions: map[string]map[string]any{
			"rule-x": {"threshold": 10},
		},
	}
	loaded := StaticValidatorConfig{
		RuleOptions: map[string]map[string]any{
			"rule-x": {"threshold": 99},
		},
	}

	_ = mergeStaticValidatorConfig(loaded, defaults)

	if got := defaults.RuleOptions["rule-x"]["threshold"]; got != 10 {
		t.Fatalf("expected defaults to remain unchanged, got %v", got)
	}
}
