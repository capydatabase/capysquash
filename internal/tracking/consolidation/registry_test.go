package consolidation

import (
	"testing"

	"github.com/capydatabase/capysquash/internal/buildinfo"
)

// Rule metadata is stamped with the version the binary was built as, not a
// literal that goes stale with every release.
func TestCoreRulesCarryTheBuildVersion(t *testing.T) {
	prev := buildinfo.Version()
	buildinfo.Set("7.7.7-registrytest", "", "")
	t.Cleanup(func() { buildinfo.Set(prev, "", "") })

	registry := NewRuleRegistry(ConflictPolicyHighestPriority)
	if err := RegisterCoreRules(registry); err != nil {
		t.Fatalf("RegisterCoreRules: %v", err)
	}

	rules := registry.GetAllRules()
	if len(rules) == 0 {
		t.Fatal("no core rules registered")
	}
	for _, rule := range rules {
		if rule.Metadata.Version != "7.7.7-registrytest" {
			t.Errorf("rule %s has version %q, want the build version", rule.Metadata.Name, rule.Metadata.Version)
		}
	}
}
