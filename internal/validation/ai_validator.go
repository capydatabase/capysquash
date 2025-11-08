package validation

import (
	"context"
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/ai"
	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/fatih/color"
)

// AIValidator provides AI-powered semantic validation and quality analysis
type AIValidator struct {
	analyzer *ai.Analyzer
	config   *AIValidationConfig
	verbose  bool
}

// AIValidationConfig configures AI validation behavior
type AIValidationConfig struct {
	EnableSemanticValidation bool    // Validate logical correctness beyond syntax
	EnableDependencyChecks   bool    // AI-powered dependency validation
	EnableQualityReports     bool    // Generate human-readable quality reports
	EnableAutoRepair         bool    // Allow AI to suggest automatic repairs
	ConfidenceThreshold      float64 // Minimum confidence for AI suggestions (0.0-1.0)
}

// AIValidationResult represents the result of AI-powered validation
type AIValidationResult struct {
	Success                 bool               `json:"success"`
	SemanticIssues          []SemanticIssue    `json:"semantic_issues,omitempty"`
	QualityReport           *QualityReport     `json:"quality_report,omitempty"`
	DependencyIssues        []DependencyIssue  `json:"dependency_issues,omitempty"`
	RepairSuggestions       []RepairSuggestion `json:"repair_suggestions,omitempty"`
	OptimizationSuggestions []ai.Optimization  `json:"optimization_suggestions,omitempty"`
}

// SemanticIssue represents a logical/semantic validation issue
type SemanticIssue struct {
	Type        string  `json:"type"`     // "logic_error", "inconsistency", "missing_constraint"
	Severity    string  `json:"severity"` // "critical", "warning", "info"
	Description string  `json:"description"`
	Location    string  `json:"location"`  // File/object where issue was found
	Reasoning   string  `json:"reasoning"` // AI's explanation
	Confidence  float64 `json:"confidence"`
	Suggestion  string  `json:"suggestion,omitempty"`
}

// QualityReport provides an overall quality assessment
type QualityReport struct {
	OverallScore    int      `json:"overall_score"`   // 1-10 scale
	Maintainability string   `json:"maintainability"` // "high", "medium", "low"
	Complexity      int      `json:"complexity"`      // 1-10 scale
	BestPractices   []string `json:"best_practices"`  // Followed best practices
	Violations      []string `json:"violations"`      // Violated best practices
	Summary         string   `json:"summary"`
}

// DependencyIssue represents an AI-detected dependency problem
type DependencyIssue struct {
	Type        string   `json:"type"` // "circular", "missing", "invalid_order"
	Description string   `json:"description"`
	Objects     []string `json:"objects"` // Affected objects
	Confidence  float64  `json:"confidence"`
	Suggestion  string   `json:"suggestion"`
}

// RepairSuggestion represents an AI-suggested fix
type RepairSuggestion struct {
	Issue      string  `json:"issue"`
	Fix        string  `json:"fix"`    // SQL fix or description
	Impact     string  `json:"impact"` // Expected impact
	Confidence float64 `json:"confidence"`
	AutoApply  bool    `json:"auto_apply"` // Whether safe to auto-apply
}

// NewAIValidator creates a new AI-powered validator
func NewAIValidator(analyzer *ai.Analyzer, config *AIValidationConfig, verbose bool) *AIValidator {
	if config == nil {
		config = &AIValidationConfig{
			EnableSemanticValidation: true,
			EnableDependencyChecks:   true,
			EnableQualityReports:     true,
			EnableAutoRepair:         false, // Conservative default
			ConfidenceThreshold:      0.85,
		}
	}

	return &AIValidator{
		analyzer: analyzer,
		config:   config,
		verbose:  verbose,
	}
}

// ValidatePostProcessing performs AI-powered post-processing validation
func (av *AIValidator) ValidatePostProcessing(ctx context.Context, migrations []*types.Migration, squashedSQL string) (*AIValidationResult, error) {
	if av.analyzer == nil {
		return nil, errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation, "AI analyzer not initialized", nil)
	}

	result := &AIValidationResult{
		Success:           true,
		SemanticIssues:    make([]SemanticIssue, 0),
		DependencyIssues:  make([]DependencyIssue, 0),
		RepairSuggestions: make([]RepairSuggestion, 0),
	}

	av.logInfo("🤖 Starting AI-powered post-processing validation...")

	// Phase 1: Semantic Validation
	if av.config.EnableSemanticValidation {
		av.logInfo("📋 Phase 1: Semantic validation...")
		if err := av.validateSemantics(ctx, squashedSQL, result); err != nil {
			av.logInfo("⚠️  Semantic validation error: %v", err)
			// Don't fail - continue with other checks
		}
	}

	// Phase 2: Dependency Validation
	if av.config.EnableDependencyChecks {
		av.logInfo("🔗 Phase 2: AI-powered dependency validation...")
		if err := av.validateDependencies(ctx, squashedSQL, result); err != nil {
			av.logInfo("⚠️  Dependency validation error: %v", err)
		}
	}

	// Phase 3: Quality Report Generation
	if av.config.EnableQualityReports {
		av.logInfo("📊 Phase 3: Generating quality report...")
		if err := av.generateQualityReport(ctx, squashedSQL, result); err != nil {
			av.logInfo("⚠️  Quality report generation error: %v", err)
		}
	}

	// Phase 4: Optimization Suggestions
	av.logInfo("💡 Phase 4: Analyzing for optimizations...")
	if err := av.suggestOptimizations(ctx, squashedSQL, result); err != nil {
		av.logInfo("⚠️  Optimization analysis error: %v", err)
	}

	// Determine overall success
	result.Success = av.evaluateOverallSuccess(result)

	if av.verbose {
		av.printSummary(result)
	}

	return result, nil
}

