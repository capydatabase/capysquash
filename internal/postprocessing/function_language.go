package postprocessing

import (
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/utils"
)

// FixFunctionLanguageConflicts fixes functions with conflicting VOLATILE/LANGUAGE placement.
// This is a safety net post-processor that catches any functions where normalization didn't run
// or where SQL transformation added volatility markers incorrectly.
//
// Problem Pattern (Invalid PostgreSQL syntax):
//
//	CREATE FUNCTION foo() RETURNS text VOLATILE AS $$ ... $$ LANGUAGE plpgsql;
//
// Fixed Pattern (Valid):
//
//	CREATE FUNCTION foo() RETURNS text LANGUAGE plpgsql VOLATILE AS $$ ... $$;
//
// Root Cause:
// When volatility markers (VOLATILE/STABLE/IMMUTABLE) are added before AS $$,
// PostgreSQL requires LANGUAGE to also be before AS $$, not after the function body.
//
// This function detects and fixes three scenarios:
// 1. VOLATILE AS $$ ... $$ LANGUAGE plpgsql → LANGUAGE plpgsql VOLATILE AS $$ ... $$
// 2. STABLE AS $$ ... $$ LANGUAGE plpgsql → LANGUAGE plpgsql STABLE AS $$ ... $$
// 3. IMMUTABLE AS $$ ... $$ LANGUAGE plpgsql → LANGUAGE plpgsql IMMUTABLE AS $$ ... $$
func FixFunctionLanguageConflicts(sql string) string {
	blocks := parseFunctionBlocks(sql)
	if len(blocks) == 0 {
		return sql
	}

	var out strings.Builder
	out.Grow(len(sql) + 32)

	cursor := 0
	fixedCount := 0

	for _, block := range blocks {
		out.WriteString(sql[cursor:block.start])

		fixedStatement, changed, volatility := fixFunctionLanguageConflictBlock(sql, block)
		if changed {
			fixedCount++
			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info(
				"Fixed function with conflicting %s placement (moved LANGUAGE before AS)",
				volatility,
			)
			out.WriteString(fixedStatement)
		} else {
			out.WriteString(sql[block.start:block.end])
		}

		cursor = block.end
	}

	out.WriteString(sql[cursor:])

	if fixedCount > 0 {
		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Fixed %d functions with conflicting VOLATILE/STABLE/IMMUTABLE and LANGUAGE placement", fixedCount)
	}

	return out.String()
}

