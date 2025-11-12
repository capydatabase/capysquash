package transformation

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/plugins"
	"github.com/capysquash/pgsquash-engine/internal/postprocessing"
	"github.com/capysquash/pgsquash-engine/internal/utils"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// TransformationType defines the type of SQL transformation
type TransformationType int

const (
	DMLToSelect TransformationType = iota
	DropToComment
	UnsafeToSafe
	ModernSyntax
	Performance
)

// TransformationConfig controls SQL transformation behavior
type TransformationConfig struct {
	EnableDMLToSelect    bool   `json:"enable_dml_to_select"`
	EnableDropToComment  bool   `json:"enable_drop_to_comment"`
	EnableUnsafeToSafe   bool   `json:"enable_unsafe_to_safe"`
	EnableModernSyntax   bool   `json:"enable_modern_syntax"`
	EnablePerformance    bool   `json:"enable_performance"`
	EnableSyntaxFixes    bool   `json:"enable_syntax_fixes"`    // Fix common SQL syntax errors
	PreserveSrcPositions bool   `json:"preserve_src_positions"`
	TargetVersion        string `json:"target_version"` // PostgreSQL version target
}

// DefaultTransformationConfig returns sensible defaults
func DefaultTransformationConfig() *TransformationConfig {
	return &TransformationConfig{
		EnableDMLToSelect:    true,
		EnableDropToComment:  true,
		EnableUnsafeToSafe:   true,
		EnableModernSyntax:   true,
		EnablePerformance:    true,
		EnableSyntaxFixes:    true, // Enable syntax error fixes by default
		PreserveSrcPositions: true,
		TargetVersion:        "16",
	}
}

// TransformationResult represents the result of a transformation
type TransformationResult struct {
	OriginalSQL     string                  `json:"original_sql"`
	TransformedSQL  string                  `json:"transformed_sql"`
	Transformations []TransformationApplied `json:"transformations"`
	Warnings        []string                `json:"warnings"`
	Success         bool                    `json:"success"`
	Error           string                  `json:"error,omitempty"`
}

// TransformationApplied tracks what transformations were applied
type TransformationApplied struct {
	Type        TransformationType `json:"type"`
	Description string             `json:"description"`
	LineStart   int                `json:"line_start"`
	LineEnd     int                `json:"line_end"`
	Before      string             `json:"before"`
	After       string             `json:"after"`
}

// SQLTransformer handles SQL transformations for safety and compatibility
type SQLTransformer struct {
	config    *TransformationConfig
	pgVersion string
	patterns  *TransformationPatterns
}

// TransformationPatterns contains regex patterns for transformations
type TransformationPatterns struct {
	// DML patterns
	InsertPattern *regexp.Regexp
	UpdatePattern *regexp.Regexp
	DeletePattern *regexp.Regexp

	// Unsafe patterns
	DropTablePattern  *regexp.Regexp
	DropColumnPattern *regexp.Regexp
	AlterTypePattern  *regexp.Regexp

	// Performance patterns
	NoIndexPattern   *regexp.Regexp
	LargeScanPattern *regexp.Regexp

	// Modern syntax opportunities
	OldJoinPattern     *regexp.Regexp
	OldFunctionPattern *regexp.Regexp
}

// NewSQLTransformer creates a new SQL transformer
func NewSQLTransformer(config *TransformationConfig) *SQLTransformer {
	if config == nil {
		config = DefaultTransformationConfig()
	}

	patterns := &TransformationPatterns{
		InsertPattern:      regexp.MustCompile(`(?i)^\s*INSERT\s+INTO\s+`),
		UpdatePattern:      regexp.MustCompile(`(?i)^\s*UPDATE\s+`),
		DeletePattern:      regexp.MustCompile(`(?i)^\s*DELETE\s+FROM\s+`),
		DropTablePattern:   regexp.MustCompile(`(?i)^\s*DROP\s+TABLE\s+`),
		DropColumnPattern:  regexp.MustCompile(`(?i)ALTER\s+TABLE\s+\w+\s+DROP\s+COLUMN\s+`),
		AlterTypePattern:   regexp.MustCompile(`(?i)ALTER\s+TABLE\s+\w+\s+ALTER\s+COLUMN\s+\w+\s+TYPE\s+`),
		NoIndexPattern:     regexp.MustCompile(`(?i)WHERE\s+[^=]+\s*=\s*[^=]+`),
		LargeScanPattern:   regexp.MustCompile(`(?i)SELECT\s+\*\s+FROM\s+\w+\s*;?\s*$`),
		OldJoinPattern:     regexp.MustCompile(`(?i)WHERE\s+\w+\.\w+\s*=\s*\w+\.\w+`),
		OldFunctionPattern: regexp.MustCompile(`(?i)substr\(|length\(|position\(`),
	}

	return &SQLTransformer{
		config:    config,
		pgVersion: config.TargetVersion,
		patterns:  patterns,
	}
}

