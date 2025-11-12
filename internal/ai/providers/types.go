package providers

import (
	"context"
	"time"
)

// ProviderType represents the type of AI provider
type ProviderType string

const (
	ProviderOpenAI      ProviderType = "openai"
	ProviderClaude      ProviderType = "claude"
	ProviderLocal       ProviderType = "local"
	ProviderBedrock     ProviderType = "bedrock"
	ProviderAzureOpenAI ProviderType = "azure-openai"
)

// AnalysisRequest represents a structured request for AI analysis
type AnalysisRequest struct {
	Type        AnalysisType           `json:"type"`
	Content     string                 `json:"content"`
	Context     string                 `json:"context,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Temperature float64                `json:"temperature,omitempty"`
	MaxTokens   int                    `json:"max_tokens,omitempty"`
}

// AnalysisResponse represents the response from AI analysis
type AnalysisResponse struct {
	Result     string                 `json:"result"`
	Confidence float64                `json:"confidence,omitempty"`
	Metadata   map[string]interface{} `json:"metadata,omitempty"`
	TokensUsed int                    `json:"tokens_used,omitempty"`
	Duration   time.Duration          `json:"duration,omitempty"`
	ProviderID string                 `json:"provider_id,omitempty"`
}

// AnalysisType represents different types of AI analysis
type AnalysisType string

const (
	AnalysisFunctionEquivalence AnalysisType = "function_equivalence"
	AnalysisDeadCode            AnalysisType = "dead_code"
	AnalysisFunctionComplexity  AnalysisType = "function_complexity"
	AnalysisAuthPatterns        AnalysisType = "auth_patterns"
	AnalysisOptimizations       AnalysisType = "optimizations"
	AnalysisCodeCoverage        AnalysisType = "code_coverage"
	AnalysisSchemaConsistency   AnalysisType = "schema_consistency"
	AnalysisSQLComplexity       AnalysisType = "sql_complexity"
	AnalysisBatchProcessing     AnalysisType = "batch_processing"
)

// ProviderConfig holds configuration for a specific AI provider
type ProviderConfig struct {
	APIKey          string                 `json:"api_key,omitempty"`
	Model           string                 `json:"model"`
	Endpoint        string                 `json:"endpoint,omitempty"`
	Timeout         time.Duration          `json:"timeout,omitempty"`
	MaxRetries      int                    `json:"max_retries,omitempty"`
	DefaultSettings map[string]interface{} `json:"default_settings,omitempty"`
	// Azure-specific fields
	AzureDeployment string `json:"azure_deployment,omitempty"` // Azure deployment name
	AzureAPIVersion string `json:"azure_api_version,omitempty"` // Azure API version
	UseAzureAD      bool   `json:"use_azure_ad,omitempty"`      // Use Azure AD authentication
}

// BatchRequest represents a batch of analysis requests
type BatchRequest struct {
	ID          string                 `json:"id"`
	Requests    []*AnalysisRequest     `json:"requests"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Priority    int                    `json:"priority,omitempty"`
	CallbackURL string                 `json:"callback_url,omitempty"`
}

// BatchResponse represents the response from batch processing
type BatchResponse struct {
	ID          string              `json:"id"`
	Status      string              `json:"status"`
	Results     []*AnalysisResponse `json:"results"`
	Progress    float64             `json:"progress"`
	Error       string              `json:"error,omitempty"`
	CompletedAt *time.Time          `json:"completed_at,omitempty"`
}

// Provider defines the interface that all AI providers must implement
type Provider interface {
	// Basic analysis methods
	Analyze(ctx context.Context, req *AnalysisRequest) (*AnalysisResponse, error)

	// Provider information
	Name() string
	Type() ProviderType
	SupportedTypes() []AnalysisType

	// Configuration and health
	Configure(config *ProviderConfig) error
	HealthCheck(ctx context.Context) error

	// Batch processing (optional, return error if not supported)
	SubmitBatch(ctx context.Context, batch *BatchRequest) (*BatchResponse, error)
	GetBatchStatus(ctx context.Context, batchID string) (*BatchResponse, error)

	// Advanced features (optional)
	SupportsStreaming() bool
	SupportsTools() bool
	SupportsBatch() bool
}

// ToolDefinition represents a tool that can be used by AI providers
type ToolDefinition struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"input_schema"`
}

// ToolUseProvider extends Provider with tool use capabilities
type ToolUseProvider interface {
	Provider

	// Tool-enhanced analysis
	AnalyzeWithTools(ctx context.Context, req *AnalysisRequest, tools []*ToolDefinition) (*AnalysisResponse, error)

	// Tool management
	RegisterTool(tool *ToolDefinition) error
	ListTools() []*ToolDefinition
}

// StreamingProvider extends Provider with streaming capabilities
type StreamingProvider interface {
	Provider

	// Streaming analysis
	AnalyzeStream(ctx context.Context, req *AnalysisRequest) (<-chan *AnalysisResponse, error)
}

// ProviderCapabilities represents what a provider supports
type ProviderCapabilities struct {
	SupportedTypes    []AnalysisType `json:"supported_types"`
	SupportsStreaming bool           `json:"supports_streaming"`
	SupportsTools     bool           `json:"supports_tools"`
	SupportsBatch     bool           `json:"supports_batch"`
	MaxTokens         int            `json:"max_tokens"`
	MaxBatchSize      int            `json:"max_batch_size"`
}