// FixRedundantTrailingLanguageClauses removes redundant LANGUAGE clauses that appear after
// the closing $$ delimiter in function definitions. PostgreSQL rejects these as
// "conflicting or redundant options" when LANGUAGE is also in the header.
//
// This function uses a line-by-line state machine to intelligently detect:
//  1. Functions with LANGUAGE before AS $ (from AST normalization) → remove trailing LANGUAGE
//  2. Functions without LANGUAGE before AS $ → keep trailing LANGUAGE (it's the only one)
//
// This avoids regex complexity and only removes truly redundant clauses.
//
// EXECUTION: Called in post-processing phase BEFORE FixFunctionLanguageConflicts.
func FixRedundantTrailingLanguageClauses(sql string) string {
	lines := strings.Split(sql, "\n")
	var result []string
	removedCount := 0

	// State machine tracking
	inFunction := false
	hasLanguageInHeader := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upperLine := strings.ToUpper(trimmed)

		// Detect function start
		if strings.HasPrefix(upperLine, "CREATE FUNCTION") ||
			strings.HasPrefix(upperLine, "CREATE OR REPLACE FUNCTION") {
			inFunction = true
			hasLanguageInHeader = false
			result = append(result, line)
			continue
		}

		if !inFunction {
			result = append(result, line)
			continue
		}

		// We're inside a function definition

		// Check if this line contains LANGUAGE (before AS $)
		if !hasLanguageInHeader && strings.Contains(upperLine, "LANGUAGE") &&
			!strings.Contains(upperLine, "AS $$") {
			hasLanguageInHeader = true
		}

		// Check if this line contains AS $ (start of function body)
		if strings.Contains(upperLine, "AS $$") || strings.Contains(upperLine, "AS $") {
			// Check if LANGUAGE is on the same line as AS $
			beforeAS := upperLine
			if before, _, ok := strings.Cut(upperLine, "AS $"); ok {
				beforeAS = before
			}
			if strings.Contains(beforeAS, "LANGUAGE") {
				hasLanguageInHeader = true
			}
		}

		// Detect function end: $$ LANGUAGE ... ; or $$;
		// This is the trailing LANGUAGE clause we might remove
		if strings.Contains(trimmed, "$$") && strings.HasSuffix(trimmed, ";") {
			// Check if this line has trailing LANGUAGE after $$
			hasTrailingLanguage := false
			afterDollar := ""

			if _, after, ok := strings.Cut(trimmed, "$$"); ok {
				afterDollar = strings.TrimSpace(after)
				afterDollarUpper := strings.ToUpper(afterDollar)
				hasTrailingLanguage = strings.HasPrefix(afterDollarUpper, "LANGUAGE")
			}

			if hasTrailingLanguage {
				// Decision: Remove trailing LANGUAGE only if header already has it
				if hasLanguageInHeader {
					// Remove trailing LANGUAGE - replace with just $$;
					cleanLine := trimmed[:strings.Index(trimmed, "$$")] + "$$;"
					result = append(result, cleanLine)
					removedCount++
					utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info(
						"Removed redundant trailing LANGUAGE clause (header already has LANGUAGE)",
					)
				} else {
					// Keep trailing LANGUAGE - it's the only one!
					result = append(result, line)
					utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info(
						"Kept trailing LANGUAGE clause (no LANGUAGE in header)",
					)
				}
			} else {
				// No trailing LANGUAGE - just add the line
				result = append(result, line)
			}

			// Reset state for next function
			inFunction = false
			hasLanguageInHeader = false
			continue
		}

		// Regular line inside function
		result = append(result, line)
	}

	if removedCount > 0 {
		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info(
			"Intelligently removed %d redundant trailing LANGUAGE clauses (kept clauses where they were the only LANGUAGE)",
			removedCount,
		)
	}

	return strings.Join(result, "\n")
}

// FixIncorrectLanguageDeclarations fixes functions that have incorrect LANGUAGE declarations
// based on their body content.
//
// Fixes TWO directions:
//  1. LANGUAGE SQL → LANGUAGE plpgsql (when body has plpgsql constructs like BEGIN/END)
//  2. LANGUAGE plpgsql → LANGUAGE sql (when body is simple SQL without plpgsql constructs)
//
// Common patterns:
//   - RETURNS TRIGGER + LANGUAGE SQL → should be LANGUAGE plpgsql (triggers require plpgsql)
//   - Body has BEGIN/END + LANGUAGE SQL → should be LANGUAGE plpgsql
//   - Body has bare SELECT + LANGUAGE plpgsql → should be LANGUAGE sql
//   - Body has DECLARE + LANGUAGE SQL → should be LANGUAGE plpgsql
//   - Body has PERFORM + LANGUAGE SQL → should be LANGUAGE plpgsql
func FixIncorrectLanguageDeclarations(sql string) string {
	blocks := parseFunctionBlocks(sql)
	if len(blocks) == 0 {
		return sql
	}

	var out strings.Builder
	out.Grow(len(sql) + 32)

	cursor := 0
	fixedCount := 0

	for _, block := range blocks {
		out.WriteString(sql[cursor:block.start])

		header := sql[block.start:block.asStart]
		language, hasLanguage := extractHeaderLanguage(header)
		if !hasLanguage {
			out.WriteString(sql[block.start:block.end])
			cursor = block.end
			continue
		}

		bodyLower := strings.ToLower(sql[block.bodyStart:block.closeStart])
		signatureLower := strings.ToLower(header)

		hasCoreConstructs := functionBodyHasCorePlpgsqlConstructs(bodyLower)
		hasExtendedConstructs := functionBodyHasExtendedPlpgsqlConstructs(bodyLower)
		isTrigger := strings.Contains(signatureLower, "returns trigger")
		bodyTrimmed := strings.TrimSpace(bodyLower)

		desiredLanguage := strings.ToLower(language)
		logMessage := ""

		switch strings.ToLower(language) {
		case "sql":
			if hasCoreConstructs || isTrigger {
				desiredLanguage = "plpgsql"
				logMessage = "Fixed incorrect language declaration: LANGUAGE SQL → LANGUAGE plpgsql (body contains plpgsql constructs or returns TRIGGER)"
			}
		case "plpgsql":
			if !hasExtendedConstructs && !isTrigger && isSimpleSQLBody(bodyTrimmed) {
				desiredLanguage = "sql"
				logMessage = "Fixed incorrect language declaration: LANGUAGE plpgsql → LANGUAGE sql (body is simple SQL without plpgsql constructs)"
			}
		}

		if logMessage == "" {
			out.WriteString(sql[block.start:block.end])
			cursor = block.end
			continue
		}

		updatedHeader, ok := replaceHeaderLanguage(header, desiredLanguage)
		if !ok {
			out.WriteString(sql[block.start:block.end])
			cursor = block.end
			continue
		}

		tail, hasSemicolon := blockTail(sql, block)
		out.WriteString(rebuildFunctionBlock(updatedHeader, sql[block.asStart:block.bodyStart], sql[block.bodyStart:block.closeStart], sql[block.closeStart:block.closeEnd], tail, hasSemicolon))
		cursor = block.end
		fixedCount++

		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("%s", logMessage)
	}

	out.WriteString(sql[cursor:])

	if fixedCount > 0 {
		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info(
			"Fixed %d incorrect LANGUAGE declarations (bidirectional: sql↔plpgsql)",
			fixedCount,
		)
	}

	return out.String()
}

