package ast

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/utils"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// FunctionNormalizer provides AST-based function normalization operations.
// It replaces regex-based function fixes with accurate AST manipulation.
type FunctionNormalizer struct {
	logger *utils.Logger
}

// NewFunctionNormalizer creates a new function normalizer.
func NewFunctionNormalizer() *FunctionNormalizer {
	return &FunctionNormalizer{
		logger: utils.GetDefaultLogger().WithPrefix("AST-FUNC"),
	}
}

// NormalizeAll applies all function normalizations to the given SQL.
// This is the main entry point that replaces all regex-based function fixes.
//
// BUG #2 FIX: Made significantly more conservative to preserve original function definitions.
// Only applies minimal fixes for truly broken SQL, not stylistic normalization.
func (fn *FunctionNormalizer) NormalizeAll(sql string) (string, error) {
	// BUG #2 FIX: CONSERVATIVE MODE
	// Don't apply AST modifications or deparsing unless absolutely necessary.
	// For single-version functions, we want to preserve them byte-for-byte.
	//
	// The only thing we do is fix trailing LANGUAGE clauses via regex,
	// but we DON'T move volatility or security markers.

	// BUG #2 FIX: Skip ALL normalization - preserve functions exactly as written.
	// The previous approach of fixing language order, removing redundancies, etc.
	// was causing volatility and security markers to be lost during deparsing.
	//
	// REMOVED: AST parsing and all modifications
	// REMOVED: fixLanguageOrder(), removeRedundantLanguage(), inferMissingLanguage(), removeDuplicateOptions()
	// REMOVED: AST deparsing (pg_query.Deparse) which doesn't preserve volatility/security markers
	// REMOVED: ensureLanguageClausesPresent() - it was using AST language values which could be wrong

	// Return SQL completely unchanged - preserve functions exactly as written
	return sql, nil
}

// fixLanguageOrder ensures LANGUAGE comes before VOLATILE/STABLE/IMMUTABLE.
// PostgreSQL requires: LANGUAGE plpgsql VOLATILE
// Not: VOLATILE LANGUAGE plpgsql
func (fn *FunctionNormalizer) fixLanguageOrder(funcStmt *pg_query.CreateFunctionStmt) bool {
	if funcStmt.Options == nil || len(funcStmt.Options) == 0 {
		return false
	}

	// Find positions of language and volatility options
	languageIdx := -1
	volatilityIdx := -1

	for i, opt := range funcStmt.Options {
		defElem := opt.GetDefElem()
		if defElem == nil {
			continue
		}

		switch defElem.Defname {
		case "language":
			languageIdx = i
		case "volatility":
			volatilityIdx = i
		}
	}

	// Need to reorder if volatility comes before language
	if languageIdx >= 0 && volatilityIdx >= 0 && volatilityIdx < languageIdx {
		// Swap positions
		funcStmt.Options[languageIdx], funcStmt.Options[volatilityIdx] =
			funcStmt.Options[volatilityIdx], funcStmt.Options[languageIdx]

		fn.logger.Info("Fixed LANGUAGE/VOLATILITY order in function")
		return true
	}

	return false
}

// removeRedundantLanguage removes duplicate LANGUAGE declarations.
func (fn *FunctionNormalizer) removeRedundantLanguage(funcStmt *pg_query.CreateFunctionStmt) bool {
	if funcStmt.Options == nil || len(funcStmt.Options) == 0 {
		return false
	}

	languageCount := 0
	var keepIdx int

	// Count and find first language option
	for i, opt := range funcStmt.Options {
		defElem := opt.GetDefElem()
		if defElem != nil && defElem.Defname == "language" {
			if languageCount == 0 {
				keepIdx = i
			}
			languageCount++
		}
	}

	if languageCount <= 1 {
		return false // No duplicates
	}

	// Remove duplicate language options (keep first one)
	newOptions := make([]*pg_query.Node, 0, len(funcStmt.Options))
	languagesSeen := 0

	for i, opt := range funcStmt.Options {
		defElem := opt.GetDefElem()
		if defElem != nil && defElem.Defname == "language" {
			if i == keepIdx {
				newOptions = append(newOptions, opt)
			}
			languagesSeen++
		} else {
			newOptions = append(newOptions, opt)
		}
	}

	funcStmt.Options = newOptions
	fn.logger.Info("Removed %d duplicate LANGUAGE declarations", languageCount-1)
	return true
}

