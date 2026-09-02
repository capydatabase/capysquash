package consolidation

import (
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// SeparateAlterRule identifies ALTER statements that cannot be integrated into CREATE TABLE
// and must remain as separate statements. This includes RLS operations, column modifications,
// renames, owner changes, and schema changes.
//
// This rule is critical for maintaining PostgreSQL execution order requirements:
// - RLS can only be enabled AFTER table exists
// - Column modifications require existing columns
// - Renames/ownership changes are administrative operations
type SeparateAlterRule struct{}

// CanApply checks if the lifecycle has ALTER statements that must remain separate
func (r *SeparateAlterRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	if lifecycle.Type != types.TypeTable {
		return false
	}

	// Check if there are ALTER statements that must be kept separate
	for _, event := range lifecycle.History {
		if event.Operation == types.OpAlter && r.mustBeSeparate(event.Statement.SQL) {
			return true
		}
	}

	return false
}

// Apply separates ALTER statements that cannot be integrated into CREATE TABLE
func (r *SeparateAlterRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]any{"rule": "SeparateAlterRule"})
	}

	// Get the CREATE TABLE statement
	var createStmt *types.Statement
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			createStmt = &event.Statement
			break
		}
	}

	if createStmt == nil {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "no CREATE statement found", map[string]any{"object": lifecycle.Name})
	}

	// Identify all ALTER statements that must remain separate
	var separateAlters []types.Statement
	var integrableAlters []types.Statement

	for _, event := range lifecycle.History {
		if event.Operation == types.OpAlter {
			if r.mustBeSeparate(event.Statement.SQL) {
				separateAlters = append(separateAlters, event.Statement)
			} else {
				integrableAlters = append(integrableAlters, event.Statement)
			}
		}
	}

	// Build consolidated SQL: CREATE TABLE (possibly with integrated ALTERs) + separate ALTERs
	consolidatedSQL := createStmt.SQL

	// Ensure CREATE TABLE ends with semicolon before appending separate ALTERs
	consolidatedSQL = strings.TrimSpace(consolidatedSQL)
	if !strings.HasSuffix(consolidatedSQL, ";") {
		consolidatedSQL += ";"
	}

	// Append ALTER statements that must be separate
	for _, alterStmt := range separateAlters {
		alterSQL := strings.TrimSpace(alterStmt.SQL)
		// Ensure each ALTER statement ends with semicolon
		if !strings.HasSuffix(alterSQL, ";") {
			alterSQL += ";"
		}
		consolidatedSQL += "\n\n" + alterSQL
	}

	// Build optimizations description
	optimizations := []string{
		fmt.Sprintf("Identified %d ALTER statements that must remain separate", len(separateAlters)),
	}
	if len(integrableAlters) > 0 {
		optimizations = append(optimizations, fmt.Sprintf("%d ALTER statements can be integrated by other rules", len(integrableAlters)))
	}

	// Track which ALTER types were kept separate
	alterTypes := r.categorizeAlters(separateAlters)
	if len(alterTypes) > 0 {
		optimizations = append(optimizations, fmt.Sprintf("Separate ALTER types: %s", strings.Join(alterTypes, ", ")))
	}

	// Build list of original statements
	originalStmts := []types.Statement{*createStmt}
	originalStmts = append(originalStmts, separateAlters...)

	// Track column renames for view rewriting
	// Even though RENAMEs are kept separate, we need to track them so views can be rewritten
	columnEvolutions := trackColumnRenamesFromLifecycle(lifecycle)

	result := &tracking.ConsolidationResult{
		OriginalStatements: originalStmts,
		ConsolidatedSQL:    consolidatedSQL,
		Optimizations:      optimizations,
		RiskLevel:          tracking.RiskLevelLow, // Preserving execution order is safe
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: 0, // Not reducing statements, just organizing them
			FilesAffected:     len(originalStmts),
			LinesReduced:      0,
		},
		// Populate column evolutions for view rewriting
		ColumnEvolutions: columnEvolutions,
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *SeparateAlterRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow
}

