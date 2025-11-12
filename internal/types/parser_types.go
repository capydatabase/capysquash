// Package types contains shared type definitions used across multiple packages.
// This avoids import cycles between parser, plugins, tracking, and other packages.
package types

// Migration represents a parsed migration file with metadata and statements
type Migration struct {
	Filename    string      // Migration filename
	Sequence    int         // Migration sequence number
	Statements  []Statement // Parsed SQL statements
	Size        int64       // File size in bytes
	ParseErrors []string    // Parse errors encountered during migration parsing
}

// Statement represents a single parsed SQL statement with rich metadata
//
// Note: ParseTree is kept as interface{} to avoid circular dependencies.
// In practice, this is typically *pg_query.ParseResult from the parser package.
// Plugins and other packages should use SQL and metadata fields instead of ParseTree.
type Statement struct {
	SQL          string         // Original SQL text
	ParseTree    interface{}    // Parse tree (kept as interface{} to avoid pg_query dependency)
	ObjectType   ObjectType     // Type of database object (TABLE, INDEX, FUNCTION, etc.)
	ObjectName   string         // Name of the object being operated on
	Operation    Operation      // SQL operation (CREATE, ALTER, DROP, etc.)
	Line         int            // Line number in migration file
	IsDataOp     bool           // Whether this is a data operation (INSERT, UPDATE, DELETE)
	Category     Category       // Statement category for organization
	Dependencies []string       // Object dependencies (foreign keys, function calls, etc.)
	Comments     []string       // Associated SQL comments
	Schema       string         // Schema name (defaults to "public")
	CrossSchema  []CrossSchemaRef // Cross-schema references
	AuthPattern  AuthPatternType  // Detected authentication pattern
	IsDynamic    bool           // Whether SQL contains dynamic elements
	IfNotExists  bool           // Whether statement uses IF NOT EXISTS clause

	// GRANT/REVOKE specific fields
	Grantees   []string // Users/roles receiving permissions
	Privileges []string // Privileges being granted/revoked

	// ALTER TYPE specific fields
	AlterTypeNewValue string // New ENUM value being added via ALTER TYPE ADD VALUE

	// Index specific fields
	IndexHadExplicitAccessMethod bool // Whether CREATE INDEX had explicit USING clause

	// Statement metadata for transaction and lock analysis
	Metadata StatementMetadata
}

// StatementMetadata contains metadata about statement execution requirements
type StatementMetadata struct {
	// Lock level required by this statement
	LockLevel LockLevel

	// Whether this statement cannot run inside a transaction
	RequiresNoTransaction bool

	// PostgreSQL version gate (e.g., "PG<12" for ENUM additions)
	VersionGate string

	// Whether this is a concurrent operation
	Concurrent bool

	// Whether this statement should be preserved verbatim (manual pragma)
	PreserveVerbatim bool

	// Estimated execution time category
	ExecutionTime ExecutionTimeCategory

	// Whether this statement is idempotent
	Idempotent bool
}

// LockLevel represents the PostgreSQL lock level required by a statement
type LockLevel string

const (
	LockNone                  LockLevel = "NONE"                     // No lock required
	LockAccessShare           LockLevel = "ACCESS_SHARE"             // SELECT
	LockRowShare              LockLevel = "ROW_SHARE"                // SELECT FOR UPDATE/SHARE
	LockRowExclusive          LockLevel = "ROW_EXCLUSIVE"            // INSERT, UPDATE, DELETE
	LockShareUpdateExclusive  LockLevel = "SHARE_UPDATE_EXCLUSIVE"   // VACUUM, CREATE INDEX CONCURRENTLY
	LockShare                 LockLevel = "SHARE"                    // CREATE INDEX (non-concurrent)
	LockShareRowExclusive     LockLevel = "SHARE_ROW_EXCLUSIVE"      // Rare, some ALTER TABLE
	LockExclusive             LockLevel = "EXCLUSIVE"                // REFRESH MATERIALIZED VIEW CONCURRENTLY
	LockAccessExclusive       LockLevel = "ACCESS_EXCLUSIVE"         // Most DDL, TRUNCATE, VACUUM FULL
)

// ExecutionTimeCategory estimates how long a statement might take
type ExecutionTimeCategory string

const (
	ExecutionInstant ExecutionTimeCategory = "INSTANT" // < 1ms (most DDL on empty tables)
	ExecutionFast    ExecutionTimeCategory = "FAST"    // < 100ms
	ExecutionMedium  ExecutionTimeCategory = "MEDIUM"  // < 1s
	ExecutionSlow    ExecutionTimeCategory = "SLOW"    // > 1s (backfills, large indexes)
	ExecutionUnknown ExecutionTimeCategory = "UNKNOWN"
)

