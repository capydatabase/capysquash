package consolidation

import (
	"fmt"
	"slices"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/utils"

	"github.com/capy-base/pgsquash-engine/internal/tracking"
	"github.com/capy-base/pgsquash-engine/internal/types"

	"github.com/capy-base/pgsquash-engine/internal/errors"
)

// EnumDeduplicationRule detects and resolves duplicate ENUM type definitions
// This handles cases where multiple ENUMs with similar names or conflicting values exist
type EnumDeduplicationRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *EnumDeduplicationRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	// Only apply to ENUM types
	if lifecycle.Type != types.TypeEnum && lifecycle.Type != types.TypeType {
		return false
	}

	// Check if this is an ENUM (either standalone or in DO block)
	hasEnumDef := false
	for _, event := range lifecycle.History {
		sqlUpper := strings.ToUpper(event.Statement.SQL)
		if strings.Contains(sqlUpper, "CREATE TYPE") && strings.Contains(sqlUpper, "AS ENUM") {
			hasEnumDef = true
			break
		}
	}

	return hasEnumDef
}

// Apply applies the consolidation rule to the given lifecycle
func (r *EnumDeduplicationRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]any{"rule": "EnumDedupRule"})
	}

	// Get safety level to determine behavior
	safetyLevel := engine.GetSafetyLevel()
	tracker := engine.GetTracker()

	// Collect all ENUM definitions
	var enumStmts []types.Statement

	for _, event := range lifecycle.History {
		if isCreateEnumStatement(event.Statement.SQL) {
			enumStmts = append(enumStmts, event.Statement)
		}
	}

	if len(enumStmts) == 0 {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "no ENUM statements found", map[string]any{"object": lifecycle.Name})
	}

	// Check for duplicate ENUM types across the entire codebase
	// CRITICAL FIX: Only consider enums in the SAME SCHEMA as duplicates
	typeName := lifecycle.Name
	typeSchema := lifecycle.Schema // Get the schema (defaults to "public" if not specified)
	allLifecycles := tracker.GetObjectsByCategory()
	var conflictingEnums []tracking.ObjectLifecycle
	var primaryEnum string // The name that should be kept

	// BUGFIX: Extract values from this enum for comparison
	currentEnumValues := extractEnumValuesFromLifecycle(lifecycle)

	// Collect all similar ENUMs including self (but only if they have the SAME values AND same schema)
	allSimilarEnums := []string{typeName}
	for _, categoryObjects := range allLifecycles {
		for _, otherLifecycle := range categoryObjects {
			if otherLifecycle.Key == lifecycle.Key {
				continue // Skip self
			}

			// CRITICAL FIX: Only treat as duplicate if in SAME SCHEMA
			otherSchema := otherLifecycle.Schema
			if typeSchema != otherSchema {
				utils.GetDefaultLogger().WithPrefix("ENUM-DEDUP").Info("ENUM %s.%s has similar name to %s.%s but DIFFERENT schemas - keeping both",
					otherSchema, otherLifecycle.Name, typeSchema, typeName)
				continue
			}

			// Check if other lifecycle is also an ENUM with similar name AND same values
			if (otherLifecycle.Type == types.TypeEnum || otherLifecycle.Type == types.TypeType) &&
				isSimilarEnumName(typeName, otherLifecycle.Name) {

				// BUGFIX: Only treat as duplicate if values match
				otherEnumValues := extractEnumValuesFromLifecycle(otherLifecycle)
				if enumValuesMatch(currentEnumValues, otherEnumValues) {
					conflictingEnums = append(conflictingEnums, *otherLifecycle)
					allSimilarEnums = append(allSimilarEnums, otherLifecycle.Name)
					utils.GetDefaultLogger().WithPrefix("ENUM-DEDUP").Info("Found duplicate ENUM: %s.%s has same values as %s.%s",
						otherSchema, otherLifecycle.Name, typeSchema, typeName)
				} else {
					utils.GetDefaultLogger().WithPrefix("ENUM-DEDUP").Info("ENUM %s.%s has similar name to %s.%s but DIFFERENT values - keeping both",
						otherSchema, otherLifecycle.Name, typeSchema, typeName)
				}
			}
		}
	}

	// Determine the primary ENUM using a deterministic rule (alphabetically first without _enum suffix)
	if len(allSimilarEnums) > 1 {
		// Sort all similar ENUMs
		for i := 0; i < len(allSimilarEnums)-1; i++ {
			for j := i + 1; j < len(allSimilarEnums); j++ {
				// Prefer names without _enum suffix, otherwise alphabetical
				name1 := allSimilarEnums[i]
				name2 := allSimilarEnums[j]

				// Strip common suffixes for comparison
				clean1 := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name1, "_enum"), "_type"), "_status")
				clean2 := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name2, "_type"), "_status"), "_enum")

				// If one is clean and the other has suffix, prefer the clean one
				hasClean1 := clean1 == name1
				hasClean2 := clean2 == name2

				var shouldSwap bool
				if hasClean1 && !hasClean2 {
					shouldSwap = false // name1 is clean, keep it first
				} else if !hasClean1 && hasClean2 {
					shouldSwap = true // name2 is clean, move it first
				} else {
					// Both clean or both have suffix - use alphabetical
					shouldSwap = name1 > name2
				}

				if shouldSwap {
					allSimilarEnums[i], allSimilarEnums[j] = allSimilarEnums[j], allSimilarEnums[i]
				}
			}
		}
		primaryEnum = allSimilarEnums[0]
		utils.GetDefaultLogger().WithPrefix("ENUM-DEDUP").Info("EnumDeduplicationRule: Primary ENUM chosen: %s from candidates %v", primaryEnum, allSimilarEnums)
	} else {
		primaryEnum = typeName
	}

	// If this ENUM has conflicts and is NOT the primary one, skip it entirely
	if len(conflictingEnums) > 0 && lifecycle.Name != primaryEnum {
		// This is a duplicate - eliminate it
		utils.GetDefaultLogger().WithPrefix("ENUM-DEDUP").Info("EnumDeduplicationRule: Eliminating duplicate ENUM %s (keeping %s)", lifecycle.Name, primaryEnum)
		result := &tracking.ConsolidationResult{
			OriginalStatements: enumStmts,
			ConsolidatedSQL:    fmt.Sprintf("-- Duplicate ENUM eliminated: %s (similar to %s)", typeName, primaryEnum),
			Optimizations: []string{
				fmt.Sprintf("Eliminated duplicate ENUM %s (keeping %s)", typeName, primaryEnum),
			},
			Warnings: []string{
				fmt.Sprintf("ENUM %s is a duplicate of %s - eliminated to prevent conflicts", typeName, primaryEnum),
			},
			RiskLevel: tracking.RiskLevelMedium, // Medium risk since we're removing a type
			EstimatedSavings: tracking.SquashSavings{
				StatementsReduced: len(enumStmts),
				FilesAffected:     len(enumStmts),
				LinesReduced:      len(enumStmts) * 5,
			},
		}
		return result, nil
	}

	// This is the earliest/first ENUM - keep it and apply safety-mode specific behavior
	firstEnum := enumStmts[0]
	warnings := []string{}

	// Collect ALTER TYPE ADD VALUE statements
	var alterTypeStmts []types.Statement
	for _, event := range lifecycle.History {
		if event.Statement.Operation == types.OpAlter &&
			event.Statement.ObjectType == types.TypeEnum &&
			event.Statement.AlterTypeNewValue != "" {
			alterTypeStmts = append(alterTypeStmts, event.Statement)
		}
	}

	// Apply safety-mode specific behavior
	consolidatedSQL := firstEnum.SQL

	switch safetyLevel {
	case "paranoid":
		// PARANOID: Preserve exact ALTER TYPE ... ADD VALUE sequence
		// Don't merge anything, keep CREATE TYPE + all ALTER TYPE statements separate
		if len(alterTypeStmts) > 0 {
			var sqlParts []string
			sqlParts = append(sqlParts, firstEnum.SQL)
			for _, stmt := range alterTypeStmts {
				sqlParts = append(sqlParts, stmt.SQL)
			}
			consolidatedSQL = strings.Join(sqlParts, ";\n") + ";"
			warnings = append(warnings, fmt.Sprintf("Paranoid mode: Preserved exact sequence of %d ALTER TYPE statements", len(alterTypeStmts)))
		}

	case "conservative":
		// CONSERVATIVE: Collapse only when final order equals original and all are appends
		if len(alterTypeStmts) > 0 {
			existingValues := extractEnumValuesFromSQL(firstEnum.SQL)
			if len(existingValues) > 0 {
				// Verify all ALTER TYPE statements are appends (no reorders)
				allAppends := true
				newValues := make([]string, len(existingValues))
				copy(newValues, existingValues)

				for _, alterStmt := range alterTypeStmts {
					if alterStmt.AlterTypeNewValue != "" {
						// Check if it's truly an append (not already in list)
						if contains(existingValues, alterStmt.AlterTypeNewValue) {
							allAppends = false
							break
						}
						newValues = append(newValues, alterStmt.AlterTypeNewValue)
					}
				}

				if allAppends {
					// Safe to merge - all are appends
					if mergedSQL, ok := replaceCreateEnumValues(firstEnum.SQL, newValues); ok {
						consolidatedSQL = mergedSQL
						warnings = append(warnings, fmt.Sprintf("Conservative mode: Merged %d append-only ALTER TYPE statements", len(alterTypeStmts)))
					}
				} else {
					// Not safe - preserve sequence
					var sqlParts []string
					sqlParts = append(sqlParts, firstEnum.SQL)
					for _, stmt := range alterTypeStmts {
						sqlParts = append(sqlParts, stmt.SQL)
					}
					consolidatedSQL = strings.Join(sqlParts, ";\n") + ";"
					warnings = append(warnings, "Conservative mode: Preserved ALTER TYPE sequence (detected non-append operations)")
				}
			}
		}

	case "standard", "aggressive":
		// STANDARD/AGGRESSIVE: Merge ALTER TYPE statements into CREATE TYPE
		if len(alterTypeStmts) > 0 {
			existingValues := extractEnumValuesFromSQL(firstEnum.SQL)
			if len(existingValues) > 0 {
				// Add new values from ALTER TYPE statements
				for _, alterStmt := range alterTypeStmts {
					if alterStmt.AlterTypeNewValue != "" && !contains(existingValues, alterStmt.AlterTypeNewValue) {
						existingValues = append(existingValues, alterStmt.AlterTypeNewValue)
					}
				}

				if mergedSQL, ok := replaceCreateEnumValues(firstEnum.SQL, existingValues); ok {
					consolidatedSQL = mergedSQL

					mode := "Standard"
					if safetyLevel == "aggressive" {
						mode = "Aggressive"
					}
					warnings = append(warnings, fmt.Sprintf("%s mode: Merged %d ALTER TYPE ADD VALUE statement(s) into CREATE TYPE", mode, len(alterTypeStmts)))
				}
			}
		}

	default:
		// Fallback to standard behavior
		if len(alterTypeStmts) > 0 {
			existingValues := extractEnumValuesFromSQL(firstEnum.SQL)
			if len(existingValues) > 0 {
				for _, alterStmt := range alterTypeStmts {
					if alterStmt.AlterTypeNewValue != "" && !contains(existingValues, alterStmt.AlterTypeNewValue) {
						existingValues = append(existingValues, alterStmt.AlterTypeNewValue)
					}
				}

				if mergedSQL, ok := replaceCreateEnumValues(firstEnum.SQL, existingValues); ok {
					consolidatedSQL = mergedSQL
					warnings = append(warnings, fmt.Sprintf("Merged %d ALTER TYPE ADD VALUE statement(s) into CREATE TYPE", len(alterTypeStmts)))
				}
			}
		}
	}

	if len(enumStmts) > 1 {
		warnings = append(warnings, fmt.Sprintf("Found %d definitions for ENUM %s, using first definition", len(enumStmts), typeName))
	}

	if len(conflictingEnums) > 0 {
		var conflictNames []string
		for _, c := range conflictingEnums {
			conflictNames = append(conflictNames, c.Name)
		}
		warnings = append(warnings, fmt.Sprintf("ENUM %s is the primary definition (duplicates eliminated: %v)", typeName, conflictNames))
	}

	// Include ALTER TYPE statements in original statements if any were merged
	originalStmts := enumStmts
	if len(alterTypeStmts) > 0 {
		originalStmts = append(originalStmts, alterTypeStmts...)
	}

	optimizations := []string{}
	if len(enumStmts) > 1 {
		optimizations = append(optimizations, fmt.Sprintf("Deduplicated %d ENUM definition(s) for %s", len(enumStmts)-1, typeName))
	}
	if len(alterTypeStmts) > 0 {
		optimizations = append(optimizations, fmt.Sprintf("Merged %d ALTER TYPE ADD VALUE statement(s) into CREATE TYPE", len(alterTypeStmts)))
	}

	result := &tracking.ConsolidationResult{
		OriginalStatements: originalStmts,
		ConsolidatedSQL:    consolidatedSQL,
		Optimizations:      optimizations,
		Warnings:           warnings,
		RiskLevel:          tracking.RiskLevelMedium, // Medium risk since we're choosing one definition
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(enumStmts) - 1 + len(alterTypeStmts),
			FilesAffected:     len(enumStmts) + len(alterTypeStmts),
			LinesReduced:      ((len(enumStmts) - 1) + len(alterTypeStmts)) * 5,
		},
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *EnumDeduplicationRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelMedium
}