// mustBeSeparate determines if an ALTER statement cannot be integrated into CREATE TABLE
func (r *SeparateAlterRule) mustBeSeparate(sql string) bool {
	upperSQL := strings.ToUpper(sql)

	// RLS operations - must be after table creation
	if strings.Contains(upperSQL, "ENABLE ROW LEVEL SECURITY") ||
		strings.Contains(upperSQL, "DISABLE ROW LEVEL SECURITY") ||
		strings.Contains(upperSQL, "FORCE ROW LEVEL SECURITY") ||
		strings.Contains(upperSQL, "NO FORCE ROW LEVEL SECURITY") {
		return true
	}

	// Column modifications - modify existing columns
	if strings.Contains(upperSQL, "ALTER COLUMN") {
		return true
	}

	// Column drops - remove existing columns
	if strings.Contains(upperSQL, "DROP COLUMN") {
		return true
	}

	// Column/table renames - administrative operations
	if strings.Contains(upperSQL, "RENAME COLUMN") || strings.Contains(upperSQL, "RENAME TO") {
		return true
	}

	// Ownership changes - administrative operations
	if strings.Contains(upperSQL, "OWNER TO") {
		return true
	}

	// Schema changes - administrative operations
	if strings.Contains(upperSQL, "SET SCHEMA") {
		return true
	}

	// Constraint modifications (not additions)
	if strings.Contains(upperSQL, "DROP CONSTRAINT") ||
		strings.Contains(upperSQL, "VALIDATE CONSTRAINT") ||
		strings.Contains(upperSQL, "ALTER CONSTRAINT") {
		return true
	}

	// Inheritance changes
	if strings.Contains(upperSQL, "INHERIT") || strings.Contains(upperSQL, "NO INHERIT") {
		return true
	}

	// Table settings
	if strings.Contains(upperSQL, "SET (") || strings.Contains(upperSQL, "RESET (") {
		return true
	}

	return false
}

// categorizeAlters groups ALTER statements by type for reporting
func (r *SeparateAlterRule) categorizeAlters(alters []types.Statement) []string {
	typeMap := make(map[string]bool)

	for _, stmt := range alters {
		upperSQL := strings.ToUpper(stmt.SQL)

		if strings.Contains(upperSQL, "ROW LEVEL SECURITY") {
			typeMap["RLS"] = true
		}
		if strings.Contains(upperSQL, "ALTER COLUMN") {
			typeMap["ALTER COLUMN"] = true
		}
		if strings.Contains(upperSQL, "DROP COLUMN") {
			typeMap["DROP COLUMN"] = true
		}
		if strings.Contains(upperSQL, "RENAME") {
			typeMap["RENAME"] = true
		}
		if strings.Contains(upperSQL, "OWNER TO") {
			typeMap["OWNER"] = true
		}
		if strings.Contains(upperSQL, "SET SCHEMA") {
			typeMap["SCHEMA"] = true
		}
		if strings.Contains(upperSQL, "CONSTRAINT") {
			typeMap["CONSTRAINT"] = true
		}
		if strings.Contains(upperSQL, "INHERIT") {
			typeMap["INHERIT"] = true
		}
	}

	types := make([]string, 0, len(typeMap))
	for t := range typeMap {
		types = append(types, t)
	}

	return types
}

// This is used by both SeparateAlterRule and ColumnEvolutionRule
func trackColumnRenamesFromLifecycle(lifecycle *tracking.ObjectLifecycle) map[string]*tracking.ColumnEvolutionInfo {
	evolutions := make(map[string]*tracking.ColumnEvolutionInfo)

	// Extract table name from lifecycle
	tableName := lifecycle.Name
	if parts := strings.Split(lifecycle.Name, "."); len(parts) > 1 {
		tableName = parts[1] // Extract table name from schema.table
	}

	// Track RENAME COLUMN operations
	for _, event := range lifecycle.History {
		if event.Operation == types.OpAlter {
			alterSQL := event.Statement.SQL
			upperSQL := strings.ToUpper(alterSQL)

			if strings.Contains(upperSQL, "RENAME COLUMN") {
				// Extract old and new column names
				oldName, newName := extractRenameColumns(alterSQL)
				if oldName != "" && newName != "" {
					key := fmt.Sprintf("%s.%s", tableName, oldName)
					evolutions[key] = &tracking.ColumnEvolutionInfo{
						TableName:    tableName,
						OriginalName: oldName,
						FinalName:    newName,
						RenameChain:  []string{oldName, newName},
					}
				}
			}
		}
	}

	return evolutions
}

// extractRenameColumns extracts old and new column names from RENAME COLUMN statement
func extractRenameColumns(alterSQL string) (string, string) {
	// Format: ALTER TABLE table_name RENAME COLUMN old_name TO new_name
	upperSQL := strings.ToUpper(alterSQL)

	renameIdx := strings.Index(upperSQL, "RENAME COLUMN")
	if renameIdx == -1 {
		return "", ""
	}

	// Extract the part after "RENAME COLUMN"
	afterRename := alterSQL[renameIdx+len("RENAME COLUMN"):]
	parts := strings.Fields(strings.TrimSpace(afterRename))

	if len(parts) < 3 {
		return "", ""
	}

	// parts[0] = old_name, parts[1] = TO, parts[2] = new_name
	oldName := strings.Trim(parts[0], "\"")
	newName := strings.Trim(parts[2], "\";")

	return oldName, newName
}
