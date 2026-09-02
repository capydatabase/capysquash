package validation

import (
	"sort"
	"sync"
)

// RuleRegistry manages the available validation rules
type RuleRegistry struct {
	mu    sync.RWMutex
	rules map[string]ValidationRule
}

var (
	// defaultRegistry is the global rule registry
	defaultRegistry = &RuleRegistry{
		rules: make(map[string]ValidationRule),
	}
)

// RegisterRule registers a validation rule with the global registry
func RegisterRule(rule ValidationRule) {
	defaultRegistry.Register(rule)
}

// GetRule returns a rule by its code
func GetRule(code string) (ValidationRule, bool) {
	return defaultRegistry.Get(code)
}

// GetAllRules returns all registered rules
func GetAllRules() []ValidationRule {
	return defaultRegistry.GetAll()
}

// ListRuleCodes returns all registered rule codes in sorted order.
func ListRuleCodes() []string {
	rules := defaultRegistry.GetAll()
	codes := make([]string, 0, len(rules))
	for _, rule := range rules {
		codes = append(codes, rule.Code())
	}
	sort.Strings(codes)
	return codes
}

// Register adds a rule to the registry
func (r *RuleRegistry) Register(rule ValidationRule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rules[rule.Code()] = rule
}

// Get retrieves a rule by its code
func (r *RuleRegistry) Get(code string) (ValidationRule, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rule, ok := r.rules[code]
	return rule, ok
}

// GetAll returns all registered rules
func (r *RuleRegistry) GetAll() []ValidationRule {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rules := make([]ValidationRule, 0, len(r.rules))
	for _, rule := range r.rules {
		rules = append(rules, rule)
	}
	return rules
}
