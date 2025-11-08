package ai

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/ai/providers"
	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// Type aliases to avoid import cycles
type (
	Provider         = providers.Provider
	ProviderType     = providers.ProviderType
	ProviderConfig   = providers.ProviderConfig
	AnalysisRequest  = providers.AnalysisRequest
	AnalysisResponse = providers.AnalysisResponse
	AnalysisType     = providers.AnalysisType
	BatchRequest     = providers.BatchRequest
	BatchResponse    = providers.BatchResponse
	ToolDefinition   = providers.ToolDefinition
)

// Provider type constants
const (
	ProviderOpenAI      = providers.ProviderOpenAI
	ProviderClaude      = providers.ProviderClaude
	ProviderLocal       = providers.ProviderLocal
	ProviderBedrock     = providers.ProviderBedrock
	ProviderAzureOpenAI = providers.ProviderAzureOpenAI
)

// HealthCache stores cached health check results for providers
type HealthCache struct {
	healthy   bool
	lastCheck time.Time
	ttl       time.Duration
	lastError string // Error message from last health check (if any)
}

// ProviderManager manages multiple AI providers and routes requests
type ProviderManager struct {
	providers    map[ProviderType]Provider
	defaultType  ProviderType
	fallbackType ProviderType
	healthCache  map[ProviderType]*HealthCache
	mutex        sync.RWMutex
	config       *ManagerConfig
}

// ManagerConfig holds configuration for the provider manager
type ManagerConfig struct {
	DefaultProvider    ProviderType                     `json:"default_provider"`
	FallbackProvider   ProviderType                     `json:"fallback_provider,omitempty"`
	EnableFallback     bool                             `json:"enable_fallback"`
	ProviderConfigs    map[ProviderType]*ProviderConfig `json:"provider_configs"`
	LoadBalancing      bool                             `json:"load_balancing"`
	PreferredProviders map[AnalysisType]ProviderType    `json:"preferred_providers,omitempty"`
	RetrySettings      RetryConfig                      `json:"retry_settings"`
}

// RetryConfig defines retry behavior
type RetryConfig struct {
	MaxRetries    int           `json:"max_retries"`
	InitialDelay  time.Duration `json:"initial_delay"`
	MaxDelay      time.Duration `json:"max_delay"`
	BackoffFactor float64       `json:"backoff_factor"`
}

// NewProviderManager creates a new provider manager
func NewProviderManager(config *ManagerConfig) (*ProviderManager, error) {
	if config == nil {
		// Default provider priority: Azure OpenAI → Anthropic Claude → OpenAI
		config = &ManagerConfig{
			DefaultProvider:  ProviderAzureOpenAI, // Default to Azure OpenAI (preferred)
			EnableFallback:   true,
			FallbackProvider: ProviderClaude, // Fallback to Claude (Anthropic)
			ProviderConfigs:  make(map[ProviderType]*ProviderConfig),
			RetrySettings: RetryConfig{
				MaxRetries:    3,
				InitialDelay:  time.Second,
				MaxDelay:      30 * time.Second,
				BackoffFactor: 2.0,
			},
		}
	}

	manager := &ProviderManager{
		providers:    make(map[ProviderType]Provider),
		defaultType:  config.DefaultProvider,
		fallbackType: config.FallbackProvider,
		healthCache:  make(map[ProviderType]*HealthCache),
		config:       config,
	}

	// Initialize providers based on available API keys and configuration
	if err := manager.initializeProviders(); err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to initialize providers",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("ensure AI provider API keys are configured")
	}

	return manager, nil
}

