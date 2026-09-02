package postprocessing

import (
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/config"
	"github.com/capy-base/pgsquash-engine/internal/postprocessing/ast"
	"github.com/capy-base/pgsquash-engine/internal/utils"
)

// ProcessorAST orchestrates AST-based post-processing operations.
// This is an alternative to regex-based processing with better accuracy and maintainability.
type ProcessorAST struct {
	logger         *utils.Logger
	config         *config.Config
	useASTForEnums bool
}

// NewProcessorAST creates a new AST-based post-processing processor.
func NewProcessorAST(cfg *config.Config) *ProcessorAST {
	return &ProcessorAST{
		logger:         utils.GetDefaultLogger().WithPrefix("POSTPROCESS-AST"),
		config:         cfg,
		useASTForEnums: true, // Enable by default
	}
}

// SetUseASTForEnums enables or disables AST-based ENUM processing.
func (p *ProcessorAST) SetUseASTForEnums(use bool) {
	p.useASTForEnums = use
}

// Apply runs all post-processing fixes using AST where possible, falling back to regex.
// This provides a migration path from regex to AST.
func (p *ProcessorAST) Apply(sql string, enumReplacements map[string]string) (string, error) {
	p.logger.Info("Starting AST-based post-processing pipeline...")

	// ================================================================
	// PHASE 1: Basic Syntax Fixes (Keep regex - operates on malformed SQL)
	// ================================================================
	p.logger.Info("Phase 1: Basic syntax fixes (regex)")
	sql = FixMalformedDropTriggers(sql)
	sql = FixExtensionOrder(sql)
	sql = RemoveOrphanedAlterStatements(sql)
	sql = FixMalformedFunctions(sql)
	sql = FixMissingLanguageClauses(sql) // Add LANGUAGE plpgsql where missing

	// ================================================================
	// PHASE 2: Function Language Normalization (regex)
	// ================================================================
	// Note: AST-based function normalization removed - package was corrupting SQL
	// Function normalization now handled via regex-based fixes
	p.logger.Info("Phase 2: Function language normalization (regex)")
	sql = p.applyRegexFunctionFixes(sql)

	// ================================================================
	// PHASE 3: Function Body Fixes (Keep regex for now)
	// ================================================================
	p.logger.Info("Phase 3: Function body fixes (regex)")
	sql = FixReturnNextWithOutParams(sql, nil)

	// ================================================================
	// PHASE 4: Final Cleanup
	// ================================================================
	p.logger.Info("Phase 4: Final cleanup")
	// FixMissingSemicolons DISABLED: Too aggressive, corrupts valid SQL inside function bodies.
	// SQLBuilder and AST deparser already handle semicolons correctly.
	// sql = FixMissingSemicolons(sql)
	p.logger.Info("FixMissingSemicolons disabled - relying on SQLBuilder and AST deparser for correct semicolons")

	// ENUM replacement (AST or regex)
	if len(enumReplacements) > 0 {
		if p.useASTForEnums {
			p.logger.Info("Phase 4: ENUM replacement (AST)")
			enumReplacer := ast.NewEnumReplacer(enumReplacements)
			replacedSQL, err := enumReplacer.ReplaceEnumReferences(sql)
			if err != nil {
				p.logger.Info("AST ENUM replacement failed, falling back to regex: %v", err)
				sql = fixEliminatedEnumReferences(sql, enumReplacements)
			} else {
				sql = replacedSQL
				p.logger.Info("AST replaced %d ENUM references", enumReplacer.GetReplacedCount())
			}
		} else {
			p.logger.Info("Phase 4: ENUM replacement (regex)")
			sql = fixEliminatedEnumReferences(sql, enumReplacements)
		}
	}
	p.logger.Info("Phase 5: Fixing pg_query deparser corruption bugs")

	// It outputs: ) RETURNS TABLE LANGUAGE plpgsql (columns...)
	// Correct:     ) RETURNS TABLE (columns...) LANGUAGE plpgsql
	// This causes syntax errors "syntax error at or near LANGUAGE"
	beforeFix := sql
	var fixCount int
	sql, fixCount = fixMisplacedLanguageAfterReturnsTable(sql)
	if sql != beforeFix {
		p.logger.Info("Fixed pg_query deparser bug: removed %d misplaced LANGUAGE clauses from RETURNS TABLE", fixCount)
	}

	// Example: char_length() becomes char_char_length()
	// Fix pg_query deparser bug where DROP POLICY object name includes full qualification
	// Incorrect: DROP POLICY IF EXISTS schema.table.policy ON schema.table
	// Correct:   DROP POLICY IF EXISTS policy ON schema.table
	if strings.Contains(sql, "DROP POLICY") {
		beforeFix := sql
		sql = FixDropPolicyDeparseCorruption(sql)
		if sql != beforeFix {
			p.logger.Info("Fixed pg_query deparser bug: corrected schema-qualified policy name in DROP POLICY")
		}
	}

	p.logger.Info("AST-based post-processing pipeline completed")
	return sql, nil
}

