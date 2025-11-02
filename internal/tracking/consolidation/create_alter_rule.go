package consolidation

import (
	"fmt"
	"strings"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/utils"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/tracking"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/types"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/errors"
)

// CreateAlterConsolidationRule consolidates CREATE statements followed by ALTER statements
type CreateAlterConsolidationRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *CreateAlterConsolidationRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	if len(lifecycle.History) < 2 {
		return false
	}

	// First check: If there are multiple CREATE statements, let MultipleCreateConsolidationRule handle it
	createCount := 0
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			createCount++
			if createCount > 1 {
				// Debug logging for profiles
				if strings.ToLower(lifecycle.Name) == "profiles" {
					utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Info("DEBUG CreateAlterConsolidationRule.CanApply: profiles has %d CREATE operations, deferring to MultipleCreateConsolidationRule", createCount)
				}
				return false // Let MultipleCreateConsolidationRule handle it
			}
		}
	}

	// Check for CREATE followed by ALTER pattern (single CREATE only)
	if lifecycle.History[0].Operation == types.OpCreate {
		for i := 1; i < len(lifecycle.History); i++ {
			if lifecycle.History[i].Operation == types.OpAlter {
				// Check if there are no data operations in between
				if !lifecycle.History[i].HasDataOps {
					// Debug logging for profiles
					if strings.ToLower(lifecycle.Name) == "profiles" {
						utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Info("DEBUG CreateAlterConsolidationRule.CanApply: profiles (type=%s) matches! Single CREATE with ALTER operations", lifecycle.Type)
					}
					return true
				}
			}
		}
	}

	return false
}

// Apply applies the consolidation rule to the given lifecycle
func (r *CreateAlterConsolidationRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]interface{}{"rule": "CreateAlterConsolidationRule"})
	}

	// Extract CREATE statement and all ALTER statements
	var createStmt *types.Statement
	var alterStmts []types.Statement

	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			createStmt = &event.Statement
		} else if event.Operation == types.OpAlter && !event.HasDataOps {
			alterStmts = append(alterStmts, event.Statement)
		}
	}

	// Build consolidated CREATE statement by actually integrating ALTER operations
	consolidatedSQL := integrateAlterIntoCreate(createStmt, alterStmts)

	// CRITICAL FIX: pg_query deparser sometimes corrupts char_length() to char_char_length()
	// This happens when deparsing CHECK constraints with char_length() function calls
	// Fix it immediately after consolidation to prevent corruption in output
	if strings.Contains(consolidatedSQL, "char_char_length") {
		utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Info("CONSOLIDATION FIX: Found and fixing char_char_length corruption in %s", createStmt.ObjectName)
		consolidatedSQL = strings.ReplaceAll(consolidatedSQL, "char_char_length", "char_length")
	}

	// BUGFIX Bug #5: Ensure consolidated SQL always ends with semicolon
	// This is critical for when separate ALTER statements (like RLS) are appended later by the engine
	consolidatedSQL = strings.TrimRight(consolidatedSQL, " \t\n")
	if !strings.HasSuffix(consolidatedSQL, ";") {
		consolidatedSQL += ";"
	}

	// Build list of original statements
	originalStmts := []types.Statement{*createStmt}
	originalStmts = append(originalStmts, alterStmts...)

	result := &tracking.ConsolidationResult{
		OriginalStatements: originalStmts,
		ConsolidatedSQL:    consolidatedSQL,
		Optimizations: []string{
			fmt.Sprintf("Consolidated CREATE with %d ALTER operations", len(alterStmts)),
		},
		RiskLevel: tracking.RiskLevelLow,
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(alterStmts),
			FilesAffected:     len(alterStmts) + 1,
			LinesReduced:      len(alterStmts) * 2,
		},
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *CreateAlterConsolidationRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow
}

// Helper functions for CREATE ALTER consolidation

