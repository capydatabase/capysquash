// Package plugins provides a public API for pgsquash plugin management.
//
// This package exports plugin registration, detection, and compatibility
// functionality for use by external tools while keeping the internal plugin
// implementation details private. Detection and compatibility results are
// produced by the same plugin implementations and conflict-resolution logic
// the squashing engine uses, so they always agree with an actual squash run.
//
// # Available Plugins
//
// Built-in plugins:
//   - Supabase: RLS policy optimization, auth schema handling, storage integration
//   - Clerk: JWT v2 support, organization handling, user ID preservation
//   - Prisma: Migration table handling, shadow database optimizations
//   - Drizzle: Identity column preference, sequence optimization
//
// # Basic Usage
//
// Register all built-in plugins:
//
//	if err := plugins.RegisterDefault(); err != nil {
//	    log.Fatal(err)
//	}
//
// # Plugin Detection
//
// Detect which plugins are applicable to your migrations:
//
//	migrations := []string{
//	    "CREATE TABLE users (id SERIAL PRIMARY KEY);",
//	    "CREATE POLICY user_select ON users USING (auth.uid() = id);",
//	}
//
//	result, err := plugins.DetectPlugins(ctx, migrations)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	for _, plugin := range result.Detected {
//	    fmt.Printf("Detected: %s (priority %d)\n", plugin.Name, plugin.Priority)
//	}
//
// # Compatibility Checking
//
// Check compatibility between detected plugins. Conflicts are resolved by
// priority exactly as during squashing (e.g. Clerk 95 excludes Supabase 90):
//
//	matrix, err := plugins.CheckCompatibility([]string{"supabase", "clerk"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	for _, warning := range matrix.Warnings {
//	    log.Printf("Warning: %s\n", warning)
//	}
//
// # Plugin Information
//
// Get information about available plugins:
//
//	for _, plugin := range plugins.GetAvailablePlugins() {
//	    fmt.Printf("%s (priority %d)\n", plugin.Name, plugin.Priority)
//	}
package plugins

import (
	internal_plugins "github.com/capy-base/pgsquash-engine/internal/plugins"
)

// RegisterDefault registers all built-in pgsquash plugins with the global
// plugin registry:
//   - Clerk (JWT v2 auth)
//   - Supabase (RLS policies, storage, auth)
//   - Prisma (ORM migrations)
//   - Drizzle (ORM migrations)
//
// This should be called during application initialization, typically in init().
//
// Example:
//
//	if err := plugins.RegisterDefault(); err != nil {
//	    log.Printf("Warning: Some plugins failed to register: %v", err)
//	}
func RegisterDefault() error {
	for _, plugin := range builtinPlugins() {
		if err := internal_plugins.Register(plugin); err != nil {
			return err
		}
	}
	return nil
}

// Note: Custom plugin registration is not exposed in the public API.
// The Plugin interface is complex and considered internal.
// If you need custom plugin support, please file an issue at:
// https://github.com/capy-base/pgsquash-engine/issues