// validateSemantics performs semantic validation on SQL
func (av *AIValidator) validateSemantics(ctx context.Context, sql string, result *AIValidationResult) error {
	// Split SQL into logical chunks for analysis (avoid token limits)
	chunks := av.chunkSQL(sql, 3000) // ~3000 tokens per chunk

	for i, chunk := range chunks {
		av.logInfo("   Analyzing chunk %d/%d...", i+1, len(chunks))

		// Use AI to detect semantic issues
		prompt := fmt.Sprintf(`Analyze this PostgreSQL migration SQL for semantic/logical issues.

Focus on:
1. Logic errors (constraints that can't be satisfied, contradictory rules)
2. Data integrity issues (missing foreign keys, orphaned references)
3. Security issues (overly permissive policies, SQL injection risks)
4. Inconsistencies (conflicting naming, mismatched types)

SQL:
%s

Respond with ONLY valid JSON:
{
  "issues": [
    {
      "type": "logic_error|inconsistency|security|missing_constraint",
      "severity": "critical|warning|info",
      "description": "what's wrong",
      "location": "where in code",
      "reasoning": "why this is an issue",
      "confidence": 0.0-1.0,
      "suggestion": "how to fix"
    }
  ]
}

If no issues found, respond with: {"issues": []}`, chunk)

		req := &ai.AnalysisRequest{
			Type:        "semantic_validation",
			Content:     prompt,
			Temperature: 0.2, // Low temperature for consistent validation
			MaxTokens:   2000,
		}

		response, err := av.analyzer.GetManager().Analyze(ctx, req)
		if err != nil {
			return errors.Wrap(err, errors.ErrorCodeAnalysisError, errors.CategoryValidation, "AI semantic analysis failed", nil)
		}

		// Parse response
		issues, err := av.parseSemanticIssues(response.Result)
		if err != nil {
			av.logInfo("⚠️  Failed to parse semantic issues: %v", err)
			continue
		}

		// Filter by confidence threshold
		for _, issue := range issues {
			if issue.Confidence >= av.config.ConfidenceThreshold {
				result.SemanticIssues = append(result.SemanticIssues, issue)
			}
		}
	}

	av.logInfo("   Found %d semantic issues", len(result.SemanticIssues))
	return nil
}

// validateDependencies performs AI-powered dependency validation
func (av *AIValidator) validateDependencies(ctx context.Context, sql string, result *AIValidationResult) error {
	prompt := fmt.Sprintf(`Analyze this PostgreSQL migration SQL for dependency issues.

Focus on:
1. Circular dependencies between objects
2. Objects used before they're created
3. Incorrect dependency ordering
4. Missing dependencies

SQL (truncated for analysis):
%s

Respond with ONLY valid JSON:
{
  "dependencies": [
    {
      "type": "circular|missing|invalid_order",
      "description": "what's wrong",
      "objects": ["list", "of", "affected", "objects"],
      "confidence": 0.0-1.0,
      "suggestion": "how to fix"
    }
  ]
}

If no issues found, respond with: {"dependencies": []}`, av.truncateForPrompt(sql, 4000))

	req := &ai.AnalysisRequest{
		Type:        "dependency_validation",
		Content:     prompt,
		Temperature: 0.2,
		MaxTokens:   1500,
	}

	response, err := av.analyzer.GetManager().Analyze(ctx, req)
	if err != nil {
		return errors.Wrap(err, errors.ErrorCodeAnalysisError, errors.CategoryValidation, "AI dependency analysis failed", nil)
	}

	// Parse response
	issues, err := av.parseDependencyIssues(response.Result)
	if err != nil {
		av.logInfo("⚠️  Failed to parse dependency issues: %v", err)
		return err
	}

	// Filter by confidence threshold
	for _, issue := range issues {
		if issue.Confidence >= av.config.ConfidenceThreshold {
			result.DependencyIssues = append(result.DependencyIssues, issue)
		}
	}

	av.logInfo("   Found %d dependency issues", len(result.DependencyIssues))
	return nil
}

