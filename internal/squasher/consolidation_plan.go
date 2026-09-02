package squasher

import (
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/parser"
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/tracking/consolidation"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// ConsolidationPlan represents a detailed plan of consolidations to be applied
type ConsolidationPlan struct {
	TotalMigrations    int
	TotalOperations    int
	Consolidations     []PlannedConsolidation
	CannotConsolidate  []ConsolidationConflict
	EstimatedReduction ConsolidationStats
}

// PlannedConsolidation represents a single consolidation action
type PlannedConsolidation struct {
	ObjectName   string
	ObjectType   types.ObjectType
	ObjectSchema string
	Operations   []string // SQL snippets
	ResultSQL    string
	Rule         string
	Reason       string
	SafetyLevel  string
	RiskLevel    tracking.RiskLevel
}

// ConsolidationConflict represents operations that cannot be consolidated
type ConsolidationConflict struct {
	ObjectName string
	ObjectType types.ObjectType
	Operations []string
	Reason     string
}

// ConsolidationStats provides summary statistics
type ConsolidationStats struct {
	OriginalFiles      int
	OriginalOperations int
	FinalFiles         int
	FinalOperations    int
	FilesReduced       int
	OperationsReduced  int
	PercentageReduced  float64
}

// GenerateConsolidationPlan analyzes migrations and creates a detailed consolidation plan
func (e *Engine) GenerateConsolidationPlan(migrationMap map[int]string) (*ConsolidationPlan, error) {
	// Parse all migrations first using the engine's existing tracker
	totalOps := 0
	for i := 0; i < len(migrationMap); i++ {
		migration, err := parser.ParseMigration(migrationMap[i], fmt.Sprintf("migration_%d.sql", i))
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeSyntaxError,
				fmt.Sprintf("parse migration %d: %v", i, err),
				errors.SeverityError,
				errors.CategoryParsing,
			).WithFile(fmt.Sprintf("migration_%d.sql", i)).WithInnerError(err)
		}

		e.tracker.ProcessMigration(migration, i)
		totalOps += len(migration.Statements)
	}

	plan := &ConsolidationPlan{
		TotalMigrations:   len(migrationMap),
		TotalOperations:   totalOps,
		Consolidations:    make([]PlannedConsolidation, 0),
		CannotConsolidate: make([]ConsolidationConflict, 0),
	}

	// Analyze each object lifecycle for consolidation opportunities
	for _, lifecycle := range e.tracker.GetObjects() {
		if len(lifecycle.History) <= 1 {
			continue // Nothing to consolidate
		}

		// Check which rules would apply
		applicableRules := e.ruleEngine.GetApplicableRules(lifecycle)

		if len(applicableRules) > 0 {
			// There are consolidation opportunities
			rule := applicableRules[0] // Use highest priority rule

			planned := PlannedConsolidation{
				ObjectName:   lifecycle.Name,
				ObjectType:   lifecycle.Type,
				ObjectSchema: lifecycle.Schema,
				Operations:   make([]string, 0, len(lifecycle.History)),
				Rule:         getRuleName(rule),
				Reason:       getRuleReason(rule, lifecycle),
				SafetyLevel:  string(e.config.SafetyLevel),
				RiskLevel:    rule.Risk(),
			}

			// Extract operation SQL snippets
			for _, event := range lifecycle.History {
				// Truncate long SQL for display
				sql := event.Statement.SQL
				if len(sql) > 100 {
					sql = sql[:97] + "..."
				}
				planned.Operations = append(planned.Operations, sql)
			}

			// Try to apply the rule to get result SQL
			result, err := rule.Apply(lifecycle, e)
			if err == nil && result != nil {
				planned.ResultSQL = result.ConsolidatedSQL
				if len(planned.ResultSQL) > 200 {
					planned.ResultSQL = planned.ResultSQL[:197] + "..."
				}
			}

			plan.Consolidations = append(plan.Consolidations, planned)
		} else if len(lifecycle.History) > 1 {
			// Multiple operations but no rules apply - conflict
			conflict := ConsolidationConflict{
				ObjectName: lifecycle.Name,
				ObjectType: lifecycle.Type,
				Operations: make([]string, 0, len(lifecycle.History)),
				Reason:     "Complex dependency or unsafe consolidation pattern",
			}

			for _, event := range lifecycle.History {
				sql := event.Statement.SQL
				if len(sql) > 80 {
					sql = sql[:77] + "..."
				}
				conflict.Operations = append(conflict.Operations, sql)
			}

			plan.CannotConsolidate = append(plan.CannotConsolidate, conflict)
		}
	}

	// Calculate estimated reduction
	estimatedOps := plan.TotalOperations
	for _, consolidation := range plan.Consolidations {
		// Each consolidation removes (operations - 1) operations
		estimatedOps -= (len(consolidation.Operations) - 1)
	}

	plan.EstimatedReduction = ConsolidationStats{
		OriginalFiles:      plan.TotalMigrations,
		OriginalOperations: plan.TotalOperations,
		FinalFiles:         1, // Squashed into single file
		FinalOperations:    estimatedOps,
		FilesReduced:       plan.TotalMigrations - 1,
		OperationsReduced:  plan.TotalOperations - estimatedOps,
	}

	if plan.TotalOperations > 0 {
		plan.EstimatedReduction.PercentageReduced = float64(plan.TotalOperations-estimatedOps) / float64(plan.TotalOperations) * 100
	}

	return plan, nil
}

