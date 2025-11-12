// Package prisma provides Prisma ORM integration for pgsquash.
// It handles Prisma-generated migrations, schema.prisma patterns, and migration metadata.
package prisma

import (
	"context"
	"database/sql"
	"regexp"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/plugins"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// PrismaPlugin implements Prisma ORM integration
type PrismaPlugin struct {
	*plugins.BasePlugin
	config PrismaConfig
}

// PrismaConfig holds Prisma-specific configuration
type PrismaConfig struct {
	Enabled              bool   `json:"enabled"`
	PreserveMigrationTable bool `json:"preserve_migration_table"` // Preserve _prisma_migrations
	ShadowDatabase       bool   `json:"shadow_database"`          // Handle shadow database patterns
	OptimizeIndices      bool   `json:"optimize_indices"`         // Optimize Prisma-generated indices
}

// NewPrismaPlugin creates a new Prisma plugin instance
func NewPrismaPlugin() *PrismaPlugin {
	return &PrismaPlugin{
		BasePlugin: plugins.NewBasePlugin("prisma", 75), // ORM priority
		config: PrismaConfig{
			Enabled:              true,
			PreserveMigrationTable: true,
			ShadowDatabase:       true,
			OptimizeIndices:      true,
		},
	}
}

// Detect checks if migrations contain Prisma patterns
// Detection patterns:
//   1. _prisma_migrations table (most reliable)
//   2. Prisma-style timestamp directory structure (migrations/20210313140442_init/)
//   3. Prisma schema comments (-- CreateTable, -- CreateIndex, etc.)
//   4. Prisma-specific naming patterns (autoincrement vs SERIAL preference)
func (pp *PrismaPlugin) Detect(migrations []*types.Migration) bool {
	for _, migration := range migrations {
		filename := strings.ToLower(migration.Filename)

		// Pattern 1: Migration filename contains Prisma timestamp pattern
		// Format: YYYYMMDDHHMMSS_migration_name
		timestampPattern := regexp.MustCompile(`\d{14}_[a-z_]+`)
		if timestampPattern.MatchString(filename) {
			return true
		}

		for _, stmt := range migration.Statements {
			sql := stmt.SQL
			sqlLower := strings.ToLower(sql)

			// Pattern 2: _prisma_migrations table
			if strings.Contains(sqlLower, "_prisma_migrations") {
				return true
			}

			// Pattern 3: Prisma migration comments
			prismaComments := []string{
				"-- CreateTable",
				"-- CreateIndex",
				"-- CreateEnum",
				"-- AlterTable",
				"-- AddForeignKey",
				"-- DropTable",
			}
			for _, comment := range prismaComments {
				if strings.Contains(sql, comment) {
					return true
				}
			}

			// Pattern 4: Prisma prefers @default(autoincrement()) over SERIAL
			// This generates: DEFAULT nextval() explicitly instead of SERIAL
			if strings.Contains(sqlLower, "default nextval") &&
			   strings.Contains(sqlLower, "_seq'::regclass)") {
				return true
			}

			// Pattern 5: Prisma @db.Text, @db.VarChar patterns
			// Often generates explicit type definitions
			if strings.Contains(sqlLower, "varchar(191)") { // Default Prisma String length
				return true
			}
		}
	}

	return false
}

// Initialize configures the Prisma plugin
func (pp *PrismaPlugin) Initialize(ctx context.Context, config interface{}) error {
	if config == nil {
		return nil
	}

	if prismaConfig, ok := config.(PrismaConfig); ok {
		pp.config = prismaConfig
	} else if configMap, ok := config.(map[string]interface{}); ok {
		if enabled, ok := configMap["enabled"].(bool); ok {
			pp.config.Enabled = enabled
		}
		if preserve, ok := configMap["preserve_migration_table"].(bool); ok {
			pp.config.PreserveMigrationTable = preserve
		}
		if shadow, ok := configMap["shadow_database"].(bool); ok {
			pp.config.ShadowDatabase = shadow
		}
		if optimize, ok := configMap["optimize_indices"].(bool); ok {
			pp.config.OptimizeIndices = optimize
		}
	}

	return nil
}

// EnrichStatement adds Prisma-specific metadata to parsed statements
func (pp *PrismaPlugin) EnrichStatement(ctx context.Context, stmt *types.Statement) error {
	sql := stmt.SQL
	sqlLower := strings.ToLower(sql)

	// Mark _prisma_migrations table as critical (never consolidate)
	if strings.Contains(sqlLower, "_prisma_migrations") {
		stmt.Category = types.CategoryCritical
		if stmt.Comments == nil {
			stmt.Comments = []string{}
		}
		stmt.Comments = append(stmt.Comments, "Prisma migration tracking table")
	}

	// Detect Prisma migration comments and extract metadata
	commentPattern := regexp.MustCompile(`--\s*(CreateTable|CreateIndex|CreateEnum|AlterTable|AddForeignKey|DropTable)\s+(.*)`)
	if matches := commentPattern.FindStringSubmatch(sql); len(matches) > 2 {
		if stmt.Comments == nil {
			stmt.Comments = []string{}
		}
		stmt.Comments = append(stmt.Comments, "Prisma: "+matches[1]+" "+matches[2])
	}

	// Mark shadow database operations
	if pp.config.ShadowDatabase && pp.isShadowDatabaseOperation(sql) {
		if stmt.Comments == nil {
			stmt.Comments = []string{}
		}
		stmt.Comments = append(stmt.Comments, "Prisma shadow database operation")
	}

	return nil
}

// isShadowDatabaseOperation checks if statement is related to shadow database
func (pp *PrismaPlugin) isShadowDatabaseOperation(sql string) bool {
	shadowPatterns := []string{
		"CREATE DATABASE",
		"DROP DATABASE",
		"TEMPLATE template0",
	}

	sqlUpper := strings.ToUpper(sql)
	for _, pattern := range shadowPatterns {
		if strings.Contains(sqlUpper, pattern) {
			return true
		}
	}

	return false
}

// DetectPatterns analyzes SQL for Prisma-specific patterns
func (pp *PrismaPlugin) DetectPatterns(sql string) []plugins.Pattern {
	var patterns []plugins.Pattern

	// Pattern 1: Prisma migration tracking table
	if strings.Contains(strings.ToLower(sql), "_prisma_migrations") {
		patterns = append(patterns, plugins.Pattern{
			Type:     plugins.PatternTypeSchema,
			Name:     "prisma_migration_table",
			Severity: plugins.SeverityCritical,
			Metadata: map[string]string{
				"table": "_prisma_migrations",
			},
		})
	}

	// Pattern 2: Prisma autoincrement pattern
	autoIncrementPattern := regexp.MustCompile(`DEFAULT nextval\('([^']+)_seq'::regclass\)`)
	if matches := autoIncrementPattern.FindStringSubmatch(sql); len(matches) > 1 {
		patterns = append(patterns, plugins.Pattern{
			Type:     plugins.PatternTypeORM,
			Name:     "prisma_autoincrement",
			Severity: plugins.SeverityMedium,
			Metadata: map[string]string{
				"sequence": matches[1] + "_seq",
			},
		})
	}

	// Pattern 3: Prisma migration comments
	commentPattern := regexp.MustCompile(`--\s*(CreateTable|CreateIndex|CreateEnum|AlterTable)`)
	if matches := commentPattern.FindStringSubmatch(sql); len(matches) > 1 {
		patterns = append(patterns, plugins.Pattern{
			Type:     plugins.PatternTypeORM,
			Name:     "prisma_migration_comment",
			Severity: plugins.SeverityLow,
			Metadata: map[string]string{
				"action": matches[1],
			},
		})
	}

	return patterns
}

// GetConflictingPlugins returns plugins that conflict with Prisma
func (pp *PrismaPlugin) GetConflictingPlugins() []string {
	// Prisma conflicts with other ORMs (can't use both in same project)
	return []string{"drizzle", "typeorm"}
}

// GetRequiredExtensions returns PostgreSQL extensions required by Prisma
func (pp *PrismaPlugin) GetRequiredExtensions() []string {
	// Prisma often uses these extensions
	return []string{
		"uuid-ossp", // For UUID default values
		"pgcrypto",  // For gen_random_uuid()
	}
}

// ShouldPreserve determines if a statement should never be consolidated
func (pp *PrismaPlugin) ShouldPreserve(stmt *types.Statement) bool {
	sqlLower := strings.ToLower(stmt.SQL)

	// Always preserve _prisma_migrations table
	if pp.config.PreserveMigrationTable && strings.Contains(sqlLower, "_prisma_migrations") {
		return true
	}

	// Preserve shadow database operations if configured
	if pp.config.ShadowDatabase && pp.isShadowDatabaseOperation(stmt.SQL) {
		return true
	}

	// Preserve statements with Prisma comments indicating critical operations
	for _, comment := range stmt.Comments {
		if strings.Contains(comment, "Prisma: CreateEnum") {
			return true // Preserve enum creations (critical for data integrity)
		}
	}

	return false
}

// ValidateSchema performs Prisma-specific schema validation
func (pp *PrismaPlugin) ValidateSchema(ctx context.Context, db *sql.DB) error {
	// Verify _prisma_migrations table exists if enabled
	if pp.config.PreserveMigrationTable {
		var tableExists bool
		err := db.QueryRowContext(ctx,
			"SELECT EXISTS(SELECT 1 FROM information_schema.tables WHERE table_name = '_prisma_migrations')").
			Scan(&tableExists)
		if err != nil {
			return errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation,
				"failed to check _prisma_migrations table",
				map[string]interface{}{"table": "_prisma_migrations"})
		}

		if !tableExists {
			return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation,
				"_prisma_migrations table does not exist (expected for Prisma project)",
				map[string]interface{}{"table": "_prisma_migrations"})
		}

		// Verify table structure
		var columnCount int
		err = db.QueryRowContext(ctx,
			"SELECT COUNT(*) FROM information_schema.columns WHERE table_name = '_prisma_migrations'").
			Scan(&columnCount)
		if err != nil {
			return errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation,
				"failed to validate _prisma_migrations structure",
				map[string]interface{}{"table": "_prisma_migrations"})
		}

		// Expected columns: id, checksum, finished_at, migration_name, logs, rolled_back_at, started_at, applied_steps_count
		if columnCount < 7 {
			return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation,
				"_prisma_migrations table has incomplete structure",
				map[string]interface{}{
					"table":            "_prisma_migrations",
					"found_columns":    columnCount,
					"expected_columns": 7,
				})
		}
	}

	return nil
}
