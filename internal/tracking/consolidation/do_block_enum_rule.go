package consolidation

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// DOBlockEnumTypeRule consolidates DO blocks that create ENUM types
type DOBlockEnumTypeRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *DOBlockEnumTypeRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	// Apply to DO blocks or ENUM types created inside DO blocks
	if lifecycle.Type != types.TypeDoBlock && lifecycle.Type != types.TypeEnum {
		return false
	}

	// Check if we have at least one CREATE TYPE operation
	hasCreateType := false
	for _, event := range lifecycle.History {
		sql := strings.ToUpper(event.Statement.SQL)
		if strings.Contains(sql, "DO") && strings.Contains(sql, "CREATE TYPE") && strings.Contains(sql, "AS ENUM") {
			hasCreateType = true
			break
		}
	}

	return hasCreateType
}

// Apply applies the consolidation rule to the given lifecycle
func (r *DOBlockEnumTypeRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]interface{}{"rule": "DOBlockEnumTypeRule"})
	}

	// Extract CREATE TYPE statement from DO block
	var doBlockStmts []types.Statement
	var extractedType string
	var extractedSQL string

	for _, event := range lifecycle.History {
		sql := event.Statement.SQL
		sqlUpper := strings.ToUpper(sql)

		if strings.Contains(sqlUpper, "DO") && strings.Contains(sqlUpper, "CREATE TYPE") {
			doBlockStmts = append(doBlockStmts, event.Statement)

			// Extract the CREATE TYPE statement from within the DO block
			if extractedSQL == "" {
				createTypePattern := regexp.MustCompile(`(?is)CREATE\s+TYPE\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+AS\s+ENUM\s*\((.*?)\)`)
				if matches := createTypePattern.FindStringSubmatch(sql); len(matches) > 2 {
					extractedType = matches[1]
					enumValues := matches[2]

					// Clean up enum values (remove quotes, extra spaces)
					enumValues = strings.TrimSpace(enumValues)

					// Build clean CREATE TYPE statement
					extractedSQL = fmt.Sprintf("DO $$ BEGIN\n    CREATE TYPE %s AS ENUM (%s);\nEXCEPTION\n    WHEN duplicate_object THEN NULL;\nEND $$;", extractedType, enumValues)
				}
			}
		}
	}

	// If we couldn't extract a valid CREATE TYPE, preserve the DO block as-is
	if extractedSQL == "" && len(doBlockStmts) > 0 {
		extractedSQL = doBlockStmts[0].SQL
	}

	result := &tracking.ConsolidationResult{
		OriginalStatements: doBlockStmts,
		ConsolidatedSQL:    extractedSQL,
		Optimizations: []string{
			fmt.Sprintf("Consolidated %d DO block(s) with CREATE TYPE for %s", len(doBlockStmts), extractedType),
		},
		RiskLevel: tracking.RiskLevelLow,
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(doBlockStmts) - 1,
			FilesAffected:     len(doBlockStmts),
			LinesReduced:      (len(doBlockStmts) - 1) * 6, // Average DO block is ~6 lines
		},
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *DOBlockEnumTypeRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow
}
