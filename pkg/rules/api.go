// Package rules provides consolidation rule management for pgsquash.
package rules

//
// This package exposes the rule registry that manages consolidation rules,
// allowing dynamic enable/disable, category filtering, and rule introspection.
//
// # Rule Registry
//
// The rule registry is a singleton that manages all consolidation rules:
//
//	registry := rules.GetRegistry()
//
//	// List all rules
//	allRules := registry.GetAllRules()
//	for _, rule := range allRules {
//	    fmt.Printf("Rule: %s (enabled: %v)\n", rule.Metadata.Name, rule.Metadata.Enabled)
//	}
//
//	// Enable/disable rules
//	err := registry.EnableRule("create_alter_consolidation")
//	err = registry.DisableRule("dead_code_removal")
//
// # Categories
//
// Rules are organized into categories:
//
//	// Get rules by category
//	tableRules := registry.GetRulesByCategory(rules.CategoryTableOps)
//
//	// Get statistics
//	stats := registry.GetStats()
//	fmt.Printf("Total rules: %d\n", stats.TotalRules)
//	fmt.Printf("Enabled: %d, Disabled: %d\n", stats.EnabledRules, stats.DisabledRules)
//
// # Bulk Operations
//
// Efficiently enable/disable multiple rules:
//
//	// Enable all table operation rules
//	tableRules := registry.GetRulesByCategory(rules.CategoryTableOps)
//	for _, rule := range tableRules {
//	    registry.EnableRule(rule.Metadata.Name)
//	}
//
// # Rule Metadata
//
// Each rule has rich metadata:
//
//	rule, err := registry.GetRule("create_alter_consolidation")
//	if err == nil {
//	    fmt.Printf("Name: %s\n", rule.Metadata.Name)
//	    fmt.Printf("Description: %s\n", rule.Metadata.Description)
//	    fmt.Printf("Category: %s\n", rule.Metadata.Category)
//	    fmt.Printf("Priority: %d\n", rule.Metadata.Priority)
//	    fmt.Printf("Provider: %s\n", rule.Metadata.Provider)
//	    fmt.Printf("Tags: %v\n", rule.Metadata.Tags)
//	    fmt.Printf("Version: %s\n", rule.Metadata.Version)
//	}
//
// # Available Rules
//
// Core rules (provider: "core"):
//   - create_alter_consolidation: Consolidates CREATE + ALTER operations
//   - drop_create_sequence: Removes DROP-CREATE cycles
//   - create_drop_consolidation: Removes CREATE-DROP patterns
//   - enum_deduplication: Removes duplicate ENUM definitions
//   - column_evolution: Consolidates column modifications
//   - external_dependency_filter: Filters external dependencies
//   - transaction_boundary: Respects transaction boundaries
//   - dead_code_removal: Removes dead code (DISABLED by default)
//
// Plugin rules (provider: "supabase", "clerk", "prisma", "drizzle"):
//   - Various plugin-specific optimizations

import (
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/tracking/consolidation"
)

// Re-export types from internal package
type (
	// RuleRegistry manages dynamic rule registration with priorities and filtering
	RuleRegistry = consolidation.RuleRegistry

	// RegisteredRule wraps a ConsolidationRule with metadata
	RegisteredRule = consolidation.RegisteredRule

	// RuleMetadata provides descriptive information about a consolidation rule
	RuleMetadata = consolidation.RuleMetadata

	// RuleCategory represents the category of a consolidation rule
	RuleCategory = consolidation.RuleCategory

	// RegistryStats provides statistics about the registry
	RegistryStats = consolidation.RegistryStats

	// ConsolidationRule is the interface that all consolidation rules implement
	ConsolidationRule = tracking.ConsolidationRule

	// ConflictPolicy determines how rule conflicts are resolved
	ConflictPolicy = consolidation.ConflictPolicy
)

// Rule categories
const (
	// CategoryTableOps includes table CREATE/ALTER consolidation rules
	CategoryTableOps RuleCategory = consolidation.CategoryTableOps

	// CategoryFunctionOps includes function deduplication rules
	CategoryFunctionOps RuleCategory = consolidation.CategoryFunctionOps

	// CategoryDeadCode includes dead code removal rules
	CategoryDeadCode RuleCategory = consolidation.CategoryDeadCode

	// CategorySecurity includes RLS, policies, and auth rules
	CategorySecurity RuleCategory = consolidation.CategorySecurity

	// CategoryOptimization includes performance optimization rules
	CategoryOptimization RuleCategory = consolidation.CategoryOptimization

	// CategoryExtension includes extension-specific rules
	CategoryExtension RuleCategory = consolidation.CategoryExtension

	// CategoryPluginAuth includes plugin authentication pattern rules
	CategoryPluginAuth RuleCategory = consolidation.CategoryPluginAuth

	// CategoryPluginORM includes ORM-specific pattern rules
	CategoryPluginORM RuleCategory = consolidation.CategoryPluginORM
)

// Conflict policies
const (
	// ConflictPolicyHighestPriority uses rule with highest priority (default)
	ConflictPolicyHighestPriority ConflictPolicy = consolidation.ConflictPolicyHighestPriority

	// ConflictPolicyFirstRegistered uses first registered rule
	ConflictPolicyFirstRegistered ConflictPolicy = consolidation.ConflictPolicyFirstRegistered

	// ConflictPolicyError fails on conflicts
	ConflictPolicyError ConflictPolicy = consolidation.ConflictPolicyError
)