// isSimilarEnumName checks if two ENUM names are similar enough to be duplicates
// Examples: "verification_status" and "verification_status_enum" are similar
func isSimilarEnumName(name1, name2 string) bool {
	n1 := strings.ToLower(name1)
	n2 := strings.ToLower(name2)

	// Exact match
	if n1 == n2 {
		return true
	}

	// One is a prefix of the other
	if strings.HasPrefix(n1, n2) || strings.HasPrefix(n2, n1) {
		return true
	}

	// Check for common suffix patterns (_enum, _type)
	suffixes := []string{"_enum", "_type", "_status"}
	for _, suffix := range suffixes {
		n1WithoutSuffix := strings.TrimSuffix(n1, suffix)
		n2WithoutSuffix := strings.TrimSuffix(n2, suffix)

		if n1WithoutSuffix == n2WithoutSuffix {
			return true
		}
	}

	return false
}

// parseEnumValues extracts individual enum values from a comma-separated list
// Input: "'active', 'inactive', 'suspended'"
// Output: ["active", "inactive", "suspended"]
func parseEnumValues(valuesStr string) []string {
	var values []string
	if valuesStr == "" {
		return values
	}

	inQuote := false
	var current strings.Builder

	for i := 0; i < len(valuesStr); i++ {
		ch := valuesStr[i]

		if !inQuote {
			if ch == '\'' {
				inQuote = true
				current.Reset()
			}
			continue
		}

		if ch == '\'' {
			if i+1 < len(valuesStr) && valuesStr[i+1] == '\'' {
				current.WriteByte('\'')
				i++
				continue
			}

			values = append(values, current.String())
			inQuote = false
			continue
		}

		current.WriteByte(ch)
	}

	return values
}