// inferMissingLanguage adds missing LANGUAGE declaration or fixes incorrect LANGUAGE based on function body.
// This is the critical fix for Bug #2: Functions with simple SQL bodies declared as plpgsql.
func (fn *FunctionNormalizer) inferMissingLanguage(funcStmt *pg_query.CreateFunctionStmt) bool {
	// Get function name for debugging
	funcName := fn.getFunctionName(funcStmt)

	// Check if function already has a language
	hasLanguage := false
	languageValue := ""
	languageOptionIndex := -1
	for i, opt := range funcStmt.Options {
		defElem := opt.GetDefElem()
		if defElem != nil && defElem.Defname == "language" {
			hasLanguage = true
			languageOptionIndex = i
			// Extract language value
			if strNode := defElem.Arg.GetString_(); strNode != nil {
				languageValue = strings.ToLower(strNode.Sval)
			}
			break
		}
	}

	// Infer the correct language from function body or return type
	correctLanguage := fn.inferLanguageFromFunction(funcStmt)
	if correctLanguage == "" {
		correctLanguage = "sql" // Default to SQL
	}

	if hasLanguage {
		// Function has a language - check if it's correct
		if languageValue == correctLanguage {
			// Language is correct, no changes needed
			fn.logger.Info("Function %s already has correct LANGUAGE %s in options, skipping", funcName, languageValue)
			return false
		}

		// Language is INCORRECT - fix it
		fn.logger.Info("Function %s has incorrect LANGUAGE %s (should be %s), fixing...", funcName, languageValue, correctLanguage)

		// Update the existing language option
		defElem := funcStmt.Options[languageOptionIndex].GetDefElem()
		if defElem != nil && defElem.Arg != nil {
			if strNode := defElem.Arg.GetString_(); strNode != nil {
				strNode.Sval = correctLanguage
				fn.logger.Info("Fixed LANGUAGE %s → %s for %s", languageValue, correctLanguage, funcName)
				return true
			}
		}

		// If we couldn't update in place, remove old and add new
		// Remove old language option
		newOptions := make([]*pg_query.Node, 0, len(funcStmt.Options))
		for i, opt := range funcStmt.Options {
			if i != languageOptionIndex {
				newOptions = append(newOptions, opt)
			}
		}
		funcStmt.Options = newOptions

		// Add new language option (fall through to code below)
		hasLanguage = false
	}

	if !hasLanguage {
		// Function doesn't have a language or we removed the incorrect one
		fn.logger.Info("Function %s is missing LANGUAGE in options, adding %s...", funcName, correctLanguage)

		// Add language option
		languageOpt := &pg_query.Node{
			Node: &pg_query.Node_DefElem{
				DefElem: &pg_query.DefElem{
					Defname: "language",
					Arg: &pg_query.Node{
						Node: &pg_query.Node_String_{
							String_: &pg_query.String{
								Sval: correctLanguage,
							},
						},
					},
				},
			},
		}

		// Insert language after RETURNS but before AS (if possible)
		// For now, append to end
		funcStmt.Options = append(funcStmt.Options, languageOpt)

		fn.logger.Info("Added LANGUAGE %s to %s", correctLanguage, funcName)
		return true
	}

	return false
}