// FixMissingLanguageDeclarations adds LANGUAGE declarations to functions that are missing them.
// PostgreSQL requires all functions to have a LANGUAGE clause.
func FixMissingLanguageDeclarations(sql string) string {
	blocks := parseFunctionBlocks(sql)
	if len(blocks) == 0 {
		return sql
	}

	var out strings.Builder
	out.Grow(len(sql) + 32)

	cursor := 0
	fixedCount := 0

	for _, block := range blocks {
		out.WriteString(sql[cursor:block.start])

		header := sql[block.start:block.asStart]
		headerUpper := strings.ToUpper(header)

		if !strings.Contains(headerUpper, "RETURNS") || containsLanguageClause(header) {
			out.WriteString(sql[block.start:block.end])
			cursor = block.end
			continue
		}

		tail, hasSemicolon := blockTail(sql, block)
		if tailHasLeadingLanguageClause(tail) {
			// Preserve valid legacy form where LANGUAGE appears after the body.
			out.WriteString(sql[block.start:block.end])
			cursor = block.end
			continue
		}

		bodyLower := strings.ToLower(sql[block.bodyStart:block.closeStart])
		hasPlpgsqlConstructs := functionBodyHasCorePlpgsqlConstructs(bodyLower)
		isTrigger := strings.Contains(strings.ToLower(header), "returns trigger")

		language := "sql"
		if hasPlpgsqlConstructs || isTrigger {
			language = "plpgsql"
		}

		updatedHeader := strings.TrimRight(header, " \t\r\n") + " LANGUAGE " + language
		out.WriteString(rebuildFunctionBlock(updatedHeader, sql[block.asStart:block.bodyStart], sql[block.bodyStart:block.closeStart], sql[block.closeStart:block.closeEnd], tail, hasSemicolon))
		cursor = block.end
		fixedCount++

		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info(
			"Added missing LANGUAGE declaration: LANGUAGE %s (inferred from %s)",
			language,
			map[bool]string{true: "body constructs", false: "function signature"}[hasPlpgsqlConstructs],
		)
	}

	out.WriteString(sql[cursor:])

	if fixedCount > 0 {
		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info(
			"Added %d missing LANGUAGE declarations",
			fixedCount,
		)
	}

	return out.String()
}

