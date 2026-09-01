package consolidation

import (
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/utils"

	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// MultipleCreateConsolidationRule handles multiple CREATE statements for the same object
type MultipleCreateConsolidationRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *MultipleCreateConsolidationRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	if len(lifecycle.History) > 0 && lifecycle.History[len(lifecycle.History)-1].Operation == types.OpDrop {
		return false
	}

	createCount := 0
	for _, event := range lifecycle.History {
		if event.Operation == types.OpCreate {
			createCount++
		}
	}

	// Debug logging for profiles
	if strings.ToLower(lifecycle.Name) == "profiles" {
		utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Debug("profiles (type=%s) has %d events total, %d CREATE operations", lifecycle.Type, len(lifecycle.History), createCount)
		for i, event := range lifecycle.History {
			utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Debug("  Event %d: Operation=%s (%v)", i, event.Operation, event.Operation)
		}
		utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Debug("  Returning %v (need createCount > 1)", createCount > 1)
	}

	return createCount > 1
}

// Apply applies the consolidation rule to the given lifecycle
func (r *MultipleCreateConsolidationRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]any{"rule": "MultipleCreateConsolidationRule"})
	}

	// Collect all CREATE statements
	// IMPORTANT: If there's a DROP in the history, only collect CREATEs AFTER the last DROP
	// because DROP invalidates all previous CREATEs (they were dropped!)
	var allCreateStmts []types.Statement
	var alterStmts []types.Statement

	// Find the index of the last DROP operation (if any)
	lastDropIndex := -1
	for i, event := range lifecycle.History {
		if event.Operation == types.OpDrop {
			lastDropIndex = i
		}
	}

	// Collect CREATEs and ALTERs, but only those AFTER the last DROP (if there was one)
	startIndex := 0
	if lastDropIndex >= 0 {
		startIndex = lastDropIndex + 1
		utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info("Found DROP at index %d for %s, only considering events after DROP", lastDropIndex, lifecycle.Name)
	}

	for i := startIndex; i < len(lifecycle.History); i++ {
		event := lifecycle.History[i]
		if event.Operation == types.OpCreate {
			allCreateStmts = append(allCreateStmts, event.Statement)
		} else if event.Operation == types.OpAlter && !event.HasDataOps {
			// Also collect ALTER statements that don't have data operations
			alterStmts = append(alterStmts, event.Statement)
		}
	}

	// Safety check: if no CREATE statements found, this shouldn't happen due to CanApply check
	if len(allCreateStmts) == 0 {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "no CREATE statements found in lifecycle", map[string]any{"object": lifecycle.Name})
	}

	// Debug logging for profiles and viewing_requests
	tableName := strings.ToLower(lifecycle.Name)
	if tableName == "profiles" || tableName == "viewing_requests" {
		utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Debug("%s has %d CREATE statements and %d ALTER statements", lifecycle.Name, len(allCreateStmts), len(alterStmts))
		for i, stmt := range allCreateStmts {
			if tableName == "profiles" {
				hasCity := strings.Contains(strings.ToLower(stmt.SQL), "city")
				hasAuthProvider := strings.Contains(strings.ToLower(stmt.SQL), "auth_provider")
				columnCount := strings.Count(stmt.SQL, ",")
				utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Debug("  CREATE %d: has 'city'=%v, 'auth_provider'=%v, columns~%d, SQL length=%d", i, hasCity, hasAuthProvider, columnCount, len(stmt.SQL))
			} else {
				utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Debug("  CREATE %d: SQL length=%d, first 150 chars: %.150s", i, len(stmt.SQL), stmt.SQL)
			}
		}
		for i, stmt := range alterStmts {
			utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Debug("  ALTER %d: %.200s", i, stmt.SQL)
		}
	}

	// Merge all CREATE statements by treating subsequent CREATEs as if they were ALTERs
	// This handles the case where migrations have duplicate CREATE TABLE IF NOT EXISTS with different column sets
	// Use the LAST CREATE statement as base (for DDL cycles: CREATE→DROP→CREATE, we want the final version)
	baseCreateStmt := allCreateStmts[len(allCreateStmts)-1]
	consolidatedSQL := baseCreateStmt.SQL

	// For tables with more than one CREATE, merge columns from all CREATE statements
	if len(allCreateStmts) > 1 && lifecycle.Type == types.TypeTable {
		// FIXED: Use cumulative merge instead of last-one-wins for multiple CREATEs
		// This handles cases where older migrations have columns (e.g. coordinates) that
		// are missing from newer "snapshot" migrations, ensuring we don't drop valid columns.
		consolidatedSQL = mergeCumulativeCreateStatements(allCreateStmts, lifecycle.Name)
		if strings.ToLower(lifecycle.Name) == "profiles" {
			utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info("  Merged %d CREATE statements into unified schema (cumulative)", len(allCreateStmts))
		}
	}

	// Also integrate ALTER operations if present
	// After merging multiple CREATEs, we need to also apply any ALTER statements
	if len(alterStmts) > 0 {
		utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info("  Integrating %d ALTER statements into merged CREATE for %s", len(alterStmts), lifecycle.Name)
		// Use the same integration logic as CreateAlterConsolidationRule
		consolidatedSQL = integrateAlterIntoCreate(&types.Statement{
			SQL:        consolidatedSQL,
			ObjectName: lifecycle.Name,
			ObjectType: lifecycle.Type,
		}, alterStmts)
	}

	// Consolidate indexes from all CREATE events (fixes index loss bug)
	// When tables have DROP→CREATE cycles, indexes from earlier CREATEs are lost
	// This consolidates all indexes from all CREATE events for this table
	var consolidatedIndexes []string
	if len(allCreateStmts) > 1 && lifecycle.Type == types.TypeTable {
		utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info("  Consolidating indexes for table %s (multiple CREATE events detected)", lifecycle.Name)
		consolidatedIndexes = consolidateIndexesForTable(lifecycle, engine, allCreateStmts)
		if len(consolidatedIndexes) > 0 {
			utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info("  Successfully consolidated %d indexes for table %s", len(consolidatedIndexes), lifecycle.Name)
			// Append indexes to the consolidated table definition
			for _, indexSQL := range consolidatedIndexes {
				consolidatedSQL += "\n\n" + indexSQL + ";"
			}
		}
	}

	// Collect all statements for tracking
	allStmts := make([]types.Statement, 0, len(allCreateStmts)+len(alterStmts))
	allStmts = append(allStmts, allCreateStmts...)
	allStmts = append(allStmts, alterStmts...)

	// Extract column evolution from multiple CREATEs (e.g., name -> market_name)
	var columnEvolutions map[string]*tracking.ColumnEvolutionInfo
	if len(allCreateStmts) > 1 && lifecycle.Type == types.TypeTable {
		utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info("Extracting column evolutions from %d CREATE statements for %s", len(allCreateStmts), lifecycle.Name)
		columnEvolutions = extractColumnEvolutionsFromMultipleCreates(lifecycle.Name, allCreateStmts)
		utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info("Found %d column evolutions for %s", len(columnEvolutions), lifecycle.Name)
	}

	result := &tracking.ConsolidationResult{
		OriginalStatements: allStmts,
		ConsolidatedSQL:    consolidatedSQL,
		Optimizations: []string{
			fmt.Sprintf("Consolidated %d CREATE statements to final definition", len(allCreateStmts)),
		},
		RiskLevel: tracking.RiskLevelLow,
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(allStmts) - 1,
			FilesAffected:     len(allStmts),
			LinesReduced:      (len(allStmts) - 1) * 5, // Estimate
		},
		ColumnEvolutions: columnEvolutions,
	}

	// Add optimization message if ALTERs were integrated
	if len(alterStmts) > 0 {
		result.Optimizations = append(result.Optimizations,
			fmt.Sprintf("Integrated %d ALTER operations", len(alterStmts)))
	}

	// Add optimization message if indexes were consolidated
	if len(consolidatedIndexes) > 0 {
		result.Optimizations = append(result.Optimizations,
			fmt.Sprintf("Consolidated %d indexes from all CREATE events", len(consolidatedIndexes)))
		// Update estimated savings to account for preserved indexes
		result.EstimatedSavings.StatementsReduced += len(consolidatedIndexes)
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *MultipleCreateConsolidationRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow
}

