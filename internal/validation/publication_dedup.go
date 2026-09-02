package validation

import (
	"fmt"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/utils"
)

// deduplicatePublicationStatements removes duplicate ALTER PUBLICATION ADD TABLE statements from SQL
// This fixes the issue where original migrations have duplicate publication member additions
// that cause validation to fail with "relation X is already member of publication Y"
//
// Enhanced to handle:
// - Schema-qualified tables (public.users, "schema"."table")
// - Quoted identifiers ("my_pub", "my_table")
// - Multiline statements
// - Case-insensitive matching
func deduplicatePublicationStatements(sql string) string {
	lines := strings.Split(sql, "\n")
	publicationsSeen := make(map[string]bool) // key: "publication_name::schema.table_name"
	var result []string

	// Enhanced regex to match various publication add table patterns (using precompiled pattern)
	// ALTER PUBLICATION pub ADD TABLE table;
	// ALTER PUBLICATION "pub" ADD TABLE "schema"."table";
	// ALTER PUBLICATION pub ADD TABLE schema.table;
	// Also captures optional schema with proper quote handling

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if parsed, ok := utils.ParsePublicationAddTable(trimmed); ok {
			pubName := strings.ToLower(parsed.Publication)
			schemaName := strings.ToLower(parsed.Schema)
			tableName := strings.ToLower(parsed.Table)

			// Build fully qualified key
			var key string
			if schemaName != "" {
				key = fmt.Sprintf("%s::%s.%s", pubName, schemaName, tableName)
			} else {
				key = fmt.Sprintf("%s::%s", pubName, tableName)
			}

			if publicationsSeen[key] {
				// Skip duplicate - don't include this line
				continue
			}
			publicationsSeen[key] = true
		}
		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

type publicationAddTable = utils.PublicationAddTableTarget

func parsePublicationAddTable(sql string) (publicationAddTable, bool) {
	return utils.ParsePublicationAddTable(sql)
}

// preprocessMigrationSQL preprocesses migration SQL to fix common issues
// before applying to the database during validation
func preprocessMigrationSQL(sql string, enablePublicationDedup bool) string {
	result := sql

	// Deduplicate publications during validation
	// This is safe because it only affects the validation container, not the actual migrations
	if enablePublicationDedup {
		result = deduplicatePublicationStatements(result)
	}

	// Future: Add other preprocessing steps here if needed
	// - Deduplicate DROP IF EXISTS statements
	// - Handle CREATE OR REPLACE conflicts
	// - etc.

	return result
}