// RemoveDuplicateLanguageDeclarations removes duplicate LANGUAGE clauses from functions.
// Example: "LANGUAGE plpgsql STABLE LANGUAGE plpgsql" → "LANGUAGE plpgsql STABLE"
// NOTE: This function is currently disabled in the processing pipeline due to regex complexity.
// It should be reimplemented using AST-based approach instead of regex.
func RemoveDuplicateLanguageDeclarations(sql string) string {
	fixedSQL := sql
	fixedCount := 0

	cursor := 0
	for {
		createIdx, ok := findCreateFunctionStart(fixedSQL, cursor)
		if !ok {
			break
		}

		asStart, _, ok := findFunctionAsDollar(fixedSQL, createIdx)
		if !ok {
			cursor = createIdx + len("CREATE")
			continue
		}

		signature := fixedSQL[createIdx:asStart]
		cleanSignature, changed := removeDuplicateLanguageClausesFromSignature(signature)
		if changed {
			fixedSQL = fixedSQL[:createIdx] + cleanSignature + fixedSQL[asStart:]
			fixedCount++
			cursor = createIdx + len(cleanSignature)
			utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Removed duplicate LANGUAGE declaration from function")
			continue
		}

		cursor = asStart
	}

	if fixedCount > 0 {
		utils.GetDefaultLogger().WithPrefix("POSTPROCESS").Info("Removed %d duplicate LANGUAGE declarations", fixedCount)
	}

	return fixedSQL
}

type languageClauseRange struct {
	start int
	end   int
}

func removeDuplicateLanguageClausesFromSignature(signature string) (string, bool) {
	clauses := findLanguageClauseRanges(signature)
	if len(clauses) <= 1 {
		return signature, false
	}

	cleaned := signature
	for i := len(clauses) - 1; i >= 1; i-- {
		clause := clauses[i]
		cleaned = cleaned[:clause.start] + cleaned[clause.end:]
	}

	return cleaned, true
}

func findLanguageClauseRanges(signature string) []languageClauseRange {
	clauses := make([]languageClauseRange, 0)

	for i := 0; i < len(signature); i++ {
		if !hasSyntaxKeywordAt(signature, i, "LANGUAGE") {
			continue
		}

		start := i
		if start > 0 && isSyntaxWhitespace(signature[start-1]) {
			start--
		}

		pos := skipSyntaxWhitespace(signature, i+len("LANGUAGE"))
		lang, end, ok := readLanguageName(signature, pos)
		if !ok {
			continue
		}

		switch strings.ToLower(lang) {
		case "plpgsql", "sql", "c", "internal":
			for end < len(signature) && isSyntaxWhitespace(signature[end]) {
				end++
			}
			clauses = append(clauses, languageClauseRange{start: start, end: end})
			i = end - 1
		}
	}

	return clauses
}

func readLanguageName(value string, pos int) (string, int, bool) {
	if pos >= len(value) || !isSyntaxIdentifierByte(value[pos]) {
		return "", 0, false
	}

	i := pos
	for i < len(value) && isSyntaxIdentifierByte(value[i]) {
		i++
	}

	return value[pos:i], i, true
}

type functionBlock struct {
	start      int
	asStart    int
	bodyStart  int
	bodyEnd    int
	closeStart int
	closeEnd   int
	semicolon  int
	end        int
}

func parseFunctionBlocks(sql string) []functionBlock {
	blocks := make([]functionBlock, 0)
	cursor := 0

	for {
		createIdx, ok := findCreateFunctionStart(sql, cursor)
		if !ok {
			break
		}

		asStart, bodyStart, delimiter, ok := findFunctionBodyDelimiter(sql, createIdx)
		if !ok {
			cursor = createIdx + len("CREATE")
			continue
		}

		bodyEnd, closeEnd, ok := findFunctionBodyEnd(sql, bodyStart, delimiter)
		if !ok {
			cursor = bodyStart
			continue
		}

		semicolon := findFunctionSemicolon(sql, closeEnd)
		end := len(sql)
		if semicolon >= 0 {
			end = semicolon + 1
		}

		blocks = append(blocks, functionBlock{
			start:      createIdx,
			asStart:    asStart,
			bodyStart:  bodyStart,
			bodyEnd:    bodyEnd,
			closeStart: bodyEnd,
			closeEnd:   closeEnd,
			semicolon:  semicolon,
			end:        end,
		})

		cursor = end
	}

	return blocks
}