// initializeProviders sets up available providers based on configuration and environment
func (pm *ProviderManager) initializeProviders() error {
	// Initialize Claude provider if API key is available
	if claudeKey := os.Getenv("ANTHROPIC_API_KEY"); claudeKey != "" {
		config := pm.config.ProviderConfigs[ProviderClaude]
		if config == nil {
			config = &ProviderConfig{
				APIKey:  claudeKey,
				Model:   "claude-3-5-sonnet-latest",
				Timeout: 60 * time.Second,
			}
		} else if config.APIKey == "" {
			config.APIKey = claudeKey
		}

		provider, err := providers.NewClaudeProvider(config)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to initialize Claude provider",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithSuggestion("verify ANTHROPIC_API_KEY is valid")
		}
		pm.providers[ProviderClaude] = provider
	}

	// Initialize OpenAI provider if API key is available
	if openaiKey := os.Getenv("OPENAI_API_KEY"); openaiKey != "" {
		config := pm.config.ProviderConfigs[ProviderOpenAI]
		if config == nil {
			config = &ProviderConfig{
				APIKey:  openaiKey,
				Model:   "gpt-4",
				Timeout: 60 * time.Second,
			}
		} else if config.APIKey == "" {
			config.APIKey = openaiKey
		}

		provider, err := providers.NewOpenAIProvider(config)
		if err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to initialize OpenAI provider",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithSuggestion("verify OPENAI_API_KEY is valid")
		}
		pm.providers[ProviderOpenAI] = provider
	}

	// Initialize Azure OpenAI provider if configuration is available
	if azureEndpoint := os.Getenv("AZURE_OPENAI_ENDPOINT"); azureEndpoint != "" {
		config := pm.config.ProviderConfigs[ProviderAzureOpenAI]
		if config == nil {
			useAzureAD := os.Getenv("AZURE_OPENAI_USE_AD") == "true"
			config = &ProviderConfig{
				Endpoint:        azureEndpoint,
				AzureDeployment: os.Getenv("AZURE_OPENAI_DEPLOYMENT"),
				AzureAPIVersion: os.Getenv("AZURE_OPENAI_API_VERSION"),
				Timeout:         60 * time.Second,
				UseAzureAD:      useAzureAD,
			}
			// Check for API key if not using Azure AD
			if !useAzureAD {
				if azureAPIKey := os.Getenv("AZURE_OPENAI_API_KEY"); azureAPIKey != "" {
					config.APIKey = azureAPIKey
				} else {
					// No API key and not using Azure AD - default to Azure AD
					config.UseAzureAD = true
				}
			}
		}

		// Only initialize if we have both endpoint and deployment
		if config.Endpoint != "" && config.AzureDeployment != "" {
			provider, err := providers.NewAzureOpenAIProvider(config)
			if err != nil {
				return errors.NewError(
					errors.ErrorCodeValidationFailed,
					"failed to initialize Azure OpenAI provider",
					errors.SeverityError,
					errors.CategoryValidation,
				).WithInnerError(err).WithSuggestion("verify Azure OpenAI endpoint and deployment configuration")
			}
			pm.providers[ProviderAzureOpenAI] = provider
		}
	}

	// Ensure we have at least one provider
	if len(pm.providers) == 0 {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"no AI providers available",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("set ANTHROPIC_API_KEY, OPENAI_API_KEY, or AZURE_OPENAI_ENDPOINT with AZURE_OPENAI_DEPLOYMENT")
	}

	// Adjust default/fallback if not available, following priority order:
	// 1. Azure OpenAI (default)
	// 2. Anthropic Claude (fallback)
	// 3. OpenAI (last resort)
	providerPriority := []ProviderType{ProviderAzureOpenAI, ProviderClaude, ProviderOpenAI}

	// Set default to first available provider in priority order
	if _, exists := pm.providers[pm.defaultType]; !exists {
		for _, providerType := range providerPriority {
			if _, exists := pm.providers[providerType]; exists {
				pm.defaultType = providerType
				break
			}
		}
	}

	// Set fallback to next available provider in priority order
	if pm.config.EnableFallback {
		if _, exists := pm.providers[pm.fallbackType]; !exists {
			foundDefault := false
			for _, providerType := range providerPriority {
				if _, exists := pm.providers[providerType]; exists {
					if providerType == pm.defaultType {
						foundDefault = true
						continue
					}
					if foundDefault {
						pm.fallbackType = providerType
						break
					}
				}
			}
		}
	}

	return nil
}

// GetProvider returns a specific provider by type
func (pm *ProviderManager) GetProvider(providerType ProviderType) (Provider, error) {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	provider, exists := pm.providers[providerType]
	if !exists {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("provider %s not available", providerType),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check available providers with ListProviders()")
	}

	return provider, nil
}

