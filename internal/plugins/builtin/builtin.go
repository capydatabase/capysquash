// Package builtin registers the built-in plugin set. It lives outside
// internal/plugins because the plugin implementations import that package
// for the Plugin interface, so the registry cannot import them back.
package builtin

import (
	"github.com/capydatabase/capysquash/internal/plugins"
	"github.com/capydatabase/capysquash/internal/plugins/clerk"
	"github.com/capydatabase/capysquash/internal/plugins/drizzle"
	"github.com/capydatabase/capysquash/internal/plugins/prisma"
	"github.com/capydatabase/capysquash/internal/plugins/supabase"
)

// RegisterDefault registers Clerk, Supabase, Prisma and Drizzle with the
// global plugin registry. Registration is idempotent.
func RegisterDefault() error {
	for _, plugin := range []plugins.Plugin{
		clerk.NewClerkPlugin(),
		supabase.NewSupabasePlugin(),
		prisma.NewPrismaPlugin(),
		drizzle.NewDrizzlePlugin(),
	} {
		if err := plugins.Register(plugin); err != nil {
			return err
		}
	}
	return nil
}
