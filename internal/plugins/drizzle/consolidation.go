package drizzle

import (
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/plugins"
	"github.com/capy-base/pgsquash-engine/internal/types"
)

// GetConsolidationRules returns Drizzle-specific consolidation rules
func (dp *DrizzlePlugin) GetConsolidationRules() []plugins.ConsolidationRule {
	return []plugins.ConsolidationRule{
		{
			Name:       "drizzle_identity_tables",
			Priority:   100,
			ObjectType: types.TypeTable,
			Conflicts:  []string{"general_table_consolidation"},
			CanMerge:   dp.canMergeDrizzleTables,
			Merge:      dp.mergeDrizzleTables,
		},
		{
			Name:       "drizzle_sequences",
			Priority:   90,
			ObjectType: types.TypeSequence,
			Conflicts:  []string{},
			CanMerge:   dp.canMergeDrizzleSequences,
			Merge:      dp.mergeDrizzleSequences,
		},
	}
}

// canMergeDrizzleTables checks if Drizzle table operations can be merged
func (dp *DrizzlePlugin) canMergeDrizzleTables(statements []*types.Statement) bool {
	if len(statements) < 2 {
		return false
	}

	// Never merge __drizzle_migrations table
	for _, stmt := range statements {
		if strings.Contains(strings.ToLower(stmt.SQL), "__drizzle_migrations") {
			return false
		}
	}

	// Check if all statements target the same table
	firstTable := statements[0].ObjectName
	for _, stmt := range statements[1:] {
		if stmt.ObjectName != firstTable {
			return false
		}
	}

	// Conservative merge: Only merge CREATE + ALTER sequences
	hasCreate := false
	for _, stmt := range statements {
		if stmt.Operation == types.OpCreate {
			hasCreate = true
			break
		}
	}

	if !hasCreate {
		return false
	}

	// Don't merge if any statement has IDENTITY columns (preserve exact definition)
	if dp.config.PreferIdentityColumns {
		for _, stmt := range statements {
			if dp.hasIdentityColumn(stmt.SQL) {
				return false // IDENTITY columns are critical, don't merge
			}
		}
	}

	return true
}

// mergeDrizzleTables merges Drizzle table CREATE + ALTER sequences
func (dp *DrizzlePlugin) mergeDrizzleTables(statements []*types.Statement) *types.Statement {
	if len(statements) == 0 {
		return nil
	}

	// Find the CREATE statement
	var createStmt *types.Statement
	for _, stmt := range statements {
		if stmt.Operation == types.OpCreate {
			createStmt = stmt
			break
		}
	}

	if createStmt == nil {
		return statements[0] // Fallback
	}

	// Create merged statement with consolidation note
	merged := &types.Statement{
		SQL:        createStmt.SQL,
		ObjectType: types.TypeTable,
		ObjectName: createStmt.ObjectName,
		Operation:  types.OpCreate,
		Category:   createStmt.Category,
		Comments: append(createStmt.Comments,
			"Consolidated from Drizzle CREATE + ALTER sequence"),
	}

	return merged
}

// canMergeDrizzleSequences checks if Drizzle sequence operations can be merged
func (dp *DrizzlePlugin) canMergeDrizzleSequences(statements []*types.Statement) bool {
	if !dp.config.OptimizeSequences {
		return false
	}

	if len(statements) < 2 {
		return false
	}

	// Check if all statements are for the same sequence
	firstName := statements[0].ObjectName
	for _, stmt := range statements[1:] {
		if stmt.ObjectName != firstName {
			return false
		}
	}

	// Allow merging ALTER SEQUENCE operations
	// Drizzle sometimes generates multiple ALTER SEQUENCE for configuration
	allAlter := true
	for _, stmt := range statements {
		if stmt.Operation != types.OpAlter {
			allAlter = false
			break
		}
	}

	return allAlter
}

// mergeDrizzleSequences merges multiple ALTER SEQUENCE statements
func (dp *DrizzlePlugin) mergeDrizzleSequences(statements []*types.Statement) *types.Statement {
	if len(statements) == 0 {
		return nil
	}

	// Use the last ALTER statement (most recent configuration)
	finalStmt := statements[len(statements)-1]

	// Add consolidation comment
	finalStmt.Comments = append(finalStmt.Comments,
		"Drizzle sequence configuration - consolidated multiple ALTER statements")

	return finalStmt
}

// PreserveIdentityColumns checks if statement defines IDENTITY columns that should be preserved
func (dp *DrizzlePlugin) PreserveIdentityColumns(stmt *types.Statement) bool {
	if !dp.config.PreferIdentityColumns {
		return false
	}

	return dp.hasIdentityColumn(stmt.SQL)
}
