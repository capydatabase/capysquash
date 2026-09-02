package cli

import (
	stderrors "errors"
	"strings"
	"testing"

	"github.com/capysquash/pgsquash-engine/internal/validation"
)

// TestEvaluateWorkflowValidation pins the exit-code/error semantics of the
// safe/fast workflows: workflows promise validation, so a validation
// execution failure (e.g. Docker unavailable) or real schema differences must
// fail the run, while an unproven comparison (original migrations failed to
// apply) continues by design and a clean pass returns nil.
func TestEvaluateWorkflowValidation(t *testing.T) {
	tests := []struct {
		name        string
		result      *validation.ValidationResult
		valErr      error
		wantErr     bool
		wantMessage string // substring the returned error must contain
	}{
		{
			name:        "validation execution failure (Docker unavailable) fails the workflow",
			result:      nil,
			valErr:      stderrors.New("docker daemon is not running"),
			wantErr:     true,
			wantMessage: "workflow validation failed",
		},
		{
			name:        "nil result without an error fails the workflow",
			result:      nil,
			valErr:      nil,
			wantErr:     true,
			wantMessage: "workflow validation returned no result",
		},
		{
			name:    "clean pass returns nil",
			result:  &validation.ValidationResult{Success: true},
			valErr:  nil,
			wantErr: false,
		},
		{
			name: "original migrations failed to apply is unverified and continues by design",
			result: &validation.ValidationResult{
				Success: false,
				DockerValidation: &validation.DockerValidationResult{
					OriginalApplyFailed:     true,
					OriginalMigrationsError: "relation \"users\" already exists",
				},
			},
			valErr:  nil,
			wantErr: false,
		},
		{
			name: "real schema differences fail the workflow",
			result: &validation.ValidationResult{
				Success: false,
				DockerValidation: &validation.DockerValidationResult{
					ComparisonValid: true,
					HasDifferences:  true,
					Differences:     "table users: column full_name missing in squashed schema",
				},
			},
			valErr:      nil,
			wantErr:     true,
			wantMessage: "schema differences detected",
		},
		{
			name:        "unsuccessful result without docker details still fails",
			result:      &validation.ValidationResult{Success: false},
			valErr:      nil,
			wantErr:     true,
			wantMessage: "schema differences detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := evaluateWorkflowValidation(tt.result, tt.valErr)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("evaluateWorkflowValidation() = nil, want error containing %q", tt.wantMessage)
				}
				if !strings.Contains(err.Error(), tt.wantMessage) {
					t.Fatalf("evaluateWorkflowValidation() error = %q, want it to contain %q", err.Error(), tt.wantMessage)
				}
				return
			}
			if err != nil {
				t.Fatalf("evaluateWorkflowValidation() = %v, want nil", err)
			}
		})
	}
}

// TestReportSquashValidationOutcome pins the post-squash validation outcome
// matrix for the plain `squash` command, including the --fail-on-diff
// downgrade path and the "unverified" status that must never be reported as
// passed.
func TestReportSquashValidationOutcome(t *testing.T) {
	tests := []struct {
		name       string
		failOnDiff bool
		result     *validation.ValidationResult
		valErr     error
		wantStatus string
		wantErr    bool
	}{
		{
			name:       "validation execution failure fails with --fail-on-diff (default)",
			failOnDiff: true,
			result:     nil,
			valErr:     stderrors.New("docker daemon is not running"),
			wantStatus: "failed",
			wantErr:    true,
		},
		{
			name:       "validation execution failure downgrades to warning with --fail-on-diff=false",
			failOnDiff: false,
			result:     nil,
			valErr:     stderrors.New("docker daemon is not running"),
			wantStatus: "failed",
			wantErr:    false,
		},
		{
			name:       "clean pass reports passed",
			failOnDiff: true,
			result:     &validation.ValidationResult{Success: true},
			valErr:     nil,
			wantStatus: "passed",
			wantErr:    false,
		},
		{
			name:       "originals failed to apply reports unverified and continues by design",
			failOnDiff: true,
			result: &validation.ValidationResult{
				Success: false,
				DockerValidation: &validation.DockerValidationResult{
					OriginalApplyFailed:     true,
					OriginalMigrationsError: "syntax error at or near \"ALTERR\"",
				},
			},
			valErr:     nil,
			wantStatus: "unverified",
			wantErr:    false,
		},
		{
			name:       "real schema differences fail with --fail-on-diff (default)",
			failOnDiff: true,
			result: &validation.ValidationResult{
				Success: false,
				DockerValidation: &validation.DockerValidationResult{
					ComparisonValid: true,
					HasDifferences:  true,
					Differences:     "index idx_users_email missing in squashed schema",
				},
			},
			valErr:     nil,
			wantStatus: "failed",
			wantErr:    true,
		},
		{
			name:       "real schema differences downgrade to warning with --fail-on-diff=false",
			failOnDiff: false,
			result: &validation.ValidationResult{
				Success: false,
				DockerValidation: &validation.DockerValidationResult{
					ComparisonValid: true,
					HasDifferences:  true,
					Differences:     "index idx_users_email missing in squashed schema",
				},
			},
			valErr:     nil,
			wantStatus: "failed",
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// reportSquashValidationOutcome reads the package-global
			// failOnDiff / openReport flag variables; set them explicitly and
			// restore afterwards. Run from a temp dir so an accidental
			// openReport regression could never write into the repo.
			t.Chdir(t.TempDir())
			prevFailOnDiff, prevOpenReport := failOnDiff, openReport
			failOnDiff, openReport = tt.failOnDiff, false
			t.Cleanup(func() { failOnDiff, openReport = prevFailOnDiff, prevOpenReport })

			status, err := reportSquashValidationOutcome(tt.result, tt.valErr)
			if status != tt.wantStatus {
				t.Errorf("status = %q, want %q", status, tt.wantStatus)
			}
			if tt.wantErr && err == nil {
				t.Fatalf("error = nil, want non-nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("error = %v, want nil", err)
			}
		})
	}
}
