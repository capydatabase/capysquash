package validation

import (
	"errors"
	"testing"
)

// TestFinalizeComparisonOutcome pins down the Success semantics that define the
// tool's core claim: "passed" only when a real comparison ran and matched.
func TestFinalizeComparisonOutcome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		originalErr     error
		diff            *SchemaDiff
		wantSuccess     bool
		wantOrigFailed  bool
		wantComparison  bool
		wantHasDiff     bool
	}{
		{
			name:           "originals apply and schemas match -> passed",
			originalErr:    nil,
			diff:           &SchemaDiff{HasDifferences: false},
			wantSuccess:    true,
			wantOrigFailed: false,
			wantComparison: true,
			wantHasDiff:    false,
		},
		{
			name:           "originals apply but schemas differ -> not passed",
			originalErr:    nil,
			diff:           &SchemaDiff{HasDifferences: true, Differences: []string{"table users differs"}},
			wantSuccess:    false,
			wantOrigFailed: false,
			wantComparison: true,
			wantHasDiff:    true,
		},
		{
			name:           "originals fail with a diff -> not success, unproven",
			originalErr:    errors.New("original migration 003 failed"),
			diff:           &SchemaDiff{HasDifferences: true, Differences: []string{"missing table"}},
			wantSuccess:    false,
			wantOrigFailed: true,
			wantComparison: false,
			wantHasDiff:    true,
		},
		{
			name:           "originals fail without a diff -> still not success, unproven",
			originalErr:    errors.New("original migration 003 failed"),
			diff:           &SchemaDiff{HasDifferences: false},
			wantSuccess:    false,
			wantOrigFailed: true,
			wantComparison: false,
			wantHasDiff:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := &DockerValidationResult{Success: true} // start optimistic to prove it is overwritten
			finalizeComparisonOutcome(result, tt.originalErr, tt.diff)

			if result.Success != tt.wantSuccess {
				t.Errorf("Success = %v, want %v", result.Success, tt.wantSuccess)
			}
			if result.OriginalApplyFailed != tt.wantOrigFailed {
				t.Errorf("OriginalApplyFailed = %v, want %v", result.OriginalApplyFailed, tt.wantOrigFailed)
			}
			if result.ComparisonValid != tt.wantComparison {
				t.Errorf("ComparisonValid = %v, want %v", result.ComparisonValid, tt.wantComparison)
			}
			if result.HasDifferences != tt.wantHasDiff {
				t.Errorf("HasDifferences = %v, want %v", result.HasDifferences, tt.wantHasDiff)
			}
			if tt.wantOrigFailed && result.OriginalMigrationsError == "" {
				t.Error("expected OriginalMigrationsError to be recorded when originals fail")
			}
		})
	}
}
