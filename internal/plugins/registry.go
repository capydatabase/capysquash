package plugins

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/capy-base/pgsquash-engine/internal/errors"
	"github.com/capy-base/pgsquash-engine/internal/types"
	"github.com/capy-base/pgsquash-engine/internal/utils"
)

// Registry manages the lifecycle of all plugins
type Registry struct {
	plugins       []Plugin
	activePlugins []Plugin
	pluginsByName map[string]Plugin
	mu            sync.RWMutex
	initialized   bool
}

// NewRegistry creates a new plugin registry
func NewRegistry() *Registry {
	return &Registry{
		plugins:       []Plugin{},
		activePlugins: []Plugin{},
		pluginsByName: make(map[string]Plugin),
		initialized:   false,
	}
}

// Register adds a plugin to the registry.
//
// Registration is idempotent by plugin name: registering a plugin whose name
// is already present is a no-op. This allows every entry point (CLI main,
// pkg/engine construction, library callers) to ensure the default plugin set
// is registered without coordinating a single init site.
func (r *Registry) Register(plugin Plugin) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	name := plugin.Name()
	if _, exists := r.pluginsByName[name]; exists {
		return nil
	}

	r.plugins = append(r.plugins, plugin)
	r.pluginsByName[name] = plugin

	// Plugin registration logging removed to reduce noise
	// Plugins log when discovered/initialized during actual operations
	return nil
}

// DiscoverAndInitialize detects applicable plugins from migrations and initializes them
// This is the main entry point called by the squashing engine
func (r *Registry) DiscoverAndInitialize(ctx context.Context, migrations []*types.Migration, config map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.initialized {
		return nil // Already initialized
	}

	utils.GetDefaultLogger().WithPrefix("PLUGINS").Info("[plugins] Discovering plugins from %d migrations...", len(migrations))

	// Phase 1: Detection - find which plugins are applicable
	var detectedPlugins []Plugin
	for _, plugin := range r.plugins {
		pluginName := plugin.Name()

		// Check if plugin is explicitly disabled in config
		if !r.isPluginEnabled(pluginName, config) {
			utils.GetDefaultLogger().WithPrefix("PLUGINS").Info("[plugins] Skipping %s (disabled in config)", pluginName)
			continue
		}

		if plugin.Detect(migrations) {
			detectedPlugins = append(detectedPlugins, plugin)
			utils.GetDefaultLogger().WithPrefix("PLUGINS").Info("[plugins] Detected: %s", pluginName)
		}
	}

	// Phase 2: Conflict resolution - remove lower priority conflicting plugins
	resolvedPlugins := r.resolveConflicts(detectedPlugins)

	// Phase 3: Initialization - configure each active plugin
	for _, plugin := range resolvedPlugins {
		pluginConfig := r.getPluginConfig(plugin.Name(), config)
		if err := plugin.Initialize(ctx, pluginConfig); err != nil {
			utils.GetDefaultLogger().WithPrefix("PLUGINS").Info("[plugins] Warning: Failed to initialize %s: %v", plugin.Name(), err)
			continue // Skip failed plugins but continue with others
		}
		r.activePlugins = append(r.activePlugins, plugin)
		utils.GetDefaultLogger().WithPrefix("PLUGINS").Info("[plugins] Initialized: %s", plugin.Name())
	}

	// Sort by priority (highest first)
	sort.Slice(r.activePlugins, func(i, j int) bool {
		return r.activePlugins[i].Priority() > r.activePlugins[j].Priority()
	})

	r.initialized = true
	utils.GetDefaultLogger().WithPrefix("PLUGINS").Info("[plugins] Activated %d plugins: %v", len(r.activePlugins), r.getPluginNames(r.activePlugins))

	return nil
}

// resolveConflicts removes lower-priority plugins that conflict with higher-priority ones
func (r *Registry) resolveConflicts(plugins []Plugin) []Plugin {
	// Sort by priority (highest first) for conflict resolution
	sorted := make([]Plugin, len(plugins))
	copy(sorted, plugins)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Priority() > sorted[j].Priority()
	})

	excluded := make(map[string]bool)
	var resolved []Plugin

	for _, plugin := range sorted {
		if excluded[plugin.Name()] {
			utils.GetDefaultLogger().WithPrefix("PLUGINS").Info("[plugins] Excluding %s due to conflict", plugin.Name())
			continue
		}

		// Mark conflicting plugins as excluded
		for _, conflictName := range plugin.GetConflictingPlugins() {
			if !excluded[conflictName] {
				utils.GetDefaultLogger().WithPrefix("PLUGINS").Info("[plugins] %s conflicts with %s (lower priority)", conflictName, plugin.Name())
				excluded[conflictName] = true
			}
		}

		resolved = append(resolved, plugin)
	}

	return resolved
}

// isPluginEnabled checks if a plugin is enabled in the configuration
// Checks third_party_integrations.{plugin}_integration.enabled
// Returns true if config is nil or if enabled field is not found (default: enabled)
func (r *Registry) isPluginEnabled(pluginName string, config map[string]any) bool {
	if config == nil {
		return true // Default: all plugins enabled if no config
	}

	// Check third_party_integrations.{plugin}_integration.enabled
	if integrations, ok := config["third_party_integrations"].(map[string]any); ok {
		integrationKey := pluginName + "_integration"
		if integration, ok := integrations[integrationKey].(map[string]any); ok {
			if enabled, ok := integration["enabled"].(bool); ok {
				return enabled
			}
		}
	}

	// Also check plugins.disabled_plugins list
	if pluginSettings, ok := config["plugins"].(map[string]any); ok {
		if disabledList, ok := pluginSettings["disabled_plugins"].([]any); ok {
			for _, disabled := range disabledList {
				if disabledName, ok := disabled.(string); ok && disabledName == pluginName {
					return false
				}
			}
		}
	}

	return true // Default: enabled if not explicitly disabled
}