// applyRegexFunctionFixes applies regex-based function fixes (fallback).
func (p *ProcessorAST) applyRegexFunctionFixes(sql string) string {
	p.logger.Info("=== Applying regex-based function fixes ===")
	sql = FixRedundantTrailingLanguageClauses(sql)
	sql = FixIncorrectLanguageDeclarations(sql)
	sql = FixMissingLanguageDeclarations(sql)
	sql = FixFunctionLanguageConflicts(sql)
	return sql
}

func fixMisplacedLanguageAfterReturnsTable(sql string) (string, int) {
	if sql == "" {
		return sql, 0
	}

	upper := strings.ToUpper(sql)
	cursor := 0
	fixedCount := 0
	var out strings.Builder

	for {
		idx := strings.Index(upper[cursor:], "RETURNS TABLE")
		if idx == -1 {
			out.WriteString(sql[cursor:])
			break
		}

		idx += cursor
		out.WriteString(sql[cursor:idx])
		out.WriteString(sql[idx : idx+len("RETURNS TABLE")])

		pos := idx + len("RETURNS TABLE")
		wsStart := pos
		for pos < len(sql) && isPostprocessWhitespace(sql[pos]) {
			pos++
		}

		if !hasPostprocessKeywordAt(sql, pos, "LANGUAGE") {
			out.WriteString(sql[wsStart:pos])
			cursor = pos
			continue
		}

		langStart := wsStart
		pos += len("LANGUAGE")
		for pos < len(sql) && isPostprocessWhitespace(sql[pos]) {
			pos++
		}

		langNameStart := pos
		for pos < len(sql) && isPostprocessIdentifierByte(sql[pos]) {
			pos++
		}
		if langNameStart == pos {
			// malformed LANGUAGE clause, keep original
			out.WriteString(sql[langStart:pos])
			cursor = pos
			continue
		}

		afterLang := pos
		for afterLang < len(sql) && isPostprocessWhitespace(sql[afterLang]) {
			afterLang++
		}

		if afterLang < len(sql) && sql[afterLang] == '(' {
			// Remove misplaced LANGUAGE clause entirely and keep a single space before '(' for readability.
			out.WriteByte(' ')
			fixedCount++
			cursor = afterLang
			continue
		}

		// Not the broken shape; keep original segment.
		out.WriteString(sql[langStart:afterLang])
		cursor = afterLang
	}

	return out.String(), fixedCount
}

func hasPostprocessKeywordAt(sql string, pos int, keyword string) bool {
	if pos < 0 || pos+len(keyword) > len(sql) {
		return false
	}

	if !strings.EqualFold(sql[pos:pos+len(keyword)], keyword) {
		return false
	}

	if pos > 0 && isPostprocessIdentifierByte(sql[pos-1]) {
		return false
	}

	end := pos + len(keyword)
	if end < len(sql) && isPostprocessIdentifierByte(sql[end]) {
		return false
	}

	return true
}

func isPostprocessWhitespace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}

func isPostprocessIdentifierByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}
