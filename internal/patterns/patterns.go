package patterns

import "regexp"

// Precompiled regex patterns to avoid repeated compilation (5-10× performance improvement)
// All patterns are compiled once at package initialization and reused throughout the application
var (
	// SQL Parsing Patterns - used in internal/utils/sql_parsing.go
	FunctionPattern = regexp.MustCompile(`(?i)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+(?:(?:"?[a-z_][a-z0-9_]*"?\.)?)?(?:"?([a-z_][a-z0-9_]*)"?)\s*\(`)
	CreateTablePattern = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:(?:"?([a-z_][a-z0-9_]*)"?\.)?)?(?:"?([a-z_][a-z0-9_]*)"?)`)
	AlterTablePattern = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?(?:(?:"?([a-z_][a-z0-9_]*)"?\.)?)?(?:"?([a-z_][a-z0-9_]*)"?)`)
	CreateSchemaPattern = regexp.MustCompile(`(?i)CREATE\s+SCHEMA\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:"?([a-z_][a-z0-9_]*)"?)`)
	CreateIndexPattern = regexp.MustCompile(`(?i)CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:CONCURRENTLY\s+)?(?:"?([a-z_][a-z0-9_]*)"?)`)

	// SQL Transformation Patterns - used in internal/transformation/sql_transformer.go
	InsertPattern = regexp.MustCompile(`(?i)^\s*INSERT\s+INTO\s+`)
	UpdatePattern = regexp.MustCompile(`(?i)^\s*UPDATE\s+`)
	DeletePattern = regexp.MustCompile(`(?i)^\s*DELETE\s+FROM\s+`)
	DropTablePattern = regexp.MustCompile(`(?i)^\s*DROP\s+TABLE\s+`)
	DropColumnPattern = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+\w+\s+DROP\s+COLUMN\s+`)
	AlterTypePattern = regexp.MustCompile(`(?i)ALTER\s+TABLE\s+\w+\s+ALTER\s+COLUMN\s+\w+\s+TYPE\s+`)
	NoIndexPattern = regexp.MustCompile(`(?i)WHERE\s+[^=]+\s*=\s*[^=]+`)
	LargeScanPattern = regexp.MustCompile(`(?i)SELECT\s+\*\s+FROM\s+\w+\s*;?\s*$`)
	OldJoinPattern = regexp.MustCompile(`(?i)WHERE\s+\w+\.\w+\s*=\s*\w+\.\w+`)
	OldFunctionPattern = regexp.MustCompile(`(?i)substr\(|length\(|position\(`)

	// Detailed Insert/Update/Delete Patterns
	InsertDetailPattern = regexp.MustCompile(`(?i)INSERT\s+INTO\s+(\w+(?:\.\w+)?)\s*(?:\(([^)]+)\))?\s*VALUES\s*\(([^)]+)\)`)
	UpdateDetailPattern = regexp.MustCompile(`(?i)UPDATE\s+(\w+(?:\.\w+)?)\s+SET\s+(.+?)(?:\s+WHERE\s+(.+))?$`)
	SetClausePattern = regexp.MustCompile(`(\w+)\s*=\s*([^,]+)`)
	DeleteDetailPattern = regexp.MustCompile(`(?i)DELETE\s+FROM\s+(\w+(?:\.\w+)?)(?:\s+WHERE\s+(.+))?$`)

	// Function Analysis Patterns
	ReturnsTablePattern = regexp.MustCompile(`(?ims)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*\([^)]*\)\s*RETURNS\s+TABLE\s*\(\s*([^)]+)\)`)
	FunctionBodyPattern = regexp.MustCompile(`(?s)AS\s+\$\$(.+?)\$\$`)
	ReturnNextPattern = regexp.MustCompile(`RETURN\s+NEXT(?:\s+([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)*))?(\s*);`)
	FunctionWhitespacePattern = regexp.MustCompile(`(?m)\$\$\s*\n\s*([^;])`)
	FunctionSignaturePattern = regexp.MustCompile(`(?ims)(CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+(?:[a-z_][a-z0-9_]*\.)?[a-z_][a-z0-9_]*\s*\([^)]*\)\s*RETURNS\s+(?:TABLE\s*\([^)]+\)|SETOF\s+[^\s]+|[^\s]+))((?:\s+(?:LANGUAGE\s+[a-z]+|SECURITY\s+DEFINER|SET\s+[^\s]+\s*=\s*[^\s]+|VOLATILE|STABLE|IMMUTABLE))*?)(\s+AS\s+(?:\$\$|\$[a-z0-9_]*\$|\'))`)
	FunctionNamePattern = regexp.MustCompile(`(?i)FUNCTION\s+(?:[a-z_][a-z0-9_]*\.)?([a-z_][a-z0-9_]*)`)

	// Dependency Resolution Patterns - used in internal/squasher/unified_dependency_resolver.go
	ForeignKeyPattern = regexp.MustCompile(`(?i)FOREIGN\s+KEY\s*\([^)]+\)\s*REFERENCES\s+([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)`)
	DirectRefPattern = regexp.MustCompile(`(?i)REFERENCES\s+([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)`)
	SQLCommentPattern = regexp.MustCompile(`--[^\n]*`) // SQL line comments (-- ...)
	CommentOnPattern = regexp.MustCompile(`(?i)COMMENT\s+ON\s+(?:COLUMN|TABLE)\s+([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)`) // COMMENT ON TABLE/COLUMN statements
	QualifiedNamePattern = regexp.MustCompile(`(?i)([a-zA-Z_][a-zA-Z0-9_]*)\.[a-zA-Z_][a-zA-Z0-9_]*`)
	InsertIntoPattern = regexp.MustCompile(`(?i)INSERT\s+INTO\s+([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)\s*\(\s*([^)]+)\)`)
	ExecuteFunctionPattern = regexp.MustCompile(`(?i)EXECUTE\s+FUNCTION\s+([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)`)
	FunctionCallPattern = regexp.MustCompile(`\b([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)\s*\(`)
	SetParameterPattern = regexp.MustCompile(`(?i)SET\s+([^=\s]+)\s*=`)
	UpdateTablePattern = regexp.MustCompile(`(?i)UPDATE\s+([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)`)
	CreateTableDetailPattern = regexp.MustCompile(`(?i)CREATE\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-zA-Z_][a-zA-Z0-9_]*(?:\.[a-zA-Z_][a-zA-Z0-9_]*)?)`)
	CreateFunctionDetailPattern = regexp.MustCompile(`(?i)CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+(?:[a-zA-Z_][a-zA-Z0-9_]*\.)?([a-zA-Z_][a-zA-Z0-9_]*)\s*\(`)
	CreateSchemaDetailPattern = regexp.MustCompile(`(?i)CREATE\s+SCHEMA\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-zA-Z_][a-zA-Z0-9_]*)`)
	CreateExtensionPattern = regexp.MustCompile(`(?i)CREATE\s+EXTENSION\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-zA-Z_][a-zA-Z0-9_-]*)`)
	CreateExtensionWithVersionPattern = regexp.MustCompile(`(?i)(CREATE\s+EXTENSION\s+(?:IF\s+NOT\s+EXISTS\s+)?)([a-zA-Z_][a-zA-Z0-9_-]*)(\s*;?)`)

	// Validation Patterns - used in internal/validation/validator.go
	CreateExtensionValidationPattern = regexp.MustCompile(`CREATE\s+EXTENSION\s+(?:IF\s+NOT\s+EXISTS\s+)?["']?(\w+)["']?`)
	DropExtensionPattern = regexp.MustCompile(`DROP\s+EXTENSION\s+(?:IF\s+EXISTS\s+)?["']?(\w+)["']?`)
	AlterPublicationPattern = regexp.MustCompile(`(ALTER\s+PUBLICATION\s+\w+\s+ADD\s+TABLE\s+\w+)(\s*\n)`)
	TransactionPattern = regexp.MustCompile(`(ALTER\s+PUBLICATION[^;]+)(\s*\n\s*ALTER\s+PUBLICATION)`)
	CreatePolicyPattern = regexp.MustCompile(`(CREATE\s+POLICY\s+\w+[^;]+)(\s*\n(?:CREATE|ALTER))`)
	ExtensionNamePattern = regexp.MustCompile(`CREATE\s+EXTENSION\s+(?:IF\s+NOT\s+EXISTS\s+)?["']?(\w+)["']?`)
	FunctionHeaderPattern = regexp.MustCompile(`(?i)CREATE\s+FUNCTION\s+`)
	CreateOrReplaceFunctionPattern = regexp.MustCompile(`(?i)CREATE\s+OR\s+REPLACE\s+FUNCTION\s+`)

	// Publication Deduplication - used in internal/validation/publication_dedup.go
	PublicationPattern = regexp.MustCompile(`(?i)ALTER\s+PUBLICATION\s+["]?([^"\s]+)["]?\s+ADD\s+TABLE\s+(?:["]?([^"\s.]+)["]?\.)?["]?([^"\s;]+)["]?`)

	// Backup Generator Patterns - used in internal/transformation/backup_generator.go
	AddColumnPattern = regexp.MustCompile(`ADD\s+COLUMN\s+(?:IF\s+NOT\s+EXISTS\s+)?([a-zA-Z_][a-zA-Z0-9_]*)`)
	DropColumnBackupPattern = regexp.MustCompile(`DROP\s+COLUMN\s+(?:IF\s+EXISTS\s+)?([a-zA-Z_][a-zA-Z0-9_]*)`)
	AddConstraintPattern = regexp.MustCompile(`ADD\s+CONSTRAINT\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	DropConstraintPattern = regexp.MustCompile(`DROP\s+CONSTRAINT\s+(?:IF\s+EXISTS\s+)?([a-zA-Z_][a-zA-Z0-9_]*)`)
	AlterColumnTypePattern = regexp.MustCompile(`ALTER\s+COLUMN\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+(?:SET\s+DATA\s+)?TYPE\s+([a-zA-Z_][a-zA-Z0-9_()]+)`)
	AlterColumnNullPattern = regexp.MustCompile(`ALTER\s+COLUMN\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+(SET|DROP)\s+NOT\s+NULL`)
	AlterColumnDefaultPattern = regexp.MustCompile(`ALTER\s+COLUMN\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+(SET|DROP)\s+DEFAULT`)
	RenameTablePattern = regexp.MustCompile(`RENAME\s+TO\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	RenameColumnPattern = regexp.MustCompile(`RENAME\s+COLUMN\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+TO\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	EnableTriggerPattern = regexp.MustCompile(`ENABLE\s+TRIGGER\s+([a-zA-Z_][a-zA-Z0-9_]*)`)
	DisableTriggerPattern = regexp.MustCompile(`DISABLE\s+TRIGGER\s+([a-zA-Z_][a-zA-Z0-9_]*)`)

	// Circular FK Handler Patterns - used in internal/squasher/circular_fk_handler.go
	InlineConstraintPattern = regexp.MustCompile(`CONSTRAINT\s+\w+\s+FOREIGN KEY\s*\([^)]+\)\s*REFERENCES[^,)]*`)

	// Drizzle Plugin Patterns - used in internal/plugins/drizzle/
	DrizzleFilePattern = regexp.MustCompile(`drizzle/\d{14}_[a-z_]+`)
	IdentitySequencePattern = regexp.MustCompile(`(?i)GENERATED\s+(ALWAYS|BY\s+DEFAULT)\s+AS\s+IDENTITY\s*\(\s*sequence\s+name\s+"[^"]+_seq"`)
	GeneratedIdentityPattern = regexp.MustCompile(`(?i)GENERATED\s+(ALWAYS|BY\s+DEFAULT)\s+AS\s+IDENTITY`)
	GeneratedIdentityDetailPattern = regexp.MustCompile(`(?i)(GENERATED\s+(ALWAYS|BY\s+DEFAULT)\s+AS\s+IDENTITY\s*\([^)]+\))`)
	GeneratedColumnPattern = regexp.MustCompile(`(?i)GENERATED\s+ALWAYS\s+AS\s+\([^)]+\)\s+STORED`)
	CacheOnePattern = regexp.MustCompile(`(?i)\s+CACHE\s+1\s*`)
	IncrementByOnePattern = regexp.MustCompile(`(?i)\s+INCREMENT\s+BY\s+1\s*`)
	GeneratedAsPattern = regexp.MustCompile(`(?i)(\s+AS\s+)`)
	SerialPrimaryKeyPattern = regexp.MustCompile(`(?i)(\w+)\s+SERIAL\s+(PRIMARY\s+KEY)`)
	BigserialPrimaryKeyPattern = regexp.MustCompile(`(?i)(\w+)\s+BIGSERIAL\s+(PRIMARY\s+KEY)`)
	GeneratedAlwaysPattern = regexp.MustCompile(`(?i)GENERATED\s+ALWAYS\s+AS\s+IDENTITY`)
	GeneratedByDefaultPattern = regexp.MustCompile(`(?i)GENERATED\s+BY\s+DEFAULT\s+AS\s+IDENTITY`)

	// Enum Deduplication Pattern - used in internal/tracking/consolidation/enum_dedup_rule.go
	EnumTypePattern = regexp.MustCompile(`(?is)CREATE\s+TYPE\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+AS\s+ENUM\s*\((.*?)\)`)

	// Column Type Usage Pattern - used in internal/squasher/unified_dependency_resolver.go
	// Matches: column_name type_name (with optional DEFAULT, CHECK, NOT NULL, etc.)
	ColumnTypePattern = regexp.MustCompile(`(?i)\b([a-zA-Z_][a-zA-Z0-9_]*)\s+([a-zA-Z_][a-zA-Z0-9_]*)(?:\s|,|\)|$|DEFAULT|CHECK|NOT NULL|UNIQUE|PRIMARY KEY|REFERENCES)`)

	// PostgreSQL Type Patterns - used in internal/types/postgresql_types.go
	ArrayTypePattern = regexp.MustCompile(`^(.+?)(\[\d*\])+$`)
	PrecisionTypePattern = regexp.MustCompile(`^(varchar|character varying|char|character|numeric|decimal|time|timestamp|interval|bit)\s*\(\d+(?:,\d+)?\)$`)
	PrecisionExtractPattern = regexp.MustCompile(`\((\d+)(?:,\d+)?\)`)

	// Utility Patterns - used in internal/utils/strings.go
	MultipleSpacePattern = regexp.MustCompile(`\s+`)

	// Engine Patterns - used in internal/squasher/engine.go
	ObjectReferencePattern = regexp.MustCompile(`(?i)(?:FROM|JOIN|INTO|UPDATE|ALTER\s+TABLE)\s+([a-z_][a-z0-9_]*)`)
	DuplicateEnumCommentPattern = regexp.MustCompile(`-- Duplicate ENUM eliminated: ([a-zA-Z_][a-zA-Z0-9_]*) \(similar to ([a-zA-Z_][a-zA-Z0-9_]*)\)`)
	EnumReplacementPattern = func(eliminatedName string) *regexp.Regexp {
		return regexp.MustCompile(`\b` + regexp.QuoteMeta(eliminatedName) + `\b`)
	}
	CircularFKCommentPattern = regexp.MustCompile(
		`(?i)-- circular foreign key constraint removed:\s*([^(]+)\(([^)]+)\)\s*->\s*([^(]+)\(([^)]+)\)`,
	)
)

// BuildPatternWithEscape creates a regex pattern with escaped special characters
// Useful for user-provided strings that need to be matched literally
func BuildPatternWithEscape(text string) *regexp.Regexp {
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(text) + `\b`)
}

// BuildPatternList compiles a list of patterns from strings
// Used when patterns need to be dynamically generated
func BuildPatternList(patterns []string) []*regexp.Regexp {
	compiled := make([]*regexp.Regexp, 0, len(patterns))
	for _, pattern := range patterns {
		if re, err := regexp.Compile(pattern); err == nil {
			compiled = append(compiled, re)
		}
	}
	return compiled
}