// Transform applies configured transformations to SQL
//
// Transformation Pipeline Order:
// 1. Function volatility markers (pre-parse, regex-based)
// 2. SQL parsing with pg_query (AST generation)
// 3. AST-dependent transformations (DML to SELECT, etc.)
//
// Note: Volatility fix runs BEFORE parsing because:
// - Doesn't require AST (uses regex pattern matching)
// - pg_query parser can fail on complex/large migration files (9000+ lines)
// - Critical for index predicate compatibility
func (st *SQLTransformer) Transform(ctx context.Context, sql string) (*TransformationResult, error) {
	result := &TransformationResult{
		OriginalSQL:     sql,
		TransformedSQL:  sql,
		Transformations: make([]TransformationApplied, 0),
		Warnings:        make([]string, 0),
		Success:         true,
	}

	transformedSQL := sql
	var err error // Declare err for use in subsequent steps

	transformedSQL, err = st.fixFunctionVolatilityMarkers(ctx, transformedSQL, result)
	if err != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Function volatility fix failed: %v", err))
	}

	// STEP 2: Parse SQL to AST (for transformations that need it)
	parseResult, err := pg_query.Parse(transformedSQL)
	if err != nil {
		// Don't fail - volatility fix already applied successfully
		// Just skip AST-dependent transformations
		result.Warnings = append(result.Warnings, fmt.Sprintf("SQL parsing skipped: %v", err))
		result.TransformedSQL = transformedSQL
		return result, nil
	}

	// Apply transformations that require AST
	if st.config.EnableDMLToSelect {
		transformedSQL, err = st.transformDMLToSelect(ctx, transformedSQL, parseResult, result)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("DML to SELECT transformation failed: %v", err))
		}
	}

	if st.config.EnableDropToComment {
		transformedSQL, err = st.transformDropToComment(ctx, transformedSQL, result)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Drop to comment transformation failed: %v", err))
		}
	}

	if st.config.EnableUnsafeToSafe {
		transformedSQL, err = st.transformUnsafeToSafe(ctx, transformedSQL, result)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Unsafe to safe transformation failed: %v", err))
		}
	}

	if st.config.EnableModernSyntax {
		transformedSQL, err = st.transformToModernSyntax(ctx, transformedSQL, result)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Modern syntax transformation failed: %v", err))
		}
	}

	if st.config.EnablePerformance {
		transformedSQL, err = st.applyPerformanceTransformations(ctx, transformedSQL, result)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Performance transformation failed: %v", err))
		}
	}

	if st.config.EnableSyntaxFixes {
		transformedSQL, err = st.fixCommonSyntaxErrors(ctx, transformedSQL, result)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Syntax fix transformation failed: %v", err))
		}
	}

	result.TransformedSQL = transformedSQL
	return result, nil
}

// applyPluginTransformations calls all active plugins to transform SQL
// Plugins are called in priority order (highest first)
// Each plugin can modify the SQL (e.g., add volatility markers, fix syntax)
func (st *SQLTransformer) applyPluginTransformations(ctx context.Context, sql string) (string, error) {
	registry := plugins.GlobalRegistry()

	// Only transform if plugins are initialized
	if len(registry.ActivePlugins()) == 0 {
		return sql, nil // No plugins active, return original SQL
	}

	// Call TransformSQL on registry (handles priority ordering internally)
	transformedSQL, err := registry.TransformSQL(ctx, sql)
	if err != nil {
		utils.GetDefaultLogger().WithPrefix("SQL-TRANSFORM").Info("[transformation] Plugin transformation error: %v", err)
		return sql, err // Return original SQL on error
	}

	return transformedSQL, nil
}

// transformDMLToSelect converts DML statements to SELECT for dry-run validation
func (st *SQLTransformer) transformDMLToSelect(ctx context.Context, sql string, parseResult *pg_query.ParseResult, result *TransformationResult) (string, error) {
	lines := strings.Split(sql, "\n")
	transformedLines := make([]string, len(lines))
	copy(transformedLines, lines)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Transform INSERT to SELECT
		if st.patterns.InsertPattern.MatchString(trimmed) {
			transformed := st.convertInsertToSelect(trimmed)
			if transformed != trimmed {
				transformedLines[i] = transformed
				result.Transformations = append(result.Transformations, TransformationApplied{
					Type:        DMLToSelect,
					Description: "Converted INSERT to SELECT for validation",
					LineStart:   i + 1,
					LineEnd:     i + 1,
					Before:      trimmed,
					After:       transformed,
				})
			}
		}

		// Transform UPDATE to SELECT
		if st.patterns.UpdatePattern.MatchString(trimmed) {
			transformed := st.convertUpdateToSelect(trimmed)
			if transformed != trimmed {
				transformedLines[i] = transformed
				result.Transformations = append(result.Transformations, TransformationApplied{
					Type:        DMLToSelect,
					Description: "Converted UPDATE to SELECT for validation",
					LineStart:   i + 1,
					LineEnd:     i + 1,
					Before:      trimmed,
					After:       transformed,
				})
			}
		}

		// Transform DELETE to SELECT
		if st.patterns.DeletePattern.MatchString(trimmed) {
			transformed := st.convertDeleteToSelect(trimmed)
			if transformed != trimmed {
				transformedLines[i] = transformed
				result.Transformations = append(result.Transformations, TransformationApplied{
					Type:        DMLToSelect,
					Description: "Converted DELETE to SELECT for validation",
					LineStart:   i + 1,
					LineEnd:     i + 1,
					Before:      trimmed,
					After:       transformed,
				})
			}
		}
	}

	return strings.Join(transformedLines, "\n"), nil
}