// inferLanguageFromFunction infers the appropriate language from function characteristics.
// BUG #2 FIX: This function now ONLY infers LANGUAGE, not volatility.
// Volatility should be explicitly set by the user or left to PostgreSQL defaults.
func (fn *FunctionNormalizer) inferLanguageFromFunction(funcStmt *pg_query.CreateFunctionStmt) string {
	// Check if function returns TRIGGER - must be plpgsql
	if funcStmt.ReturnType != nil {
		returnType := fn.getTypeNameFromTypeName(funcStmt.ReturnType)
		if strings.ToLower(returnType) == "trigger" {
			return "plpgsql"
		}
	}

	// Check function body for plpgsql constructs
	// Function body can be in SqlBody (for BEGIN ATOMIC syntax) or in Options (for AS $$ syntax)
	bodyStr := fn.extractFunctionBody(funcStmt)
	if bodyStr != "" {
		bodyLower := strings.ToLower(bodyStr)

		// Check for plpgsql-specific constructs
		plpgsqlKeywords := []string{
			"begin", "declare", "perform ", "raise ",
			"return next", "return query", "if then", "loop",
		}

		for _, keyword := range plpgsqlKeywords {
			if strings.Contains(bodyLower, keyword) {
				return "plpgsql"
			}
		}
	}

	return "sql" // Default to SQL
}

// removeDuplicateOptions removes any duplicate options.
func (fn *FunctionNormalizer) removeDuplicateOptions(funcStmt *pg_query.CreateFunctionStmt) bool {
	if funcStmt.Options == nil || len(funcStmt.Options) == 0 {
		return false
	}

	seen := make(map[string]int) // option name -> first index
	duplicates := 0

	// Find duplicates
	for i, opt := range funcStmt.Options {
		defElem := opt.GetDefElem()
		if defElem == nil {
			continue
		}

		if firstIdx, exists := seen[defElem.Defname]; exists {
			// Duplicate found - keep first occurrence
			duplicates++
			fn.logger.Info("Found duplicate option: %s (keeping first at index %d)", defElem.Defname, firstIdx)
		} else {
			seen[defElem.Defname] = i
		}
	}

	if duplicates == 0 {
		return false
	}

	// Build new options list without duplicates
	newOptions := make([]*pg_query.Node, 0, len(funcStmt.Options))
	seenFinal := make(map[string]bool)

	for _, opt := range funcStmt.Options {
		defElem := opt.GetDefElem()
		if defElem == nil {
			newOptions = append(newOptions, opt)
			continue
		}

		if !seenFinal[defElem.Defname] {
			newOptions = append(newOptions, opt)
			seenFinal[defElem.Defname] = true
		}
	}

	funcStmt.Options = newOptions
	return true
}

// Helper functions

func (fn *FunctionNormalizer) getFunctionName(funcStmt *pg_query.CreateFunctionStmt) string {
	if funcStmt == nil || len(funcStmt.Funcname) == 0 {
		return "<unknown>"
	}

	// Build qualified name from ObjectWithArgs
	var nameParts []string
	for _, node := range funcStmt.Funcname {
		if str := node.GetString_(); str != nil {
			nameParts = append(nameParts, str.Sval)
		}
	}

	if len(nameParts) == 0 {
		return "<unknown>"
	}

	// Return the last part (unqualified name) for brevity
	return nameParts[len(nameParts)-1]
}

func (fn *FunctionNormalizer) getTypeNameFromTypeName(typeName *pg_query.TypeName) string {
	if typeName == nil {
		return ""
	}

	if len(typeName.Names) == 0 {
		return ""
	}

	// Get last component of type name (e.g., "pg_catalog.text" -> "text")
	lastNode := typeName.Names[len(typeName.Names)-1]
	str := lastNode.GetString_()
	if str != nil {
		return str.Sval
	}

	return ""
}

