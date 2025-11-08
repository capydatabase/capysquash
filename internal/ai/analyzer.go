package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/ai/providers"
	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// Analyzer provides AI-powered analysis using the modular provider system
type Analyzer struct {
	manager *ProviderManager
}

// NewAnalyzer creates a new analyzer with the modular provider system
func NewAnalyzer() (*Analyzer, error) {
	manager, err := NewProviderManager(nil) // Use default config
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create provider manager",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("ensure AI provider API keys are configured")
	}

	return &Analyzer{
		manager: manager,
	}, nil
}

// AreFunctionsSemanticallyEquivalent checks if two functions are semantically equivalent
// Returns: equivalent (bool), confidence (0.0-1.0), error
func (a *Analyzer) AreFunctionsSemanticallyEquivalent(ctx context.Context, func1, func2 string) (bool, float64, error) {
	if a.manager == nil {
		return false, 0.0, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"AI analyzer not initialized - no providers available",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check that AI provider API keys are configured")
	}

	content := func1 + "|||" + func2 // Use separator for function pairs

	req := &AnalysisRequest{
		Type:        providers.AnalysisFunctionEquivalence,
		Content:     content,
		Temperature: 0.1, // Low temperature for consistent results
		MaxTokens:   1000,
	}

	response, err := a.manager.Analyze(ctx, req)
	if err != nil {
		return false, 0.0, err
	}

	// Try to parse as structured response first
	structured, err := ParseStructuredResponse[FunctionEquivalenceResponse](response.Result)
	if err == nil {
		return structured.Equivalent, structured.Confidence, nil
	}

	// Fallback to plain text parsing for backward compatibility
	result := strings.TrimSpace(strings.ToLower(response.Result))
	if result == "true" {
		return true, 0.8, nil // Assume moderate confidence for plain text responses
	}
	return false, 0.8, nil
}

// IsDeadCode determines if a function is unused/dead code
// Returns: isDeadCode (bool), confidence (0.0-1.0), error
func (a *Analyzer) IsDeadCode(ctx context.Context, schema, functionName string) (bool, float64, error) {
	if a.manager == nil {
		return false, 0.0, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"AI analyzer not initialized - no providers available",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check that AI provider API keys are configured")
	}

	req := &AnalysisRequest{
		Type:        providers.AnalysisDeadCode,
		Content:     fmt.Sprintf("Function: %s\n\nSchema:\n%s", functionName, schema),
		Temperature: 0.1,
		MaxTokens:   1000,
	}

	response, err := a.manager.Analyze(ctx, req)
	if err != nil {
		return false, 0.0, err
	}

	// Try to parse as structured response first
	structured, err := ParseStructuredResponse[DeadCodeResponse](response.Result)
	if err == nil {
		return structured.IsDeadCode, structured.Confidence, nil
	}

	// Fallback to plain text parsing
	result := strings.TrimSpace(strings.ToLower(response.Result))
	if result == "true" {
		return true, 0.8, nil
	}
	return false, 0.8, nil
}

// AnalyzeFunctionComplexity analyzes the complexity of a PostgreSQL function
// Returns structured complexity analysis with score, maintainability, and recommendations
func (a *Analyzer) AnalyzeFunctionComplexity(ctx context.Context, functionSQL string) (*FunctionComplexityResponse, error) {
	if a.manager == nil {
		return nil, errors.NewError(
			errors.ErrorCodeAnalysisError,
			"AI analyzer not initialized - no providers available",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check that AI provider API keys are configured")
	}

	req := &AnalysisRequest{
		Type:        providers.AnalysisFunctionComplexity,
		Content:     functionSQL,
		Temperature: 0.1,
		MaxTokens:   2000,
	}

	response, err := a.manager.Analyze(ctx, req)
	if err != nil {
		return nil, err
	}

	// Try to parse as structured response
	structured, err := ParseStructuredResponse[FunctionComplexityResponse](response.Result)
	if err == nil {
		return structured, nil
	}

	// Fallback: return basic response from plain text
	return &FunctionComplexityResponse{
		ComplexityScore:  5, // Default moderate complexity
		Maintainability:  "medium",
		PerformanceIssues: []string{},
		Recommendations:  []string{response.Result},
		Reasoning:        response.Result,
	}, nil
}

// DetectAuthPatterns identifies authentication/authorization patterns in SQL
// Returns structured auth pattern analysis with types and descriptions
func (a *Analyzer) DetectAuthPatterns(ctx context.Context, sqlContent string) (*AuthPatternsResponse, error) {
	if a.manager == nil {
		return nil, errors.NewError(
			errors.ErrorCodeAnalysisError,
			"AI analyzer not initialized - no providers available",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check that AI provider API keys are configured")
	}

	req := &AnalysisRequest{
		Type:        providers.AnalysisAuthPatterns,
		Content:     sqlContent,
		Temperature: 0.1,
		MaxTokens:   2000,
	}

	response, err := a.manager.Analyze(ctx, req)
	if err != nil {
		return nil, err
	}

	// Try to parse as structured response
	structured, err := ParseStructuredResponse[AuthPatternsResponse](response.Result)
	if err == nil {
		return structured, nil
	}

	// Fallback: parse plain text line by line
	if strings.TrimSpace(strings.ToUpper(response.Result)) == "NONE" {
		return &AuthPatternsResponse{
			Patterns: []AuthPattern{},
			Summary:  "No authentication patterns detected",
		}, nil
	}

	lines := strings.Split(response.Result, "\n")
	var patterns []AuthPattern
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			patterns = append(patterns, AuthPattern{
				Type:        "unknown",
				Description: line,
			})
		}
	}

	return &AuthPatternsResponse{
		Patterns: patterns,
		Summary:  fmt.Sprintf("Detected %d authentication patterns", len(patterns)),
	}, nil
}

