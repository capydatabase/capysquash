package builder

import (
	"bytes"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/types"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// SQLBuilder provides fluent interface for PostgreSQL SQL generation
type SQLBuilder struct {
	buf     bytes.Buffer
	indent  int
	options *BuildOptions
	errors  []error
}

// BuildOptions configures SQL generation behavior
type BuildOptions struct {
	FormatStyle      FormatStyle `json:"format_style"`
	IndentSize       int         `json:"indent_size"`
	MaxLineLength    int         `json:"max_line_length"`
	UseDoubleQuotes  bool        `json:"use_double_quotes"`
	NormalizeNames   bool        `json:"normalize_names"`
	PreserveComments bool        `json:"preserve_comments"`
	PostgreSQLMode   bool        `json:"postgresql_mode"`
}

// FormatStyle defines SQL formatting approaches
type FormatStyle int

const (
	FormatCompact FormatStyle = iota
	FormatPretty
	FormatDense
)

// DefaultBuildOptions returns sensible defaults for PostgreSQL
func DefaultBuildOptions() *BuildOptions {
	return &BuildOptions{
		FormatStyle:      FormatPretty,
		IndentSize:       2,
		MaxLineLength:    120,
		UseDoubleQuotes:  true, // PostgreSQL standard
		NormalizeNames:   true,
		PreserveComments: true,
		PostgreSQLMode:   true,
	}
}

// NewSQLBuilder creates a new SQL builder
func NewSQLBuilder(options *BuildOptions) *SQLBuilder {
	if options == nil {
		options = DefaultBuildOptions()
	}

	return &SQLBuilder{
		options: options,
		errors:  []error{},
	}
}

// Fluent interface methods

// P adds phrases separated by spaces
func (b *SQLBuilder) P(phrases ...string) *SQLBuilder {
	for i, phrase := range phrases {
		if i > 0 {
			b.buf.WriteByte(' ')
		}
		b.buf.WriteString(phrase)
	}
	return b
}

// Statement adds a complete SQL statement with automatic semicolon
func (b *SQLBuilder) Statement(sql string) *SQLBuilder {
	b.P(sql)
	if !strings.HasSuffix(strings.TrimSpace(sql), ";") {
		b.P(";")
	}
	return b
}

// S adds a single space
func (b *SQLBuilder) S() *SQLBuilder {
	b.buf.WriteByte(' ')
	return b
}

// NL adds a newline with proper indentation
func (b *SQLBuilder) NL() *SQLBuilder {
	switch b.options.FormatStyle {
	case FormatPretty:
		b.buf.WriteByte('\n')
		for i := 0; i < b.indent*b.options.IndentSize; i++ {
			b.buf.WriteByte(' ')
		}
	case FormatDense:
		b.buf.WriteByte(' ')
	}
	return b
}

// Indent increases indentation level
func (b *SQLBuilder) Indent() *SQLBuilder {
	b.indent++
	return b
}

// Dedent decreases indentation level
func (b *SQLBuilder) Dedent() *SQLBuilder {
	if b.indent > 0 {
		b.indent--
	}
	return b
}

// Wrap wraps content in parentheses with proper formatting
func (b *SQLBuilder) Wrap(fn func(*SQLBuilder)) *SQLBuilder {
	b.buf.WriteByte('(')
	if b.options.FormatStyle == FormatPretty {
		b.Indent().NL()
	}

	fn(b)

	if b.options.FormatStyle == FormatPretty {
		b.Dedent().NL()
	}
	b.buf.WriteByte(')')
	return b
}

// Quote adds properly quoted identifier
func (b *SQLBuilder) Quote(identifier string) *SQLBuilder {
	if b.needsQuoting(identifier) {
		if b.options.UseDoubleQuotes {
			b.buf.WriteByte('"')
			b.buf.WriteString(strings.ReplaceAll(identifier, `"`, `""`))
			b.buf.WriteByte('"')
		} else {
			b.buf.WriteByte('`')
			b.buf.WriteString(strings.ReplaceAll(identifier, "`", "``"))
			b.buf.WriteByte('`')
		}
	} else {
		b.buf.WriteString(identifier)
	}
	return b
}

// QuoteQualifiedName writes a schema-qualified name, omitting schema if empty or "public"
// This ensures generated SQL is clean and doesn't unnecessarily qualify names in public schema
func (b *SQLBuilder) QuoteQualifiedName(schema, name string) *SQLBuilder {
	// Omit schema if it's empty or "public" (PostgreSQL default)
	if schema == "" || schema == "public" {
		return b.Quote(name)
	}
	// Include schema qualification for non-public schemas
	return b.Quote(schema).P(".").Quote(name)
}

// Comment adds a SQL comment
func (b *SQLBuilder) Comment(comment string) *SQLBuilder {
	if b.options.PreserveComments && comment != "" {
		b.P("--", comment).NL()
	}
	return b
}

// String returns the built SQL
func (b *SQLBuilder) String() string {
	return b.buf.String()
}

// Errors returns any errors encountered during building
func (b *SQLBuilder) Errors() []error {
	return b.errors
}

// HasErrors returns true if there are any errors
func (b *SQLBuilder) HasErrors() bool {
	return len(b.errors) > 0
}

// Reset clears the builder
func (b *SQLBuilder) Reset() *SQLBuilder {
	b.buf.Reset()
	b.indent = 0
	b.errors = []error{}
	return b
}

// High-level SQL construction methods

// CreateTable builds a CREATE TABLE statement
func (b *SQLBuilder) CreateTable(table *TableDefinition) *SQLBuilder {
	b.P("CREATE TABLE")
	if table.IfNotExists {
		b.P("IF NOT EXISTS")
	}

	b.S().QuoteQualifiedName(table.Schema, table.Name)

	b.Wrap(func(inner *SQLBuilder) {
		for i, col := range table.Columns {
			if i > 0 {
				inner.P(",").NL()
			}
			inner.buildColumnDefinition(col)
		}

		if len(table.Constraints) > 0 {
			inner.P(",").NL()
			for i, constraint := range table.Constraints {
				if i > 0 {
					inner.P(",").NL()
				}
				inner.buildConstraintDefinition(constraint)
			}
		}
	})

	if len(table.Inherits) > 0 {
		b.NL().P("INHERITS").Wrap(func(inner *SQLBuilder) {
			for i, parent := range table.Inherits {
				if i > 0 {
					inner.P(",").S()
				}
				inner.Quote(parent)
			}
		})
	}

	if table.Tablespace != "" {
		b.NL().P("TABLESPACE").S().Quote(table.Tablespace)
	}

	return b
}

// CreateIndex builds a CREATE INDEX statement
func (b *SQLBuilder) CreateIndex(index *IndexDefinition) *SQLBuilder {
	b.P("CREATE")

	if index.Unique {
		b.P("UNIQUE")
	}

	b.P("INDEX")

	if index.IfNotExists {
		b.P("IF NOT EXISTS")
	}

	if index.Name != "" {
		b.S().Quote(index.Name)
	}

	b.P("ON").S().Quote(index.Schema).P(".").Quote(index.Table)

	// 1. Method is non-BTREE (always explicit)
	// 2. Method is BTREE AND it was explicit in original SQL
	// This prevents adding "USING btree" to spatial indexes where it would fail
	if index.Method != "" && index.Method != "BTREE" {
		// Non-BTREE methods (GIN, GIST, HASH, etc.) are always explicit
		b.S().P("USING").S().P(index.Method)
	} else if index.Method == "BTREE" && index.HadExplicitAccessMethod {
		// BTREE was explicitly specified, preserve it
		b.S().P("USING").S().P("btree")
	}
	// If Method is BTREE but wasn't explicit, omit USING clause entirely
	// PostgreSQL will choose the correct default based on column type

	b.Wrap(func(inner *SQLBuilder) {
		for i, col := range index.Columns {
			if i > 0 {
				inner.P(",").S()
			}
			if col.Expression != "" {
				inner.P(col.Expression)
			} else {
				inner.Quote(col.Name)
				if col.Order != "" {
					inner.S().P(col.Order)
				}
			}
		}
	})

	if index.Where != "" {
		b.NL().P("WHERE").S().P(index.Where)
	}

	return b
}

// CreateFunction builds a CREATE FUNCTION statement
func (b *SQLBuilder) CreateFunction(function *FunctionDefinition) *SQLBuilder {
	b.P("CREATE OR REPLACE FUNCTION")
	b.S().QuoteQualifiedName(function.Schema, function.Name)

	// Parameters
	b.Wrap(func(inner *SQLBuilder) {
		for i, param := range function.Parameters {
			if i > 0 {
				inner.P(",").S()
			}
			if param.Mode != "" && param.Mode != "IN" {
				inner.P(param.Mode).S()
			}
			if param.Name != "" {
				inner.Quote(param.Name).S()
			}
			inner.P(param.Type)
			if param.Default != "" {
				inner.S().P("DEFAULT").S().P(param.Default)
			}
		}
	})

	// Return type
	b.NL().P("RETURNS").S().P(function.ReturnType)

	// Prepare body and handle language declaration
	body := function.Body
	bodyLanguage := ""

	// ALWAYS remove trailing language clauses using string replacement
	// This handles all cases regardless of format
	body = strings.ReplaceAll(body, " language 'plpgsql';", "")
	body = strings.ReplaceAll(body, " language 'sql';", "")
	body = strings.ReplaceAll(body, " language plpgsql;", "")
	body = strings.ReplaceAll(body, " language sql;", "")

	// Find and extract language declaration from body if present
	// Check for common trailing language patterns and remove them
	patterns := []string{
		" language 'plpgsql';",
		" language 'sql';",
		" language plpgsql;",
		" language sql;",
		" language 'plpgsql'",
		" language 'sql'",
		" language plpgsql",
		" language sql",
		"\nlanguage 'plpgsql';",
		"\nlanguage 'sql';",
		"\nlanguage plpgsql;",
		"\nlanguage sql;",
	}

	bodyLower := strings.ToLower(body)
	for _, pattern := range patterns {
		if idx := strings.LastIndex(bodyLower, pattern); idx > 0 {
			// Extract language from pattern
			if strings.Contains(pattern, "plpgsql") {
				bodyLanguage = "plpgsql"
			} else {
				bodyLanguage = "sql"
			}

			// Remove the pattern from the body
			body = body[:idx]
			break
		}
	}

	// Determine language with priority:
	// 1. Extracted from body (most reliable)
	// 2. Infer from body content (overrides function.Language if body has plpgsql constructs)
	// 3. Use function.Language
	// 4. Default to sql
	language := bodyLanguage

	if language == "" {
		// Check body content for plpgsql constructs
		bodyLowerForInfer := strings.ToLower(body)
		hasPlpgsqlConstructs := strings.Contains(bodyLowerForInfer, "declare") ||
			(strings.Contains(bodyLowerForInfer, "begin") && strings.Contains(bodyLowerForInfer, "end")) ||
			strings.Contains(bodyLowerForInfer, "perform ")

		if hasPlpgsqlConstructs {
			language = "plpgsql"
		} else if function.Language != "" {
			language = function.Language
		} else {
			language = "sql"
		}
	}

	// Add LANGUAGE clause
	b.NL().P("LANGUAGE").S().P(language)

	// Properties
	if function.Volatility != "" && function.Volatility != "VOLATILE" {
		b.NL().P(function.Volatility)
	}

	if function.Strict {
		b.NL().P("STRICT")
	}

	if function.Security != "" && function.Security != "INVOKER" {
		b.NL().P("SECURITY").S().P(function.Security)
	}

	// Body
	b.NL().P("AS").S().P(strings.TrimSpace(body))

	return b
}

// Helper methods

func (b *SQLBuilder) buildColumnDefinition(col *ColumnDefinition) {
	b.Quote(col.Name).S().P(col.DataType)

	if col.NotNull {
		b.S().P("NOT NULL")
	}

	if col.Default != "" {
		b.S().P("DEFAULT").S().P(col.Default)
	}

	if col.Generated {
		b.S().P("GENERATED ALWAYS AS").Wrap(func(inner *SQLBuilder) {
			inner.P(col.GeneratedExpression)
		})
		if col.GeneratedStored {
			b.S().P("STORED")
		}
	}

	if col.Collation != "" {
		b.S().P("COLLATE").S().P(col.Collation)
	}
}

func (b *SQLBuilder) buildConstraintDefinition(constraint *ConstraintDefinition) {
	if constraint.Name != "" {
		b.P("CONSTRAINT").S().Quote(constraint.Name).S()
	}

	switch constraint.Type {
	case "PRIMARY KEY":
		b.P("PRIMARY KEY").Wrap(func(inner *SQLBuilder) {
			for i, col := range constraint.Columns {
				if i > 0 {
					inner.P(",").S()
				}
				inner.Quote(col)
			}
		})

	case "FOREIGN KEY":
		b.P("FOREIGN KEY").Wrap(func(inner *SQLBuilder) {
			for i, col := range constraint.Columns {
				if i > 0 {
					inner.P(",").S()
				}
				inner.Quote(col)
			}
		})
		b.S().P("REFERENCES").S().Quote(constraint.RefTable)
		if len(constraint.RefColumns) > 0 {
			b.Wrap(func(inner *SQLBuilder) {
				for i, col := range constraint.RefColumns {
					if i > 0 {
						inner.P(",").S()
					}
					inner.Quote(col)
				}
			})
		}
		if constraint.OnDelete != "" {
			b.S().P("ON DELETE").S().P(constraint.OnDelete)
		}
		if constraint.OnUpdate != "" {
			b.S().P("ON UPDATE").S().P(constraint.OnUpdate)
		}

	case "UNIQUE":
		b.P("UNIQUE").Wrap(func(inner *SQLBuilder) {
			for i, col := range constraint.Columns {
				if i > 0 {
					inner.P(",").S()
				}
				inner.Quote(col)
			}
		})

	case "CHECK":
		b.P("CHECK").Wrap(func(inner *SQLBuilder) {
			inner.P(constraint.CheckExpression)
		})
	}

	if constraint.Deferrable {
		b.S().P("DEFERRABLE")
		if constraint.InitiallyDeferred {
			b.S().P("INITIALLY DEFERRED")
		}
	}
}

func (b *SQLBuilder) needsQuoting(identifier string) bool {
	if !b.options.PostgreSQLMode {
		return false
	}

	// Check if identifier needs quoting in PostgreSQL
	if identifier == "" {
		return false
	}

	// Check for PostgreSQL keywords
	if IsPostgreSQLKeyword(identifier) {
		return true
	}

	// Check for special characters
	for _, char := range identifier {
		isLowerCase := char >= 'a' && char <= 'z'
		isUpperCase := char >= 'A' && char <= 'Z'
		isDigit := char >= '0' && char <= '9'
		isUnderscore := char == '_'

		if !isLowerCase && !isUpperCase && !isDigit && !isUnderscore {
			return true
		}
	}

	// Check if starts with digit
	if identifier[0] >= '0' && identifier[0] <= '9' {
		return true
	}

	// Check for mixed case (PostgreSQL folds to lowercase unless quoted)
	if b.options.NormalizeNames {
		hasUpper := false
		for _, char := range identifier {
			if char >= 'A' && char <= 'Z' {
				hasUpper = true
				break
			}
		}
		return hasUpper
	}

	return false
}

// Definition structures

type TableDefinition struct {
	Schema      string                  `json:"schema"`
	Name        string                  `json:"name"`
	IfNotExists bool                    `json:"if_not_exists"`
	Columns     []*ColumnDefinition     `json:"columns"`
	Constraints []*ConstraintDefinition `json:"constraints"`
	Inherits    []string                `json:"inherits,omitempty"`
	Tablespace  string                  `json:"tablespace,omitempty"`
	Comment     string                  `json:"comment,omitempty"`
}

type ColumnDefinition struct {
	Name                string `json:"name"`
	DataType            string `json:"data_type"`
	NotNull             bool   `json:"not_null"`
	Default             string `json:"default,omitempty"`
	Generated           bool   `json:"generated"`
	GeneratedExpression string `json:"generated_expression,omitempty"`
	GeneratedStored     bool   `json:"generated_stored"`
	Collation           string `json:"collation,omitempty"`
	Comment             string `json:"comment,omitempty"`
}

type ConstraintDefinition struct {
	Name              string   `json:"name,omitempty"`
	Type              string   `json:"type"`
	Columns           []string `json:"columns"`
	RefTable          string   `json:"ref_table,omitempty"`
	RefColumns        []string `json:"ref_columns,omitempty"`
	OnDelete          string   `json:"on_delete,omitempty"`
	OnUpdate          string   `json:"on_update,omitempty"`
	CheckExpression   string   `json:"check_expression,omitempty"`
	Deferrable        bool     `json:"deferrable"`
	InitiallyDeferred bool     `json:"initially_deferred"`
}

type IndexDefinition struct {
	Schema                  string         `json:"schema"`
	Table                   string         `json:"table"`
	Name                    string         `json:"name,omitempty"`
	IfNotExists             bool           `json:"if_not_exists"`
	Unique                  bool           `json:"unique"`
	Method                  string         `json:"method"`                     // BTREE, HASH, GIN, GIST, etc.
	HadExplicitAccessMethod bool           `json:"had_explicit_access_method"` // BUG-001: Track if USING was explicit
	Columns                 []*IndexColumn `json:"columns"`
	Where                   string         `json:"where,omitempty"`
	Comment                 string         `json:"comment,omitempty"`
}

type IndexColumn struct {
	Name       string `json:"name,omitempty"`
	Expression string `json:"expression,omitempty"`
	Order      string `json:"order,omitempty"` // ASC, DESC
}

type FunctionDefinition struct {
	Schema     string                 `json:"schema"`
	Name       string                 `json:"name"`
	Parameters []*ParameterDefinition `json:"parameters"`
	ReturnType string                 `json:"return_type"`
	Language   string                 `json:"language"`
	Body       string                 `json:"body"`
	Volatility string                 `json:"volatility"` // VOLATILE, STABLE, IMMUTABLE
	Strict     bool                   `json:"strict"`
	Security   string                 `json:"security"` // DEFINER, INVOKER
	Comment    string                 `json:"comment,omitempty"`
}

type ParameterDefinition struct {
	Name    string `json:"name,omitempty"`
	Type    string `json:"type"`
	Mode    string `json:"mode,omitempty"` // IN, OUT, INOUT
	Default string `json:"default,omitempty"`
}

// PostgreSQL keyword checking (simplified implementation)
func IsPostgreSQLKeyword(word string) bool {
	keywords := map[string]bool{
		"SELECT": true, "FROM": true, "WHERE": true, "INSERT": true,
		"UPDATE": true, "DELETE": true, "CREATE": true, "ALTER": true,
		"DROP": true, "TABLE": true, "INDEX": true, "VIEW": true,
		"FUNCTION": true, "TRIGGER": true, "CONSTRAINT": true,
		"PRIMARY": true, "FOREIGN": true, "KEY": true, "UNIQUE": true,
		"NOT": true, "NULL": true, "DEFAULT": true, "CHECK": true,
		"REFERENCES": true, "ON": true, "CASCADE": true, "RESTRICT": true,
		"AND": true, "OR": true, "IN": true, "EXISTS": true, "LIKE": true,
		"BETWEEN": true, "IS": true, "AS": true, "DISTINCT": true,
		"ORDER": true, "BY": true, "GROUP": true, "HAVING": true,
		"LIMIT": true, "OFFSET": true, "UNION": true, "INTERSECT": true,
		"EXCEPT": true, "ALL": true, "ANY": true, "SOME": true,
		"JOIN": true, "INNER": true, "LEFT": true, "RIGHT": true,
		"FULL": true, "OUTER": true, "CROSS": true, "NATURAL": true,
		"USING": true, "CASE": true, "WHEN": true, "THEN": true,
		"ELSE": true, "END": true, "IF": true, "ELSIF": true,
		"WHILE": true, "FOR": true, "LOOP": true, "RETURN": true,
		"BEGIN": true, "COMMIT": true, "ROLLBACK": true, "GRANT": true,
		"REVOKE": true, "USER": true, "ROLE": true, "SCHEMA": true,
		"DATABASE": true, "EXTENSION": true, "TYPE": true, "DOMAIN": true,
	}

	return keywords[strings.ToUpper(word)]
}

// Conversion from parser types

// FromStatement converts a parser statement to SQL using the builder
func (b *SQLBuilder) FromStatement(stmt types.Statement) *SQLBuilder {
	switch stmt.Operation {
	case types.OpCreate, types.OpAlter:
		return b.fromASTStatement(stmt)
	case types.OpDrop:
		return b.fromDropStatement(stmt)
	case types.OpGrant:
		return b.fromGrantStatement(stmt)
	case types.OpRevoke:
		return b.fromRevokeStatement(stmt)
	default:
		// Fallback to original SQL
		b.Statement(stmt.SQL)
		return b
	}
}

// fromASTStatement converts AST-based statements (CREATE, ALTER) back to SQL
func (b *SQLBuilder) fromASTStatement(stmt types.Statement) *SQLBuilder {
	// Deparsing (pg_query.Deparse) corrupts functions by changing:
	// - LANGUAGE placement (before AS vs after body)
	// - LANGUAGE type (sql vs plpgsql)
	// - Volatility markers (STABLE, IMMUTABLE, VOLATILE)
	// - Security markers (SECURITY DEFINER)
	//
	// Since consolidation rules now preserve original SQL (rule.go, function_dedup_rule.go),
	// we must use that SQL directly instead of deparsing the AST.
	if stmt.SQL != "" {
		b.Statement(stmt.SQL)
		return b
	}

	// Only deparse if we don't have original SQL
	if stmt.ParseTree != nil {
		// The ParseTree is stored as interface{}, try to cast to *pg_query.ParseResult
		// The ParseTree is stored as concrete type
		if parseTree := stmt.ParseTree; parseTree != nil {
			if deparsed, err := pg_query.Deparse(parseTree); err == nil {
				// pg_query.Deparse() adds "USING btree" even when it wasn't in the original SQL.
				// This causes errors for spatial types (point, geography, geometry) which need GIST/SP-GIST.
				if stmt.ObjectType == types.TypeIndex && !stmt.IndexHadExplicitAccessMethod {
					deparsed = stripImplicitUsingBtreeClause(deparsed)
				}
				b.Statement(deparsed)
				return b
			}
		}
	}

	// If both original SQL and AST conversion fail, return empty
	return b
}

func (b *SQLBuilder) fromDropStatement(stmt types.Statement) *SQLBuilder {
	if stmt.ObjectType == types.TypeUnknown || stmt.ObjectType == "" {
		// Cannot generate valid DROP statement for unknown object types
		// Generate a comment instead
		b.P("-- WARNING: Cannot generate DROP statement for object '").P(stmt.ObjectName).P("' with unknown type")
		b.NL()
		b.P("-- Consider manual cleanup if this object exists in your database")
		return b
	}

	b.P("DROP").S().P(string(stmt.ObjectType))
	if stmt.IfNotExists {
		b.P("IF EXISTS")
	}
	b.S().Quote(stmt.ObjectName)

	// DROP POLICY requires ON table_name clause
	if stmt.ObjectType == types.TypePolicy && len(stmt.Dependencies) > 0 {
		b.S().P("ON").S().Quote(stmt.Dependencies[0])
	}
	return b
}

func (b *SQLBuilder) fromGrantStatement(stmt types.Statement) *SQLBuilder {
	b.P("GRANT").S().P(strings.Join(stmt.Privileges, ", "))
	b.S().P("ON").S().P(string(stmt.ObjectType)).S().Quote(stmt.ObjectName)
	b.S().P("TO").S().P(strings.Join(stmt.Grantees, ", "))
	return b
}

func (b *SQLBuilder) fromRevokeStatement(stmt types.Statement) *SQLBuilder {
	b.P("REVOKE").S().P(strings.Join(stmt.Privileges, ", "))
	b.S().P("ON").S().P(string(stmt.ObjectType)).S().Quote(stmt.ObjectName)
	b.S().P("FROM").S().P(strings.Join(stmt.Grantees, ", "))
	return b
}

func stripImplicitUsingBtreeClause(sql string) string {
	if strings.TrimSpace(sql) == "" {
		return sql
	}

	lower := strings.ToLower(sql)
	for i := 0; i < len(lower); i++ {
		if !hasBuilderKeywordAt(lower, i, "using") {
			continue
		}

		j := skipBuilderWhitespace(lower, i+len("using"))
		if !hasBuilderKeywordAt(lower, j, "btree") {
			continue
		}

		start := i
		for start > 0 && isBuilderWhitespaceByte(lower[start-1]) {
			start--
		}

		end := j + len("btree")
		for end < len(lower) && isBuilderWhitespaceByte(lower[end]) {
			end++
		}

		prefix := sql[:start]
		suffix := sql[end:]

		if prefix != "" && suffix != "" && !isBuilderWhitespaceByte(prefix[len(prefix)-1]) && !isBuilderWhitespaceByte(suffix[0]) {
			return prefix + " " + suffix
		}

		return prefix + suffix
	}

	return sql
}

func hasBuilderKeywordAt(value string, pos int, keyword string) bool {
	if pos < 0 || pos+len(keyword) > len(value) {
		return false
	}

	if value[pos:pos+len(keyword)] != keyword {
		return false
	}

	if pos > 0 && isBuilderIdentifierByte(value[pos-1]) {
		return false
	}

	end := pos + len(keyword)
	if end < len(value) && isBuilderIdentifierByte(value[end]) {
		return false
	}

	return true
}

func skipBuilderWhitespace(value string, pos int) int {
	for pos < len(value) && isBuilderWhitespaceByte(value[pos]) {
		pos++
	}
	return pos
}

func isBuilderIdentifierByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func isBuilderWhitespaceByte(ch byte) bool {
	switch ch {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	default:
		return false
	}
}
