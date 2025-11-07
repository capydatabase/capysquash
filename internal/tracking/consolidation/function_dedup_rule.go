package consolidation

import (
	"fmt"
	"strings"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/utils"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/tracking"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/types"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/errors"
)

// FunctionDeduplicationRule consolidates duplicate function definitions
type FunctionDeduplicationRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *FunctionDeduplicationRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	if lifecycle.Type != types.TypeFunction {
		return false
	}

	// Check for multiple CREATE operations with identical function signatures
	createCount := 0
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			createCount++
		}
	}

	return createCount > 1
}

// Apply applies the consolidation rule to the given lifecycle
func (r *FunctionDeduplicationRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]interface{}{"rule": "FunctionDedupRule"})
	}

	var latestCreate *types.Statement
	var duplicateStmts []types.Statement

	// Find the latest CREATE statement and collect duplicates
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			if latestCreate != nil {
				duplicateStmts = append(duplicateStmts, *latestCreate)
			}
			latestCreate = &event.Statement
		}
	}

	if latestCreate == nil {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "no CREATE statement found", map[string]interface{}{"object": lifecycle.Name})
	}

	// BUG #2 FIX: Use original SQL directly from latest CREATE statement
	// Multi-version functions should preserve the exact SQL from the latest definition
	// without any extraction, reconstruction, or deparser round-trips that can corrupt:
	// - LANGUAGE placement (before AS vs after body)
	// - LANGUAGE type (sql vs plpgsql)
	// - Volatility markers (STABLE, IMMUTABLE, VOLATILE)
	// - Security markers (SECURITY DEFINER)
	// - Function body quoting
	consolidatedSQL := latestCreate.SQL

	utils.GetDefaultLogger().WithPrefix("FUNCTION-DEDUP").Info("Preserving original SQL for multi-version function: %s (length=%d)",
		lifecycle.Name, len(consolidatedSQL))

	result := &tracking.ConsolidationResult{
		OriginalStatements: append(duplicateStmts, *latestCreate),
		ConsolidatedSQL:    consolidatedSQL,
		Optimizations: []string{
			fmt.Sprintf("Removed %d duplicate function definitions", len(duplicateStmts)),
		},
		RiskLevel: tracking.RiskLevelLow,
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(duplicateStmts),
			FilesAffected:     len(duplicateStmts) + 1,
			LinesReduced:      len(duplicateStmts) * 5, // Estimate based on function size
		},
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *FunctionDeduplicationRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow
}

// Helper functions for function deduplication

