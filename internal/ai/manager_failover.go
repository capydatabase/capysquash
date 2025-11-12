package ai

import (
	"context"
	"time"
)

// getPrioritizedProviders returns a prioritized list of providers to try for a request
func (pm *ProviderManager) getPrioritizedProviders(req *AnalysisRequest) []ProviderType {
	pm.mutex.RLock()
	defer pm.mutex.RUnlock()

	// Start with preferred provider for this analysis type (if configured)
	var priority []ProviderType
	if preferred, exists := pm.config.PreferredProviders[req.Type]; exists {
		if _, providerExists := pm.providers[preferred]; providerExists {
			priority = append(priority, preferred)
		}
	}

	// Add default provider if not already added
	if len(priority) == 0 || priority[0] != pm.defaultType {
		if _, exists := pm.providers[pm.defaultType]; exists {
			priority = append(priority, pm.defaultType)
		}
	}

	// Add all other available providers (excluding those already added)
	seen := make(map[ProviderType]bool)
	for _, p := range priority {
		seen[p] = true
	}

	for providerType := range pm.providers {
		if !seen[providerType] {
			priority = append(priority, providerType)
		}
	}

	return priority
}

// isProviderHealthy checks if a provider is healthy (uses cache)
func (pm *ProviderManager) isProviderHealthy(ctx context.Context, providerType ProviderType) bool {
	pm.mutex.RLock()
	cache := pm.healthCache[providerType]
	pm.mutex.RUnlock()

	// Check cache
	if cache != nil && time.Since(cache.lastCheck) < cache.ttl {
		return cache.healthy
	}

	// Cache miss or expired - perform health check
	pm.mutex.RLock()
	provider, exists := pm.providers[providerType]
	pm.mutex.RUnlock()

	if !exists {
		return false
	}

	// Perform health check
	err := provider.HealthCheck(ctx)
	healthy := err == nil

	// Capture error message for better diagnostics
	var errorMsg string
	if err != nil {
		errorMsg = err.Error()
	}

	// Update cache
	pm.mutex.Lock()
	pm.healthCache[providerType] = &HealthCache{
		healthy:   healthy,
		lastCheck: time.Now(),
		ttl:       5 * time.Minute, // Cache health status for 5 minutes
		lastError: errorMsg,
	}
	pm.mutex.Unlock()

	return healthy
}

// updateHealthCache updates the health status of a provider in the cache
func (pm *ProviderManager) updateHealthCache(providerType ProviderType, healthy bool) {
	pm.mutex.Lock()
	defer pm.mutex.Unlock()

	pm.healthCache[providerType] = &HealthCache{
		healthy:   healthy,
		lastCheck: time.Now(),
		ttl:       5 * time.Minute,
		lastError: "", // Clear error on update
	}
}

// getProviderHealthReason returns the reason why a provider is unhealthy, or empty string if healthy
func (pm *ProviderManager) getProviderHealthReason(ctx context.Context, providerType ProviderType) string {
	pm.mutex.RLock()
	cache := pm.healthCache[providerType]
	pm.mutex.RUnlock()

	// Check cache first
	if cache != nil && time.Since(cache.lastCheck) < cache.ttl {
		if !cache.healthy {
			return cache.lastError
		}
		return ""
	}

	// Cache miss or expired - perform health check
	if !pm.isProviderHealthy(ctx, providerType) {
		// After health check, cache should be updated
		pm.mutex.RLock()
		updatedCache := pm.healthCache[providerType]
		pm.mutex.RUnlock()

		if updatedCache != nil && !updatedCache.healthy {
			return updatedCache.lastError
		}
		return "health check failed"
	}

	return ""
}