// quoteEnumValues wraps each value in single quotes for SQL
// Input: ["active", "inactive"]
// Output: ["'active'", "'inactive'"]
func quoteEnumValues(values []string) []string {
	quoted := make([]string, len(values))
	for i, v := range values {
		quoted[i] = fmt.Sprintf("'%s'", v)
	}
	return quoted
}

// contains checks if a string slice contains a specific value
func contains(slice []string, value string) bool {
	return slices.Contains(slice, value)
}

// extractEnumValuesFromLifecycle extracts enum values from a lifecycle's SQL
func extractEnumValuesFromLifecycle(lifecycle *tracking.ObjectLifecycle) []string {
	for _, event := range lifecycle.History {
		if values := extractEnumValuesFromSQL(event.Statement.SQL); len(values) > 0 {
			return values
		}
	}
	return []string{}
}

func isCreateEnumStatement(sql string) bool {
	_, _, ok := findEnumValuesSpan(sql)
	return ok
}

func extractEnumValuesFromSQL(sql string) []string {
	start, end, ok := findEnumValuesSpan(sql)
	if !ok || start >= end || start < 0 || end > len(sql) {
		return nil
	}

	return parseEnumValues(sql[start:end])
}

func replaceCreateEnumValues(sql string, values []string) (string, bool) {
	start, end, ok := findEnumValuesSpan(sql)
	if !ok || start > end || start < 0 || end > len(sql) {
		return "", false
	}

	replacement := strings.Join(quoteEnumValues(values), ", ")
	return sql[:start] + replacement + sql[end:], true
}

