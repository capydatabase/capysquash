package consolidation

import (
	"fmt"
	"slices"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/utils"

	"github.com/capy-base/pgsquash-engine/internal/tracking"
	"github.com/capy-base/pgsquash-engine/internal/types"

	"github.com/capy-base/pgsquash-engine/internal/errors"
)

// CreateAlterConsolidationRule consolidates CREATE statements followed by ALTER statements
type CreateAlterConsolidationRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *CreateAlterConsolidationRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	if len(lifecycle.History) < 2 {
		return false
	}

	if lifecycle.History[len(lifecycle.History)-1].Operation == types.OpDrop {
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
					utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Debug("profiles has %d CREATE operations, deferring to MultipleCreateConsolidationRule", createCount)
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
						utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Debug("profiles (type=%s) matches single CREATE with ALTER operations", lifecycle.Type)
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
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]any{"rule": "CreateAlterConsolidationRule"})
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

	// This happens when deparsing CHECK constraints with char_length() function calls
	// Fix it immediately after consolidation to prevent corruption in output
	if strings.Contains(consolidatedSQL, "char_char_length") {
		utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Info("CONSOLIDATION FIX: Found and fixing char_char_length corruption in %s", createStmt.ObjectName)
		consolidatedSQL = strings.ReplaceAll(consolidatedSQL, "char_char_length", "char_length")
	}

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
		utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Debug(
			"BEFORE consolidation for %s: SQL length=%d, starts with: %s",
			objectName, len(createSQL), createSQL[:min(100, len(createSQL))])
	}

	// Handle ENUM types specially - merge ALTER TYPE ADD VALUE into CREATE TYPE
	if createStmt.ObjectType == types.TypeEnum {
		return integrateAlterTypeIntoCreate(createSQL, alterStmts)
	}

	// Extract existing columns from base CREATE to avoid duplicates
	existingColumns := extractColumnsFromCreate(createSQL)

	// Extract column additions and constraints from ALTER statements
	// Use a map to track column definitions by name (last definition wins for duplicates)
	columnDefinitions := make(map[string]string) // map[columnName]columnDef
	var columnOrder []string                     // preserve order of first appearance
	var addedConstraints []string

	for _, alterStmt := range alterStmts {
		// Parse the ALTER statement directly to extract what needs to be added
		alterSQL := strings.TrimSpace(alterStmt.SQL)

		// DEBUG: Log all ALTER statements for profiles
		if strings.Contains(strings.ToLower(objectName), "profiles") {
			utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Debug(
				"Processing ALTER for %s: %s",
				objectName, alterSQL[:min(150, len(alterSQL))])
		}

		if strings.Contains(strings.ToUpper(alterSQL), "ADD COLUMN") {
			// Handle ALTER statements with multiple ADD COLUMN operations
			// Example: ALTER TABLE foo ADD COLUMN a TEXT, ADD COLUMN b INT;
			// We need to extract each column separately
			columns := extractMultipleAddColumnsFromAlter(alterSQL)

			for _, columnDef := range columns {
				if columnDef == "" {
					continue
				}

				// Extract column name (first word of the definition)
				columnName := extractColumnName(columnDef)

				// Skip if column already exists in base CREATE
				if _, existsInCreate := existingColumns[columnName]; existsInCreate {
					utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Info(
						"Skipping duplicate column '%s' - already exists in base CREATE for %s",
						columnName, objectName)
					continue
				}

				// Check for duplicate column definitions within ALTERs
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
			// Skip constraints inside DO blocks (they have conditional logic)
			if strings.Contains(strings.ToUpper(alterSQL), "DO $$") || strings.Contains(strings.ToUpper(alterSQL), "DO $BODY$") {
				utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Info(
					"Skipping constraint in DO block for %s - preserving conditional logic",
					objectName)
				continue
			}
			// Extract constraint definition from ADD CONSTRAINT statement
			if constraintDef := extractConstraintFromAddStatement(alterSQL); constraintDef != "" {
				// Check if constraint already exists inline in CREATE statement
				if !constraintExistsInline(createSQL, constraintDef) {
					addedConstraints = append(addedConstraints, constraintDef)
				} else {
					utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Info(
						"Skipping duplicate constraint for %s - already exists inline in CREATE",
						objectName)
				}
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

	// DEBUG: Log columns being added for profiles
	if strings.Contains(strings.ToLower(objectName), "profiles") {
		utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Debug(
			"Integrating %d columns for %s: %v",
			len(addedColumns), objectName, columnOrder)
	}

	// Integrate columns and constraints into the CREATE statement
	if len(addedColumns) > 0 || len(addedConstraints) > 0 {
		createSQL = integrateColumnsAndConstraintsIntoCreate(createSQL, addedColumns, addedConstraints)
	}

	// Ensure CREATE TABLE always ends with semicolon
	// Even if no columns/constraints were added, we still need a semicolon
	// because pg_query.SplitWithScanner strips semicolons during parsing
	createSQL = strings.TrimRight(createSQL, " \t\n")
	if !strings.HasSuffix(createSQL, ";") {
		createSQL += ";"
	}

	// DEBUG: Log outgoing SQL for analytics tables
	if strings.Contains(strings.ToLower(objectName), "analytics") {
		utils.GetDefaultLogger().WithPrefix("CREATE-ALTER").Debug(
			"AFTER consolidation for %s: SQL length=%d, starts with: %s, ends with: %s",
			objectName, len(createSQL), createSQL[:min(100, len(createSQL))], createSQL[max(0, len(createSQL)-50):])
	}

	return createSQL
}

// extractMultipleAddColumnsFromAlter extracts multiple column definitions from a single ALTER statement
// Handles cases like: ALTER TABLE foo ADD COLUMN a TEXT, ADD COLUMN b INT, ADD COLUMN c BOOL;
func extractMultipleAddColumnsFromAlter(alterSQL string) []string {
	upperSQL := strings.ToUpper(alterSQL)

	// Find all "ADD COLUMN" positions
	var columns []string
	searchStart := 0

	for {
		addColIndex := strings.Index(upperSQL[searchStart:], "ADD COLUMN")
		if addColIndex == -1 {
			break
		}

		// Adjust to absolute position
		addColIndex += searchStart

		// Find the next "ADD COLUMN" or end of statement
		nextAddCol := strings.Index(upperSQL[addColIndex+len("ADD COLUMN"):], "ADD COLUMN")
		var endPos int
		if nextAddCol == -1 {
			// No more ADD COLUMN, take until semicolon or end of string
			endPos = len(alterSQL)
			if semicolonPos := strings.Index(alterSQL[addColIndex:], ";"); semicolonPos != -1 {
				endPos = addColIndex + semicolonPos
			}
		} else {
			// There's another ADD COLUMN, take until there
			endPos = addColIndex + len("ADD COLUMN") + nextAddCol
		}

		// Extract this column definition
		columnPart := strings.TrimSpace(alterSQL[addColIndex+len("ADD COLUMN") : endPos])

		// Remove trailing comma if present
		columnPart = strings.TrimRight(columnPart, ",")
		columnPart = strings.TrimRight(columnPart, ";")
		columnPart = strings.TrimSpace(columnPart)

		// Strip "IF NOT EXISTS" clause
		upperColumnPart := strings.ToUpper(columnPart)
		if strings.HasPrefix(upperColumnPart, "IF NOT EXISTS ") {
			columnPart = strings.TrimSpace(columnPart[len("IF NOT EXISTS "):])
		}

		// Normalize multi-line column definitions
		columnPart = strings.ReplaceAll(columnPart, "\n", " ")
		for strings.Contains(columnPart, "  ") {
			columnPart = strings.ReplaceAll(columnPart, "  ", " ")
		}
		columnPart = strings.TrimSpace(columnPart)

		if columnPart != "" {
			columns = append(columns, columnPart)
		}

		// Move search position forward
		searchStart = endPos
		if searchStart >= len(upperSQL) {
			break
		}
	}

	return columns
}

// extractConstraintFromAddStatement extracts the constraint definition from ALTER TABLE ADD CONSTRAINT
func extractConstraintFromAddStatement(alterSQL string) string {
	lines := strings.Split(alterSQL, "\n")
	var constraintLines []string
	inConstraint := false

	for _, line := range lines {
		upperLine := strings.ToUpper(strings.TrimSpace(line))
		trimmedLine := strings.TrimSpace(line)

		if strings.Contains(upperLine, "ADD CONSTRAINT") {
			inConstraint = true
			// Find position in original line (case-insensitive)
			// Use case-insensitive search to find the actual position
			addConstIndex := strings.Index(upperLine, "ADD CONSTRAINT")
			// Calculate the position in the trimmed line
			constraintPart := strings.TrimSpace(trimmedLine[addConstIndex+len("ADD CONSTRAINT"):])
			constraintLines = append(constraintLines, "CONSTRAINT "+constraintPart)
		} else if inConstraint && trimmedLine != "" {
			// Continue collecting constraint definition until we hit a semicolon or end
			constraintLines = append(constraintLines, trimmedLine)
			if strings.HasSuffix(trimmedLine, ";") {
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

// constraintExistsInline checks if a constraint condition already exists inline in the CREATE statement
// Example: CREATE TABLE t (col INTEGER CHECK (col >= 0)) has inline constraint
// that would conflict with: CONSTRAINT t_col_check CHECK (col IS NULL OR col >= 0)
func constraintExistsInline(createSQL string, constraintDef string) bool {
	// Extract the constraint condition (the CHECK part)
	upperConstraint := strings.ToUpper(constraintDef)
	checkIndex := strings.Index(upperConstraint, "CHECK")
	if checkIndex == -1 {
		return false // Not a CHECK constraint
	}

	// Get the condition part after CHECK
	constraintCondition := strings.TrimSpace(constraintDef[checkIndex+5:])
	// Remove parentheses and normalize
	constraintCondition = strings.Trim(constraintCondition, " ()")
	constraintCondition = normalizeConstraintCondition(constraintCondition)

	// Check if this condition exists anywhere in the CREATE statement
	upperCreate := strings.ToUpper(createSQL)
	if strings.Contains(upperCreate, "CHECK") {
		// Extract all CHECK clauses from CREATE statement
		for line := range strings.SplitSeq(createSQL, "\n") {
			upperLine := strings.ToUpper(line)
			if strings.Contains(upperLine, "CHECK") {
				checkIdx := strings.Index(upperLine, "CHECK")
				existingCondition := strings.TrimSpace(line[checkIdx+5:])
				existingCondition = strings.Trim(existingCondition, " (),")
				existingCondition = normalizeConstraintCondition(existingCondition)

				// Compare conditions (case-insensitive, whitespace-normalized)
				if constraintsAreEquivalent(existingCondition, constraintCondition) {
					return true
				}
			}
		}
	}

	return false
}

// normalizeConstraintCondition normalizes a CHECK constraint condition for comparison
func normalizeConstraintCondition(condition string) string {
	// Convert to uppercase
	condition = strings.ToUpper(condition)
	// Remove extra whitespace
	for strings.Contains(condition, "  ") {
		condition = strings.ReplaceAll(condition, "  ", " ")
	}
	return strings.TrimSpace(condition)
}

// constraintsAreEquivalent checks if two constraint conditions are functionally equivalent
// Example: "col >= 0" is a subset of "col IS NULL OR col >= 0"
func constraintsAreEquivalent(cond1, cond2 string) bool {
	// Exact match
	if cond1 == cond2 {
		return true
	}

	// Check if one is a subset of the other (handles "col >= 0" vs "col IS NULL OR col >= 0")
	if strings.Contains(cond1, cond2) || strings.Contains(cond2, cond1) {
		return true
	}

	return false
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
	parts := strings.SplitSeq(valuesStr, ",")
	for part := range parts {
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
	return slices.Contains(slice, value)
}