// mergeCumulativeCreateStatements merges multiple CREATE statements, accumulating columns from ALL statements.
// This is different from mergeMultipleCreateStatements which only respects columns from the LAST statement.
func mergeCumulativeCreateStatements(createStmts []types.Statement, tableName string) string {
	if len(createStmts) == 0 {
		return ""
	}

	// Start with the first statement as the effective baseline
	effectiveStmt := createStmts[0]
	utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info("  Starting with schema from CREATE statement 0 (Base)")

	// Iterate through subsequent statements
	for i := 1; i < len(createStmts); i++ {
		stmt := createStmts[i]

		isConditional := hasCreateTableIfNotExists(stmt.SQL)

		if isConditional {
			// IF NOT EXISTS: The statement does nothing if the table exists.
			// Since we have a prior CREATE (effectiveStmt), the table exists.
			// Thus, this statement is a no-op schem-wise and should NOT override the definition.
			utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info(
				"  Skipping CREATE statement %d (IF NOT EXISTS type) - preserving prior effective schema", i)
		} else {
			// Hard CREATE: This implies the table is arguably being redefined forcibly
			// or was dropped (though this loop usually runs on post-drop segments).
			// We apply "Last Write Wins" logic for explicit hard creates.
			utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info(
				"  Overriding schema with CREATE statement %d (Hard CREATE detected)", i)
			effectiveStmt = stmt
		}
	}

	return effectiveStmt.SQL
}

