package consolidation

import (
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/utils"

	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// DropCreateCycleRule handles DROP followed by CREATE consolidation
// This includes tables and views (DROP VIEW + CREATE VIEW → CREATE OR REPLACE VIEW)
type DropCreateCycleRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *DropCreateCycleRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	if strings.Contains(lifecycle.Name, "prevent_null_fairrent_fields") {
		utils.GetDefaultLogger().Debug("DropCreateCycleRule CanApply for %s (History len: %d)", lifecycle.Name, len(lifecycle.History))
		for i, h := range lifecycle.History {
			utils.GetDefaultLogger().Debug("  Event %d: Op=%s", i, h.Operation)
		}
	}

	if len(lifecycle.History) < 2 {
		return false
	}

	// CRITICAL FIX: If the object is ultimately DROPPED (last event is OpDrop),
	// we must NOT apply this rule. Using this rule would consolidate the intermediate
	// Drop->Create cycle into a "valid" object, ignoring the final deletion.
	// We want the Engine-level dropped-object skip to handle the final DROP.
	if len(lifecycle.History) > 0 && lifecycle.History[len(lifecycle.History)-1].Operation == types.OpDrop {
		return false
	}

	// Look for DROP followed by CREATE pattern
	for i := 0; i < len(lifecycle.History)-1; i++ {
		if lifecycle.History[i].Operation == types.OpDrop &&
			lifecycle.History[i+1].Operation == types.OpCreate {
			return true
		}
	}

	// Also check for DROP VIEW pattern (might not have subsequent CREATE in same lifecycle)
	if lifecycle.Type == types.TypeView {
		for _, event := range lifecycle.History {
			if event.Operation == types.OpDrop {
				return true
			}
		}
	}

	return false
}

// Apply applies the consolidation rule to the given lifecycle
func (r *DropCreateCycleRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if strings.Contains(lifecycle.Name, "prevent_null_fairrent_fields") {
		utils.GetDefaultLogger().Debug("DropCreateCycleRule Apply for %s", lifecycle.Name)
	}
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]any{"rule": "DropCreateCycleRule"})
	}

	// Find the DROP-CREATE pair
	var dropStmt *types.Statement
	var originalStmts []types.Statement

	for i := 0; i < len(lifecycle.History)-1; i++ {
		if lifecycle.History[i].Operation == types.OpDrop &&
			lifecycle.History[i+1].Operation == types.OpCreate {
			dropStmt = &lifecycle.History[i].Statement
			originalStmts = append(originalStmts, *dropStmt)
			break
		}
	}

	// Collect ALL CREATE statements (not just the one after DROP)
	// dependency information (extracted by parser from AST) that's needed for proper ordering
	var allCreateStmts []types.Statement
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			allCreateStmts = append(allCreateStmts, event.Statement)
			// ALWAYS add CREATE to originalStmts to preserve dependencies
			originalStmts = append(originalStmts, event.Statement)
		}
	}

	// Merge multiple CREATE statements if needed
	var consolidatedSQL string
	optimizations := []string{}

	if lifecycle.Type == types.TypeView {
		// For VIEWs: Convert DROP VIEW + CREATE VIEW to CREATE OR REPLACE VIEW
		if len(allCreateStmts) == 1 {
			createViewSQL := allCreateStmts[0].SQL

			// Replace "CREATE VIEW" with "CREATE OR REPLACE VIEW"
			if strings.Contains(strings.ToUpper(createViewSQL), "CREATE VIEW") {
				consolidatedSQL = replaceCreateViewClause(createViewSQL)
				optimizations = append(optimizations, "Converted DROP VIEW + CREATE VIEW to CREATE OR REPLACE VIEW")
			} else {
				consolidatedSQL = createViewSQL
				optimizations = append(optimizations, "Eliminated DROP operation before CREATE VIEW")
			}
		} else if len(allCreateStmts) > 1 {
			// Multiple CREATE VIEW statements - use the last one with CREATE OR REPLACE
			lastCreate := allCreateStmts[len(allCreateStmts)-1].SQL
			consolidatedSQL = replaceCreateViewClause(lastCreate)
			optimizations = append(optimizations, fmt.Sprintf("Merged %d CREATE VIEW statements into CREATE OR REPLACE VIEW", len(allCreateStmts)))
		} else {
			return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "no CREATE statement found for VIEW", map[string]any{"object": lifecycle.Name})
		}

		utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Info("DropCreateCycleRule: Converted VIEW %s to CREATE OR REPLACE pattern", lifecycle.Name)
	} else if len(allCreateStmts) > 1 && lifecycle.Type == types.TypeTable {
		// For TABLES: Merge multiple CREATE statements
		consolidatedSQL = mergeMultipleCreateStatements(allCreateStmts, lifecycle.Name)
		optimizations = append(optimizations, fmt.Sprintf("Merged %d CREATE statements for table %s", len(allCreateStmts), lifecycle.Name))
		utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Info("DropCreateCycleRule: Merged %d CREATE statements for %s", len(allCreateStmts), lifecycle.Name)
	} else if len(allCreateStmts) >= 1 {
		// Generic handling for other types (indexes, policies, triggers, etc.)
		// Just take the LAST create statement
		consolidatedSQL = allCreateStmts[len(allCreateStmts)-1].SQL
		utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Info("DropCreateCycleRule: Used last of %d CREATE statements for %s", len(allCreateStmts), lifecycle.Name)

		if len(allCreateStmts) > 1 {
			optimizations = append(optimizations, fmt.Sprintf("Merged %d versions of %s to final definition", len(allCreateStmts), lifecycle.Name))
		} else {
			optimizations = append(optimizations, "Eliminated DROP operation before CREATE")
		}
	} else {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "no CREATE statements found in lifecycle", map[string]any{"object": lifecycle.Name})
	}

	// Track column evolution from multiple CREATEs (e.g., name -> market_name)
	var columnEvolutions map[string]*tracking.ColumnEvolutionInfo
	if len(allCreateStmts) > 1 && lifecycle.Type == types.TypeTable {
		utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Info("Extracting column evolutions from %d CREATE statements for %s", len(allCreateStmts), lifecycle.Name)
		columnEvolutions = extractColumnEvolutionsFromMultipleCreates(lifecycle.Name, allCreateStmts)
		utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Info("Found %d column evolutions for %s", len(columnEvolutions), lifecycle.Name)
	}

	result := &tracking.ConsolidationResult{
		OriginalStatements: originalStmts,
		ConsolidatedSQL:    consolidatedSQL,
		Optimizations:      optimizations,
		RiskLevel:          tracking.RiskLevelLow,
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(originalStmts) - 1,
			FilesAffected:     len(originalStmts),
			LinesReduced:      (len(originalStmts) - 1) * 5,
		},
		ColumnEvolutions: columnEvolutions,
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *DropCreateCycleRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelMedium
}

