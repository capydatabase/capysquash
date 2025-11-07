package postprocessing

import (
	"fmt"
	"regexp"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// StatementFormatter provides AST-aware SQL statement formatting
// to ensure proper spacing, line breaks, and readability
type StatementFormatter struct {
	indentSize int
}

// NewStatementFormatter creates a new statement formatter
func NewStatementFormatter() *StatementFormatter {
	return &StatementFormatter{
		indentSize: 2,
	}
}

// FormatSQL adds proper spacing and newlines between SQL statements
// This fixes Bug #2 where multiple CREATE FUNCTION statements are concatenated
// without proper formatting, making output unreadable
func (f *StatementFormatter) FormatSQL(sql string) string {
	if sql == "" {
		return sql
	}

	// Try AST-based formatting first (most accurate)
	if formatted, err := f.formatWithAST(sql); err == nil {
		return formatted
	}

	// Fallback to regex-based formatting if AST fails
	return f.formatWithRegex(sql)
}

// formatWithAST uses PostgreSQL parser to properly format statements
func (f *StatementFormatter) formatWithAST(sql string) (string, error) {
	// BUG #2 FIX: DO NOT use formatWithAST - it calls pg_query.Deparse() which corrupts functions
	// Instead, fall back to regex-based formatting which preserves original SQL
	//
	// The issue: pg_query.Deparse() doesn't preserve:
	// - LANGUAGE placement (before AS vs after body)
	// - LANGUAGE type (sql vs plpgsql)
	// - Volatility markers (STABLE, IMMUTABLE, VOLATILE)
	// - Security markers (SECURITY DEFINER)
	// - Function body quoting and formatting
	//
	// Since we now preserve original SQL in consolidation rules (rule.go, function_dedup_rule.go),
	// we must NOT deparse it here or we'll corrupt it again.
	return "", fmt.Errorf("AST-based formatting disabled to prevent function corruption")
}

// formatWithRegex uses regex patterns to add formatting when AST parsing fails
func (f *StatementFormatter) formatWithRegex(sql string) string {
	// Pattern 1: Add blank lines between CREATE FUNCTION statements
	// Match: $$; followed immediately by CREATE (without proper spacing)
	functionPattern := regexp.MustCompile(`\$\$\s*;\s*CREATE\s+(OR\s+REPLACE\s+)?FUNCTION`)
	sql = functionPattern.ReplaceAllString(sql, "$$;\n\nCREATE ${1}FUNCTION")

	// Pattern 2: Add blank lines between CREATE TABLE statements
	tablePattern := regexp.MustCompile(`;\s*CREATE\s+(TABLE|INDEX|TRIGGER|POLICY|VIEW)`)
	sql = tablePattern.ReplaceAllString(sql, ";\n\nCREATE $1")

	// Pattern 3: Fix missing space after semicolons (but not in function bodies)
	// Only match semicolons that are followed by uppercase keywords (statement boundaries)
	semicolonPattern := regexp.MustCompile(`;([A-Z])`)
	sql = semicolonPattern.ReplaceAllString(sql, "; $1")

	// Pattern 4: Remove excessive blank lines (more than 2)
	excessiveNewlines := regexp.MustCompile(`\n{4,}`)
	sql = excessiveNewlines.ReplaceAllString(sql, "\n\n")

	return sql
}

// isLargeStatement checks if a statement is "large" (CREATE FUNCTION, CREATE TABLE, etc.)
// Large statements get extra blank lines for readability
func isLargeStatement(stmt *pg_query.RawStmt) bool {
	if stmt == nil || stmt.Stmt == nil {
		return false
	}

	// Check statement type
	switch stmt.Stmt.Node.(type) {
	case *pg_query.Node_CreateFunctionStmt:
		return true
	case *pg_query.Node_CreateStmt: // CREATE TABLE
		return true
	case *pg_query.Node_ViewStmt: // CREATE VIEW
		return true
	case *pg_query.Node_IndexStmt: // CREATE INDEX
		// Only add extra space for complex indexes
		return false
	default:
		return false
	}
}

// FormatFunctionBody formats the body of a CREATE FUNCTION statement
// ensuring proper indentation and line breaks
func (f *StatementFormatter) FormatFunctionBody(functionSQL string) string {
	// This is a placeholder for future enhancement
	// For now, just ensure the function has proper line breaks

	// Add line break after AS $$
	functionSQL = regexp.MustCompile(`AS\s+\$\$`).ReplaceAllString(functionSQL, "AS $$\n")

	// Add line break before $$ LANGUAGE
	functionSQL = regexp.MustCompile(`\$\$\s+LANGUAGE`).ReplaceAllString(functionSQL, "$$\nLANGUAGE")

	return functionSQL
}

// EnsureStatementSpacing is the main entry point for formatting
// It ensures proper spacing between all types of SQL statements
func EnsureStatementSpacing(sql string) string {
	formatter := NewStatementFormatter()
	return formatter.FormatSQL(sql)
}