// integrateAlterIntoCreate integrates ALTER operations into a CREATE statement
func integrateAlterIntoCreate(createStmt *types.Statement, alterStmts []types.Statement) string {
	createSQL := createStmt.SQL

	// DEBUG: Log incoming SQL for analytics tables
	objectName := createStmt.ObjectName
	if strings.Contains(strings.ToLower(objectName), "analytics") {
		utils.GetDefaultLogger().WithPrefix("CREATE-ALTER-DEBUG").Info(
			"BEFORE consolidation for %s: SQL length=%d, starts with: %s",
			objectName, len(createSQL), createSQL[:min(100, len(createSQL))])
	}

	// Handle ENUM types specially - merge ALTER TYPE ADD VALUE into CREATE TYPE
	if createStmt.ObjectType == types.TypeEnum {
		return integrateAlterTypeIntoCreate(createSQL, alterStmts)
	}

	// Extract column additions and constraints from ALTER statements
	// Use a map to track column definitions by name (last definition wins for duplicates)
	columnDefinitions := make(map[string]string) // map[columnName]columnDef
	var columnOrder []string                     // preserve order of first appearance
	var addedConstraints []string

	for _, alterStmt := range alterStmts {
		// Parse the ALTER statement directly to extract what needs to be added
		alterSQL := strings.TrimSpace(alterStmt.SQL)

		if strings.Contains(strings.ToUpper(alterSQL), "ADD COLUMN") {
			// Extract column definition from ADD COLUMN statement
			if columnDef := extractColumnFromAddStatement(alterSQL); columnDef != "" {
				// Extract column name (first word of the definition)
				columnName := extractColumnName(columnDef)

				// Check for duplicate column definitions
				if existingDef, exists := columnDefinitions[columnName]; exists {
					// Duplicate detected - log warning and use the latest definition
					utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Warn(
						"Duplicate column '%s' detected during consolidation - using latest definition (was: %s, now: %s)",
						columnName, existingDef, columnDef,
					)
				} else {
					// First time seeing this column - add to order tracking
					columnOrder = append(columnOrder, columnName)
				}

				// Store/update the column definition (last one wins)
				columnDefinitions[columnName] = columnDef
			}
		} else if strings.Contains(strings.ToUpper(alterSQL), "ADD CONSTRAINT") {
			// Extract constraint definition from ADD CONSTRAINT statement
			if constraintDef := extractConstraintFromAddStatement(alterSQL); constraintDef != "" {
				addedConstraints = append(addedConstraints, constraintDef)
			}
		}

		// Handle other ALTER operations that should be integrated
		if strings.Contains(strings.ToUpper(alterSQL), "ENABLE ROW LEVEL SECURITY") {
			// Skip RLS - this needs to be a separate statement after CREATE
			continue
		}
	}

	// Build final column list in order of first appearance
	var addedColumns []string
	for _, columnName := range columnOrder {
		addedColumns = append(addedColumns, columnDefinitions[columnName])
	}

	// Integrate columns and constraints into the CREATE statement
	if len(addedColumns) > 0 || len(addedConstraints) > 0 {
		createSQL = integrateColumnsAndConstraintsIntoCreate(createSQL, addedColumns, addedConstraints)
	}

	// DEBUG: Log outgoing SQL for analytics tables
	if strings.Contains(strings.ToLower(objectName), "analytics") {
		utils.GetDefaultLogger().WithPrefix("CREATE-ALTER-DEBUG").Info(
			"AFTER consolidation for %s: SQL length=%d, starts with: %s, ends with: %s",
			objectName, len(createSQL), createSQL[:min(100, len(createSQL))], createSQL[max(0, len(createSQL)-50):])
	}

	return createSQL
}

// extractColumnFromAddStatement extracts the column definition from ALTER TABLE ADD COLUMN
func extractColumnFromAddStatement(alterSQL string) string {
	upperSQL := strings.ToUpper(alterSQL)
	addColIndex := strings.Index(upperSQL, "ADD COLUMN")
	if addColIndex == -1 {
		return ""
	}

	// Extract everything after "ADD COLUMN"
	afterAddCol := strings.TrimSpace(alterSQL[addColIndex+len("ADD COLUMN"):])

	// Remove trailing semicolon
	afterAddCol = strings.TrimRight(afterAddCol, ";")

	// Strip "IF NOT EXISTS" clause - it's not valid inside CREATE TABLE column definitions
	// We need to preserve case-sensitive column names, so use regex to strip only the IF NOT EXISTS part
	upperAfterCol := strings.ToUpper(afterAddCol)
	if strings.HasPrefix(upperAfterCol, "IF NOT EXISTS ") {
		// Remove the first "IF NOT EXISTS " (case-insensitive)
		afterAddCol = strings.TrimSpace(afterAddCol[len("IF NOT EXISTS "):])
	}

	// Normalize multi-line column definitions (e.g., when CHECK is on next line)
	// Replace newlines with spaces to keep the column definition on one logical line
	afterAddCol = strings.ReplaceAll(afterAddCol, "\n", " ")
	// Clean up multiple consecutive spaces
	for strings.Contains(afterAddCol, "  ") {
		afterAddCol = strings.ReplaceAll(afterAddCol, "  ", " ")
	}
	afterAddCol = strings.TrimSpace(afterAddCol)

	return afterAddCol
}

// extractConstraintFromAddStatement extracts the constraint definition from ALTER TABLE ADD CONSTRAINT
func extractConstraintFromAddStatement(alterSQL string) string {
	lines := strings.Split(alterSQL, "\n")
	var constraintLines []string
	inConstraint := false

	for _, line := range lines {
		upperLine := strings.ToUpper(strings.TrimSpace(line))

		if strings.Contains(upperLine, "ADD CONSTRAINT") {
			inConstraint = true
			// Extract the constraint name and start of definition
			addConstIndex := strings.Index(upperLine, "ADD CONSTRAINT")
			constraintPart := strings.TrimSpace(line[addConstIndex+len("ADD CONSTRAINT"):])
			constraintLines = append(constraintLines, "CONSTRAINT "+constraintPart)
		} else if inConstraint && strings.TrimSpace(line) != "" {
			// Continue collecting constraint definition until we hit a semicolon or end
			constraintLines = append(constraintLines, strings.TrimSpace(line))
			if strings.HasSuffix(strings.TrimSpace(line), ";") {
				break
			}
		}
	}

	if len(constraintLines) > 0 {
		result := strings.Join(constraintLines, "\n        ")
		// Remove trailing semicolon since it will be inside the CREATE statement
		result = strings.TrimRight(result, ";")
		return result
	}

	return ""
}