// getPluginConfig extracts plugin-specific config from global config map
func (r *Registry) getPluginConfig(pluginName string, config map[string]any) any {
	if config == nil {
		return nil
	}

	// Look for plugin-specific config under "plugins.{name}" or "{name}_plugin"
	if pluginConfig, ok := config[pluginName]; ok {
		return pluginConfig
	}

	if plugins, ok := config["plugins"].(map[string]any); ok {
		if pluginConfig, ok := plugins[pluginName]; ok {
			return pluginConfig
		}
	}

	return nil
}

// getPluginNames extracts names from plugin list for logging
func (r *Registry) getPluginNames(plugins []Plugin) []string {
	names := make([]string, len(plugins))
	for i, p := range plugins {
		names[i] = p.Name()
	}
	return names
}

// ActivePlugins returns the list of currently active plugins (sorted by priority)
func (r *Registry) ActivePlugins() []Plugin {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activePlugins
}

// GetPlugin retrieves a plugin by name
func (r *Registry) GetPlugin(name string) (Plugin, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	plugin, exists := r.pluginsByName[name]
	return plugin, exists
}

// IsActive checks if a plugin is currently active
func (r *Registry) IsActive(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.activePlugins {
		if p.Name() == name {
			return true
		}
	}
	return false
}

// EnrichStatement calls EnrichStatement on all active plugins
// Plugins are called in priority order (highest first)
func (r *Registry) EnrichStatement(ctx context.Context, stmt *types.Statement) error {
	for _, plugin := range r.ActivePlugins() {
		if err := plugin.EnrichStatement(ctx, stmt); err != nil {
			utils.GetDefaultLogger().WithPrefix("PLUGINS").Info("[plugins] Warning: %s.EnrichStatement failed: %v", plugin.Name(), err)
			// Continue with other plugins even if one fails
		}
	}
	return nil
}

// TransformSQL calls TransformSQL on all active plugins
// Plugins are called in priority order, each transforming the output of the previous
func (r *Registry) TransformSQL(ctx context.Context, sql string) (string, error) {
	transformed := sql
	var err error

	for _, plugin := range r.ActivePlugins() {
		transformed, err = plugin.TransformSQL(ctx, transformed)
		if err != nil {
			return sql, errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation,
				"plugin transformation failed",
				map[string]any{"plugin": plugin.Name()})
		}
	}

	return transformed, nil
}

// InjectCompatibilityLayer aggregates compatibility SQL from all active plugins
func (r *Registry) InjectCompatibilityLayer(ctx context.Context) string {
	var layers []string

	for _, plugin := range r.ActivePlugins() {
		layer := plugin.InjectCompatibilityLayer(ctx)
		if layer != "" {
			layers = append(layers, fmt.Sprintf("-- Compatibility layer for %s\n%s", plugin.Name(), layer))
		}
	}

	if len(layers) == 0 {
		return ""
	}

	return "-- Auto-generated plugin compatibility layers\n\n" + strings.Join(layers, "\n\n")
}

// GetRequiredExtensions aggregates required extensions from all active plugins
func (r *Registry) GetRequiredExtensions() []string {
	extensionSet := make(map[string]bool)

	for _, plugin := range r.ActivePlugins() {
		for _, ext := range plugin.GetRequiredExtensions() {
			extensionSet[ext] = true
		}
	}

	extensions := make([]string, 0, len(extensionSet))
	for ext := range extensionSet {
		extensions = append(extensions, ext)
	}

	sort.Strings(extensions)
	return extensions
}

// GetConsolidationRules aggregates consolidation rules from all active plugins
func (r *Registry) GetConsolidationRules() []ConsolidationRule {
	var allRules []ConsolidationRule

	for _, plugin := range r.ActivePlugins() {
		rules := plugin.GetConsolidationRules()
		allRules = append(allRules, rules...)
	}

	// Sort by priority (highest first)
	sort.Slice(allRules, func(i, j int) bool {
		return allRules[i].Priority > allRules[j].Priority
	})

	return allRules
}

// ShouldPreserve checks if any active plugin wants to preserve this statement
func (r *Registry) ShouldPreserve(stmt *types.Statement) bool {
	for _, plugin := range r.ActivePlugins() {
		if plugin.ShouldPreserve(stmt) {
			utils.GetDefaultLogger().WithPrefix("PLUGINS").Info("[plugins] %s marked statement for preservation: %s", plugin.Name(), stmt.ObjectName)
			return true
		}
	}
	return false
}

// ValidateSchema calls ValidateSchema on all active plugins
func (r *Registry) ValidateSchema(ctx context.Context, db *sql.DB) error {
	for _, plugin := range r.ActivePlugins() {
		if err := plugin.ValidateSchema(ctx, db); err != nil {
			return errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation,
				"plugin validation failed",
				map[string]any{"plugin": plugin.Name()})
		}
	}
	return nil
}

// Reset clears all active plugins (useful for testing)
func (r *Registry) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.activePlugins = []Plugin{}
	r.initialized = false
}

// Global registry instance
var globalRegistry = NewRegistry()

// GlobalRegistry returns the global plugin registry
func GlobalRegistry() *Registry {
	return globalRegistry
}

// Register adds a plugin to the global registry (convenience function)
func Register(plugin Plugin) error {
	return globalRegistry.Register(plugin)
}
