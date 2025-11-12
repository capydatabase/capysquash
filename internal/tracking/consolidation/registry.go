package consolidation

import (
	"fmt"
	"sort"
	"sync"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/tracking"
)

// RuleMetadata provides descriptive information about a consolidation rule
type RuleMetadata struct {
	Name        string   // Unique rule name (e.g., "create_alter_consolidation")
	Description string   // Human-readable description
	Category    RuleCategory // Rule category for organization
	Priority    int      // Execution priority (higher = executed first)
	Provider    string   // Provider name (e.g., "core", "supabase", "clerk")
	Tags        []string // Tags for filtering (e.g., "aggressive", "safe", "auth")
	Enabled     bool     // Whether rule is enabled
	Version     string   // Rule version for compatibility
}

// RuleCategory represents the category of a consolidation rule
type RuleCategory string

const (
	CategoryTableOps     RuleCategory = "table_operations"    // Table CREATE/ALTER consolidation
	CategoryFunctionOps  RuleCategory = "function_operations" // Function deduplication
	CategoryDeadCode     RuleCategory = "dead_code"           // Dead code removal
	CategorySecurity     RuleCategory = "security"            // RLS, policies, auth
	CategoryOptimization RuleCategory = "optimization"        // Performance optimizations
	CategoryExtension    RuleCategory = "extension"           // Extension-specific rules
	CategoryPluginAuth   RuleCategory = "plugin_auth"         // Plugin authentication patterns
	CategoryPluginORM    RuleCategory = "plugin_orm"          // ORM-specific patterns
)

// RegisteredRule wraps a ConsolidationRule with metadata
type RegisteredRule struct {
	Rule     ConsolidationRule
	Metadata RuleMetadata
}

// RuleRegistry manages dynamic rule registration with priorities and filtering
type RuleRegistry struct {
	mu             sync.RWMutex
	rules          map[string]*RegisteredRule // Key: rule name
	rulesByCategory map[RuleCategory][]*RegisteredRule
	rulesByProvider map[string][]*RegisteredRule
	conflictPolicy ConflictPolicy
}

// ConflictPolicy defines how to handle rule conflicts
type ConflictPolicy string

const (
	ConflictPolicyHighestPriority ConflictPolicy = "highest_priority" // Use rule with highest priority
	ConflictPolicyFirstRegistered ConflictPolicy = "first_registered" // Use first registered rule
	ConflictPolicyError           ConflictPolicy = "error"            // Return error on conflict
)

// Global registry instance
var globalRegistry *RuleRegistry
var once sync.Once

// GetRegistry returns the global rule registry (singleton)
func GetRegistry() *RuleRegistry {
	once.Do(func() {
		globalRegistry = NewRuleRegistry(ConflictPolicyHighestPriority)
	})
	return globalRegistry
}

// NewRuleRegistry creates a new rule registry
func NewRuleRegistry(conflictPolicy ConflictPolicy) *RuleRegistry {
	return &RuleRegistry{
		rules:           make(map[string]*RegisteredRule),
		rulesByCategory: make(map[RuleCategory][]*RegisteredRule),
		rulesByProvider: make(map[string][]*RegisteredRule),
		conflictPolicy:  conflictPolicy,
	}
}

// Register registers a rule with metadata
func (r *RuleRegistry) Register(rule ConsolidationRule, metadata RuleMetadata) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Validate metadata
	if metadata.Name == "" {
		return errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"rule name cannot be empty",
			errors.SeverityError,
			errors.CategoryConsolidation,
		)
	}
	if metadata.Provider == "" {
		metadata.Provider = "core"
	}
	if metadata.Version == "" {
		metadata.Version = "0.9.7"
	}

	// Check for conflicts
	if existing, exists := r.rules[metadata.Name]; exists {
		switch r.conflictPolicy {
		case ConflictPolicyHighestPriority:
			// Replace if new rule has higher priority
			if metadata.Priority <= existing.Metadata.Priority {
				return errors.NewError(
					errors.ErrorCodeConsolidationFailed,
					fmt.Sprintf("rule '%s' already registered with higher or equal priority (%d >= %d)",
						metadata.Name, existing.Metadata.Priority, metadata.Priority),
					errors.SeverityError,
					errors.CategoryConsolidation,
				)
			}
		case ConflictPolicyFirstRegistered:
			return errors.NewError(
				errors.ErrorCodeConsolidationFailed,
				fmt.Sprintf("rule '%s' already registered by provider '%s'",
					metadata.Name, existing.Metadata.Provider),
				errors.SeverityError,
				errors.CategoryConsolidation,
			)
		case ConflictPolicyError:
			return errors.NewError(
				errors.ErrorCodeConsolidationFailed,
				fmt.Sprintf("rule conflict: '%s' is already registered", metadata.Name),
				errors.SeverityError,
				errors.CategoryConsolidation,
			)
		}
	}

	// Register rule
	registered := &RegisteredRule{
		Rule:     rule,
		Metadata: metadata,
	}
	r.rules[metadata.Name] = registered

	// Index by category
	r.rulesByCategory[metadata.Category] = append(r.rulesByCategory[metadata.Category], registered)

	// Index by provider
	r.rulesByProvider[metadata.Provider] = append(r.rulesByProvider[metadata.Provider], registered)

	return nil
}

