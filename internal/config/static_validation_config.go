package config

import "maps"

// StaticValidatorConfig configures the static validator
type StaticValidatorConfig struct {
	// EnabledRules is a list of rule codes to enable.
	// If empty or nil, all registered rules are enabled.
	EnabledRules []string `json:"enabled_rules" yaml:"enabled_rules" toml:"enabled_rules"`

	// RuleOptions allows passing custom configuration to specific rules.
	// Key is the rule code.
	RuleOptions map[string]map[string]any `json:"rule_options" yaml:"rule_options" toml:"rule_options"`

	// TreatWarningsAsErrors causes all violations to be reported as errors
	TreatWarningsAsErrors bool `json:"treat_warnings_as_errors" yaml:"treat_warnings_as_errors" toml:"treat_warnings_as_errors"`
}

// DefaultStaticValidatorConfig returns a default configuration
func DefaultStaticValidatorConfig() *StaticValidatorConfig {
	return &StaticValidatorConfig{
		EnabledRules: nil, // All rules enabled by default
		RuleOptions:  make(map[string]map[string]any),
	}
}

func mergeStaticValidatorConfig(loaded, defaults StaticValidatorConfig) StaticValidatorConfig {
	merged := loaded

	if len(merged.EnabledRules) == 0 {
		if defaults.EnabledRules != nil {
			rules := make([]string, len(defaults.EnabledRules))
			copy(rules, defaults.EnabledRules)
			merged.EnabledRules = rules
		}
	}

	// Overlay semantics (adapted from potential-tools/pgvet/config.go):
	// start with defaults, then overlay loaded values.
	merged.RuleOptions = deepCopyRuleOptions(defaults.RuleOptions)
	for code, loadedOptions := range loaded.RuleOptions {
		if _, ok := merged.RuleOptions[code]; !ok {
			merged.RuleOptions[code] = map[string]any{}
		}
		maps.Copy(merged.RuleOptions[code], loadedOptions)
	}

	return merged
}

func deepCopyRuleOptions(input map[string]map[string]any) map[string]map[string]any {
	if input == nil {
		return map[string]map[string]any{}
	}

	output := make(map[string]map[string]any, len(input))
	for ruleCode, options := range input {
		if options == nil {
			output[ruleCode] = map[string]any{}
			continue
		}

		optionCopy := make(map[string]any, len(options))
		maps.Copy(optionCopy, options)
		output[ruleCode] = optionCopy
	}

	return output
}
