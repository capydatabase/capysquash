// Package clerk provides Clerk authentication integration for pgsquash.
// It handles JWT v2 organization claims, user ID extraction, and RLS policy patterns.
package clerk

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/errors"
	"github.com/capy-base/pgsquash-engine/internal/plugins"
	"github.com/capy-base/pgsquash-engine/internal/types"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// ClerkPlugin implements Clerk authentication service integration
type ClerkPlugin struct {
	*plugins.BasePlugin
	config ClerkConfig
}

// Clerk-specific auth pattern identifiers
const (
	AuthPatternClerkJWTV2 = "clerk_jwt_v2" // Clerk JWT v2 with organization claims
	AuthPatternClerk      = "clerk_auth"   // Clerk generic patterns
)

// ClerkConfig holds Clerk-specific configuration
type ClerkConfig struct {
	Enabled               bool   `json:"enabled"`
	JWTVersion            string `json:"jwt_version"`             // "v1" or "v2"
	OrganizationSupport   bool   `json:"organization_support"`    // Enable org claim handling
	PublicMetadataSupport bool   `json:"public_metadata_support"` // Enable public metadata
}

// NewClerkPlugin creates a new Clerk plugin instance
func NewClerkPlugin() *ClerkPlugin {
	return &ClerkPlugin{
		BasePlugin: plugins.NewBasePlugin("clerk", 95), // High priority for auth
		config: ClerkConfig{
			Enabled:               true,
			JWTVersion:            "v2",
			OrganizationSupport:   true,
			PublicMetadataSupport: true,
		},
	}
}

// Detect checks if migrations contain Clerk patterns
// Detection patterns (in order of specificity):
//  1. JWT v2 organization claims: auth.jwt()->'o'->>'id'
//  2. JWT v1/generic patterns: auth.jwt() with 'sub'
//  3. Clerk helper functions: clerk_user_id(), clerk_is_admin()
func (cp *ClerkPlugin) Detect(migrations []*types.Migration) bool {
	for _, migration := range migrations {
		for _, stmt := range migration.Statements {
			sql := stmt.SQL

			// Pattern 1: JWT v2 organization patterns (most specific)
			if strings.Contains(sql, "auth.jwt()") && strings.Contains(sql, "'o'->>'id'") {
				return true
			}

			// Pattern 2: Clerk-specific helper functions
			clerkFunctions := []string{
				"clerk_user_id()",
				"clerk_is_admin()",
				"clerk_organization_id()",
				"current_organization_id()",
				"current_organization_role()",
			}
			for _, fn := range clerkFunctions {
				if strings.Contains(sql, fn) {
					return true
				}
			}

			// Pattern 3: Generic JWT with Clerk-like patterns
			// (Only if we find auth.jwt() with explicit claim access)
			if strings.Contains(sql, "auth.jwt()") &&
				(strings.Contains(sql, "->>'sub'") || strings.Contains(sql, "->>'email'")) {
				// Could be Clerk, but might also be Supabase or custom JWT
				// Return true only if no Supabase patterns detected
				if !cp.hasSupabasePatterns(sql) {
					return true
				}
			}
		}
	}

	return false
}

// hasSupabasePatterns checks if SQL contains Supabase-specific patterns
func (cp *ClerkPlugin) hasSupabasePatterns(sql string) bool {
	supabasePatterns := []string{
		"auth.uid()",
		"auth.users",
		"storage.objects",
		"storage.buckets",
	}

	for _, pattern := range supabasePatterns {
		if strings.Contains(sql, pattern) {
			return true
		}
	}

	return false
}

// Initialize configures the Clerk plugin with service-specific settings
func (cp *ClerkPlugin) Initialize(ctx context.Context, config any) error {
	if config == nil {
		// Use defaults
		return nil
	}

	// Type assertion to extract Clerk config
	if clerkCfg, ok := config.(ClerkConfig); ok {
		cp.config = clerkCfg
	} else if configMap, ok := config.(map[string]any); ok {
		// Parse from map (typical JSON unmarshal result)
		if enabled, ok := configMap["enabled"].(bool); ok {
			cp.config.Enabled = enabled
		}
		if jwtVersion, ok := configMap["jwt_version"].(string); ok {
			cp.config.JWTVersion = jwtVersion
		}
		if orgSupport, ok := configMap["organization_support"].(bool); ok {
			cp.config.OrganizationSupport = orgSupport
		}
		if metadataSupport, ok := configMap["public_metadata_support"].(bool); ok {
			cp.config.PublicMetadataSupport = metadataSupport
		}
	} else {
		return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation,
			"invalid configuration type for Clerk plugin",
			map[string]any{"config_type": fmt.Sprintf("%T", config)})
	}

	return nil
}

// DetectAuthPattern analyzes a statement for Clerk authentication patterns
func (cp *ClerkPlugin) DetectAuthPattern(stmt *types.Statement) string {
	// Pattern 1: JWT v2 organization patterns (most specific)
	if cp.hasJWTV2OrgPattern(stmt.SQL) {
		return AuthPatternClerkJWTV2
	}

	// Pattern 2: Clerk helper functions
	if cp.isClerkAuthFunction(stmt.SQL) {
		return AuthPatternClerk
	}

	return ""
}

