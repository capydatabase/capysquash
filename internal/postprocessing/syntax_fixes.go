package postprocessing

import (
	"fmt"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/utils"
)

// FixMalformedDropTriggers fixes DROP TRIGGER statements with incorrect syntax.
// PostgreSQL requires: DROP TRIGGER IF EXISTS trigger_name ON table_name;
// This function fixes: DROP TRIGGER IF EXISTS table_name.trigger_name;
func FixMalformedDropTriggers(sql string) string {
	lines := strings.Split(sql, "\n")
	for i, line := range lines {
		upperLine := strings.ToUpper(strings.TrimSpace(line))

		// Check if this is a DROP TRIGGER statement with qualified name
		if strings.HasPrefix(upperLine, "DROP TRIGGER IF EXISTS") && strings.Contains(line, ".") {
			// Extract the qualified name
			// Format: DROP TRIGGER IF EXISTS table_name.trigger_name;
			parts := strings.Fields(line)
			if len(parts) >= 5 {
				qualifiedName := parts[4] // table_name.trigger_name or table_name.trigger_name;
				qualifiedName = strings.TrimSuffix(qualifiedName, ";")

				// Split on the dot
				dotIndex := strings.Index(qualifiedName, ".")
				if dotIndex > 0 {
					tableName := qualifiedName[:dotIndex]
					triggerName := qualifiedName[dotIndex+1:]

					// Reconstruct with correct syntax
					lines[i] = fmt.Sprintf("DROP TRIGGER IF EXISTS %s ON %s;", triggerName, tableName)
					utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Fixed malformed DROP TRIGGER: %s.%s -> %s ON %s", tableName, triggerName, triggerName, tableName)
				}
			}
		}
	}

	return strings.Join(lines, "\n")
}

// FixDropPolicyDeparseCorruption fixes pg_query deparser bug where DROP POLICY object name includes full qualification.
// IncorrectFromDeparse: DROP POLICY IF EXISTS schema.table.policy ON schema.table
// CorrectPostgresSQL:   DROP POLICY IF EXISTS policy ON schema.table
func FixDropPolicyDeparseCorruption(sql string) string {
	if strings.Contains(sql, "DROP POLICY") {
		beforeFix := sql
		sql = normalizeDropPolicyQualifiedName(sql)

		if sql != beforeFix {
			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Fixed pg_query deparser bug: corrected schema-qualified policy name in DROP POLICY")
		}
	}
	return sql
}

// FixMissingSemicolons adds missing semicolons to SQL statements.
// PostgreSQL requires all statements to end with a semicolon.
func FixMissingSemicolons(sql string) string {
	lines := strings.Split(sql, "\n")
	var result []string
	fixedCount := 0

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Skip empty lines, comments, and lines that already end with semicolon
		if trimmed == "" || strings.HasPrefix(trimmed, "--") || strings.HasSuffix(trimmed, ";") {
			result = append(result, line)
			continue
		}

		// Skip lines that are part of function bodies (contain $$)
		if strings.Contains(line, "$$") {
			result = append(result, line)
			continue
		}

		// Skip lines that look like they're in the middle of a statement
		// (e.g., column definitions, constraint definitions)
		if i+1 < len(lines) {
			nextLine := strings.TrimSpace(lines[i+1])
			if strings.HasPrefix(nextLine, ",") || strings.HasPrefix(nextLine, ")") {
				result = append(result, line)
				continue
			}
		}

		// Skip lines that end with opening parenthesis (start of multi-line statement)
		// e.g., CREATE TABLE foo ( or CREATE FUNCTION foo() RETURNS void AS (
		if strings.HasSuffix(trimmed, "(") {
			result = append(result, line)
			continue
		}

		// Skip CREATE/ALTER FUNCTION lines - they're always multi-line (have RETURNS, LANGUAGE, AS, etc.)
		upperLine := strings.ToUpper(trimmed)
		if (strings.HasPrefix(upperLine, "CREATE ") && strings.Contains(upperLine, "FUNCTION")) ||
			(strings.HasPrefix(upperLine, "ALTER ") && strings.Contains(upperLine, "FUNCTION")) {
			result = append(result, line)
			continue
		}

		// Check if this looks like a complete statement that needs a semicolon
		needsSemicolon := strings.HasPrefix(upperLine, "CREATE ") ||
			strings.HasPrefix(upperLine, "ALTER ") ||
			strings.HasPrefix(upperLine, "DROP ") ||
			strings.HasPrefix(upperLine, "INSERT ") ||
			strings.HasPrefix(upperLine, "UPDATE ") ||
			strings.HasPrefix(upperLine, "DELETE ") ||
			strings.HasPrefix(upperLine, "GRANT ") ||
			strings.HasPrefix(upperLine, "REVOKE ")

		if needsSemicolon {
			result = append(result, line+";")
			fixedCount++
			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Added missing semicolon to: %s", trimmed[:min(50, len(trimmed))])
		} else {
			result = append(result, line)
		}
	}

	if fixedCount > 0 {
		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Added %d missing semicolons", fixedCount)
	}

	return strings.Join(result, "\n")
}