// SuggestOptimizations suggests performance and maintainability improvements
// Returns structured optimization suggestions with priorities and impact assessment
func (a *Analyzer) SuggestOptimizations(ctx context.Context, migrationSQL string) (*OptimizationsResponse, error) {
	if a.manager == nil {
		return nil, errors.NewError(
			errors.ErrorCodeAnalysisError,
			"AI analyzer not initialized - no providers available",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check that AI provider API keys are configured")
	}

	req := &AnalysisRequest{
		Type:        providers.AnalysisOptimizations,
		Content:     migrationSQL,
		Temperature: 0.1,
		MaxTokens:   2000,
	}

	response, err := a.manager.Analyze(ctx, req)
	if err != nil {
		return nil, err
	}

	// Try to parse as structured response
	structured, err := ParseStructuredResponse[OptimizationsResponse](response.Result)
	if err == nil {
		return structured, nil
	}

	// Fallback: parse plain text line by line
	if strings.TrimSpace(strings.ToUpper(response.Result)) == "NONE" {
		return &OptimizationsResponse{
			Optimizations: []Optimization{},
			OverallScore:  10,
			Summary:       "No optimizations needed",
		}, nil
	}

	lines := strings.Split(response.Result, "\n")
	var optimizations []Optimization
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			optimizations = append(optimizations, Optimization{
				Type:        "maintainability",
				Priority:    "medium",
				Description: line,
				Impact:      "moderate",
				Suggestion:  line,
			})
		}
	}

	return &OptimizationsResponse{
		Optimizations: optimizations,
		OverallScore:  7,
		Summary:       fmt.Sprintf("Found %d optimization opportunities", len(optimizations)),
	}, nil
}

// AnalyzeCodeCoverage analyzes function usage and determines if code is dead
func (a *Analyzer) AnalyzeCodeCoverage(ctx context.Context, functionSQL, usageContext string) (string, error) {
	if a.manager == nil {
		return "", errors.NewError(
			errors.ErrorCodeAnalysisError,
			"AI analyzer not initialized - no providers available",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check that AI provider API keys are configured")
	}

	req := &AnalysisRequest{
		Type:        providers.AnalysisCodeCoverage,
		Content:     functionSQL,
		Context:     usageContext,
		Temperature: 0.1,
		MaxTokens:   1500,
	}

	response, err := a.manager.Analyze(ctx, req)
	if err != nil {
		return "", err
	}

	return response.Result, nil
}

// ValidateSchemaConsistency compares original vs squashed schemas
// Returns structured schema consistency analysis with categorized differences
func (a *Analyzer) ValidateSchemaConsistency(ctx context.Context, originalSchema, squashedSchema string) (*SchemaConsistencyResponse, error) {
	if a.manager == nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"AI analyzer not initialized - no providers available",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check that AI provider API keys are configured")
	}

	content := fmt.Sprintf("ORIGINAL SCHEMA:\n%s\n\nSQUASHED SCHEMA:\n%s", originalSchema, squashedSchema)

	req := &AnalysisRequest{
		Type:        providers.AnalysisSchemaConsistency,
		Content:     content,
		Temperature: 0.1,
		MaxTokens:   3000,
	}

	response, err := a.manager.Analyze(ctx, req)
	if err != nil {
		return nil, err
	}

	// Try to parse as structured response
	structured, err := ParseStructuredResponse[SchemaConsistencyResponse](response.Result)
	if err == nil {
		return structured, nil
	}

	// Fallback: parse plain text
	if strings.TrimSpace(strings.ToUpper(response.Result)) == "IDENTICAL" {
		return &SchemaConsistencyResponse{
			IsConsistent: true,
			Differences:  []SchemaDifference{},
			Summary:      "Schemas are identical",
		}, nil
	}

	lines := strings.Split(response.Result, "\n")
	var differences []SchemaDifference
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			differences = append(differences, SchemaDifference{
				Type:        "modified",
				ObjectType:  "unknown",
				ObjectName:  "",
				Description: line,
				Severity:    "warning",
			})
		}
	}

	return &SchemaConsistencyResponse{
		IsConsistent: len(differences) == 0,
		Differences:  differences,
		Summary:      fmt.Sprintf("Found %d schema differences", len(differences)),
	}, nil
}