// GetDefaultProvider returns the default provider
func (pm *ProviderManager) GetDefaultProvider() (Provider, error) {
	return pm.GetProvider(pm.defaultType)
}

// ListProviders returns all available provider types
func (pm *ProviderManager) ListProviders() []ProviderType {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	types := make([]ProviderType, 0, len(pm.providers))
	for providerType := range pm.providers {
		types = append(types, providerType)
	}
	return types
}

// Analyze routes the analysis request to the appropriate provider with multi-provider failover
func (pm *ProviderManager) Analyze(ctx context.Context, req *AnalysisRequest) (*AnalysisResponse, error) {
	// Get prioritized list of providers to try
	providersToTry := pm.getPrioritizedProviders(req)

	var allErrors []string
	var primaryError error

	// Try each provider in priority order
	for i, providerType := range providersToTry {
		isPrimary := i == 0

		// Check health cache before attempting (skip known-unhealthy providers)
		healthReason := pm.getProviderHealthReason(ctx, providerType)
		if healthReason != "" {
			fmt.Printf("⚠️  Skipping unhealthy provider: %s (%s)\n", providerType, healthReason)
			// If this was the primary provider, record that it was skipped
			if isPrimary {
				primaryError = fmt.Errorf("primary provider %s marked as unhealthy: %s", providerType, healthReason)
			}
			allErrors = append(allErrors, fmt.Sprintf("%s: skipped (%s)", providerType, healthReason))
			continue
		}

		// Log attempt
		if isPrimary {
			fmt.Printf("🔄 Attempting primary AI provider: %s\n", providerType)
		} else {
			fmt.Printf("🔄 Attempting fallback provider %d: %s\n", i, providerType)
		}

		// Try the provider
		response, err := pm.analyzeWithProvider(ctx, req, providerType)
		if err == nil {
			// Success!
			if !isPrimary {
				// Add metadata indicating fallback was used
				if response.Metadata == nil {
					response.Metadata = make(map[string]interface{})
				}
				response.Metadata["fallback_used"] = true
				response.Metadata["provider_used"] = string(providerType)
				// Safely add primary error message if available
				if primaryError != nil {
					response.Metadata["primary_error"] = primaryError.Error()
				} else {
					response.Metadata["primary_error"] = "primary provider was not available"
				}
				fmt.Printf("☑ AI provider %s succeeded (fallback)\n", providerType)
			} else {
				fmt.Printf("☑ AI provider %s succeeded\n", providerType)
			}
			return response, nil
		}

		// Provider failed - save error and continue
		fmt.Printf("⚠️  Provider %s failed: %v\n", providerType, err)
		if isPrimary {
			primaryError = err
		}
		allErrors = append(allErrors, fmt.Sprintf("%s: %v", providerType, err))

		// Mark provider as unhealthy in cache
		pm.updateHealthCache(providerType, false)
	}

	// All providers failed
	if primaryError != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			fmt.Sprintf("all %d providers failed", len(providersToTry)),
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(primaryError).WithAdditional("all_errors", allErrors).WithSuggestion("check AI provider connectivity and credentials")
	}
	return nil, errors.NewError(
		errors.ErrorCodeValidationFailed,
		fmt.Sprintf("all %d providers failed", len(providersToTry)),
		errors.SeverityError,
		errors.CategoryValidation,
	).WithAdditional("all_errors", allErrors).WithSuggestion("check AI provider connectivity and credentials")
}

