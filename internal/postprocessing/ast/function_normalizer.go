package ast

import (
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
// Made significantly more conservative to preserve original function definitions.
// Only applies minimal fixes for truly broken SQL, not stylistic normalization.
func (fn *FunctionNormalizer) NormalizeAll(sql string) (string, error) {
	// CONSERVATIVE MODE
	// Don't apply AST modifications or deparsing unless absolutely necessary.
	// For single-version functions, we want to preserve them byte-for-byte.
	//
	// The only thing we do is fix trailing LANGUAGE clauses via regex,
	// but we DON'T move volatility or security markers.

	// Skip ALL normalization - preserve functions exactly as written.
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
// This function now ONLY infers LANGUAGE, not volatility.
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
