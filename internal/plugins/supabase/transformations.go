package supabase

import (
	"context"

	"github.com/capysquash/pgsquash-engine/internal/plugins/auth"
	"github.com/capysquash/pgsquash-engine/internal/plugins/volatility"
)

// InjectCompatibilityLayer returns SQL to mock Supabase authentication for validation
// This creates:
//   - auth schema
//   - auth.users table (stub for foreign key references)
//   - Supabase roles (anon, authenticated, service_role)
//   - Mock auth.uid() function
//   - Mock auth.jwt() function
//   - Supabase Realtime publication
func (sp *SupabasePlugin) InjectCompatibilityLayer(ctx context.Context) string {
    generator := auth.NewCompatibilityGenerator(auth.ServiceSupabase)
    return generator.Generate()
}

// TransformSQL performs Supabase-specific SQL transformations
// Currently delegates to FixFunctionVolatility
func (sp *SupabasePlugin) TransformSQL(ctx context.Context, sql string) (string, error) {
    // Apply function volatility fixes for Supabase auth functions
    return sp.FixFunctionVolatility(ctx, sql)
}

// FixFunctionVolatility adds STABLE markers to Supabase auth functions
// Uses AST-based parsing for more accurate and maintainable transformations
func (sp *SupabasePlugin) FixFunctionVolatility(ctx context.Context, functionSQL string) (string, error) {
    registry := volatility.CreateSupabaseRegistry()
    fixer := volatility.NewASTVolatilityFixer(registry)
    return fixer.Fix(functionSQL)
}