// EnrichStatement adds Clerk-specific metadata to parsed statements
func (cp *ClerkPlugin) EnrichStatement(ctx context.Context, stmt *types.Statement) error {
	// Detect and set auth pattern using plugin-specific detection
	if authPattern := cp.DetectAuthPattern(stmt); authPattern != "" {
		stmt.AuthPattern = types.AuthPatternType(authPattern)
	}

	// Mark RLS policies using Clerk auth as critical
	if stmt.ObjectType == types.TypePolicy && stmt.AuthPattern != types.AuthPatternNone {
		stmt.Category = types.CategoryCritical
	}

	// Mark Clerk helper functions as critical (should not be deduplicated)
	if stmt.ObjectType == types.TypeFunction && cp.isClerkAuthFunction(stmt.SQL) {
		stmt.Category = types.CategoryCritical
	}

	return nil
}

// isClerkAuthFunction checks if SQL defines a Clerk authentication helper
func (cp *ClerkPlugin) isClerkAuthFunction(sql string) bool {
	clerkFunctions := []string{
		"clerk_user_id",
		"clerk_is_admin",
		"clerk_organization_id",
		"current_user_id",
		"current_organization_id",
		"current_organization_role",
		"current_organization_name",
	}

	sqlUpper := strings.ToUpper(sql)
	for _, fn := range clerkFunctions {
		// Match CREATE FUNCTION statements
		if strings.Contains(sqlUpper, "CREATE FUNCTION "+strings.ToUpper(fn)) ||
			strings.Contains(sqlUpper, "CREATE OR REPLACE FUNCTION "+strings.ToUpper(fn)) {
			return true
		}
	}

	return false
}

// DetectPatterns analyzes SQL for Clerk-specific patterns
func (cp *ClerkPlugin) DetectPatterns(sql string) []plugins.Pattern {
	var patterns []plugins.Pattern

	// Pattern 1: JWT v2 organization claims
	if cp.hasJWTV2OrgPattern(sql) {
		patterns = append(patterns, plugins.Pattern{
			Type: plugins.PatternTypeAuth,
			Name: "clerk_jwt_v2_org",
			Metadata: map[string]string{
				"claim_path": "auth.jwt()->'o'->>'id'",
				"version":    "v2",
			},
			Severity: plugins.SeverityCritical,
		})
	}

	// Pattern 2: Clerk helper functions
	if cp.isClerkAuthFunction(sql) {
		patterns = append(patterns, plugins.Pattern{
			Type:     plugins.PatternTypeFunction,
			Name:     "clerk_auth_helper",
			Severity: plugins.SeverityCritical,
		})
	}

	return patterns
}

// hasJWTV2OrgPattern checks for Clerk JWT v2 organization claim patterns
func (cp *ClerkPlugin) hasJWTV2OrgPattern(sql string) bool {
	// Robust check: Parse and Deparse to strip comments and normalize
	// This prevents "-- auth.jwt()" comments from triggering detection
	if tree, err := pg_query.Parse(sql); err == nil {
		if clean, err := pg_query.Deparse(tree); err == nil {
			sql = clean
		}
	}

	return strings.Contains(sql, "auth.jwt()") &&
		(strings.Contains(sql, "'o'->>'id'") ||
			strings.Contains(sql, "'o'->'id'"))
}

// GetConflictingPlugins returns plugins that conflict with Clerk
func (cp *ClerkPlugin) GetConflictingPlugins() []string {
	// Clerk conflicts with Supabase auth (both use auth.jwt()).
	// Clerk has higher priority (95 > 90), so Supabase is excluded when both are detected.
	return []string{"supabase"}
}

// GetRequiredExtensions returns PostgreSQL extensions required by Clerk
func (cp *ClerkPlugin) GetRequiredExtensions() []string {
	// Clerk typically doesn't require special extensions
	// But if using UUIDs for user IDs, might need uuid-ossp
	return []string{}
}

// ShouldPreserve determines if a statement should never be consolidated
func (cp *ClerkPlugin) ShouldPreserve(stmt *types.Statement) bool {
	// Preserve all Clerk auth functions (critical for RLS policies)
	if stmt.ObjectType == types.TypeFunction && cp.isClerkAuthFunction(stmt.SQL) {
		return true
	}

	// Preserve RLS policies using Clerk JWT v2 patterns
	if stmt.ObjectType == types.TypePolicy && stmt.AuthPattern == types.AuthPatternType(AuthPatternClerkJWTV2) {
		return true
	}

	return false
}

// ValidateSchema performs Clerk-specific schema validation
func (cp *ClerkPlugin) ValidateSchema(ctx context.Context, db *sql.DB) error {
	// Verify auth schema exists
	var schemaExists bool
	err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'auth')").Scan(&schemaExists)
	if err != nil {
		return errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation,
			"failed to check auth schema",
			map[string]any{"operation": "check_schema"})
	}

	if !schemaExists {
		return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation,
			"auth schema does not exist (required for Clerk integration)",
			map[string]any{"schema": "auth"})
	}

	// Verify auth.jwt() function exists
	var functionExists bool
	err = db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_proc p JOIN pg_namespace n ON p.pronamespace = n.oid WHERE n.nspname = 'auth' AND p.proname = 'jwt')").
		Scan(&functionExists)
	if err != nil {
		return errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation,
			"failed to check auth.jwt() function",
			map[string]any{"function": "auth.jwt"})
	}

	if !functionExists {
		return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation,
			"auth.jwt() function does not exist (required for Clerk integration)",
			map[string]any{"function": "auth.jwt"})
	}

	return nil
}

// Note: Plugin registration happens in the squasher/engine initialization
// to avoid import cycles. Do not use init() for plugin registration.
