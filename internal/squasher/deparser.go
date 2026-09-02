package squasher

import (
	"fmt"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/errors"
	"github.com/capy-base/pgsquash-engine/internal/types"
	"github.com/capy-base/pgsquash-engine/internal/utils"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// Deparse takes a modified pg_query.ParseResult and generates a SQL string.
// This is the primary interface for converting AST back to SQL.
// Formats deparsed SQL to ensure proper spacing and readability.
func Deparse(tree *pg_query.ParseResult) (string, error) {
	if tree == nil {
		return "", nil
	}

	res, err := pg_query.Deparse(tree)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			fmt.Sprintf("failed to deparse tree: %v", err),
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}

	// Format the deparsed SQL for readability
	// pg_query.Deparse returns compressed single-line SQL, so we add proper formatting
	formatted := formatDeparserOutput(res)
	return formatted, nil
}

// formatDeparserOutput adds proper formatting to compressed SQL from pg_query.Deparse
func formatDeparserOutput(sql string) string {
	if sql == "" {
		return sql
	}

	// 1. Add newlines after semicolons (statement boundaries)
	sql = insertStatementBreaks(sql)

	// 2. Add newlines after key clauses in CREATE TABLE
	sql = addTupleCommaLineBreaks(sql)

	// 3. Normalize excessive horizontal whitespace
	sql = collapseHorizontalWhitespace(sql)

	// 4. Clean up lines
	lines := strings.Split(sql, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	sql = strings.Join(lines, "\n")

	return sql
}

func insertStatementBreaks(sql string) string {
	if sql == "" {
		return sql
	}

	var out strings.Builder
	out.Grow(len(sql) + 16)

	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		if ch != ';' {
			out.WriteByte(ch)
			continue
		}

		out.WriteByte(';')

		j := i + 1
		for j < len(sql) && isHorizontalOrLineWhitespace(sql[j]) {
			j++
		}

		if hasStatementStarterAtCI(sql, j) {
			out.WriteString("\n\n")
			i = j - 1
		}
	}

	return out.String()
}

func addTupleCommaLineBreaks(sql string) string {
	if sql == "" {
		return sql
	}

	var out strings.Builder
	out.Grow(len(sql) + 16)

	for i := 0; i < len(sql); i++ {
		if sql[i] == ')' && i+1 < len(sql) && sql[i+1] == ',' {
			out.WriteString("),\n  ")

			i += 1
			j := i + 1
			for j < len(sql) && isHorizontalOrLineWhitespace(sql[j]) {
				j++
			}
			i = j - 1
			continue
		}

		out.WriteByte(sql[i])
	}

	return out.String()
}

func collapseHorizontalWhitespace(sql string) string {
	if sql == "" {
		return sql
	}

	var out strings.Builder
	out.Grow(len(sql))

	seenSpace := false
	for i := 0; i < len(sql); i++ {
		ch := sql[i]

		if ch == '\n' || ch == '\r' {
			seenSpace = false
			out.WriteByte(ch)
			continue
		}

		if isHorizontalWhitespace(ch) {
			if !seenSpace {
				out.WriteByte(' ')
				seenSpace = true
			}
			continue
		}

		seenSpace = false
		out.WriteByte(ch)
	}

	return out.String()
}

func isHorizontalWhitespace(ch byte) bool {
	return ch == ' ' || ch == '\t' || ch == '\f' || ch == '\v'
}

func isHorizontalOrLineWhitespace(ch byte) bool {
	return isHorizontalWhitespace(ch) || ch == '\n' || ch == '\r'
}

func hasStatementStarterAtCI(sql string, pos int) bool {
	if pos < 0 || pos >= len(sql) {
		return false
	}

	starters := []string{"CREATE", "ALTER", "DROP", "INSERT", "UPDATE", "DELETE", "SELECT"}
	for _, keyword := range starters {
		if hasKeywordAtCI(sql, pos, keyword) {
			return true
		}
	}

	return false
}

