package providers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// ClaudeProvider implements the AI provider interface for Anthropic's Claude
type ClaudeProvider struct {
	client       *anthropic.Client
	config       *ProviderConfig
	model        anthropic.Model
	tools        []*ToolDefinition
	capabilities ProviderCapabilities
}

// NewClaudeProvider creates a new Claude provider instance
func NewClaudeProvider(config *ProviderConfig) (*ClaudeProvider, error) {
	if config.APIKey == "" {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"claude API key is required",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("set ANTHROPIC_API_KEY environment variable")
	}

	client := anthropic.NewClient(option.WithAPIKey(config.APIKey))

	// Default to Claude Sonnet 4.5 if no model specified
	model := anthropic.ModelClaudeSonnet4_5
	if config.Model != "" {
		model = anthropic.Model(config.Model)
	}

	provider := &ClaudeProvider{
		client: &client,
		config: config,
		model:  model,
		tools:  make([]*ToolDefinition, 0),
		capabilities: ProviderCapabilities{
			SupportedTypes: []AnalysisType{
				AnalysisFunctionEquivalence,
				AnalysisDeadCode,
				AnalysisFunctionComplexity,
				AnalysisAuthPatterns,
				AnalysisOptimizations,
				AnalysisCodeCoverage,
				AnalysisSchemaConsistency,
				AnalysisSQLComplexity,
				AnalysisBatchProcessing,
			},
			SupportsStreaming: true,
			SupportsTools:     true,
			SupportsBatch:     true,
			MaxTokens:         200000, // Claude 3.5 context window
			MaxBatchSize:      100000, // Claude batch processing limit
		},
	}

	return provider, nil
}

// Provider interface implementation

func (c *ClaudeProvider) Name() string {
	return "Claude"
}

func (c *ClaudeProvider) Type() ProviderType {
	return ProviderClaude
}

func (c *ClaudeProvider) SupportedTypes() []AnalysisType {
	return c.capabilities.SupportedTypes
}

func (c *ClaudeProvider) Configure(config *ProviderConfig) error {
	if config.APIKey != "" {
		client := anthropic.NewClient(option.WithAPIKey(config.APIKey))
		c.client = &client
	}

	if config.Model != "" {
		c.model = anthropic.Model(config.Model)
	}

	c.config = config
	return nil
}

func (c *ClaudeProvider) HealthCheck(ctx context.Context) error {
	// Simple health check by making a minimal API call
	_, err := c.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: 10,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("test")),
		},
	})
	return err
}

func (c *ClaudeProvider) Analyze(ctx context.Context, req *AnalysisRequest) (*AnalysisResponse, error) {
	startTime := time.Now()

	systemPrompt := GetSystemPromptForType(req.Type)
	userPrompt := BuildUserPrompt(req)

	maxTokens := 4000
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	// Note: Temperature not implemented in current Claude SDK version

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
	}

	// Add system prompt as first user message if provided
	if systemPrompt != "" {
		messages = []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("System: " + systemPrompt + "\n\nUser: " + userPrompt)),
		}
	}

	params := anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: int64(maxTokens),
		Messages:  messages,
	}

	response, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"claude API call failed",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("check ANTHROPIC_API_KEY and rate limits")
	}

	result := c.extractTextFromResponse(response)

	return &AnalysisResponse{
		Result:     result,
		Confidence: c.calculateConfidence(req.Type, result),
		Metadata: map[string]interface{}{
			"model":         string(c.model),
			"input_tokens":  response.Usage.InputTokens,
			"output_tokens": response.Usage.OutputTokens,
		},
		TokensUsed: int(response.Usage.InputTokens + response.Usage.OutputTokens),
		Duration:   time.Since(startTime),
		ProviderID: "claude",
	}, nil
}

func (c *ClaudeProvider) SubmitBatch(ctx context.Context, batch *BatchRequest) (*BatchResponse, error) {
	// Claude batch processing implementation
	// Note: This would use Claude's batch API when available
	return nil, errors.NewError(
		errors.ErrorCodeValidationFailed,
		"batch processing not yet implemented for Claude",
		errors.SeverityError,
		errors.CategoryValidation,
	).WithSuggestion("batch processing will be available in a future version")
}

func (c *ClaudeProvider) GetBatchStatus(ctx context.Context, batchID string) (*BatchResponse, error) {
	return nil, errors.NewError(
		errors.ErrorCodeValidationFailed,
		"batch status not yet implemented for Claude",
		errors.SeverityError,
		errors.CategoryValidation,
	).WithSuggestion("batch processing will be available in a future version")
}