// Unregister removes a rule from the registry
func (r *RuleRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	registered, exists := r.rules[name]
	if !exists {
		return errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			fmt.Sprintf("rule '%s' not found", name),
			errors.SeverityError,
			errors.CategoryConsolidation,
		)
	}

	// Remove from main map
	delete(r.rules, name)

	// Remove from category index
	categoryRules := r.rulesByCategory[registered.Metadata.Category]
	r.rulesByCategory[registered.Metadata.Category] = r.removeFromSlice(categoryRules, registered)

	// Remove from provider index
	providerRules := r.rulesByProvider[registered.Metadata.Provider]
	r.rulesByProvider[registered.Metadata.Provider] = r.removeFromSlice(providerRules, registered)

	return nil
}

// EnableRule enables a rule
func (r *RuleRegistry) EnableRule(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	registered, exists := r.rules[name]
	if !exists {
		return errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			fmt.Sprintf("rule '%s' not found", name),
			errors.SeverityError,
			errors.CategoryConsolidation,
		)
	}

	registered.Metadata.Enabled = true
	return nil
}

// DisableRule disables a rule
func (r *RuleRegistry) DisableRule(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	registered, exists := r.rules[name]
	if !exists {
		return errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			fmt.Sprintf("rule '%s' not found", name),
			errors.SeverityError,
			errors.CategoryConsolidation,
		)
	}

	registered.Metadata.Enabled = false
	return nil
}

// GetRule retrieves a registered rule by name
func (r *RuleRegistry) GetRule(name string) (*RegisteredRule, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	registered, exists := r.rules[name]
	if !exists {
		return nil, errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			fmt.Sprintf("rule '%s' not found", name),
			errors.SeverityError,
			errors.CategoryConsolidation,
		)
	}

	return registered, nil
}

// GetAllRules returns all registered rules sorted by priority (descending)
func (r *RuleRegistry) GetAllRules() []*RegisteredRule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rules := make([]*RegisteredRule, 0, len(r.rules))
	for _, registered := range r.rules {
		rules = append(rules, registered)
	}

	// Sort by priority (descending)
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Metadata.Priority > rules[j].Metadata.Priority
	})

	return rules
}

// GetEnabledRules returns all enabled rules sorted by priority
func (r *RuleRegistry) GetEnabledRules() []*RegisteredRule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rules := make([]*RegisteredRule, 0)
	for _, registered := range r.rules {
		if registered.Metadata.Enabled {
			rules = append(rules, registered)
		}
	}

	// Sort by priority (descending)
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Metadata.Priority > rules[j].Metadata.Priority
	})

	return rules
}

// GetRulesByCategory returns all rules in a category sorted by priority
func (r *RuleRegistry) GetRulesByCategory(category RuleCategory) []*RegisteredRule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rules := make([]*RegisteredRule, len(r.rulesByCategory[category]))
	copy(rules, r.rulesByCategory[category])

	// Sort by priority (descending)
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Metadata.Priority > rules[j].Metadata.Priority
	})

	return rules
}

// GetRulesByProvider returns all rules from a provider sorted by priority
func (r *RuleRegistry) GetRulesByProvider(provider string) []*RegisteredRule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rules := make([]*RegisteredRule, len(r.rulesByProvider[provider]))
	copy(rules, r.rulesByProvider[provider])

	// Sort by priority (descending)
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Metadata.Priority > rules[j].Metadata.Priority
	})

	return rules
}

// GetRulesByTag returns all rules with a specific tag
func (r *RuleRegistry) GetRulesByTag(tag string) []*RegisteredRule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	rules := make([]*RegisteredRule, 0)
	for _, registered := range r.rules {
		if r.hasTag(registered.Metadata.Tags, tag) {
			rules = append(rules, registered)
		}
	}

	// Sort by priority (descending)
	sort.Slice(rules, func(i, j int) bool {
		return rules[i].Metadata.Priority > rules[j].Metadata.Priority
	})

	return rules
}

