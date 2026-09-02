package harness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

const (
	HarnessContextV1Version = "1.0.0"
	HarnessPlanV1Version    = "1.0.0"
)

type HarnessContextV1 struct {
	ContextVersion        string                   `json:"context_version"`
	GeneratedAt           time.Time                `json:"generated_at"`
	EngineVersion         string                   `json:"engine_version"`
	ContextHash           string                   `json:"context_hash"`
	Workspace             ContextWorkspace         `json:"workspace"`
	ConfigSnapshot        ContextConfigSnapshot    `json:"config_snapshot"`
	SchemaState           ContextSchemaState       `json:"schema_state"`
	MigrationChain        []ContextMigrationNode   `json:"migration_chain"`
	DependencyGraph       ContextDependencyGraph   `json:"dependency_graph"`
	SquashCandidates      []ContextSquashCandidate `json:"squash_candidates"`
	AmbiguousCases        []ContextAmbiguousCase   `json:"ambiguous_cases"`
	DeterministicWarnings []string                 `json:"deterministic_warnings,omitempty"`
	Metrics               ContextMetrics           `json:"metrics"`
}

type ContextWorkspace struct {
	ProjectRoot      string `json:"project_root,omitempty"`
	OutputDirectory  string `json:"output_directory,omitempty"`
	OriginalFileRoot string `json:"original_file_root,omitempty"`
	FileCount        int    `json:"file_count,omitempty"`
	WorkspaceHash    string `json:"workspace_hash,omitempty"`
}

type ContextConfigSnapshot struct {
	SafetyLevel    string `json:"safety_level,omitempty"`
	ValidationMode string `json:"validation_mode,omitempty"`
}

type ContextSchemaState struct {
	SchemaSQLPath  string                `json:"schema_sql_path,omitempty"`
	SchemaSQLHash  string                `json:"schema_sql_hash,omitempty"`
	StatementCount int                   `json:"statement_count,omitempty"`
	ObjectCount    int                   `json:"object_count,omitempty"`
	Objects        []ContextSchemaObject `json:"objects,omitempty"`
	Extensions     []string              `json:"extensions,omitempty"`
}

type ContextSchemaObject struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

type ContextMigrationNode struct {
	MigrationID        string   `json:"migration_id"`
	Sequence           int      `json:"sequence"`
	Checksum           string   `json:"checksum,omitempty"`
	OperationTypes     []string `json:"operation_types,omitempty"`
	AffectedObjects    []string `json:"affected_objects,omitempty"`
	SemanticOperations []string `json:"semantic_operations,omitempty"`
	DependsOn          []string `json:"depends_on,omitempty"`
	Reversible         bool     `json:"reversible"`
	SquashSafe         bool     `json:"squash_safe"`
	RiskFlags          []string `json:"risk_flags,omitempty"`
}

type ContextDependencyGraph struct {
	Nodes  []ContextDependencyNode `json:"nodes"`
	Edges  []ContextDependencyEdge `json:"edges"`
	Cycles [][]string              `json:"cycles,omitempty"`
}

type ContextDependencyNode struct {
	ID   string `json:"id"`
	Kind string `json:"kind,omitempty"`
}

type ContextDependencyEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Type string `json:"type,omitempty"`
}

type ContextMetrics struct {
	OriginalFileCount      int    `json:"original_file_count,omitempty"`
	OriginalStatementCount int    `json:"original_statement_count,omitempty"`
	DataOperationCount     int    `json:"data_operation_count,omitempty"`
	ObjectsConsolidated    int    `json:"objects_consolidated,omitempty"`
	WarningsCount          int    `json:"warnings_count,omitempty"`
	ProcessingTime         string `json:"processing_time,omitempty"`
}

type ContextSquashCandidate struct {
	GroupID        string   `json:"group_id"`
	MigrationIDs   []string `json:"migration_ids,omitempty"`
	Confidence     string   `json:"confidence,omitempty"`
	Reason         string   `json:"reason,omitempty"`
	BlockedBy      []string `json:"blocked_by,omitempty"`
	AllowedActions []string `json:"allowed_actions"`
}

type ContextAmbiguousCase struct {
	Type             string   `json:"type,omitempty"`
	Summary          string   `json:"summary,omitempty"`
	Reason           string   `json:"reason,omitempty"`
	RawSQLExcerpt    string   `json:"raw_sql_excerpt,omitempty"`
	Context          string   `json:"context,omitempty"`
	CaseID           string   `json:"case_id"`
	MigrationID      string   `json:"migration_id"`
	RiskLevel        string   `json:"risk_level"`
	AllowedDecisions []string `json:"allowed_decisions"`
}

