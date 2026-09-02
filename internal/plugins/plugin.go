// Package plugins provides a unified third-party integration system.
// It allows third-party services (auth providers, ORMs, managed Platforms)
// to hook into the migration analysis, transformation, and validation pipeline.
package plugins

import (
	"context"
	"database/sql"

	"github.com/capy-base/pgsquash-engine/internal/types"
)

// Plugin represents a third-party service integration that can hook into
// the migration processing pipeline at multiple points.
//
// Lifecycle:
//  1. Registry.Detect() checks all migrations to find applicable plugins
//  2. Plugin.Initialize() is called with service-specific configuration
//  3. Parser calls EnrichStatement() to add metadata
//  4. Transformation calls TransformSQL() to modify SQL
//  5. Validation calls InjectCompatibilityLayer() and ValidateSchema()
//  6. Squasher uses GetConsolidationRules() and ShouldPreserve()
type Plugin interface {
	// ===== Lifecycle Methods =====

	// Name returns the unique plugin identifier (e.g., "clerk", "supabase", "prisma")
	Name() string

	// Priority returns plugin priority for conflict resolution (higher = higher priority)
	// Standard priorities:
	//   - Auth services: 90-100
	//   - ORMs: 70-89
	//   - Platforms: 50-69
	//   - Utilities: 0-49
	Priority() int

	// Detect analyzes migrations to determine if this plugin is applicable.
	// Returns true if plugin should be activated based on migration patterns.
	Detect(migrations []*types.Migration) bool

	// Initialize configures the plugin with service-specific settings.
	// Config is the plugin-specific configuration section from pgsquash.config.json.
	Initialize(ctx context.Context, config any) error

	// ===== Parser Hooks =====

	// EnrichStatement adds plugin-specific metadata to parsed statements.
	// Called after pg_query parsing, before tracking.
	// Examples:
	//   - Mark auth functions with AuthPattern metadata
	//   - Tag ORM-generated tables with schema preservation flags
	//   - Add cross-reference information for validation
	EnrichStatement(ctx context.Context, stmt *types.Statement) error

	// DetectPatterns analyzes SQL text for plugin-specific patterns.
	// Returns patterns found (e.g., JWT claim access, Prisma directives).
	// Called before pg_query parsing for pre-parse transformations.
	DetectPatterns(sql string) []Pattern

	// DetectAuthPattern analyzes a statement for authentication patterns.
	// Returns the auth pattern identifier (e.g., "clerk_jwt_v2", "supabase_rls")
	// or empty string if no auth pattern is detected.
	// This allows plugins to claim ownership of auth-related statements.
	DetectAuthPattern(stmt *types.Statement) string

	// ===== Transformation Hooks =====

	// TransformSQL modifies SQL statements for compatibility or optimization.
	// Called before pg_query parsing to fix syntax issues.
	// Examples:
	//   - Add STABLE markers to auth functions
	//   - Convert Prisma @@ directives to PostgreSQL equivalents
	//   - Fix ORM-specific syntax
	TransformSQL(ctx context.Context, sql string) (string, error)

	// InjectCompatibilityLayer returns SQL to execute before migrations.
	// Used to create mock functions, roles, or schemas for validation.
	// Examples:
	//   - Clerk: CREATE SCHEMA auth; CREATE FUNCTION auth.jwt() ...
	//   - Supabase: CREATE ROLE authenticated; CREATE FUNCTION auth.uid() ...
	//   - NextAuth: CREATE TABLE accounts, sessions, users, verification_tokens ...
	InjectCompatibilityLayer(ctx context.Context) string

	// FixFunctionVolatility adds IMMUTABLE/STABLE/VOLATILE markers to functions.
	// Service-specific implementation based on function patterns.
	// Returns modified SQL with volatility markers added.
	FixFunctionVolatility(ctx context.Context, functionSQL string) (string, error)

	// ===== Validation Hooks =====

	// ValidateSchema performs plugin-specific schema validation.
	// Called after standard validation with access to database connection.
	// Examples:
	//   - Verify RLS policies reference correct auth functions
	//   - Check Prisma shadow database compatibility
	//   - Validate ORM adapter table schemas
	ValidateSchema(ctx context.Context, db *sql.DB) error

	// GetRequiredExtensions returns PostgreSQL extensions required by this plugin.
	// Examples:
	//   - Supabase: []string{"uuid-ossp"}
	//   - Vector DBs: []string{"vector"}
	GetRequiredExtensions() []string

	// ===== Squashing Hooks =====

	// GetConsolidationRules returns plugin-specific consolidation rules.
	// These rules are merged with standard consolidation rules.
	// Higher priority plugins override conflicting rules.
	GetConsolidationRules() []ConsolidationRule

	// ShouldPreserve determines if a statement should never be consolidated.
	// Returns true to mark statement as critical (e.g., auth functions, ORM metadata).
	// Examples:
	//   - Preserve Clerk JWT v2 organization functions
	//   - Preserve Prisma migration metadata tables
	//   - Preserve NextAuth session management triggers
	ShouldPreserve(stmt *types.Statement) bool

	// GetConflictingPlugins returns names of plugins that conflict with this one.
	// Used for conflict resolution when multiple plugins want to handle same pattern.
	GetConflictingPlugins() []string
}