// analyzeWithProvider performs analysis with a specific provider with retry logic
func (pm *ProviderManager) analyzeWithProvider(ctx context.Context, req *AnalysisRequest, providerType ProviderType) (*AnalysisResponse, error) {
	provider, err := pm.GetProvider(providerType)
	if err != nil {
		return nil, err
	}

	// Implement retry logic
	var lastErr error
	delay := pm.config.RetrySettings.InitialDelay

	for attempt := 0; attempt <= pm.config.RetrySettings.MaxRetries; attempt++ {
		if attempt > 0 {
			// Wait before retry
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}

			// Exponential backoff
			delay = time.Duration(float64(delay) * pm.config.RetrySettings.BackoffFactor)
			if delay > pm.config.RetrySettings.MaxDelay {
				delay = pm.config.RetrySettings.MaxDelay
			}
		}

		response, err := provider.Analyze(ctx, req)
		if err == nil {
			// Add retry metadata if this wasn't the first attempt
			if attempt > 0 {
				if response.Metadata == nil {
					response.Metadata = make(map[string]interface{})
				}
				response.Metadata["retry_attempts"] = attempt
			}
			return response, nil
		}

		lastErr = err

		// Don't retry on context cancellation
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	return nil, errors.NewError(
		errors.ErrorCodeAnalysisError,
		fmt.Sprintf("analysis failed after %d retries", pm.config.RetrySettings.MaxRetries),
		errors.SeverityError,
		errors.CategoryValidation,
	).WithInnerError(lastErr).WithSuggestion("check AI provider connectivity and rate limits")
}

// selectProvider chooses the best provider for a given request
func (pm *ProviderManager) selectProvider(req *AnalysisRequest) ProviderType {
	// Check for preferred provider for this analysis type
	if preferred, exists := pm.config.PreferredProviders[req.Type]; exists {
		if _, providerExists := pm.providers[preferred]; providerExists {
			return preferred
		}
	}

	// Use default provider
	return pm.defaultType
}

// AnalyzeWithTools routes tool-enhanced analysis requests
func (pm *ProviderManager) AnalyzeWithTools(ctx context.Context, req *AnalysisRequest, tools []*ToolDefinition) (*AnalysisResponse, error) {
	providerType := pm.selectProvider(req)

	provider, err := pm.GetProvider(providerType)
	if err != nil {
		return nil, err
	}

	// Check if provider supports tools
	if toolProvider, ok := provider.(providers.ToolUseProvider); ok {
		return toolProvider.AnalyzeWithTools(ctx, req, tools)
	}

	// Fallback to regular analysis if tools aren't supported
	return provider.Analyze(ctx, req)
}

// SubmitBatch routes batch processing requests to providers that support it
func (pm *ProviderManager) SubmitBatch(ctx context.Context, batch *BatchRequest) (*BatchResponse, error) {
	// Find the first provider that supports batch processing
	for _, provider := range pm.providers {
		if provider.SupportsBatch() {
			return provider.SubmitBatch(ctx, batch)
		}
	}

	return nil, errors.NewError(
		errors.ErrorCodeValidationFailed,
		"no providers support batch processing",
		errors.SeverityError,
		errors.CategoryValidation,
	).WithSuggestion("use a provider that supports batch processing (e.g., Claude)")
}

// GetBatchStatus retrieves batch status from all providers
func (pm *ProviderManager) GetBatchStatus(ctx context.Context, batchID string) (*BatchResponse, error) {
	// Try all providers that support batch processing
	for _, provider := range pm.providers {
		if provider.SupportsBatch() {
			response, err := provider.GetBatchStatus(ctx, batchID)
			if err == nil {
				return response, nil
			}
		}
	}

	return nil, errors.NewError(
		errors.ErrorCodeValidationFailed,
		"batch not found or no batch-supporting providers available",
		errors.SeverityError,
		errors.CategoryValidation,
	).WithSuggestion("verify batch ID and ensure batch provider is available")
}

// HealthCheck performs health checks on all providers
func (pm *ProviderManager) HealthCheck(ctx context.Context) map[ProviderType]error {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	results := make(map[ProviderType]error)
	for providerType, provider := range pm.providers {
		results[providerType] = provider.HealthCheck(ctx)
	}

	return results
}

// GetCapabilities returns capabilities for all providers
func (pm *ProviderManager) GetCapabilities() map[ProviderType]providers.ProviderCapabilities {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	capabilities := make(map[ProviderType]providers.ProviderCapabilities)

	for providerType, provider := range pm.providers {
		caps := providers.ProviderCapabilities{
			SupportedTypes:    provider.SupportedTypes(),
			SupportsStreaming: provider.SupportsStreaming(),
			SupportsTools:     provider.SupportsTools(),
			SupportsBatch:     provider.SupportsBatch(),
		}
		capabilities[providerType] = caps
	}

	return capabilities
}