// AnalyzeSQLComplexity analyzes the complexity of SQL statements
// Returns structured SQL complexity analysis with score and best practices
func (a *Analyzer) AnalyzeSQLComplexity(ctx context.Context, sqlStatement string) (*SQLComplexityResponse, error) {
	if a.manager == nil {
		return nil, errors.NewError(
			errors.ErrorCodeAnalysisError,
			"AI analyzer not initialized - no providers available",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check that AI provider API keys are configured")
	}

	req := &AnalysisRequest{
		Type:        providers.AnalysisSQLComplexity,
		Content:     sqlStatement,
		Temperature: 0.1,
		MaxTokens:   2000,
	}

	response, err := a.manager.Analyze(ctx, req)
	if err != nil {
		return nil, err
	}

	// Try to parse as structured response
	structured, err := ParseStructuredResponse[SQLComplexityResponse](response.Result)
	if err == nil {
		return structured, nil
	}

	// Fallback: return basic response from plain text
	return &SQLComplexityResponse{
		ComplexityScore: 5,
		Issues:          []string{},
		BestPractices:   []string{},
		Reasoning:       response.Result,
	}, nil
}

// AnalyzeWithTools performs analysis using tool-enhanced capabilities
func (a *Analyzer) AnalyzeWithTools(ctx context.Context, req *AnalysisRequest, tools []*ToolDefinition) (*AnalysisResponse, error) {
	if a.manager == nil {
		return nil, errors.NewError(
			errors.ErrorCodeAnalysisError,
			"AI analyzer not initialized - no providers available",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check that AI provider API keys are configured")
	}

	return a.manager.AnalyzeWithTools(ctx, req, tools)
}

// SubmitBatch submits a batch of analysis requests
func (a *Analyzer) SubmitBatch(ctx context.Context, batch *BatchRequest) (*BatchResponse, error) {
	if a.manager == nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"AI analyzer not initialized - no providers available",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check that AI provider API keys are configured")
	}

	return a.manager.SubmitBatch(ctx, batch)
}

// GetBatchStatus retrieves the status of a batch analysis
func (a *Analyzer) GetBatchStatus(ctx context.Context, batchID string) (*BatchResponse, error) {
	if a.manager == nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"AI analyzer not initialized - no providers available",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check that AI provider API keys are configured")
	}

	return a.manager.GetBatchStatus(ctx, batchID)
}

// HealthCheck performs health check on all providers
func (a *Analyzer) HealthCheck(ctx context.Context) map[ProviderType]error {
	if a.manager == nil {
		return map[ProviderType]error{
			"unknown": errors.NewError(
				errors.ErrorCodeValidationFailed,
				"AI analyzer not initialized",
				errors.SeverityError,
				errors.CategoryValidation,
			),
		}
	}

	return a.manager.HealthCheck(ctx)
}

// GetCapabilities returns capabilities of all available providers
func (a *Analyzer) GetCapabilities() map[ProviderType]providers.ProviderCapabilities {
	if a.manager == nil {
		return make(map[ProviderType]providers.ProviderCapabilities)
	}

	return a.manager.GetCapabilities()
}

// GetAvailableProviders returns list of available providers
func (a *Analyzer) GetAvailableProviders() []ProviderType {
	if a.manager == nil {
		return []ProviderType{}
	}

	return a.manager.ListProviders()
}

// GetProvider returns a specific provider for advanced usage
func (a *Analyzer) GetProvider(providerType ProviderType) (Provider, error) {
	if a.manager == nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"AI analyzer not initialized",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check that AI provider API keys are configured")
	}

	return a.manager.GetProvider(providerType)
}

// GetManager returns the underlying provider manager for advanced usage
func (a *Analyzer) GetManager() *ProviderManager {
	return a.manager
}