// FixMalformedFunctions repairs common function definition issues from consolidation.
// Issues fixed:
// 1. Missing AS keyword before function body
// 2. Duplicate LANGUAGE clauses
// 3. Standalone LANGUAGE lines after $$
// 4. Multiple volatility markers (STABLE and IMMUTABLE together)
// 5. Orphaned function bodies without CREATE FUNCTION headers
func FixMalformedFunctions(sql string) string {
	lines := strings.Split(sql, "\n")
	var result []string
	fixedCount := 0
	inOrphanedBody := false

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		upperLine := strings.ToUpper(trimmed)

		// Skip empty lines and comments
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			result = append(result, line)
			continue
		}

		// CONSERVATIVE orphaned function body detection
		// Only skip bodies that are truly orphaned (appear after section comments with no CREATE FUNCTION)
		// This is now VERY conservative to avoid corrupting valid multi-line function definitions
		if !inOrphanedBody && (strings.HasPrefix(upperLine, "AS $$") || strings.HasPrefix(upperLine, "LANGUAGE ")) {
			// Look back to find either:
			// 1. CREATE FUNCTION (valid function) - keep the line
			// 2. Section header like "=== FUNCTIONS ===" (orphaned) - skip the line
			foundCreateFunction := false
			foundSectionHeader := false

			// Check up to 100 lines back to handle functions with many parameters
			for j := i - 1; j >= 0 && j >= i-100; j-- {
				prevTrimmed := strings.TrimSpace(lines[j])
				if prevTrimmed == "" || strings.HasPrefix(prevTrimmed, "--") {
					// Check if this is a section header comment
					if strings.Contains(prevTrimmed, "===") {
						foundSectionHeader = true
						break
					}
					continue
				}

				// Found a CREATE FUNCTION - this is valid
				if strings.Contains(strings.ToUpper(prevTrimmed), "CREATE") && strings.Contains(strings.ToUpper(prevTrimmed), "FUNCTION") {
					foundCreateFunction = true
					break
				}

				// If we hit another statement keyword, stop looking
				if strings.HasPrefix(strings.ToUpper(prevTrimmed), "CREATE ") ||
					strings.HasPrefix(strings.ToUpper(prevTrimmed), "ALTER ") ||
					strings.HasPrefix(strings.ToUpper(prevTrimmed), "DROP ") {
					break
				}
			}

			// Only skip if we found a section header AND no CREATE FUNCTION
			if foundSectionHeader && !foundCreateFunction {
				utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Skipping truly orphaned function body at line %d (after section header)", i+1)
				inOrphanedBody = true
				continue
			}
		}

		// Exit orphaned body mode when we see end delimiter
		if inOrphanedBody && strings.Contains(trimmed, "$$") && !strings.HasPrefix(upperLine, "AS $$") {
			inOrphanedBody = false
			continue
		}

		// Skip lines while in orphaned body
		if inOrphanedBody {
			continue
		}

		// Fix: Standalone LANGUAGE lines after $$
		if strings.HasPrefix(upperLine, "LANGUAGE ") && i > 0 {
			prevLine := strings.TrimSpace(lines[i-1])
			if strings.HasSuffix(prevLine, "$$") || strings.HasSuffix(prevLine, "$$;") {
				utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Removing standalone LANGUAGE line after $$")
				fixedCount++
				continue
			}
		}

		// Fix: Volatility marker after $$ (e.g., "$$;STABLE;" or "$$ STABLE;" or "$$;STABLE; CREATE")
		// This happens when function consolidation adds conflicting markers
		if strings.Contains(line, "$$;STABLE") || strings.Contains(line, "$$;VOLATILE") ||
			strings.Contains(line, "$$;IMMUTABLE") || strings.Contains(line, "$$ STABLE") ||
			strings.Contains(line, "$$ VOLATILE") || strings.Contains(line, "$$ IMMUTABLE") {
			// Remove the orphaned volatility marker after $$
			// Pattern handles: $$;STABLE; or $$;STABLE; CREATE or $$ STABLE;
			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Found orphaned volatility marker: %s", line[:min(80, len(line))])
			fixedLine, ok := removeOrphanedVolatilityAfterDollar(line)
			if ok {
				result = append(result, fixedLine)
			} else {
				result = append(result, line)
			}
			fixedCount++
			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Removed orphaned volatility marker after function body")
			continue
		} // Fix: Multiple volatility markers in same line
		if strings.Contains(upperLine, "STABLE") && strings.Contains(upperLine, "IMMUTABLE") {
			// Keep IMMUTABLE, remove STABLE (more restrictive)
			fixedLine := removeKeywordWholeWordCI(line, "STABLE")
			fixedLine = strings.Join(strings.Fields(fixedLine), " ")
			result = append(result, fixedLine)
			fixedCount++
			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Removed STABLE in favor of IMMUTABLE")
			continue
		}

		result = append(result, line)
	}

	if fixedCount > 0 {
		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Fixed %d malformed function issues", fixedCount)
	}

	return strings.Join(result, "\n")
}