func findFunctionBodyDelimiter(sql string, start int) (int, int, string, bool) {
	for i := start; i < len(sql); i++ {
		if !hasSyntaxKeywordAt(sql, i, "AS") {
			continue
		}

		pos := skipSyntaxWhitespace(sql, i+len("AS"))
		if pos >= len(sql) {
			continue
		}

		if sql[pos] == '$' {
			end := pos + 1
			for end < len(sql) && isFunctionDollarTagByte(sql[end]) {
				end++
			}
			if end < len(sql) && sql[end] == '$' {
				return i, end + 1, sql[pos : end+1], true
			}
		}

		if sql[pos] == '\'' {
			return i, pos + 1, "'", true
		}
	}

	return 0, 0, "", false
}

func findFunctionBodyEnd(sql string, bodyStart int, delimiter string) (int, int, bool) {
	if delimiter == "'" {
		for i := bodyStart; i < len(sql); i++ {
			if sql[i] != '\'' {
				continue
			}

			if i+1 < len(sql) && sql[i+1] == '\'' {
				i++
				continue
			}

			return i, i + 1, true
		}
		return 0, 0, false
	}

	rel := strings.Index(sql[bodyStart:], delimiter)
	if rel == -1 {
		return 0, 0, false
	}

	bodyEnd := bodyStart + rel
	return bodyEnd, bodyEnd + len(delimiter), true
}

func findFunctionSemicolon(sql string, start int) int {
	if start < 0 || start >= len(sql) {
		return -1
	}

	if rel := strings.IndexByte(sql[start:], ';'); rel != -1 {
		return start + rel
	}

	return -1
}

func fixFunctionLanguageConflictBlock(sql string, block functionBlock) (string, bool, string) {
	header := sql[block.start:block.asStart]
	if !containsVolatilityClause(header) {
		return "", false, ""
	}

	tail, hasSemicolon := blockTail(sql, block)
	languageClause, tailRemainder, ok := parseTrailingLanguageClause(tail)
	if !ok {
		return "", false, ""
	}

	updatedHeader := header
	if !containsLanguageClause(updatedHeader) {
		updatedHeader = insertClauseBeforeVolatility(updatedHeader, languageClause)
	}

	volatility, _, _ := firstVolatilityClause(updatedHeader)
	statement := rebuildFunctionBlock(
		updatedHeader,
		sql[block.asStart:block.bodyStart],
		sql[block.bodyStart:block.closeStart],
		sql[block.closeStart:block.closeEnd],
		tailRemainder,
		hasSemicolon,
	)

	return statement, true, strings.ToUpper(volatility)
}

func blockTail(sql string, block functionBlock) (string, bool) {
	if block.semicolon >= 0 {
		return sql[block.closeEnd:block.semicolon], true
	}
	return sql[block.closeEnd:block.end], false
}

func parseTrailingLanguageClause(tail string) (string, string, bool) {
	trimmed := strings.TrimSpace(tail)
	if trimmed == "" || !hasSyntaxKeywordAt(trimmed, 0, "LANGUAGE") {
		return "", "", false
	}

	pos := skipSyntaxWhitespace(trimmed, len("LANGUAGE"))
	language, next, ok := readLanguageName(trimmed, pos)
	if !ok {
		return "", "", false
	}
	pos = skipSyntaxWhitespace(trimmed, next)

	clause := "LANGUAGE " + language

	if hasSyntaxKeywordAt(trimmed, pos, "SECURITY") {
		securityPos := skipSyntaxWhitespace(trimmed, pos+len("SECURITY"))
		switch {
		case hasSyntaxKeywordAt(trimmed, securityPos, "DEFINER"):
			clause += " SECURITY DEFINER"
			pos = skipSyntaxWhitespace(trimmed, securityPos+len("DEFINER"))
		case hasSyntaxKeywordAt(trimmed, securityPos, "INVOKER"):
			clause += " SECURITY INVOKER"
			pos = skipSyntaxWhitespace(trimmed, securityPos+len("INVOKER"))
		}
	}

	return clause, strings.TrimSpace(trimmed[pos:]), true
}

