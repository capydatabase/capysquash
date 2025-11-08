package consolidation

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// DOBlockAlterTableRule extracts ALTER TABLE statements from DO blocks
// This allows ALTER statements wrapped in IF NOT EXISTS checks to be consolidated
// with their corresponding CREATE TABLE statements
type DOBlockAlterTableRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *DOBlockAlterTableRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	// Only apply to DO blocks
	if lifecycle.Type != types.TypeDoBlock {
		return false
	}

	// Check if we have ALTER TABLE statements inside DO blocks
	hasAlterTable := false
	for _, event := range lifecycle.History {
		sql := strings.ToUpper(event.Statement.SQL)
		if strings.Contains(sql, "DO") && strings.Contains(sql, "ALTER TABLE") {
			hasAlterTable = true
			break
		}
	}

	return hasAlterTable
}

// Apply applies the consolidation rule to the given lifecycle
func (r *DOBlockAlterTableRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]interface{}{"rule": "DOBlockAlterTableRule"})
	}

	// Extract ALTER TABLE statements from DO blocks
	var extractedStatements []string
	var doBlockStmts []types.Statement

	for _, event := range lifecycle.History {
		sql := event.Statement.SQL
		sqlUpper := strings.ToUpper(sql)

		if strings.Contains(sqlUpper, "DO") && strings.Contains(sqlUpper, "ALTER TABLE") {
			doBlockStmts = append(doBlockStmts, event.Statement)

			// Extract all ALTER TABLE statements from within the DO block
			alterStatements := extractAlterStatementsFromDoBlock(sql)
			extractedStatements = append(extractedStatements, alterStatements...)
		}
	}

	// If no ALTER statements were extracted, return empty result
	if len(extractedStatements) == 0 {
		return &tracking.ConsolidationResult{
			OriginalStatements: doBlockStmts,
			ConsolidatedSQL:    "",
			Optimizations: []string{
				"DO block did not contain extractable ALTER statements",
			},
			RiskLevel: tracking.RiskLevelLow,
			EstimatedSavings: tracking.SquashSavings{
				StatementsReduced: len(doBlockStmts),
				FilesAffected:     len(doBlockStmts),
				LinesReduced:      len(doBlockStmts) * 10,
			},
		}, nil
	}

	// Join all extracted ALTER statements
	consolidatedSQL := strings.Join(extractedStatements, "\n")

	result := &tracking.ConsolidationResult{
		OriginalStatements: doBlockStmts,
		ConsolidatedSQL:    consolidatedSQL,
		Optimizations: []string{
			fmt.Sprintf("Extracted %d ALTER TABLE statement(s) from %d DO block(s)", len(extractedStatements), len(doBlockStmts)),
		},
		RiskLevel: tracking.RiskLevelLow,
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(doBlockStmts) - len(extractedStatements),
			FilesAffected:     len(doBlockStmts),
			LinesReduced:      len(doBlockStmts) * 8,
		},
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *DOBlockAlterTableRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow
}

