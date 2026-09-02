package postprocessing

import (
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/config"
	"github.com/capysquash/pgsquash-engine/internal/postprocessing/ast"
	"github.com/capysquash/pgsquash-engine/internal/utils"
)

// Processor orchestrates all post-processing operations on consolidated SQL.
// It applies fixes in a specific order to ensure correctness.
type Processor struct {
	logger           *utils.Logger
	config           *config.Config
	enumReplacer     *ast.EnumReplacer
	useASTProcessing bool // Feature flag for AST-based processing
}

// NewProcessor creates a new post-processing processor with the given configuration.
func NewProcessor(cfg *config.Config) *Processor {
	return &Processor{
		logger:           utils.GetDefaultLogger().WithPrefix("POSTPROCESS"),
		config:           cfg,
		enumReplacer:     ast.NewEnumReplacer(nil), // Will set replacements later
		useASTProcessing: true,                     // Enable AST processing by default
	}
}

// Apply runs all post-processing fixes in the correct order.
// The order is critical and should not be changed without careful consideration.
//
// Processing Pipeline:
//  1. Basic Syntax Fixes - Fix malformed SQL constructs
//  2. Extension Ordering - Ensure extension dependencies are correct
//  3. Function Language Normalization - Fix LANGUAGE clause issues
//  4. Function Body Fixes - Fix RETURN NEXT patterns
//  5. Final Cleanup - Add missing semicolons and fix enum references
//
// Each phase builds on the previous one, so order matters.
func (p *Processor) Apply(sql string, enumReplacements map[string]string) (string, error) {
	p.logger.Info("Starting post-processing pipeline...")

	// ================================================================
	// PHASE 1: Basic Syntax Fixes
	// ================================================================
	p.logger.Info("Phase 1: Basic syntax fixes")
	sql = FixMalformedDropTriggers(sql)
	sql = FixExtensionOrder(sql)
	sql = RemoveOrphanedAlterStatements(sql)
	sql = FixMalformedFunctions(sql)

	// ================================================================
	// PHASE 2: Function Language Normalization
	// ================================================================
	p.logger.Info("Phase 2: Function language normalization")

	// Debug: Check auth.jwt() function before post-processing
	if match := extractFunctionSignatureSnippet(sql, "auth.jwt()"); match != "" {
		p.logger.Info("BEFORE POST-PROCESSING - auth.jwt() signature: %s", match)
	}

	// AST-based function normalization DISABLED: Package removed due to corruption issues
	// The function_normalizer package was creating duplicate LANGUAGE clauses and corrupting valid SQL.
	// Function normalization is now handled at the consolidation layer instead.
	p.logger.Info("=== AST-based function normalization DISABLED ===")
	p.logger.Info("Reason: Function normalizer package removed - was corrupting SQL")
	p.logger.Info("Note: Function normalization handled at consolidation layer")

	// ================================================================
	// PHASE 3: Function Body Fixes
	// ================================================================
	p.logger.Info("Phase 3: Function body fixes")
	sql = FixReturnNextWithOutParams(sql, nil)

	// Remove duplicate trailing LANGUAGE clauses (after $$;)
	// This happens when AST normalization adds LANGUAGE before AS $$
	// but the original has language 'plpgsql'; after $$;
	p.logger.Info("Removing duplicate trailing LANGUAGE clauses")
	sql = FixRedundantTrailingLanguageClauses(sql)

	p.logger.Info("Fixing incorrect LANGUAGE declarations (SQL → plpgsql for functions with plpgsql constructs)")
	sql = FixIncorrectLanguageDeclarations(sql)

	// Add missing LANGUAGE declarations
	// This handles functions where AST normalization didn't add LANGUAGE at all
	p.logger.Info("Adding missing LANGUAGE declarations (inferred from function body)")
	sql = FixMissingLanguageDeclarations(sql)

	// ================================================================
	// PHASE 4: Final Cleanup
	// ================================================================
	p.logger.Info("Phase 4: Final cleanup")
	// FixMissingSemicolons DISABLED: SQLBuilder handles semicolons correctly.
	// This function was too aggressive and corrupted valid SQL.
	// sql = FixMissingSemicolons(sql)
	p.logger.Info("FixMissingSemicolons disabled - relying on SQLBuilder for correct semicolons")

	// Handle ENUM reference replacement
	if len(enumReplacements) > 0 {
		// Try AST-based ENUM replacement first
		if p.useASTProcessing {
			p.logger.Info("=== Attempting AST-based ENUM replacement ===")
			p.enumReplacer = ast.NewEnumReplacer(enumReplacements)
			fixedSQL, err := p.enumReplacer.ReplaceEnumReferences(sql)
			if err == nil && fixedSQL != "" {
				p.logger.Info("✓ AST-based ENUM replacement succeeded")
				sql = fixedSQL
			} else {
				p.logger.Info("⚠ AST-based ENUM replacement failed (%v), falling back to regex", err)
				sql = fixEliminatedEnumReferences(sql, enumReplacements)
			}
		} else {
			// Fallback to regex-based approach
			sql = fixEliminatedEnumReferences(sql, enumReplacements)
		}
	}

	// ================================================================
	// PHASE 5: Statement Formatting
	// ================================================================
	p.logger.Info("Phase 5: Statement formatting (adding proper spacing)")
	sql = EnsureStatementSpacing(sql)

	p.logger.Info("Post-processing pipeline completed")
	return sql, nil
}

