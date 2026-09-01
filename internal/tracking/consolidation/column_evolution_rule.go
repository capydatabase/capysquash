package consolidation

import (
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/capysquash/pgsquash-engine/internal/utils"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// ColumnEvolutionRule handles complex column lifecycle patterns
type ColumnEvolutionRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *ColumnEvolutionRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	if lifecycle.Type != types.TypeTable {
		return false
	}

	debugColumnEvolution("CanApply table=%s history_events=%d", lifecycle.Name, len(lifecycle.History))
	for i, event := range lifecycle.History {
		debugColumnEvolution(
			"  Event %d: Op=%s SQL=%s",
			i,
			event.Operation,
			strings.ReplaceAll(event.Statement.SQL[:min(80, len(event.Statement.SQL))], "\n", "\\n"),
		)
	}

	// Check if there are column-related ALTER operations
	hasColumnOps := false
	hasRenameColumn := false
	for _, event := range lifecycle.History {
		if event.Operation == types.OpAlter {
			alterSQL := strings.ToUpper(event.Statement.SQL)
			if strings.Contains(alterSQL, "RENAME COLUMN") {
				hasRenameColumn = true
			}
			if strings.Contains(alterSQL, "ADD COLUMN") ||
				strings.Contains(alterSQL, "DROP COLUMN") ||
				strings.Contains(alterSQL, "ALTER COLUMN") ||
				strings.Contains(alterSQL, "RENAME COLUMN") {
				hasColumnOps = true
				break
			}
		}
	}

	// Log if we found RENAME COLUMN
	if hasRenameColumn {
		debugColumnEvolution("Table %s has RENAME COLUMN operations, CanApply=true", lifecycle.Name)
	} else {
		debugColumnEvolution("Table %s has NO RENAME COLUMN operations, CanApply=%v", lifecycle.Name, hasColumnOps)
	}

	return hasColumnOps
}

// Apply applies the consolidation rule to the given lifecycle
func (r *ColumnEvolutionRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]any{"rule": "ColumnEvolutionRule"})
	}

	// Track column evolution through lifecycle
	columnChanges := r.analyzeColumnEvolution(lifecycle)

	// Track column renames for view rewriting
	columnEvolutions := r.trackColumnRenames(lifecycle)

	// Generate final schema with all column changes applied
	finalSQL := r.generateFinalSchema(lifecycle, columnChanges)

	// Find all column-related statements for consolidation
	var originalStmts []types.Statement
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate ||
			(event.Operation == types.OpAlter && r.isColumnOperation(event.Statement.SQL)) {
			originalStmts = append(originalStmts, event.Statement)
		}
	}

	result := &tracking.ConsolidationResult{
		OriginalStatements: originalStmts,
		ConsolidatedSQL:    finalSQL,
		Optimizations: []string{
			fmt.Sprintf("Consolidated %d column operations into final schema", len(columnChanges)),
		},
		RiskLevel: tracking.RiskLevelMedium, // Medium risk due to schema changes
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(originalStmts) - 1,
			FilesAffected:     len(originalStmts),
			LinesReduced:      len(originalStmts) * 3,
		},
		// Populate column evolutions for view rewriting
		ColumnEvolutions: columnEvolutions,
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *ColumnEvolutionRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelMedium
}

// ColumnChange tracks a column modification operation
type ColumnChange struct {
	Name      string
	Operation string // ADD, DROP, ALTER, RENAME
	DataType  string
	Default   string
	NotNull   bool
	Order     int
}

func (r *ColumnEvolutionRule) analyzeColumnEvolution(lifecycle *tracking.ObjectLifecycle) []ColumnChange {
	var changes []ColumnChange
	order := 0

	for _, event := range lifecycle.History {
		if event.Operation == types.OpAlter {
			alterSQL := event.Statement.SQL

			// Extract column operations from ALTER statements
			if change := r.extractColumnChange(alterSQL, order); change != nil {
				changes = append(changes, *change)
				order++
			}
		}
	}

	return changes
}

