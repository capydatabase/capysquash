package consolidation

import (
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/patterns"
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// PublicationDeduplicationRule removes duplicate ALTER PUBLICATION ADD TABLE statements
// This prevents "relation X is already member of publication Y" errors during deployment
type PublicationDeduplicationRule struct{}

// CanApply checks if this rule can be applied to the given lifecycle
func (r *PublicationDeduplicationRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	// This rule applies to publications
	if lifecycle.Type != types.TypePublication {
		return false
	}

	// Check if there are duplicate ALTER PUBLICATION ADD TABLE operations
	addTableOps := make(map[string]int) // key: "publication_name::schema.table_name"

	for _, event := range lifecycle.History {
		sql := event.Statement.SQL
		if !strings.Contains(strings.ToUpper(sql), "ALTER PUBLICATION") {
			continue
		}
		if !strings.Contains(strings.ToUpper(sql), "ADD TABLE") {
			continue
		}

		// Extract publication name and table name
		if match := patterns.PublicationPattern.FindStringSubmatch(sql); match != nil {
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

			addTableOps[key]++
		}
	}

	// Rule applies if there are any duplicates
	for _, count := range addTableOps {
		if count > 1 {
			return true
		}
	}

	return false
}

// Apply applies the rule to deduplicate publication member additions
func (r *PublicationDeduplicationRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]interface{}{"rule": "PublicationDedupRule"})
	}

	// Track which publication + table combinations we've seen
	seen := make(map[string]bool) // key: "publication_name::schema.table_name"
	var newHistory []tracking.LifecycleEvent
	duplicateCount := 0

	for _, event := range lifecycle.History {
		sql := event.Statement.SQL

		// Only process ALTER PUBLICATION ADD TABLE statements
		upperSQL := strings.ToUpper(sql)
		if !strings.Contains(upperSQL, "ALTER PUBLICATION") || !strings.Contains(upperSQL, "ADD TABLE") {
			newHistory = append(newHistory, event)
			continue
		}

		// Extract publication name and table name
		if match := patterns.PublicationPattern.FindStringSubmatch(sql); match != nil {
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

			// If we've seen this combination before, skip it (duplicate)
			if seen[key] {
				duplicateCount++
				continue
			}

			// Mark as seen and keep this operation
			seen[key] = true
			newHistory = append(newHistory, event)
		} else {
			// Couldn't parse - keep it to be safe
			newHistory = append(newHistory, event)
		}
	}

	if duplicateCount == 0 {
		// No duplicates found - return nil to indicate no consolidation applied
		return nil, nil
	}

	// Build consolidated SQL from deduplicated history
	var sqlParts []string
	var originalStatements []types.Statement
	for _, event := range newHistory {
		sqlParts = append(sqlParts, event.Statement.SQL)
		originalStatements = append(originalStatements, event.Statement)
	}

	// Update lifecycle with deduplicated history
	lifecycle.History = newHistory

	return &tracking.ConsolidationResult{
		ConsolidatedSQL:    strings.Join(sqlParts, ";\n"),
		OriginalStatements: originalStatements,
		Optimizations: []string{
			fmt.Sprintf("Deduplicated %d publication member additions", duplicateCount),
		},
		Warnings:  []string{},
		RiskLevel: tracking.RiskLevelLow,
	}, nil
}

// Risk returns the risk level of this consolidation rule
func (r *PublicationDeduplicationRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow // Deduplication is very safe
}
