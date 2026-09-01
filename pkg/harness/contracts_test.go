package harness

import (
	"testing"
	"time"
)

// baseContext builds a valid context with one squash candidate and one critical
// ambiguous case, plus a dependent candidate to exercise dependency ordering.
func baseContext(t *testing.T) *HarnessContextV1 {
	t.Helper()
	ctx := &HarnessContextV1{
		ContextVersion: HarnessContextV1Version,
		SquashCandidates: []ContextSquashCandidate{
			{GroupID: "g1", AllowedActions: []string{"confirm", "reject", "split"}, Confidence: "high"},
			{GroupID: "g2", AllowedActions: []string{"confirm", "reject", "split"}, Confidence: "high", BlockedBy: []string{"g1"}},
		},
		AmbiguousCases: []ContextAmbiguousCase{
			{CaseID: "c1", RiskLevel: "critical", AllowedDecisions: []string{"preserve", "split", "manual_review"}, MigrationID: "001.sql"},
		},
	}
	hash, err := ComputeHarnessContextHash(ctx)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	ctx.ContextHash = hash
	return ctx
}

func basePlan(ctx *HarnessContextV1) *HarnessPlanV1 {
	return &HarnessPlanV1{
		PlanVersion:    HarnessPlanV1Version,
		ContextVersion: HarnessContextV1Version,
		ContextHash:    ctx.ContextHash,
		Model:          HarnessPlanModel{Provider: "test", Model: "m"},
		CandidateDecisions: []CandidateDecision{
			{GroupID: "g1", Decision: "confirm", Confidence: 0.95},
			{GroupID: "g2", Decision: "confirm", Confidence: 0.95},
		},
		AmbiguousResolutions: []AmbiguousResolution{
			{CaseID: "c1", Decision: "manual_review", Confidence: 0.9},
		},
		RequiresHumanReview: []HumanReviewRequirement{
			{ID: "c1", Reason: "critical", Confidence: 0.9, RiskLevel: "critical"},
		},
	}
}

func hasCode(res *PlanValidationResult, code string) bool {
	for _, e := range res.Errors {
		if e.Code == code {
			return true
		}
	}
	return false
}

func TestValidateHarnessPlanV1Matrix(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(ctx *HarnessContextV1, plan *HarnessPlanV1)
		wantValid bool
		wantCode  string
	}{
		{
			name: "missing candidate decision rejected",
			mutate: func(_ *HarnessContextV1, plan *HarnessPlanV1) {
				plan.CandidateDecisions = plan.CandidateDecisions[:1]
			},
			wantValid: false,
			wantCode:  "HARNESS_PLAN_INCOMPLETE",
		},
		{
			name: "duplicate candidate decision rejected",
			mutate: func(_ *HarnessContextV1, plan *HarnessPlanV1) {
				plan.CandidateDecisions = append(plan.CandidateDecisions, plan.CandidateDecisions[0])
			},
			wantValid: false,
			wantCode:  "HARNESS_PLAN_INVALID",
		},
		{
			name: "missing ambiguous resolution rejected",
			mutate: func(_ *HarnessContextV1, plan *HarnessPlanV1) {
				plan.AmbiguousResolutions = nil
			},
			wantValid: false,
			wantCode:  "HARNESS_PLAN_INCOMPLETE",
		},
		{
			name:      "valid plan accepted",
			mutate:    func(_ *HarnessContextV1, _ *HarnessPlanV1) {},
			wantValid: true,
		},
		{
			name:      "context hash mismatch rejected",
			mutate:    func(_ *HarnessContextV1, plan *HarnessPlanV1) { plan.ContextHash = "sha256:deadbeef" },
			wantValid: false,
			wantCode:  "HARNESS_PLAN_INVALID",
		},
		{
			name:      "plan version mismatch rejected",
			mutate:    func(_ *HarnessContextV1, plan *HarnessPlanV1) { plan.PlanVersion = "9.9.9" },
			wantValid: false,
			wantCode:  "HARNESS_PLAN_INVALID",
		},
		{
			name: "unknown candidate group rejected",
			mutate: func(_ *HarnessContextV1, plan *HarnessPlanV1) {
				plan.CandidateDecisions = append(plan.CandidateDecisions, CandidateDecision{GroupID: "ghost", Decision: "confirm", Confidence: 0.95})
			},
			wantValid: false,
			wantCode:  "HARNESS_PLAN_INVALID",
		},
		{
			name: "decision outside allowed_actions rejected",
			mutate: func(ctx *HarnessContextV1, plan *HarnessPlanV1) {
				ctx.SquashCandidates[0].AllowedActions = []string{"reject"}
				rehash(ctx, plan)
			},
			wantValid: false,
			wantCode:  "HARNESS_PLAN_INVALID",
		},
		{
			name: "critical risk not manual_review rejected",
			mutate: func(_ *HarnessContextV1, plan *HarnessPlanV1) {
				plan.AmbiguousResolutions[0].Decision = "preserve"
			},
			wantValid: false,
			wantCode:  "HARNESS_PLAN_CRITICAL_RISK_REVIEW_REQUIRED",
		},
		{
			name: "low confidence candidate without review rejected",
			mutate: func(_ *HarnessContextV1, plan *HarnessPlanV1) {
				plan.CandidateDecisions[0].Confidence = 0.4
			},
			wantValid: false,
			wantCode:  "HARNESS_PLAN_LOW_CONFIDENCE_REVIEW_REQUIRED",
		},
		{
			name: "dependency order violation rejected",
			mutate: func(_ *HarnessContextV1, plan *HarnessPlanV1) {
				// g2 depends on g1; rejecting g1 while confirming g2 violates order.
				plan.CandidateDecisions[0].Decision = "reject"
			},
			wantValid: false,
			wantCode:  "HARNESS_PLAN_DEPENDENCY_ORDER_INVALID",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := baseContext(t)
			plan := basePlan(ctx)
			tc.mutate(ctx, plan)

			res, err := ValidateHarnessPlanV1(plan, ctx, nil)
			if err != nil {
				t.Fatalf("validate returned error: %v", err)
			}
			if res.Valid != tc.wantValid {
				t.Fatalf("valid=%v want %v (errors=%+v)", res.Valid, tc.wantValid, res.Errors)
			}
			if tc.wantCode != "" && !hasCode(res, tc.wantCode) {
				t.Fatalf("expected error code %q, got %+v", tc.wantCode, res.Errors)
			}
		})
	}
}

func TestComputeHarnessContextHashIgnoresGeneratedAt(t *testing.T) {
	ctx := baseContext(t)
	first := ctx.ContextHash
	ctx.GeneratedAt = time.Now().UTC().Add(24 * time.Hour)
	ctx.ContextHash = ""
	second, err := ComputeHarnessContextHash(ctx)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if first != second {
		t.Fatalf("generated_at changed semantic context hash: %s != %s", first, second)
	}
}

// rehash recomputes the context hash and rebinds the plan after a context mutation.
func rehash(ctx *HarnessContextV1, plan *HarnessPlanV1) {
	ctx.ContextHash = ""
	h, _ := ComputeHarnessContextHash(ctx)
	ctx.ContextHash = h
	plan.ContextHash = h
}