// ObjectType represents the type of database object
type ObjectType string

const (
	TypeTable           ObjectType = "TABLE"
	TypeIndex           ObjectType = "INDEX"
	TypeFunction        ObjectType = "FUNCTION"
	TypeTrigger         ObjectType = "TRIGGER"
	TypeView            ObjectType = "VIEW"
	TypeSequence        ObjectType = "SEQUENCE"
	TypeConstraint      ObjectType = "CONSTRAINT"
	TypePolicy          ObjectType = "POLICY"
	TypeRole            ObjectType = "ROLE"
	TypeSchema          ObjectType = "SCHEMA"
	TypeExtension       ObjectType = "EXTENSION"
	TypePublication     ObjectType = "PUBLICATION"
	TypeComment         ObjectType = "COMMENT"
	TypeDoBlock         ObjectType = "DO_BLOCK"
	TypeType            ObjectType = "TYPE"              // CREATE TYPE statements (enums, composites, domains)
	TypeDomain          ObjectType = "DOMAIN"            // CREATE DOMAIN statements
	TypeEnum            ObjectType = "ENUM"              // CREATE TYPE ... AS ENUM statements
	TypeComposite       ObjectType = "COMPOSITE"         // CREATE TYPE ... AS (composite types)
	TypeSubscription    ObjectType = "SUBSCRIPTION"      // PostgreSQL 15+
	TypeStatistic       ObjectType = "STATISTIC"         // PostgreSQL 15+
	TypeGeneratedColumn ObjectType = "GENERATED_COLUMN"  // PostgreSQL 15+
	TypeMultirangeType  ObjectType = "MULTIRANGE_TYPE"   // PostgreSQL 14+
	TypeVectorIndex     ObjectType = "VECTOR_INDEX"      // pgvector extension
	TypeEventTrigger    ObjectType = "EVENT_TRIGGER"     // PostgreSQL 17+
	TypeUnknown         ObjectType = "UNKNOWN"
)

// CrossSchemaRef represents a reference to an object in a different schema
type CrossSchemaRef struct {
	Schema     string     // Referenced schema name
	ObjectType ObjectType // Type of referenced object
	ObjectName string     // Name of referenced object
}

// AuthPatternType represents detected authentication/authorization patterns.
// Plugins provide specific identifiers (e.g., "clerk_jwt_v2", "supabase_rls").
// The parser delegates auth pattern detection to the plugin layer.
type AuthPatternType string

// Generic auth pattern categories (for backward compatibility and filtering)
const (
	AuthPatternNone    AuthPatternType = ""
	AuthPatternRLS     AuthPatternType = "RLS_POLICY"     // Row-Level Security policies
	AuthPatternStorage AuthPatternType = "STORAGE_POLICY" // Storage bucket policies
	AuthPatternJWT     AuthPatternType = "JWT_AUTH"       // JWT-based authentication
	AuthPatternSession AuthPatternType = "SESSION_AUTH"   // Session-based authentication
	AuthPatternCustom  AuthPatternType = "CUSTOM_AUTH"    // Custom authentication
)

// Operation represents the SQL operation being performed
type Operation string

const (
	OpCreate  Operation = "CREATE"
	OpAlter   Operation = "ALTER"
	OpDrop    Operation = "DROP"
	OpInsert  Operation = "INSERT"
	OpUpdate  Operation = "UPDATE"
	OpDelete  Operation = "DELETE"
	OpGrant   Operation = "GRANT"
	OpRevoke  Operation = "REVOKE"
	OpComment Operation = "COMMENT"
)

// Category represents the semantic category of a statement for organization
type Category string

const (
	CategoryFoundation  Category = "foundation"  // Schemas, extensions, base types
	CategoryConstraints Category = "constraints" // Foreign keys, checks, unique constraints
	CategoryIndexes     Category = "indexes"     // Indexes and index-like objects
	CategoryFunctions   Category = "functions"   // Functions, procedures, triggers
	CategoryTriggers    Category = "triggers"    // Trigger definitions
	CategoryComments    Category = "comments"    // COMMENT ON statements (must come after objects)
	CategorySecurity    Category = "security"    // RLS policies, grants, roles
	CategoryData        Category = "data"        // INSERT, UPDATE, DELETE statements
	CategoryExtensions  Category = "extensions"  // PostgreSQL extensions
	CategoryCritical    Category = "critical"    // Critical statements that should never be modified
)
