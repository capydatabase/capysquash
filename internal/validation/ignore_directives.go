package validation

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

const (
	ignoreMarker       = "-- capysquash-ignore:"
	ignoreNextMarker   = "-- capysquash-ignore-next:"
	ignoreFileMarker   = "-- capysquash-ignore-file:"
	ignoreCommentStart = "--"
)

type ignoreDirectiveScope string

const (
	ignoreScopeStatement ignoreDirectiveScope = "statement"
	ignoreScopeNext      ignoreDirectiveScope = "next"
	ignoreScopeFile      ignoreDirectiveScope = "file"
)

type ignoreDirective struct {
	line  int32
	scope ignoreDirectiveScope
	rules []string
	used  map[string]bool
}

// filterIgnoredViolations removes violations suppressed via
// -- capysquash-ignore:rule-code[,rule-code]
// -- capysquash-ignore-next:rule-code[,rule-code]
// -- capysquash-ignore-file:rule-code[,rule-code]
//
// Adapted from potential-tools/pgvet/nolint.go and scoped to per-statement ranges.
func filterIgnoredViolations(rawSQL string, violations []Violation) []Violation {
	directives := parseIgnoreDirectives(rawSQL)
	if len(directives) == 0 {
		return violations
	}

	filtered := make([]Violation, 0, len(violations))

	for _, violation := range violations {
		if violation.StmtStart < 0 || violation.StmtEnd <= violation.StmtStart || int(violation.StmtEnd) > len(rawSQL) {
			filtered = append(filtered, violation)
			continue
		}

		windowStart, _ := statementWindowBoundsWithLeadingComments(rawSQL, violation.StmtStart, violation.StmtEnd)
		stmtStartLine := lineNumberAtOffset(rawSQL, int32(windowStart))
		stmtEndLine := lineNumberAtOffset(rawSQL, violation.StmtEnd)

		suppressed := false
		for i := range directives {
			directive := &directives[i]
			if !directiveContainsRule(*directive, violation.Code) {
				continue
			}

			switch directive.scope {
			case ignoreScopeFile:
				suppressed = true
				directive.used[violation.Code] = true
			case ignoreScopeStatement:
				if directive.line >= stmtStartLine && directive.line <= stmtEndLine {
					suppressed = true
					directive.used[violation.Code] = true
				}
			case ignoreScopeNext:
				if directiveAppliesToNextStatement(rawSQL, directive.line, stmtStartLine) {
					suppressed = true
					directive.used[violation.Code] = true
				}
			}
		}

		if !suppressed {
			filtered = append(filtered, violation)
		}
	}

	filtered = append(filtered, buildUnusedIgnoreViolations(directives)...)
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].Line == filtered[j].Line {
			return filtered[i].Code < filtered[j].Code
		}
		return filtered[i].Line < filtered[j].Line
	})

	return filtered
}

// statementWindowWithLeadingComments expands the statement slice to include
// immediately preceding comment/blank lines so marker comments above a
// statement can suppress that statement's rule violations.
func statementWindowWithLeadingComments(rawSQL string, start, end int32) string {
	windowStart, windowEnd := statementWindowBoundsWithLeadingComments(rawSQL, start, end)
	return strings.TrimSpace(rawSQL[windowStart:windowEnd])
}

func statementWindowBoundsWithLeadingComments(rawSQL string, start, end int32) (int, int) {
	prefixStart := int(start)
	for prefixStart > 0 {
		lineStart := strings.LastIndex(rawSQL[:prefixStart], "\n")
		if lineStart < 0 {
			prefixStart = 0
			break
		}

		line := strings.TrimSpace(rawSQL[lineStart+1 : prefixStart])
		if line == "" || strings.HasPrefix(line, "--") {
			prefixStart = lineStart
			continue
		}

		break
	}

	return prefixStart, int(end)
}

func parseIgnoreDirectives(rawSQL string) []ignoreDirective {
	normalized := strings.ReplaceAll(rawSQL, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	directives := make([]ignoreDirective, 0)

	for idx, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, ignoreCommentStart) {
			continue
		}

		if rules := parseDirectiveRuleList(trimmed, ignoreFileMarker); len(rules) > 0 {
			directives = append(directives, ignoreDirective{
				line:  int32(idx + 1),
				scope: ignoreScopeFile,
				rules: rules,
				used:  make(map[string]bool),
			})
			continue
		}

		if rules := parseDirectiveRuleList(trimmed, ignoreNextMarker); len(rules) > 0 {
			directives = append(directives, ignoreDirective{
				line:  int32(idx + 1),
				scope: ignoreScopeNext,
				rules: rules,
				used:  make(map[string]bool),
			})
			continue
		}

		if rules := parseDirectiveRuleList(trimmed, ignoreMarker); len(rules) > 0 {
			directives = append(directives, ignoreDirective{
				line:  int32(idx + 1),
				scope: ignoreScopeStatement,
				rules: rules,
				used:  make(map[string]bool),
			})
		}
	}

	return directives
}

func parseDirectiveRuleList(trimmedLine, marker string) []string {
	if !strings.HasPrefix(trimmedLine, marker) {
		return nil
	}

	tail := strings.TrimSpace(strings.TrimPrefix(trimmedLine, marker))
	if tail == "" {
		return nil
	}

	parts := strings.Fields(tail)
	if len(parts) == 0 {
		return nil
	}

	rules := make([]string, 0)
	for code := range strings.SplitSeq(parts[0], ",") {
		normalized := strings.TrimSpace(code)
		if normalized != "" {
			rules = append(rules, normalized)
		}
	}

	return rules
}

func directiveContainsRule(directive ignoreDirective, ruleCode string) bool {
	return slices.Contains(directive.rules, ruleCode)
}

func directiveAppliesToNextStatement(rawSQL string, directiveLine, statementStartLine int32) bool {
	if statementStartLine <= directiveLine {
		return false
	}

	normalized := strings.ReplaceAll(rawSQL, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	if int(directiveLine) > len(lines) {
		return false
	}

	for i := directiveLine + 1; i < statementStartLine; i++ {
		if i <= 0 || int(i) > len(lines) {
			continue
		}

		trimmed := strings.TrimSpace(lines[i-1])
		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, ignoreCommentStart) {
			continue
		}

		return false
	}

	return true
}

func buildUnusedIgnoreViolations(directives []ignoreDirective) []Violation {
	violations := make([]Violation, 0)
	for _, directive := range directives {
		for _, code := range directive.rules {
			if directive.used[code] {
				continue
			}

			violations = append(violations, Violation{
				Code:       RuleCodeMetaUnusedIgnoreDirective,
				Category:   CategoryHygiene,
				Line:       directive.line,
				Statement:  code,
				Message:    fmt.Sprintf("Ignore directive for rule '%s' was not used.", code),
				Suggestion: "Remove this ignore directive or update it to match an active rule code.",
			})
		}
	}

	return violations
}