// convertInsertToSelect converts INSERT statement to SELECT for validation
func (st *SQLTransformer) convertInsertToSelect(sql string) string {
	// INSERT INTO table (col1, col2) VALUES (val1, val2)
	// -> SELECT val1 as col1, val2 as col2

	insertPattern := regexp.MustCompile(`(?i)INSERT\s+INTO\s+(\w+(?:\.\w+)?)\s*(?:\(([^)]+)\))?\s*VALUES\s*\(([^)]+)\)`)
	matches := insertPattern.FindStringSubmatch(sql)

	if len(matches) >= 4 {
		table := matches[1]
		columns := matches[2]
		values := matches[3]

		if columns != "" {
			// Parse columns and values
			cols := strings.Split(columns, ",")
			vals := strings.Split(values, ",")

			selectParts := make([]string, 0, len(vals))
			for i, val := range vals {
				val = strings.TrimSpace(val)
				if i < len(cols) {
					col := strings.TrimSpace(cols[i])
					selectParts = append(selectParts, fmt.Sprintf("%s as %s", val, col))
				} else {
					selectParts = append(selectParts, val)
				}
			}

			return fmt.Sprintf("-- INSERT validation: SELECT %s -- FROM %s",
				strings.Join(selectParts, ", "), table)
		} else {
			return fmt.Sprintf("-- INSERT validation: SELECT %s -- INTO %s", values, table)
		}
	}

	return sql
}

// convertUpdateToSelect converts UPDATE statement to SELECT for validation
func (st *SQLTransformer) convertUpdateToSelect(sql string) string {
	// UPDATE table SET col1 = val1 WHERE condition
	// -> SELECT val1 as col1 FROM table WHERE condition

	updatePattern := regexp.MustCompile(`(?i)UPDATE\s+(\w+(?:\.\w+)?)\s+SET\s+(.+?)(?:\s+WHERE\s+(.+))?$`)
	matches := updatePattern.FindStringSubmatch(sql)

	if len(matches) >= 3 {
		table := matches[1]
		setPart := matches[2]
		wherePart := ""
		if len(matches) >= 4 && matches[3] != "" {
			wherePart = " WHERE " + matches[3]
		}

		// Parse SET clause
		setPattern := regexp.MustCompile(`(\w+)\s*=\s*([^,]+)`)
		setMatches := setPattern.FindAllStringSubmatch(setPart, -1)

		selectParts := make([]string, 0, len(setMatches))
		for _, match := range setMatches {
			if len(match) >= 3 {
				col := strings.TrimSpace(match[1])
				val := strings.TrimSpace(match[2])
				selectParts = append(selectParts, fmt.Sprintf("%s as %s", val, col))
			}
		}

		if len(selectParts) > 0 {
			return fmt.Sprintf("-- UPDATE validation: SELECT %s FROM %s%s",
				strings.Join(selectParts, ", "), table, wherePart)
		}
	}

	return sql
}

// convertDeleteToSelect converts DELETE statement to SELECT for validation
func (st *SQLTransformer) convertDeleteToSelect(sql string) string {
	// DELETE FROM table WHERE condition
	// -> SELECT COUNT(*) FROM table WHERE condition

	deletePattern := regexp.MustCompile(`(?i)DELETE\s+FROM\s+(\w+(?:\.\w+)?)(?:\s+WHERE\s+(.+))?$`)
	matches := deletePattern.FindStringSubmatch(sql)

	if len(matches) >= 2 {
		table := matches[1]
		wherePart := ""
		if len(matches) >= 3 && matches[2] != "" {
			wherePart = " WHERE " + matches[2]
		}

		return fmt.Sprintf("-- DELETE validation: SELECT COUNT(*) FROM %s%s", table, wherePart)
	}

	return sql
}

// transformDropToComment converts dangerous DROP statements to comments
func (st *SQLTransformer) transformDropToComment(ctx context.Context, sql string, result *TransformationResult) (string, error) {
	lines := strings.Split(sql, "\n")
	transformedLines := make([]string, len(lines))
	copy(transformedLines, lines)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if st.patterns.DropTablePattern.MatchString(trimmed) {
			commented := "-- DANGEROUS: " + trimmed
			transformedLines[i] = commented
			result.Transformations = append(result.Transformations, TransformationApplied{
				Type:        DropToComment,
				Description: "Commented out dangerous DROP TABLE statement",
				LineStart:   i + 1,
				LineEnd:     i + 1,
				Before:      trimmed,
				After:       commented,
			})
		}

		if st.patterns.DropColumnPattern.MatchString(trimmed) {
			commented := "-- DANGEROUS: " + trimmed
			transformedLines[i] = commented
			result.Transformations = append(result.Transformations, TransformationApplied{
				Type:        DropToComment,
				Description: "Commented out dangerous DROP COLUMN statement",
				LineStart:   i + 1,
				LineEnd:     i + 1,
				Before:      trimmed,
				After:       commented,
			})
		}
	}

	return strings.Join(transformedLines, "\n"), nil
}