func hasKeywordAtCI(sql string, pos int, keyword string) bool {
	if pos < 0 || pos+len(keyword) > len(sql) {
		return false
	}

	if pos > 0 && isIdentifierByte(sql[pos-1]) {
		return false
	}

	segment := sql[pos : pos+len(keyword)]
	if !strings.EqualFold(segment, keyword) {
		return false
	}

	end := pos + len(keyword)
	if end < len(sql) && isIdentifierByte(sql[end]) {
		return false
	}

	return true
}

func isIdentifierByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

// DeparseWithStatement takes a Statement and its ParseTree, and generates SQL.
// Clears implicit AccessMethod="btree" from indexes to preserve original semantics.
// Preserves function volatility markers (STABLE/VOLATILE/IMMUTABLE) during deparsing.
func DeparseWithStatement(stmt *types.Statement) (string, error) {
	if stmt == nil || stmt.ParseTree == nil {
		if stmt != nil {
			return stmt.SQL, nil
		}
		return "", nil
	}

	parseResult := stmt.ParseTree
	if parseResult == nil {
		return stmt.SQL, nil
	}

	// For indexes without explicit access method, clear the
	// AccessMethod field in the AST before deparsing. This prevents pg_query
	// from adding "USING btree" which breaks spatial indexes.
	if stmt.ObjectType == types.TypeIndex && !stmt.IndexHadExplicitAccessMethod {
		cleanIndexAccessMethod(parseResult)
	}

	// For functions, preserve volatility markers from original SQL
	// pg_query.Deparse() doesn't preserve volatility, so we extract from original
	// SQL and restore after deparsing
	if stmt.ObjectType == types.TypeFunction {
		if logger := utils.GetDefaultLogger(); logger != nil {
			logger.WithPrefix("DEPARSER").Debug(
				"Deparsing function with volatility preservation. Original SQL: %.100s",
				stmt.SQL,
			)
		}
		result, err := deparseWithVolatilityPreservation(parseResult, stmt.SQL)
		if err == nil {
			if logger := utils.GetDefaultLogger(); logger != nil {
				logger.WithPrefix("DEPARSER").Debug("Deparsed result: %.100s", result)
			}
		}
		return result, err
	}

	return Deparse(parseResult)
}

// deparseWithVolatilityPreservation deparses a function while preserving its volatility marker
// pg_query.Deparse() doesn't preserve STABLE/VOLATILE/IMMUTABLE markers
// pg_query.Deparse() doesn't preserve SECURITY DEFINER/INVOKER
func deparseWithVolatilityPreservation(tree *pg_query.ParseResult, originalSQL string) (string, error) {
	// Extract volatility from original SQL
	volatility := extractVolatilityMarker(originalSQL)

	// Extract SECURITY DEFINER/INVOKER from original SQL
	securityDefiner := extractSecurityDefiner(originalSQL)

	// Deparse normally
	deparsed, err := Deparse(tree)
	if err != nil {
		return "", err
	}

	// If no volatility found in original, return as-is
	if volatility == "" && securityDefiner == "" {
		return deparsed, nil
	}

	// Inject the preserved markers into deparsed SQL
	// pg_query.Deparse puts LANGUAGE before AS, so we inject after LANGUAGE
	// Order: LANGUAGE xxx [VOLATILITY] [SECURITY DEFINER] AS $$
	if volatility != "" {
		deparsed, err = injectVolatilityMarker(deparsed, volatility)
		if err != nil {
			return "", err
		}
	}

	// Inject SECURITY DEFINER after volatility (or after LANGUAGE if no volatility)
	if securityDefiner != "" {
		deparsed, err = injectSecurityDefiner(deparsed, securityDefiner)
		if err != nil {
			return "", err
		}
	}

	return deparsed, nil
}