// FormatPlan generates a human-readable consolidation plan
func (p *ConsolidationPlan) FormatPlan() string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString("═══════════════════════════════════════════════════════════════════\n")
	sb.WriteString("                    CONSOLIDATION PLAN                             \n")
	sb.WriteString("═══════════════════════════════════════════════════════════════════\n\n")

	// Summary
	sb.WriteString("📊 SUMMARY\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString(fmt.Sprintf("Input:  %d migration files, %d operations\n", p.TotalMigrations, p.TotalOperations))
	sb.WriteString(fmt.Sprintf("Output: %d migration file, %d operations\n", p.EstimatedReduction.FinalFiles, p.EstimatedReduction.FinalOperations))
	sb.WriteString(fmt.Sprintf("Reduction: %d fewer files (%.1f%%), %d fewer operations (%.1f%%)\n\n",
		p.EstimatedReduction.FilesReduced,
		float64(p.EstimatedReduction.FilesReduced)/float64(p.TotalMigrations)*100,
		p.EstimatedReduction.OperationsReduced,
		p.EstimatedReduction.PercentageReduced))

	// Consolidations
	if len(p.Consolidations) > 0 {
		sb.WriteString("\n☑ PLANNED CONSOLIDATIONS\n")
		sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

		for i, consolidation := range p.Consolidations {
			sb.WriteString(fmt.Sprintf("[%d] %s.%s (%s)\n", i+1, consolidation.ObjectSchema, consolidation.ObjectName, consolidation.ObjectType))
			sb.WriteString(fmt.Sprintf("    Rule: %s\n", consolidation.Rule))
			sb.WriteString(fmt.Sprintf("    Reason: %s\n", consolidation.Reason))
			sb.WriteString(fmt.Sprintf("    Safety: %s | Risk: %s\n", consolidation.SafetyLevel, consolidation.RiskLevel))
			sb.WriteString(fmt.Sprintf("    Operations: %d → 1 (-%d operations)\n\n", len(consolidation.Operations), len(consolidation.Operations)-1))

			// Show before/after
			sb.WriteString("    Before:\n")
			for j, op := range consolidation.Operations {
				sb.WriteString(fmt.Sprintf("      %d. %s\n", j+1, op))
			}
			if consolidation.ResultSQL != "" {
				sb.WriteString(fmt.Sprintf("\n    After:\n      → %s\n\n", consolidation.ResultSQL))
			}
			sb.WriteString("    ───────────────────────────────────────────────────────────────\n\n")
		}
	}

	// Conflicts
	if len(p.CannotConsolidate) > 0 {
		sb.WriteString("\n✗ CANNOT CONSOLIDATE\n")
		sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

		for i, conflict := range p.CannotConsolidate {
			sb.WriteString(fmt.Sprintf("[%d] %s (%s)\n", i+1, conflict.ObjectName, conflict.ObjectType))
			sb.WriteString(fmt.Sprintf("    Reason: %s\n", conflict.Reason))
			sb.WriteString("    Operations:\n")
			for j, op := range conflict.Operations {
				sb.WriteString(fmt.Sprintf("      %d. %s\n", j+1, op))
			}
			sb.WriteString("\n")
		}
	}

	// Estimated time savings
	sb.WriteString("\n⏱️  ESTIMATED IMPACT\n")
	sb.WriteString("━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
	sb.WriteString(fmt.Sprintf("Database Operations Eliminated: %d\n", p.EstimatedReduction.OperationsReduced))
	sb.WriteString(fmt.Sprintf("Migration Complexity Reduced: %.1f%%\n", p.EstimatedReduction.PercentageReduced))
	sb.WriteString(fmt.Sprintf("Estimated Execution Time Saved: ~%d ms per deployment\n", p.EstimatedReduction.OperationsReduced*10))
	sb.WriteString("\n")

	sb.WriteString("═══════════════════════════════════════════════════════════════════\n")
	sb.WriteString("Run without --explain to apply these consolidations\n")
	sb.WriteString("═══════════════════════════════════════════════════════════════════\n")

	return sb.String()
}

// Helper functions to get rule information
func getRuleName(rule consolidation.ConsolidationRule) string {
	// Extract rule name from type
	typeName := fmt.Sprintf("%T", rule)
	parts := strings.Split(typeName, ".")
	if len(parts) > 0 {
		name := parts[len(parts)-1]
		// Remove "Rule" suffix if present
		return strings.TrimSuffix(name, "Rule")
	}
	return "UnknownRule"
}

func getRuleReason(rule consolidation.ConsolidationRule, lifecycle *tracking.ObjectLifecycle) string {
	ruleName := getRuleName(rule)

	switch ruleName {
	case "CreateAlterConsolidation":
		return "Sequential table modification - can combine CREATE and ALTER into single CREATE"
	case "DropCreateCycle":
		return "Table dropped then recreated without changes - redundant operations"
	case "FunctionDeduplication":
		return "Multiple versions of same function - keep latest only"
	case "EnumDeduplication":
		return "Duplicate ENUM definitions detected"
	case "PublicationDeduplication":
		return "Duplicate publication member additions"
	case "RLSConsolidation":
		return "Multiple RLS policies can be combined"
	case "DeadCodeRemoval":
		return "Object is never referenced - likely unused"
	case "ColumnEvolution":
		return "Column modifications can be consolidated"
	case "MultipleCreateConsolidation":
		return "Multiple CREATE statements for same object"
	default:
		return "Consolidation opportunity detected"
	}
}