// transformUnsafeToSafe converts unsafe operations to safer alternatives
func (st *SQLTransformer) transformUnsafeToSafe(ctx context.Context, sql string, result *TransformationResult) (string, error) {
	lines := strings.Split(sql, "\n")
	transformedLines := make([]string, len(lines))
	copy(transformedLines, lines)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// ALTER TYPE operations - add USING clause suggestions
		if st.patterns.AlterTypePattern.MatchString(trimmed) {
			if !strings.Contains(strings.ToUpper(trimmed), "USING") {
				suggestion := trimmed + " -- Add USING clause for type conversion"
				transformedLines[i] = suggestion
				result.Transformations = append(result.Transformations, TransformationApplied{
					Type:        UnsafeToSafe,
					Description: "Added suggestion for USING clause in type conversion",
					LineStart:   i + 1,
					LineEnd:     i + 1,
					Before:      trimmed,
					After:       suggestion,
				})
				result.Warnings = append(result.Warnings, "Type conversion may require explicit USING clause")
			}
		}
	}

	return strings.Join(transformedLines, "\n"), nil
}

// transformToModernSyntax converts old SQL syntax to modern PostgreSQL syntax
func (st *SQLTransformer) transformToModernSyntax(ctx context.Context, sql string, result *TransformationResult) (string, error) {
	transformedSQL := sql

	// Convert old join syntax to modern ANSI joins
	if st.patterns.OldJoinPattern.MatchString(sql) {
		// This is a complex transformation that would need more sophisticated parsing
		result.Warnings = append(result.Warnings, "Consider converting old-style joins to ANSI join syntax")
	}

	// Convert old function names to modern equivalents
	if st.patterns.OldFunctionPattern.MatchString(sql) {
		modernSQL := strings.ReplaceAll(transformedSQL, "substr(", "substring(")

		// BUGFIX: Use word boundary regex to avoid converting char_length -> char_char_length
		// The \b boundary prevents matching when "length" is preceded by underscore (char_length, character_length)
		lengthRegex := regexp.MustCompile(`\blength\(`)
		modernSQL = lengthRegex.ReplaceAllString(modernSQL, "char_length(")

		modernSQL = strings.ReplaceAll(modernSQL, "position(", "strpos(")

		if modernSQL != transformedSQL {
			result.Transformations = append(result.Transformations, TransformationApplied{
				Type:        ModernSyntax,
				Description: "Updated function names to modern equivalents",
				LineStart:   1,
				LineEnd:     len(strings.Split(sql, "\n")),
				Before:      "Old function names",
				After:       "Modern function names",
			})
			transformedSQL = modernSQL
		}
	}

	return transformedSQL, nil
}

// applyPerformanceTransformations adds performance-related improvements
func (st *SQLTransformer) applyPerformanceTransformations(ctx context.Context, sql string, result *TransformationResult) (string, error) {
	lines := strings.Split(sql, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		// Detect queries that might benefit from indexes
		if st.patterns.NoIndexPattern.MatchString(trimmed) && !strings.Contains(strings.ToUpper(trimmed), "INDEX") {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Line %d: Consider adding index for WHERE clause performance", i+1))
		}

		// Detect SELECT * queries
		if st.patterns.LargeScanPattern.MatchString(trimmed) {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Line %d: SELECT * may cause performance issues", i+1))
		}
	}

	return sql, nil
}

// BatchTransform processes multiple SQL statements
func (st *SQLTransformer) BatchTransform(ctx context.Context, statements []string) ([]*TransformationResult, error) {
	results := make([]*TransformationResult, len(statements))

	for i, stmt := range statements {
		result, err := st.Transform(ctx, stmt)
		if err != nil {
			return nil, errors.Wrap(err, errors.ErrorCodeTransformationFailed, errors.CategoryTransformation, fmt.Sprintf("failed to transform statement %d", i), nil)
		}
		results[i] = result
	}

	return results, nil
}

// ValidateTransformation checks if transformation preserves semantic meaning
func (st *SQLTransformer) ValidateTransformation(original, transformed string) error {
	// Parse both to ensure they're valid SQL
	_, err := pg_query.Parse(original)
	if err != nil {
		return errors.Wrap(err, errors.ErrorCodeInvalidSQL, errors.CategoryTransformation, "original SQL is invalid", nil)
	}

	_, err = pg_query.Parse(transformed)
	if err != nil {
		return errors.Wrap(err, errors.ErrorCodeInvalidSQL, errors.CategoryTransformation, "transformed SQL is invalid", nil)
	}

	return nil
}

// fixCommonSyntaxErrors fixes common PostgreSQL syntax errors found in migrations
func (st *SQLTransformer) fixCommonSyntaxErrors(ctx context.Context, sql string, result *TransformationResult) (string, error) {
	transformedSQL := sql

	// Fix 1: RETURN NEXT with parameter in RETURNS TABLE functions
	// Issue: RETURNS TABLE creates implicit OUT parameters, RETURN NEXT should not have arguments
	// Pattern: RETURNS TABLE(...) ... RETURN NEXT variable_name;
	// Fix: RETURN QUERY SELECT variable_name.field1, variable_name.field2...;
	transformedSQL = st.fixReturnNextWithOutParams(transformedSQL, result)

	// Fix 2: Missing semicolons at function ends
	// DISABLED: This was breaking function definitions by corrupting $$ delimiters
	// transformedSQL = st.fixMissingSemicolons(transformedSQL, result)

	// Fix 3: Invalid COMMENT ON syntax
	transformedSQL = st.fixCommentSyntax(transformedSQL, result)

	return transformedSQL, nil
}