func (c *ClaudeProvider) SupportsStreaming() bool {
	return c.capabilities.SupportsStreaming
}

func (c *ClaudeProvider) SupportsTools() bool {
	return c.capabilities.SupportsTools
}

func (c *ClaudeProvider) SupportsBatch() bool {
	return c.capabilities.SupportsBatch
}

// ToolUseProvider interface implementation

func (c *ClaudeProvider) AnalyzeWithTools(ctx context.Context, req *AnalysisRequest, tools []*ToolDefinition) (*AnalysisResponse, error) {
	startTime := time.Now()

	systemPrompt := GetSystemPromptForType(req.Type)
	userPrompt := BuildUserPrompt(req)

	maxTokens := 4000
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	// Note: Temperature not implemented in current Claude SDK version

	messages := []anthropic.MessageParam{
		anthropic.NewUserMessage(anthropic.NewTextBlock(userPrompt)),
	}

	// Add system prompt as first user message if provided
	if systemPrompt != "" {
		messages = []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("System: " + systemPrompt + "\n\nUser: " + userPrompt)),
		}
	}

	params := anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: int64(maxTokens),
		Messages:  messages,
	}

	response, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"claude API call with tools failed",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("check ANTHROPIC_API_KEY and rate limits")
	}

	result := c.extractTextFromResponse(response)

	return &AnalysisResponse{
		Result:     result,
		Confidence: c.calculateConfidence(req.Type, result),
		Metadata: map[string]interface{}{
			"model":         string(c.model),
			"input_tokens":  response.Usage.InputTokens,
			"output_tokens": response.Usage.OutputTokens,
			"tools_used":    len(tools),
		},
		TokensUsed: int(response.Usage.InputTokens + response.Usage.OutputTokens),
		Duration:   time.Since(startTime),
		ProviderID: "claude",
	}, nil
}

func (c *ClaudeProvider) RegisterTool(tool *ToolDefinition) error {
	c.tools = append(c.tools, tool)
	return nil
}

func (c *ClaudeProvider) ListTools() []*ToolDefinition {
	return c.tools
}

// StreamingProvider interface implementation

func (c *ClaudeProvider) AnalyzeStream(ctx context.Context, req *AnalysisRequest) (<-chan *AnalysisResponse, error) {
	responseChan := make(chan *AnalysisResponse, 1)

	go func() {
		defer close(responseChan)

		// For now, we'll implement streaming as a single response
		// In the future, this could use Claude's streaming API
		response, err := c.Analyze(ctx, req)
		if err != nil {
			// Send error response
			responseChan <- &AnalysisResponse{
				Result: fmt.Sprintf("Error: %v", err),
				Metadata: map[string]interface{}{
					"error": err.Error(),
				},
			}
			return
		}

		responseChan <- response
	}()

	return responseChan, nil
}

// Helper methods

func (c *ClaudeProvider) extractTextFromResponse(response *anthropic.Message) string {
	if len(response.Content) == 0 {
		return ""
	}

	// Extract text from content blocks properly
	var result strings.Builder
	for _, block := range response.Content {
		// ContentBlockUnion is a struct with Type field indicating the variant
		switch block.Type {
		case "text":
			// Extract text from text block
			result.WriteString(block.Text)
		case "thinking":
			// Extract thinking content (Claude extended thinking)
			result.WriteString(block.Thinking)
		case "tool_use", "server_tool_use":
			// For tool use blocks, skip or handle based on use case
			continue
		case "web_search_tool_result":
			// Web search results - could extract if needed
			continue
		default:
			// Unknown block type - use fallback
			result.WriteString(fmt.Sprintf("%v", block))
		}
	}

	return result.String()
}

func (c *ClaudeProvider) calculateConfidence(analysisType AnalysisType, result string) float64 {
	// Simple confidence calculation based on result characteristics
	result = strings.TrimSpace(strings.ToLower(result))

	switch analysisType {
	case AnalysisFunctionEquivalence, AnalysisDeadCode:
		// For boolean responses, high confidence if it's a clear true/false
		if result == "true" || result == "false" {
			return 0.95
		}
		return 0.7
	default:
		// For other types, base confidence on result length and structure
		if len(result) > 50 && strings.Contains(result, "\n") {
			return 0.85
		}
		return 0.75
	}
}
