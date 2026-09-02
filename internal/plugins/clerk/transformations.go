package clerk

import (
	"context"

	"github.com/capysquash/pgsquash-engine/internal/plugins/auth"
	"github.com/capysquash/pgsquash-engine/internal/plugins/volatility"
)

// InjectCompatibilityLayer returns SQL to mock Clerk authentication for validation
// This creates:
//   - auth schema
//   - Common Clerk/Supabase roles (anon, authenticated, service_role)
//   - Mock auth.jwt() function returning Clerk JWT v2 payload
//   - Helper functions (current_user_id, current_organization_id, etc.)
func (cp *ClerkPlugin) InjectCompatibilityLayer(ctx context.Context) string {
	generator := auth.NewCompatibilityGenerator(auth.ServiceClerk)
	return generator.Generate()
}

// TransformSQL performs Clerk-specific SQL transformations
// Currently delegates to FixFunctionVolatility
func (cp *ClerkPlugin) TransformSQL(ctx context.Context, sql string) (string, error) {
	// Apply function volatility fixes for Clerk auth functions
	return cp.FixFunctionVolatility(ctx, sql)
}

// FixFunctionVolatility adds STABLE markers to Clerk auth functions
// Uses AST-based parsing for more accurate and maintainable transformations
func (cp *ClerkPlugin) FixFunctionVolatility(ctx context.Context, functionSQL string) (string, error) {
	registry := volatility.CreateClerkRegistry()
	fixer := volatility.NewASTVolatilityFixer(registry)
	return fixer.Fix(functionSQL)
}