// Registry access
var (
	// GetRegistry returns the global rule registry singleton
	GetRegistry = consolidation.GetRegistry
)

// RuleRegistry methods
// These are available on *RuleRegistry instances:
//
//   Register(rule ConsolidationRule, metadata RuleMetadata) error
//   Unregister(name string) error
//   EnableRule(name string) error
//   DisableRule(name string) error
//   GetRule(name string) (*RegisteredRule, error)
//   GetAllRules() []*RegisteredRule
//   GetEnabledRules() []*RegisteredRule
//   GetRulesByCategory(category RuleCategory) []*RegisteredRule
//   GetRulesByProvider(provider string) []*RegisteredRule
//   GetRulesByTag(tag string) []*RegisteredRule
//   GetApplicableRules(lifecycle *ObjectLifecycle) []*RegisteredRule
//   GetStats() RegistryStats
//   Clear()

// Example usage patterns

// ExampleListAllRules demonstrates listing all rules
func ExampleListAllRules() {
	registry := GetRegistry()

	allRules := registry.GetAllRules()
	for _, rule := range allRules {
		// Access rule metadata
		_ = rule.Metadata.Name
		_ = rule.Metadata.Description
		_ = rule.Metadata.Category
		_ = rule.Metadata.Priority
		_ = rule.Metadata.Enabled
	}
}

// ExampleEnableDisableRules demonstrates enabling and disabling rules
func ExampleEnableDisableRules() {
	registry := GetRegistry()

	// Enable a rule
	err := registry.EnableRule("create_alter_consolidation")
	if err != nil {
		panic(err)
	}

	// Disable a rule
	err = registry.DisableRule("dead_code_removal")
	if err != nil {
		panic(err)
	}
}

// ExampleFilterByCategory demonstrates category filtering
func ExampleFilterByCategory() {
	registry := GetRegistry()

	// Get all table operation rules
	tableRules := registry.GetRulesByCategory(CategoryTableOps)
	for _, rule := range tableRules {
		_ = rule.Metadata.Name
	}

	// Get all security rules
	securityRules := registry.GetRulesByCategory(CategorySecurity)
	for _, rule := range securityRules {
		_ = rule.Metadata.Name
	}
}

// ExampleBulkOperations demonstrates bulk enable/disable
func ExampleBulkOperations() {
	registry := GetRegistry()

	// Enable all rules in a category
	tableRules := registry.GetRulesByCategory(CategoryTableOps)
	for _, rule := range tableRules {
		if err := registry.EnableRule(rule.Metadata.Name); err != nil {
			// Handle error
		}
	}

	// Disable all aggressive rules
	aggressiveRules := registry.GetRulesByTag("aggressive")
	for _, rule := range aggressiveRules {
		if err := registry.DisableRule(rule.Metadata.Name); err != nil {
			// Handle error
		}
	}
}

// ExampleRegistryStats demonstrates getting registry statistics
func ExampleRegistryStats() {
	registry := GetRegistry()

	stats := registry.GetStats()

	// Total counts
	_ = stats.TotalRules
	_ = stats.EnabledRules
	_ = stats.DisabledRules

	// By category
	for category, count := range stats.RulesByCategory {
		_ = category
		_ = count
	}

	// By provider
	for provider, count := range stats.RulesByProvider {
		_ = provider
		_ = count
	}
}

// ExampleRuleMetadata demonstrates accessing rule metadata
func ExampleRuleMetadata() {
	registry := GetRegistry()

	rule, err := registry.GetRule("create_alter_consolidation")
	if err != nil {
		panic(err)
	}

	// Access metadata
	_ = rule.Metadata.Name        // "create_alter_consolidation"
	_ = rule.Metadata.Description // Human-readable description
	_ = rule.Metadata.Category    // CategoryTableOps
	_ = rule.Metadata.Priority    // Higher = executed first
	_ = rule.Metadata.Provider    // "core", "supabase", etc.
	_ = rule.Metadata.Tags        // ["safe", "standard"]
	_ = rule.Metadata.Enabled     // true/false
	_ = rule.Metadata.Version     // "1.0.0"
}

// CategoryNames returns human-readable names for all categories
func CategoryNames() map[RuleCategory]string {
	return map[RuleCategory]string{
		CategoryTableOps:     "Table Operations",
		CategoryFunctionOps:  "Function Operations",
		CategoryDeadCode:     "Dead Code",
		CategorySecurity:     "Security",
		CategoryOptimization: "Optimization",
		CategoryExtension:    "Extension",
		CategoryPluginAuth:   "Plugin Auth",
		CategoryPluginORM:    "Plugin ORM",
	}
}

// CategoryDescriptions returns descriptions for all categories
func CategoryDescriptions() map[RuleCategory]string {
	return map[RuleCategory]string{
		CategoryTableOps:     "Rules for consolidating table creation and modification",
		CategoryFunctionOps:  "Rules for deduplicating and optimizing functions",
		CategoryDeadCode:     "Rules for removing dead or unused code",
		CategorySecurity:     "Rules for RLS policies and authentication patterns",
		CategoryOptimization: "Rules for performance optimizations",
		CategoryExtension:    "Extension-specific consolidation rules",
		CategoryPluginAuth:   "Plugin authentication pattern rules",
		CategoryPluginORM:    "ORM-specific pattern rules",
	}
}
