package postprocessing

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/utils"
)

// FixCallback is called when a fix is applied, allowing tracking of transformations.
// Parameters: description, before (original SQL), after (fixed SQL)
type FixCallback func(description, before, after string)

// FixReturnNextWithOutParams fixes RETURN NEXT usage in RETURNS TABLE functions.
//
// PostgreSQL Issue:
// RETURNS TABLE creates implicit OUT parameters. Using RETURN NEXT with arguments
// in such functions causes: "pq: RETURN NEXT cannot have a parameter in function with OUT parameters"
//
// Patterns Fixed:
// 1. RETURN NEXT record_var; → RETURN QUERY SELECT record_var.field1, record_var.field2;
// 2. RETURN NEXT; (no argument) → RETURN QUERY SELECT field1, field2;
//
// This function is called in post-processing to fix any functions that slipped through
// the SQL transformation phase (when EnableTransformation is false).
//
// The optional callback parameter allows tracking transformations for reporting.
// Pass nil if tracking is not needed.
func FixReturnNextWithOutParams(sql string, callback FixCallback) string {
	// Find all functions with RETURNS TABLE
	returnsTableRegex := regexp.MustCompile(`(?ims)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\([^)]*\)\s*RETURNS\s+TABLE\s*\(\s*([^)]+)\)`)

	matches := returnsTableRegex.FindAllStringSubmatchIndex(sql, -1)
	if len(matches) == 0 {
		return sql // No RETURNS TABLE functions found
	}

	utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Found %d RETURNS TABLE functions", len(matches))

	transformedSQL := sql
	offset := 0

	for _, match := range matches {
		if len(match) < 6 {
			continue
		}

		funcNameStart := match[2] + offset
		funcNameEnd := match[3] + offset
		columnsStart := match[4] + offset
		columnsEnd := match[5] + offset

		funcName := sql[funcNameStart:funcNameEnd]
		columnsSpec := sql[columnsStart:columnsEnd]

		// Parse column names from TABLE(...) definition
		columnNames := parseTableColumns(columnsSpec)
		if len(columnNames) == 0 {
			continue
		}

		// Find the function body (between AS $$ and $$)
		funcStart := match[0] + offset
		bodyRegex := regexp.MustCompile(`(?s)AS\s+\$\$(.+?)\$\$`)
		bodyMatch := bodyRegex.FindStringSubmatchIndex(transformedSQL[funcStart:])

		if len(bodyMatch) < 4 {
			continue
		}

		bodyStart := funcStart + bodyMatch[2]
		bodyEnd := funcStart + bodyMatch[3]
		body := transformedSQL[bodyStart:bodyEnd]

		// Find RETURN NEXT statements (with or without arguments)
		returnNextRegex := regexp.MustCompile(`RETURN\s+NEXT(?:\s+([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)*))?(\s*);`)
		returnMatches := returnNextRegex.FindAllStringSubmatchIndex(body, -1)

		if len(returnMatches) == 0 {
			continue
		}

		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Found %d RETURN NEXT statements in function %s", len(returnMatches), funcName)

		fixedBody := body
		bodyOffset := 0

		for _, returnMatch := range returnMatches {
			if len(returnMatch) < 2 {
				continue
			}

			// Check if there's a variable name (Group 1)
			var varName string
			if returnMatch[2] >= 0 && returnMatch[3] >= 0 {
				// Has variable: RETURN NEXT variable_name;
				varName = fixedBody[returnMatch[2]+bodyOffset : returnMatch[3]+bodyOffset]
			}

			var selectStmt string
			if varName != "" {
				// Case 1: RETURN NEXT record_var; → RETURN QUERY SELECT record_var.field1, record_var.field2;
				var selectColumns []string
				for _, colName := range columnNames {
					selectColumns = append(selectColumns, fmt.Sprintf("%s.%s", varName, colName))
				}
				selectStmt = fmt.Sprintf("RETURN QUERY SELECT %s;", strings.Join(selectColumns, ", "))
			} else {
				// Case 2: RETURN NEXT; (no argument)
				// Build: RETURN QUERY SELECT col1, col2, col3;
				selectStmt = fmt.Sprintf("RETURN QUERY SELECT %s;", strings.Join(columnNames, ", "))
			}

			// Replace RETURN NEXT with RETURN QUERY SELECT
			oldStmt := fixedBody[returnMatch[0]+bodyOffset : returnMatch[1]+bodyOffset]
			fixedBody = fixedBody[:returnMatch[0]+bodyOffset] + selectStmt + fixedBody[returnMatch[1]+bodyOffset:]

			bodyOffset += len(selectStmt) - len(oldStmt)

			// Log the fix
			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Fixed RETURN NEXT in function %s: %s → %s", funcName, strings.TrimSpace(oldStmt), strings.TrimSpace(selectStmt))

			// Notify callback if provided
			if callback != nil {
				callback(
					fmt.Sprintf("Fixed RETURN NEXT syntax in function %s (RETURNS TABLE should use RETURN QUERY SELECT)", funcName),
					strings.TrimSpace(oldStmt),
					strings.TrimSpace(selectStmt),
				)
			}
		}

		// Replace the function body in the transformed SQL
		transformedSQL = transformedSQL[:bodyStart] + fixedBody + transformedSQL[bodyEnd:]
		offset += len(fixedBody) - len(body)
	}

	return transformedSQL
}

// parseTableColumns extracts column names from RETURNS TABLE(...) specification
func parseTableColumns(columnsSpec string) []string {
	// Split by commas, handling nested parentheses
	var columns []string
	var current strings.Builder
	depth := 0

	for _, char := range columnsSpec {
		switch char {
		case '(':
			depth++
			current.WriteRune(char)
		case ')':
			depth--
			current.WriteRune(char)
		case ',':
			if depth == 0 {
				colDef := strings.TrimSpace(current.String())
				if colDef != "" {
					// Extract just the column name (first word)
					parts := strings.Fields(colDef)
					if len(parts) > 0 {
						columns = append(columns, parts[0])
					}
				}
				current.Reset()
			} else {
				current.WriteRune(char)
			}
		default:
			current.WriteRune(char)
		}
	}

	// Handle last column
	if current.Len() > 0 {
		colDef := strings.TrimSpace(current.String())
		if colDef != "" {
			parts := strings.Fields(colDef)
			if len(parts) > 0 {
				columns = append(columns, parts[0])
			}
		}
	}

	return columns
}

// FixReturnNextWithOutParamsSimple is a backward-compatible wrapper that fixes RETURN NEXT
// without tracking transformations. For new code, prefer FixReturnNextWithOutParams with a callback.
func FixReturnNextWithOutParamsSimple(sql string) string {
	return FixReturnNextWithOutParams(sql, nil)
}