func findEnumValuesSpan(sql string) (int, int, bool) {
	if sql == "" {
		return 0, 0, false
	}

	asEnumIndex := findKeywordSequenceIndexCI(sql, "AS", "ENUM")
	if asEnumIndex < 0 {
		return 0, 0, false
	}

	openParen := -1
	for i := asEnumIndex; i < len(sql); i++ {
		if sql[i] == '(' {
			openParen = i
			break
		}
	}
	if openParen == -1 {
		return 0, 0, false
	}

	closeParen := findMatchingParenWithQuotes(sql, openParen)
	if closeParen == -1 || closeParen <= openParen {
		return 0, 0, false
	}

	return openParen + 1, closeParen, true
}

func findKeywordSequenceIndexCI(sql string, words ...string) int {
	if len(words) == 0 || sql == "" {
		return -1
	}

	for i := 0; i < len(sql); i++ {
		if !isWordBoundaryStart(sql, i) {
			continue
		}

		index := i
		matched := true
		for _, word := range words {
			index = skipWhitespaceBytes(sql, index)
			if index+len(word) > len(sql) || !strings.EqualFold(sql[index:index+len(word)], word) {
				matched = false
				break
			}

			end := index + len(word)
			if end < len(sql) && isIdentifierByte(sql[end]) {
				matched = false
				break
			}

			index = end
		}

		if matched {
			return i
		}
	}

	return -1
}