// FixMissingLanguageClauses adds LANGUAGE plpgsql to functions that are missing it.
// This is critical for PostgreSQL - functions without explicit LANGUAGE will fail.
func FixMissingLanguageClauses(sql string) string {
	// First, split any concatenated functions that ended up on the same line.
	// Example: END;\n$$;STABLE; CREATE OR REPLACE FUNCTION...
	var splitCount int
	sql, splitCount = splitConcatenatedFunctionBoundaries(sql)
	if splitCount > 0 {
		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Found %d concatenated function patterns to split", splitCount)
	}

	fixed, changed := addMissingLanguageBeforeFunctionBody(sql)
	if changed {
		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Added missing LANGUAGE clauses to functions")
	}

	return fixed
} // RemoveOrphanedAlterStatements removes ALTER statements for objects that don't exist.
// This happens when an object is created and altered in different migrations,
// but the consolidation removes the CREATE.
func RemoveOrphanedAlterStatements(sql string) string {
	lines := strings.Split(sql, "\n")

	// First pass: collect all created objects
	createdObjects := make(map[string]bool)
	for _, line := range lines {
		upperLine := strings.ToUpper(strings.TrimSpace(line))

		// Match: CREATE TABLE [IF NOT EXISTS] schema.name or CREATE TABLE [IF NOT EXISTS] name
		if strings.HasPrefix(upperLine, "CREATE TABLE") {
			parts := strings.Fields(line)
			// Handle both "CREATE TABLE name" and "CREATE TABLE IF NOT EXISTS name"
			nameIndex := 2
			if len(parts) > 4 && strings.ToUpper(parts[2]) == "IF" && strings.ToUpper(parts[3]) == "NOT" && strings.ToUpper(parts[4]) == "EXISTS" {
				nameIndex = 5
			}
			if len(parts) > nameIndex {
				objectName := strings.Trim(parts[nameIndex], `";,()`)
				createdObjects[objectName] = true
			}
		}

		// Match: CREATE [UNIQUE] INDEX [IF NOT EXISTS], CREATE FUNCTION, etc.
		if strings.HasPrefix(upperLine, "CREATE INDEX") ||
			strings.HasPrefix(upperLine, "CREATE UNIQUE INDEX") ||
			strings.HasPrefix(upperLine, "CREATE FUNCTION") ||
			strings.HasPrefix(upperLine, "CREATE TYPE") {
			parts := strings.Fields(line)
			for i, part := range parts {
				partUpper := strings.ToUpper(part)
				if partUpper == "INDEX" || partUpper == "FUNCTION" || partUpper == "TYPE" {
					// Skip "IF NOT EXISTS" if present
					nameIndex := i + 1
					if nameIndex+3 < len(parts) &&
						strings.ToUpper(parts[nameIndex]) == "IF" &&
						strings.ToUpper(parts[nameIndex+1]) == "NOT" &&
						strings.ToUpper(parts[nameIndex+2]) == "EXISTS" {
						nameIndex += 3
					}
					if nameIndex < len(parts) {
						objectName := strings.Trim(parts[nameIndex], `";,()`)
						createdObjects[objectName] = true
					}
					break
				}
			}
		}
	}

	// Second pass: remove ALTER statements for non-existent objects
	var result []string
	removedCount := 0

	for _, line := range lines {
		upperLine := strings.ToUpper(strings.TrimSpace(line))

		// Check if this is an ALTER statement
		if strings.HasPrefix(upperLine, "ALTER TABLE") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				objectName := strings.Trim(parts[2], `";,()`)
				if !createdObjects[objectName] {
					utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Removing orphaned ALTER TABLE for non-existent object: %s", objectName)
					removedCount++
					continue
				}
			}
		}

		result = append(result, line)
	}

	if removedCount > 0 {
		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Removed %d orphaned ALTER statements", removedCount)
	}

	return strings.Join(result, "\n")
}