type HarnessPlanV1 struct {
	PlanVersion           string                   `json:"plan_version"`
	ContextVersion        string                   `json:"context_version"`
	ContextHash           string                   `json:"context_hash"`
	GeneratedAt           time.Time                `json:"generated_at"`
	Model                 HarnessPlanModel         `json:"model"`
	CandidateDecisions    []CandidateDecision      `json:"candidate_decisions"`
	AmbiguousResolutions  []AmbiguousResolution    `json:"ambiguous_resolutions"`
	GlobalRecommendations []string                 `json:"global_recommendations"`
	RequiresHumanReview   []HumanReviewRequirement `json:"requires_human_review"`
}

type HarnessPlanModel struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
}

type CandidateDecision struct {
	GroupID    string  `json:"group_id"`
	Decision   string  `json:"decision"`
	Rationale  string  `json:"rationale"`
	Confidence float64 `json:"confidence"`
}

type AmbiguousResolution struct {
	CaseID         string  `json:"case_id"`
	Classification string  `json:"classification"`
	Decision       string  `json:"decision"`
	Rationale      string  `json:"rationale"`
	Confidence     float64 `json:"confidence"`
}

type HumanReviewRequirement struct {
	ID         string  `json:"id"`
	Reason     string  `json:"reason"`
	Confidence float64 `json:"confidence"`
	RiskLevel  string  `json:"risk_level"`
}

type PlanValidationOptions struct {
	ConfidenceThreshold         float64
	RequireCriticalManualReview bool
}

type PlanValidationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
	ItemID  string `json:"item_id,omitempty"`
}

type PlanValidationResult struct {
	Valid    bool                  `json:"valid"`
	Errors   []PlanValidationIssue `json:"errors"`
	Warnings []PlanValidationIssue `json:"warnings"`
}

func canonicalContextCopy(context *HarnessContextV1) HarnessContextV1 {
	copyContext := *context
	copyContext.ContextVersion = strings.TrimSpace(copyContext.ContextVersion)
	if copyContext.ContextVersion == "" {
		copyContext.ContextVersion = HarnessContextV1Version
	}
	copyContext.ContextHash = strings.TrimSpace(copyContext.ContextHash)
	return copyContext
}

func canonicalPlanCopy(plan *HarnessPlanV1) HarnessPlanV1 {
	copyPlan := *plan
	copyPlan.ContextVersion = strings.TrimSpace(copyPlan.ContextVersion)
	if copyPlan.ContextVersion == "" {
		copyPlan.ContextVersion = HarnessContextV1Version
	}
	copyPlan.ContextHash = strings.TrimSpace(copyPlan.ContextHash)
	return copyPlan
}

