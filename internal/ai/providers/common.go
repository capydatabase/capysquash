package providers

import (
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// GetSystemPromptForType returns the system prompt for a given analysis type
// Shared by all AI providers for consistent behavior
func GetSystemPromptForType(analysisType AnalysisType) string {
	basePrompt := "You are a PostgreSQL expert specializing in database optimization, migration analysis, and SQL best practices. Provide precise, accurate responses."

	switch analysisType {
	case AnalysisFunctionEquivalence:
		return basePrompt + " Focus on semantic equivalence of PostgreSQL functions, considering inputs, outputs, and side effects."
	case AnalysisDeadCode:
		return basePrompt + " Analyze PostgreSQL schemas to identify unused functions, triggers, and database objects."
	case AnalysisFunctionComplexity:
		return basePrompt + " Evaluate PostgreSQL function complexity, maintainability, and performance characteristics."
	case AnalysisAuthPatterns:
		return basePrompt + " Identify authentication and authorization patterns in PostgreSQL schemas, including RLS, JWT, and third-party integrations."
	case AnalysisOptimizations:
		return basePrompt + " Suggest performance and maintainability optimizations for PostgreSQL migrations and schemas."
	case AnalysisSQLComplexity:
		return basePrompt + " Analyze SQL statement complexity, performance implications, and best practice adherence."
	case AnalysisCodeCoverage:
		return basePrompt + " Analyze test coverage and suggest improvements for database testing strategies."
	case AnalysisSchemaConsistency:
		return basePrompt + " Verify schema consistency, naming conventions, and design patterns across migrations."
	default:
		return basePrompt
	}
}

// BuildUserPrompt constructs the user prompt for a given analysis request
// Shared by all AI providers for consistent behavior
// Enhanced to request structured JSON responses
func BuildUserPrompt(req *AnalysisRequest) string {
	switch req.Type {
	case AnalysisFunctionEquivalence:
		parts := strings.Split(req.Content, "|||")
		if len(parts) == 2 {
			return fmt.Sprintf(`Analyze these two PostgreSQL functions and determine if they are semantically equivalent.

Two functions are semantically equivalent if they:
1. Have the same input parameters (names can differ, but types and order must match)
2. Produce identical outputs for all possible inputs
3. Have the same side effects (or lack thereof)
4. Handle edge cases identically

Function 1:
%s

Function 2:
%s

Respond with ONLY valid JSON matching this schema:
{
  "equivalent": boolean,
  "reasoning": "explanation of why they are/aren't equivalent",
  "confidence": 0.0-1.0,
  "differences": ["list any differences if not equivalent"]
}

No markdown code blocks, just the raw JSON.`, parts[0], parts[1])
		}
		return req.Content

	case AnalysisDeadCode:
		return fmt.Sprintf(`Analyze this PostgreSQL schema and determine if the specified function is dead code.

A function is considered dead code if:
1. It's not called by any other functions in the schema
2. It's not referenced in any triggers
3. It's not used in any policies or constraints
4. It's not referenced in any views
5. It appears to be unused application entry point

Schema and Analysis Request:
%s

Context (if provided):
%s

Respond with ONLY valid JSON matching this schema:
{
  "is_dead_code": boolean,
  "reasoning": "explanation",
  "confidence": 0.0-1.0,
  "used_by": ["list of references if not dead"]
}

No markdown code blocks, just the raw JSON.`, req.Content, req.Context)

	case AnalysisAuthPatterns:
		return fmt.Sprintf(`Analyze this SQL content and identify authentication/authorization patterns.

Look for:
1. JWT token processing (auth.jwt(), claims extraction)
2. RLS (Row Level Security) policies
3. User/role-based access patterns
4. Supabase auth patterns
5. Clerk auth patterns
6. Custom authentication schemes

SQL Content:
%s

Respond with ONLY valid JSON matching this schema:
{
  "patterns": [
    {
      "type": "jwt|rls|supabase|clerk|custom",
      "description": "pattern description",
      "location": "where in code"
    }
  ],
  "summary": "overall summary"
}

No markdown code blocks, just the raw JSON.`, req.Content)

	case AnalysisFunctionComplexity:
		return fmt.Sprintf(`Analyze this PostgreSQL function and assess its complexity.

Consider:
1. Cyclomatic complexity (branching logic)
2. Number of SQL operations
3. Use of nested queries
4. Error handling completeness
5. Maintainability score

Function:
%s

Respond with ONLY valid JSON matching this schema:
{
  "complexity_score": 1-10,
  "maintainability": "high|medium|low",
  "performance_issues": ["list of issues"],
  "recommendations": ["list of recommendations"],
  "reasoning": "explanation"
}

No markdown code blocks, just the raw JSON.`, req.Content)

	case AnalysisOptimizations:
		return fmt.Sprintf(`Analyze this PostgreSQL migration and suggest optimizations.

Focus on:
1. Index usage and missing indexes
2. Query performance improvements
3. Schema design patterns
4. Data type efficiency
5. Constraint optimization

Migration:
%s

Respond with ONLY valid JSON matching this schema:
{
  "optimizations": [
    {
      "type": "performance|maintainability|security",
      "priority": "high|medium|low",
      "description": "what to optimize",
      "impact": "expected impact",
      "suggestion": "how to fix"
    }
  ],
  "overall_score": 1-10,
  "summary": "overall assessment"
}

No markdown code blocks, just the raw JSON.`, req.Content)

	case AnalysisSQLComplexity:
		return fmt.Sprintf(`Analyze this SQL statement for complexity and performance.

Evaluate:
1. Query structure and nesting level
2. Join complexity
3. Subquery usage
4. Potential performance bottlenecks
5. Best practice adherence

SQL:
%s

Respond with ONLY valid JSON matching this schema:
{
  "complexity_score": 1-10,
  "issues": ["list of complexity issues"],
  "best_practices": ["violated best practices"],
  "reasoning": "explanation"
}

No markdown code blocks, just the raw JSON.`, req.Content)

	case AnalysisSchemaConsistency:
		return fmt.Sprintf(`Analyze schema consistency across migrations.

Check for:
1. Naming convention consistency
2. Column type consistency for similar fields
3. Constraint naming patterns
4. Foreign key relationship patterns
5. Index naming conventions

Schema Context:
%s

Respond with ONLY valid JSON matching this schema:
{
  "is_consistent": boolean,
  "differences": [
    {
      "type": "missing|extra|modified",
      "object_type": "table|function|trigger|etc",
      "object_name": "name",
      "description": "what differs",
      "severity": "critical|warning|info"
    }
  ],
  "summary": "overall summary"
}

No markdown code blocks, just the raw JSON.`, req.Content)

	default:
		prompt := req.Content
		if req.Context != "" {
			prompt += "\n\nContext:\n" + req.Context
		}
		return prompt
	}
}

// CalculateConfidence calculates confidence score for an analysis result
// Shared by all AI providers for consistent scoring
func CalculateConfidence(analysisType AnalysisType, result string) float64 {
	// Simple confidence calculation based on result characteristics
	result = strings.TrimSpace(strings.ToLower(result))

	switch analysisType {
	case AnalysisFunctionEquivalence, AnalysisDeadCode:
		// For boolean responses, high confidence if it's a clear true/false
		if result == "true" || result == "false" {
			return 0.95
		}
		// If the result contains explanation along with boolean
		if strings.Contains(result, "true") || strings.Contains(result, "false") {
			return 0.85
		}
		return 0.7

	case AnalysisFunctionComplexity, AnalysisSQLComplexity:
		// For complexity analysis, check for score/numeric indicators
		if strings.Contains(result, "score") || strings.Contains(result, "complexity") {
			return 0.90
		}
		return 0.75

	case AnalysisAuthPatterns:
		// For auth patterns, confidence based on specificity
		if result == "none" {
			return 0.95 // High confidence in absence
		}
		if strings.Contains(result, "\n") && len(result) > 50 {
			return 0.85 // Multiple patterns detected
		}
		return 0.75

	default:
		// For other types, base confidence on result length and structure
		if len(result) > 100 && strings.Contains(result, "\n") {
			return 0.85 // Detailed, structured response
		}
		if len(result) > 50 {
			return 0.80 // Moderate detail
		}
		return 0.75 // Basic response
	}
}

// ValidateAnalysisRequest validates common analysis request parameters
func ValidateAnalysisRequest(req *AnalysisRequest) error {
	if req == nil {
		return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation, "analysis request cannot be nil", nil)
	}

	if req.Content == "" {
		return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation, "analysis content cannot be empty", nil)
	}

	if req.MaxTokens < 0 {
		return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation, "max tokens must be non-negative", nil)
	}

	if req.Temperature < 0 || req.Temperature > 2 {
		return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation, "temperature must be between 0 and 2", nil)
	}

	return nil
}
