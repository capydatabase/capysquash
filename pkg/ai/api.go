// Package ai provides AI-powered migration analysis and optimization.
package ai

//
// This package exposes AI provider management for building intelligent migration
// analysis tools that leverage multiple AI providers with automatic failover.
//
// # Supported Providers
//
// The AI manager supports multiple providers with automatic failover:
//
//   1. Azure OpenAI (default) - Enterprise-grade, recommended for production
//   2. Anthropic Claude (fallback) - High-quality analysis with tool use
//   3. OpenAI GPT (last resort) - Widely available, good for development
//
// # Basic Usage
//
// Create a manager with automatic provider detection:
//
//	manager, err := ai.NewProviderManager(nil) // Uses defaults
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Analyze migrations
//	req := &ai.AnalysisRequest{
//	    Type:       ai.AnalysisTypeOptimization,
//	    Migrations: []string{migrationSQL},
//	    Context: map[string]interface{}{
//	        "safety_level": "conservative",
//	    },
//	}
//
//	resp, err := manager.Analyze(ctx, req)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	fmt.Printf("Analysis: %s\n", resp.Analysis)
//	fmt.Printf("Confidence: %.2f\n", resp.Confidence)
//
// # Custom Configuration
//
// Configure provider priority and fallback behavior:
//
//	config := &ai.ManagerConfig{
//	    DefaultProvider:  ai.ProviderAzureOpenAI,
//	    FallbackProvider: ai.ProviderClaude,
//	    EnableFallback:   true,
//	    ProviderConfigs: map[ai.ProviderType]*ai.ProviderConfig{
//	        ai.ProviderAzureOpenAI: {
//	            Endpoint:        os.Getenv("AZURE_OPENAI_ENDPOINT"),
//	            AzureDeployment: os.Getenv("AZURE_OPENAI_DEPLOYMENT"),
//	            AzureAPIVersion: "2024-02-15-preview",
//	            Timeout:         60 * time.Second,
//	        },
//	        ai.ProviderClaude: {
//	            APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
//	            Model:   "claude-3-5-sonnet-latest",
//	            Timeout: 60 * time.Second,
//	        },
//	    },
//	    RetrySettings: ai.RetryConfig{
//	        MaxRetries:    3,
//	        InitialDelay:  time.Second,
//	        MaxDelay:      30 * time.Second,
//	        BackoffFactor: 2.0,
//	    },
//	}
//
//	manager, err := ai.NewProviderManager(config)
//
// # Health Checks
//
// Monitor provider health:
//
//	health := manager.HealthCheck(ctx)
//	for provider, err := range health {
//	    if err != nil {
//	        log.Printf("Provider %s unhealthy: %v", provider, err)
//	    }
//	}
//
// # Environment Variables
//
// Azure OpenAI:
//   - AZURE_OPENAI_ENDPOINT: Azure endpoint URL
//   - AZURE_OPENAI_DEPLOYMENT: Deployment name
//   - AZURE_OPENAI_API_KEY: API key (or use Azure AD)
//   - AZURE_OPENAI_API_VERSION: API version (optional)
//   - AZURE_OPENAI_USE_AD: Use Azure AD auth (optional)
//
// Anthropic Claude:
//   - ANTHROPIC_API_KEY: API key
//
// OpenAI:
//   - OPENAI_API_KEY: API key

import (
	"context"
	"time"

	internal_ai "github.com/capysquash/pgsquash-engine/internal/ai"
	"github.com/capysquash/pgsquash-engine/internal/ai/providers"
)

// Re-export types from internal packages
type (
	// ProviderManager manages multiple AI providers and routes requests with automatic failover
	ProviderManager = internal_ai.ProviderManager

	// ManagerConfig holds configuration for the provider manager
	ManagerConfig = internal_ai.ManagerConfig

	// RetryConfig defines retry behavior for AI operations
	RetryConfig = internal_ai.RetryConfig

	// Provider represents an AI provider interface
	Provider = providers.Provider

	// ProviderType represents the type of AI provider
	ProviderType = providers.ProviderType

	// ProviderConfig holds provider-specific configuration
	ProviderConfig = providers.ProviderConfig

	// AnalysisRequest represents a request for AI analysis
	AnalysisRequest = providers.AnalysisRequest

	// AnalysisResponse contains the results of AI analysis
	AnalysisResponse = providers.AnalysisResponse

	// AnalysisType represents different types of AI analysis
	AnalysisType = providers.AnalysisType

	// BatchRequest represents a batch analysis request
	BatchRequest = providers.BatchRequest

	// BatchResponse contains batch analysis results
	BatchResponse = providers.BatchResponse

	// ToolDefinition defines a tool that can be used by AI
	ToolDefinition = providers.ToolDefinition

	// ProviderCapabilities describes what a provider supports
	ProviderCapabilities = providers.ProviderCapabilities

	// ToolUseProvider represents a provider that supports tool use
	ToolUseProvider = providers.ToolUseProvider

	// StreamingProvider represents a provider that supports streaming
	StreamingProvider = providers.StreamingProvider
)

// Provider type constants
const (
	// ProviderAzureOpenAI uses Azure OpenAI Service (recommended for production)
	ProviderAzureOpenAI ProviderType = providers.ProviderAzureOpenAI

	// ProviderClaude uses Anthropic Claude (excellent for analysis)
	ProviderClaude ProviderType = providers.ProviderClaude

	// ProviderOpenAI uses OpenAI GPT models (widely available)
	ProviderOpenAI ProviderType = providers.ProviderOpenAI

	// ProviderLocal uses local models (experimental)
	ProviderLocal ProviderType = providers.ProviderLocal

	// ProviderBedrock uses AWS Bedrock (enterprise)
	ProviderBedrock ProviderType = providers.ProviderBedrock
)

