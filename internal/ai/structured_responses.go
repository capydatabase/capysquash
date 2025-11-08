package ai

import (
	"encoding/json"
	"fmt"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// FunctionEquivalenceResponse represents structured AI response for function equivalence
type FunctionEquivalenceResponse struct {
	Equivalent bool    `json:"equivalent"`
	Reasoning  string  `json:"reasoning"`
	Confidence float64 `json:"confidence"`
	Differences []string `json:"differences,omitempty"` // If not equivalent, what differs
}

// DeadCodeResponse represents structured AI response for dead code detection
type DeadCodeResponse struct {
	IsDeadCode bool    `json:"is_dead_code"`
	Reasoning  string  `json:"reasoning"`
	Confidence float64 `json:"confidence"`
	UsedBy     []string `json:"used_by,omitempty"` // Where the code is referenced
}

// FunctionComplexityResponse represents structured AI response for complexity analysis
type FunctionComplexityResponse struct {
	ComplexityScore  int      `json:"complexity_score"` // 1-10 scale
	Maintainability  string   `json:"maintainability"`  // "high", "medium", "low"
	PerformanceIssues []string `json:"performance_issues"`
	Recommendations  []string `json:"recommendations"`
	Reasoning        string   `json:"reasoning"`
}

// AuthPattern represents a single authentication pattern
type AuthPattern struct {
	Type        string `json:"type"`        // "jwt", "rls", "supabase", "clerk", "custom"
	Description string `json:"description"`
	Location    string `json:"location,omitempty"` // Where in the code
}

// AuthPatternsResponse represents structured AI response for auth pattern detection
type AuthPatternsResponse struct {
	Patterns []AuthPattern `json:"patterns"`
	Summary  string        `json:"summary"`
}

// Optimization represents a single optimization suggestion
type Optimization struct {
	Type        string `json:"type"`        // "performance", "maintainability", "security"
	Priority    string `json:"priority"`    // "high", "medium", "low"
	Description string `json:"description"`
	Impact      string `json:"impact"`
	Suggestion  string `json:"suggestion"`
}

// OptimizationsResponse represents structured AI response for optimization suggestions
type OptimizationsResponse struct {
	Optimizations []Optimization `json:"optimizations"`
	OverallScore  int            `json:"overall_score"` // 1-10 quality score
	Summary       string         `json:"summary"`
}

// SchemaDifference represents a single schema difference
type SchemaDifference struct {
	Type        string `json:"type"`        // "missing", "extra", "modified"
	ObjectType  string `json:"object_type"` // "table", "function", "trigger", etc.
	ObjectName  string `json:"object_name"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // "critical", "warning", "info"
}

// SchemaConsistencyResponse represents structured AI response for schema validation
type SchemaConsistencyResponse struct {
	IsConsistent bool               `json:"is_consistent"`
	Differences  []SchemaDifference `json:"differences"`
	Summary      string             `json:"summary"`
}

// SQLComplexityResponse represents structured AI response for SQL complexity analysis
type SQLComplexityResponse struct {
	ComplexityScore int      `json:"complexity_score"` // 1-10 scale
	Issues          []string `json:"issues"`
	BestPractices   []string `json:"best_practices"`
	Reasoning       string   `json:"reasoning"`
}

// ParseStructuredResponse parses a JSON response string into the target type
func ParseStructuredResponse[T any](response string) (*T, error) {
	var result T

	// Try to extract JSON from markdown code blocks if present
	jsonStr := extractJSONFromMarkdown(response)

	err := json.Unmarshal([]byte(jsonStr), &result)
	if err != nil {
		return nil, errors.Wrap(err, errors.ErrorCodeAnalysisError, errors.CategoryValidation, fmt.Sprintf("failed to parse JSON response\nRaw response: %s", response), nil)
	}

	return &result, nil
}

// extractJSONFromMarkdown extracts JSON from markdown code blocks
func extractJSONFromMarkdown(content string) string {
	// Check if content is wrapped in markdown code blocks
	if len(content) > 8 && content[:3] == "```" {
		// Find the end of the opening code fence
		start := 3
		for start < len(content) && content[start] != '\n' {
			start++
		}
		start++ // Skip the newline

		// Find the closing code fence
		end := len(content) - 3
		for end > start && content[end-3:end] != "```" {
			end--
		}

		if end > start {
			return content[start:end-3]
		}
	}

	return content
}

// GetStructuredPromptSuffix returns the suffix to append to prompts for JSON responses
func GetStructuredPromptSuffix(schema string) string {
	return fmt.Sprintf(`

IMPORTANT: Respond with ONLY valid JSON matching this schema:
%s

Do not include any markdown code blocks, explanations, or additional text.
Just the raw JSON object.`, schema)
}

// Schema definitions for prompts
const (
	FunctionEquivalenceSchema = `{
  "equivalent": boolean,
  "reasoning": "explanation",
  "confidence": 0.0-1.0,
  "differences": ["list of differences if not equivalent"]
}`

	DeadCodeSchema = `{
  "is_dead_code": boolean,
  "reasoning": "explanation",
  "confidence": 0.0-1.0,
  "used_by": ["list of references if not dead"]
}`

	FunctionComplexitySchema = `{
  "complexity_score": 1-10,
  "maintainability": "high|medium|low",
  "performance_issues": ["list of issues"],
  "recommendations": ["list of recommendations"],
  "reasoning": "explanation"
}`

	AuthPatternsSchema = `{
  "patterns": [
    {
      "type": "jwt|rls|supabase|clerk|custom",
      "description": "pattern description",
      "location": "where in code"
    }
  ],
  "summary": "overall summary"
}`

	OptimizationsSchema = `{
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
}`

	SchemaConsistencySchema = `{
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
}`

	SQLComplexitySchema = `{
  "complexity_score": 1-10,
  "issues": ["list of complexity issues"],
  "best_practices": ["violated best practices"],
  "reasoning": "explanation"
}`
)