func ComputeHarnessContextHash(context *HarnessContextV1) (string, error) {
	if context == nil {
		return "", fmt.Errorf("context cannot be nil")
	}
	copyContext := canonicalContextCopy(context)
	copyContext.ContextHash = ""
	// generated_at is provenance, not semantic input. Excluding it makes the
	// context hash stable for identical workspace/config/schema evidence.
	copyContext.GeneratedAt = time.Time{}
	body, err := json.Marshal(copyContext)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func WriteHarnessContextV1(path string, context *HarnessContextV1) error {
	if context == nil {
		return fmt.Errorf("context cannot be nil")
	}
	normalized := canonicalContextCopy(context)
	if normalized.ContextVersion == "" {
		normalized.ContextVersion = HarnessContextV1Version
	}
	if normalized.ContextHash == "" {
		hash, err := ComputeHarnessContextHash(&normalized)
		if err != nil {
			return err
		}
		normalized.ContextHash = hash
	}
	body, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func LoadHarnessContextV1(path string) (*HarnessContextV1, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var context HarnessContextV1
	if err := json.Unmarshal(body, &context); err != nil {
		return nil, err
	}
	context.ContextVersion = strings.TrimSpace(context.ContextVersion)
	if context.ContextVersion != HarnessContextV1Version {
		return nil, fmt.Errorf("unsupported context version: %s", context.ContextVersion)
	}
	context.ContextHash = strings.TrimSpace(context.ContextHash)
	if context.ContextHash == "" {
		return nil, fmt.Errorf("context_hash is required")
	}
	return &context, nil
}

func WriteHarnessPlanV1(path string, plan *HarnessPlanV1) error {
	if plan == nil {
		return fmt.Errorf("plan cannot be nil")
	}
	normalized := canonicalPlanCopy(plan)
	if normalized.ContextVersion == "" {
		normalized.ContextVersion = HarnessContextV1Version
	}
	body, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}

func LoadHarnessPlanV1(path string) (*HarnessPlanV1, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var plan HarnessPlanV1
	if err := json.Unmarshal(body, &plan); err != nil {
		return nil, err
	}
	if plan.PlanVersion != HarnessPlanV1Version {
		return nil, fmt.Errorf("unsupported plan version: %s", plan.PlanVersion)
	}
	plan.ContextVersion = strings.TrimSpace(plan.ContextVersion)
	plan.ContextHash = strings.TrimSpace(plan.ContextHash)
	if plan.ContextVersion != HarnessContextV1Version {
		return nil, fmt.Errorf("unsupported context version: %s", plan.ContextVersion)
	}
	if plan.ContextHash == "" {
		return nil, fmt.Errorf("context_hash is required")
	}
	return &plan, nil
}

func ValidateHarnessPlanV1(plan *HarnessPlanV1, context *HarnessContextV1, options *PlanValidationOptions) (*PlanValidationResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan cannot be nil")
	}
	if context == nil {
		return nil, fmt.Errorf("context cannot be nil")
	}

	opts := PlanValidationOptions{ConfidenceThreshold: 0.85, RequireCriticalManualReview: true}
	if options != nil {
		if options.ConfidenceThreshold > 0 {
			opts.ConfidenceThreshold = options.ConfidenceThreshold
		}
		opts.RequireCriticalManualReview = options.RequireCriticalManualReview
	}

	result := &PlanValidationResult{Valid: true, Errors: []PlanValidationIssue{}, Warnings: []PlanValidationIssue{}}
	addError := func(issue PlanValidationIssue) {
		result.Valid = false
		result.Errors = append(result.Errors, issue)
	}

	if plan.PlanVersion != HarnessPlanV1Version {
		addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "plan_version", Message: fmt.Sprintf("unsupported plan version: %s", plan.PlanVersion)})
	}

	contextVersion := strings.TrimSpace(context.ContextVersion)
	if contextVersion != HarnessContextV1Version {
		addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "context_version", Message: fmt.Sprintf("unsupported context version: %s", contextVersion)})
	}
	planContextVersion := strings.TrimSpace(plan.ContextVersion)
	if planContextVersion != contextVersion {
		addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "context_version", Message: fmt.Sprintf("plan context version %s does not match context version %s", planContextVersion, contextVersion)})
	}

	contextHash := strings.TrimSpace(context.ContextHash)
	computedContextHash, err := ComputeHarnessContextHash(context)
	if err != nil {
		return nil, err
	}
	if contextHash == "" || contextHash != computedContextHash {
		addError(PlanValidationIssue{Code: "HARNESS_CONTEXT_INTEGRITY_INVALID", Field: "context_hash", Message: "context hash is missing or does not match context contents"})
	}
	planContextHash := strings.TrimSpace(plan.ContextHash)
	if planContextHash != contextHash {
		addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "context_hash", Message: "plan hash does not match context hash"})
	}

	candidateIDs := make(map[string]ContextSquashCandidate, len(context.SquashCandidates))
	for _, candidate := range context.SquashCandidates {
		if _, exists := candidateIDs[candidate.GroupID]; exists {
			addError(PlanValidationIssue{Code: "HARNESS_CONTEXT_INVALID", Field: "squash_candidates", ItemID: candidate.GroupID, Message: "duplicate candidate id"})
		}
		candidateIDs[candidate.GroupID] = candidate
	}
	caseIDs := make(map[string]ContextAmbiguousCase, len(context.AmbiguousCases))
	for _, ambiguousCase := range context.AmbiguousCases {
		if _, exists := caseIDs[ambiguousCase.CaseID]; exists {
			addError(PlanValidationIssue{Code: "HARNESS_CONTEXT_INVALID", Field: "ambiguous_cases", ItemID: ambiguousCase.CaseID, Message: "duplicate ambiguous case id"})
		}
		caseIDs[ambiguousCase.CaseID] = ambiguousCase
	}

	reviewSet := make(map[string]struct{}, len(plan.RequiresHumanReview))
	for _, review := range plan.RequiresHumanReview {
		if _, exists := reviewSet[review.ID]; exists {
			addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "requires_human_review", ItemID: review.ID, Message: "duplicate human review entry"})
		}
		reviewSet[review.ID] = struct{}{}
		if _, ok := candidateIDs[review.ID]; !ok {
			if _, ok := caseIDs[review.ID]; !ok {
				addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "requires_human_review", ItemID: review.ID, Message: "review entry references unknown id"})
			}
		}
	}

	candidateDecisionByID := make(map[string]CandidateDecision, len(plan.CandidateDecisions))
	for _, decision := range plan.CandidateDecisions {
		if _, exists := candidateDecisionByID[decision.GroupID]; exists {
			addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "candidate_decisions", ItemID: decision.GroupID, Message: "duplicate candidate decision"})
		}
		candidateDecisionByID[decision.GroupID] = decision
	}
	for candidateID := range candidateIDs {
		if _, exists := candidateDecisionByID[candidateID]; !exists {
			addError(PlanValidationIssue{Code: "HARNESS_PLAN_INCOMPLETE", Field: "candidate_decisions", ItemID: candidateID, Message: "candidate decision is required"})
		}
	}

	allowedCandidateDecisions := map[string]struct{}{"confirm": {}, "reject": {}, "split": {}}
	for _, decision := range plan.CandidateDecisions {
		candidate, exists := candidateIDs[decision.GroupID]
		if !exists {
			addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "candidate_decisions", ItemID: decision.GroupID, Message: "candidate decision references unknown group"})
			continue
		}
		if _, ok := allowedCandidateDecisions[decision.Decision]; !ok {
			addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "candidate_decisions.decision", ItemID: decision.GroupID, Message: fmt.Sprintf("invalid candidate decision: %s", decision.Decision)})
		}
		if len(candidate.AllowedActions) > 0 && !containsString(candidate.AllowedActions, decision.Decision) {
			addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "candidate_decisions.decision", ItemID: decision.GroupID, Message: fmt.Sprintf("decision '%s' is not allowed for candidate", decision.Decision)})
		}
		if decision.Confidence < 0 || decision.Confidence > 1 {
			addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "candidate_decisions.confidence", ItemID: decision.GroupID, Message: "candidate confidence must be between 0.0 and 1.0"})
		}
		if decision.Decision == "confirm" && len(candidate.BlockedBy) > 0 {
			for _, dependencyID := range candidate.BlockedBy {
				dependencyDecision, ok := candidateDecisionByID[dependencyID]
				if !ok || dependencyDecision.Decision != "confirm" {
					addError(PlanValidationIssue{Code: "HARNESS_PLAN_DEPENDENCY_ORDER_INVALID", Field: "candidate_decisions", ItemID: decision.GroupID, Message: fmt.Sprintf("candidate depends on %s which is not confirmed", dependencyID)})
				}
			}
		}
		if decision.Confidence < opts.ConfidenceThreshold {
			if _, reviewed := reviewSet[decision.GroupID]; !reviewed {
				addError(PlanValidationIssue{Code: "HARNESS_PLAN_LOW_CONFIDENCE_REVIEW_REQUIRED", Field: "candidate_decisions.confidence", ItemID: decision.GroupID, Message: "low confidence candidate decision requires human review"})
			}
		}
	}

	resolutionByID := make(map[string]AmbiguousResolution, len(plan.AmbiguousResolutions))
	for _, resolution := range plan.AmbiguousResolutions {
		if _, exists := resolutionByID[resolution.CaseID]; exists {
			addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "ambiguous_resolutions", ItemID: resolution.CaseID, Message: "duplicate ambiguous resolution"})
		}
		resolutionByID[resolution.CaseID] = resolution
		ambiguousCase, exists := caseIDs[resolution.CaseID]
		if !exists {
			addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "ambiguous_resolutions", ItemID: resolution.CaseID, Message: "ambiguous resolution references unknown case"})
			continue
		}
		if resolution.Confidence < 0 || resolution.Confidence > 1 {
			addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "ambiguous_resolutions.confidence", ItemID: resolution.CaseID, Message: "resolution confidence must be between 0.0 and 1.0"})
		}
		if !containsString(ambiguousCase.AllowedDecisions, resolution.Decision) {
			addError(PlanValidationIssue{Code: "HARNESS_PLAN_INVALID", Field: "ambiguous_resolutions.decision", ItemID: resolution.CaseID, Message: fmt.Sprintf("decision '%s' is not allowed for case", resolution.Decision)})
		}
		if opts.RequireCriticalManualReview && strings.EqualFold(ambiguousCase.RiskLevel, "critical") && resolution.Decision != "manual_review" {
			addError(PlanValidationIssue{Code: "HARNESS_PLAN_CRITICAL_RISK_REVIEW_REQUIRED", Field: "ambiguous_resolutions", ItemID: resolution.CaseID, Message: "critical risk case must remain manual review"})
		}
		if resolution.Confidence < opts.ConfidenceThreshold && resolution.Decision != "manual_review" {
			if _, reviewed := reviewSet[resolution.CaseID]; !reviewed {
				addError(PlanValidationIssue{Code: "HARNESS_PLAN_LOW_CONFIDENCE_REVIEW_REQUIRED", Field: "ambiguous_resolutions.confidence", ItemID: resolution.CaseID, Message: "low confidence resolution requires human review"})
			}
		}
	}
	for caseID := range caseIDs {
		if _, exists := resolutionByID[caseID]; !exists {
			addError(PlanValidationIssue{Code: "HARNESS_PLAN_INCOMPLETE", Field: "ambiguous_resolutions", ItemID: caseID, Message: "ambiguous resolution is required"})
		}
	}

	return result, nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if strings.EqualFold(value, target) {
			return true
		}
	}
	return false
}