func normalizeDropPolicyQualifiedName(sql string) string {
	lines := strings.Split(sql, "\n")
	for i, line := range lines {
		fixed, changed := normalizeDropPolicyLine(line)
		if changed {
			lines[i] = fixed
		}
	}
	return strings.Join(lines, "\n")
}

func normalizeDropPolicyLine(line string) (string, bool) {
	parts := strings.Fields(line)
	if len(parts) < 5 {
		return line, false
	}

	if !strings.EqualFold(parts[0], "DROP") || !strings.EqualFold(parts[1], "POLICY") {
		return line, false
	}

	nameIdx := 2
	if len(parts) > 4 && strings.EqualFold(parts[2], "IF") && strings.EqualFold(parts[3], "EXISTS") {
		nameIdx = 4
	}
	if len(parts) <= nameIdx+1 || !strings.EqualFold(parts[nameIdx+1], "ON") {
		return line, false
	}

	policy := strings.TrimSuffix(parts[nameIdx], ";")
	segments := splitDotQualifiedIdentifier(policy)
	if len(segments) < 3 {
		return line, false
	}

	parts[nameIdx] = segments[len(segments)-1]
	return strings.Join(parts, " "), true
}

func removeOrphanedVolatilityAfterDollar(line string) (string, bool) {
	upper := strings.ToUpper(line)
	idx := strings.Index(upper, "$$")
	if idx == -1 {
		return line, false
	}

	pos := idx + 2
	if pos < len(line) && line[pos] == ';' {
		pos++
	}
	for pos < len(line) && isSyntaxWhitespace(line[pos]) {
		pos++
	}

	keywordEnd := pos
	if strings.HasPrefix(upper[pos:], "STABLE") {
		keywordEnd = pos + len("STABLE")
	} else if strings.HasPrefix(upper[pos:], "VOLATILE") {
		keywordEnd = pos + len("VOLATILE")
	} else if strings.HasPrefix(upper[pos:], "IMMUTABLE") {
		keywordEnd = pos + len("IMMUTABLE")
	} else {
		return line, false
	}

	if keywordEnd < len(line) && isSyntaxIdentifierByte(line[keywordEnd]) {
		return line, false
	}

	pos = keywordEnd
	if pos < len(line) && line[pos] == ';' {
		pos++
	}
	for pos < len(line) && isSyntaxWhitespace(line[pos]) {
		pos++
	}

	return line[:idx] + "$$;\n" + line[pos:], true
}

