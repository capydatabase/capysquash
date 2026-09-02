// Package utils provides common utility functions used across the codebase.
package utils

import (
	"strings"
)

// NormalizeObjectName normalizes database object names (tables, columns, functions)
// by converting to lowercase and trimming whitespace.
// This provides consistent object name comparison across the codebase.
func NormalizeObjectName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// ContainsKeyword checks if SQL contains a specific keyword (case-insensitive).
// Both sql and keyword are normalized to uppercase before comparison.
//
// Example:
//
//	ContainsKeyword("create table foo", "CREATE") // true
//	ContainsKeyword("SELECT * FROM bar", "insert") // false
func ContainsKeyword(sql string, keyword string) bool {
	return strings.Contains(strings.ToUpper(sql), strings.ToUpper(keyword))
}

// HasClause checks if SQL contains a specific SQL clause (case-insensitive).
// Adds a space after the clause to avoid partial matches.
//
// Example:
//
//	HasClause("CREATE TABLE users (...)", "CREATE TABLE") // true
//	HasClause("ALTER TABLE users ADD", "ALTER") // true
//	HasClause("SELECT * FROM altered", "ALTER") // false (no space after)
func HasClause(sql string, clause string) bool {
	return strings.Contains(strings.ToUpper(sql), strings.ToUpper(clause)+" ")
}

// HasVolatilityMarker checks if SQL contains a PostgreSQL function volatility marker.
// Returns true if IMMUTABLE, STABLE, or VOLATILE is found in the string.
//
// Used by plugin transformation systems to avoid adding duplicate volatility markers.
func HasVolatilityMarker(sql string) bool {
	upper := strings.ToUpper(sql)
	return strings.Contains(upper, " IMMUTABLE") ||
		strings.Contains(upper, " STABLE") ||
		strings.Contains(upper, " VOLATILE")
}

// TrimSQLComments removes single-line SQL comments (--) from the beginning of a string.
// Multi-line comments (/* */) are not handled.
//
// Example:
//
//	TrimSQLComments("-- Comment\nCREATE TABLE") // "CREATE TABLE"
//	TrimSQLComments("CREATE TABLE") // "CREATE TABLE"
func TrimSQLComments(sql string) string {
	lines := strings.Split(sql, "\n")
	var result []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "--") {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// NormalizeSQLWhitespace collapses multiple spaces into single spaces
// and removes leading/trailing whitespace.
// Using precompiled pattern for performance.
func NormalizeSQLWhitespace(sql string) string {
	if strings.TrimSpace(sql) == "" {
		return ""
	}

	return strings.Join(strings.Fields(sql), " ")
}