// Helper functions for DROP CREATE consolidation

// mergeMultipleCreateStatements merges multiple CREATE statements for the same table
func mergeMultipleCreateStatements(createStmts []types.Statement, tableName string) string {
	if len(createStmts) == 0 {
		return ""
	}

	// Use the LAST CREATE as the base (for DDL cycles: CREATE→DROP→CREATE, we want the final version)
	baseSQL := createStmts[len(createStmts)-1].SQL

	//  Extract column evolutions FIRST to identify renamed columns
	columnEvolutions := extractColumnEvolutionsFromMultipleCreates(tableName, createStmts)
	supersededColumns := make(map[string]bool) // Track columns that were renamed (old names)
	for _, evolution := range columnEvolutions {
		// Mark original name as superseded (should not appear in final CREATE)
		supersededColumns[strings.ToLower(evolution.OriginalName)] = true
		utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Info("  Marking column '%s' as superseded (renamed to '%s')",
			evolution.OriginalName, evolution.FinalName)
	}

	// IMPORTANT: Use ONLY the columns from the LAST CREATE statement
	// This ensures we respect when a later migration removes columns from an earlier one
	// For example: CREATE 0 has columns A,B,C -> CREATE 1 has only A,B (removed C)
	// The final schema should have A,B, not A,B,C
	allColumns := make(map[string]string) // column name -> full definition
	columnOrder := make([]string, 0)      // maintain order

	// Process ALL CREATEs to log them, but only USE the LAST one's columns
	for i, stmt := range createStmts {
		columns := extractColumnsFromCreate(stmt.SQL)
		utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Info("  Extracted %d columns from CREATE statement %d", len(columns), i)

		// DEBUG: Log first 5 column names from this statement
		colNames := make([]string, 0, len(columns))
		for colName := range columns {
			colNames = append(colNames, colName)
			if len(colNames) >= 5 {
				break
			}
		}
		utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Info("    Sample columns: %v", colNames)

		// ONLY use columns from the LAST CREATE (final schema)
		if i != len(createStmts)-1 {
			utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Debug("  Skipping CREATE %d (not the final one)", i)
			continue
		}

		// This is the LAST CREATE - use its columns
		utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Info("  Using columns from CREATE %d (FINAL schema)", i)
		for colName, colDef := range columns {
			// Skip superseded columns (old column names that were renamed)
			if supersededColumns[strings.ToLower(colName)] {
				utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Info("  Skipping superseded column '%s' (was renamed)", colName)
				continue
			}

			columnOrder = append(columnOrder, colName)
			allColumns[colName] = colDef
			utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Debug("    Adding column '%s' from final CREATE", colName)
		}
	}

	utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Info("  Total unique columns across all CREATEs (after removing superseded): %d", len(allColumns))

	// DEBUG: Check for specific missing columns in profiles table
	if strings.ToLower(tableName) == "profiles" {
		missingCols := []string{"property_management_scope", "managed_properties_count", "portfolio_size_limit", "auth_provider"}
		for _, col := range missingCols {
			if _, exists := allColumns[col]; exists {
				utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Info("  ✓ Column '%s' IS present in allColumns", col)
			} else {
				utils.GetDefaultLogger().WithPrefix("DROP-CREATE").Warn("  ✗ Column '%s' IS MISSING from allColumns!", col)
			}
		}
	}

	// Extract table-level constraints from the LAST CREATE statement (this has the final schema)
	tableConstraints := extractTableConstraintsFromCreate(baseSQL)

	// Rebuild CREATE statement with all columns and constraints
	return reconstructCreateWithColumns(baseSQL, allColumns, columnOrder, tableConstraints, tableName)
}

