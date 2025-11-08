// Package plugins provides a public API for pgsquash plugin management.
//
// This package exports plugin registration and detection functionality for use
// by external tools while keeping the internal plugin implementation details private.
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
//	    "ALTER TABLE users ADD COLUMN clerk_user_id TEXT;",
//	}
//
//	result, err := plugins.DetectPlugins(ctx, migrations)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	for _, plugin := range result.Detected {
//	    fmt.Printf("Detected: %s (%s)\n", plugin.Name, plugin.Description)
//	}
//
// # Compatibility Checking
//
// Check compatibility between detected plugins:
//
//	pluginNames := []string{"supabase", "clerk"}
//	matrix, err := plugins.CheckCompatibility(pluginNames)
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
//	allPlugins := plugins.GetAvailablePlugins()
//	for _, plugin := range allPlugins {
//	    fmt.Printf("%s v%s: %s\n", plugin.Name, plugin.Version, plugin.Description)
//	}
package plugins

import (
	internal_plugins "github.com/capysquash/pgsquash-engine/internal/plugins"
	"github.com/capysquash/pgsquash-engine/internal/plugins/clerk"
	"github.com/capysquash/pgsquash-engine/internal/plugins/drizzle"
	"github.com/capysquash/pgsquash-engine/internal/plugins/prisma"
	"github.com/capysquash/pgsquash-engine/internal/plugins/supabase"
)

// RegisterDefault registers all built-in pgsquash plugins.
// This includes plugins for popular platforms and ORMs:
//   - Supabase (RLS policies, storage, auth)
//   - Clerk (JWT v2 auth)
//   - Prisma (ORM migrations)
//   - Drizzle (ORM migrations)
//
// This should be called during application initialization, typically in init().
// Registration failures are logged as warnings but don't prevent startup.
//
// Example:
//   if err := plugins.RegisterDefault(); err != nil {
//       log.Printf("Warning: Some plugins failed to register: %v", err)
//   }
func RegisterDefault() error {
	// Register auth service plugins
	if err := internal_plugins.Register(clerk.NewClerkPlugin()); err != nil {
		return err
	}

	if err := internal_plugins.Register(supabase.NewSupabasePlugin()); err != nil {
		return err
	}

	// Register ORM plugins
	if err := internal_plugins.Register(prisma.NewPrismaPlugin()); err != nil {
		return err
	}

	if err := internal_plugins.Register(drizzle.NewDrizzlePlugin()); err != nil {
		return err
	}

	return nil
}

// Note: Custom plugin registration is not exposed in the public API.
// The Plugin interface is complex and considered internal.
// If you need custom plugin support, please file an issue at:
// https://github.com/capysquash/pgsquash-engine/issues
