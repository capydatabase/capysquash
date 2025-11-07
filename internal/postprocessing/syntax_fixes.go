package postprocessing

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/utils"
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
			fixedLine := regexp.MustCompile(`(?i)\$\$;?\s*(STABLE|VOLATILE|IMMUTABLE);?\s*`).ReplaceAllString(line, "$$;\n")
			result = append(result, fixedLine)
			fixedCount++
			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Removed orphaned volatility marker after function body")
			continue
		}		// Fix: Multiple volatility markers in same line
		if strings.Contains(upperLine, "STABLE") && strings.Contains(upperLine, "IMMUTABLE") {
			// Keep IMMUTABLE, remove STABLE (more restrictive)
			fixedLine := regexp.MustCompile(`(?i)\bSTABLE\b`).ReplaceAllString(line, "")
			fixedLine = regexp.MustCompile(`\s+`).ReplaceAllString(fixedLine, " ")
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
	// First, split any concatenated functions that ended up on the same line
	// Pattern: $$; followed by volatility marker, then CREATE on same line
	// Example: END;\n$$;STABLE; CREATE OR REPLACE FUNCTION...
	// Handles both "$$; STABLE; CREATE" (with space) and "$$;STABLE;CREATE" (no space)
	pattern := regexp.MustCompile(`(?i)(\$\$;)\s*(STABLE|VOLATILE|IMMUTABLE);?\s*(CREATE\s+)`)
	matches := pattern.FindAllString(sql, -1)
	if len(matches) > 0 {
		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Found %d concatenated function patterns to split", len(matches))
	}
	sql = pattern.ReplaceAllString(sql, "$1\n\n$3")

	// Pattern: CREATE FUNCTION ... RETURNS ... AS $$
	// Need to insert LANGUAGE plpgsql before AS $$
	// This handles both single-line and multiline function definitions

	funcPattern := regexp.MustCompile(`(?is)(CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+[^;]+?RETURNS\s+[^;]+?)\s+(AS\s+\$\$)`)

	fixed := funcPattern.ReplaceAllStringFunc(sql, func(match string) string {
		// Check if LANGUAGE already exists in the match
		if strings.Contains(strings.ToUpper(match), "LANGUAGE") {
			return match
		}

		// Find the AS $$ part and insert LANGUAGE before it
		asIdx := strings.LastIndex(strings.ToUpper(match), "AS")
		if asIdx < 0 {
			return match
		}

		before := match[:asIdx]
		after := match[asIdx:]

		return before + " LANGUAGE plpgsql " + after
	})

	if fixed != sql {
		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Added missing LANGUAGE clauses to functions")
	}

	return fixed
}// RemoveOrphanedAlterStatements removes ALTER statements for objects that don't exist.
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