func (r *ColumnEvolutionRule) extractColumnChange(alterSQL string, order int) *ColumnChange {
	upperSQL := strings.ToUpper(alterSQL)

	if strings.Contains(upperSQL, "ADD COLUMN") {
		// Extract ADD COLUMN operation
		return r.parseAddColumn(alterSQL, order)
	} else if strings.Contains(upperSQL, "DROP COLUMN") {
		// Extract DROP COLUMN operation
		return r.parseDropColumn(alterSQL, order)
	} else if strings.Contains(upperSQL, "ALTER COLUMN") {
		// Extract ALTER COLUMN operation
		return r.parseAlterColumn(alterSQL, order)
	}

	return nil
}

func (r *ColumnEvolutionRule) parseAddColumn(alterSQL string, order int) *ColumnChange {
	// Simple extraction for ADD COLUMN
	lines := strings.SplitSeq(alterSQL, "\n")
	for line := range lines {
		upperLine := strings.ToUpper(strings.TrimSpace(line))
		if strings.Contains(upperLine, "ADD COLUMN") {
			// Extract column name and definition
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				return &ColumnChange{
					Name:      parts[3], // Column name after "ADD COLUMN"
					Operation: "ADD",
					Order:     order,
				}
			}
		}
	}
	return nil
}

func (r *ColumnEvolutionRule) parseDropColumn(alterSQL string, order int) *ColumnChange {
	// Simple extraction for DROP COLUMN
	lines := strings.SplitSeq(alterSQL, "\n")
	for line := range lines {
		upperLine := strings.ToUpper(strings.TrimSpace(line))
		if strings.Contains(upperLine, "DROP COLUMN") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				return &ColumnChange{
					Name:      parts[3], // Column name after "DROP COLUMN"
					Operation: "DROP",
					Order:     order,
				}
			}
		}
	}
	return nil
}

func (r *ColumnEvolutionRule) parseAlterColumn(alterSQL string, order int) *ColumnChange {
	// Simple extraction for ALTER COLUMN
	lines := strings.SplitSeq(alterSQL, "\n")
	for line := range lines {
		upperLine := strings.ToUpper(strings.TrimSpace(line))
		if strings.Contains(upperLine, "ALTER COLUMN") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				return &ColumnChange{
					Name:      parts[3], // Column name after "ALTER COLUMN"
					Operation: "ALTER",
					Order:     order,
				}
			}
		}
	}
	return nil
}

func (r *ColumnEvolutionRule) generateFinalSchema(lifecycle *tracking.ObjectLifecycle, columnChanges []ColumnChange) string {
	// Get the base CREATE statement
	var baseCreateStmt *types.Statement
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			baseCreateStmt = &event.Statement
			break
		}
	}

	if baseCreateStmt == nil {
		return ""
	}

	// Collect all ALTER statements that add columns
	var alterStmts []types.Statement
	for _, event := range lifecycle.History {
		if event.Operation == types.OpAlter {
			alterSQL := strings.ToUpper(event.Statement.SQL)
			if strings.Contains(alterSQL, "ADD COLUMN") {
				alterStmts = append(alterStmts, event.Statement)
			}
		}
	}

	// Use the working integrateAlterIntoCreate function from create_alter_rule.go
	// This properly parses and integrates ALTER TABLE ADD COLUMN statements
	finalSQL := integrateAlterIntoCreate(baseCreateStmt, alterStmts)

	// Add a comment about the column changes
	if len(columnChanges) > 0 {
		comment := fmt.Sprintf("-- Column evolution: %d operations consolidated\n", len(columnChanges))
		finalSQL = comment + finalSQL
	}

	return finalSQL
}

func (r *ColumnEvolutionRule) isColumnOperation(sql string) bool {
	upperSQL := strings.ToUpper(sql)
	return strings.Contains(upperSQL, "ADD COLUMN") ||
		strings.Contains(upperSQL, "DROP COLUMN") ||
		strings.Contains(upperSQL, "ALTER COLUMN") ||
		strings.Contains(upperSQL, "RENAME COLUMN")
}