// extractColumnEvolutionsFromMultipleCreates detects column renames from multiple CREATE statements
func extractColumnEvolutionsFromMultipleCreates(tableName string, createStmts []types.Statement) map[string]*tracking.ColumnEvolutionInfo {
	evolutions := make(map[string]*tracking.ColumnEvolutionInfo)

	if len(createStmts) <= 1 {
		return evolutions // No evolution with single CREATE
	}

	// Helper to get column names as slice
	getColumnNames := func(cols map[string]string) []string {
		keys := make([]string, 0, len(cols))
		for k := range cols {
			keys = append(keys, k)
		}
		return keys
	}

	// Extract columns from each CREATE statement
	columnsByStmt := make([]map[string]string, len(createStmts))
	for i, stmt := range createStmts {
		columnsByStmt[i] = extractColumnsFromCreate(stmt.SQL)
	}

	// Find columns that appear in earlier CREATEs but not in the final one
	// These are candidates for renames (e.g., 'name' -> 'market_name')
	finalColumns := columnsByStmt[len(columnsByStmt)-1]
	utils.GetDefaultLogger().WithPrefix("COLUMN-EVOLUTION").Info("Final CREATE has columns: %v", getColumnNames(finalColumns))

	// ALSO check if columns from middle CREATEs evolved FROM earlier versions
	// Example: CREATE 0 has 'name', CREATE 1 has 'market_name', CREATE 2 has 'name' again
	// We need to detect name -> market_name -> name pattern and rewrite data ops from CREATE 0/1
	allColumns := make(map[string]bool) // Track all column names across all CREATEs
	for i, cols := range columnsByStmt {
		utils.GetDefaultLogger().WithPrefix("COLUMN-EVOLUTION").Info("CREATE %d has columns: %v", i, getColumnNames(cols))
		for colName := range cols {
			allColumns[colName] = true
		}
	}

	// For each CREATE, find columns that don't exist in the final schema
	for i := range columnsByStmt {
		for oldColName := range columnsByStmt[i] {
			if _, existsInFinal := finalColumns[oldColName]; !existsInFinal {
				utils.GetDefaultLogger().WithPrefix("COLUMN-EVOLUTION").Info("Column %s disappeared from CREATE %d, checking for renames...", oldColName, i)
				// This column disappeared - check if a similar column exists in the FINAL or ANY other CREATE
				// Check final columns first
				for newColName := range finalColumns {
					// Check if new column name contains old column name (e.g., market_name contains name)
					utils.GetDefaultLogger().WithPrefix("COLUMN-EVOLUTION").Debug("  Checking if '%s' (final) matches pattern for '%s'", newColName, oldColName)
					if strings.HasSuffix(newColName, "_"+oldColName) ||
						(strings.Contains(newColName, oldColName) && len(newColName) > len(oldColName)) {
						key := fmt.Sprintf("%s.%s", strings.ToLower(tableName), strings.ToLower(oldColName))
						if _, exists := evolutions[key]; !exists { // Don't override existing mappings
							evolutions[key] = &tracking.ColumnEvolutionInfo{
								TableName:    strings.ToLower(tableName),
								OriginalName: oldColName,
								FinalName:    newColName,
								RenameChain:  []string{oldColName, newColName},
							}
							utils.GetDefaultLogger().WithPrefix("COLUMN-EVOLUTION").Info(
								"✓ Detected column evolution in %s: %s -> %s (key: %s)",
								tableName, oldColName, newColName, key)
						}
						break
					}
				}

				// IMPORTANT: Also check if this column name CONTAINS another column (reverse direction)
				// E.g., 'market_name' from CREATE 1 should map to 'name' in final CREATE
				// E.g., 'compatibility_score' from CREATE 0 should map to 'compatibility' in final CREATE
				for finalColName := range finalColumns {
					utils.GetDefaultLogger().WithPrefix("COLUMN-EVOLUTION").Debug("  Checking reverse: if '%s' contains '%s'", oldColName, finalColName)
					// Check multiple patterns:
					// 1. oldCol ends with "_finalCol" (e.g., market_name -> name)
					// 2. oldCol starts with "finalCol_" (e.g., compatibility_score -> compatibility)
					// 3. oldCol contains finalCol and is longer
					if strings.HasSuffix(oldColName, "_"+finalColName) ||
						strings.HasPrefix(oldColName, finalColName+"_") ||
						(strings.Contains(oldColName, finalColName) && len(oldColName) > len(finalColName)) {
						// The disappeared column is MORE SPECIFIC than final column
						// E.g., 'market_name' (disappeared) -> 'name' (final)
						// Or 'compatibility_score' (disappeared) -> 'compatibility' (final)
						key := fmt.Sprintf("%s.%s", strings.ToLower(tableName), strings.ToLower(oldColName))
						if _, exists := evolutions[key]; !exists {
							evolutions[key] = &tracking.ColumnEvolutionInfo{
								TableName:    strings.ToLower(tableName),
								OriginalName: oldColName,
								FinalName:    finalColName,
								RenameChain:  []string{oldColName, finalColName},
							}
							utils.GetDefaultLogger().WithPrefix("COLUMN-EVOLUTION").Info(
								"✓ Detected reverse column evolution in %s: %s -> %s (key: %s)",
								tableName, oldColName, finalColName, key)
						}
						break
					}
				}
			}
		}
	}

	return evolutions
}

