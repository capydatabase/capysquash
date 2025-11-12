// Package engine - Enhanced metrics for partner integrations
package engine

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
	Type        string `json:"type"`         // "drop_create_cycle", "duplicate_alter", etc.
	ObjectName  string `json:"object_name"`  // "users", "posts", etc.
	ObjectType  string `json:"object_type"`  // "table", "index", "function"
	Severity    string `json:"severity"`     // "low", "medium", "high", "critical"
	Description string `json:"description"`
	FileNumbers []int  `json:"file_numbers"` // Which migration files involved
	Savings     string `json:"savings"`      // "2 operations consolidated"
}

// RecommendedAction suggests next steps
type RecommendedAction struct {
	Action      string `json:"action"`       // "auto_cleanup", "manual_review", "guarded_apply"
	Reason      string `json:"reason"`
	Priority    string `json:"priority"`     // "high", "medium", "low"
	AutomateURL string `json:"automate_url"` // Deep link to platform action
	RiskLevel   string `json:"risk_level"`   // "safe", "moderate", "high"
}

// EnhancedSquashResult extends SquashResult with detailed metrics
type EnhancedSquashResult struct {
	SquashResult

	// Enhanced fields for partner integrations
	DetailedMetrics    *DetailedMetrics    `json:"detailed_metrics,omitempty"`
	RecommendedActions []RecommendedAction `json:"recommended_actions,omitempty"`
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