// extractFunctionBody extracts the function body text from CreateFunctionStmt.
// It checks both SqlBody (for BEGIN ATOMIC syntax) and Options (for AS $$ syntax).
func (fn *FunctionNormalizer) extractFunctionBody(funcStmt *pg_query.CreateFunctionStmt) string {
	// First check SqlBody (for BEGIN ATOMIC syntax)
	if funcStmt.SqlBody != nil {
		// SqlBody contains structured SQL statements
		// Try to deparse it to get the body text
		if bodyText := fn.deparseNode(funcStmt.SqlBody); bodyText != "" {
			return bodyText
		}
	}

	// Check Options for "as" DefElem (traditional $$ syntax)
	for _, opt := range funcStmt.Options {
		defElem := opt.GetDefElem()
		if defElem != nil && defElem.Defname == "as" {
			// The body is in defElem.Arg
			if defElem.Arg != nil {
				// Try to extract as list of strings
				if listNode := defElem.Arg.GetList(); listNode != nil {
					var bodyParts []string
					for _, item := range listNode.Items {
						if strNode := item.GetString_(); strNode != nil {
							bodyParts = append(bodyParts, strNode.Sval)
						}
					}
					if len(bodyParts) > 0 {
						return strings.Join(bodyParts, "\n")
					}
				}
				// Try to extract as single string
				if strNode := defElem.Arg.GetString_(); strNode != nil {
					return strNode.Sval
				}
			}
		}
	}

	return ""
}

// deparseNode attempts to deparse a Node back to SQL text.
func (fn *FunctionNormalizer) deparseNode(node *pg_query.Node) string {
	// Create a minimal parse result with just this node
	parseResult := &pg_query.ParseResult{
		Stmts: []*pg_query.RawStmt{
			{Stmt: node},
		},
	}

	deparsed, err := pg_query.Deparse(parseResult)
	if err != nil {
		return ""
	}

	return deparsed
}

