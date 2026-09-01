// Package engine - Enhanced metrics for partner integrations
package engine

import (
	"fmt"
	"strings"
)

// DetailedMetrics provides comprehensive analysis metrics for partner platforms
type DetailedMetrics struct {
	// Migration counts
	TotalMigrations     int     `json:"total_migrations"`
	OptimizedMigrations int     `json:"optimized_migrations"`
	ReductionPercentage float64 `json:"reduction_percentage"`

	// Operation breakdown
	Operations OperationBreakdown `json:"operations"`

	// Performance metrics
	EstimatedTimeSavingsSeconds int     `json:"estimated_time_savings_seconds"`
	FileSizeReductionBytes      int64   `json:"file_size_reduction_bytes"`
	FileSizeReductionPercent    float64 `json:"file_size_reduction_percent"`

	// Redundancy details
	RedundanciesFound []RedundancyDetail `json:"redundancies_found"`
}

// OperationBreakdown categorizes SQL operations
type OperationBreakdown struct {
	Creates      int `json:"creates"`
	Alters       int `json:"alters"`
	Drops        int `json:"drops"`
	Inserts      int `json:"inserts"`
	Updates      int `json:"updates"`
	Deletes      int `json:"deletes"`
	Consolidated int `json:"consolidated"`
}

// RedundancyDetail describes a specific redundancy found
type RedundancyDetail struct {
	Type        string `json:"type"`        // "drop_create_cycle", "duplicate_alter", etc.
	ObjectName  string `json:"object_name"` // "users", "posts", etc.
	ObjectType  string `json:"object_type"` // "table", "index", "function"
	Severity    string `json:"severity"`    // "low", "medium", "high", "critical"
	Description string `json:"description"`
	FileNumbers []int  `json:"file_numbers"` // Which migration files involved
	Savings     string `json:"savings"`      // "2 operations consolidated"
}

// RecommendedAction suggests next steps
type RecommendedAction struct {
	Action      string `json:"action"` // "auto_cleanup", "manual_review", "guarded_apply"
	Reason      string `json:"reason"`
	Priority    string `json:"priority"`     // "high", "medium", "low"
	AutomateURL string `json:"automate_url"` // Deep link to platform action
	RiskLevel   string `json:"risk_level"`   // "safe", "moderate", "high"
}

// CalculateDetailedMetrics generates comprehensive metrics from a squash result
func CalculateDetailedMetrics(result *SquashResult, originalSize int64, optimizedSize int64) *DetailedMetrics {
	reductionPercent := 0.0
	if result.FilesProcessed > 0 {
		reductionPercent = (1.0 - float64(result.ObjectsConsolidated)/float64(result.FilesProcessed)) * 100
	}

	fileSizeReductionPercent := 0.0
	if originalSize > 0 {
		fileSizeReductionPercent = (1.0 - float64(optimizedSize)/float64(originalSize)) * 100
	}

	// Estimate time savings: ~2 seconds per migration file reduced
	filesReduced := result.FilesProcessed - result.ObjectsConsolidated
	timeSavingsSeconds := filesReduced * 2

	return &DetailedMetrics{
		TotalMigrations:             result.FilesProcessed,
		OptimizedMigrations:         result.ObjectsConsolidated,
		ReductionPercentage:         reductionPercent,
		EstimatedTimeSavingsSeconds: timeSavingsSeconds,
		FileSizeReductionBytes:      originalSize - optimizedSize,
		FileSizeReductionPercent:    fileSizeReductionPercent,
		Operations: OperationBreakdown{
			Consolidated: result.ObjectsConsolidated,
		},
		RedundanciesFound: []RedundancyDetail{},
	}
}

// GenerateRecommendations creates actionable recommendations
func GenerateRecommendations(result *SquashResult, metrics *DetailedMetrics) []RecommendedAction {
	recommendations := []RecommendedAction{}

	// High file count recommendation
	if result.FilesProcessed > 100 {
		recommendations = append(recommendations, RecommendedAction{
			Action:    "auto_cleanup",
			Reason:    "High migration file count detected. Consolidation recommended.",
			Priority:  "high",
			RiskLevel: "safe",
		})
	}

	// Significant reduction potential
	if metrics.ReductionPercentage > 50 {
		recommendations = append(recommendations, RecommendedAction{
			Action:    "auto_cleanup",
			Reason:    "Significant optimization potential detected.",
			Priority:  "high",
			RiskLevel: "safe",
		})
	}

	// Warnings present
	if len(result.Warnings) > 0 {
		recommendations = append(recommendations, RecommendedAction{
			Action:    "manual_review",
			Reason:    "Warnings detected. Manual review recommended before applying.",
			Priority:  "medium",
			RiskLevel: "moderate",
		})
	}

	return recommendations
}

