package postprocessing

import (
	"fmt"
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
	transformedSQL := sql
	transformedAny := true
	loggedFunctionCount := false

	// Keep processing until no more transformations occur
	// This handles the case where multiple RETURNS TABLE functions need fixing
	// and ensures we always work with current indices (not stale offsets)
	for transformedAny {
		transformedAny = false

		functions := findReturnsTableFunctions(transformedSQL)
		if len(functions) == 0 {
			break // No more RETURNS TABLE functions found
		}

		if !loggedFunctionCount {
			// First iteration only
			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Found %d RETURNS TABLE functions", len(functions))
			loggedFunctionCount = true
		}

		for _, function := range functions {
			funcName := function.Name
			columnsSpec := function.ColumnsSpec

			// Parse column names from TABLE(...) definition
			columnNames := parseTableColumns(columnsSpec)

			// DEBUG: Log what we extracted
			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("🔍 Function %s RETURNS TABLE columns spec: %q", funcName, columnsSpec)
			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("🔍 Extracted column names: %v", columnNames)

			if len(columnNames) == 0 {
				continue
			}

			bodyStart := function.BodyStart
			bodyEnd := function.BodyEnd
			body := transformedSQL[bodyStart:bodyEnd]

			returnMatches := findReturnNextStatements(body)

			if len(returnMatches) == 0 {
				continue
			}

			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Found %d RETURN NEXT statements in function %s", len(returnMatches), funcName)

			fixedBody := body
			bodyOffset := 0

			for _, returnMatch := range returnMatches {
				varName := returnMatch.VarName

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
				oldStmt := fixedBody[returnMatch.Start+bodyOffset : returnMatch.End+bodyOffset]

				// DEBUG: Log the transformation
				utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("🔍 Transforming RETURN NEXT:")
				utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("   varName: %q", varName)
				utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("   columnNames: %v", columnNames)
				utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("   selectStmt: %q", selectStmt)

				fixedBody = fixedBody[:returnMatch.Start+bodyOffset] + selectStmt + fixedBody[returnMatch.End+bodyOffset:]

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

			// Mark that we made a transformation, so we'll re-scan for more functions
			transformedAny = true

			// Break out of the inner loop since we've modified transformedSQL
			// and need to re-run the regex to get fresh indices
			break
		}
	}

	return transformedSQL
}

type returnsTableFunction struct {
	Name        string
	ColumnsSpec string
	BodyStart   int
	BodyEnd     int
}

type returnNextStatement struct {
	Start   int
	End     int
	VarName string
}

func findReturnsTableFunctions(sql string) []returnsTableFunction {
	functions := make([]returnsTableFunction, 0)
	cursor := 0

	for cursor < len(sql) {
		fn, next, ok := findNextReturnsTableFunction(sql, cursor)
		if !ok {
			break
		}
		functions = append(functions, fn)
		cursor = next
	}

	return functions
}

func findNextReturnsTableFunction(sql string, start int) (returnsTableFunction, int, bool) {
	for i := start; i < len(sql); i++ {
		if !hasReturnFixKeywordAt(sql, i, "CREATE") {
			continue
		}

		pos := skipReturnFixWhitespace(sql, i+len("CREATE"))
		if hasReturnFixKeywordAt(sql, pos, "OR") {
			pos = skipReturnFixWhitespace(sql, pos+len("OR"))
			if !hasReturnFixKeywordAt(sql, pos, "REPLACE") {
				continue
			}
			pos = skipReturnFixWhitespace(sql, pos+len("REPLACE"))
		}

		if !hasReturnFixKeywordAt(sql, pos, "FUNCTION") {
			continue
		}
		pos = skipReturnFixWhitespace(sql, pos+len("FUNCTION"))

		funcName, nextPos, ok := readReturnFixFunctionName(sql, pos)
		if !ok {
			continue
		}
		pos = skipReturnFixWhitespace(sql, nextPos)

		if pos >= len(sql) || sql[pos] != '(' {
			continue
		}

		paramsEnd, ok := findReturnFixMatchingParen(sql, pos)
		if !ok {
			continue
		}
		pos = skipReturnFixWhitespace(sql, paramsEnd+1)

		if !hasReturnFixKeywordAt(sql, pos, "RETURNS") {
			continue
		}
		pos = skipReturnFixWhitespace(sql, pos+len("RETURNS"))

		if !hasReturnFixKeywordAt(sql, pos, "TABLE") {
			continue
		}
		pos = skipReturnFixWhitespace(sql, pos+len("TABLE"))

		if pos >= len(sql) || sql[pos] != '(' {
			continue
		}

		colsEnd, ok := findReturnFixMatchingParen(sql, pos)
		if !ok {
			continue
		}
		columnsSpec := sql[pos+1 : colsEnd]

		_, bodyStart, ok := findAsDollarDelimiter(sql, colsEnd+1)
		if !ok {
			continue
		}

		bodyEndRel := strings.Index(sql[bodyStart:], "$$")
		if bodyEndRel == -1 {
			continue
		}
		bodyEnd := bodyStart + bodyEndRel

		return returnsTableFunction{
			Name:        funcName,
			ColumnsSpec: columnsSpec,
			BodyStart:   bodyStart,
			BodyEnd:     bodyEnd,
		}, bodyEnd + 2, true
	}

	return returnsTableFunction{}, len(sql), false
}

func findReturnNextStatements(body string) []returnNextStatement {
	statements := make([]returnNextStatement, 0)

	for i := 0; i < len(body); i++ {
		if !hasReturnFixKeywordAt(body, i, "RETURN") {
			continue
		}

		pos := skipReturnFixWhitespace(body, i+len("RETURN"))
		if !hasReturnFixKeywordAt(body, pos, "NEXT") {
			continue
		}
		pos = skipReturnFixWhitespace(body, pos+len("NEXT"))

		semiRel := strings.Index(body[pos:], ";")
		if semiRel == -1 {
			break
		}

		semi := pos + semiRel
		arg := strings.TrimSpace(body[pos:semi])
		if arg != "" && !isReturnNextVariable(arg) {
			i = semi
			continue
		}

		statements = append(statements, returnNextStatement{
			Start:   i,
			End:     semi + 1,
			VarName: arg,
		})
		i = semi
	}

	return statements
}

func findAsDollarDelimiter(sql string, start int) (int, int, bool) {
	for i := start; i < len(sql); i++ {
		if !hasReturnFixKeywordAt(sql, i, "AS") {
			continue
		}

		pos := skipReturnFixWhitespace(sql, i+len("AS"))
		if pos+1 < len(sql) && sql[pos] == '$' && sql[pos+1] == '$' {
			return i, pos + 2, true
		}
	}

	return 0, 0, false
}

func findReturnFixMatchingParen(value string, open int) (int, bool) {
	depth := 0
	inSingleQuote := false
	inDoubleQuote := false

	for i := open; i < len(value); i++ {
		ch := value[i]

		if inSingleQuote {
			if ch == '\'' {
				if i+1 < len(value) && value[i+1] == '\'' {
					i++
					continue
				}
				inSingleQuote = false
			}
			continue
		}

		if inDoubleQuote {
			if ch == '"' {
				inDoubleQuote = false
			}
			continue
		}

		switch ch {
		case '\'':
			inSingleQuote = true
		case '"':
			inDoubleQuote = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}

	return 0, false
}

func hasReturnFixKeywordAt(value string, pos int, keyword string) bool {
	if pos < 0 || pos+len(keyword) > len(value) {
		return false
	}

	if !strings.EqualFold(value[pos:pos+len(keyword)], keyword) {
		return false
	}

	if pos > 0 && isReturnFixIdentifierByte(value[pos-1]) {
		return false
	}

	end := pos + len(keyword)
	if end < len(value) && isReturnFixIdentifierByte(value[end]) {
		return false
	}

	return true
}

func skipReturnFixWhitespace(value string, pos int) int {
	for pos < len(value) {
		switch value[pos] {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			pos++
		default:
			return pos
		}
	}
	return pos
}

func readReturnFixFunctionName(value string, pos int) (string, int, bool) {
	first, next, ok := readReturnFixIdentifier(value, pos)
	if !ok {
		return "", 0, false
	}

	if next < len(value) && value[next] == '.' {
		next++
		second, nextPos, ok := readReturnFixIdentifier(value, next)
		if !ok {
			return "", 0, false
		}
		return second, nextPos, true
	}

	return first, next, true
}

func readReturnFixIdentifier(value string, pos int) (string, int, bool) {
	if pos >= len(value) || !isReturnFixIdentifierStart(value[pos]) {
		return "", 0, false
	}

	i := pos + 1
	for i < len(value) && isReturnFixIdentifierByte(value[i]) {
		i++
	}

	return value[pos:i], i, true
}

func isReturnNextVariable(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) == 0 || len(parts) > 2 {
		return false
	}

	for _, part := range parts {
		if part == "" || !isReturnFixIdentifierStart(part[0]) {
			return false
		}
		for i := 1; i < len(part); i++ {
			if !isReturnFixIdentifierByte(part[i]) {
				return false
			}
		}
	}

	return true
}

func isReturnFixIdentifierStart(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
}

func isReturnFixIdentifierByte(ch byte) bool {
	return isReturnFixIdentifierStart(ch) || (ch >= '0' && ch <= '9')
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
