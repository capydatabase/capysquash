package postprocessing

import "strings"

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

func (f *StatementFormatter) FormatSQL(sql string) string {
	if sql == "" {
		return sql
	}

	// Regex-based formatting only. AST formatting via pg_query.Deparse() is
	// deliberately not used: it does not preserve LANGUAGE placement, the
	// language itself, volatility and security markers, or function body
	// quoting, and the consolidation rules now keep the original SQL intact.
	return f.formatWithRegex(sql)
}

// formatWithRegex uses regex patterns to add formatting when AST parsing fails
func (f *StatementFormatter) formatWithRegex(sql string) string {
	// Pattern 1: Add blank lines between CREATE FUNCTION statements
	sql = addBlankLineAfterFunctionTerminator(sql)

	// Pattern 2: Add blank lines between CREATE TABLE/INDEX/TRIGGER/POLICY/VIEW statements
	sql = addBlankLineBeforeCreateObject(sql)

	// Pattern 3: Fix missing space after semicolons for uppercase statement boundaries
	sql = addSpaceAfterSemicolonBeforeUpper(sql)

	// Pattern 4: Remove excessive blank lines (more than 2)
	sql = collapseRunsOfNewlines(sql, 2)

	return sql
}

// FormatFunctionBody formats the body of a CREATE FUNCTION statement
// ensuring proper indentation and line breaks
func (f *StatementFormatter) FormatFunctionBody(functionSQL string) string {
	// This is a placeholder for future enhancement
	// For now, just ensure the function has proper line breaks

	// Add line break after AS $$
	functionSQL = normalizeAsDollarSpacing(functionSQL)

	// Add line break before $$ LANGUAGE
	functionSQL = moveLanguageToNextLineAfterDollar(functionSQL)

	return functionSQL
}

// EnsureStatementSpacing is the main entry point for formatting
// It ensures proper spacing between all types of SQL statements
func EnsureStatementSpacing(sql string) string {
	formatter := NewStatementFormatter()
	return formatter.FormatSQL(sql)
}

func addBlankLineAfterFunctionTerminator(sql string) string {
	if sql == "" {
		return sql
	}

	var out strings.Builder
	out.Grow(len(sql) + 16)

	for i := 0; i < len(sql); i++ {
		if sql[i] == '$' && i+2 < len(sql) && sql[i+1] == '$' && sql[i+2] == ';' {
			out.WriteString("$$;")
			i += 2

			j := skipFormattingWhitespace(sql, i+1)
			if hasCreateFunctionPrefix(sql, j) {
				out.WriteString("\n\n")
				i = j - 1
			}
			continue
		}

		out.WriteByte(sql[i])
	}

	return out.String()
}

func addBlankLineBeforeCreateObject(sql string) string {
	if sql == "" {
		return sql
	}

	var out strings.Builder
	out.Grow(len(sql) + 16)

	for i := 0; i < len(sql); i++ {
		if sql[i] != ';' {
			out.WriteByte(sql[i])
			continue
		}

		out.WriteByte(';')
		j := skipFormattingWhitespace(sql, i+1)
		if hasCreateObjectPrefix(sql, j) {
			out.WriteString("\n\n")
			i = j - 1
		}
	}

	return out.String()
}

func addSpaceAfterSemicolonBeforeUpper(sql string) string {
	if sql == "" {
		return sql
	}

	var out strings.Builder
	out.Grow(len(sql) + 8)

	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		if ch != ';' {
			out.WriteByte(ch)
			continue
		}

		out.WriteByte(';')
		if i+1 < len(sql) && sql[i+1] >= 'A' && sql[i+1] <= 'Z' {
			out.WriteByte(' ')
		}
	}

	return out.String()
}

func collapseRunsOfNewlines(sql string, maxRun int) string {
	if sql == "" || maxRun < 1 {
		return sql
	}

	var out strings.Builder
	out.Grow(len(sql))

	newlineRun := 0
	for i := 0; i < len(sql); i++ {
		if sql[i] == '\n' {
			newlineRun++
			if newlineRun <= maxRun {
				out.WriteByte('\n')
			}
			continue
		}

		newlineRun = 0
		out.WriteByte(sql[i])
	}

	return out.String()
}

func normalizeAsDollarSpacing(sql string) string {
	if sql == "" {
		return sql
	}

	var out strings.Builder
	out.Grow(len(sql) + 8)

	for i := 0; i < len(sql); i++ {
		if hasKeywordAt(sql, i, "AS") {
			j := skipFormattingWhitespace(sql, i+2)
			if j+1 < len(sql) && sql[j] == '$' && sql[j+1] == '$' {
				out.WriteString("AS $$\n")
				i = j + 1
				continue
			}
		}

		out.WriteByte(sql[i])
	}

	return out.String()
}

func moveLanguageToNextLineAfterDollar(sql string) string {
	if sql == "" {
		return sql
	}

	var out strings.Builder
	out.Grow(len(sql) + 8)

	for i := 0; i < len(sql); i++ {
		if i+1 < len(sql) && sql[i] == '$' && sql[i+1] == '$' {
			out.WriteString("$$")
			i += 1

			j := skipFormattingWhitespace(sql, i+1)
			if hasKeywordAt(sql, j, "LANGUAGE") {
				out.WriteString("\nLANGUAGE")
				i = j + len("LANGUAGE") - 1
			}
			continue
		}

		out.WriteByte(sql[i])
	}

	return out.String()
}

func hasCreateFunctionPrefix(sql string, pos int) bool {
	if !hasKeywordAt(sql, pos, "CREATE") {
		return false
	}

	idx := skipFormattingWhitespace(sql, pos+len("CREATE"))
	if hasKeywordAt(sql, idx, "OR") {
		idx = skipFormattingWhitespace(sql, idx+len("OR"))
		if !hasKeywordAt(sql, idx, "REPLACE") {
			return false
		}
		idx = skipFormattingWhitespace(sql, idx+len("REPLACE"))
	}

	return hasKeywordAt(sql, idx, "FUNCTION")
}

func hasCreateObjectPrefix(sql string, pos int) bool {
	if !hasKeywordAt(sql, pos, "CREATE") {
		return false
	}

	idx := skipFormattingWhitespace(sql, pos+len("CREATE"))
	return hasKeywordAt(sql, idx, "TABLE") ||
		hasKeywordAt(sql, idx, "INDEX") ||
		hasKeywordAt(sql, idx, "TRIGGER") ||
		hasKeywordAt(sql, idx, "POLICY") ||
		hasKeywordAt(sql, idx, "VIEW")
}

func hasKeywordAt(sql string, pos int, keyword string) bool {
	if pos < 0 || pos+len(keyword) > len(sql) {
		return false
	}

	segment := sql[pos : pos+len(keyword)]
	if !strings.EqualFold(segment, keyword) {
		return false
	}

	if pos > 0 && isIdentifierChar(sql[pos-1]) {
		return false
	}

	end := pos + len(keyword)
	if end < len(sql) && isIdentifierChar(sql[end]) {
		return false
	}

	return true
}

func skipFormattingWhitespace(sql string, pos int) int {
	for pos < len(sql) {
		switch sql[pos] {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			pos++
		default:
			return pos
		}
	}
	return pos
}

func isIdentifierChar(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}
