package providers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/azure"
	"github.com/openai/openai-go/option"
)

// AzureOpenAIProvider implements the AI provider interface for Azure OpenAI
type AzureOpenAIProvider struct {
    client       openai.Client
    config       *ProviderConfig
    capabilities ProviderCapabilities
}

// NewAzureOpenAIProvider creates a new Azure OpenAI provider instance
func NewAzureOpenAIProvider(config *ProviderConfig) (*AzureOpenAIProvider, error) {
    if config.Endpoint == "" {
        return nil, errors.NewError(
            errors.ErrorCodeValidationFailed,
            "azure OpenAI endpoint is required",
            errors.SeverityError,
            errors.CategoryValidation,
        ).WithSuggestion("set AZURE_OPENAI_ENDPOINT environment variable")
    }

    if config.AzureDeployment == "" {
        return nil, errors.NewError(
            errors.ErrorCodeValidationFailed,
            "azure deployment name is required",
            errors.SeverityError,
            errors.CategoryValidation,
        ).WithSuggestion("set AZURE_OPENAI_DEPLOYMENT environment variable")
    }

    timeout := 60 * time.Second
    if config.Timeout > 0 {
        timeout = config.Timeout
    }

    // Set default API version if not specified
    if config.AzureAPIVersion == "" {
        // Use "preview" for v1 API (recommended for latest features)
        // Alternative: "2024-10-21" for latest GA version
        config.AzureAPIVersion = "preview"
    }

    // Create HTTP client with timeout
    httpClient := &http.Client{Timeout: timeout}

    // Determine if we're using v1 API (preview or no version specified)
    // v1 API uses /openai/v1/ endpoint format
    useV1API := config.AzureAPIVersion == "preview" || config.AzureAPIVersion == ""

    // Create Azure OpenAI client based on API version and authentication method
    var client openai.Client

    if useV1API {
        // v1 API: Use /openai/v1/ endpoint format
        baseURL := config.Endpoint
        if !strings.HasSuffix(baseURL, "/") {
            baseURL += "/"
        }
        baseURL += "openai/v1/"

        if config.UseAzureAD {
            // Use Azure AD authentication
            credential, err := azidentity.NewDefaultAzureCredential(nil)
            if err != nil {
                return nil, errors.NewError(
                    errors.ErrorCodeValidationFailed,
                    "failed to create Azure AD credential",
                    errors.SeverityError,
                    errors.CategoryValidation,
                ).WithInnerError(err).WithSuggestion("ensure Azure AD authentication is configured")
            }

            client = openai.NewClient(
                option.WithBaseURL(baseURL),
                azure.WithTokenCredential(credential),
                option.WithHTTPClient(httpClient),
            )
        } else {
            // Use API key authentication
            if config.APIKey == "" {
                return nil, errors.NewError(
                    errors.ErrorCodeValidationFailed,
                    "azure OpenAI API key is required when not using Azure AD",
                    errors.SeverityError,
                    errors.CategoryValidation,
                ).WithSuggestion("set AZURE_OPENAI_API_KEY environment variable")
            }

            client = openai.NewClient(
                option.WithBaseURL(baseURL),
                option.WithAPIKey(config.APIKey),
                option.WithHTTPClient(httpClient),
            )
        }
    } else {
        // v0 API (legacy): Use deployment-specific endpoint format
        // Format: https://{resource}.openai.azure.com/openai/deployments/{deployment}/...
        // The SDK handles deployment in the request path for v0 API
        baseURL := config.Endpoint
        if !strings.HasSuffix(baseURL, "/") {
            baseURL += "/"
        }

        if config.UseAzureAD {
            // Use Azure AD authentication
            credential, err := azidentity.NewDefaultAzureCredential(nil)
            if err != nil {
                return nil, errors.NewError(
                    errors.ErrorCodeValidationFailed,
                    "failed to create Azure AD credential",
                    errors.SeverityError,
                    errors.CategoryValidation,
                ).WithInnerError(err).WithSuggestion("ensure Azure AD authentication is configured")
            }

            client = openai.NewClient(
                azure.WithEndpoint(baseURL, config.AzureAPIVersion),
                azure.WithTokenCredential(credential),
                option.WithHTTPClient(httpClient),
            )
        } else {
            // Use API key authentication
            if config.APIKey == "" {
                return nil, errors.NewError(
                    errors.ErrorCodeValidationFailed,
                    "azure OpenAI API key is required when not using Azure AD",
                    errors.SeverityError,
                    errors.CategoryValidation,
                ).WithSuggestion("set AZURE_OPENAI_API_KEY environment variable")
            }

            client = openai.NewClient(
                azure.WithEndpoint(baseURL, config.AzureAPIVersion),
                azure.WithAPIKey(config.APIKey),
                option.WithHTTPClient(httpClient),
            )
        }
    }

    provider := &AzureOpenAIProvider{
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
            SupportsStreaming: true,
            SupportsTools:     true,
            SupportsBatch:     false,
            MaxTokens:         128000, // GPT-4 context window
            MaxBatchSize:      0,      // No batch support currently
        },
    }

    return provider, nil
}

// Provider interface implementation

func (a *AzureOpenAIProvider) Name() string {
    return "Azure OpenAI"
}