// Analysis type constants
const (
	// AnalysisFunctionEquivalence analyzes function equivalence
	AnalysisFunctionEquivalence AnalysisType = providers.AnalysisFunctionEquivalence

	// AnalysisDeadCode detects unused or redundant code
	AnalysisDeadCode AnalysisType = providers.AnalysisDeadCode

	// AnalysisFunctionComplexity analyzes function complexity
	AnalysisFunctionComplexity AnalysisType = providers.AnalysisFunctionComplexity

	// AnalysisAuthPatterns detects authentication patterns
	AnalysisAuthPatterns AnalysisType = providers.AnalysisAuthPatterns

	// AnalysisOptimizations analyzes optimization opportunities
	AnalysisOptimizations AnalysisType = providers.AnalysisOptimizations

	// AnalysisCodeCoverage analyzes code coverage
	AnalysisCodeCoverage AnalysisType = providers.AnalysisCodeCoverage

	// AnalysisSchemaConsistency validates schema consistency
	AnalysisSchemaConsistency AnalysisType = providers.AnalysisSchemaConsistency

	// AnalysisSQLComplexity analyzes SQL complexity
	AnalysisSQLComplexity AnalysisType = providers.AnalysisSQLComplexity

	// AnalysisBatchProcessing for batch analysis
	AnalysisBatchProcessing AnalysisType = providers.AnalysisBatchProcessing
)

// Constructors
var (
	// NewProviderManager creates a new AI provider manager
	// If config is nil, uses default configuration with auto-detected providers
	NewProviderManager = internal_ai.NewProviderManager
)

// ProviderManager methods
// These are available on *ProviderManager instances:
//
//   Analyze(ctx, req) (*AnalysisResponse, error)
//   AnalyzeWithTools(ctx, req, tools) (*AnalysisResponse, error)
//   GetProvider(providerType) (Provider, error)
//   GetDefaultProvider() (Provider, error)
//   ListProviders() []ProviderType
//   SubmitBatch(ctx, batch) (*BatchResponse, error)
//   GetBatchStatus(ctx, batchID) (*BatchResponse, error)
//   HealthCheck(ctx) map[ProviderType]error
//   GetCapabilities() map[ProviderType]ProviderCapabilities

// DefaultManagerConfig returns a recommended configuration for production use
func DefaultManagerConfig() *ManagerConfig {
	return &ManagerConfig{
		DefaultProvider:  ProviderAzureOpenAI, // Best for production
		FallbackProvider: ProviderClaude,      // High-quality fallback
		EnableFallback:   true,
		ProviderConfigs:  make(map[ProviderType]*ProviderConfig),
		RetrySettings: RetryConfig{
			MaxRetries:    3,
			InitialDelay:  time.Second,
			MaxDelay:      30 * time.Second,
			BackoffFactor: 2.0,
		},
	}
}

// Example usage patterns

// ExampleBasicAnalysis demonstrates basic AI analysis
func ExampleBasicAnalysis() {
	ctx := context.Background()

	// Create manager with defaults (auto-detects providers from environment)
	manager, err := NewProviderManager(nil)
	if err != nil {
		panic(err)
	}

	// Create analysis request
	req := &AnalysisRequest{
		Type: AnalysisOptimizations,
		Content: "CREATE TABLE users (id SERIAL PRIMARY KEY); ALTER TABLE users ADD COLUMN email TEXT;",
		Context: "Analyzing migration for optimization opportunities with conservative safety level",
		Metadata: map[string]interface{}{
			"safety_level": "conservative",
		},
	}

	// Analyze with automatic failover
	resp, err := manager.Analyze(ctx, req)
	if err != nil {
		panic(err)
	}

	// Use results
	_ = resp.Result     // AI analysis result
	_ = resp.Confidence // Confidence score (0-1)
}

// ExampleCustomConfiguration demonstrates custom provider configuration
func ExampleCustomConfiguration() {
	config := &ManagerConfig{
		DefaultProvider:  ProviderAzureOpenAI,
		FallbackProvider: ProviderClaude,
		EnableFallback:   true,
		ProviderConfigs: map[ProviderType]*ProviderConfig{
			ProviderAzureOpenAI: {
				Endpoint:        "https://myroomieai-eu.openai.azure.com/",
				AzureDeployment: "gpt-4",
				AzureAPIVersion: "2024-02-15-preview",
				Timeout:         60 * time.Second,
			},
		},
		RetrySettings: RetryConfig{
			MaxRetries:    3,
			InitialDelay:  time.Second,
			MaxDelay:      30 * time.Second,
			BackoffFactor: 2.0,
		},
	}

	manager, err := NewProviderManager(config)
	if err != nil {
		panic(err)
	}
	_ = manager
}

// ExampleHealthCheck demonstrates provider health monitoring
func ExampleHealthCheck() {
	ctx := context.Background()

	manager, err := NewProviderManager(nil)
	if err != nil {
		panic(err)
	}

	// Check health of all providers
	health := manager.HealthCheck(ctx)
	for provider, err := range health {
		if err != nil {
			// Provider is unhealthy
			_ = provider
		}
	}
}

// ExampleProviderCapabilities demonstrates querying provider capabilities
func ExampleProviderCapabilities() {
	manager, err := NewProviderManager(nil)
	if err != nil {
		panic(err)
	}

	// Get capabilities of all providers
	capabilities := manager.GetCapabilities()
	for provider, caps := range capabilities {
		_ = provider
		_ = caps.SupportedTypes    // What analysis types are supported
		_ = caps.SupportsStreaming // Does it support streaming
		_ = caps.SupportsTools     // Does it support tool use
		_ = caps.SupportsBatch     // Does it support batch processing
	}
}