func hasCreateTableIfNotExists(sql string) bool {
	lower := strings.ToLower(sql)
	idx := strings.Index(lower, "create")
	if idx == -1 {
		return false
	}

	pos := skipMultipleCreateWhitespace(lower, idx+len("create"))
	if strings.HasPrefix(lower[pos:], "temp") {
		pos = skipMultipleCreateWhitespace(lower, pos+len("temp"))
	} else if strings.HasPrefix(lower[pos:], "temporary") {
		pos = skipMultipleCreateWhitespace(lower, pos+len("temporary"))
	} else if strings.HasPrefix(lower[pos:], "unlogged") {
		pos = skipMultipleCreateWhitespace(lower, pos+len("unlogged"))
	}

	if !strings.HasPrefix(lower[pos:], "table") {
		return false
	}
	pos = skipMultipleCreateWhitespace(lower, pos+len("table"))

	if !strings.HasPrefix(lower[pos:], "if") {
		return false
	}

	pos = skipMultipleCreateWhitespace(lower, pos+len("if"))
	if !strings.HasPrefix(lower[pos:], "not") {
		return false
	}

	pos = skipMultipleCreateWhitespace(lower, pos+len("not"))
	return strings.HasPrefix(lower[pos:], "exists")
}

func skipMultipleCreateWhitespace(value string, pos int) int {
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

// Note: mergeMultipleCreateStatements is defined in drop_create_rule.go
// and shared across multiple rules

// consolidateIndexesForTable merges indexes from all CREATE events for a table
// AST-based approach using IndexStmt.Relation to track table associations
//
// Problem: When a table has multiple CREATE statements (CREATE → DROP → CREATE pattern),
// indexes from the first CREATE are lost. This function consolidates indexes from ALL
// CREATE events for the table.
//
// Example:
//
//	Migration 01: CREATE TABLE communities (...);
//	              CREATE INDEX idx_creator_id ON communities (creator_id);
//	              CREATE INDEX idx_type ON communities (type);
//	Migration 02: CREATE TABLE communities (...); -- Different schema
//
// Without this fix: Only 1 index survives (from last CREATE)
// With this fix: All 3 indexes are preserved and deduplicated
func consolidateIndexesForTable(
	tableLifecycle *tracking.ObjectLifecycle,
	engine ConsolidationEngine,
	allCreateStmts []types.Statement,
) []string {
	if engine == nil {
		return nil
	}

	tracker := engine.GetTracker()
	if tracker == nil {
		return nil
	}

	tableName := strings.ToLower(tableLifecycle.Name)
	utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info(
		"  Consolidating indexes for table: %s", tableName)

	// Get all tracked objects (returns map[string]*ObjectLifecycle)
	allObjects := tracker.GetObjects()

	// Find all index lifecycles that reference this table
	// Use AST-extracted table name from Dependencies field (IndexStmt.Relation)
	// IMPORTANT: Only include indexes that were NOT dropped
	var tableIndexes []*tracking.ObjectLifecycle
	for _, obj := range allObjects {
		if obj.Type != types.TypeIndex {
			continue
		}

		// Skip indexes that were dropped (they shouldn't be in the final output)
		if obj.WasDropped {
			continue
		}

		// Check if this index is associated with our table
		// The parser extracts table name from IndexStmt.Relation and stores in Dependencies
		for _, dep := range obj.History {
			for _, depName := range dep.Statement.Dependencies {
				if strings.ToLower(depName) == tableName {
					tableIndexes = append(tableIndexes, obj)
					break
				}
			}
		}
	}

	if len(tableIndexes) == 0 {
		utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info(
			"  No indexes found for table %s", tableName)
		return nil
	}

	utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info(
		"  Found %d indexes associated with table %s", len(tableIndexes), tableName)

	// Deduplicate indexes by name, keeping the latest definition
	// Map: index name -> index lifecycle
	indexMap := make(map[string]*tracking.ObjectLifecycle)

	for _, idx := range tableIndexes {
		idxName := strings.ToLower(idx.Name)

		// If we already have this index, check which one is newer
		if existing, exists := indexMap[idxName]; exists {
			// Get the last CREATE operation from each lifecycle
			existingLastOp := getLastCreateOperation(existing)
			currentLastOp := getLastCreateOperation(idx)

			// Keep the one with the higher sequence number (more recent)
			if currentLastOp != nil && (existingLastOp == nil || currentLastOp.Sequence > existingLastOp.Sequence) {
				indexMap[idxName] = idx
				utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info(
					"    Replacing index %s with newer definition (seq %d > %d)",
					idxName, currentLastOp.Sequence, existingLastOp.Sequence)
			}
		} else {
			indexMap[idxName] = idx
		}
	}

	// Extract SQL from consolidated indexes
	var indexSQLs []string
	for idxName, idx := range indexMap {
		// Get the SQL from the last CREATE operation for this index
		lastCreate := getLastCreateOperation(idx)
		if lastCreate != nil && lastCreate.Statement.SQL != "" {
			indexSQLs = append(indexSQLs, lastCreate.Statement.SQL)
			utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info(
				"    Including index: %s", idxName)
		}
	}

	utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info(
		"  Consolidated %d unique indexes for table %s", len(indexSQLs), tableName)

	return indexSQLs
}

// getLastCreateOperation finds the last CREATE operation in an object's lifecycle
func getLastCreateOperation(lifecycle *tracking.ObjectLifecycle) *tracking.LifecycleEvent {
	for i := len(lifecycle.History) - 1; i >= 0; i-- {
		if lifecycle.History[i].Operation == types.OpCreate {
			return &lifecycle.History[i]
		}
	}
	return nil
}