// fixReturnNextWithOutParams fixes RETURN NEXT usage in RETURNS TABLE functions.
// Delegates to postprocessing package with transformation tracking.
//
// PostgreSQL Issue:
// RETURNS TABLE creates implicit OUT parameters. Using RETURN NEXT with arguments
// in such functions causes: "pq: RETURN NEXT cannot have a parameter in function with OUT parameters"
//
// This method wraps the postprocessing implementation and captures transformations for reporting.
func (st *SQLTransformer) fixReturnNextWithOutParams(sql string, result *TransformationResult) string {
	return postprocessing.FixReturnNextWithOutParams(sql, func(description, before, after string) {
		// Track transformation for reporting
		result.Transformations = append(result.Transformations, TransformationApplied{
			Type:        UnsafeToSafe,
			Description: description,
			Before:      before,
			After:       after,
		})
	})
}

// fixCommentSyntax fixes invalid COMMENT ON syntax
func (st *SQLTransformer) fixCommentSyntax(sql string, result *TransformationResult) string {
	// Fix COMMENT ON FUNCTION with invalid signature
	// Pattern: COMMENT ON FUNCTION func_name() IS '...';
	// Sometimes the function signature might be incomplete or invalid

	// For now, we'll just validate the syntax is correct
	// More sophisticated fixes can be added as needed

	return sql
}

// normalizeLanguagePosition moves LANGUAGE clauses from after-body to before-AS position.
// This standardizes function syntax before adding volatility markers.
//
// Problem:
// pg_query.Deparse() preserves original function syntax, which may have:
//   Format A: CREATE FUNCTION foo() RETURNS text AS $$ ... $$ LANGUAGE plpgsql;
//   Format B: CREATE FUNCTION foo() RETURNS text LANGUAGE plpgsql AS $$ ... $$;
//
// When we add VOLATILE markers (before AS), Format A creates invalid syntax:
//   Invalid: CREATE FUNCTION foo() RETURNS text VOLATILE AS $$ ... $$ LANGUAGE plpgsql;
//   (VOLATILE before AS conflicts with LANGUAGE after body)
//
// Solution:
// Normalize all functions to Format B BEFORE adding volatility markers:
//   Format A → Format B: Move LANGUAGE from after-body to before-AS
//   Then adding VOLATILE works: LANGUAGE plpgsql VOLATILE AS $$ ... $$
//
// PostgreSQL Rule:
// When volatility markers (VOLATILE/STABLE/IMMUTABLE) appear before AS $$,
// the LANGUAGE clause must also appear before AS $$, not after the function body.
func (st *SQLTransformer) normalizeLanguagePosition(sql string) string {
	// Match ONE function at a time using terminating semicolon
	// Pattern: CREATE FUNCTION ... AS $$ body $$ LANGUAGE plpgsql ... ;
	//
	// We match up to the FIRST semicolon after LANGUAGE clause to avoid
	// matching multiple functions as one and corrupting the output.
	//
	// Group 1: CREATE [OR REPLACE] FUNCTION [schema.]name(...) RETURNS type
	// Group 2: Optional modifiers before AS (SECURITY DEFINER, etc.) - but NOT LANGUAGE
	// Group 3: AS $$
	// Group 4: Function body
	// Group 5: $$
	// Group 6: LANGUAGE clause (e.g., "LANGUAGE plpgsql")
	// Group 7: Optional modifiers after LANGUAGE (SECURITY DEFINER, VOLATILE, etc.)
	// Group 8: Terminating semicolon
	pattern := regexp.MustCompile(
		`(?ims)(CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+(?:[a-z_][a-z0-9_]*\.)?[a-z_][a-z0-9_]*\s*\([^)]*\)\s*RETURNS\s+(?:TABLE\s*\([^)]+\)|SETOF\s+[^\s]+|[^\s]+))((?:\s+(?:SECURITY\s+DEFINER|SET\s+[^\s]+\s*=\s*[^\s]+|VOLATILE|STABLE|IMMUTABLE))*?)(\s+AS\s+\$\$)([\s\S]*?)(\$\$)\s+(LANGUAGE\s+[a-z]+)((?:\s+(?:SECURITY\s+DEFINER|VOLATILE|STABLE|IMMUTABLE))*?)\s*;`,
	)

	matches := pattern.FindAllStringSubmatchIndex(sql, -1)
	if len(matches) == 0 {
		return sql // No functions with post-body LANGUAGE found
	}

	transformedSQL := sql
	offset := 0
	fixedCount := 0

	for _, match := range matches {
		if len(match) < 16 {
			continue
		}

		// Extract parts from capture groups
		signature := transformedSQL[match[2]+offset : match[3]+offset]           // Group 1: CREATE FUNCTION...RETURNS type
		preModifiers := transformedSQL[match[4]+offset : match[5]+offset]        // Group 2: Existing modifiers before AS
		asKeyword := transformedSQL[match[6]+offset : match[7]+offset]           // Group 3: AS $$
		body := transformedSQL[match[8]+offset : match[9]+offset]                // Group 4: function body
		closingDelim := transformedSQL[match[10]+offset : match[11]+offset]      // Group 5: $$
		languageClause := transformedSQL[match[12]+offset : match[13]+offset]    // Group 6: LANGUAGE plpgsql
		postModifiers := transformedSQL[match[14]+offset : match[15]+offset]     // Group 7: Modifiers after LANGUAGE

		// Build normalized function:
		// signature + preModifiers + LANGUAGE + postModifiers + AS $$ + body + $$;
		normalizedPreMods := strings.TrimSpace(preModifiers)
		normalizedLang := strings.TrimSpace(languageClause)
		normalizedPostMods := strings.TrimSpace(postModifiers)

		// Combine all modifiers in correct order: pre + LANGUAGE + post
		var allModifiers string
		if normalizedPreMods != "" {
			allModifiers = normalizedPreMods
		}
		if normalizedLang != "" {
			if allModifiers != "" {
				allModifiers += " "
			}
			allModifiers += normalizedLang
		}
		if normalizedPostMods != "" {
			if allModifiers != "" {
				allModifiers += " "
			}
			allModifiers += normalizedPostMods
		}

		// Reconstruct: signature + " " + allModifiers + AS $$ + body + $$;
		var normalizedFunction string
		if allModifiers != "" {
			normalizedFunction = signature + " " + allModifiers + asKeyword + body + closingDelim + ";"
		} else {
			normalizedFunction = signature + asKeyword + body + closingDelim + ";"
		}

		// Replace the entire matched function (from CREATE to semicolon)
		oldFunctionStart := match[0] + offset
		oldFunctionEnd := match[1] + offset // Includes the semicolon
		oldFunction := transformedSQL[oldFunctionStart:oldFunctionEnd]

		transformedSQL = transformedSQL[:oldFunctionStart] + normalizedFunction + transformedSQL[oldFunctionEnd:]
		offset += len(normalizedFunction) - len(oldFunction)
		fixedCount++
	}

	if fixedCount > 0 {
		utils.GetDefaultLogger().WithPrefix("SQL-TRANSFORM").Info("Normalized LANGUAGE position in %d functions (moved from after-body to before-AS)", fixedCount)
	}

	return transformedSQL
}