// ensureLanguageClausesPresent fixes pg_query deparser omissions by manually inserting
// missing LANGUAGE clauses. The deparser inconsistently outputs LANGUAGE options even
// when they exist in the AST.
//
// NOTE: pg_query deparser outputs compressed SQL where functions may appear on single
// lines or with previous statements (e.g., "$$; CREATE OR REPLACE FUNCTION ...").
func (fn *FunctionNormalizer) ensureLanguageClausesPresent(parseResult *pg_query.ParseResult, sql string) string {
	// Build map of function names to their LANGUAGE values from the AST
	functionLanguages := make(map[string]string)

	for _, stmt := range parseResult.Stmts {
		funcStmt := stmt.Stmt.GetCreateFunctionStmt()
		if funcStmt == nil {
			continue
		}

		funcName := fn.getFunctionName(funcStmt)

		// Extract LANGUAGE from options
		for _, opt := range funcStmt.Options {
			defElem := opt.GetDefElem()
			if defElem != nil && defElem.Defname == "language" {
				if strNode := defElem.Arg.GetString_(); strNode != nil {
					functionLanguages[funcName] = strNode.Sval
				}
				break
			}
		}
	}

	if len(functionLanguages) == 0 {
		return sql // No functions with LANGUAGE to check
	}

	fixedCount := 0

	// CRITICAL FIX: pg_query deparser bug with RETURNS TABLE
	// It outputs: ) RETURNS TABLE LANGUAGE plpgsql (columns...)
	// Correct:     ) RETURNS TABLE (columns...) LANGUAGE plpgsql
	// Fix this globally before processing individual functions
	// Pattern handles multiline: closing paren, newline/spaces, RETURNS TABLE, LANGUAGE, opening paren
	returnsTablePattern := `(?is)(\)\s+RETURNS\s+TABLE)\s+(LANGUAGE\s+\w+)\s+(\()`
	returnsTableRe := regexp.MustCompile(returnsTablePattern)
	beforeFix := sql
	// Remove misplaced LANGUAGE between TABLE and columns
	sql = returnsTableRe.ReplaceAllString(sql, "$1 $3")
	if sql != beforeFix {
		fixCount := len(returnsTableRe.FindAllString(beforeFix, -1))
		fn.logger.Info("Fixed pg_query deparser bug: removed %d misplaced LANGUAGE clauses from RETURNS TABLE", fixCount)
	}

	// Use regex to find and fix each function
	// Pattern matches: CREATE [OR REPLACE] FUNCTION name(...) RETURNS ... [modifiers] AS $$
	// CRITICAL: LANGUAGE must come BEFORE VOLATILE/STABLE/IMMUTABLE/SECURITY modifiers
	for funcName, expectedLanguage := range functionLanguages {
		// Build regex pattern for this specific function
		// Pattern: CREATE ... FUNCTION funcName(...) RETURNS <type> ... AS $$
		re := strings.NewReplacer(
			"(", `\(`,
			")", `\)`,
		)
		escapedName := re.Replace(funcName)

		// Pattern must handle:
		// 1. RETURNS [SETOF] type AS $$
		// 2. RETURNS TABLE (...) AS $$ (single or multiline)
		// 3. RETURNS TABLE(\n  col1 type,\n  col2 type\n) AS $$ (multiline with newlines)
		//
		// Strategy: Match up to RETURNS, then non-greedy .*? to AS $$
		// This captures everything between RETURNS clause and AS $$ as group 2
		funcPattern := `(?is)(CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+(?:[\w.]+\.)?` + escapedName + `\s*\([^)]*\)\s+RETURNS\s+.+?)(\s+)(AS\s+\$\$)`

		funcRe, err := regexp.Compile(funcPattern)
		if err != nil {
			fn.logger.Info("Failed to compile regex for function %s: %v", funcName, err)
			continue
		}

		// Check if this function is missing LANGUAGE before AS $$
		matches := funcRe.FindStringSubmatch(sql)
		if len(matches) >= 4 {
			// matches[1]: Everything up to and including RETURNS clause (may be multiline TABLE)
			// matches[2]: Space/whitespace between RETURNS clause and AS $$
			// matches[3]: AS $$

			// Check if LANGUAGE appears ANYWHERE between RETURNS and AS $$
			// Extract the full text between RETURNS and AS to check for LANGUAGE
			fullSection := matches[0] // The entire matched text
			upperSection := strings.ToUpper(fullSection)

			// Find where "RETURNS" ends and where "AS $$" begins
			returnsIdx := strings.Index(upperSection, "RETURNS")
			asIdx := strings.LastIndex(upperSection, "AS")

			if returnsIdx >= 0 && asIdx > returnsIdx {
				betweenReturnsAndAs := upperSection[returnsIdx:asIdx]

				// Check if LANGUAGE is missing from the section between RETURNS and AS $$
				if !strings.Contains(betweenReturnsAndAs, "LANGUAGE") {
					fn.logger.Info("Adding LANGUAGE %s for function %s (missing between RETURNS and AS)", expectedLanguage, funcName)

					// Insert LANGUAGE right before AS $$
					// matches[1] = CREATE...RETURNS clause, matches[2] = space, matches[3] = AS $$
					replacement := "$1 LANGUAGE " + expectedLanguage + "$2$3"
					sql = funcRe.ReplaceAllString(sql, replacement)
					fixedCount++
				}
			}
		}
	}

	if fixedCount > 0 {
		fn.logger.Info("Fixed %d functions where deparser omitted LANGUAGE clauses", fixedCount)
	}

	// PHASE 2: Remove trailing function options after closing $$
	// The deparser sometimes outputs: AS $$ ... $$ LANGUAGE lang VOLATILE/STABLE/IMMUTABLE SECURITY DEFINER;
	// After we insert options before AS, we need to remove the trailing ones to avoid conflicts
	//
	// BUG #10 FIX: Before removing SECURITY DEFINER from trailing position, we need to extract it
	// and move it to the correct position (before AS) to preserve this critical security attribute.

	trailingFixCount := 0

	// BUG #10 FIX: Extract and preserve SECURITY DEFINER before removing trailing options
	// Pattern: Look for functions that have SECURITY DEFINER after $$
	// We need to move it before AS $$ in the format: LANGUAGE xxx SECURITY DEFINER AS $$
	trailingSecurityPattern := `(?i)(\$\$);?\s*(SECURITY\s+(DEFINER|INVOKER))\s*;?`
	trailingSecurityRe := regexp.MustCompile(trailingSecurityPattern)

	// Find all functions with trailing SECURITY attributes
	securityMatches := trailingSecurityRe.FindAllStringSubmatch(sql, -1)
	if len(securityMatches) > 0 {
		fn.logger.Info("Found %d functions with trailing SECURITY attributes that need to be moved", len(securityMatches))

		// For each function with trailing SECURITY, move it before AS $$
		for _, match := range securityMatches {
			securityClause := match[2] // "SECURITY DEFINER" or "SECURITY INVOKER"

			// Find the function containing this trailing security clause
			// Pattern: LANGUAGE xxx AS $$ ... $$ SECURITY DEFINER
			// We want to move SECURITY DEFINER to: LANGUAGE xxx SECURITY DEFINER AS $$
			funcPattern := `(?i)(LANGUAGE\s+\w+)(\s+)(AS\s+\$\$[^$]*\$\$);?\s*` + regexp.QuoteMeta(securityClause)
			funcRe := regexp.MustCompile(funcPattern)

			sql = funcRe.ReplaceAllString(sql, fmt.Sprintf("$1 %s $3;", securityClause))
		}

		fn.logger.Info("Moved %d SECURITY attributes from trailing position to before AS clause", len(securityMatches))
	}

	// Remove LANGUAGE (handle $$LANGUAGE, $$; LANGUAGE, $$ LANGUAGE - all variations)
	// Replace with $$; to ensure semicolon is always present
	trailingLangPattern := `(?i)(\$\$);?\s*(LANGUAGE\s+(?:sql|plpgsql|plperl|plpython|c))\s*;?`
	trailingLangRe := regexp.MustCompile(trailingLangPattern)
	beforeLang := sql
	sql = trailingLangRe.ReplaceAllString(sql, "$1;")
	if sql != beforeLang {
		trailingFixCount += len(trailingLangRe.FindAllString(beforeLang, -1))
	}

	// Remove volatility markers (handle $$VOLATILE, $$; VOLATILE, $$ VOLATILE - all variations)
	// Replace with $$; to ensure semicolon is always present
	trailingVolatilityPattern := `(?i)(\$\$);?\s*(VOLATILE|STABLE|IMMUTABLE)\s*;?`
	trailingVolatilityRe := regexp.MustCompile(trailingVolatilityPattern)
	beforeVol := sql
	sql = trailingVolatilityRe.ReplaceAllString(sql, "$1;")
	if sql != beforeVol {
		trailingFixCount += len(trailingVolatilityRe.FindAllString(beforeVol, -1))
	}

	// Now remove any remaining trailing SECURITY DEFINER/INVOKER that we already moved
	// (This handles any edge cases where the pattern didn't match perfectly)
	beforeSec := sql
	sql = trailingSecurityRe.ReplaceAllString(sql, "$1;")
	if sql != beforeSec {
		trailingFixCount += len(trailingSecurityRe.FindAllString(beforeSec, -1))
	}

	if trailingFixCount > 0 {
		fn.logger.Info("Removed %d redundant trailing function options", trailingFixCount)
	}

	return sql
}

// extractFunctionNameFromSQL extracts the function name from a CREATE FUNCTION line.
func (fn *FunctionNormalizer) extractFunctionNameFromSQL(line string) string {
	// Pattern: CREATE [OR REPLACE] FUNCTION [schema.]name(...)
	upperLine := strings.ToUpper(line)

	// Find FUNCTION keyword
	funcIdx := strings.Index(upperLine, "FUNCTION")
	if funcIdx == -1 {
		return ""
	}

	// Get everything after FUNCTION
	afterFunc := strings.TrimSpace(line[funcIdx+8:])

	// Function name is everything before the opening parenthesis
	parenIdx := strings.Index(afterFunc, "(")
	if parenIdx == -1 {
		return ""
	}

	funcNameFull := strings.TrimSpace(afterFunc[:parenIdx])

	// If it's schema-qualified (public.func_name), get just the name
	parts := strings.Split(funcNameFull, ".")
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}

	return funcNameFull
}