func extractHeaderLanguage(header string) (string, bool) {
	for i := 0; i < len(header); i++ {
		if !hasSyntaxKeywordAt(header, i, "LANGUAGE") {
			continue
		}

		pos := skipSyntaxWhitespace(header, i+len("LANGUAGE"))
		language, _, ok := readLanguageName(header, pos)
		if !ok {
			return "", false
		}

		return language, true
	}

	return "", false
}

func replaceHeaderLanguage(header string, language string) (string, bool) {
	for i := 0; i < len(header); i++ {
		if !hasSyntaxKeywordAt(header, i, "LANGUAGE") {
			continue
		}

		pos := skipSyntaxWhitespace(header, i+len("LANGUAGE"))
		_, end, ok := readLanguageName(header, pos)
		if !ok {
			return "", false
		}

		return header[:pos] + language + header[end:], true
	}

	return "", false
}

func containsLanguageClause(header string) bool {
	_, ok := extractHeaderLanguage(header)
	return ok
}

func containsVolatilityClause(header string) bool {
	_, _, ok := firstVolatilityClause(header)
	return ok
}

func firstVolatilityClause(header string) (string, int, bool) {
	for i := 0; i < len(header); i++ {
		if i > 0 && isSyntaxIdentifierByte(header[i-1]) {
			continue
		}

		volatility, _, ok := readVolatilityKeyword(header, i)
		if ok {
			return volatility, i, true
		}
	}

	return "", 0, false
}

func insertClauseBeforeVolatility(header string, clause string) string {
	trimmedHeader := strings.TrimRight(header, " \t\r\n")
	trimmedClause := strings.TrimSpace(clause)
	if trimmedClause == "" {
		return trimmedHeader
	}

	_, index, found := firstVolatilityClause(trimmedHeader)
	if !found {
		return trimmedHeader + " " + trimmedClause
	}

	prefix := strings.TrimRight(trimmedHeader[:index], " \t\r\n")
	suffix := strings.TrimLeft(trimmedHeader[index:], " \t\r\n")
	if prefix == "" {
		return trimmedClause + " " + suffix
	}

	return prefix + " " + trimmedClause + " " + suffix
}

func tailHasLeadingLanguageClause(tail string) bool {
	trimmed := strings.TrimSpace(tail)
	return trimmed != "" && hasSyntaxKeywordAt(trimmed, 0, "LANGUAGE")
}

func functionBodyHasCorePlpgsqlConstructs(body string) bool {
	return strings.Contains(body, "begin") ||
		strings.Contains(body, "declare") ||
		strings.Contains(body, "perform ") ||
		strings.Contains(body, "raise ") ||
		strings.Contains(body, "return next") ||
		strings.Contains(body, "return query")
}

func functionBodyHasExtendedPlpgsqlConstructs(body string) bool {
	return functionBodyHasCorePlpgsqlConstructs(body) ||
		strings.Contains(body, "if ") ||
		strings.Contains(body, "loop") ||
		strings.Contains(body, "while ") ||
		strings.Contains(body, "for ")
}

func isSimpleSQLBody(body string) bool {
	return strings.HasPrefix(body, "select ") ||
		strings.HasPrefix(body, "insert ") ||
		strings.HasPrefix(body, "update ") ||
		strings.HasPrefix(body, "delete ") ||
		strings.HasPrefix(body, "return ")
}

func rebuildFunctionBlock(header, asClause, body, closingDelimiter, tail string, hasSemicolon bool) string {
	var out strings.Builder
	out.Grow(len(header) + len(asClause) + len(body) + len(closingDelimiter) + len(tail) + 4)

	out.WriteString(strings.TrimRight(header, " \t\r\n"))
	out.WriteString(" ")
	out.WriteString(strings.TrimLeft(asClause, " \t\r\n"))
	out.WriteString(body)
	out.WriteString(closingDelimiter)

	if strings.TrimSpace(tail) != "" {
		out.WriteString(" ")
		out.WriteString(strings.TrimSpace(tail))
	}

	if hasSemicolon {
		out.WriteString(";")
	}

	return out.String()
}

func isFunctionDollarTagByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}