func (a *AzureOpenAIProvider) Type() ProviderType {
    return ProviderAzureOpenAI
}

func (a *AzureOpenAIProvider) SupportedTypes() []AnalysisType {
    return a.capabilities.SupportedTypes
}

func (a *AzureOpenAIProvider) Configure(config *ProviderConfig) error {
    if config.APIKey != "" {
        a.config.APIKey = config.APIKey
    }

    if config.Model != "" {
        a.config.Model = config.Model
    }

    if config.Endpoint != "" {
        a.config.Endpoint = config.Endpoint
    }

    if config.AzureDeployment != "" {
        a.config.AzureDeployment = config.AzureDeployment
    }

    if config.AzureAPIVersion != "" {
        a.config.AzureAPIVersion = config.AzureAPIVersion
    }

    if config.Timeout > 0 {
        // Note: Timeout change would require recreating the client
        a.config.Timeout = config.Timeout
    }

    return nil
}

func (a *AzureOpenAIProvider) HealthCheck(ctx context.Context) error {
    // Simple health check by making a minimal API call
    req := &AnalysisRequest{
        Content:     "test",
        MaxTokens:   10,
        Temperature: 0.1,
    }

    _, err := a.Analyze(ctx, req)
    return err
}

func (a *AzureOpenAIProvider) Analyze(ctx context.Context, req *AnalysisRequest) (*AnalysisResponse, error) {
    startTime := time.Now()

    systemPrompt := GetSystemPromptForType(req.Type)
    userPrompt := BuildUserPrompt(req)

    maxTokens := int64(1000)
    if req.MaxTokens > 0 {
        maxTokens = int64(req.MaxTokens)
    }

    temperature := float64(0.1) // Default low temperature for consistent results
    if req.Temperature > 0 {
        temperature = req.Temperature
    }

    // Build messages using openai-go types
    messages := []openai.ChatCompletionMessageParamUnion{
        {
            OfSystem: &openai.ChatCompletionSystemMessageParam{
                Content: openai.ChatCompletionSystemMessageParamContentUnion{
                    OfString: openai.String(systemPrompt),
                },
            },
        },
        {
            OfUser: &openai.ChatCompletionUserMessageParam{
                Content: openai.ChatCompletionUserMessageParamContentUnion{
                    OfString: openai.String(userPrompt),
                },
            },
        },
    }

    // Make the API call using openai-go ChatCompletion API
    resp, err := a.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
        Messages:    messages,
        Model:       openai.ChatModel(a.config.AzureDeployment),
        MaxTokens:   openai.Int(maxTokens),
        Temperature: openai.Float(temperature),
    })

    if err != nil {
        return nil, errors.NewError(
            errors.ErrorCodeValidationFailed,
            "azure OpenAI API call failed",
            errors.SeverityError,
            errors.CategoryValidation,
        ).WithInnerError(err).WithSuggestion("check Azure OpenAI endpoint and credentials")
    }

    if len(resp.Choices) == 0 {
        return nil, errors.NewError(
            errors.ErrorCodeValidationFailed,
            "no choices in Azure OpenAI response",
            errors.SeverityError,
            errors.CategoryValidation,
        ).WithSuggestion("check API request parameters")
    }

    content := resp.Choices[0].Message.Content

    // Extract token usage
    tokensUsed := int(resp.Usage.TotalTokens)
    promptTokens := int(resp.Usage.PromptTokens)
    completionTokens := int(resp.Usage.CompletionTokens)

    return &AnalysisResponse{
        Result:     content,
        Confidence: CalculateConfidence(req.Type, content),
        Metadata: map[string]interface{}{
            "deployment":        a.config.AzureDeployment,
            "api_version":       a.config.AzureAPIVersion,
            "prompt_tokens":     promptTokens,
            "completion_tokens": completionTokens,
        },
        TokensUsed: tokensUsed,
        Duration:   time.Since(startTime),
        ProviderID: "azure-openai",
    }, nil
}

func (a *AzureOpenAIProvider) SubmitBatch(ctx context.Context, batch *BatchRequest) (*BatchResponse, error) {
    return nil, errors.NewError(
        errors.ErrorCodeValidationFailed,
        "batch processing not supported by Azure OpenAI provider",
        errors.SeverityError,
        errors.CategoryValidation,
    ).WithSuggestion("use Claude provider for batch processing")
}

func (a *AzureOpenAIProvider) GetBatchStatus(ctx context.Context, batchID string) (*BatchResponse, error) {
    return nil, errors.NewError(
        errors.ErrorCodeValidationFailed,
        "batch processing not supported by Azure OpenAI provider",
        errors.SeverityError,
        errors.CategoryValidation,
    ).WithSuggestion("use Claude provider for batch processing")
}

func (a *AzureOpenAIProvider) SupportsStreaming() bool {
    return a.capabilities.SupportsStreaming
}

func (a *AzureOpenAIProvider) SupportsTools() bool {
    return a.capabilities.SupportsTools
}

func (a *AzureOpenAIProvider) SupportsBatch() bool {
    return a.capabilities.SupportsBatch
}

// Note: Shared helper functions (GetSystemPromptForType, BuildUserPrompt, CalculateConfidence)
// are now in common.go and used by all AI providers for consistency
