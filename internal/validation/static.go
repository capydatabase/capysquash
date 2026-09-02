package validation

import (
	"fmt"
	"sort"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/config"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// ViolationCategory represents the severity/type of a violation
type ViolationCategory string

const (
	CategorySafety   ViolationCategory = "safety"
	CategoryBreaking ViolationCategory = "breaking"
	CategoryHygiene  ViolationCategory = "hygiene"
)

// Fix represents a suggested code change
type Fix struct {
	Replacement string `json:"replacement"`
	Start       int32  `json:"start"` // Byte offset start (inclusive)
	End         int32  `json:"end"`   // Byte offset end (exclusive)
}

// Violation represents a rule violation found during static analysis
type Violation struct {
	Code       string            `json:"code"`
	Message    string            `json:"message"`
	Category   ViolationCategory `json:"category"`
	Statement  string            `json:"statement,omitempty"`
	Line       int32             `json:"line,omitempty"`
	StmtStart  int32             `json:"stmt_start,omitempty"`
	StmtEnd    int32             `json:"stmt_end,omitempty"`
	Suggestion string            `json:"suggestion,omitempty"`
	Fix        *Fix              `json:"fix,omitempty"`
}

// ValidationRule defines the interface for static validation rules
type ValidationRule interface {
	Code() string
	Name() string
	Category() ViolationCategory
	Check(sql string, tree *pg_query.ParseResult) ([]Violation, error)
}

// StaticValidator performs AST-based checking of SQL
type StaticValidator struct {
	config *config.StaticValidatorConfig
	rules  []ValidationRule
}

// ResolveEnabledRules resolves the final enabled rule set from a base config and
// runtime enable/disable overrides.
//
// Behavior:
//   - If baseEnabled is empty, all registered rules are considered enabled.
//   - enableRules are added to the enabled set.
//   - disableRules are removed from the enabled set.
//   - Unknown rule codes are rejected.
func ResolveEnabledRules(baseEnabled, enableRules, disableRules []string) ([]string, error) {
	known := make(map[string]struct{})
	for _, code := range ListRuleCodes() {
		known[code] = struct{}{}
	}

	enabledSet := make(map[string]struct{})
	unknown := make(map[string]struct{})

	if len(baseEnabled) == 0 {
		for code := range known {
			enabledSet[code] = struct{}{}
		}
	} else {
		for _, code := range baseEnabled {
			trimmed := strings.TrimSpace(code)
			if trimmed == "" {
				continue
			}

			if _, ok := known[trimmed]; !ok {
				unknown[trimmed] = struct{}{}
				continue
			}

			enabledSet[trimmed] = struct{}{}
		}
	}

	for _, code := range enableRules {
		trimmed := strings.TrimSpace(code)
		if trimmed == "" {
			continue
		}

		if _, ok := known[trimmed]; !ok {
			unknown[trimmed] = struct{}{}
			continue
		}

		enabledSet[trimmed] = struct{}{}
	}

	for _, code := range disableRules {
		trimmed := strings.TrimSpace(code)
		if trimmed == "" {
			continue
		}

		if _, ok := known[trimmed]; !ok {
			unknown[trimmed] = struct{}{}
			continue
		}

		delete(enabledSet, trimmed)
	}

	if len(unknown) > 0 {
		unknownList := make([]string, 0, len(unknown))
		for code := range unknown {
			unknownList = append(unknownList, code)
		}
		sort.Strings(unknownList)
		return nil, fmt.Errorf("unknown rule code(s): %s", strings.Join(unknownList, ", "))
	}

	enabled := make([]string, 0, len(enabledSet))
	for code := range enabledSet {
		enabled = append(enabled, code)
	}
	sort.Strings(enabled)

	return enabled, nil
}

// NewStaticValidator creates a validator with the configured rules
func NewStaticValidator(conf *config.StaticValidatorConfig) *StaticValidator {
	if conf == nil {
		conf = config.DefaultStaticValidatorConfig()
	}

	var rules []ValidationRule
	allRules := GetAllRules()
	sort.Slice(allRules, func(i, j int) bool {
		return allRules[i].Code() < allRules[j].Code()
	})

	// If EnabledRules is empty, load all registered rules
	if len(conf.EnabledRules) == 0 {
		rules = allRules
	} else {
		// Load specific rules in deterministic order.
		enabledSet := make(map[string]struct{}, len(conf.EnabledRules))
		for _, code := range conf.EnabledRules {
			enabledSet[code] = struct{}{}
		}

		for _, rule := range allRules {
			if _, ok := enabledSet[rule.Code()]; ok {
				rules = append(rules, rule)
			}
		}
	}

	return &StaticValidator{
		config: conf,
		rules:  rules,
	}
}

// Check parses the SQL and runs all configured rules against the AST
func (v *StaticValidator) Check(sql string) ([]Violation, error) {
	if sql == "" {
		return nil, nil
	}

	tree, err := pg_query.Parse(sql)
	if err != nil {
		return nil, fmt.Errorf("failed to parse SQL: %w", err)
	}

	var violations []Violation
	for _, rule := range v.rules {
		ruleViolations, err := rule.Check(sql, tree)
		if err != nil {
			// Log error but continue validation? Or return?
			// For now, let's treat rule failure as non-fatal to other rules but return error
			return violations, fmt.Errorf("rule %s failed: %w", rule.Code(), err)
		}
		violations = append(violations, ruleViolations...)
	}

	violations = filterIgnoredViolations(sql, violations)
	annotateViolationLineNumbers(sql, violations)

	return violations, nil
}

func annotateViolationLineNumbers(rawSQL string, violations []Violation) {
	if strings.TrimSpace(rawSQL) == "" {
		return
	}

	for i := range violations {
		if violations[i].Line > 0 {
			continue
		}

		offset := violations[i].StmtStart
		if violations[i].Fix != nil {
			offset = violations[i].Fix.Start
		}

		if offset < 0 {
			offset = 0
		}

		violations[i].Line = lineNumberAtOffset(rawSQL, offset)
	}
}

func lineNumberAtOffset(rawSQL string, offset int32) int32 {
	if offset <= 0 {
		return 1
	}

	if int(offset) > len(rawSQL) {
		offset = int32(len(rawSQL))
	}

	line := int32(1)
	for i := int32(0); i < offset; i++ {
		if rawSQL[i] == '\n' {
			line++
		}
	}

	return line
}

// ApplyFixes applies the fixes from the given violations to the SQL string
func (v *StaticValidator) ApplyFixes(sql string, violations []Violation) (string, error) {
	// Collect fixes
	var fixes []*Fix
	for _, vio := range violations {
		if vio.Fix != nil {
			fixes = append(fixes, vio.Fix)
		}
	}

	if len(fixes) == 0 {
		return sql, nil
	}

	// Sort fixes in reverse order of position to avoid offset shifting
	sort.Slice(fixes, func(i, j int) bool {
		return fixes[i].Start > fixes[j].Start
	})

	// Apply fixes
	// Apply fixes
	currentSQL := sql
	for _, fix := range fixes {
		if fix.Start < 0 || fix.End > int32(len(currentSQL)) || fix.Start > fix.End {
			// Skip invalid ranges to be safe
			continue
		}

		// Apply replacement
		// prefix + replacement + suffix
		// We are iterating backwards, so we replace the chunk at fix.Start to fix.End

		prefix := currentSQL[:fix.Start]
		suffix := currentSQL[fix.End:]
		currentSQL = prefix + fix.Replacement + suffix
	}

	return currentSQL, nil
}