// trackColumnRenames extracts RENAME COLUMN operations and builds ColumnEvolutionInfo map
func (r *ColumnEvolutionRule) trackColumnRenames(lifecycle *tracking.ObjectLifecycle) map[string]*tracking.ColumnEvolutionInfo {
	debugColumnEvolution("trackColumnRenames called for table %s", lifecycle.Name)
	evolutions := make(map[string]*tracking.ColumnEvolutionInfo)

	// Map to track current column names (oldName -> currentName)
	columnNames := make(map[string]string)

	// Extract table name from lifecycle
	tableName := lifecycle.Name
	if parts := strings.Split(lifecycle.Name, "."); len(parts) > 1 {
		tableName = parts[1] // Extract table name from schema.table
	}
	debugColumnEvolution("Table name extracted: %s", tableName)

	// First pass: Extract initial column names from CREATE TABLE
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			// Initialize column names from CREATE statement
			// This is a simplified extraction - in real use, would parse AST
			createSQL := event.Statement.SQL
			columnNames = r.extractColumnNamesFromCreate(createSQL)
			break
		}
	}

	// Second pass: Track RENAME COLUMN operations
	for _, event := range lifecycle.History {
		if event.Operation == types.OpAlter {
			alterSQL := event.Statement.SQL
			upperSQL := strings.ToUpper(alterSQL)

			if strings.Contains(upperSQL, "RENAME COLUMN") {
				// Extract RENAME COLUMN operation
				// Format: ALTER TABLE table_name RENAME COLUMN old_name TO new_name
				oldName, newName := r.extractRenameColumns(alterSQL)
				debugColumnEvolution("Found RENAME COLUMN: %s -> %s", oldName, newName)
				if oldName != "" && newName != "" {
					// Find the original name (trace back through renames)
					originalName := oldName
					if _, exists := columnNames[oldName]; exists {
						// This column was already tracked, find its original name
						for origName, currName := range columnNames {
							if currName == oldName {
								originalName = origName
								break
							}
						}
					}

					// Create or update evolution info
					key := fmt.Sprintf("%s.%s", tableName, originalName)
					if evolution, exists := evolutions[key]; exists {
						// Append to rename chain
						evolution.RenameChain = append(evolution.RenameChain, newName)
						evolution.FinalName = newName
					} else {
						// Create new evolution info
						evolutions[key] = &tracking.ColumnEvolutionInfo{
							TableName:    tableName,
							OriginalName: originalName,
							FinalName:    newName,
							RenameChain:  []string{originalName, newName},
						}
					}

					// Update column name mapping
					columnNames[newName] = newName
					delete(columnNames, oldName)
				}
			}
		}
	}

	debugColumnEvolution("trackColumnRenames completed: found %d column evolutions", len(evolutions))
	for key, evo := range evolutions {
		debugColumnEvolution("  %s: %s -> %s", key, evo.OriginalName, evo.FinalName)
	}
	return evolutions
}

func debugColumnEvolution(format string, args ...any) {
	logger := utils.GetDefaultLogger()
	if logger == nil {
		return
	}
	logger.WithPrefix("COLUMN-EVO").Debug(format, args...)
}

// extractColumnNamesFromCreate extracts column names from CREATE TABLE statement
func (r *ColumnEvolutionRule) extractColumnNamesFromCreate(createSQL string) map[string]string {
	columnNames := make(map[string]string)

	// Simple extraction: find lines between CREATE TABLE and first constraint
	// This is simplified - a robust implementation would use AST parsing
	lines := strings.Split(createSQL, "\n")
	inColumns := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		upperLine := strings.ToUpper(trimmed)

		if strings.Contains(upperLine, "CREATE TABLE") {
			inColumns = true
			continue
		}

		if inColumns {
			// Stop at constraints or closing paren
			if strings.HasPrefix(upperLine, "CONSTRAINT") ||
				strings.HasPrefix(upperLine, "PRIMARY KEY") ||
				strings.HasPrefix(upperLine, "FOREIGN KEY") ||
				strings.HasPrefix(upperLine, "UNIQUE") ||
				strings.HasPrefix(upperLine, "CHECK") ||
				trimmed == ");" || trimmed == ")" {
				break
			}

			// Extract column name (first word, possibly quoted)
			if trimmed != "" && !strings.HasPrefix(trimmed, "--") {
				parts := strings.Fields(trimmed)
				if len(parts) > 0 {
					colName := strings.TrimLeft(parts[0], "\"")
					colName = strings.TrimRight(colName, "\",")
					if colName != "" && colName != "(" {
						columnNames[colName] = colName
					}
				}
			}
		}
	}

	return columnNames
}

// extractRenameColumns extracts old and new column names from RENAME COLUMN statement
func (r *ColumnEvolutionRule) extractRenameColumns(alterSQL string) (string, string) {
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