// extractFirstCompleteFunction extracts the first complete function definition from concatenated SQL
func extractFirstCompleteFunction(concatenatedSQL string, functionName string) string {
	utils.GetDefaultLogger().WithPrefix("FUNCTION-DEDUP").Info("Extracting first complete function from concatenated SQL for: %s", functionName)

	// Pattern: CREATE [OR REPLACE] FUNCTION ... AS $...$ LANGUAGE ... ;
	// We need to extract everything up to and including the first complete function

	// Find the first CREATE FUNCTION
	firstCreateIdx := strings.Index(strings.ToUpper(concatenatedSQL), "CREATE")
	if firstCreateIdx == -1 {
		utils.GetDefaultLogger().WithPrefix("FUNCTION-DEDUP").Info("  ERROR: No CREATE keyword found in concatenated SQL")
		return concatenatedSQL
	}

	// Find the AS keyword (start of function body)
	asIdx := strings.Index(strings.ToUpper(concatenatedSQL[firstCreateIdx:]), " AS ")
	if asIdx == -1 {
		utils.GetDefaultLogger().WithPrefix("FUNCTION-DEDUP").Info("  ERROR: No AS keyword found after CREATE FUNCTION")
		return concatenatedSQL
	}
	asIdx += firstCreateIdx

	// Find the function body delimiter ($ or $$)
	bodyStart := asIdx + 4 // Skip " AS "
	var delimiter string
	if strings.HasPrefix(concatenatedSQL[bodyStart:], "$$") {
		delimiter = "$$"
	} else if strings.HasPrefix(concatenatedSQL[bodyStart:], "$") {
		delimiter = "$"
	} else {
		utils.GetDefaultLogger().WithPrefix("FUNCTION-DEDUP").Info("  ERROR: No $ delimiter found after AS")
		return concatenatedSQL
	}

	// Find the closing delimiter
	bodyStartAfterDelim := bodyStart + len(delimiter)
	closingDelimIdx := strings.Index(concatenatedSQL[bodyStartAfterDelim:], delimiter)
	if closingDelimIdx == -1 {
		utils.GetDefaultLogger().WithPrefix("FUNCTION-DEDUP").Info("  ERROR: No closing %s delimiter found", delimiter)
		return concatenatedSQL
	}
	closingDelimIdx += bodyStartAfterDelim + len(delimiter)

	// Find LANGUAGE clause after the closing delimiter
	afterBodySQL := concatenatedSQL[closingDelimIdx:]
	languageIdx := strings.Index(strings.ToUpper(afterBodySQL), "LANGUAGE")
	if languageIdx == -1 {
		utils.GetDefaultLogger().WithPrefix("FUNCTION-DEDUP").Info("  WARNING: No LANGUAGE clause found after function body, using up to closing delimiter")
		return concatenatedSQL[firstCreateIdx:closingDelimIdx] + ";"
	}

	// Find the end of the LANGUAGE clause (either semicolon or next CREATE keyword)
	languageStart := closingDelimIdx + languageIdx
	afterLanguage := concatenatedSQL[languageStart:]

	// Find the end of the LANGUAGE line (language name is next word after LANGUAGE)
	words := strings.Fields(afterLanguage)
	if len(words) < 2 {
		utils.GetDefaultLogger().WithPrefix("FUNCTION-DEDUP").Info("  ERROR: Malformed LANGUAGE clause")
		return concatenatedSQL[firstCreateIdx:languageStart+7] + " plpgsql;" // Add default
	}

	// LANGUAGE clause: "LANGUAGE plpgsql" or "LANGUAGE plpgsql SECURITY DEFINER"
	// Find the end: semicolon or next CREATE keyword
	languageClauseEnd := languageStart + len("LANGUAGE") + 1 + len(words[1])

	// Check for additional modifiers after language (SECURITY DEFINER, etc.)
	remainingAfterLang := concatenatedSQL[languageClauseEnd:]
	if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(remainingAfterLang)), "SECURITY DEFINER") {
		languageClauseEnd += strings.Index(strings.ToUpper(remainingAfterLang), "SECURITY DEFINER") + len("SECURITY DEFINER")
	} else if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(remainingAfterLang)), "STABLE") {
		languageClauseEnd += strings.Index(strings.ToUpper(remainingAfterLang), "STABLE") + len("STABLE")
	} else if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(remainingAfterLang)), "VOLATILE") {
		languageClauseEnd += strings.Index(strings.ToUpper(remainingAfterLang), "VOLATILE") + len("VOLATILE")
	} else if strings.HasPrefix(strings.TrimSpace(strings.ToUpper(remainingAfterLang)), "IMMUTABLE") {
		languageClauseEnd += strings.Index(strings.ToUpper(remainingAfterLang), "IMMUTABLE") + len("IMMUTABLE")
	}

	// Find the end of the function (semicolon or next CREATE)
	afterClause := concatenatedSQL[languageClauseEnd:]
	semicolonIdx := strings.Index(afterClause, ";")
	nextCreateIdx := strings.Index(strings.ToUpper(afterClause), "CREATE")

	var functionEnd int
	if semicolonIdx != -1 && (nextCreateIdx == -1 || semicolonIdx < nextCreateIdx) {
		// Semicolon comes first or there's no next CREATE
		functionEnd = languageClauseEnd + semicolonIdx + 1
	} else if nextCreateIdx != -1 {
		// Next CREATE comes first
		functionEnd = languageClauseEnd + nextCreateIdx
	} else {
		// No semicolon or next CREATE, use end of LANGUAGE clause
		functionEnd = languageClauseEnd
		concatenatedSQL = concatenatedSQL[:functionEnd] + ";" // Add semicolon
		functionEnd++
	}

	extractedSQL := strings.TrimSpace(concatenatedSQL[firstCreateIdx:functionEnd])
	utils.GetDefaultLogger().WithPrefix("FUNCTION-DEDUP").Info("  Successfully extracted first function (%d chars from %d total)", len(extractedSQL), len(concatenatedSQL))

	return extractedSQL
}