// generateQualityReport generates an AI-powered quality assessment
func (av *AIValidator) generateQualityReport(ctx context.Context, sql string, result *AIValidationResult) error {
	prompt := fmt.Sprintf(`Analyze this PostgreSQL migration SQL and provide a quality assessment.

Evaluate:
1. Overall code quality (1-10 score)
2. Maintainability (high/medium/low)
3. Complexity (1-10 score)
4. Best practices followed
5. Best practices violated

SQL (truncated):
%s

Respond with ONLY valid JSON:
{
  "overall_score": 1-10,
  "maintainability": "high|medium|low",
  "complexity": 1-10,
  "best_practices": ["practices followed"],
  "violations": ["practices violated"],
  "summary": "2-3 sentence overall assessment"
}`, av.truncateForPrompt(sql, 4000))

	req := &ai.AnalysisRequest{
		Type:        "quality_assessment",
		Content:     prompt,
		Temperature: 0.3,
		MaxTokens:   1500,
	}

	response, err := av.analyzer.GetManager().Analyze(ctx, req)
	if err != nil {
		return errors.Wrap(err, errors.ErrorCodeAnalysisError, errors.CategoryValidation, "AI quality assessment failed", nil)
	}

	// Parse response
	report, err := av.parseQualityReport(response.Result)
	if err != nil {
		av.logInfo("⚠️  Failed to parse quality report: %v", err)
		return err
	}

	result.QualityReport = report
	av.logInfo("   Quality score: %d/10 (maintainability: %s, complexity: %d/10)",
		report.OverallScore, report.Maintainability, report.Complexity)
	return nil
}

// suggestOptimizations gets AI-powered optimization suggestions
func (av *AIValidator) suggestOptimizations(ctx context.Context, sql string, result *AIValidationResult) error {
	// Use the analyzer's SuggestOptimizations method
	optimizationsResp, err := av.analyzer.SuggestOptimizations(ctx, av.truncateForPrompt(sql, 4000))
	if err != nil {
		return errors.Wrap(err, errors.ErrorCodeAnalysisError, errors.CategoryValidation, "failed to get optimizations", nil)
	}

	optimizations := optimizationsResp.Optimizations

	// LIMIT: Only show top 10 most important suggestions to avoid overwhelming users
	// The full list can be very long (100+ suggestions), but most are low-impact
	maxSuggestions := 10
	if len(optimizations) > maxSuggestions {
		av.logInfo("   Limiting suggestions to %d most important (found %d total)", maxSuggestions, len(optimizations))
		optimizations = optimizations[:maxSuggestions]
	}

	// Now we have structured data - convert to repair suggestions
	for _, opt := range optimizations {
		suggestion := RepairSuggestion{
			Issue:      fmt.Sprintf("[%s] %s", opt.Type, opt.Description),
			Fix:        opt.Suggestion,
			Impact:     opt.Impact,
			Confidence: parseConfidence(opt.Priority), // high=0.9, medium=0.7, low=0.5
			AutoApply:  false,                         // Never auto-apply optimizations
		}
		result.RepairSuggestions = append(result.RepairSuggestions, suggestion)
	}

	av.logInfo("   Found %d optimization suggestions (Overall score: %d/10)", len(optimizations), optimizationsResp.OverallScore)
	return nil
}

// parseConfidence converts priority string to confidence score
func parseConfidence(priority string) float64 {
	switch priority {
	case "high":
		return 0.9
	case "medium":
		return 0.7
	case "low":
		return 0.5
	default:
		return 0.7
	}
}

// Helper methods

func (av *AIValidator) chunkSQL(sql string, maxTokens int) []string {
	// Simple chunking by lines (approximate tokens)
	lines := strings.Split(sql, "\n")
	var chunks []string
	var currentChunk strings.Builder
	currentSize := 0

	for _, line := range lines {
		lineSize := len(line) / 4 // Rough token estimate
		if currentSize+lineSize > maxTokens && currentChunk.Len() > 0 {
			chunks = append(chunks, currentChunk.String())
			currentChunk.Reset()
			currentSize = 0
		}
		currentChunk.WriteString(line)
		currentChunk.WriteString("\n")
		currentSize += lineSize
	}

	if currentChunk.Len() > 0 {
		chunks = append(chunks, currentChunk.String())
	}

	return chunks
}

func (av *AIValidator) truncateForPrompt(sql string, maxLength int) string {
	if len(sql) <= maxLength {
		return sql
	}
	return sql[:maxLength] + "\n\n... (truncated for analysis)"
}