// GetApplicableRules returns all enabled rules that can apply to a lifecycle
func (r *RuleRegistry) GetApplicableRules(lifecycle *tracking.ObjectLifecycle) []*RegisteredRule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	applicable := make([]*RegisteredRule, 0)
	for _, registered := range r.rules {
		if registered.Metadata.Enabled && registered.Rule.CanApply(lifecycle) {
			applicable = append(applicable, registered)
		}
	}

	// Sort by priority (descending)
	sort.Slice(applicable, func(i, j int) bool {
		return applicable[i].Metadata.Priority > applicable[j].Metadata.Priority
	})

	return applicable
}

// GetStats returns registry statistics
func (r *RuleRegistry) GetStats() RegistryStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := RegistryStats{
		TotalRules:       len(r.rules),
		EnabledRules:     0,
		DisabledRules:    0,
		RulesByCategory:  make(map[RuleCategory]int),
		RulesByProvider:  make(map[string]int),
	}

	for _, registered := range r.rules {
		if registered.Metadata.Enabled {
			stats.EnabledRules++
		} else {
			stats.DisabledRules++
		}

		stats.RulesByCategory[registered.Metadata.Category]++
		stats.RulesByProvider[registered.Metadata.Provider]++
	}

	return stats
}

// RegistryStats provides statistics about the registry
type RegistryStats struct {
	TotalRules      int
	EnabledRules    int
	DisabledRules   int
	RulesByCategory map[RuleCategory]int
	RulesByProvider map[string]int
}

// Clear removes all rules from the registry
func (r *RuleRegistry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rules = make(map[string]*RegisteredRule)
	r.rulesByCategory = make(map[RuleCategory][]*RegisteredRule)
	r.rulesByProvider = make(map[string][]*RegisteredRule)
}

// Helper methods

func (r *RuleRegistry) removeFromSlice(slice []*RegisteredRule, target *RegisteredRule) []*RegisteredRule {
	for i, registered := range slice {
		if registered == target {
			return append(slice[:i], slice[i+1:]...)
		}
	}
	return slice
}

func (r *RuleRegistry) hasTag(tags []string, tag string) bool {
	for _, t := range tags {
		if t == tag {
			return true
		}
	}
	return false
}

