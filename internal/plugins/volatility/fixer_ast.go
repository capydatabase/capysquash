// Package volatility provides AST-based function volatility marker detection and fixing.
// This refactored version uses pg_query_go AST parsing instead of regex for more accurate results.
package volatility

import (
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// ASTVolatilityFixer adds volatility markers to PostgreSQL functions using AST parsing
type ASTVolatilityFixer struct {
	registry *FunctionRegistry
}

// NewASTVolatilityFixer creates a new AST-based volatility fixer with a function registry
func NewASTVolatilityFixer(registry *FunctionRegistry) *ASTVolatilityFixer {
	return &ASTVolatilityFixer{
		registry: registry,
	}
}

// Fix adds volatility markers to registered functions in the SQL using AST parsing
// Returns the modified SQL with volatility markers added
func (vf *ASTVolatilityFixer) Fix(sql string) (string, error) {
	// Parse the SQL into AST
	parseResult, err := pg_query.Parse(sql)
	if err != nil {
		return "", errors.New(errors.ErrorCodeInvalidSQL, errors.CategoryParsing, "failed to parse SQL for volatility fixing", map[string]any{"error": err.Error()})
	}

	// Track whether we made any changes
	madeChanges := false
	transformedSQL := sql

	// Process each statement
	for _, stmt := range parseResult.Stmts {
		if createFunc := stmt.Stmt.GetCreateFunctionStmt(); createFunc != nil {
			// Extract function name
			funcName := extractFunctionName(createFunc)
			if funcName == "" {
				continue
			}

			// Check if this function needs a volatility marker
			desiredVolatility, isRegistered := vf.registry.GetVolatility(funcName)
			if !isRegistered {
				continue
			}

			// Check if function already has a volatility marker
			if hasVolatilityInAST(createFunc) {
				continue
			}

			// We need to add the volatility marker
			// Since pg_query doesn't provide position info for reconstruction,
			// we'll need to do targeted string replacement
			replacement, err := addVolatilityToFunction(sql, funcName, desiredVolatility)
			if err != nil {
				continue // Skip on error
			}

			transformedSQL = replacement
			madeChanges = true
		}
	}

	if !madeChanges {
		return sql, nil
	}

	return transformedSQL, nil
}

// extractFunctionName extracts the function name from CreateFunctionStmt
func extractFunctionName(createFunc *pg_query.CreateFunctionStmt) string {
	if createFunc == nil || len(createFunc.Funcname) == 0 {
		return ""
	}

	// Get the last part of the name (ignores schema)
	lastName := createFunc.Funcname[len(createFunc.Funcname)-1]
	if lastName.GetString_() != nil {
		return lastName.GetString_().Sval
	}

	return ""
}

// hasVolatilityInAST checks if the function already has a volatility marker in the AST
func hasVolatilityInAST(createFunc *pg_query.CreateFunctionStmt) bool {
	if createFunc == nil {
		return false
	}

	// Check the options list for volatility
	for _, option := range createFunc.Options {
		if defElem := option.GetDefElem(); defElem != nil {
			optName := strings.ToUpper(defElem.Defname)
			if optName == "VOLATILITY" {
				return true
			}
		}
	}

	return false
}

// addVolatilityToFunction adds volatility marker to a function in SQL
// This is a targeted string replacement based on function name
func addVolatilityToFunction(sql string, funcName string, volatility VolatilityType) (string, error) {
	// Find the function definition
	// Look for patterns like:
	// CREATE [OR REPLACE] FUNCTION [schema.]funcName(...)
	// RETURNS ... [modifiers] AS $$...

	// Strategy: Find the function, then find " AS " or " AS $$" or " AS $tag$"
	// and insert the volatility marker before it

	lines := strings.Split(sql, "\n")
	var result []string
	inFunction := false
	functionStarted := false

	for _, line := range lines {
		upperLine := strings.ToUpper(line)

		// Check if this line starts the target function
		if !functionStarted && strings.Contains(upperLine, "CREATE") &&
			strings.Contains(upperLine, "FUNCTION") &&
			containsFunctionName(line, funcName) {
			functionStarted = true
			inFunction = true
		}

		// If we're in the target function, look for AS keyword
		if inFunction {
			// Check for " AS " with dollar quotes or single quotes
			if strings.Contains(upperLine, " AS ") {
				// Insert volatility marker before AS
				asIndex := findASKeywordIndex(line)
				if asIndex > 0 {
					before := line[:asIndex]
					after := line[asIndex:]
					line = before + " " + string(volatility) + after
					inFunction = false // Done with this function
				}
			}
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n"), nil
}

// containsFunctionName checks if a line contains the function name
// Handles schema-qualified names (e.g., "auth.uid", "current_user_id")
func containsFunctionName(line string, funcName string) bool {
	lowerLine := strings.ToLower(line)
	lowerFunc := strings.ToLower(funcName)

	// Check for exact match with word boundaries
	// Matches: "function funcName(", "function schema.funcName("
	if strings.Contains(lowerLine, "function "+lowerFunc+"(") {
		return true
	}
	if strings.Contains(lowerLine, "."+lowerFunc+"(") {
		return true
	}

	return false
}

// findASKeywordIndex finds the index of " AS " keyword (case-insensitive)
// Returns the index where " AS " starts, or -1 if not found
func findASKeywordIndex(line string) int {
	upperLine := strings.ToUpper(line)

	// Look for " AS " with various patterns
	patterns := []string{" AS $$", " AS $", " AS '", " AS \""}

	for _, pattern := range patterns {
		if idx := strings.Index(upperLine, pattern); idx != -1 {
			return idx
		}
	}

	// Fallback: simple " AS " match
	if idx := strings.Index(upperLine, " AS "); idx != -1 {
		return idx
	}

	return -1
}

// FixSQL is a convenience function that creates a fixer and fixes SQL in one call
// Used for quick one-off transformations
func FixSQL(sql string, registry *FunctionRegistry) (string, error) {
	fixer := NewASTVolatilityFixer(registry)
	return fixer.Fix(sql)
}
