package parser

import (
	"strings"
	"unicode"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// NormalizationContext provides context for PostgreSQL identifier normalization
type NormalizationContext struct {
	DefaultSchema       string `json:"default_schema"`
	CaseSensitive       bool   `json:"case_sensitive"`
	PreserveQuotes      bool   `json:"preserve_quotes"`
	MaxIdentifierLength int    `json:"max_identifier_length"`
	PostgreSQLVersion   int    `json:"postgresql_version"`
}

// DefaultNormalizationContext returns PostgreSQL-compliant defaults
func DefaultNormalizationContext() *NormalizationContext {
	return &NormalizationContext{
		DefaultSchema:       "public",
		CaseSensitive:       false, // PostgreSQL folds to lowercase unless quoted
		PreserveQuotes:      true,
		MaxIdentifierLength: 63, // PostgreSQL NAMEDATALEN - 1
		PostgreSQLVersion:   17,
	}
}

// ContextualNormalizer handles PostgreSQL identifier normalization with context awareness
type ContextualNormalizer struct {
	context *NormalizationContext
}

// NewContextualNormalizer creates a normalizer with the given context
func NewContextualNormalizer(ctx *NormalizationContext) *ContextualNormalizer {
	if ctx == nil {
		ctx = DefaultNormalizationContext()
	}
	return &ContextualNormalizer{context: ctx}
}

// NormalizeIdentifier normalizes a PostgreSQL identifier according to context
func (cn *ContextualNormalizer) NormalizeIdentifier(identifier string) string {
	if identifier == "" {
		return ""
	}

	// Handle quoted identifiers
	if strings.HasPrefix(identifier, `"`) && strings.HasSuffix(identifier, `"`) {
		if cn.context.PreserveQuotes {
			// Preserve quotes but normalize internal content
			content := identifier[1 : len(identifier)-1]
			// Handle doubled quotes within quoted identifier
			normalized := strings.ReplaceAll(content, `""`, `"`)
			return `"` + normalized + `"`
		}
		// Remove quotes and return as-is (preserving case)
		return strings.ReplaceAll(identifier[1:len(identifier)-1], `""`, `"`)
	}

	// Unquoted identifiers
	if cn.context.CaseSensitive {
		return identifier
	}

	// PostgreSQL folds unquoted identifiers to lowercase
	normalized := strings.ToLower(identifier)

	// Truncate if exceeds maximum length
	if len(normalized) > cn.context.MaxIdentifierLength {
		normalized = normalized[:cn.context.MaxIdentifierLength]
	}

	return normalized
}

// NormalizeSchemaName normalizes a schema name with proper defaulting
func (cn *ContextualNormalizer) NormalizeSchemaName(schemaName string) string {
	if schemaName == "" {
		return cn.context.DefaultSchema
	}
	return cn.NormalizeIdentifier(schemaName)
}

// NormalizeTableName normalizes a table name, handling schema prefixes
func (cn *ContextualNormalizer) NormalizeTableName(tableName string) (schema, table string) {
	parts := cn.ParseQualifiedName(tableName)
	switch len(parts) {
	case 1:
		return cn.context.DefaultSchema, cn.NormalizeIdentifier(parts[0])
	case 2:
		return cn.NormalizeIdentifier(parts[0]), cn.NormalizeIdentifier(parts[1])
	default:
		// Handle complex cases or return as-is
		return cn.context.DefaultSchema, cn.NormalizeIdentifier(tableName)
	}
}

// NormalizeFunctionName normalizes a function name with overloading support
func (cn *ContextualNormalizer) NormalizeFunctionName(funcName string) (schema, name string) {
	parts := cn.ParseQualifiedName(funcName)
	switch len(parts) {
	case 1:
		return cn.context.DefaultSchema, cn.NormalizeIdentifier(parts[0])
	case 2:
		return cn.NormalizeIdentifier(parts[0]), cn.NormalizeIdentifier(parts[1])
	default:
		return cn.context.DefaultSchema, cn.NormalizeIdentifier(funcName)
	}
}

// ParseQualifiedName parses a qualified name handling quotes properly
func (cn *ContextualNormalizer) ParseQualifiedName(qualifiedName string) []string {
	if qualifiedName == "" {
		return []string{}
	}

	var parts []string
	var current strings.Builder
	inQuotes := false
	i := 0

	for i < len(qualifiedName) {
		char := qualifiedName[i]

		switch char {
		case '"':
			if inQuotes {
				// Check for doubled quote
				if i+1 < len(qualifiedName) && qualifiedName[i+1] == '"' {
					current.WriteByte('"')
					i += 2
					continue
				}
				inQuotes = false
			} else {
				inQuotes = true
			}
			current.WriteByte(char)

		case '.':
			if !inQuotes {
				parts = append(parts, current.String())
				current.Reset()
			} else {
				current.WriteByte(char)
			}

		default:
			current.WriteByte(char)
		}
		i++
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

// BatchNormalize normalizes multiple identifiers with error collection
func (cn *ContextualNormalizer) BatchNormalize(identifiers []string) ([]string, []error) {
	normalized := make([]string, len(identifiers))
	var errors []error

	for i, identifier := range identifiers {
		if identifier == "" {
			errors = append(errors, NewNormalizationError("empty identifier", i))
			continue
		}

		if len(identifier) > cn.context.MaxIdentifierLength*2 { // Reasonable upper bound
			errors = append(errors, NewNormalizationError("identifier too long", i))
			continue
		}

		normalized[i] = cn.NormalizeIdentifier(identifier)
	}

	return normalized, errors
}

// CompareIdentifiers performs case-insensitive comparison according to PostgreSQL rules
func (cn *ContextualNormalizer) CompareIdentifiers(id1, id2 string) bool {
	norm1 := cn.NormalizeIdentifier(id1)
	norm2 := cn.NormalizeIdentifier(id2)
	return norm1 == norm2
}

// VersionedKeywordManager manages PostgreSQL keywords by version
type VersionedKeywordManager struct {
	version      int
	baseWords    map[string]KeywordType
	versionWords map[int]map[string]KeywordType
}

// KeywordType represents different types of PostgreSQL keywords
type KeywordType int

const (
	KeywordTypeReserved KeywordType = iota
	KeywordTypeNonReserved
	KeywordTypeContextual
	KeywordTypeUnreserved
)

// NewVersionedKeywordManager creates a keyword manager for a specific PostgreSQL version
func NewVersionedKeywordManager(version int) *VersionedKeywordManager {
	manager := &VersionedKeywordManager{
		version:      version,
		baseWords:    make(map[string]KeywordType),
		versionWords: make(map[int]map[string]KeywordType),
	}

	manager.loadBaseKeywords()
	manager.loadVersionSpecificKeywords()

	return manager
}

// loadBaseKeywords loads PostgreSQL keywords common to all versions
func (vkm *VersionedKeywordManager) loadBaseKeywords() {
	// Reserved keywords (cannot be used as identifiers without quoting)
	reserved := []string{
		"ALL", "ANALYSE", "ANALYZE", "AND", "ANY", "ARRAY", "AS", "ASC", "ASYMMETRIC",
		"AUTHORIZATION", "BINARY", "BOTH", "CASE", "CAST", "CHECK", "COLLATE", "COLLATION",
		"COLUMN", "CONCURRENTLY", "CONSTRAINT", "CREATE", "CROSS", "CURRENT_CATALOG",
		"CURRENT_DATE", "CURRENT_ROLE", "CURRENT_SCHEMA", "CURRENT_TIME", "CURRENT_TIMESTAMP",
		"CURRENT_USER", "DEFAULT", "DEFERRABLE", "DESC", "DISTINCT", "DO", "ELSE", "END",
		"EXCEPT", "FALSE", "FETCH", "FOR", "FOREIGN", "FREEZE", "FROM", "FULL", "GRANT",
		"GROUP", "HAVING", "ILIKE", "IN", "INITIALLY", "INNER", "INTERSECT", "INTO", "IS",
		"ISNULL", "JOIN", "LATERAL", "LEADING", "LEFT", "LIKE", "LIMIT", "LOCALTIME",
		"LOCALTIMESTAMP", "NATURAL", "NOT", "NOTNULL", "NULL", "OFFSET", "ON", "ONLY", "OR",
		"ORDER", "OUTER", "OVERLAPS", "PLACING", "PRIMARY", "REFERENCES", "RETURNING",
		"RIGHT", "SELECT", "SESSION_USER", "SIMILAR", "SOME", "SYMMETRIC", "TABLE",
		"TABLESAMPLE", "THEN", "TO", "TRAILING", "TRUE", "UNION", "UNIQUE", "USER", "USING",
		"VARIADIC", "VERBOSE", "WHEN", "WHERE", "WINDOW", "WITH",
	}

	for _, keyword := range reserved {
		vkm.baseWords[keyword] = KeywordTypeReserved
	}

	// Non-reserved keywords (can be used as identifiers in most contexts)
	nonReserved := []string{
		"ABORT", "ABSOLUTE", "ACCESS", "ACTION", "ADD", "ADMIN", "AFTER", "AGGREGATE",
		"ALSO", "ALTER", "ALWAYS", "ASSERTION", "ASSIGNMENT", "AT", "ATTACH", "ATTRIBUTE",
		"BACKWARD", "BEFORE", "BEGIN", "BY", "CACHE", "CALLED", "CASCADE", "CASCADED",
		"CATALOG", "CHAIN", "CHARACTERISTICS", "CHECKPOINT", "CLASS", "CLOSE", "CLUSTER",
		"COALESCE", "COMMENT", "COMMENTS", "COMMIT", "COMMITTED", "CONFIGURATION", "CONFLICT",
		"CONNECTION", "CONSTRAINTS", "CONTENT", "CONTINUE", "CONVERSION", "COPY", "COST",
		"CSV", "CUBE", "CURRENT", "CURSOR", "CYCLE", "DATA", "DATABASE", "DAY", "DEALLOCATE",
		"DECLARE", "DEFAULTS", "DEFERRED", "DEFINER", "DELETE", "DELIMITER", "DELIMITERS",
		"DEPENDS", "DETACH", "DICTIONARY", "DISABLE", "DISCARD", "DOCUMENT", "DOMAIN",
		"DOUBLE", "DROP", "EACH", "ENABLE", "ENCODING", "ENCRYPTED", "ENUM", "ESCAPE",
		"EVENT", "EXCLUDE", "EXCLUDING", "EXCLUSIVE", "EXECUTE", "EXPLAIN", "EXTENSION",
		"EXTERNAL", "FAMILY", "FILTER", "FIRST", "FOLLOWING", "FORCE", "FORWARD", "FUNCTION",
		"FUNCTIONS", "GENERATED", "GLOBAL", "GRANTED", "GROUPS", "HANDLER", "HEADER", "HOLD",
		"HOUR", "IDENTITY", "IF", "IMMEDIATE", "IMMUTABLE", "IMPLICIT", "IMPORT", "INCLUDE",
		"INCLUDING", "INCREMENT", "INDEX", "INDEXES", "INHERIT", "INHERITS", "INLINE",
		"INPUT", "INSENSITIVE", "INSERT", "INSTEAD", "INVOKER", "ISOLATION", "KEY", "LABEL",
		"LANGUAGE", "LARGE", "LAST", "LEAKPROOF", "LEVEL", "LISTEN", "LOAD", "LOCAL",
		"LOCATION", "LOCK", "LOCKED", "LOGGED", "MAPPING", "MATCH", "MATERIALIZED", "MAXVALUE",
		"METHOD", "MINUTE", "MINVALUE", "MODE", "MONTH", "MOVE", "NAME", "NAMES", "NEW",
		"NEXT", "NO", "NOTHING", "NOTIFY", "NOWAIT", "NULLS", "OBJECT", "OF", "OFF", "OIDS",
		"OLD", "OPERATOR", "OPTION", "OPTIONS", "ORDINALITY", "OTHERS", "OVER", "OVERRIDING",
		"OWNED", "OWNER", "PARALLEL", "PARSER", "PARTIAL", "PARTITION", "PASSING", "PASSWORD",
		"PLANS", "POLICY", "PRECEDING", "PREPARE", "PREPARED", "PRESERVE", "PRIOR", "PRIVILEGES",
		"PROCEDURAL", "PROCEDURE", "PROCEDURES", "PROGRAM", "PUBLICATION", "QUOTE", "RANGE",
		"READ", "REASSIGN", "RECHECK", "RECURSIVE", "REF", "REFERENCING", "REFRESH", "REINDEX",
		"RELATIVE", "RELEASE", "RENAME", "REPEATABLE", "REPLACE", "REPLICA", "RESET", "RESTART",
		"RESTRICT", "RETURNS", "REVOKE", "ROLE", "ROLLBACK", "ROLLUP", "ROUTINE", "ROUTINES",
		"ROWS", "RULE", "SAVEPOINT", "SCHEMA", "SCHEMAS", "SCROLL", "SEARCH", "SECOND",
		"SECURITY", "SEQUENCE", "SEQUENCES", "SERIALIZABLE", "SERVER", "SESSION", "SET",
		"SETS", "SHARE", "SHOW", "SIMPLE", "SKIP", "SNAPSHOT", "SQL", "STABLE", "STANDALONE",
		"START", "STATEMENT", "STATISTICS", "STDIN", "STDOUT", "STORAGE", "STORED", "STRICT",
		"STRIP", "SUBSCRIPTION", "SUPPORT", "SYSID", "SYSTEM", "TABLES", "TABLESPACE", "TEMP",
		"TEMPLATE", "TEMPORARY", "TEXT", "TIES", "TRANSACTION", "TRANSFORM", "TRIGGER",
		"TRUNCATE", "TRUSTED", "TYPE", "TYPES", "UNBOUNDED", "UNCOMMITTED", "UNENCRYPTED",
		"UNKNOWN", "UNLISTEN", "UNLOGGED", "UNTIL", "UPDATE", "VACUUM", "VALID", "VALIDATE",
		"VALIDATOR", "VALUE", "VALUES", "VERSION", "VIEW", "VIEWS", "VOLATILE", "WHITESPACE",
		"WITHIN", "WITHOUT", "WORK", "WRAPPER", "WRITE", "XML", "YEAR", "YES", "ZONE",
	}

	for _, keyword := range nonReserved {
		vkm.baseWords[keyword] = KeywordTypeNonReserved
	}
}

// loadVersionSpecificKeywords loads version-specific PostgreSQL keywords
func (vkm *VersionedKeywordManager) loadVersionSpecificKeywords() {
	// PostgreSQL 14+ keywords
	if vkm.version >= 14 {
		vkm.addVersionKeywords(14, map[string]KeywordType{
			"MULTIRANGE": KeywordTypeNonReserved,
		})
	}

	// PostgreSQL 17+ keywords
	if vkm.version >= 17 {
		vkm.addVersionKeywords(17, map[string]KeywordType{
			"MERGE":  KeywordTypeReserved,
			"STORED": KeywordTypeNonReserved,
		})
	}
}

// addVersionKeywords adds keywords for a specific version
func (vkm *VersionedKeywordManager) addVersionKeywords(version int, keywords map[string]KeywordType) {
	if vkm.versionWords[version] == nil {
		vkm.versionWords[version] = make(map[string]KeywordType)
	}
	for keyword, keywordType := range keywords {
		vkm.versionWords[version][keyword] = keywordType
	}
}

// IsKeyword checks if a word is a PostgreSQL keyword
func (vkm *VersionedKeywordManager) IsKeyword(word string) bool {
	upperWord := strings.ToUpper(word)

	// Check base keywords
	if _, exists := vkm.baseWords[upperWord]; exists {
		return true
	}

	// Check version-specific keywords
	for version := 9; version <= vkm.version; version++ {
		if versionKeywords, exists := vkm.versionWords[version]; exists {
			if _, exists := versionKeywords[upperWord]; exists {
				return true
			}
		}
	}

	return false
}

// GetKeywordType returns the type of a keyword
func (vkm *VersionedKeywordManager) GetKeywordType(word string) (KeywordType, bool) {
	upperWord := strings.ToUpper(word)

	// Check version-specific keywords first (newer takes precedence)
	for version := vkm.version; version >= 9; version-- {
		if versionKeywords, exists := vkm.versionWords[version]; exists {
			if keywordType, exists := versionKeywords[upperWord]; exists {
				return keywordType, true
			}
		}
	}

	// Check base keywords
	if keywordType, exists := vkm.baseWords[upperWord]; exists {
		return keywordType, true
	}

	return KeywordTypeUnreserved, false
}

// IsReservedKeyword checks if a word is a reserved keyword
func (vkm *VersionedKeywordManager) IsReservedKeyword(word string) bool {
	keywordType, isKeyword := vkm.GetKeywordType(word)
	return isKeyword && keywordType == KeywordTypeReserved
}

// ContextualKeywordChecker provides context-aware keyword checking
type ContextualKeywordChecker struct {
	context KeywordContext
	manager *VersionedKeywordManager
}

// KeywordContext represents the context in which a keyword appears
type KeywordContext int

const (
	KeywordContextGeneral KeywordContext = iota
	KeywordContextDDL
	KeywordContextDML
	KeywordContextFunction
	KeywordContextConstraint
)

// NewContextualKeywordChecker creates a context-aware keyword checker
func NewContextualKeywordChecker(context KeywordContext, version int) *ContextualKeywordChecker {
	return &ContextualKeywordChecker{
		context: context,
		manager: NewVersionedKeywordManager(version),
	}
}

// IsKeywordInContext checks if a word is a keyword in the given context
func (ckc *ContextualKeywordChecker) IsKeywordInContext(word string) bool {
	// First check if it's a keyword at all
	if !ckc.manager.IsKeyword(word) {
		return false
	}

	upperWord := strings.ToUpper(word)

	// Context-specific checks
	switch ckc.context {
	case KeywordContextDDL:
		return ckc.isDDLKeyword(upperWord)
	case KeywordContextDML:
		return ckc.isDMLKeyword(upperWord)
	case KeywordContextFunction:
		return ckc.isFunctionKeyword(upperWord)
	case KeywordContextConstraint:
		return ckc.isConstraintKeyword(upperWord)
	default:
		return true // General context - all keywords apply
	}
}

func (ckc *ContextualKeywordChecker) isDDLKeyword(word string) bool {
	ddlKeywords := map[string]bool{
		"CREATE": true, "ALTER": true, "DROP": true, "TABLE": true, "INDEX": true,
		"VIEW": true, "FUNCTION": true, "TRIGGER": true, "CONSTRAINT": true,
		"SCHEMA": true, "DATABASE": true, "EXTENSION": true, "TYPE": true,
		"SEQUENCE": true, "DOMAIN": true, "COLLATION": true,
	}
	return ddlKeywords[word]
}

func (ckc *ContextualKeywordChecker) isDMLKeyword(word string) bool {
	dmlKeywords := map[string]bool{
		"SELECT": true, "INSERT": true, "UPDATE": true, "DELETE": true,
		"FROM": true, "WHERE": true, "JOIN": true, "ON": true, "GROUP": true,
		"ORDER": true, "HAVING": true, "LIMIT": true, "OFFSET": true,
		"UNION": true, "INTERSECT": true, "EXCEPT": true,
	}
	return dmlKeywords[word]
}

func (ckc *ContextualKeywordChecker) isFunctionKeyword(word string) bool {
	functionKeywords := map[string]bool{
		"FUNCTION": true, "PROCEDURE": true, "RETURNS": true, "LANGUAGE": true,
		"VOLATILE": true, "STABLE": true, "IMMUTABLE": true, "STRICT": true,
		"SECURITY": true, "DEFINER": true, "INVOKER": true, "PARALLEL": true,
	}
	return functionKeywords[word]
}

func (ckc *ContextualKeywordChecker) isConstraintKeyword(word string) bool {
	constraintKeywords := map[string]bool{
		"CONSTRAINT": true, "PRIMARY": true, "FOREIGN": true, "KEY": true,
		"UNIQUE": true, "CHECK": true, "NOT": true, "NULL": true,
		"REFERENCES": true, "ON": true, "DELETE": true, "UPDATE": true,
		"CASCADE": true, "RESTRICT": true, "SET": true, "DEFAULT": true,
		"DEFERRABLE": true, "INITIALLY": true, "DEFERRED": true, "IMMEDIATE": true,
	}
	return constraintKeywords[word]
}

// NormalizationError is now an alias to errors.StructuredError
// This maintains backward compatibility
type NormalizationError = ParseError

// NewNormalizationError creates a new normalization error
func NewNormalizationError(message string, index int) *NormalizationError {
	return &ParseError{
		StructuredError: errors.NewParseError(errors.ErrorCodeNormalizationFailed, message).
			WithAdditional("index", index),
	}
}

// Utility functions for common PostgreSQL naming patterns

// IsValidPostgreSQLIdentifier checks if an identifier is valid in PostgreSQL
func IsValidPostgreSQLIdentifier(identifier string) bool {
	if identifier == "" {
		return false
	}

	// Check length
	if len(identifier) > 63 {
		return false
	}

	// Quoted identifiers are always valid (if properly quoted)
	if strings.HasPrefix(identifier, `"`) && strings.HasSuffix(identifier, `"`) {
		return len(identifier) >= 2
	}

	// Unquoted identifiers must start with letter or underscore
	first := rune(identifier[0])
	if !unicode.IsLetter(first) && first != '_' {
		return false
	}

	// Rest must be letters, digits, underscores, or dollar signs
	for _, char := range identifier[1:] {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) && char != '_' && char != '$' {
			return false
		}
	}

	return true
}

// SuggestIdentifierName suggests a valid identifier name based on input
func SuggestIdentifierName(input string) string {
	if input == "" {
		return "unnamed"
	}

	var result strings.Builder

	// Ensure first character is valid
	first := rune(input[0])
	if unicode.IsLetter(first) || first == '_' {
		result.WriteRune(first)
	} else {
		result.WriteByte('_')
		if unicode.IsDigit(first) {
			result.WriteRune(first)
		}
	}

	// Process remaining characters
	for _, char := range input[1:] {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || char == '_' || char == '$' {
			result.WriteRune(char)
		} else if unicode.IsSpace(char) || char == '-' {
			result.WriteByte('_')
		}
		// Skip other invalid characters
	}

	suggested := result.String()

	// Truncate if too long
	if len(suggested) > 63 {
		suggested = suggested[:63]
	}

	// Ensure it's not empty
	if suggested == "" {
		suggested = "unnamed"
	}

	return strings.ToLower(suggested)
}