// extractColumnName extracts the column name from a column definition
// Example: "email VARCHAR(255) NOT NULL" -> "email"
func extractColumnName(columnDef string) string {
	// Trim whitespace
	columnDef = strings.TrimSpace(columnDef)

	// Split by whitespace to get the first word (column name)
	parts := strings.Fields(columnDef)
	if len(parts) > 0 {
		// Remove any quotes from the column name
		columnName := strings.Trim(parts[0], `"`)
		// Normalize to lowercase for case-insensitive comparison
		return strings.ToLower(columnName)
	}

	return ""
}

// integrateColumnsAndConstraintsIntoCreate integrates both columns and constraints into CREATE statement
func integrateColumnsAndConstraintsIntoCreate(createSQL string, columns []string, constraints []string) string {
	// Find the last closing parenthesis of the CREATE statement
	lastParenIndex := strings.LastIndex(createSQL, ")")
	if lastParenIndex == -1 {
		return createSQL // Can't parse, return original
	}

	beforeParen := createSQL[:lastParenIndex]
	afterParen := createSQL[lastParenIndex:]

	var additions []string

	// Add columns
	for _, col := range columns {
		additions = append(additions, "    "+col)
	}

	// Add constraints
	for _, constraint := range constraints {
		additions = append(additions, "    "+constraint)
	}

	if len(additions) > 0 {
		// Remove any trailing whitespace and commas from beforeParen
		beforeParen = strings.TrimRight(beforeParen, " \t\n")

		// Check if we need to add a comma
		if !strings.HasSuffix(beforeParen, ",") && !strings.HasSuffix(beforeParen, "(") {
			beforeParen += ","
		}

		// Add the new columns and constraints with proper formatting
		beforeParen += "\n" + strings.Join(additions, ",\n")
	}

	// Ensure proper formatting with closing paren on its own line
	result := beforeParen + "\n" + strings.TrimLeft(afterParen, " \t")

	// Ensure the statement ends with a semicolon
	result = strings.TrimRight(result, " \t\n")
	if !strings.HasSuffix(result, ";") {
		result += ";"
	}

	return result
}

// integrateAlterTypeIntoCreate merges ALTER TYPE ADD VALUE statements into CREATE TYPE
func integrateAlterTypeIntoCreate(createSQL string, alterStmts []types.Statement) string {
	// Extract new values from ALTER TYPE ADD VALUE statements
	var newValues []string
	for _, alterStmt := range alterStmts {
		if alterStmt.AlterTypeNewValue != "" {
			newValues = append(newValues, alterStmt.AlterTypeNewValue)
		}
	}

	if len(newValues) == 0 {
		return createSQL // No ALTER TYPE ADD VALUE statements to merge
	}

	// Parse existing CREATE TYPE to extract current values
	// Match: CREATE TYPE name AS ENUM ('value1', 'value2')
	upperSQL := strings.ToUpper(createSQL)
	enumStart := strings.Index(upperSQL, "AS ENUM")
	if enumStart == -1 {
		return createSQL // Not an ENUM type, can't merge
	}

	// Find the parentheses containing enum values
	parenStart := strings.Index(createSQL[enumStart:], "(")
	if parenStart == -1 {
		return createSQL
	}
	parenStart += enumStart

	parenEnd := strings.Index(createSQL[parenStart:], ")")
	if parenEnd == -1 {
		return createSQL
	}
	parenEnd += parenStart

	// Extract existing values
	valuesStr := createSQL[parenStart+1 : parenEnd]
	existingValues := parseEnumValuesFromSQL(valuesStr)

	// Merge new values (avoid duplicates)
	allValues := existingValues
	for _, newVal := range newValues {
		if !containsValue(existingValues, newVal) {
			allValues = append(allValues, newVal)
		}
	}

	// Reconstruct the CREATE TYPE statement with all values
	quotedValues := make([]string, len(allValues))
	for i, val := range allValues {
		quotedValues[i] = fmt.Sprintf("'%s'", val)
	}

	beforeValues := createSQL[:parenStart+1]
	afterValues := createSQL[parenEnd:]
	return beforeValues + strings.Join(quotedValues, ", ") + afterValues
}

// parseEnumValuesFromSQL extracts enum values from the values string
// Input: "'active', 'inactive', 'suspended'"
// Output: ["active", "inactive", "suspended"]
func parseEnumValuesFromSQL(valuesStr string) []string {
	var values []string
	// Remove whitespace and split by comma
	parts := strings.Split(valuesStr, ",")
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		// Remove surrounding quotes
		if len(trimmed) >= 2 && trimmed[0] == '\'' && trimmed[len(trimmed)-1] == '\'' {
			value := trimmed[1 : len(trimmed)-1]
			values = append(values, value)
		}
	}
	return values
}

// containsValue checks if a string slice contains a specific value
func containsValue(slice []string, value string) bool {
	for _, item := range slice {
		if item == value {
			return true
		}
	}
	return false
}