// extractAlterStatementsFromDoBlock extracts ALTER TABLE statements from a DO block
func extractAlterStatementsFromDoBlock(doBlockSQL string) []string {
	var statements []string

	// Strategy: Extract the ALTER TABLE statement from inside the IF NOT EXISTS check
	// The structure is typically:
	//   DO $$
	//   BEGIN
	//     IF NOT EXISTS (...) THEN
	//       ALTER TABLE table_name ADD COLUMN ...;
	//     END IF;
	//   END $$;

	// Pattern 1: Extract ALTER TABLE ... ADD COLUMN statements (handles multi-line GENERATED columns)
	// This pattern captures everything from ALTER TABLE until the next semicolon at the end of a line
	addColumnPattern := regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(\S+)\s+ADD\s+COLUMN\s+([^;]+);`)
	matches := addColumnPattern.FindAllStringSubmatch(doBlockSQL, -1)

	for _, match := range matches {
		if len(match) >= 3 {
			tableName := strings.TrimSpace(match[1])
			columnDef := strings.TrimSpace(match[2])

			// Clean up the column definition (remove extra whitespace/newlines)
			columnDef = normalizeWhitespace(columnDef)

			if columnDef != "" {
				stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", tableName, columnDef)
				statements = append(statements, stmt)
			}
		}
	}

	// Pattern 2: Extract ALTER TABLE ... ADD CONSTRAINT statements
	addConstraintPattern := regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(\S+)\s+ADD\s+CONSTRAINT\s+([^;]+);`)
	constraintMatches := addConstraintPattern.FindAllStringSubmatch(doBlockSQL, -1)

	for _, match := range constraintMatches {
		if len(match) >= 3 {
			tableName := strings.TrimSpace(match[1])
			constraintDef := strings.TrimSpace(match[2])

			// Clean up constraint definition
			constraintDef = normalizeWhitespace(constraintDef)

			if constraintDef != "" {
				stmt := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s;", tableName, constraintDef)
				statements = append(statements, stmt)
			}
		}
	}

	// Pattern 3: Extract other ALTER TABLE operations (ENABLE/DISABLE RLS, etc.)
	otherAlterPattern := regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(\S+)\s+((?:ENABLE|DISABLE)\s+ROW\s+LEVEL\s+SECURITY);`)
	otherMatches := otherAlterPattern.FindAllStringSubmatch(doBlockSQL, -1)

	for _, match := range otherMatches {
		if len(match) >= 3 {
			tableName := strings.TrimSpace(match[1])
			alterAction := strings.TrimSpace(match[2])

			stmt := fmt.Sprintf("ALTER TABLE %s %s;", tableName, alterAction)
			statements = append(statements, stmt)
		}
	}

	return statements
}

// normalizeWhitespace collapses multiple whitespace characters (including newlines) into single spaces
func normalizeWhitespace(s string) string {
	// Replace all whitespace sequences with a single space
	normalized := regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(normalized)
}

// cleanAlterAction removes IF NOT EXISTS logic and extracts the actual ALTER action
func cleanAlterAction(alterAction string) string {
	// Remove trailing semicolons
	alterAction = strings.TrimRight(alterAction, ";")

	// If the action contains IF NOT EXISTS checks, we want to preserve the ALTER action
	// but remove the IF logic since we're extracting to a standalone statement

	// Pattern: ADD COLUMN column_name type ...
	addColumnPattern := regexp.MustCompile(`(?is)ADD\s+COLUMN\s+([^\s]+\s+[^\s,;]+(?:\s+(?:REFERENCES|DEFAULT|NOT\s+NULL|NULL|UNIQUE|CHECK|GENERATED)[^\n,;]*)*?)(?:\s*;|\s*$|\s+ON\s+DELETE|\s+ON\s+UPDATE)`)
	if matches := addColumnPattern.FindStringSubmatch(alterAction); len(matches) > 1 {
		columnDef := strings.TrimSpace(matches[1])
		return "ADD COLUMN " + columnDef
	}

	// Pattern: ADD CONSTRAINT constraint_name ...
	addConstraintPattern := regexp.MustCompile(`(?is)ADD\s+CONSTRAINT\s+([^\s]+)\s+((?:CHECK|FOREIGN\s+KEY|PRIMARY\s+KEY|UNIQUE)[^;]*)`)
	if matches := addConstraintPattern.FindStringSubmatch(alterAction); len(matches) > 2 {
		constraintName := strings.TrimSpace(matches[1])
		constraintDef := strings.TrimSpace(matches[2])
		return fmt.Sprintf("ADD CONSTRAINT %s %s", constraintName, constraintDef)
	}

	// Pattern: ENABLE/DISABLE ROW LEVEL SECURITY
	if strings.Contains(strings.ToUpper(alterAction), "ROW LEVEL SECURITY") {
		if strings.Contains(strings.ToUpper(alterAction), "ENABLE") {
			return "ENABLE ROW LEVEL SECURITY"
		} else if strings.Contains(strings.ToUpper(alterAction), "DISABLE") {
			return "DISABLE ROW LEVEL SECURITY"
		}
	}

	// For other ALTER actions, return as-is
	return alterAction
}
