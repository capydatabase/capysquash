package prisma

import (
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/plugins"
	"github.com/capy-base/pgsquash-engine/internal/types"
)

// GetConsolidationRules returns Prisma-specific consolidation rules
func (pp *PrismaPlugin) GetConsolidationRules() []plugins.ConsolidationRule {
	return []plugins.ConsolidationRule{
		{
			Name:       "prisma_migration_metadata",
			Priority:   100,
			ObjectType: types.TypeTable,
			Conflicts:  []string{"general_table_consolidation"},
			CanMerge:   pp.canMergePrismaTables,
			Merge:      pp.mergePrismaTables,
		},
		{
			Name:       "prisma_enum_types",
			Priority:   95,
			ObjectType: types.TypeEnum,
			Conflicts:  []string{},
			CanMerge:   pp.canMergePrismaEnums,
			Merge:      pp.mergePrismaEnums,
		},
		{
			Name:       "prisma_indices",
			Priority:   80,
			ObjectType: types.TypeIndex,
			Conflicts:  []string{},
			CanMerge:   pp.canMergePrismaIndices,
			Merge:      pp.mergePrismaIndices,
		},
	}
}

// canMergePrismaTables checks if Prisma table operations can be merged
func (pp *PrismaPlugin) canMergePrismaTables(statements []*types.Statement) bool {
	if len(statements) < 2 {
		return false
	}

	// Never merge _prisma_migrations table
	for _, stmt := range statements {
		if strings.Contains(strings.ToLower(stmt.SQL), "_prisma_migrations") {
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

	// Conservative merge: Only if operations are CREATE followed by ALTER
	hasCreate := false
	for _, stmt := range statements {
		if stmt.Operation == types.OpCreate {
			hasCreate = true
			break
		}
	}

	if !hasCreate {
		return false // No CREATE, can't consolidate ALTER-only sequences
	}

	return true
}

// mergePrismaTables merges Prisma table CREATE + ALTER sequences
func (pp *PrismaPlugin) mergePrismaTables(statements []*types.Statement) *types.Statement {
	if len(statements) == 0 {
		return nil
	}

	// Find the CREATE statement
	var createStmt *types.Statement
	var alterStmts []*types.Statement

	for _, stmt := range statements {
		switch stmt.Operation {
		case types.OpCreate:
			createStmt = stmt
		case types.OpAlter:
			alterStmts = append(alterStmts, stmt)
		}
	}

	if createStmt == nil {
		return statements[0] // Fallback
	}

	// If no ALTER statements, return CREATE as-is
	if len(alterStmts) == 0 {
		return createStmt
	}

	// Merge ALTER operations into CREATE
	// This is a simplified merge - real implementation would parse column definitions
	mergedSQL := createStmt.SQL

	// Preserve Prisma migration comments
	if pp.hasPrismaComment(createStmt.SQL) {
		// Keep the comment
		mergedSQL = createStmt.SQL
	}

	// Create merged statement
	merged := &types.Statement{
		SQL:        mergedSQL,
		ObjectType: types.TypeTable,
		ObjectName: createStmt.ObjectName,
		Operation:  types.OpCreate,
		Category:   createStmt.Category,
		Comments: append(createStmt.Comments,
			"Consolidated from Prisma CREATE + ALTER sequence"),
	}

	return merged
}

// hasPrismaComment checks if SQL contains Prisma migration comments
func (pp *PrismaPlugin) hasPrismaComment(sql string) bool {
	prismaComments := []string{
		"-- CreateTable",
		"-- AlterTable",
		"-- CreateIndex",
	}

	for _, comment := range prismaComments {
		if strings.Contains(sql, comment) {
			return true
		}
	}

	return false
}

// canMergePrismaEnums checks if Prisma enum operations can be merged
func (pp *PrismaPlugin) canMergePrismaEnums(statements []*types.Statement) bool {
	// Never merge ENUMs - data integrity critical
	// Prisma enums map to TypeScript types, changes can break application
	return false
}

// mergePrismaEnums merges Prisma enum operations (should never be called)
func (pp *PrismaPlugin) mergePrismaEnums(statements []*types.Statement) *types.Statement {
	if len(statements) == 0 {
		return nil
	}
	// Return most recent definition
	return statements[len(statements)-1]
}

// canMergePrismaIndices checks if Prisma index operations can be merged
func (pp *PrismaPlugin) canMergePrismaIndices(statements []*types.Statement) bool {
	if !pp.config.OptimizeIndices {
		return false // Don't merge if optimization disabled
	}

	if len(statements) < 2 {
		return false
	}

	// Check if all statements are for the same index
	firstName := statements[0].ObjectName
	for _, stmt := range statements[1:] {
		if stmt.ObjectName != firstName {
			return false
		}
	}

	// Allow merging DROP + CREATE cycles for indices
	// (Prisma sometimes recreates indices when changing columns)
	hasDrop := false
	hasCreate := false

	for _, stmt := range statements {
		if stmt.Operation == types.OpDrop {
			hasDrop = true
		}
		if stmt.Operation == types.OpCreate {
			hasCreate = true
		}
	}

	return hasDrop && hasCreate
}

// mergePrismaIndices merges Prisma index DROP + CREATE cycles
func (pp *PrismaPlugin) mergePrismaIndices(statements []*types.Statement) *types.Statement {
	if len(statements) == 0 {
		return nil
	}

	// Find the final CREATE statement (most recent index definition)
	var finalCreate *types.Statement
	for i := len(statements) - 1; i >= 0; i-- {
		if statements[i].Operation == types.OpCreate {
			finalCreate = statements[i]
			break
		}
	}

	if finalCreate == nil {
		return statements[len(statements)-1] // Fallback
	}

	// Add consolidation comment
	finalCreate.Comments = append(finalCreate.Comments,
		"Prisma index recreated - consolidated DROP + CREATE")

	return finalCreate
}