// RegisterCoreRules registers all core consolidation rules with metadata
func RegisterCoreRules(registry *RuleRegistry) error {
	coreRules := []struct {
		rule     ConsolidationRule
		metadata RuleMetadata
	}{
		{
			rule: &MultipleCreateConsolidationRule{},
			metadata: RuleMetadata{
				Name:        "multiple_create_consolidation",
				Description: "Consolidates multiple CREATE statements for the same object",
				Category:    CategoryTableOps,
				Priority:    100,
				Provider:    "core",
				Tags:        []string{"safe", "standard"},
				Enabled:     true,
				Version:     "0.9.7",
			},
		},
		{
			rule: &SeparateAlterRule{},
			metadata: RuleMetadata{
				Name:        "separate_alter",
				Description: "Identifies ALTER statements that must remain separate from CREATE TABLE (RLS, renames, etc.)",
				Category:    CategoryTableOps,
				Priority:    95, // Higher priority than CreateAlterConsolidationRule (90)
				Provider:    "core",
				Tags:        []string{"safe", "standard", "execution-order"},
				Enabled:     true,
				Version:     "0.9.7",
			},
		},
		{
			rule: &CreateAlterConsolidationRule{},
			metadata: RuleMetadata{
				Name:        "create_alter_consolidation",
				Description: "Combines CREATE and ALTER TABLE into single CREATE",
				Category:    CategoryTableOps,
				Priority:    90,
				Provider:    "core",
				Tags:        []string{"safe", "standard"},
				Enabled:     true,
				Version:     "0.9.7",
			},
		},
		{
			rule: &DropCreateCycleRule{},
			metadata: RuleMetadata{
				Name:        "drop_create_cycle",
				Description: "Removes redundant DROP/CREATE cycles",
				Category:    CategoryTableOps,
				Priority:    85,
				Provider:    "core",
				Tags:        []string{"safe", "optimization"},
				Enabled:     true,
				Version:     "0.9.7",
			},
		},
		{
			rule: &FunctionDeduplicationRule{},
			metadata: RuleMetadata{
				Name:        "function_deduplication",
				Description: "Removes duplicate function definitions",
				Category:    CategoryFunctionOps,
				Priority:    80,
				Provider:    "core",
				Tags:        []string{"safe", "deduplication"},
				Enabled:     true,
				Version:     "0.9.7",
			},
		},
		{
			rule: &PublicationDeduplicationRule{},
			metadata: RuleMetadata{
				Name:        "publication_deduplication",
				Description: "Removes duplicate publication member additions",
				Category:    CategoryOptimization,
				Priority:    80,
				Provider:    "core",
				Tags:        []string{"safe", "replication"},
				Enabled:     true,
				Version:     "0.9.7",
			},
		},
		{
			rule: &DeadCodeRemovalRule{},
			metadata: RuleMetadata{
				Name:        "dead_code_removal",
				Description: "Removes unreferenced functions and objects",
				Category:    CategoryDeadCode,
				Priority:    70,
				Provider:    "core",
				Tags:        []string{"aggressive", "cleanup"},
				Enabled:     false, // Disabled by default (aggressive)
				Version:     "0.9.7",
			},
		},
		{
			rule: &EnumDeduplicationRule{},
			metadata: RuleMetadata{
				Name:        "enum_deduplication",
				Description: "Removes duplicate ENUM type definitions",
				Category:    CategoryTableOps,
				Priority:    75,
				Provider:    "core",
				Tags:        []string{"safe", "types"},
				Enabled:     true,
				Version:     "0.9.7",
			},
		},
		{
			rule: &ColumnEvolutionRule{},
			metadata: RuleMetadata{
				Name:        "column_evolution",
				Description: "Consolidates column modifications",
				Category:    CategoryTableOps,
				Priority:    65,
				Provider:    "core",
				Tags:        []string{"standard", "schema"},
				Enabled:     true,
				Version:     "0.9.7",
			},
		},
		{
			rule: &RLSConsolidationRule{},
			metadata: RuleMetadata{
				Name:        "rls_consolidation",
				Description: "Consolidates duplicate RLS operations to final state",
				Category:    CategorySecurity,
				Priority:    96, // Higher priority than SeparateAlterRule (95) - must consolidate before separation
				Provider:    "core",
				Tags:        []string{"safe", "security", "rls"},
				Enabled:     true, // Re-enabled: Now works with SeparateAlterRule for proper RLS handling
				Version:     "0.9.7",
			},
		},
		{
			rule: &DOBlockEnumTypeRule{},
			metadata: RuleMetadata{
				Name:        "do_block_enum_type",
				Description: "Handles DO block ENUM type operations",
				Category:    CategoryTableOps,
				Priority:    55,
				Provider:    "core",
				Tags:        []string{"safe", "types", "do_block"},
				Enabled:     true,
				Version:     "0.9.7",
			},
		},
		{
			rule: NewExternalDependencyFilterRule(),
			metadata: RuleMetadata{
				Name:        "external_dependency_filter",
				Description: "Filters external dependency operations",
				Category:    CategoryOptimization,
				Priority:    50,
				Provider:    "core",
				Tags:        []string{"standard", "dependencies"},
				Enabled:     true,
				Version:     "0.9.7",
			},
		},
		{
			rule: &ConditionalSchemaRule{},
			metadata: RuleMetadata{
				Name:        "conditional_schema",
				Description: "Handles conditional schema operations",
				Category:    CategoryTableOps,
				Priority:    45,
				Provider:    "core",
				Tags:        []string{"safe", "schema"},
				Enabled:     true,
				Version:     "0.9.7",
			},
		},
		{
			rule: &TransactionBoundaryRule{},
			metadata: RuleMetadata{
				Name:        "transaction_boundary",
				Description: "Manages transaction boundary preservation",
				Category:    CategoryOptimization,
				Priority:    40,
				Provider:    "core",
				Tags:        []string{"safe", "transactions"},
				Enabled:     true,
				Version:     "0.9.7",
			},
		},
		{
			rule: NewErrorRecoveryRule(3, "conservative", true),
			metadata: RuleMetadata{
				Name:        "error_recovery",
				Description: "Handles error recovery and retry logic",
				Category:    CategoryOptimization,
				Priority:    30,
				Provider:    "core",
				Tags:        []string{"safe", "recovery"},
				Enabled:     true,
				Version:     "0.9.7",
			},
		},
	}

	for _, cr := range coreRules {
		if err := registry.Register(cr.rule, cr.metadata); err != nil {
			return errors.NewError(
				errors.ErrorCodeConsolidationFailed,
				fmt.Sprintf("failed to register core rule '%s'", cr.metadata.Name),
				errors.SeverityError,
				errors.CategoryConsolidation,
			).WithInnerError(err)
		}
	}

	return nil
}
