package validation

import (
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/patterns"
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
//
//nolint:unused // Used by preprocessMigrationSQL (also unused but kept for future use)
func deduplicatePublicationStatements(sql string) string {
	lines := strings.Split(sql, "\n")
	publicationsSeen := make(map[string]bool) // key: "publication_name::schema.table_name"
	var result []string
	var skippedCount int

	// Enhanced regex to match various publication add table patterns (using precompiled pattern)
	// ALTER PUBLICATION pub ADD TABLE table;
	// ALTER PUBLICATION "pub" ADD TABLE "schema"."table";
	// ALTER PUBLICATION pub ADD TABLE schema.table;
	// Also captures optional schema with proper quote handling

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if match := patterns.PublicationPattern.FindStringSubmatch(trimmed); match != nil {
			pubName := strings.ToLower(match[1])
			schemaName := strings.ToLower(match[2])
			tableName := strings.ToLower(match[3])

			// Build fully qualified key
			var key string
			if schemaName != "" {
				key = fmt.Sprintf("%s::%s.%s", pubName, schemaName, tableName)
			} else {
				key = fmt.Sprintf("%s::%s", pubName, tableName)
			}

			if publicationsSeen[key] {
				// Skip duplicate - don't include this line
				skippedCount++
				continue
			}
			publicationsSeen[key] = true
		}
		result = append(result, line)
	}

	// Log deduplication summary if any duplicates were found
	if skippedCount > 0 {
		fmt.Printf("ℹ️  Skipped %d duplicate publication statement(s) during validation\n", skippedCount)
	}

	return strings.Join(result, "\n")
}

// preprocessMigrationSQL preprocesses migration SQL to fix common issues
// before applying to the database during validation
//
//nolint:unused // Reserved for future SQL preprocessing feature
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