// extractVolatilityMarker extracts STABLE/VOLATILE/IMMUTABLE from function SQL
func extractVolatilityMarker(sql string) string {
	upperSQL := strings.ToUpper(sql)

	markers := []string{"IMMUTABLE", "STABLE", "VOLATILE"}
	for _, marker := range markers {
		// Check various positions where marker might appear
		patterns := []string{
			" " + marker + " ",
			" " + marker + ";",
			"\n" + marker + " ",
			"\n" + marker + ";",
		}

		for _, pattern := range patterns {
			if strings.Contains(upperSQL, pattern) {
				return marker
			}
		}
	}

	return ""
}

// extractSecurityDefiner extracts SECURITY DEFINER or SECURITY INVOKER from function SQL
// pg_query.Deparse() doesn't preserve these critical security attributes
func extractSecurityDefiner(sql string) string {
	upperSQL := strings.ToUpper(sql)

	// Check for SECURITY DEFINER or SECURITY INVOKER
	securityPatterns := []struct {
		pattern string
		marker  string
	}{
		{" SECURITY DEFINER", "SECURITY DEFINER"},
		{"\nSECURITY DEFINER", "SECURITY DEFINER"},
		{" SECURITY INVOKER", "SECURITY INVOKER"},
		{"\nSECURITY INVOKER", "SECURITY INVOKER"},
	}

	for _, sp := range securityPatterns {
		if strings.Contains(upperSQL, sp.pattern) {
			return sp.marker
		}
	}

	return ""
}

// injectVolatilityMarker adds volatility marker to deparsed function SQL
// Injects after LANGUAGE clause, before AS
func injectVolatilityMarker(sql string, volatility string) (string, error) {
	return insertMarkerBeforeAS(sql, volatility), nil
}

// injectSecurityDefiner adds SECURITY DEFINER/INVOKER to deparsed function SQL
// Injects after LANGUAGE and volatility (if present), before AS
func injectSecurityDefiner(sql string, securityMarker string) (string, error) {
	return insertMarkerBeforeAS(sql, securityMarker), nil
}

func insertMarkerBeforeAS(sql string, marker string) string {
	trimmedMarker := strings.TrimSpace(marker)
	if sql == "" || trimmedMarker == "" {
		return sql
	}

	upperSQL := strings.ToUpper(sql)
	if strings.Contains(upperSQL, strings.ToUpper(trimmedMarker)) {
		return sql
	}

	asIdx := findAsBeforeDollarIndex(sql)
	if asIdx == -1 {
		asIdx = findStandaloneAsIndex(sql)
	}
	if asIdx == -1 {
		return sql
	}

	prefix := strings.TrimRight(sql[:asIdx], " \t\r\n")
	suffix := strings.TrimLeft(sql[asIdx:], " \t\r\n")
	if prefix == "" {
		return trimmedMarker + " " + suffix
	}

	return prefix + " " + trimmedMarker + " " + suffix
}

func findAsBeforeDollarIndex(sql string) int {
	for i := 0; i+2 <= len(sql); i++ {
		if !hasKeywordAtCI(sql, i, "AS") {
			continue
		}

		j := i + 2
		for j < len(sql) && isHorizontalOrLineWhitespace(sql[j]) {
			j++
		}

		if j < len(sql) && sql[j] == '$' {
			return i
		}
	}

	return -1
}

func findStandaloneAsIndex(sql string) int {
	for i := 0; i+2 <= len(sql); i++ {
		if hasKeywordAtCI(sql, i, "AS") {
			return i
		}
	}

	return -1
}

// cleanIndexAccessMethod removes implicit "btree" access method from IndexStmt nodes
// Prevents pg_query.Deparse from adding "USING btree" to spatial indexes
func cleanIndexAccessMethod(tree *pg_query.ParseResult) {
	if tree == nil {
		return
	}

	for _, rawStmt := range tree.Stmts {
		if rawStmt.Stmt == nil {
			continue
		}

		if indexStmt := rawStmt.Stmt.GetIndexStmt(); indexStmt != nil {
			// Only clear if method is "btree" (default added by pg_query)
			// Preserve explicit non-btree methods (GIN, GIST, HASH, etc.)
			if indexStmt.AccessMethod == "btree" {
				indexStmt.AccessMethod = ""
			}
		}
	}
}