func removeKeywordWholeWordCI(input string, keyword string) string {
	if input == "" || keyword == "" {
		return input
	}

	var out strings.Builder
	cursor := 0

	for {
		rel := strings.Index(strings.ToUpper(input[cursor:]), strings.ToUpper(keyword))
		if rel == -1 {
			out.WriteString(input[cursor:])
			break
		}

		idx := cursor + rel
		end := idx + len(keyword)

		beforeOK := idx == 0 || !isSyntaxIdentifierByte(input[idx-1])
		afterOK := end >= len(input) || !isSyntaxIdentifierByte(input[end])

		if beforeOK && afterOK && strings.EqualFold(input[idx:end], keyword) {
			out.WriteString(input[cursor:idx])
			cursor = end
			continue
		}

		out.WriteString(input[cursor : idx+1])
		cursor = idx + 1
	}

	return out.String()
}

func splitConcatenatedFunctionBoundaries(sql string) (string, int) {
	if sql == "" {
		return sql, 0
	}

	var out strings.Builder
	out.Grow(len(sql) + 16)

	splits := 0
	for i := 0; i < len(sql); {
		if i+2 < len(sql) && sql[i] == '$' && sql[i+1] == '$' && sql[i+2] == ';' {
			out.WriteString("$$;")
			j := i + 3

			j = skipSyntaxWhitespace(sql, j)
			if keyword, next, ok := readVolatilityKeyword(sql, j); ok {
				_ = keyword
				j = next
				if j < len(sql) && sql[j] == ';' {
					j++
				}
				j = skipSyntaxWhitespace(sql, j)
				if hasSyntaxKeywordAt(sql, j, "CREATE") {
					out.WriteString("\n\nCREATE ")
					i = skipSyntaxWhitespace(sql, j+len("CREATE"))
					splits++
					continue
				}
			}

			i += 3
			continue
		}

		out.WriteByte(sql[i])
		i++
	}

	return out.String(), splits
}

func addMissingLanguageBeforeFunctionBody(sql string) (string, bool) {
	if strings.TrimSpace(sql) == "" {
		return sql, false
	}

	var out strings.Builder
	out.Grow(len(sql) + 32)

	changed := false
	cursor := 0

	for {
		createIdx, ok := findCreateFunctionStart(sql, cursor)
		if !ok {
			out.WriteString(sql[cursor:])
			break
		}

		out.WriteString(sql[cursor:createIdx])

		asStart, asEnd, ok := findFunctionAsDollar(sql, createIdx)
		if !ok {
			out.WriteString(sql[createIdx:])
			break
		}

		header := sql[createIdx:asStart]
		headerUpper := strings.ToUpper(header)
		if strings.Contains(headerUpper, "RETURNS") && !strings.Contains(headerUpper, "LANGUAGE") {
			out.WriteString(header)
			out.WriteString(" LANGUAGE plpgsql ")
			changed = true
		} else {
			out.WriteString(header)
		}

		out.WriteString(sql[asStart:asEnd])

		closeRel := strings.Index(sql[asEnd:], "$$")
		if closeRel == -1 {
			out.WriteString(sql[asEnd:])
			break
		}

		closeIdx := asEnd + closeRel + 2
		out.WriteString(sql[asEnd:closeIdx])
		cursor = closeIdx
	}

	return out.String(), changed
}

