package engine

import "testing"

func TestBuildAnalysisMetricsAndRecommendations(t *testing.T) {
	analysis := &AnalysisResult{
		TotalFiles:      3,
		TotalStatements: 8,
		ObjectsByType: map[string]int{
			"table": 2,
			"index": 1,
		},
		DataOperations: DataOpCounts{
			Inserts: 1,
			Updates: 2,
			Deletes: 3,
		},
		Warnings: []string{"Potential lock contention on users"},
		Redundancies: []Redundancy{
			{
				Type:        "duplicate_index",
				ObjectName:  "users_email_idx",
				Description: "Duplicate index definition",
				Severity:    "high",
			},
			{
				Type:        "overridden_alter",
				ObjectName:  "users",
				Description: "Column default overridden later",
				Severity:    "medium",
			},
		},
	}

	metrics := BuildAnalysisMetrics(analysis)
	if metrics == nil {
		t.Fatal("expected metrics to be populated")
	}
	if metrics.TotalMigrations != 3 {
		t.Fatalf("expected total_migrations=3, got %d", metrics.TotalMigrations)
	}
	if metrics.OptimizedMigrations != 6 {
		t.Fatalf("expected optimized_migrations=6, got %d", metrics.OptimizedMigrations)
	}
	if metrics.Operations.Consolidated != 2 {
		t.Fatalf("expected consolidated=2, got %d", metrics.Operations.Consolidated)
	}
	if len(metrics.RedundanciesFound) != 2 {
		t.Fatalf("expected 2 redundancy details, got %d", len(metrics.RedundanciesFound))
	}

	actions := BuildAnalysisRecommendedActions(analysis, metrics)
	if len(actions) < 2 {
		t.Fatalf("expected structured recommendations, got %d", len(actions))
	}

	summaries := SummarizeRecommendedActions(actions)
	if len(summaries) == 0 {
		t.Fatal("expected recommendation summaries")
	}
	if summaries[0] == "" {
		t.Fatal("expected non-empty recommendation summary")
	}
}

func TestFormatEstimatedTimeSavings(t *testing.T) {
	cases := []struct {
		seconds int
		want    string
	}{
		{seconds: 0, want: "0s"},
		{seconds: 45, want: "45s"},
		{seconds: 120, want: "2m"},
		{seconds: 125, want: "2m 5s"},
	}

	for _, tc := range cases {
		if got := FormatEstimatedTimeSavings(tc.seconds); got != tc.want {
			t.Fatalf("FormatEstimatedTimeSavings(%d) = %q, want %q", tc.seconds, got, tc.want)
		}
	}
}