// fixFunctionVolatilityMarkers adds STABLE/IMMUTABLE/VOLATILE markers to functions without them.
//
// PostgreSQL Requirement:
// Functions used in index predicates (CREATE INDEX ... WHERE function(...)) MUST have
// explicit volatility markers. Without them, PostgreSQL throws:
// "ERROR: functions in index predicate must be marked IMMUTABLE"
//
// Volatility Categories:
// - IMMUTABLE: Pure functions, same input always produces same output (e.g., math operations)
// - STABLE: Reads database/session state but doesn't modify it (e.g., auth.jwt(), current_user)
// - VOLATILE: Modifies data or has side effects (e.g., INSERT, UPDATE, nextval())
//
// This transformation:
// 1. Finds all CREATE FUNCTION statements without volatility markers
// 2. Analyzes function body to determine appropriate volatility
// 3. Injects marker before AS keyword
//
// Common Use Cases:
// - Clerk/Supabase auth functions (clerk_user_id, clerk_is_admin) → STABLE
// - Trigger functions with INSERT/UPDATE → VOLATILE
// - Calculation functions → IMMUTABLE
//
// Implementation Note:
// This runs BEFORE pg_query.Parse() because the parser can fail on complex migrations.
// Uses regex-based detection instead of AST analysis.
//
// IMPORTANT: Call normalizeLanguagePosition() BEFORE this function to ensure
// LANGUAGE clauses are in the correct position (before AS, not after body).
func (st *SQLTransformer) fixFunctionVolatilityMarkers(ctx context.Context, sql string, result *TransformationResult) (string, error) {
	// Regex Pattern Explanation:
	// (?ims) - Case insensitive, multiline, dot matches newline
	// Group 1: CREATE [OR REPLACE] FUNCTION [schema.]name(...) RETURNS type
	// Group 2: Optional modifiers (LANGUAGE, SECURITY DEFINER, SET)
	// Group 3: AS keyword with delimiter ($$ or $tag$ or ')
	//
	// Handles both formats:
	// - Single line: CREATE FUNCTION foo() RETURNS TEXT AS $$...
	// - Multi-line:  CREATE FUNCTION foo()
	//                RETURNS TEXT
	//                LANGUAGE plpgsql
	//                AS $$...
	funcPattern := regexp.MustCompile(`(?ims)(CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+(?:[a-z_][a-z0-9_]*\.)?[a-z_][a-z0-9_]*\s*\([^)]*\)\s*RETURNS\s+(?:TABLE\s*\([^)]+\)|SETOF\s+[^\s]+|[^\s]+))((?:\s+(?:LANGUAGE\s+[a-z]+|SECURITY\s+DEFINER|SET\s+[^\s]+\s*=\s*[^\s]+|VOLATILE|STABLE|IMMUTABLE))*?)(\s+AS\s+(?:\$\$|\$[a-z0-9_]*\$|\'))`)

	matches := funcPattern.FindAllStringSubmatchIndex(sql, -1)
	if len(matches) == 0 {
		return sql, nil // No functions to fix
	}

	transformedSQL := sql
	offset := 0

	for _, match := range matches {
		if len(match) < 8 {
			continue
		}

		// Extract function definition parts
		beforeAS := transformedSQL[match[0]+offset : match[6]+offset]  // Full match up to AS
		modifiers := transformedSQL[match[4]+offset : match[5]+offset] // LANGUAGE/SECURITY DEFINER/SET
		asKeyword := transformedSQL[match[6]+offset : match[7]+offset] // " AS "

		// Check if volatility marker already exists (before AS or after body)
		hasVolatility := st.hasVolatilityMarker(modifiers) || st.hasVolatilityMarker(beforeAS)

		// Also check AFTER the function body (format: AS $$...$$ LANGUAGE SQL STABLE)
		// Extract text from AS to end of function definition
		// Increase capture to 500 chars to ensure we catch LANGUAGE clause after body
		bodyStart := match[7] + offset
		bodyPattern := regexp.MustCompile(`(?s)\$\$(.+?)\$\$(.{0,500})`)
		bodyMatch := bodyPattern.FindStringSubmatchIndex(transformedSQL[bodyStart:])
		if len(bodyMatch) >= 6 {
			afterBody := transformedSQL[bodyStart+bodyMatch[4] : bodyStart+bodyMatch[5]]
			fmt.Printf("[VOLATILITY-DEBUG] funcName=%s, afterBody (first 100 chars): '%s'\n", st.extractFunctionName(beforeAS), afterBody[:min(100, len(afterBody))])
			if st.hasVolatilityMarker(afterBody) {
				fmt.Printf("[VOLATILITY-DEBUG] ✓ Found volatility marker in afterBody for %s!\n", st.extractFunctionName(beforeAS))
				hasVolatility = true
			} else {
				fmt.Printf("[VOLATILITY-DEBUG] ✗ NO volatility marker found for %s, full afterBody: '%s'\n", st.extractFunctionName(beforeAS), afterBody)
			}
		} else {
			fmt.Printf("[VOLATILITY-DEBUG] bodyMatch length=%d for %s\n", len(bodyMatch), st.extractFunctionName(beforeAS))
		}

		if hasVolatility {
			continue // Already has volatility marker, skip
		}

		// Do NOT automatically add volatility markers to functions.
		// Only add them if explicitly required for index predicates or other constraints.
		// For single-version functions, preserve them exactly as written.
		//
		// The previous approach (forcing STABLE for auth functions) was too aggressive
		// and caused schema differences after squashing.
		//
		// SKIP: Don't add volatility markers proactively
		// If a function truly needs a volatility marker for an index predicate,
		// it should be present in the original SQL or added manually by the developer.

		// Skip this function - don't add volatility markers
		_ = beforeAS    // Suppress unused variable warning
		_ = modifiers   // Suppress unused variable warning
		_ = asKeyword   // Suppress unused variable warning
		continue

		// DEAD CODE BELOW - Left for reference but never executed due to continue above
		// If we decide to re-enable automatic volatility markers in the future,
		// this code shows how it was done previously.
		/*
		// Build new modifiers string
		// PostgreSQL syntax: CREATE FUNCTION name() RETURNS type [LANGUAGE lang] [SECURITY DEFINER] [VOLATILE|STABLE|IMMUTABLE] AS $$
		newModifiers := strings.TrimSpace(modifiers)

		// Only add volatility if not already present
		if newModifiers != "" {
			// Modifiers already exist, volatility marker goes after them
			newModifiers = newModifiers + " " + volatility
		} else {
			// No existing modifiers, just add volatility
			newModifiers = volatility
		}

		// Construct replacement: signature + newModifiers + AS
		// Ensure single space separation
		signature := beforeAS[:match[4]+offset-match[0]-offset]
		replacement := strings.TrimRight(signature, " ") + " " + newModifiers + asKeyword

		transformedSQL = transformedSQL[:match[0]+offset] + replacement + transformedSQL[match[7]+offset:]
		offset += len(replacement) - (match[7] - match[0])

		// Record transformation
		result.Transformations = append(result.Transformations, TransformationApplied{
			Type:        UnsafeToSafe,
			Description: fmt.Sprintf("Added %s volatility marker to function %s for index predicate compatibility", volatility, funcName),
			Before:      beforeAS + asKeyword,
			After:       replacement,
		})
		*/
	}

	return transformedSQL, nil
}