func findMatchingParenWithQuotes(sql string, openIndex int) int {
	if openIndex < 0 || openIndex >= len(sql) || sql[openIndex] != '(' {
		return -1
	}

	depth := 0
	inSingleQuote := false

	for i := openIndex; i < len(sql); i++ {
		ch := sql[i]

		if inSingleQuote {
			if ch == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					i++
					continue
				}
				inSingleQuote = false
			}
			continue
		}

		switch ch {
		case '\'':
			inSingleQuote = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}

	return -1
}

func isWordBoundaryStart(sql string, index int) bool {
	if index <= 0 {
		return true
	}
	return !isIdentifierByte(sql[index-1])
}

func skipWhitespaceBytes(sql string, index int) int {
	for index < len(sql) {
		switch sql[index] {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			index++
		default:
			return index
		}
	}
	return index
}

func isIdentifierByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

// enumValuesMatch checks if two enum value lists are the same (order-independent)
func enumValuesMatch(values1, values2 []string) bool {
	if len(values1) != len(values2) {
		return false
	}

	// Create a map of values from first list
	valueMap := make(map[string]bool)
	for _, v := range values1 {
		valueMap[strings.ToLower(strings.TrimSpace(v))] = true
	}

	// Check if all values from second list exist in map
	for _, v := range values2 {
		normalized := strings.ToLower(strings.TrimSpace(v))
		if !valueMap[normalized] {
			return false
		}
	}

	return true
}