func (av *AIValidator) parseSemanticIssues(jsonResponse string) ([]SemanticIssue, error) {
	// Use the structured response parser
	type Response struct {
		Issues []SemanticIssue `json:"issues"`
	}

	response, err := ai.ParseStructuredResponse[Response](jsonResponse)
	if err != nil {
		return nil, err
	}

	return response.Issues, nil
}

func (av *AIValidator) parseDependencyIssues(jsonResponse string) ([]DependencyIssue, error) {
	type Response struct {
		Dependencies []DependencyIssue `json:"dependencies"`
	}

	response, err := ai.ParseStructuredResponse[Response](jsonResponse)
	if err != nil {
		return nil, err
	}

	return response.Dependencies, nil
}

func (av *AIValidator) parseQualityReport(jsonResponse string) (*QualityReport, error) {
	report, err := ai.ParseStructuredResponse[QualityReport](jsonResponse)
	if err != nil {
		return nil, err
	}

	return report, nil
}

func (av *AIValidator) evaluateOverallSuccess(result *AIValidationResult) bool {
	// Check for critical issues
	for _, issue := range result.SemanticIssues {
		if issue.Severity == "critical" {
			return false
		}
	}

	// Check for high-confidence dependency issues
	for _, issue := range result.DependencyIssues {
		if issue.Type == "circular" && issue.Confidence > 0.9 {
			return false
		}
	}

	// Check quality report
	if result.QualityReport != nil && result.QualityReport.OverallScore < 4 {
		return false
	}

	return true
}

func (av *AIValidator) printSummary(result *AIValidationResult) {
	color.Cyan("\n═══════════════════════════════════════════════════════")
	color.Cyan("             AI VALIDATION SUMMARY")
	color.Cyan("═══════════════════════════════════════════════════════\n")

	if result.Success {
		color.Green("☑ Overall Result: PASSED\n")
	} else {
		color.Red("☒ Overall Result: FAILED\n")
	}

	// Semantic issues
	if len(result.SemanticIssues) > 0 {
		color.Yellow("\n🔍 Semantic Issues (%d):\n", len(result.SemanticIssues))
		for i, issue := range result.SemanticIssues {
			severityColor := color.New(color.FgYellow)
			if issue.Severity == "critical" {
				severityColor = color.New(color.FgRed)
			}
			_, _ = severityColor.Printf("  %d. [%s] %s\n", i+1, strings.ToUpper(issue.Severity), issue.Description)
			if issue.Suggestion != "" {
				color.Cyan("     💡 Suggestion: %s\n", issue.Suggestion)
			}
		}
	}

	// Dependency issues
	if len(result.DependencyIssues) > 0 {
		color.Yellow("\n🔗 Dependency Issues (%d):\n", len(result.DependencyIssues))
		for i, issue := range result.DependencyIssues {
			color.Yellow("  %d. [%s] %s\n", i+1, issue.Type, issue.Description)
			if issue.Suggestion != "" {
				color.Cyan("     💡 Suggestion: %s\n", issue.Suggestion)
			}
		}
	}

	// Quality report
	if result.QualityReport != nil {
		color.Cyan("\n📊 Quality Report:\n")
		scoreColor := color.New(color.FgGreen)
		if result.QualityReport.OverallScore < 7 {
			scoreColor = color.New(color.FgYellow)
		}
		if result.QualityReport.OverallScore < 4 {
			scoreColor = color.New(color.FgRed)
		}
		_, _ = scoreColor.Printf("  Overall Score: %d/10\n", result.QualityReport.OverallScore)
		color.Cyan("  Maintainability: %s\n", result.QualityReport.Maintainability)
		color.Cyan("  Complexity: %d/10\n", result.QualityReport.Complexity)
		if result.QualityReport.Summary != "" {
			color.Cyan("  Summary: %s\n", result.QualityReport.Summary)
		}
	}

	// Repair suggestions
	if len(result.RepairSuggestions) > 0 {
		color.Cyan("\n💡 Optimization Suggestions (%d):\n", len(result.RepairSuggestions))
		for i, suggestion := range result.RepairSuggestions {
			if i < 5 { // Show only first 5
				color.Cyan("  %d. %s\n", i+1, suggestion.Fix)
			}
		}
		if len(result.RepairSuggestions) > 5 {
			color.Cyan("  ... and %d more suggestions\n", len(result.RepairSuggestions)-5)
		}
	}

	color.Cyan("\n═══════════════════════════════════════════════════════\n")
}

func (av *AIValidator) logInfo(format string, args ...interface{}) {
	if av.verbose {
		color.Cyan("[AI-VALIDATOR] "+format+"\n", args...)
	}
}
