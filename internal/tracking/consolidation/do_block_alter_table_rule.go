package consolidation

import (
	"fmt"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/errors"
	"github.com/capy-base/pgsquash-engine/internal/parser"
	"github.com/capy-base/pgsquash-engine/internal/tracking"
	"github.com/capy-base/pgsquash-engine/internal/types"
)

// DOBlockAlterTableRule extracts ALTER TABLE statements from DO blocks
// This allows ALTER statements wrapped in IF NOT EXISTS checks to be consolidated
// with their corresponding CREATE TABLE statements
type DOBlockAlterTableRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *DOBlockAlterTableRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	// Only apply to DO blocks
	if lifecycle.Type != types.TypeDoBlock {
		return false
	}

	// Check if we have ALTER TABLE statements inside DO blocks
	hasAlterTable := false
	for _, event := range lifecycle.History {
		sql := strings.ToUpper(event.Statement.SQL)
		if strings.Contains(sql, "DO") && strings.Contains(sql, "ALTER TABLE") {
			hasAlterTable = true
			break
		}
	}

	return hasAlterTable
}

// Apply applies the consolidation rule to the given lifecycle
func (r *DOBlockAlterTableRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]any{"rule": "DOBlockAlterTableRule"})
	}

	// Extract ALTER TABLE statements from DO blocks
	var extractedStatements []string
	var doBlockStmts []types.Statement

	for _, event := range lifecycle.History {
		sql := event.Statement.SQL
		sqlUpper := strings.ToUpper(sql)

		if strings.Contains(sqlUpper, "DO") && strings.Contains(sqlUpper, "ALTER TABLE") {
			doBlockStmts = append(doBlockStmts, event.Statement)

			// Extract all ALTER TABLE statements from within the DO block
			alterStatements := extractAlterStatementsFromDoBlock(sql)
			extractedStatements = append(extractedStatements, alterStatements...)
		}
	}

	// If no ALTER statements were extracted, return empty result
	if len(extractedStatements) == 0 {
		return &tracking.ConsolidationResult{
			OriginalStatements: doBlockStmts,
			ConsolidatedSQL:    "",
			Optimizations: []string{
				"DO block did not contain extractable ALTER statements",
			},
			RiskLevel: tracking.RiskLevelLow,
			EstimatedSavings: tracking.SquashSavings{
				StatementsReduced: len(doBlockStmts),
				FilesAffected:     len(doBlockStmts),
				LinesReduced:      len(doBlockStmts) * 10,
			},
		}, nil
	}

	// Join all extracted ALTER statements
	consolidatedSQL := strings.Join(extractedStatements, "\n")

	result := &tracking.ConsolidationResult{
		OriginalStatements: doBlockStmts,
		ConsolidatedSQL:    consolidatedSQL,
		Optimizations: []string{
			fmt.Sprintf("Extracted %d ALTER TABLE statement(s) from %d DO block(s)", len(extractedStatements), len(doBlockStmts)),
		},
		RiskLevel: tracking.RiskLevelLow,
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(doBlockStmts) - len(extractedStatements),
			FilesAffected:     len(doBlockStmts),
			LinesReduced:      len(doBlockStmts) * 8,
		},
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *DOBlockAlterTableRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow
}

// extractAlterStatementsFromDoBlock extracts ALTER TABLE statements from a DO block
func extractAlterStatementsFromDoBlock(doBlockSQL string) []string {
	staticDDL := parser.ExtractStaticDDLFromDoBlock(doBlockSQL)
	statements := make([]string, 0, len(staticDDL))
	for _, sql := range staticDDL {
		migration, err := parser.ParseMigration(sql, "__do_block_alter_extract__.sql")
		if err != nil || migration == nil || len(migration.Statements) != 1 {
			continue
		}

		stmt := migration.Statements[0]
		if stmt.Operation != types.OpAlter || stmt.ObjectType != types.TypeTable {
			continue
		}

		sql = strings.TrimSpace(stmt.SQL)
		if sql == "" {
			continue
		}

		if !strings.HasSuffix(sql, ";") {
			sql += ";"
		}

		statements = append(statements, sql)
	}

	return statements
}
