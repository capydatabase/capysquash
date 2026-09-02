// Package supabase provides Supabase Platform integration for pgsquash.
// It handles auth.uid(), RLS policies, Storage bucket policies, and Realtime publications.
package supabase

import (
	"context"
	"database/sql"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/plugins"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// Supabase-specific auth pattern identifiers
const (
	AuthPatternSupabase        = "supabase_auth"    // Supabase auth.uid() patterns
	AuthPatternSupabaseRLS     = "supabase_rls"     // RLS policies with auth.uid()
	AuthPatternSupabaseStorage = "supabase_storage" // Storage bucket policies
)

// SupabasePlugin implements Supabase Platform integration
type SupabasePlugin struct {
	*plugins.BasePlugin
	config SupabaseConfig
}

// SupabaseConfig holds Supabase-specific configuration
type SupabaseConfig struct {
	Enabled            bool   `json:"enabled"`
	JWTSecret          string `json:"jwt_secret"`
	EnableRLS          bool   `json:"enable_rls"`
	StorageIntegration bool   `json:"storage_integration"`
}

// NewSupabasePlugin creates a new Supabase plugin instance
func NewSupabasePlugin() *SupabasePlugin {
	return &SupabasePlugin{
		BasePlugin: plugins.NewBasePlugin("supabase", 90), // High priority for auth
		config: SupabaseConfig{
			Enabled:            true,
			EnableRLS:          true,
			StorageIntegration: true,
		},
	}
}

// Detect checks if migrations contain Supabase patterns
// Detection patterns (in order of specificity):
//  1. auth.uid() function calls (most specific to Supabase)
//  2. auth.users table references
//  3. storage.objects or storage.buckets tables
//  4. supabase_realtime publication
func (sp *SupabasePlugin) Detect(migrations []*types.Migration) bool {
	for _, migration := range migrations {
		for _, stmt := range migration.Statements {
			sql := stmt.SQL

			// Pattern 1: auth.uid() function (Supabase-specific)
			// Note: Don't match comments mentioning "No legacy auth.uid()"
			if strings.Contains(sql, "auth.uid()") && !strings.Contains(sql, "No legacy auth.uid()") {
				return true
			}

			// Pattern 2: auth.users table
			if strings.Contains(sql, "auth.users") {
				return true
			}

			// Pattern 3: Storage tables
			storagePatterns := []string{
				"storage.objects",
				"storage.buckets",
			}
			for _, pattern := range storagePatterns {
				if strings.Contains(sql, pattern) {
					return true
				}
			}

			// Pattern 4: Supabase Realtime publication
			if strings.Contains(sql, "supabase_realtime") {
				return true
			}

			// Pattern 5: Supabase-specific extensions or schemas
			supabasePatterns := []string{
				"CREATE EXTENSION IF NOT EXISTS pgjwt",
				"CREATE SCHEMA IF NOT EXISTS storage",
				"CREATE SCHEMA IF NOT EXISTS auth",
			}
			for _, pattern := range supabasePatterns {
				if strings.Contains(sql, pattern) {
					return true
				}
			}
		}
	}

	return false
}

// Initialize configures the Supabase plugin with service-specific settings
func (sp *SupabasePlugin) Initialize(ctx context.Context, config any) error {
	if config == nil {
		// Use defaults
		return nil
	}

	// Type assertion to extract Supabase config
	if supabaseCfg, ok := config.(SupabaseConfig); ok {
		sp.config = supabaseCfg
	} else if configMap, ok := config.(map[string]any); ok {
		// Parse from map (typical JSON unmarshal result)
		if enabled, ok := configMap["enabled"].(bool); ok {
			sp.config.Enabled = enabled
		}
		if jwtSecret, ok := configMap["jwt_secret"].(string); ok {
			sp.config.JWTSecret = jwtSecret
		}
		if enableRLS, ok := configMap["enable_rls"].(bool); ok {
			sp.config.EnableRLS = enableRLS
		}
		if storageIntegration, ok := configMap["storage_integration"].(bool); ok {
			sp.config.StorageIntegration = storageIntegration
		}
	}

	return nil
}

// DetectAuthPattern analyzes a statement for Supabase authentication patterns
func (sp *SupabasePlugin) DetectAuthPattern(stmt *types.Statement) string {
	sql := stmt.SQL

	// Pattern 1: RLS policies with auth.uid() (most specific)
	if stmt.ObjectType == types.TypePolicy && strings.Contains(sql, "auth.uid()") {
		return AuthPatternSupabaseRLS
	}

	// Pattern 2: Storage policies
	if stmt.ObjectType == types.TypePolicy &&
		(strings.Contains(sql, "storage.objects") || strings.Contains(sql, "storage.buckets")) {
		return AuthPatternSupabaseStorage
	}

	// Pattern 3: Generic Supabase auth patterns
	if strings.Contains(sql, "auth.uid()") {
		return AuthPatternSupabase
	}

	return ""
}

// EnrichStatement adds Supabase-specific metadata to parsed statements
func (sp *SupabasePlugin) EnrichStatement(ctx context.Context, stmt *types.Statement) error {
	// Detect and set auth pattern using plugin-specific detection
	if authPattern := sp.DetectAuthPattern(stmt); authPattern != "" {
		stmt.AuthPattern = types.AuthPatternType(authPattern)

		// Mark policies with auth patterns as critical
		if stmt.ObjectType == types.TypePolicy {
			stmt.Category = types.CategoryCritical
		}
	}

	// Detect Realtime publications
	if stmt.ObjectType == types.TypePublication && strings.Contains(stmt.SQL, "supabase_realtime") {
		stmt.Category = types.CategoryCritical
	}

	return nil
}

// DetectPatterns analyzes SQL for Supabase-specific patterns
func (sp *SupabasePlugin) DetectPatterns(sql string) []plugins.Pattern {
	var patterns []plugins.Pattern

	// Pattern 1: auth.uid() usage
	if strings.Contains(sql, "auth.uid()") {
		patterns = append(patterns, plugins.Pattern{
			Type: plugins.PatternTypeAuth,
			Name: "supabase_auth_uid",
			Metadata: map[string]string{
				"function": "auth.uid()",
			},
			Severity: plugins.SeverityCritical,
		})
	}

	// Pattern 2: Storage policies
	if strings.Contains(sql, "storage.objects") || strings.Contains(sql, "storage.buckets") {
		patterns = append(patterns, plugins.Pattern{
			Type:     plugins.PatternTypeStorage,
			Name:     "supabase_storage",
			Severity: plugins.SeverityHigh,
		})
	}

	// Pattern 3: Realtime publication
	if strings.Contains(sql, "supabase_realtime") {
		patterns = append(patterns, plugins.Pattern{
			Type:     plugins.PatternTypeExtension,
			Name:     "supabase_realtime",
			Severity: plugins.SeverityHigh,
		})
	}

	return patterns
}

// GetConflictingPlugins returns plugins that conflict with Supabase
func (sp *SupabasePlugin) GetConflictingPlugins() []string {
	// Supabase doesn't conflict with other plugins (lower priority than Clerk)
	return []string{}
}

// GetRequiredExtensions returns PostgreSQL extensions required by Supabase
func (sp *SupabasePlugin) GetRequiredExtensions() []string {
	extensions := []string{"uuid-ossp"}

	// Additional extensions if storage is enabled
	if sp.config.StorageIntegration {
		extensions = append(extensions, "pg_stat_statements")
	}

	return extensions
}

// ShouldPreserve determines if a statement should never be consolidated
func (sp *SupabasePlugin) ShouldPreserve(stmt *types.Statement) bool {
	// Preserve RLS policies using auth.uid()
	if stmt.ObjectType == types.TypePolicy && (stmt.AuthPattern == types.AuthPatternType(AuthPatternSupabase) ||
		stmt.AuthPattern == types.AuthPatternType(AuthPatternSupabaseRLS)) {
		return true
	}

	// Preserve Storage policies
	if stmt.ObjectType == types.TypePolicy && stmt.AuthPattern == types.AuthPatternType(AuthPatternSupabaseStorage) {
		return true
	}

	// Preserve Realtime publications
	if stmt.ObjectType == types.TypePublication && strings.Contains(stmt.SQL, "supabase_realtime") {
		return true
	}

	// Preserve auth schema functions
	if stmt.ObjectType == types.TypeFunction && sp.isSupabaseAuthFunction(stmt.SQL) {
		return true
	}

	// These are Supabase-managed tables that should not be modified during squashing
	if stmt.Schema == "storage" && stmt.ObjectType == types.TypeTable {
		return true
	}

	// Also preserve auth schema tables (auth.users, auth.sessions, etc.)
	if stmt.Schema == "auth" && stmt.ObjectType == types.TypeTable {
		return true
	}

	return false
}

// isSupabaseAuthFunction checks if SQL defines a Supabase authentication helper
func (sp *SupabasePlugin) isSupabaseAuthFunction(sql string) bool {
	supabaseFunctions := []string{
		"auth.uid",
		"auth.jwt",
		"auth.role",
	}

	sqlUpper := strings.ToUpper(sql)
	for _, fn := range supabaseFunctions {
		fnUpper := strings.ToUpper(fn)
		if strings.Contains(sqlUpper, "CREATE FUNCTION "+fnUpper) ||
			strings.Contains(sqlUpper, "CREATE OR REPLACE FUNCTION "+fnUpper) {
			return true
		}
	}

	return false
}

// ValidateSchema performs Supabase-specific schema validation
func (sp *SupabasePlugin) ValidateSchema(ctx context.Context, db *sql.DB) error {
	// Verify auth schema exists
	var schemaExists bool
	err := db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'auth')").Scan(&schemaExists)
	if err != nil {
		return errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation,
			"failed to check auth schema",
			map[string]any{"schema": "auth"})
	}

	if !schemaExists {
		return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation,
			"auth schema does not exist (required for Supabase integration)",
			map[string]any{"schema": "auth"})
	}

	// Verify auth.uid() function exists
	var functionExists bool
	err = db.QueryRowContext(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_proc p JOIN pg_namespace n ON p.pronamespace = n.oid WHERE n.nspname = 'auth' AND p.proname = 'uid')").
		Scan(&functionExists)
	if err != nil {
		return errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation,
			"failed to check auth.uid() function",
			map[string]any{"function": "auth.uid"})
	}

	if !functionExists {
		return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation,
			"auth.uid() function does not exist (required for Supabase integration)",
			map[string]any{"function": "auth.uid"})
	}

	// If storage integration is enabled, verify storage schema
	if sp.config.StorageIntegration {
		var storageExists bool
		err = db.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname = 'storage')").Scan(&storageExists)
		if err != nil {
			return errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation,
				"failed to check storage schema",
				map[string]any{"schema": "storage"})
		}

		if !storageExists {
			return errors.New(errors.ErrorCodeValidationFailed, errors.CategoryValidation,
				"storage schema does not exist (required for Supabase Storage integration)",
				map[string]any{"schema": "storage"})
		}
	}

	return nil
}

// Note: Plugin registration happens in the squasher/engine initialization
// to avoid import cycles. Do not use init() for plugin registration.