// BuildAnalysisMetrics generates enhanced metrics for analysis-only workflows.
func BuildAnalysisMetrics(analysis *AnalysisResult) *DetailedMetrics {
	if analysis == nil {
		return nil
	}

	optimizedStatements := analysis.TotalStatements - len(analysis.Redundancies)
	if optimizedStatements < 0 {
		optimizedStatements = analysis.TotalStatements
	}

	reductionPercent := 0.0
	if analysis.TotalStatements > 0 {
		reductionPercent = float64(len(analysis.Redundancies)) / float64(analysis.TotalStatements) * 100
	}

	operations := OperationBreakdown{
		Inserts:      analysis.DataOperations.Inserts,
		Updates:      analysis.DataOperations.Updates,
		Deletes:      analysis.DataOperations.Deletes,
		Consolidated: len(analysis.Redundancies),
	}
	for objectType, count := range analysis.ObjectsByType {
		lower := strings.ToLower(strings.TrimSpace(objectType))
		switch {
		case strings.Contains(lower, "alter"):
			operations.Alters += count
		case strings.Contains(lower, "drop"):
			operations.Drops += count
		case strings.Contains(lower, "table"),
			strings.Contains(lower, "index"),
			strings.Contains(lower, "function"),
			strings.Contains(lower, "trigger"),
			strings.Contains(lower, "view"),
			strings.Contains(lower, "create"):
			operations.Creates += count
		}
	}

	redundanciesFound := make([]RedundancyDetail, 0, len(analysis.Redundancies))
	for _, redundancy := range analysis.Redundancies {
		redundanciesFound = append(redundanciesFound, RedundancyDetail{
			Type:        redundancy.Type,
			ObjectName:  redundancy.ObjectName,
			ObjectType:  inferRedundancyObjectType(redundancy),
			Severity:    strings.ToLower(strings.TrimSpace(redundancy.Severity)),
			Description: redundancy.Description,
			Savings:     "1 redundant operation consolidated",
		})
	}

	return &DetailedMetrics{
		TotalMigrations:             analysis.TotalFiles,
		OptimizedMigrations:         optimizedStatements,
		ReductionPercentage:         reductionPercent,
		EstimatedTimeSavingsSeconds: len(analysis.Redundancies) * 2,
		FileSizeReductionBytes:      0,
		FileSizeReductionPercent:    0,
		Operations:                  operations,
		RedundanciesFound:           redundanciesFound,
	}
}

// BuildAnalysisRecommendedActions generates structured recommendations for analysis-only workflows.
func BuildAnalysisRecommendedActions(analysis *AnalysisResult, metrics *DetailedMetrics) []RecommendedAction {
	if analysis == nil {
		return nil
	}

	recommendations := make([]RecommendedAction, 0, 3)
	redundancyCount := len(analysis.Redundancies)
	if redundancyCount > 0 {
		recommendations = append(recommendations, RecommendedAction{
			Action:    "auto_cleanup",
			Reason:    fmt.Sprintf("Detected %d redundant operations that can be consolidated.", redundancyCount),
			Priority:  "high",
			RiskLevel: "safe",
		})
	}

	if len(analysis.Warnings) > 0 {
		recommendations = append(recommendations, RecommendedAction{
			Action:    "manual_review",
			Reason:    fmt.Sprintf("Review %d analysis warnings before applying a squash.", len(analysis.Warnings)),
			Priority:  "medium",
			RiskLevel: "moderate",
		})
	}

	if metrics != nil && metrics.ReductionPercentage >= 50 {
		recommendations = append(recommendations, RecommendedAction{
			Action:    "guarded_apply",
			Reason:    "High reduction potential detected. Validate the consolidated output before deployment.",
			Priority:  "medium",
			RiskLevel: "safe",
		})
	}

	if len(recommendations) == 0 {
		recommendations = append(recommendations, RecommendedAction{
			Action:    "guarded_apply",
			Reason:    "No blocking issues detected. Proceed with review and squash when ready.",
			Priority:  "low",
			RiskLevel: "safe",
		})
	}

	return recommendations
}

// SummarizeRecommendedActions produces stable human-readable recommendation strings.
func SummarizeRecommendedActions(actions []RecommendedAction) []string {
	summaries := make([]string, 0, len(actions))
	for _, action := range actions {
		summary := strings.TrimSpace(action.Reason)
		if summary == "" {
			summary = strings.ReplaceAll(strings.TrimSpace(action.Action), "_", " ")
		}
		if summary != "" {
			summaries = append(summaries, summary)
		}
	}
	return dedupeNonEmpty(summaries)
}

// FormatEstimatedTimeSavings formats a seconds-based estimate for API/UI responses.
func FormatEstimatedTimeSavings(seconds int) string {
	if seconds <= 0 {
		return "0s"
	}
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	remainingSeconds := seconds % 60
	if remainingSeconds == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%dm %ds", minutes, remainingSeconds)
}

func inferRedundancyObjectType(redundancy Redundancy) string {
	lowerType := strings.ToLower(strings.TrimSpace(redundancy.Type))
	lowerName := strings.ToLower(strings.TrimSpace(redundancy.ObjectName))
	switch {
	case strings.Contains(lowerType, "table"), strings.Contains(lowerName, "table"):
		return "table"
	case strings.Contains(lowerType, "index"), strings.Contains(lowerName, "index"):
		return "index"
	case strings.Contains(lowerType, "function"), strings.Contains(lowerName, "function"):
		return "function"
	case strings.Contains(lowerType, "trigger"), strings.Contains(lowerName, "trigger"):
		return "trigger"
	case strings.Contains(lowerType, "view"), strings.Contains(lowerName, "view"):
		return "view"
	default:
		return "object"
	}
}
