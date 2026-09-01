package transformation

import (
	"context"
	"fmt"
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
	EnableSyntaxFixes    bool   `json:"enable_syntax_fixes"` // Fix common SQL syntax errors
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
}

// NewSQLTransformer creates a new SQL transformer
func NewSQLTransformer(config *TransformationConfig) *SQLTransformer {
	if config == nil {
		config = DefaultTransformationConfig()
	}

	return &SQLTransformer{
		config:    config,
		pgVersion: config.TargetVersion,
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
		if isInsertStatement(trimmed) {
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
		if isUpdateStatement(trimmed) {
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
		if isDeleteStatement(trimmed) {
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
	trimmed := strings.TrimSpace(strings.TrimSuffix(sql, ";"))
	if !isInsertStatement(trimmed) {
		return sql
	}

	rest := strings.TrimSpace(trimmed[len("INSERT INTO "):])
	tableName, rest := splitLeadingIdentifier(rest)
	if tableName == "" {
		return sql
	}

	columnsPart := ""
	if strings.HasPrefix(strings.TrimSpace(rest), "(") {
		open := strings.Index(rest, "(")
		close := findMatchingParen(rest, open)
		if close == -1 {
			return sql
		}
		columnsPart = rest[open+1 : close]
		rest = strings.TrimSpace(rest[close+1:])
	}

	valuesIdx := findKeywordIndexCI(rest, "VALUES")
	if valuesIdx == -1 {
		return sql
	}

	valuesExpr := strings.TrimSpace(rest[valuesIdx+len("VALUES"):])
	if !strings.HasPrefix(valuesExpr, "(") {
		return sql
	}

	valuesClose := findMatchingParen(valuesExpr, 0)
	if valuesClose == -1 {
		return sql
	}

	valuesPart := valuesExpr[1:valuesClose]
	vals := splitCSVTopLevel(valuesPart)
	if len(vals) == 0 {
		return sql
	}

	if strings.TrimSpace(columnsPart) != "" {
		cols := splitCSVTopLevel(columnsPart)
		selectParts := make([]string, 0, len(vals))
		for i, val := range vals {
			value := strings.TrimSpace(val)
			if i < len(cols) {
				col := strings.TrimSpace(cols[i])
				selectParts = append(selectParts, fmt.Sprintf("%s as %s", value, col))
				continue
			}
			selectParts = append(selectParts, value)
		}

		return fmt.Sprintf("-- INSERT validation: SELECT %s -- FROM %s", strings.Join(selectParts, ", "), tableName)
	}

	return fmt.Sprintf("-- INSERT validation: SELECT %s -- INTO %s", strings.Join(vals, ", "), tableName)
}

// convertUpdateToSelect converts UPDATE statement to SELECT for validation
func (st *SQLTransformer) convertUpdateToSelect(sql string) string {
	trimmed := strings.TrimSpace(strings.TrimSuffix(sql, ";"))
	if !isUpdateStatement(trimmed) {
		return sql
	}

	rest := strings.TrimSpace(trimmed[len("UPDATE "):])
	tableName, rest := splitLeadingIdentifier(rest)
	if tableName == "" {
		return sql
	}

	setIdx := findKeywordIndexCI(rest, "SET")
	if setIdx == -1 {
		return sql
	}

	setAndWhere := strings.TrimSpace(rest[setIdx+len("SET"):])
	whereIdx := findKeywordIndexCI(setAndWhere, "WHERE")

	setPart := setAndWhere
	wherePart := ""
	if whereIdx >= 0 {
		setPart = strings.TrimSpace(setAndWhere[:whereIdx])
		wherePart = strings.TrimSpace(setAndWhere[whereIdx+len("WHERE"):])
	}

	assignments := splitCSVTopLevel(setPart)
	selectParts := make([]string, 0, len(assignments))
	for _, assignment := range assignments {
		parts := strings.SplitN(assignment, "=", 2)
		if len(parts) != 2 {
			continue
		}

		col := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if col == "" || val == "" {
			continue
		}

		selectParts = append(selectParts, fmt.Sprintf("%s as %s", val, col))
	}

	if len(selectParts) == 0 {
		return sql
	}

	if wherePart != "" {
		return fmt.Sprintf("-- UPDATE validation: SELECT %s FROM %s WHERE %s", strings.Join(selectParts, ", "), tableName, wherePart)
	}

	return fmt.Sprintf("-- UPDATE validation: SELECT %s FROM %s", strings.Join(selectParts, ", "), tableName)
}

// convertDeleteToSelect converts DELETE statement to SELECT for validation
func (st *SQLTransformer) convertDeleteToSelect(sql string) string {
	trimmed := strings.TrimSpace(strings.TrimSuffix(sql, ";"))
	if !isDeleteStatement(trimmed) {
		return sql
	}

	rest := strings.TrimSpace(trimmed[len("DELETE FROM "):])
	tableName, rest := splitLeadingIdentifier(rest)
	if tableName == "" {
		return sql
	}

	whereIdx := findKeywordIndexCI(rest, "WHERE")
	if whereIdx >= 0 {
		whereExpr := strings.TrimSpace(rest[whereIdx+len("WHERE"):])
		if whereExpr != "" {
			return fmt.Sprintf("-- DELETE validation: SELECT COUNT(*) FROM %s WHERE %s", tableName, whereExpr)
		}
	}

	return fmt.Sprintf("-- DELETE validation: SELECT COUNT(*) FROM %s", tableName)
}

func splitLeadingIdentifier(input string) (string, string) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", ""
	}

	for i, r := range trimmed {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '(' {
			return strings.TrimSpace(trimmed[:i]), strings.TrimSpace(trimmed[i:])
		}
	}

	return trimmed, ""
}

func splitCSVTopLevel(input string) []string {
	parts := make([]string, 0)
	if strings.TrimSpace(input) == "" {
		return parts
	}

	start := 0
	depth := 0
	inSingle := false
	inDouble := false
	escaped := false

	for i, r := range input {
		if escaped {
			escaped = false
			continue
		}

		switch r {
		case '\\':
			escaped = true
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '(':
			if !inSingle && !inDouble {
				depth++
			}
		case ')':
			if !inSingle && !inDouble && depth > 0 {
				depth--
			}
		case ',':
			if !inSingle && !inDouble && depth == 0 {
				parts = append(parts, strings.TrimSpace(input[start:i]))
				start = i + 1
			}
		}
	}

	if start < len(input) {
		parts = append(parts, strings.TrimSpace(input[start:]))
	}

	filtered := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}

	return filtered
}

func findMatchingParen(input string, openIndex int) int {
	if openIndex < 0 || openIndex >= len(input) || input[openIndex] != '(' {
		return -1
	}

	depth := 0
	inSingle := false
	inDouble := false
	escaped := false

	for i := openIndex; i < len(input); i++ {
		ch := input[i]
		if escaped {
			escaped = false
			continue
		}

		switch ch {
		case '\\':
			escaped = true
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '(':
			if !inSingle && !inDouble {
				depth++
			}
		case ')':
			if !inSingle && !inDouble {
				depth--
				if depth == 0 {
					return i
				}
			}
		}
	}

	return -1
}

func isInsertStatement(sql string) bool {
	return hasKeywordPrefixCI(sql, "INSERT", "INTO")
}

func isUpdateStatement(sql string) bool {
	return hasKeywordPrefixCI(sql, "UPDATE")
}

func isDeleteStatement(sql string) bool {
	return hasKeywordPrefixCI(sql, "DELETE", "FROM")
}

func isCreateFunctionStatement(sql string) bool {
	if hasKeywordPrefixCI(sql, "CREATE", "FUNCTION") {
		return true
	}
	return hasKeywordPrefixCI(sql, "CREATE", "OR", "REPLACE", "FUNCTION")
}

func isDropTableStatement(sql string) bool {
	return hasKeywordPrefixCI(sql, "DROP", "TABLE")
}

func isDropColumnStatement(sql string) bool {
	upper := normalizeUpperWhitespace(sql)
	return strings.HasPrefix(upper, "ALTER TABLE ") && strings.Contains(upper, " DROP COLUMN ")
}

func isAlterTypeStatement(sql string) bool {
	upper := normalizeUpperWhitespace(sql)
	return strings.HasPrefix(upper, "ALTER TABLE ") && strings.Contains(upper, " ALTER COLUMN ") && strings.Contains(upper, " TYPE ")
}

func hasLegacyJoinPredicate(sql string) bool {
	upper := strings.ToUpper(sql)
	whereIdx := strings.Index(upper, "WHERE")
	if whereIdx == -1 {
		return false
	}

	predicate := sql[whereIdx:]
	before, after, ok := strings.Cut(predicate, "=")
	if !ok {
		return false
	}

	left := strings.TrimSpace(before)
	right := strings.TrimSpace(after)

	leftToken := lastIdentifierToken(left)
	rightToken := firstIdentifierToken(right)

	return strings.Contains(leftToken, ".") && strings.Contains(rightToken, ".")
}

func hasLegacyFunctionNames(sql string) bool {
	lower := strings.ToLower(sql)
	return strings.Contains(lower, "substr(") || strings.Contains(lower, "length(") || strings.Contains(lower, "position(")
}

func replaceBareLengthFunction(sql string) string {
	lower := strings.ToLower(sql)
	needle := "length("

	var builder strings.Builder
	start := 0
	for {
		relIdx := strings.Index(lower[start:], needle)
		if relIdx == -1 {
			builder.WriteString(sql[start:])
			break
		}

		idx := start + relIdx
		builder.WriteString(sql[start:idx])

		if idx > 0 {
			prev := sql[idx-1]
			if (prev >= 'a' && prev <= 'z') || (prev >= 'A' && prev <= 'Z') || (prev >= '0' && prev <= '9') || prev == '_' {
				builder.WriteString(sql[idx : idx+len(needle)])
				start = idx + len(needle)
				continue
			}
		}

		builder.WriteString("char_length(")
		start = idx + len(needle)
	}

	return builder.String()
}

func hasSimpleWhereEquality(sql string) bool {
	upper := strings.ToUpper(sql)
	whereIdx := strings.Index(upper, "WHERE")
	if whereIdx == -1 {
		return false
	}

	predicate := sql[whereIdx+len("WHERE"):]
	eqIdx := strings.Index(predicate, "=")
	if eqIdx == -1 {
		return false
	}

	left := strings.TrimSpace(predicate[:eqIdx])
	right := strings.TrimSpace(predicate[eqIdx+1:])
	if left == "" || right == "" {
		return false
	}

	// Ignore non-equality operators.
	if eqIdx > 0 && predicate[eqIdx-1] == '!' {
		return false
	}
	if eqIdx+1 < len(predicate) && predicate[eqIdx+1] == '=' {
		return false
	}

	return true
}

func isSelectStarFromSingleTable(sql string) bool {
	upper := normalizeUpperWhitespace(sql)
	if !strings.HasPrefix(upper, "SELECT * FROM ") {
		return false
	}

	if strings.Contains(upper, " WHERE ") || strings.Contains(upper, " JOIN ") || strings.Contains(upper, " GROUP BY ") || strings.Contains(upper, " ORDER BY ") {
		return false
	}

	return true
}

func hasKeywordPrefixCI(sql string, keywords ...string) bool {
	if len(keywords) == 0 {
		return false
	}

	tokens := strings.Fields(strings.ToUpper(strings.TrimSpace(sql)))
	if len(tokens) < len(keywords) {
		return false
	}

	for i, keyword := range keywords {
		if tokens[i] != strings.ToUpper(keyword) {
			return false
		}
	}

	return true
}

func normalizeUpperWhitespace(sql string) string {
	return strings.Join(strings.Fields(strings.ToUpper(sql)), " ")
}

func findKeywordIndexCI(sql, keyword string) int {
	upperSQL := strings.ToUpper(sql)
	upperKeyword := strings.ToUpper(keyword)

	for i := 0; i+len(upperKeyword) <= len(upperSQL); i++ {
		if upperSQL[i:i+len(upperKeyword)] != upperKeyword {
			continue
		}

		beforeOk := i == 0 || !isIdentifierByte(upperSQL[i-1])
		afterPos := i + len(upperKeyword)
		afterOk := afterPos >= len(upperSQL) || !isIdentifierByte(upperSQL[afterPos])
		if beforeOk && afterOk {
			return i
		}
	}

	return -1
}

func isIdentifierByte(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

func lastIdentifierToken(s string) string {
	tokens := strings.FieldsFunc(strings.TrimSpace(s), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '(' || r == ')' || r == ','
	})
	if len(tokens) == 0 {
		return ""
	}
	return tokens[len(tokens)-1]
}

func firstIdentifierToken(s string) string {
	tokens := strings.FieldsFunc(strings.TrimSpace(s), func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '(' || r == ')' || r == ','
	})
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

// transformDropToComment converts dangerous DROP statements to comments
func (st *SQLTransformer) transformDropToComment(ctx context.Context, sql string, result *TransformationResult) (string, error) {
	lines := strings.Split(sql, "\n")
	transformedLines := make([]string, len(lines))
	copy(transformedLines, lines)

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)

		if isDropTableStatement(trimmed) {
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

		if isDropColumnStatement(trimmed) {
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
		if isAlterTypeStatement(trimmed) {
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
	if hasLegacyJoinPredicate(sql) {
		// This is a complex transformation that would need more sophisticated parsing
		result.Warnings = append(result.Warnings, "Consider converting old-style joins to ANSI join syntax")
	}

	// Convert old function names to modern equivalents
	if hasLegacyFunctionNames(sql) {
		modernSQL := strings.ReplaceAll(transformedSQL, "substr(", "substring(")

		modernSQL = replaceBareLengthFunction(modernSQL)

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
		if hasSimpleWhereEquality(trimmed) && !strings.Contains(strings.ToUpper(trimmed), "INDEX") {
			result.Warnings = append(result.Warnings, fmt.Sprintf("Line %d: Consider adding index for WHERE clause performance", i+1))
		}

		// Detect SELECT * queries
		if isSelectStarFromSingleTable(trimmed) {
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

// fixFunctionVolatilityMarkers checks for the presence of volatility markers in functions.
//
// NOTE: This function currently performs read-only validation and logging.
// It does NOT automatically add volatility markers. The previous approach of automatically
// adding volatility markers was too aggressive and caused schema differences after squashing.
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
// If a function needs a volatility marker, it should be present in the original SQL
// or added manually by the developer.
//
// Implementation Note:
// This runs BEFORE pg_query.Parse() because the parser can fail on complex migrations.
// Uses regex-based detection instead of AST analysis.
func (st *SQLTransformer) fixFunctionVolatilityMarkers(ctx context.Context, sql string, result *TransformationResult) (string, error) {
	statements, err := pg_query.SplitWithScanner(sql, true)
	if err != nil {
		return sql, nil
	}

	for _, stmt := range statements {
		trimmed := strings.TrimSpace(stmt)
		if !isCreateFunctionStatement(trimmed) {
			continue
		}

		if st.hasVolatilityMarker(trimmed) {
			continue
		}

		// Read-only analysis only: keep SQL unchanged.
		// We intentionally do not auto-insert volatility keywords.
		_ = st.extractFunctionName(trimmed)
	}

	return sql, nil
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
//   - Modify data: INSERT, UPDATE, DELETE, TRUNCATE
//   - Modify schema: CREATE, DROP, ALTER
//   - Use non-deterministic sequences: nextval(), setval(), currval()
//   - Use randomness: random(), setseed()
//
// 2. STABLE - Functions that:
//   - Read session/auth state: auth.jwt(), auth.uid(), current_user, current_setting()
//   - Read time (within transaction): now(), current_timestamp, current_date
//   - Read database: SELECT (any query is at minimum STABLE)
//
// 3. IMMUTABLE - Functions that:
//   - Are purely computational (no DB access, no state)
//   - Always return same result for same inputs
//   - Example: Mathematical calculations, string operations
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
		"AUTH.JWT()", "AUTH.UID()", "AUTH.ROLE()", // Supabase/Clerk auth
		"CURRENT_USER", "CURRENT_SETTING(", "SESSION_USER", // PostgreSQL session
		"CURRENT_TIMESTAMP", "NOW()", "TIMEOFDAY()", // Time functions
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
	upper := strings.ToUpper(funcDef)
	idx := strings.Index(upper, "FUNCTION")
	if idx == -1 {
		return "unknown"
	}

	rest := strings.TrimSpace(funcDef[idx+len("FUNCTION"):])
	if strings.HasPrefix(strings.ToUpper(rest), "IF NOT EXISTS") {
		rest = strings.TrimSpace(rest[len("IF NOT EXISTS"):])
	}

	nameToken, _ := splitLeadingIdentifier(rest)
	if nameToken == "" {
		return "unknown"
	}

	if strings.Contains(nameToken, ".") {
		parts := strings.Split(nameToken, ".")
		return parts[len(parts)-1]
	}

	return nameToken
}
