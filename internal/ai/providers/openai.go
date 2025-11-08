package providers

import (
	"context"
	"net/http"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/sashabaranov/go-openai"
)

// OpenAIProvider implements the AI provider interface for OpenAI using go-openai library
type OpenAIProvider struct {
	client       *openai.Client
	config       *ProviderConfig
	capabilities ProviderCapabilities
}

// NewOpenAIProvider creates a new OpenAI provider instance using go-openai library
func NewOpenAIProvider(config *ProviderConfig) (*OpenAIProvider, error) {
	if config.APIKey == "" {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"OpenAI API key is required",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("set OPENAI_API_KEY environment variable")
	}

	// Set default model
	model := "gpt-4"
	if config.Model != "" {
		model = config.Model
	}

	// Set default endpoint
	endpoint := ""
	if config.Endpoint != "" {
		endpoint = config.Endpoint
	}

	// Create OpenAI client configuration
	clientConfig := openai.DefaultConfig(config.APIKey)
	if endpoint != "" {
		clientConfig.BaseURL = endpoint
	}

	// Set timeout if specified
	if config.Timeout > 0 {
		clientConfig.HTTPClient = &http.Client{
			Timeout: config.Timeout,
		}
	}

	client := openai.NewClientWithConfig(clientConfig)

	provider := &OpenAIProvider{
		client: client,
		config: config,
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
			},
			SupportsStreaming: false,
			SupportsTools:     true,
			SupportsBatch:     false,
			MaxTokens:         128000, // GPT-4 context window
			MaxBatchSize:      0,      // No batch support
		},
	}

	// Update config with defaults
	if provider.config.Model == "" {
		provider.config.Model = model
	}

	return provider, nil
}

// Provider interface implementation

func (o *OpenAIProvider) Name() string {
	return "OpenAI"
}

func (o *OpenAIProvider) Type() ProviderType {
	return ProviderOpenAI
}

func (o *OpenAIProvider) SupportedTypes() []AnalysisType {
	return o.capabilities.SupportedTypes
}

func (o *OpenAIProvider) Configure(config *ProviderConfig) error {
	if config.APIKey != "" {
		o.config.APIKey = config.APIKey
		// Recreate client with new API key
		clientConfig := openai.DefaultConfig(config.APIKey)
		if o.config.Endpoint != "" {
			clientConfig.BaseURL = o.config.Endpoint
		}
		if o.config.Timeout > 0 {
			clientConfig.HTTPClient = &http.Client{
				Timeout: o.config.Timeout,
			}
		}
		o.client = openai.NewClientWithConfig(clientConfig)
	}

	if config.Model != "" {
		o.config.Model = config.Model
	}

	if config.Endpoint != "" {
		o.config.Endpoint = config.Endpoint
	}

	return nil
}

func (o *OpenAIProvider) HealthCheck(ctx context.Context) error {
	// Simple health check by making a minimal API call
	req := &AnalysisRequest{
		Content:     "test",
		MaxTokens:   10,
		Temperature: 0.1,
	}

	_, err := o.Analyze(ctx, req)
	return err
}

func (o *OpenAIProvider) Analyze(ctx context.Context, req *AnalysisRequest) (*AnalysisResponse, error) {
	startTime := time.Now()

	systemPrompt := GetSystemPromptForType(req.Type)
	userPrompt := BuildUserPrompt(req)

	maxTokens := 1000
	if req.MaxTokens > 0 {
		maxTokens = req.MaxTokens
	}

	temperature := float32(0.1) // Default low temperature for consistent results
	if req.Temperature > 0 {
		temperature = float32(req.Temperature)
	}

	// Build chat completion request using go-openai
	chatReq := openai.ChatCompletionRequest{
		Model: o.config.Model,
		Messages: []openai.ChatCompletionMessage{
			{
				Role:    openai.ChatMessageRoleSystem,
				Content: systemPrompt,
			},
			{
				Role:    openai.ChatMessageRoleUser,
				Content: userPrompt,
			},
		},
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}

	// Make API call
	resp, err := o.client.CreateChatCompletion(ctx, chatReq)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"OpenAI API call failed",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("check OPENAI_API_KEY and rate limits")
	}

	if len(resp.Choices) == 0 {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"no choices in API response",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check API request parameters")
	}

	result := resp.Choices[0].Message.Content

	return &AnalysisResponse{
		Result:     result,
		Confidence: CalculateConfidence(req.Type, result),
		Metadata: map[string]interface{}{
			"model":             resp.Model,
			"prompt_tokens":     resp.Usage.PromptTokens,
			"completion_tokens": resp.Usage.CompletionTokens,
			"finish_reason":     resp.Choices[0].FinishReason,
		},
		TokensUsed: resp.Usage.TotalTokens,
		Duration:   time.Since(startTime),
		ProviderID: "openai",
	}, nil
}

func (o *OpenAIProvider) SubmitBatch(ctx context.Context, batch *BatchRequest) (*BatchResponse, error) {
	return nil, errors.NewError(
		errors.ErrorCodeValidationFailed,
		"batch processing not supported by OpenAI provider",
		errors.SeverityError,
		errors.CategoryValidation,
	).WithSuggestion("use Claude provider for batch processing")
}

func (o *OpenAIProvider) GetBatchStatus(ctx context.Context, batchID string) (*BatchResponse, error) {
	return nil, errors.NewError(
		errors.ErrorCodeValidationFailed,
		"batch processing not supported by OpenAI provider",
		errors.SeverityError,
		errors.CategoryValidation,
	).WithSuggestion("use Claude provider for batch processing")
}

func (o *OpenAIProvider) SupportsStreaming() bool {
	return o.capabilities.SupportsStreaming
}

func (o *OpenAIProvider) SupportsTools() bool {
	return o.capabilities.SupportsTools
}

func (o *OpenAIProvider) SupportsBatch() bool {
	return o.capabilities.SupportsBatch
}

// Note: Shared helper functions (GetSystemPromptForType, BuildUserPrompt, CalculateConfidence)
// have been moved to common.go to be shared across all AI providers
