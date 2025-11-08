package postprocessing

import (
	"regexp"

	"github.com/capysquash/pgsquash-engine/internal/config"
	"github.com/capysquash/pgsquash-engine/internal/postprocessing/ast"
	"github.com/capysquash/pgsquash-engine/internal/utils"
)

// Processor orchestrates all post-processing operations on consolidated SQL.
// It applies fixes in a specific order to ensure correctness.
type Processor struct {
	logger              *utils.Logger
	config              *config.Config
	functionNormalizer  *ast.FunctionNormalizer
	enumReplacer        *ast.EnumReplacer
	useASTProcessing    bool // Feature flag for AST-based processing
}

// NewProcessor creates a new post-processing processor with the given configuration.
func NewProcessor(cfg *config.Config) *Processor {
	return &Processor{
		logger:             utils.GetDefaultLogger().WithPrefix("POSTPROCESS"),
		config:             cfg,
		functionNormalizer: ast.NewFunctionNormalizer(),
		enumReplacer:       ast.NewEnumReplacer(nil), // Will set replacements later
		useASTProcessing:   true, // Enable AST processing by default
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
	jwtPattern := regexp.MustCompile(`(?i)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+auth\.jwt\(\)[^\$]*AS\s+\$\$`)
	if match := jwtPattern.FindString(sql); match != "" {
		p.logger.Info("BEFORE POST-PROCESSING - auth.jwt() signature: %s", match)
	}

	// Try AST-based normalization first (more accurate)
	if p.useASTProcessing {
		p.logger.Info("=== Attempting AST-based function normalization ===")
		normalizedSQL, err := p.functionNormalizer.NormalizeAll(sql)
		if err == nil && normalizedSQL != "" {
			p.logger.Info("✓ AST-based function normalization succeeded")
			sql = normalizedSQL
			if match := jwtPattern.FindString(sql); match != "" {
				p.logger.Info("AFTER AST NORMALIZATION - auth.jwt() signature: %s", match)
			}
		} else {
			p.logger.Info("⚠ AST-based normalization failed (%v), falling back to regex", err)
			// Fall through to regex-based approach
		}
	}

	// Regex-based normalization DISABLED: It was creating duplicate LANGUAGE clauses
	// and corrupting valid SQL. AST-based normalization is preferred.
	if !p.useASTProcessing {
		p.logger.Info("=== Regex-based function normalization DISABLED ===")
		p.logger.Info("Reason: Creates duplicate LANGUAGE clauses and corrupts valid SQL")
		p.logger.Info("Recommendation: Use AST-based processing or fix at consolidation layer")
	}

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

	// Fix incorrect LANGUAGE declarations (e.g., LANGUAGE SQL for functions with BEGIN/END)
	// This is critical because AST normalization might add the wrong LANGUAGE
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
	// PHASE 5: Statement Formatting (BUG #2 FIX)
	// ================================================================
	// Add proper spacing and formatting between statements to fix
	// Bug #2 where multiple CREATE FUNCTION statements are concatenated
	// without line breaks, making output unreadable
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
		// Pattern: Column type declarations in CREATE TABLE
		// Example: status verification_status_enum DEFAULT
		pattern := regexp.MustCompile(`\b` + regexp.QuoteMeta(eliminatedName) + `\b`)

		// Count matches before replacement
		matches := pattern.FindAllStringIndex(fixedSQL, -1)
		if len(matches) > 0 {
			fixedSQL = pattern.ReplaceAllString(fixedSQL, primaryName)
			fixedCount += len(matches)
			logger.Info("Replaced %d reference(s) to %s with %s", len(matches), eliminatedName, primaryName)
		}
	}

	if fixedCount > 0 {
		logger.Info("Fixed %d ENUM references to eliminated types", fixedCount)
	}

	return fixedSQL
}