func findCreateFunctionStart(sql string, start int) (int, bool) {
	for i := start; i < len(sql); i++ {
		if !hasSyntaxKeywordAt(sql, i, "CREATE") {
			continue
		}

		pos := skipSyntaxWhitespace(sql, i+len("CREATE"))
		if hasSyntaxKeywordAt(sql, pos, "OR") {
			pos = skipSyntaxWhitespace(sql, pos+len("OR"))
			if !hasSyntaxKeywordAt(sql, pos, "REPLACE") {
				continue
			}
			pos = skipSyntaxWhitespace(sql, pos+len("REPLACE"))
		}

		if hasSyntaxKeywordAt(sql, pos, "FUNCTION") {
			return i, true
		}
	}

	return 0, false
}

func findFunctionAsDollar(sql string, start int) (int, int, bool) {
	for i := start; i < len(sql); i++ {
		if !hasSyntaxKeywordAt(sql, i, "AS") {
			continue
		}

		pos := skipSyntaxWhitespace(sql, i+len("AS"))
		if pos+1 < len(sql) && sql[pos] == '$' && sql[pos+1] == '$' {
			return i, pos + 2, true
		}
	}

	return 0, 0, false
}

func hasSyntaxKeywordAt(value string, pos int, keyword string) bool {
	if pos < 0 || pos+len(keyword) > len(value) {
		return false
	}

	if !strings.EqualFold(value[pos:pos+len(keyword)], keyword) {
		return false
	}

	if pos > 0 && isSyntaxIdentifierByte(value[pos-1]) {
		return false
	}

	end := pos + len(keyword)
	if end < len(value) && isSyntaxIdentifierByte(value[end]) {
		return false
	}

	return true
}

func skipSyntaxWhitespace(value string, pos int) int {
	for pos < len(value) && isSyntaxWhitespace(value[pos]) {
		pos++
	}
	return pos
}

func readVolatilityKeyword(value string, pos int) (string, int, bool) {
	if strings.HasPrefix(strings.ToUpper(value[pos:]), "STABLE") {
		end := pos + len("STABLE")
		if end == len(value) || !isSyntaxIdentifierByte(value[end]) {
			return "STABLE", end, true
		}
	}
	if strings.HasPrefix(strings.ToUpper(value[pos:]), "VOLATILE") {
		end := pos + len("VOLATILE")
		if end == len(value) || !isSyntaxIdentifierByte(value[end]) {
			return "VOLATILE", end, true
		}
	}
	if strings.HasPrefix(strings.ToUpper(value[pos:]), "IMMUTABLE") {
		end := pos + len("IMMUTABLE")
		if end == len(value) || !isSyntaxIdentifierByte(value[end]) {
			return "IMMUTABLE", end, true
		}
	}

	return "", 0, false
}

func splitDotQualifiedIdentifier(value string) []string {
	parts := make([]string, 0)
	var current strings.Builder
	inDoubleQuote := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		parts = append(parts, strings.ReplaceAll(current.String(), "\"", ""))
		current.Reset()
	}

	for i := 0; i < len(value); i++ {
		ch := value[i]
		if ch == '"' {
			inDoubleQuote = !inDoubleQuote
			current.WriteByte(ch)
			continue
		}

		if ch == '.' && !inDoubleQuote {
			flush()
			continue
		}

		current.WriteByte(ch)
	}

	flush()
	return parts
}

func isSyntaxWhitespace(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}

func isSyntaxIdentifierByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}
