package plugins

import (
	"context"
	"fmt"
	"strings"

	internal_plugins "github.com/capy-base/pgsquash-engine/internal/plugins"
	"github.com/capy-base/pgsquash-engine/internal/plugins/clerk"
	"github.com/capy-base/pgsquash-engine/internal/plugins/drizzle"
	"github.com/capy-base/pgsquash-engine/internal/plugins/prisma"
	"github.com/capy-base/pgsquash-engine/internal/plugins/supabase"
	"github.com/capy-base/pgsquash-engine/internal/parser"
	"github.com/capy-base/pgsquash-engine/internal/types"
)

// PluginInfo describes a plugin as reported by the plugin implementation itself.
type PluginInfo struct {
	Name               string   `json:"name"`
	Priority           int      `json:"priority"`
	ConflictsWith      []string `json:"conflicts_with"`
	RequiredExtensions []string `json:"required_extensions"`
	Detected           bool     `json:"detected"`
}

// DetectionResult contains the results of plugin detection
type DetectionResult struct {
	Detected []PluginInfo        `json:"detected"`
	Count    int                 `json:"count"`
	Details  map[string][]string `json:"details"` // Plugin name -> detected patterns
}

// CompatibilityMatrix describes plugin compatibility after priority-based
// conflict resolution, matching the resolution the squashing engine applies.
type CompatibilityMatrix struct {
	Compatible   []string          `json:"compatible"`
	Incompatible []string          `json:"incompatible"`
	Warnings     []string          `json:"warnings"`
	Details      map[string]string `json:"details"` // Plugin name -> compatibility note
}

// builtinPlugins returns fresh instances of all built-in plugins.
// This is the single source of truth for the built-in plugin set; both
// RegisterDefault and the detection/compatibility APIs derive from it.
func builtinPlugins() []internal_plugins.Plugin {
	return []internal_plugins.Plugin{
		clerk.NewClerkPlugin(),
		supabase.NewSupabasePlugin(),
		prisma.NewPrismaPlugin(),
		drizzle.NewDrizzlePlugin(),
	}
}

// pluginInfo builds PluginInfo from a live plugin instance.
func pluginInfo(p internal_plugins.Plugin) PluginInfo {
	return PluginInfo{
		Name:               p.Name(),
		Priority:           p.Priority(),
		ConflictsWith:      p.GetConflictingPlugins(),
		RequiredExtensions: p.GetRequiredExtensions(),
	}
}

// GetAvailablePlugins returns information about all built-in plugins,
// derived from the plugin implementations themselves.
func GetAvailablePlugins() []PluginInfo {
	builtins := builtinPlugins()
	infos := make([]PluginInfo, 0, len(builtins))
	for _, p := range builtins {
		infos = append(infos, pluginInfo(p))
	}
	return infos
}

// DetectPlugins analyzes SQL migrations and detects which plugins are applicable.
//
// Migrations are parsed with the same parser the squashing engine uses, and each
// built-in plugin's own Detect implementation runs over the parsed statements —
// so detection results here always agree with the plugins activated during an
// actual squash.
func DetectPlugins(ctx context.Context, migrations []string) (*DetectionResult, error) {
	parsed := make([]*types.Migration, 0, len(migrations))
	for i, sql := range migrations {
		migration, err := parser.ParseMigrationWithContext(ctx, sql, fmt.Sprintf("migration_%03d.sql", i+1))
		if err != nil && migration == nil {
			return nil, fmt.Errorf("failed to parse migration %d for plugin detection: %w", i+1, err)
		}
		// A non-nil migration with recoverable parse errors still carries the
		// successfully parsed statements, which is enough for detection.
		migration.Sequence = i + 1
		parsed = append(parsed, migration)
	}

	combinedSQL := strings.Join(migrations, "\n")

	result := &DetectionResult{
		Detected: []PluginInfo{},
		Details:  make(map[string][]string),
	}

	for _, plugin := range builtinPlugins() {
		if !plugin.Detect(parsed) {
			continue
		}

		info := pluginInfo(plugin)
		info.Detected = true
		result.Detected = append(result.Detected, info)

		patterns := plugin.DetectPatterns(combinedSQL)
		names := make([]string, 0, len(patterns))
		for _, pattern := range patterns {
			names = append(names, pattern.Name)
		}
		result.Details[plugin.Name()] = names
	}

	result.Count = len(result.Detected)
	return result, nil
}

// CheckCompatibility checks compatibility between plugins using the same
// priority-based conflict resolution the plugin registry applies during
// squashing: each plugin's GetConflictingPlugins() is honored and, on
// conflict, the higher-priority plugin wins (e.g. Clerk 95 excludes Supabase 90).
func CheckCompatibility(pluginNames []string) (*CompatibilityMatrix, error) {
	available := make(map[string]internal_plugins.Plugin)
	for _, p := range builtinPlugins() {
		available[p.Name()] = p
	}

	selected := make([]internal_plugins.Plugin, 0, len(pluginNames))
	seen := make(map[string]bool, len(pluginNames))
	for _, name := range pluginNames {
		plugin, ok := available[name]
		if !ok {
			return nil, fmt.Errorf("unknown plugin: %s", name)
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		selected = append(selected, plugin)
	}

	active := internal_plugins.ResolveConflicts(selected)
	activeByName := make(map[string]internal_plugins.Plugin, len(active))

	matrix := &CompatibilityMatrix{
		Compatible:   []string{},
		Incompatible: []string{},
		Warnings:     []string{},
		Details:      make(map[string]string),
	}

	for _, plugin := range active {
		activeByName[plugin.Name()] = plugin
		matrix.Compatible = append(matrix.Compatible, plugin.Name())
		matrix.Details[plugin.Name()] = fmt.Sprintf("active (priority %d)", plugin.Priority())
	}

	for _, plugin := range selected {
		if _, ok := activeByName[plugin.Name()]; ok {
			continue
		}

		winner := conflictWinner(plugin.Name(), active)
		matrix.Incompatible = append(matrix.Incompatible, plugin.Name())
		matrix.Details[plugin.Name()] = fmt.Sprintf(
			"excluded: conflicts with %s (priority %d > %d)",
			winner.Name(), winner.Priority(), plugin.Priority(),
		)
		matrix.Warnings = append(matrix.Warnings, fmt.Sprintf(
			"%s conflicts with higher-priority plugin %s and will be excluded during squashing",
			plugin.Name(), winner.Name(),
		))
	}

	return matrix, nil
}

// conflictWinner finds the active plugin whose conflict list excluded the named
// plugin. Falls back to the highest-priority active plugin, which can only
// happen if resolution semantics change without this function being updated.
func conflictWinner(excluded string, active []internal_plugins.Plugin) internal_plugins.Plugin {
	for _, plugin := range active {
		for _, conflict := range plugin.GetConflictingPlugins() {
			if conflict == excluded {
				return plugin
			}
		}
	}
	// active is sorted by priority (highest first) by ResolveConflicts.
	return active[0]
}