// extractTableConstraintsFromCreate extracts table-level constraints (not column definitions)
func extractTableConstraintsFromCreate(createSQL string) []string {
	var constraints []string

	firstParen := strings.Index(createSQL, "(")
	lastParen := strings.LastIndex(createSQL, ")")

	if firstParen == -1 || lastParen == -1 || firstParen >= lastParen {
		return constraints
	}

	columnList := createSQL[firstParen+1 : lastParen]
	parts := splitColumnDefinitions(columnList)

	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}

		// Skip JSON keys
		if strings.HasPrefix(trimmed, "\"") || strings.HasPrefix(trimmed, "'") {
			continue
		}

		upper := strings.ToUpper(trimmed)

		// Only include table-level constraints
		if strings.HasPrefix(upper, "CONSTRAINT ") ||
			strings.HasPrefix(upper, "PRIMARY KEY (") ||
			strings.HasPrefix(upper, "FOREIGN KEY (") ||
			strings.HasPrefix(upper, "UNIQUE (") {
			constraints = append(constraints, trimmed)
		}
	}

	return constraints
}

// extractColumnsFromCreate extracts column definitions from a CREATE TABLE statement
func extractColumnsFromCreate(createSQL string) map[string]string {
	columns := make(map[string]string)

	// Find the column list between first ( and last )
	firstParen := strings.Index(createSQL, "(")
	lastParen := strings.LastIndex(createSQL, ")")

	if firstParen == -1 || lastParen == -1 || firstParen >= lastParen {
		return columns
	}

	columnList := createSQL[firstParen+1 : lastParen]

	// Split by commas (being careful with nested parentheses in constraints)
	parts := splitColumnDefinitions(columnList)

	for _, part := range parts {
		cleanedPart := part
		if found := strings.Contains(part, "--"); found {
			// Found inline comment - split by newline and reassemble without the comment line
			lines := strings.Split(part, "\n")
			var cleanedLines []string
			for _, line := range lines {
				if commentIdx := strings.Index(line, "--"); commentIdx != -1 {
					// Remove everything from -- to end of this line
					line = line[:commentIdx]
				}
				if strings.TrimSpace(line) != "" {
					cleanedLines = append(cleanedLines, line)
				}
			}
			cleanedPart = strings.Join(cleanedLines, "\n")
		}

		trimmed := strings.TrimSpace(cleanedPart)
		if trimmed == "" {
			continue
		}

		// Skip JSON keys (they start with quotes)
		if strings.HasPrefix(trimmed, "\"") || strings.HasPrefix(trimmed, "'") {
			continue
		}

		// Skip standalone comments (lines that start with --)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}

		// Skip table-level constraints (CONSTRAINT, PRIMARY KEY, FOREIGN KEY, CHECK, UNIQUE without column name)
		upper := strings.ToUpper(trimmed)
		if strings.HasPrefix(upper, "CONSTRAINT ") ||
			strings.HasPrefix(upper, "PRIMARY KEY") ||
			strings.HasPrefix(upper, "FOREIGN KEY") ||
			(strings.HasPrefix(upper, "CHECK") && !strings.Contains(trimmed, " ")) ||
			(strings.HasPrefix(upper, "UNIQUE") && strings.HasPrefix(upper, "UNIQUE (")) {
			continue
		}

		// Extract column name (first word, remove quotes if present)
		words := strings.Fields(trimmed)
		if len(words) > 0 {
			colName := strings.ToLower(strings.Trim(words[0], "\"'"))
			columns[colName] = trimmed
		}
	}

	return columns
}