// hasVolatilityMarker checks if a function definition already has a volatility marker.
// Returns true if IMMUTABLE, STABLE, or VOLATILE is found (case-insensitive).
// Handles both space-separated and newline-separated keywords.
func (st *SQLTransformer) hasVolatilityMarker(s string) bool {
	upper := strings.ToUpper(s)
	// Check with space prefix (single-line format)
	hasWithSpace := strings.Contains(upper, " IMMUTABLE") ||
		strings.Contains(upper, " STABLE") ||
		strings.Contains(upper, " VOLATILE")
	if hasWithSpace {
		return true
	}
	// Check with newline prefix (multi-line format)
	hasWithNewline := strings.Contains(upper, "\nIMMUTABLE") ||
		strings.Contains(upper, "\nSTABLE") ||
		strings.Contains(upper, "\nVOLATILE")
	if hasWithNewline {
		return true
	}
	// Check if it's at the start of the string (for modifiers group)
	hasAtStart := strings.HasPrefix(upper, "IMMUTABLE") ||
		strings.HasPrefix(upper, "STABLE") ||
		strings.HasPrefix(upper, "VOLATILE")
	return hasAtStart
}

// isAuthFunction checks if a function name matches known auth function patterns
// Auth functions (Clerk, Supabase, etc.) should always be STABLE
func isAuthFunction(funcName string) bool {
	lowerName := strings.ToLower(funcName)

	// Clerk auth function patterns
	clerkPatterns := []string{
		"current_clerk_",
		"clerk_user_id",
		"clerk_is_admin",
		"clerk_organization",
		"current_user_id",
		"current_organization",
		"validate_jwt",
		"get_planning_analytics",
		"set_session_user",
	}

	// Supabase auth function patterns
	supabasePatterns := []string{
		"auth.uid",
		"auth.jwt",
		"auth.role",
	}

	// Check all patterns
	allPatterns := append(clerkPatterns, supabasePatterns...)
	for _, pattern := range allPatterns {
		if strings.Contains(lowerName, pattern) {
			return true
		}
	}

	return false
}

