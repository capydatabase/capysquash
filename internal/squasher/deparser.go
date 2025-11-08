package squasher

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/types"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// Deparse takes a modified pg_query.ParseResult and generates a SQL string.
// This is the primary interface for converting AST back to SQL.
// BUG #2 FIX: Formats deparsed SQL to ensure proper spacing and readability.
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

	// BUG #2 FIX: Format the deparsed SQL for readability
	// pg_query.Deparse returns compressed single-line SQL, so we add proper formatting
	formatted := formatDeparserOutput(res)
	return formatted, nil
}

// formatDeparserOutput adds proper formatting to compressed SQL from pg_query.Deparse
// This fixes Bug #2 by ensuring CREATE TABLE, CREATE FUNCTION, etc. are readable
func formatDeparserOutput(sql string) string {
	if sql == "" {
		return sql
	}

	// 1. Add newlines after semicolons (statement boundaries)
	sql = regexp.MustCompile(`;(\s*)(CREATE|ALTER|DROP|INSERT|UPDATE|DELETE|SELECT)`).
		ReplaceAllString(sql, ";\n\n$2")

	// 2. Add newlines after key clauses in CREATE TABLE
	sql = regexp.MustCompile(`\),\s*`).ReplaceAllString(sql, "),\n  ")

	// 3. Add spacing around CHECK constraints
	sql = regexp.MustCompile(`\s*CHECK\s*\(`).ReplaceAllString(sql, " CHECK (")

	// 4. Fix DEFAULT clauses
	sql = regexp.MustCompile(`\s*DEFAULT\s+`).ReplaceAllString(sql, " DEFAULT ")

	// 5. Normalize excessive whitespace
	sql = regexp.MustCompile(`[ \t]+`).ReplaceAllString(sql, " ")

	// 6. Clean up lines
	lines := strings.Split(sql, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	sql = strings.Join(lines, "\n")

	return sql
}

// DeparseWithStatement takes a Statement and its ParseTree, and generates SQL.
// BUG-001 fix: Clears implicit AccessMethod="btree" from indexes to preserve original semantics.
// BUG-003 fix: Preserves function volatility markers (STABLE/VOLATILE/IMMUTABLE) during deparsing.
func DeparseWithStatement(stmt *types.Statement) (string, error) {
	if stmt == nil || stmt.ParseTree == nil {
		if stmt != nil {
			return stmt.SQL, nil
		}
		return "", nil
	}

	parseResult, ok := stmt.ParseTree.(*pg_query.ParseResult)
	if !ok {
		return stmt.SQL, nil
	}

	// BUG-001 fix: For indexes without explicit access method, clear the
	// AccessMethod field in the AST before deparsing. This prevents pg_query
	// from adding "USING btree" which breaks spatial indexes.
	if stmt.ObjectType == types.TypeIndex && !stmt.IndexHadExplicitAccessMethod {
		cleanIndexAccessMethod(parseResult)
	}

	// BUG-003 fix: For functions, preserve volatility markers from original SQL
	// pg_query.Deparse() doesn't preserve volatility, so we extract from original
	// SQL and restore after deparsing
	if stmt.ObjectType == types.TypeFunction {
		fmt.Printf("[DEBUG] Deparsing function with volatility preservation. Original SQL: %.100s\n", stmt.SQL)
		result, err := deparseWithVolatilityPreservation(parseResult, stmt.SQL)
		if err == nil {
			fmt.Printf("[DEBUG] Deparsed result: %.100s\n", result)
		}
		return result, err
	}

	return Deparse(parseResult)
}

// deparseWithVolatilityPreservation deparses a function while preserving its volatility marker
// BUG-003 fix: pg_query.Deparse() doesn't preserve STABLE/VOLATILE/IMMUTABLE markers
// BUG-010 fix: pg_query.Deparse() doesn't preserve SECURITY DEFINER/INVOKER
func deparseWithVolatilityPreservation(tree *pg_query.ParseResult, originalSQL string) (string, error) {
	// Extract volatility from original SQL
	volatility := extractVolatilityMarker(originalSQL)

	// BUG-010 fix: Extract SECURITY DEFINER/INVOKER from original SQL
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

	// BUG-010 fix: Inject SECURITY DEFINER after volatility (or after LANGUAGE if no volatility)
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
// BUG-010 fix: pg_query.Deparse() doesn't preserve these critical security attributes
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
	// Pattern: ... LANGUAGE xxx AS $$ or ... LANGUAGE xxx SECURITY DEFINER AS $$
	// We want to inject volatility after LANGUAGE (and SECURITY DEFINER if present)
	// but before AS

	// Try to find "LANGUAGE xxx" followed by optional "SECURITY DEFINER", then "AS"
	pattern := regexp.MustCompile(`(?i)(LANGUAGE\s+\w+)(\s+(?:SECURITY\s+(?:DEFINER|INVOKER)\s+)?)(AS\s+)`)

	// Replace with: LANGUAGE xxx [SECURITY DEFINER] VOLATILITY AS
	replacement := fmt.Sprintf("$1$2%s $3", volatility)
	result := pattern.ReplaceAllString(sql, replacement)

	// If no match, try simpler pattern: just before AS
	if result == sql {
		simplePattern := regexp.MustCompile(`(?i)(\s+)(AS\s+\$)`)
		result = simplePattern.ReplaceAllString(sql, fmt.Sprintf(" %s $2", volatility))
	}

	return result, nil
}

// injectSecurityDefiner adds SECURITY DEFINER/INVOKER to deparsed function SQL
// Injects after LANGUAGE and volatility (if present), before AS
// BUG-010 fix: Preserves critical security attributes dropped by pg_query.Deparse()
func injectSecurityDefiner(sql string, securityMarker string) (string, error) {
	// Pattern options:
	// 1. LANGUAGE xxx VOLATILE/STABLE/IMMUTABLE AS $$ -> inject after volatility
	// 2. LANGUAGE xxx AS $$ -> inject after LANGUAGE
	//
	// We want: LANGUAGE xxx [VOLATILITY] SECURITY DEFINER AS $$

	// Try pattern with volatility marker first
	withVolatilityPattern := regexp.MustCompile(`(?i)(LANGUAGE\s+\w+\s+(?:VOLATILE|STABLE|IMMUTABLE))(\s+)(AS\s+)`)
	result := withVolatilityPattern.ReplaceAllString(sql, fmt.Sprintf("$1 %s $3", securityMarker))

	// If no match (no volatility), inject after LANGUAGE
	if result == sql {
		withoutVolatilityPattern := regexp.MustCompile(`(?i)(LANGUAGE\s+\w+)(\s+)(AS\s+)`)
		result = withoutVolatilityPattern.ReplaceAllString(sql, fmt.Sprintf("$1 %s $3", securityMarker))
	}

	// If still no match, try simpler pattern: just before AS
	if result == sql {
		simplePattern := regexp.MustCompile(`(?i)(\s+)(AS\s+\$)`)
		result = simplePattern.ReplaceAllString(sql, fmt.Sprintf(" %s $2", securityMarker))
	}

	return result, nil
}

// cleanIndexAccessMethod removes implicit "btree" access method from IndexStmt nodes
// BUG-001 fix: Prevents pg_query.Deparse from adding "USING btree" to spatial indexes
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

//nolint:unused // Utility function for future node deparsing needs
// deparseNode converts a single Node back to SQL using pg_query.Deparse
func deparseNode(node *pg_query.Node) (string, error) {
	if node == nil {
		return "", nil
	}
	// Create a dummy ParseResult to use the real deparser
	tree := &pg_query.ParseResult{
		Stmts: []*pg_query.RawStmt{
			{
				Stmt: node,
			},
		},
	}
	return Deparse(tree)
}