// splitColumnDefinitions splits column definitions by comma, respecting nested parentheses and curly braces (for JSON/JSONB)
func splitColumnDefinitions(columnList string) []string {
	var parts []string
	var current strings.Builder
	parenDepth := 0
	braceDepth := 0
	inString := false
	var stringDelim rune

	for i, char := range columnList {
		// Handle string literals (both single and double quotes)
		if (char == '\'' || char == '"') && (i == 0 || columnList[i-1] != '\\') {
			if !inString {
				inString = true
				stringDelim = char
			} else if char == stringDelim {
				inString = false
			}
			current.WriteRune(char)
			continue
		}

		// If we're in a string, don't process special characters
		if inString {
			current.WriteRune(char)
			continue
		}

		switch char {
		case '(':
			parenDepth++
			current.WriteRune(char)
		case ')':
			parenDepth--
			current.WriteRune(char)
		case '{':
			braceDepth++
			current.WriteRune(char)
		case '}':
			braceDepth--
			current.WriteRune(char)
		case ',':
			// Only split on commas at depth 0 for both parens and braces
			if parenDepth == 0 && braceDepth == 0 {
				parts = append(parts, current.String())
				current.Reset()
			} else {
				current.WriteRune(char)
			}
		default:
			current.WriteRune(char)
		}
	}

	// Add the last part
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// reconstructCreateWithColumns rebuilds a CREATE TABLE statement with merged columns
func reconstructCreateWithColumns(baseSQL string, columns map[string]string, columnOrder []string, tableConstraints []string, tableName string) string {
	// Extract the CREATE TABLE ... ( part
	firstParen := strings.Index(baseSQL, "(")
	if firstParen == -1 {
		return baseSQL
	}

	header := baseSQL[:firstParen+1]

	// Rebuild column list
	var columnDefs []string
	for _, colName := range columnOrder {
		if colDef, exists := columns[colName]; exists {
			columnDefs = append(columnDefs, "  "+colDef)
		}
	}

	// Add table-level constraints
	for _, constraint := range tableConstraints {
		columnDefs = append(columnDefs, "  "+constraint)
	}

	// Find everything after the last ) (like table options, semicolon, etc.)
	lastParen := strings.LastIndex(baseSQL, ")")
	suffix := ""
	if lastParen != -1 && lastParen+1 < len(baseSQL) {
		suffix = baseSQL[lastParen:]
	} else {
		suffix = "\n);"
	}

	// Reconstruct
	result := header + "\n"
	result += strings.Join(columnDefs, ",\n")
	result += "\n" + suffix

	return result
}

func replaceCreateViewClause(sql string) string {
	if strings.TrimSpace(sql) == "" {
		return sql
	}

	lower := strings.ToLower(sql)
	for idx := 0; idx < len(lower); {
		createIdx := strings.Index(lower[idx:], "create")
		if createIdx == -1 {
			break
		}
		createIdx += idx

		if createIdx > 0 && isDropCreateIdentifierByte(lower[createIdx-1]) {
			idx = createIdx + len("create")
			continue
		}

		afterCreate := skipWhitespace(lower, createIdx+len("create"))
		if afterCreate+len("view") > len(lower) || lower[afterCreate:afterCreate+len("view")] != "view" {
			idx = createIdx + len("create")
			continue
		}

		viewEnd := afterCreate + len("view")
		if viewEnd < len(lower) && isDropCreateIdentifierByte(lower[viewEnd]) {
			idx = createIdx + len("create")
			continue
		}

		return sql[:createIdx] + "CREATE OR REPLACE VIEW" + sql[viewEnd:]
	}

	return sql
}

func skipWhitespace(value string, pos int) int {
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

func isDropCreateIdentifierByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}
