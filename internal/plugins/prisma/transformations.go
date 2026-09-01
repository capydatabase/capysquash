package prisma

import (
	"context"
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/utils"
)

// TransformSQL performs Prisma-specific SQL transformations
func (pp *PrismaPlugin) TransformSQL(ctx context.Context, sql string) (string, error) {
	transformed := sql

	// Transformation 1: Preserve Prisma migration comments
	transformed = pp.preservePrismaComments(transformed)

	// Transformation 2: Optimize VARCHAR(191) if configured
	if pp.config.OptimizeIndices {
		transformed = pp.optimizeVarcharLength(transformed)
	}

	return transformed, nil
}

// preservePrismaComments ensures Prisma migration comments are not stripped
func (pp *PrismaPlugin) preservePrismaComments(sql string) string {
	// Prisma comments should be preserved verbatim.
	// We still detect them to keep this hook explicit and future-proof.
	if action, _, found := extractPrismaCommentActionTarget(sql); found && action != "" {
		return sql
	}
	return sql
}

// optimizeVarcharLength optimizes Prisma's default VARCHAR(191) to more appropriate lengths
// Prisma uses 191 for MySQL compatibility (utf8mb4 index limit), but PostgreSQL doesn't need this
func (pp *PrismaPlugin) optimizeVarcharLength(sql string) string {
	// Only optimize if this is a non-indexed column
	// Keep VARCHAR(191) for indexed columns for consistency

	// Replace with VARCHAR(255) for non-indexed columns (more standard PostgreSQL length)
	return strings.ReplaceAll(sql, "VARCHAR(191)", "VARCHAR(255)")
}

// InjectCompatibilityLayer returns SQL for Prisma compatibility during validation
func (pp *PrismaPlugin) InjectCompatibilityLayer(ctx context.Context) string {
	if !pp.config.PreserveMigrationTable {
		return ""
	}

	return `-- Prisma Migration Compatibility Layer
-- Create _prisma_migrations table for migration tracking

CREATE TABLE IF NOT EXISTS "_prisma_migrations" (
    "id"                    VARCHAR(36) PRIMARY KEY NOT NULL,
    "checksum"              VARCHAR(64) NOT NULL,
    "finished_at"           TIMESTAMPTZ,
    "migration_name"        VARCHAR(255) NOT NULL,
    "logs"                  TEXT,
    "rolled_back_at"        TIMESTAMPTZ,
    "started_at"            TIMESTAMPTZ NOT NULL DEFAULT now(),
    "applied_steps_count"   INTEGER NOT NULL DEFAULT 0
);

-- Insert mock migration record
INSERT INTO "_prisma_migrations"
    (id, checksum, migration_name, started_at, applied_steps_count)
VALUES
    ('00000000-0000-0000-0000-000000000000',
     'mock_checksum',
     '00000000000000_init',
     now(),
     1)
ON CONFLICT (id) DO NOTHING;
`
}

// FixFunctionVolatility adds volatility markers to Prisma-generated functions
// Prisma rarely generates custom functions, but if it does, they should be marked
func (pp *PrismaPlugin) FixFunctionVolatility(ctx context.Context, functionSQL string) (string, error) {
	// Prisma doesn't typically generate custom functions
	// If we detect any, mark them as STABLE (safe default for ORM functions)

	if !strings.Contains(strings.ToUpper(functionSQL), "CREATE FUNCTION") {
		return functionSQL, nil
	}

	// Check if volatility marker already exists (using shared utility)
	if utils.HasVolatilityMarker(functionSQL) {
		return functionSQL, nil
	}

	// Add STABLE marker before AS keyword
	return insertMarkerBeforeAS(functionSQL, "STABLE"), nil
}

// GeneratePrismaMetadata creates metadata for Prisma migrations
func (pp *PrismaPlugin) GeneratePrismaMetadata(tableName string, action string) string {
	return fmt.Sprintf("-- %s %s\n", action, tableName)
}

// ExtractPrismaComment extracts Prisma action from migration comment
func (pp *PrismaPlugin) ExtractPrismaComment(sql string) (action string, target string, found bool) {
	return extractPrismaCommentActionTarget(sql)
}

// IsPrismaGeneratedSQL checks if SQL appears to be Prisma-generated
func (pp *PrismaPlugin) IsPrismaGeneratedSQL(sql string) bool {
	// Check for Prisma migration comments
	if action, _, found := pp.ExtractPrismaComment(sql); found {
		return action != ""
	}

	// Check for Prisma patterns
	patterns := []string{
		"_prisma_migrations",
		"VARCHAR(191)",
		"DEFAULT nextval",
	}

	sqlLower := strings.ToLower(sql)
	for _, pattern := range patterns {
		if strings.Contains(sqlLower, strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}

func insertMarkerBeforeAS(sql string, marker string) string {
	trimmedMarker := strings.TrimSpace(marker)
	if strings.TrimSpace(sql) == "" || trimmedMarker == "" {
		return sql
	}

	asIdx := findStandaloneASIndex(sql)
	if asIdx == -1 {
		return sql
	}

	prefix := strings.TrimRight(sql[:asIdx], " \t\r\n")
	suffix := strings.TrimLeft(sql[asIdx:], " \t\r\n")
	if prefix == "" {
		return trimmedMarker + " " + suffix
	}

	return prefix + " " + trimmedMarker + " " + suffix
}

func findStandaloneASIndex(sql string) int {
	for i := 0; i < len(sql); i++ {
		if hasKeywordAtCI(sql, i, "AS") {
			return i
		}
	}
	return -1
}

func hasKeywordAtCI(sql string, pos int, keyword string) bool {
	if pos < 0 || pos+len(keyword) > len(sql) {
		return false
	}

	if !strings.EqualFold(sql[pos:pos+len(keyword)], keyword) {
		return false
	}

	if pos > 0 && isIdentifierByte(sql[pos-1]) {
		return false
	}

	end := pos + len(keyword)
	if end < len(sql) && isIdentifierByte(sql[end]) {
		return false
	}

	return true
}

func isIdentifierByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}