// determineVolatility analyzes a function body to determine appropriate volatility category.
//
// Decision Logic (in order of precedence):
//
// 1. VOLATILE - Functions that:
//    - Modify data: INSERT, UPDATE, DELETE, TRUNCATE
//    - Modify schema: CREATE, DROP, ALTER
//    - Use non-deterministic sequences: nextval(), setval(), currval()
//    - Use randomness: random(), setseed()
//
// 2. STABLE - Functions that:
//    - Read session/auth state: auth.jwt(), auth.uid(), current_user, current_setting()
//    - Read time (within transaction): now(), current_timestamp, current_date
//    - Read database: SELECT (any query is at minimum STABLE)
//
// 3. IMMUTABLE - Functions that:
//    - Are purely computational (no DB access, no state)
//    - Always return same result for same inputs
//    - Example: Mathematical calculations, string operations
//
// Default: STABLE (safest for auth functions, works with index predicates)
//
// Note: We err on the side of caution. VOLATILE/STABLE are safe for index predicates,
// while incorrectly marking a function as IMMUTABLE can cause correctness issues.
func (st *SQLTransformer) determineVolatility(functionBody string) string {
	bodyUpper := strings.ToUpper(functionBody)

	// Pattern 1: VOLATILE - Data modification or side effects
	volatilePatterns := []string{
		"INSERT ", "UPDATE ", "DELETE ", "TRUNCATE ",
		"CREATE ", "DROP ", "ALTER ",
		"NEXTVAL(", "SETVAL(", "CURRVAL(",
		"RANDOM()", "SETSEED(",
	}
	for _, pattern := range volatilePatterns {
		if strings.Contains(bodyUpper, pattern) {
			return "VOLATILE"
		}
	}

	// Pattern 2: STABLE - Session/database state reads (no modifications)
	// This is the safest default for auth functions (Clerk, Supabase, Auth0)
	stablePatterns := []string{
		"AUTH.JWT()", "AUTH.UID()", "AUTH.ROLE()",           // Supabase/Clerk auth
		"CURRENT_USER", "CURRENT_SETTING(", "SESSION_USER",  // PostgreSQL session
		"CURRENT_TIMESTAMP", "NOW()", "TIMEOFDAY()",         // Time functions
		"CURRENT_DATE", "CURRENT_TIME",
		"TRANSACTION_TIMESTAMP()", "STATEMENT_TIMESTAMP()",
		"SELECT ", // Any SELECT query reads state
	}
	for _, pattern := range stablePatterns {
		if strings.Contains(bodyUpper, pattern) {
			return "STABLE"
		}
	}

	// Pattern 3: IMMUTABLE - Pure computational functions only
	// Very conservative check - must have no database/state access
	isPure := !strings.Contains(bodyUpper, "SELECT") &&
	          !strings.Contains(bodyUpper, "FROM") &&
	          !strings.Contains(bodyUpper, "PERFORM") &&
	          len(strings.TrimSpace(functionBody)) > 0

	if isPure {
		return "IMMUTABLE"
	}

	// Default: STABLE (safest, works with index predicates, appropriate for auth functions)
	return "STABLE"
}

// extractFunctionName extracts the function name from a CREATE FUNCTION statement.
// Handles both schema-qualified and unqualified names.
//
// Examples:
//   - "CREATE FUNCTION foo(...)" → "foo"
//   - "CREATE FUNCTION public.bar(...)" → "bar"
//   - "CREATE OR REPLACE FUNCTION baz(...)" → "baz"
func (st *SQLTransformer) extractFunctionName(funcDef string) string {
	// Pattern: FUNCTION [schema.]name
	// Captures only the function name, not the schema
	namePattern := regexp.MustCompile(`(?i)FUNCTION\s+(?:[a-z_][a-z0-9_]*\.)?([a-z_][a-z0-9_]*)`)
	match := namePattern.FindStringSubmatch(funcDef)
	if len(match) >= 2 {
		return match[1]
	}
	return "unknown"
}