// fixEliminatedEnumReferences rewrites references to eliminated ENUM types.
// This is called as part of the post-processing pipeline when ENUM consolidation
// has occurred and we need to update references to point to the primary ENUM.
func fixEliminatedEnumReferences(sql string, enumReplacements map[string]string) string {
	if len(enumReplacements) == 0 {
		return sql // No ENUMs were eliminated
	}

	logger := utils.GetDefaultLogger().WithPrefix("POSTPROCESS")

	// Rewrite all references to eliminated ENUMs
	fixedSQL := sql
	fixedCount := 0

	for eliminatedName, primaryName := range enumReplacements {
		replaced, count := replaceWholeWord(fixedSQL, eliminatedName, primaryName)
		if count > 0 {
			fixedSQL = replaced
			fixedCount += count
			logger.Info("Replaced %d reference(s) to %s with %s", count, eliminatedName, primaryName)
		}
	}

	if fixedCount > 0 {
		logger.Info("Fixed %d ENUM references to eliminated types", fixedCount)
	}

	return fixedSQL
}

func extractFunctionSignatureSnippet(sql string, functionName string) string {
	if strings.TrimSpace(sql) == "" || strings.TrimSpace(functionName) == "" {
		return ""
	}

	lower := strings.ToLower(sql)
	fnLower := strings.ToLower(functionName)
	fnIdx := strings.Index(lower, fnLower)
	if fnIdx == -1 {
		return ""
	}

	createIdx := strings.LastIndex(lower[:fnIdx], "create")
	if createIdx == -1 {
		createIdx = fnIdx
	}

	asIdx := strings.Index(lower[fnIdx:], "as $$")
	if asIdx == -1 {
		end := min(fnIdx+120, len(sql))
		return strings.TrimSpace(sql[createIdx:end])
	}

	asIdx += fnIdx + len("as $$")
	if asIdx > len(sql) {
		asIdx = len(sql)
	}

	return strings.TrimSpace(sql[createIdx:asIdx])
}

func replaceWholeWord(input string, target string, replacement string) (string, int) {
	if input == "" || target == "" {
		return input, 0
	}

	var out strings.Builder
	out.Grow(len(input))

	count := 0
	cursor := 0

	for {
		rel := strings.Index(input[cursor:], target)
		if rel == -1 {
			out.WriteString(input[cursor:])
			break
		}

		idx := cursor + rel
		end := idx + len(target)

		beforeOK := idx == 0 || !isWordByte(input[idx-1])
		afterOK := end >= len(input) || !isWordByte(input[end])

		if beforeOK && afterOK {
			out.WriteString(input[cursor:idx])
			out.WriteString(replacement)
			cursor = end
			count++
			continue
		}

		out.WriteString(input[cursor : idx+1])
		cursor = idx + 1
	}

	return out.String(), count
}

func isWordByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_'
}