// Pattern represents a detected plugin-specific pattern in SQL
type Pattern struct {
	Type     PatternType       // Pattern category
	Name     string            // Pattern identifier (e.g., "clerk_org_function", "prisma_directive")
	Location Location          // Source location in SQL
	Metadata map[string]string // Pattern-specific metadata
	Severity PatternSeverity   // How important is this pattern
}

// PatternType categorizes detected patterns
type PatternType string

const (
	PatternTypeAuth      PatternType = "AUTH"      // Authentication/authorization patterns
	PatternTypeORM       PatternType = "ORM"       // ORM-specific syntax
	PatternTypeSchema    PatternType = "SCHEMA"    // Schema metadata
	PatternTypeFunction  PatternType = "FUNCTION"  // Function definitions
	PatternTypePolicy    PatternType = "POLICY"    // RLS/security policies
	PatternTypeStorage   PatternType = "STORAGE"   // File storage patterns
	PatternTypeTrigger   PatternType = "TRIGGER"   // Trigger definitions
	PatternTypeExtension PatternType = "EXTENSION" // Extension usage
)

// PatternSeverity indicates pattern importance
type PatternSeverity string

const (
	SeverityCritical PatternSeverity = "CRITICAL" // Must preserve (e.g., auth functions)
	SeverityHigh     PatternSeverity = "HIGH"     // Should preserve (e.g., RLS policies)
	SeverityMedium   PatternSeverity = "MEDIUM"   // Can consolidate carefully
	SeverityLow      PatternSeverity = "LOW"      // Safe to consolidate
)

// Location represents a position in SQL source
type Location struct {
	StartLine   int
	StartColumn int
	EndLine     int
	EndColumn   int
}

// ConsolidationRule defines how to consolidate plugin-specific statements
type ConsolidationRule struct {
	Name        string                                    // Rule identifier
	Priority    int                                       // Higher priority overrides conflicts
	ObjectType  types.ObjectType                          // Target object type
	AuthPattern types.AuthPatternType                     // Target auth pattern (if applicable)
	Conflicts   []string                                  // Conflicting rule names
	CanMerge    func([]*types.Statement) bool             // Check if statements can merge
	Merge       func([]*types.Statement) *types.Statement // Perform merge
}

// BasePlugin provides default implementations for optional Plugin methods
type BasePlugin struct {
	name      string
	priority  int
	conflicts []string
}

// NewBasePlugin creates a base plugin with common defaults
func NewBasePlugin(name string, priority int) *BasePlugin {
	return &BasePlugin{
		name:      name,
		priority:  priority,
		conflicts: []string{},
	}
}

func (bp *BasePlugin) Name() string                    { return bp.name }
func (bp *BasePlugin) Priority() int                   { return bp.priority }
func (bp *BasePlugin) GetConflictingPlugins() []string { return bp.conflicts }

// Default implementations (no-ops)
func (bp *BasePlugin) EnrichStatement(ctx context.Context, stmt *types.Statement) error {
	return nil
}

func (bp *BasePlugin) DetectPatterns(sql string) []Pattern {
	return []Pattern{}
}

func (bp *BasePlugin) TransformSQL(ctx context.Context, sql string) (string, error) {
	return sql, nil
}

func (bp *BasePlugin) InjectCompatibilityLayer(ctx context.Context) string {
	return ""
}

func (bp *BasePlugin) FixFunctionVolatility(ctx context.Context, functionSQL string) (string, error) {
	return functionSQL, nil
}

func (bp *BasePlugin) ValidateSchema(ctx context.Context, db *sql.DB) error {
	return nil
}

func (bp *BasePlugin) GetRequiredExtensions() []string {
	return []string{}
}

func (bp *BasePlugin) GetConsolidationRules() []ConsolidationRule {
	return []ConsolidationRule{}
}

func (bp *BasePlugin) ShouldPreserve(stmt *types.Statement) bool {
	return false
}

func (bp *BasePlugin) DetectAuthPattern(stmt *types.Statement) string {
	return "" // Default: no auth pattern detected
}
