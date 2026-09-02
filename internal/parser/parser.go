package parser

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/errors"
	"github.com/capy-base/pgsquash-engine/internal/plugins"
	"github.com/capy-base/pgsquash-engine/internal/types"
	"github.com/capy-base/pgsquash-engine/internal/utils"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// ParseMigration parses a migration file with enhanced PostgreSQL-specific processing
func ParseMigration(content string, filename string) (*types.Migration, error) {
	ctx := context.Background()
	return ParseMigrationWithContext(ctx, content, filename)
}

// ParseMigrationWithContext parses a migration file with context and enhanced error handling
func ParseMigrationWithContext(ctx context.Context, content string, filename string) (*types.Migration, error) {
	// Initialize error handler
	errorHandler := NewErrorHandler(ctx)
	defer errorHandler.Recovery(filename, 0)

	// Initialize normalizer with PostgreSQL defaults
	normalizer := NewContextualNormalizer(DefaultNormalizationContext())

	// Clean and normalize SQL content
	cleanContent := cleanSQL(content)

	// Extract comments separately
	cleanContent, comments := extractComments(cleanContent)

	// Split into individual statements
	stmts, err := pg_query.SplitWithScanner(cleanContent, true)
	if err != nil {
		parseCtx := errorHandler.CreateContext(filename, 0, nil)
		if handleErr := errorHandler.HandleParseError(err, parseCtx); handleErr != nil {
			utils.GetDefaultLogger().Warn("Failed to handle parse error: %v", handleErr)
		}
		if !errorHandler.ShouldContinue() {
			return nil, errors.Wrap(err, errors.ErrorCodeSyntaxError, errors.CategoryParsing, "failed to split statements", nil)
		}
		// If we should continue, create empty stmts slice
		stmts = []string{}
	}

	// If splitting returned no statements but we have content, try parsing to detect syntax errors
	if len(stmts) == 0 && strings.TrimSpace(cleanContent) != "" {
		_, parseErr := pg_query.Parse(cleanContent)
		if parseErr != nil {
			parseCtx := errorHandler.CreateContext(filename, 0, nil)
			parseCtx.StatementText = cleanContent
			if handleErr := errorHandler.HandleParseError(parseErr, parseCtx); handleErr != nil {
				utils.GetDefaultLogger().Warn("Failed to handle parse error: %v", handleErr)
			}
		}
	}

	migration := &types.Migration{
		Filename:    filename,
		Statements:  make([]types.Statement, 0),
		Size:        int64(len(content)),
		ParseErrors: make([]string, 0),
	}

	searchCursor := 0
	for i, stmtStr := range stmts {
		if strings.TrimSpace(stmtStr) == "" {
			continue
		}

		stmtLine, stmtColumn, nextCursor := locateStatementPosition(cleanContent, stmtStr, searchCursor)
		searchCursor = nextCursor

		// Parse the statement normally
		stmt, err := parseStatementWithNormalizationAndContext(stmtStr, stmtLine, stmtColumn, normalizer, errorHandler, filename)
		if err != nil {
			// Error was already handled by parseStatementWithNormalizationAndContext
			if !errorHandler.ShouldContinue() {
				break
			}
			continue
		}

		// Assign relevant comments to statement
		stmt.Comments = getRelevantComments(comments, i)
		stmt.Category = categorizeStatement(*stmt)

		migration.Statements = append(migration.Statements, *stmt)
	}

	// Collect parse errors from error handler (but don't log summary yet - caller will aggregate)
	for _, parseErr := range errorHandler.GetCollector().GetErrors() {
		migration.ParseErrors = append(migration.ParseErrors, parseErr.Error())
	}

	// Return migration with errors attached even when parsing fails
	// This allows partial parsing with error warnings instead of complete failure
	if errorHandler.GetCollector().HasErrors() {
		utils.GetDefaultLogger().WithPrefix("PARSER").Info("Warning: Parsed %d statements with %d errors in %s",
			len(migration.Statements), len(migration.ParseErrors), filename)

		if len(migration.Statements) == 0 && len(migration.ParseErrors) > 0 {
			// Return error to indicate catastrophic failure
			return migration, errors.New(errors.ErrorCodeSyntaxError, errors.CategoryParsing, fmt.Sprintf("fatal: all statements failed to parse in %s: %d parse errors, 0 statements recovered", filename, len(migration.ParseErrors)), nil)
		}

		return migration, errors.New(
			errors.ErrorCodeSyntaxError,
			errors.CategoryParsing,
			fmt.Sprintf("migration %s contains %d statement parse errors", filename, len(migration.ParseErrors)),
			nil,
		)
	}

	// migration is never nil here since it was initialized above
	return migration, nil
}

// parseStatementWithNormalizationAndContext parses a statement with context and error handling
func parseStatementWithNormalizationAndContext(sql string, line int, column int, normalizer *ContextualNormalizer, errorHandler *ErrorHandler, filename string) (*types.Statement, error) {
	defer errorHandler.Recovery(filename, line)

	parsed, err := pg_query.Parse(sql)
	if err != nil {
		parseCtx := errorHandler.CreateContext(filename, line, nil)
		parseCtx.StatementText = sql
		if handleErr := errorHandler.HandleParseError(err, parseCtx); handleErr != nil {
			utils.GetDefaultLogger().Warn("Failed to handle parse error: %v", handleErr)
		}
		return nil, err
	}

	if len(parsed.Stmts) == 0 {
		parseCtx := errorHandler.CreateContext(filename, line, nil)
		parseCtx.StatementText = sql
		err := errors.New(errors.ErrorCodeSyntaxError, errors.CategoryParsing, "no statements found", nil)
		if handleErr := errorHandler.HandleValidationError(err.Error(), parseCtx); handleErr != nil {
			utils.GetDefaultLogger().Warn("Failed to handle validation error: %v", handleErr)
		}
		return nil, err
	}

	stmt := &types.Statement{
		SQL:       strings.TrimSpace(sql),
		ParseTree: parsed,
		Filename:  filename,
		Line:      line,
		Column:    column,
	}

	// Analyze first statement with normalization
	rawStmt := parsed.Stmts[0]
	analyzeStatementWithNormalization(rawStmt, stmt, normalizer)

	// Check for IF NOT EXISTS for supported node types
	switch n := rawStmt.Stmt.Node.(type) {
	case *pg_query.Node_CreateStmt:
		stmt.IfNotExists = n.CreateStmt.IfNotExists
	case *pg_query.Node_IndexStmt:
		stmt.IfNotExists = n.IndexStmt.IfNotExists
	case *pg_query.Node_CreateExtensionStmt:
		stmt.IfNotExists = n.CreateExtensionStmt.IfNotExists
	}

	// Enhanced analysis for new features
	stmt.Schema = extractSchemaWithNormalization(stmt, normalizer)
	stmt.CrossSchema = extractCrossSchemaReferences(stmt)
	stmt.AuthPattern = detectAuthPattern(stmt)
	stmt.IsDynamic = isDynamicSQL(stmt)

	// Add naming convention warnings if applicable
	if stmt.ObjectName != "" {
		parseCtx := errorHandler.CreateContext(filename, line, stmt)
		validateNamingConventions(stmt, errorHandler.GetCollector(), parseCtx)
	}

	// Plugin enrichment: Allow plugins to add metadata to statements
	// This is called after pg_query parsing but before returning the statement
	// Plugins can enhance auth patterns, mark critical statements, add dependencies, etc.
	enrichStatementWithPlugins(context.Background(), stmt)

	// Analyze statement metadata (lock levels, transaction requirements, etc.)
	// Use PostgreSQL 17 as default version (can be overridden by config later)
	analyzer := NewStatementAnalyzer("17")
	analyzer.AnalyzeStatement(stmt)
	analyzer.AnalyzePragmas(stmt)

	return stmt, nil
}

func locateStatementPosition(content string, statement string, searchFrom int) (line int, column int, nextSearchFrom int) {
	if searchFrom < 0 {
		searchFrom = 0
	}
	if searchFrom > len(content) {
		searchFrom = len(content)
	}

	absoluteOffset := searchFrom
	searchTarget := statement

	if idx := strings.Index(content[searchFrom:], statement); idx >= 0 {
		absoluteOffset = searchFrom + idx
	} else {
		trimmed := strings.TrimSpace(statement)
		if trimmed != "" {
			if idx := strings.Index(content[searchFrom:], trimmed); idx >= 0 {
				absoluteOffset = searchFrom + idx
				searchTarget = trimmed
			}
		}
	}

	line, column = offsetToLineColumn(content, absoluteOffset)
	nextSearchFrom = max(absoluteOffset+len(searchTarget), searchFrom)

	return line, column, nextSearchFrom
}

func offsetToLineColumn(content string, offset int) (line int, column int) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(content) {
		offset = len(content)
	}

	line = 1
	column = 1
	for i := 0; i < offset; i++ {
		if content[i] == '\n' {
			line++
			column = 1
			continue
		}
		column++
	}

	return line, column
}

// analyzeStatementWithNormalization analyzes statements with PostgreSQL-specific normalization
func analyzeStatementWithNormalization(raw *pg_query.RawStmt, stmt *types.Statement, normalizer *ContextualNormalizer) {
	switch node := raw.Stmt.Node.(type) {
	case *pg_query.Node_CreateStmt:
		stmt.ObjectType = types.TypeTable
		stmt.Operation = types.OpCreate
		stmt.ObjectName = getTableNameWithNormalization(node.CreateStmt.Relation, normalizer)
		stmt.Dependencies = extractTableDependenciesWithNormalization(node.CreateStmt, normalizer)

	case *pg_query.Node_AlterTableStmt:
		stmt.ObjectType = types.TypeTable
		stmt.Operation = types.OpAlter
		stmt.ObjectName = getTableNameWithNormalization(node.AlterTableStmt.Relation, normalizer)

		// Extract constraint information from ALTER TABLE commands
		stmt.Dependencies = extractAlterTableConstraints(node.AlterTableStmt, normalizer)

	case *pg_query.Node_RenameStmt:
		// RenameStmt covers: ALTER TABLE ... RENAME COLUMN, ALTER TABLE ... RENAME TO, etc.
		renameStmt := node.RenameStmt
		stmt.Operation = types.OpAlter // RENAME is a type of ALTER operation

		// Extract table name from the Relation
		if renameStmt.Relation != nil {
			stmt.ObjectName = getTableNameWithNormalization(renameStmt.Relation, normalizer)
			stmt.ObjectType = types.TypeTable
		}

		// No dependencies for RENAME operations
		stmt.Dependencies = []string{}

	case *pg_query.Node_DropStmt:
		stmt.Operation = types.OpDrop
		if len(node.DropStmt.Objects) > 0 {
			stmt.ObjectType = mapObjectType(node.DropStmt.RemoveType)
			firstObject := node.DropStmt.Objects[0]
			if node.DropStmt.RemoveType == pg_query.ObjectType_OBJECT_POLICY {
				tableName, policyName := extractDropPolicyDetails(firstObject, normalizer)
				if policyName != "" {
					stmt.ObjectName = policyName
				} else {
					stmt.ObjectName = extractObjectNameWithNormalization(firstObject, normalizer)
				}
				if tableName != "" {
					stmt.Dependencies = append(stmt.Dependencies, tableName)
				}
			} else if node.DropStmt.RemoveType == pg_query.ObjectType_OBJECT_TRIGGER {
				// DROP TRIGGER syntax: DROP TRIGGER [IF EXISTS] trigger_name ON table_name
				// The pg_query parser returns a list like [schema?, table, trigger]
				tableName, triggerName := extractDropTriggerDetails(firstObject, normalizer)
				if triggerName != "" {
					stmt.ObjectName = triggerName
				} else {
					stmt.ObjectName = extractObjectNameWithNormalization(firstObject, normalizer)
				}
				if tableName != "" {
					stmt.Dependencies = append(stmt.Dependencies, tableName)
				}
			} else if firstObject != nil {
				stmt.ObjectName = extractObjectNameWithNormalization(firstObject, normalizer)
			}
		}

	case *pg_query.Node_IndexStmt:
		stmt.ObjectType = types.TypeIndex
		stmt.Operation = types.OpCreate
		// Normalize index name, potentially adding schema from relation
		idxName := normalizer.NormalizeIdentifier(node.IndexStmt.Idxname)
		if node.IndexStmt.Relation != nil && node.IndexStmt.Relation.Schemaname != "" {
			// If relation has explicit schema, prepend it to index name for consistency
			// Note: Indexes live in the same schema as their table
			schema := normalizer.NormalizeSchemaName(node.IndexStmt.Relation.Schemaname)
			stmt.ObjectName = fmt.Sprintf("%s.%s", schema, idxName)
		} else if node.IndexStmt.Relation != nil && normalizer.context.DefaultSchema != "" {
			// Infer partial qualification from default schema if available
			stmt.ObjectName = fmt.Sprintf("%s.%s", normalizer.context.DefaultSchema, idxName)
		} else {
			stmt.ObjectName = idxName
		}

		// This prevents pg_query.Deparse from adding "USING btree" to spatial indexes
		// Check the original SQL, not the AST, since pg_query may have normalized it
		if strings.Contains(strings.ToUpper(stmt.SQL), " USING ") {
			stmt.IndexHadExplicitAccessMethod = true
		}
		if node.IndexStmt.Relation != nil {
			stmt.Dependencies = []string{getTableNameWithNormalization(node.IndexStmt.Relation, normalizer)}
		}

	case *pg_query.Node_CreateTrigStmt:
		stmt.ObjectType = types.TypeTrigger
		stmt.Operation = types.OpCreate
		stmt.ObjectName = normalizer.NormalizeIdentifier(node.CreateTrigStmt.Trigname)
		if node.CreateTrigStmt.Relation != nil {
			stmt.Dependencies = []string{getTableNameWithNormalization(node.CreateTrigStmt.Relation, normalizer)}
		}
		if node.CreateTrigStmt.Funcname != nil {
			funcName := extractFunctionNameWithNormalization(node.CreateTrigStmt.Funcname, normalizer)
			stmt.Dependencies = append(stmt.Dependencies, funcName)
		}

	case *pg_query.Node_ViewStmt:
		stmt.ObjectType = types.TypeView
		stmt.Operation = types.OpCreate
		if node.ViewStmt.View != nil {
			stmt.ObjectName = getTableNameWithNormalization(node.ViewStmt.View, normalizer)
		}
		// This ensures views are placed AFTER the tables they reference
		if node.ViewStmt.Query != nil {
			stmt.Dependencies = extractQueryDependencies(node.ViewStmt.Query, normalizer)
		}

	case *pg_query.Node_CreateTableAsStmt:
		// Handle CREATE MATERIALIZED VIEW (and CREATE TABLE AS)
		// MATERIALIZED VIEW is parsed as CreateTableAsStmt with Objtype = OBJECT_MATVIEW
		stmt.ObjectType = types.TypeView // Treat materialized views as views
		stmt.Operation = types.OpCreate
		if node.CreateTableAsStmt.Into != nil && node.CreateTableAsStmt.Into.Rel != nil {
			stmt.ObjectName = getTableNameWithNormalization(node.CreateTableAsStmt.Into.Rel, normalizer)
		}
		// Extract table dependencies from the SELECT query
		if node.CreateTableAsStmt.Query != nil {
			stmt.Dependencies = extractQueryDependencies(node.CreateTableAsStmt.Query, normalizer)
		}
		// Note: Regular CREATE TABLE AS SELECT would also come here, but is uncommon in migrations

	case *pg_query.Node_CreatePolicyStmt:
		stmt.ObjectType = types.TypePolicy
		stmt.Operation = types.OpCreate

		policyName := normalizer.NormalizeIdentifier(node.CreatePolicyStmt.PolicyName)
		if node.CreatePolicyStmt.Table != nil {
			tableName := getTableNameWithNormalization(node.CreatePolicyStmt.Table, normalizer)
			stmt.ObjectName = fmt.Sprintf("%s.%s", tableName, policyName)
			stmt.Dependencies = []string{tableName}
		} else {
			stmt.ObjectName = policyName
		}

		// Extract dependencies from USING (Qual) and WITH CHECK (WithCheck) clauses
		if node.CreatePolicyStmt.Qual != nil {
			stmt.Dependencies = append(stmt.Dependencies, extractExpressionDependencies(node.CreatePolicyStmt.Qual, normalizer)...)
		}
		if node.CreatePolicyStmt.WithCheck != nil {
			stmt.Dependencies = append(stmt.Dependencies, extractExpressionDependencies(node.CreatePolicyStmt.WithCheck, normalizer)...)
		}

		// Check for RESTRICTIVE policies
		if strings.Contains(stmt.SQL, "AS RESTRICTIVE") {
			stmt.Comments = append(stmt.Comments, "-- RESTRICTIVE policy")
		}

	case *pg_query.Node_InsertStmt:
		stmt.Operation = types.OpInsert
		stmt.ObjectType = types.TypeData
		stmt.IsDataOp = true
		stmt.ObjectName = getTableNameWithNormalization(node.InsertStmt.Relation, normalizer)
		// Check for ON CONFLICT clause
		if node.InsertStmt.OnConflictClause != nil {
			stmt.Comments = append(stmt.Comments, "-- Contains ON CONFLICT clause")
		}

	case *pg_query.Node_UpdateStmt:
		stmt.Operation = types.OpUpdate
		stmt.ObjectType = types.TypeData
		stmt.IsDataOp = true
		stmt.ObjectName = getTableNameWithNormalization(node.UpdateStmt.Relation, normalizer)

	case *pg_query.Node_DeleteStmt:
		stmt.Operation = types.OpDelete
		stmt.ObjectType = types.TypeData
		stmt.IsDataOp = true
		stmt.ObjectName = getTableNameWithNormalization(node.DeleteStmt.Relation, normalizer)

	case *pg_query.Node_GrantStmt:
		stmt.Operation = types.OpGrant
		stmt.ObjectType = mapGrantObjectType(node.GrantStmt.Objtype)
		if len(node.GrantStmt.Objects) > 0 {
			stmt.ObjectName = extractObjectNameWithNormalization(node.GrantStmt.Objects[0], normalizer)
		}
		stmt.Grantees = extractGranteesWithNormalization(node.GrantStmt.Grantees, normalizer)
		stmt.Privileges = extractPrivileges(node.GrantStmt.Privileges)

	case *pg_query.Node_GrantRoleStmt:
		if node.GrantRoleStmt.IsGrant {
			stmt.Operation = types.OpGrant
		} else {
			stmt.Operation = types.OpRevoke
		}
		stmt.ObjectType = types.TypeRole
		if len(node.GrantRoleStmt.GrantedRoles) > 0 {
			stmt.ObjectName = extractRoleNameWithNormalization(node.GrantRoleStmt.GrantedRoles[0], normalizer)
		}
		stmt.Grantees = extractGranteeRolesWithNormalization(node.GrantRoleStmt.GranteeRoles, normalizer)
	case *pg_query.Node_CreateFunctionStmt:
		stmt.ObjectType = types.TypeFunction
		stmt.Operation = types.OpCreate
		if node.CreateFunctionStmt.Funcname != nil {
			stmt.ObjectName = extractFunctionNameWithNormalization(node.CreateFunctionStmt.Funcname, normalizer)

			// Extract schema explicitly for dependency matching
			// Dependency matching compares Dep.Schema == Lifecycle.Schema
			funcNameParts := node.CreateFunctionStmt.Funcname
			if len(funcNameParts) >= 2 {
				// Has schema qualification (e.g. public.func)
				stmt.Schema = normalizer.NormalizeIdentifier(funcNameParts[0].GetString_().Sval)
			} else {
				// Unqualified, defaults to public
				stmt.Schema = "public"
			}
		}

	case *pg_query.Node_CreateExtensionStmt:
		stmt.ObjectType = types.TypeExtension
		stmt.Operation = types.OpCreate
		stmt.ObjectName = normalizer.NormalizeIdentifier(node.CreateExtensionStmt.Extname)

	case *pg_query.Node_CommentStmt:
		// This prevents "Object X::VIEW depends on X which is never created" false warnings
		// where COMMENT ON VIEW was being stored as a VIEW object instead of a COMMENT object
		stmt.ObjectType = types.TypeComment
		stmt.Operation = types.OpComment
		// Extract object name and type from comment statement
		if node.CommentStmt.Object != nil {
			stmt.ObjectName = extractCommentObjectNameWithNormalization(node.CommentStmt, normalizer)
			// Extract dependency on the object being commented (with proper type)
			// This creates a dependency like: "collection_contents::COMMENT depends on collection_contents::VIEW"
			stmt.Dependencies = extractCommentDependenciesWithNormalization(node.CommentStmt, normalizer)
		}

	case *pg_query.Node_CreatePublicationStmt:
		stmt.ObjectType = types.TypePublication
		stmt.Operation = types.OpCreate
		stmt.ObjectName = normalizer.NormalizeIdentifier(node.CreatePublicationStmt.Pubname)

	case *pg_query.Node_AlterPublicationStmt:
		stmt.ObjectType = types.TypePublication
		stmt.Operation = types.OpAlter
		stmt.ObjectName = normalizer.NormalizeIdentifier(node.AlterPublicationStmt.Pubname)

	case *pg_query.Node_DoStmt:
		// Check if this DO block contains CREATE TYPE statements
		if nestedTypes := extractNestedTypesFromDoBlock(stmt.SQL); len(nestedTypes) > 0 {
			// Treat DO blocks with CREATE TYPE as TYPE statements
			if len(nestedTypes) == 1 {
				stmt.ObjectType = types.TypeEnum
				stmt.Operation = types.OpCreate
				stmt.ObjectName = normalizer.NormalizeIdentifier(nestedTypes[0])
			} else {
				// Multiple types - treat as generic TYPE block
				stmt.ObjectType = types.TypeType
				stmt.Operation = types.OpCreate
				stmt.ObjectName = "multiple_types_block"
				stmt.Dependencies = nestedTypes
			}
		} else {
			// Handle DO blocks (anonymous code blocks)
			// These are often used for data migration or complex logic
			stmt.ObjectType = types.TypeDoBlock
			stmt.Operation = types.Operation("UNKNOWN")
			// Use a hash of the content to ensure unique identity for each DO block
			// This prevents them from being consolidated into a single "anonymous_block" object
			// which would mess up sorting (using the first block's position for all)
			sum := sha256.Sum256([]byte(stmt.SQL))
			stmt.ObjectName = fmt.Sprintf("anonymous_block_%x", sum[:8])
		}

	case *pg_query.Node_CreateEnumStmt:
		stmt.ObjectType = types.TypeEnum
		stmt.Operation = types.OpCreate
		if len(node.CreateEnumStmt.TypeName) > 0 {
			stmt.ObjectName = normalizer.NormalizeIdentifier(node.CreateEnumStmt.TypeName[len(node.CreateEnumStmt.TypeName)-1].GetString_().Sval)
		}

	case *pg_query.Node_AlterEnumStmt:
		// Handle ALTER TYPE ... ADD VALUE statements
		stmt.ObjectType = types.TypeEnum
		stmt.Operation = types.OpAlter
		if len(node.AlterEnumStmt.TypeName) > 0 {
			stmt.ObjectName = normalizer.NormalizeIdentifier(node.AlterEnumStmt.TypeName[len(node.AlterEnumStmt.TypeName)-1].GetString_().Sval)
		}
		// Store the new value being added for consolidation
		if node.AlterEnumStmt.NewVal != "" {
			stmt.AlterTypeNewValue = node.AlterEnumStmt.NewVal
		}

	case *pg_query.Node_CompositeTypeStmt:
		stmt.ObjectType = types.TypeComposite
		stmt.Operation = types.OpCreate
		if node.CompositeTypeStmt.Typevar != nil {
			stmt.ObjectName = normalizer.NormalizeIdentifier(node.CompositeTypeStmt.Typevar.Relname)
		}

	case *pg_query.Node_CreateDomainStmt:
		stmt.ObjectType = types.TypeDomain
		stmt.Operation = types.OpCreate
		if len(node.CreateDomainStmt.Domainname) > 0 {
			stmt.ObjectName = normalizer.NormalizeIdentifier(node.CreateDomainStmt.Domainname[len(node.CreateDomainStmt.Domainname)-1].GetString_().Sval)
		}

	case *pg_query.Node_SelectStmt:
		// Handle SELECT statements to track function calls and data reads
		// Mark as data operation so they're tracked even without an object name
		stmt.ObjectType = types.TypeUnknown
		stmt.Operation = types.Operation("SELECT")
		stmt.IsDataOp = true
		stmt.ObjectName = "select_statement" // Generic name for tracking purposes

	default:
		stmt.ObjectType = types.TypeUnknown
		stmt.Operation = types.Operation("UNKNOWN")
		stmt.Metadata.PreserveVerbatim = true
		sum := sha256.Sum256([]byte(stmt.SQL))
		stmt.ObjectName = fmt.Sprintf("verbatim_statement_%x", sum[:8])
	}
}

func categorizeStatement(stmt types.Statement) types.Category {
	if stmt.IsDataOp {
		return types.CategoryData
	}

	// DO blocks usually contain ad-hoc data operations (INSERTs) or scripts
	// Treat them as Data to preserve migration order
	if stmt.ObjectType == types.TypeDoBlock {
		return types.CategoryData
	}

	// COMMENT ON statements have ObjectType set to the object being commented (e.g., TypeTable),
	// but must be categorized as CategoryComments to ensure they come after object creation
	if stmt.Operation == types.OpComment {
		// Comments must come after the objects they reference
		return types.CategoryComments
	}

	switch stmt.ObjectType {
	case types.TypeTable, types.TypeSequence, types.TypeType, types.TypeEnum, types.TypeComposite, types.TypeDomain:
		return types.CategoryFoundation
	case types.TypeView:
		// Views (including materialized views) are foundational objects
		// They must be created before indexes and triggers can reference them
		return types.CategoryFoundation
	case types.TypeFunction:
		// CRITICAL: Functions MUST be in CategoryFoundation (not CategoryFunctions)
		// because tables may reference functions in CHECK constraints
		// Within CategoryFoundation, topological sort will order functions before tables
		return types.CategoryFoundation
	case types.TypeConstraint:
		return types.CategoryConstraints
	case types.TypeIndex:
		return types.CategoryIndexes
	case types.TypeTrigger:
		return types.CategoryTriggers
	case types.TypePolicy, types.TypeRole, types.TypePublication:
		return types.CategorySecurity
	case types.TypeExtension:
		return types.CategoryExtensions
	case types.TypeComment:
		// Comments must come after the objects they reference
		// Note: This case is now redundant due to the OpComment check above,
		// but kept for clarity and as a fallback
		return types.CategoryComments
	default:
		return types.CategoryFoundation
	}
}

// getTableNameWithNormalization extracts and normalizes table names with schema awareness
func getTableNameWithNormalization(rangeVar *pg_query.RangeVar, normalizer *ContextualNormalizer) string {
	if rangeVar == nil {
		return ""
	}

	schema := normalizer.NormalizeSchemaName(rangeVar.Schemaname)
	table := normalizer.NormalizeIdentifier(rangeVar.Relname)

	// Always use the normalized schema which handles defaults
	if schema != "" {
		return fmt.Sprintf("%s.%s", schema, table)
	}
	return table
}

// extractTableDependenciesWithNormalization extracts foreign key dependencies with normalization
func extractTableDependenciesWithNormalization(createStmt *pg_query.CreateStmt, normalizer *ContextualNormalizer) []string {
	var deps []string

	// Extract foreign key dependencies from table elements
	for _, tableElt := range createStmt.TableElts {
		// Check table-level constraints
		if constraint := tableElt.GetConstraint(); constraint != nil {
			if constraint.Contype == pg_query.ConstrType_CONSTR_FOREIGN {
				if constraint.Pktable != nil {
					refTableName := getTableNameWithNormalization(constraint.Pktable, normalizer)
					if refTableName != "" {
						deps = append(deps, fmt.Sprintf("REFERENCES:%s", refTableName))
					}
				}
			}
		}

		// Check column-level constraints (inline REFERENCES clauses)
		if columnDef := tableElt.GetColumnDef(); columnDef != nil {
			// Extract type dependencies from column type
			if columnDef.TypeName != nil {
				typeName := extractTypeNameFromTypeName(columnDef.TypeName, normalizer)
				if typeName != "" && !isBuiltInType(typeName) {
					// This is a custom type (ENUM, COMPOSITE, DOMAIN, etc.)
					deps = append(deps, typeName)
				}
			}

			for _, colConstraint := range columnDef.Constraints {
				if constraint := colConstraint.GetConstraint(); constraint != nil {
					if constraint.Contype == pg_query.ConstrType_CONSTR_FOREIGN {
						if constraint.Pktable != nil {
							refTableName := getTableNameWithNormalization(constraint.Pktable, normalizer)
							if refTableName != "" {
								deps = append(deps, fmt.Sprintf("REFERENCES:%s", refTableName))
							}
						}
					}
				}
			}
		}
	}

	return deps
}

// extractTypeNameFromTypeName extracts the type name from a TypeName node
func extractTypeNameFromTypeName(typeName *pg_query.TypeName, normalizer *ContextualNormalizer) string {
	if len(typeName.Names) == 0 {
		return ""
	}
	// Get the last component of the type name (the actual type)
	typeNode := typeName.Names[len(typeName.Names)-1]
	if strNode := typeNode.GetString_(); strNode != nil {
		return normalizer.NormalizeIdentifier(strNode.Sval)
	}
	return ""
}

// isBuiltInType checks if a type name is a built-in PostgreSQL type
func isBuiltInType(typeName string) bool {
	builtInTypes := map[string]bool{
		// Numeric types
		"smallint": true, "integer": true, "bigint": true, "int2": true, "int4": true, "int8": true,
		"decimal": true, "numeric": true, "real": true, "double precision": true,
		"smallserial": true, "serial": true, "bigserial": true,
		"float4": true, "float8": true,
		// Monetary types
		"money": true,
		// Character types
		"character varying": true, "varchar": true, "character": true, "char": true, "text": true,
		// Binary types
		"bytea": true,
		// Date/Time types
		"timestamp": true, "timestamp without time zone": true, "timestamp with time zone": true,
		"timestamptz": true, "date": true, "time": true, "time without time zone": true,
		"time with time zone": true, "timetz": true, "interval": true,
		// Boolean type
		"boolean": true, "bool": true,
		// Geometric types
		"point": true, "line": true, "lseg": true, "box": true, "path": true, "polygon": true, "circle": true,
		// Network types
		"cidr": true, "inet": true, "macaddr": true, "macaddr8": true,
		// Bit string types
		"bit": true, "bit varying": true, "varbit": true,
		// Text search types
		"tsvector": true, "tsquery": true,
		// UUID type
		"uuid": true,
		// XML type
		"xml": true,
		// JSON types
		"json": true, "jsonb": true,
		// Arrays
		"int[]": true, "text[]": true, "varchar[]": true,
		// Range types
		"int4range": true, "int8range": true, "numrange": true,
		"tsrange": true, "tstzrange": true, "daterange": true,
		// Special types
		"void": true, "record": true, "trigger": true,
		// Common extension types (to prevent false positive dependency warnings)
		"vector": true, "geometry": true, "geography": true, "ltree": true, "hstore": true,
	}
	return builtInTypes[strings.ToLower(typeName)]
}

// extractQueryDependencies extracts table dependencies from a SELECT query (for views)
func extractQueryDependencies(queryNode *pg_query.Node, normalizer *ContextualNormalizer) []string {
	var deps []string

	// Handle SelectStmt (most common case for views)
	if selectStmt := queryNode.GetSelectStmt(); selectStmt != nil {
		// Extract FROM clause tables
		for _, fromClause := range selectStmt.FromClause {
			if rangeVar := fromClause.GetRangeVar(); rangeVar != nil {
				tableName := getTableNameWithNormalization(rangeVar, normalizer)
				if tableName != "" {
					deps = append(deps, tableName)
				}
			}
			// Handle JOINs (JoinExpr)
			if joinExpr := fromClause.GetJoinExpr(); joinExpr != nil {
				deps = append(deps, extractJoinDependencies(joinExpr, normalizer)...)
			}
			// Handle subqueries (RangeSubselect)
			if rangeSubselect := fromClause.GetRangeSubselect(); rangeSubselect != nil {
				if rangeSubselect.Subquery != nil {
					// Recursively extract dependencies from subquery
					deps = append(deps, extractQueryDependencies(rangeSubselect.Subquery, normalizer)...)
				}
			}
		}
	}

	return deps
}

// extractJoinDependencies recursively extracts table names from JOIN expressions
func extractJoinDependencies(joinExpr *pg_query.JoinExpr, normalizer *ContextualNormalizer) []string {
	var deps []string

	// Left side of JOIN
	if joinExpr.Larg != nil {
		if rangeVar := joinExpr.Larg.GetRangeVar(); rangeVar != nil {
			tableName := getTableNameWithNormalization(rangeVar, normalizer)
			if tableName != "" {
				deps = append(deps, tableName)
			}
		}
		if nestedJoin := joinExpr.Larg.GetJoinExpr(); nestedJoin != nil {
			deps = append(deps, extractJoinDependencies(nestedJoin, normalizer)...)
		}
		// Handle subquery on left side
		if rangeSubselect := joinExpr.Larg.GetRangeSubselect(); rangeSubselect != nil {
			if rangeSubselect.Subquery != nil {
				deps = append(deps, extractQueryDependencies(rangeSubselect.Subquery, normalizer)...)
			}
		}
	}

	// Right side of JOIN
	if joinExpr.Rarg != nil {
		if rangeVar := joinExpr.Rarg.GetRangeVar(); rangeVar != nil {
			tableName := getTableNameWithNormalization(rangeVar, normalizer)
			if tableName != "" {
				deps = append(deps, tableName)
			}
		}
		if nestedJoin := joinExpr.Rarg.GetJoinExpr(); nestedJoin != nil {
			deps = append(deps, extractJoinDependencies(nestedJoin, normalizer)...)
		}
		// Handle subquery on right side
		if rangeSubselect := joinExpr.Rarg.GetRangeSubselect(); rangeSubselect != nil {
			if rangeSubselect.Subquery != nil {
				deps = append(deps, extractQueryDependencies(rangeSubselect.Subquery, normalizer)...)
			}
		}
	}

	return deps
}

// extractAlterTableConstraints extracts constraint information from ALTER TABLE statements
func extractAlterTableConstraints(alterStmt *pg_query.AlterTableStmt, normalizer *ContextualNormalizer) []string {
	var constraints []string

	// Process each ALTER TABLE command
	for _, cmd := range alterStmt.Cmds {
		if alterCmd := cmd.GetAlterTableCmd(); alterCmd != nil {
			switch alterCmd.Subtype {
			case pg_query.AlterTableType_AT_AddConstraint:
				// Extract constraint name from ADD CONSTRAINT commands
				if constraint := alterCmd.Def.GetConstraint(); constraint != nil {
					if constraint.Conname != "" {
						constraintName := normalizer.NormalizeIdentifier(constraint.Conname)
						constraints = append(constraints, fmt.Sprintf("CONSTRAINT:%s", constraintName))
					}

					// Extract foreign key dependencies
					if constraint.Contype == pg_query.ConstrType_CONSTR_FOREIGN {
						if constraint.Pktable != nil {
							refTableName := getTableNameWithNormalization(constraint.Pktable, normalizer)
							if refTableName != "" {
								constraints = append(constraints, fmt.Sprintf("REFERENCES:%s", refTableName))
							}
						}
					}
				}
			case pg_query.AlterTableType_AT_AddColumn:
				// Track column additions for consolidation
				if columnDef := alterCmd.Def.GetColumnDef(); columnDef != nil {
					columnName := normalizer.NormalizeIdentifier(columnDef.Colname)
					constraints = append(constraints, fmt.Sprintf("COLUMN:%s", columnName))
				}
			}
		}
	}

	return constraints
}

// extractFunctionNameWithNormalization extracts function names with normalization
func extractFunctionNameWithNormalization(funcname []*pg_query.Node, normalizer *ContextualNormalizer) string {
	if len(funcname) == 0 {
		return ""
	}

	var parts []string
	for _, part := range funcname {
		if str := part.GetString_(); str != nil {
			parts = append(parts, normalizer.NormalizeIdentifier(str.Sval))
		}
	}

	// Normalize: always add "public" schema if not present (simplified heuristic)
	// This prevents duplication between "func" and "public.func"
	if len(parts) == 1 {
		return "public." + parts[0]
	}

	return strings.Join(parts, ".")
}

// extractGranteesWithNormalization extracts grantees with normalization
func extractGranteesWithNormalization(grantees []*pg_query.Node, normalizer *ContextualNormalizer) []string {
	var names []string
	for _, grantee := range grantees {
		if roleSpec := grantee.GetRoleSpec(); roleSpec != nil {
			names = append(names, normalizer.NormalizeIdentifier(roleSpec.Rolename))
		}
	}
	return names
}

func extractPrivileges(privileges []*pg_query.Node) []string {
	var names []string
	for _, privilege := range privileges {
		if privilege.GetAccessPriv() != nil {
			if privilege.GetAccessPriv().PrivName != "" {
				names = append(names, privilege.GetAccessPriv().PrivName)
			} else {
				names = append(names, "ALL") // ALL PRIVILEGES
			}
		} else if str := privilege.GetString_(); str != nil {
			names = append(names, str.Sval)
		} else {
			names = append(names, "UNKNOWN_PRIVILEGE")
		}
	}
	if len(names) == 0 {
		names = append(names, "ALL") // Default to ALL if no specific privileges
	}
	return names
}

// extractRoleNameWithNormalization extracts role names with normalization
func extractRoleNameWithNormalization(role *pg_query.Node, normalizer *ContextualNormalizer) string {
	if roleSpec := role.GetRoleSpec(); roleSpec != nil {
		return normalizer.NormalizeIdentifier(roleSpec.Rolename)
	}
	if str := role.GetString_(); str != nil {
		return normalizer.NormalizeIdentifier(str.Sval)
	}
	return ""
}

// extractGranteeRolesWithNormalization extracts grantee roles with normalization
func extractGranteeRolesWithNormalization(granteeRoles []*pg_query.Node, normalizer *ContextualNormalizer) []string {
	var names []string
	for _, grantee := range granteeRoles {
		if name := extractRoleNameWithNormalization(grantee, normalizer); name != "" {
			names = append(names, name)
		}
	}
	return names
}

func mapGrantObjectType(objType pg_query.ObjectType) types.ObjectType {
	switch objType {
	case pg_query.ObjectType_OBJECT_TABLE:
		return types.TypeTable
	case pg_query.ObjectType_OBJECT_SEQUENCE:
		return types.TypeSequence
	case pg_query.ObjectType_OBJECT_SCHEMA:
		return types.TypeSchema
	case pg_query.ObjectType_OBJECT_FUNCTION:
		return types.TypeFunction
	default:
		return types.TypeUnknown
	}
}

func mapObjectType(removeType pg_query.ObjectType) types.ObjectType {
	switch removeType {
	case pg_query.ObjectType_OBJECT_TABLE:
		return types.TypeTable
	case pg_query.ObjectType_OBJECT_INDEX:
		return types.TypeIndex
	case pg_query.ObjectType_OBJECT_FUNCTION:
		return types.TypeFunction
	case pg_query.ObjectType_OBJECT_TRIGGER:
		return types.TypeTrigger
	case pg_query.ObjectType_OBJECT_VIEW:
		return types.TypeView
	case pg_query.ObjectType_OBJECT_SEQUENCE:
		return types.TypeSequence
	case pg_query.ObjectType_OBJECT_POLICY:
		return types.TypePolicy
	default:
		return types.TypeUnknown
	}
}

// extractObjectNameWithNormalization extracts object names with normalization
func extractObjectNameWithNormalization(obj *pg_query.Node, normalizer *ContextualNormalizer) string {
	if list := obj.GetList(); list != nil {
		var parts []string
		for _, item := range list.Items {
			if str := item.GetString_(); str != nil {
				parts = append(parts, normalizer.NormalizeIdentifier(str.Sval))
			}
		}

		// Normalize: always add default schema (public) if not present
		if len(parts) == 1 {
			return fmt.Sprintf("%s.%s", normalizer.NormalizeSchemaName(""), parts[0])
		}
		return strings.Join(parts, ".")
	}

	// Handle ObjectWithArgs (used in DROP FUNCTION foo(args))
	if args := obj.GetObjectWithArgs(); args != nil {
		var parts []string
		for _, item := range args.Objname {
			if str := item.GetString_(); str != nil {
				parts = append(parts, normalizer.NormalizeIdentifier(str.Sval))
			}
		}
		// NOTE: currently we ignore args.Objargs signatures to match extractFunctionNameWithNormalization behavior
		// This ensures CREATE FUNCTION foo() and DROP FUNCTION foo() generate matching keys.
		// TODO: Support full function signatures in object keys for strict overloading support.

		// Normalize: always add default schema (public) if not present
		if len(parts) == 1 {
			return fmt.Sprintf("%s.%s", normalizer.NormalizeSchemaName(""), parts[0])
		}
		return strings.Join(parts, ".")
	}

	return ""
}

// extractDropPolicyDetails returns the associated table (schema-qualified when available)
// and policy name from a DROP POLICY statement target list.
func extractDropPolicyDetails(obj *pg_query.Node, normalizer *ContextualNormalizer) (string, string) {
	if obj == nil {
		return "", ""
	}

	list := obj.GetList()
	if list == nil || len(list.Items) == 0 {
		return "", ""
	}

	var tableParts []string
	var policyName string

	for idx, item := range list.Items {
		str := item.GetString_()
		if str == nil {
			continue
		}

		name := normalizer.NormalizeIdentifier(str.Sval)
		if idx == len(list.Items)-1 {
			policyName = name
			continue
		}

		tableParts = append(tableParts, name)
	}

	var tableName string
	if len(tableParts) == 1 {
		// Preserve unqualified table names as-is for DROP POLICY ... ON table_name.
		// Schema defaults are derived separately from AST extraction when needed.
		tableName = tableParts[0]
	} else if len(tableParts) > 1 {
		// Already qualified
		tableName = strings.Join(tableParts, ".")
	}

	return tableName, policyName
}

// extractDropTriggerDetails returns the associated table (schema-qualified when available)
// and trigger name from a DROP TRIGGER statement target list.
// DROP TRIGGER syntax: DROP TRIGGER [IF EXISTS] trigger_name ON table_name
// The pg_query parser returns a list like [schema?, table, trigger]
func extractDropTriggerDetails(obj *pg_query.Node, normalizer *ContextualNormalizer) (string, string) {
	if obj == nil {
		return "", ""
	}

	list := obj.GetList()
	if list == nil || len(list.Items) == 0 {
		return "", ""
	}

	var tableParts []string
	var triggerName string

	for idx, item := range list.Items {
		str := item.GetString_()
		if str == nil {
			continue
		}

		name := normalizer.NormalizeIdentifier(str.Sval)
		if idx == len(list.Items)-1 {
			// Last element is the trigger name
			triggerName = name
			continue
		}

		// All other elements are part of the table name (schema.table or just table)
		tableParts = append(tableParts, name)
	}

	var tableName string
	if len(tableParts) == 1 {
		tableName = tableParts[0]
	} else if len(tableParts) > 1 {
		tableName = strings.Join(tableParts, ".")
	}

	return tableName, triggerName
}

func extractComments(content string) (string, []string) {
	lines := strings.Split(content, "\n")
	var cleanLines []string
	var comments []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			comments = append(comments, trimmed)
		} else {
			cleanLines = append(cleanLines, line)
		}
	}

	return strings.Join(cleanLines, "\n"), comments
}

func getRelevantComments(comments []string, stmtIndex int) []string {
	// Simple heuristic: assign comments to nearby statements
	// This could be enhanced with more sophisticated logic
	var relevant []string

	if stmtIndex < len(comments) {
		relevant = append(relevant, comments[stmtIndex])
	}

	return relevant
}

// Helper functions for enhanced PostgreSQL parsing
// extractCommentObjectNameWithNormalization extracts comment object names with normalization
func extractCommentObjectNameWithNormalization(commentStmt *pg_query.CommentStmt, normalizer *ContextualNormalizer) string {
	// Extract object name from comment statement with normalization
	if commentStmt.Object != nil {
		return extractObjectNameWithNormalization(commentStmt.Object, normalizer)
	}
	return ""
}

func mapCommentObjectType(objtype pg_query.ObjectType) types.ObjectType {
	// This prevents "never created" warnings for COMMENT ON VIEW, COMMENT ON POLICY, etc.
	switch objtype {
	case pg_query.ObjectType_OBJECT_TABLE:
		return types.TypeTable
	case pg_query.ObjectType_OBJECT_COLUMN:
		return types.TypeTable // Column comments are table-related
	case pg_query.ObjectType_OBJECT_VIEW:
		return types.TypeView
	case pg_query.ObjectType_OBJECT_INDEX:
		return types.TypeIndex
	case pg_query.ObjectType_OBJECT_FUNCTION:
		return types.TypeFunction
	case pg_query.ObjectType_OBJECT_TRIGGER:
		return types.TypeTrigger
	case pg_query.ObjectType_OBJECT_SEQUENCE:
		return types.TypeSequence
	case pg_query.ObjectType_OBJECT_POLICY:
		return types.TypePolicy
	case pg_query.ObjectType_OBJECT_SCHEMA:
		return types.TypeSchema
	case pg_query.ObjectType_OBJECT_EXTENSION:
		return types.TypeExtension
	case pg_query.ObjectType_OBJECT_TYPE:
		return types.TypeType
	default:
		return types.TypeComment
	}
}

// extractCommentDependenciesWithNormalization extracts the target object that a COMMENT ON references
// Returns dependencies with type prefix in format: "TYPE:name" (e.g., "VIEW:collection_contents")
// This allows proper dependency tracking using the same pattern as CONSTRAINT: and COLUMN: prefixes
func extractCommentDependenciesWithNormalization(commentStmt *pg_query.CommentStmt, normalizer *ContextualNormalizer) []string {
	if commentStmt.Object == nil {
		return nil
	}

	var deps []string

	// Extract the target object name
	targetName := extractObjectNameWithNormalization(commentStmt.Object, normalizer)
	if targetName != "" {
		// Get the target object type
		targetType := mapCommentObjectType(commentStmt.Objtype)

		// For COMMENT ON COLUMN, extract just the table name
		if commentStmt.Objtype == pg_query.ObjectType_OBJECT_COLUMN {
			// Format is: table_name.column_name, we want just table_name
			parts := strings.Split(targetName, ".")
			if len(parts) >= 2 {
				// If schema.table.column, use TABLE:schema.table
				if len(parts) == 3 {
					deps = append(deps, fmt.Sprintf("%s:%s.%s", types.TypeTable, parts[0], parts[1]))
				} else {
					// If table.column, use TABLE:table
					deps = append(deps, fmt.Sprintf("%s:%s", types.TypeTable, parts[0]))
				}
			}
		} else {
			// For other object types, use prefix format: "TYPE:name"
			// This matches the CONSTRAINT: and COLUMN: prefix pattern used elsewhere
			deps = append(deps, fmt.Sprintf("%s:%s", targetType, targetName))
		}
	}

	return deps
}

// Processing state to prevent duplicate analysis
type StatementProcessingState struct {
	StatementID      string
	ProcessedBy      []string
	FinalState       *types.Statement
	ConflictFlags    []string
	DependencySource string // Which function detected the dependencies
}

// extractSchemaWithNormalization extracts schema names from AST with normalization
// Uses AST data instead of regex heuristics for accurate schema extraction
func extractSchemaWithNormalization(stmt *types.Statement, normalizer *ContextualNormalizer) string {
	if stmt.ParseTree == nil {
		return "public" // Default if no parse tree available
	}

	// Access ParseResult directly
	parseResult := stmt.ParseTree
	if parseResult == nil || len(parseResult.Stmts) == 0 {
		return "public"
	}

	rawStmt := parseResult.Stmts[0]
	schema := extractSchemaFromAST(rawStmt, normalizer)

	if schema != "" {
		return schema
	}

	// Default to public schema if no explicit schema found in AST
	return "public"
}

// extractSchemaFromAST extracts schema name from AST node
func extractSchemaFromAST(rawStmt *pg_query.RawStmt, normalizer *ContextualNormalizer) string {
	if rawStmt == nil || rawStmt.Stmt == nil {
		return ""
	}

	switch node := rawStmt.Stmt.Node.(type) {
	case *pg_query.Node_CreateStmt:
		if node.CreateStmt.Relation != nil && node.CreateStmt.Relation.Schemaname != "" {
			return normalizer.NormalizeSchemaName(node.CreateStmt.Relation.Schemaname)
		}

	case *pg_query.Node_AlterTableStmt:
		if node.AlterTableStmt.Relation != nil && node.AlterTableStmt.Relation.Schemaname != "" {
			return normalizer.NormalizeSchemaName(node.AlterTableStmt.Relation.Schemaname)
		}

	case *pg_query.Node_DropStmt:
		if len(node.DropStmt.Objects) > 0 {
			if node.DropStmt.RemoveType == pg_query.ObjectType_OBJECT_POLICY {
				tableName, _ := extractDropPolicyDetails(node.DropStmt.Objects[0], normalizer)
				if tableName != "" {
					if strings.Contains(tableName, ".") {
						return strings.SplitN(tableName, ".", 2)[0]
					}
					return "public"
				}
			}
			return extractSchemaFromObjectList(node.DropStmt.Objects[0], normalizer)
		}

	case *pg_query.Node_IndexStmt:
		if node.IndexStmt.Relation != nil && node.IndexStmt.Relation.Schemaname != "" {
			return normalizer.NormalizeSchemaName(node.IndexStmt.Relation.Schemaname)
		}

	case *pg_query.Node_CreateFunctionStmt:
		if len(node.CreateFunctionStmt.Funcname) > 1 {
			// First element is schema for schema-qualified functions
			if strNode := node.CreateFunctionStmt.Funcname[0].GetString_(); strNode != nil {
				return normalizer.NormalizeSchemaName(strNode.Sval)
			}
		}

	case *pg_query.Node_CreateTrigStmt:
		if node.CreateTrigStmt.Relation != nil && node.CreateTrigStmt.Relation.Schemaname != "" {
			return normalizer.NormalizeSchemaName(node.CreateTrigStmt.Relation.Schemaname)
		}

	case *pg_query.Node_ViewStmt:
		if node.ViewStmt.View != nil && node.ViewStmt.View.Schemaname != "" {
			return normalizer.NormalizeSchemaName(node.ViewStmt.View.Schemaname)
		}

	case *pg_query.Node_CreatePolicyStmt:
		if node.CreatePolicyStmt.Table != nil && node.CreatePolicyStmt.Table.Schemaname != "" {
			return normalizer.NormalizeSchemaName(node.CreatePolicyStmt.Table.Schemaname)
		}

	case *pg_query.Node_InsertStmt:
		if node.InsertStmt.Relation != nil && node.InsertStmt.Relation.Schemaname != "" {
			return normalizer.NormalizeSchemaName(node.InsertStmt.Relation.Schemaname)
		}

	case *pg_query.Node_UpdateStmt:
		if node.UpdateStmt.Relation != nil && node.UpdateStmt.Relation.Schemaname != "" {
			return normalizer.NormalizeSchemaName(node.UpdateStmt.Relation.Schemaname)
		}

	case *pg_query.Node_DeleteStmt:
		if node.DeleteStmt.Relation != nil && node.DeleteStmt.Relation.Schemaname != "" {
			return normalizer.NormalizeSchemaName(node.DeleteStmt.Relation.Schemaname)
		}

	case *pg_query.Node_GrantStmt:
		if len(node.GrantStmt.Objects) > 0 {
			return extractSchemaFromObjectList(node.GrantStmt.Objects[0], normalizer)
		}

	case *pg_query.Node_CreateSchemaStmt:
		if node.CreateSchemaStmt.Schemaname != "" {
			return normalizer.NormalizeSchemaName(node.CreateSchemaStmt.Schemaname)
		}

	case *pg_query.Node_CreateSeqStmt:
		if node.CreateSeqStmt.Sequence != nil && node.CreateSeqStmt.Sequence.Schemaname != "" {
			return normalizer.NormalizeSchemaName(node.CreateSeqStmt.Sequence.Schemaname)
		}

	case *pg_query.Node_CreateEnumStmt:
		if len(node.CreateEnumStmt.TypeName) > 1 {
			// First element is schema for schema-qualified types
			if strNode := node.CreateEnumStmt.TypeName[0].GetString_(); strNode != nil {
				return normalizer.NormalizeSchemaName(strNode.Sval)
			}
		}

	case *pg_query.Node_CompositeTypeStmt:
		if node.CompositeTypeStmt.Typevar != nil && node.CompositeTypeStmt.Typevar.Schemaname != "" {
			return normalizer.NormalizeSchemaName(node.CompositeTypeStmt.Typevar.Schemaname)
		}

	case *pg_query.Node_CreateDomainStmt:
		if len(node.CreateDomainStmt.Domainname) > 1 {
			// First element is schema for schema-qualified domains
			if strNode := node.CreateDomainStmt.Domainname[0].GetString_(); strNode != nil {
				return normalizer.NormalizeSchemaName(strNode.Sval)
			}
		}
	}

	return "" // No explicit schema found in AST
}

// extractSchemaFromObjectList extracts schema from pg_query object list node
func extractSchemaFromObjectList(objNode *pg_query.Node, normalizer *ContextualNormalizer) string {
	if objNode == nil {
		return ""
	}

	if listNode := objNode.GetList(); listNode != nil && len(listNode.Items) > 0 {
		// Schema is typically the first element in qualified names
		if strNode := listNode.Items[0].GetString_(); strNode != nil {
			// Check if there are more elements (indicating schema.object format)
			if len(listNode.Items) > 1 {
				return normalizer.NormalizeSchemaName(strNode.Sval)
			}
		}
	}

	return ""
}

// validateNamingConventions checks object names against PostgreSQL naming conventions
func validateNamingConventions(stmt *types.Statement, collector *ErrorCollector, ctx *ParseContext) {
	if stmt.ObjectName == "" {
		return
	}

	objectName := stmt.ObjectName

	// Check length limits (PostgreSQL limit is 63 characters)
	if len(objectName) > 63 {
		collector.AddNamingWarning(
			fmt.Sprintf("Object name '%s' exceeds PostgreSQL limit of 63 characters", objectName),
			"Consider shortening the name",
			ctx,
		)
	}

	// Check for reserved words
	keywordManager := NewVersionedKeywordManager(17) // PostgreSQL 17
	if keywordManager.IsReservedKeyword(objectName) {
		collector.AddNamingWarning(
			fmt.Sprintf("Object name '%s' is a PostgreSQL reserved keyword", objectName),
			"Consider using a different name or quoting the identifier",
			ctx,
		)
	}

	// Check naming conventions based on object type
	switch stmt.ObjectType {
	case types.TypeTable:
		validateTableNaming(objectName, collector, ctx)
	case types.TypeIndex:
		validateIndexNaming(objectName, collector, ctx)
	case types.TypeFunction:
		validateFunctionNaming(objectName, collector, ctx)
	case types.TypeConstraint:
		validateConstraintNaming(objectName, collector, ctx)
	case types.TypePolicy:
		validatePolicyNaming(objectName, collector, ctx)
	}
}

// validateTableNaming validates table naming conventions
func validateTableNaming(name string, collector *ErrorCollector, ctx *ParseContext) {
	// Prefer snake_case for table names
	if strings.Contains(name, " ") {
		collector.AddNamingWarning(
			fmt.Sprintf("Table name '%s' contains spaces", name),
			"Use snake_case naming convention",
			ctx,
		)
	}

	// Check for camelCase
	if name != strings.ToLower(name) && !strings.Contains(name, "_") {
		collector.AddNamingWarning(
			fmt.Sprintf("Table name '%s' appears to use camelCase", name),
			"PostgreSQL convention prefers snake_case for table names",
			ctx,
		)
	}
}

// validateIndexNaming validates index naming conventions
func validateIndexNaming(name string, collector *ErrorCollector, ctx *ParseContext) {
	// Index names should be descriptive
	commonBadPrefixes := []string{"idx_", "index_", "i_"}
	for _, prefix := range commonBadPrefixes {
		if strings.HasPrefix(strings.ToLower(name), prefix) {
			collector.AddNamingWarning(
				fmt.Sprintf("Index name '%s' uses generic prefix '%s'", name, prefix),
				"Consider more descriptive naming like 'tablename_column_idx'",
				ctx,
			)
			break
		}
	}
}

// validateFunctionNaming validates function naming conventions
func validateFunctionNaming(name string, collector *ErrorCollector, ctx *ParseContext) {
	// Function names should be descriptive verbs
	if len(name) < 3 {
		collector.AddNamingWarning(
			fmt.Sprintf("Function name '%s' is very short", name),
			"Use descriptive verb-based names for functions",
			ctx,
		)
	}
}

// validateConstraintNaming validates constraint naming conventions
func validateConstraintNaming(name string, collector *ErrorCollector, ctx *ParseContext) {
	// Constraint names should indicate their type
	constraintPrefixes := []string{"pk_", "fk_", "ck_", "uq_"}
	hasPrefix := false
	for _, prefix := range constraintPrefixes {
		if strings.HasPrefix(strings.ToLower(name), prefix) {
			hasPrefix = true
			break
		}
	}

	if !hasPrefix {
		collector.AddNamingWarning(
			fmt.Sprintf("Constraint name '%s' doesn't indicate its type", name),
			"Consider prefixes like pk_, fk_, ck_, uq_ for primary key, foreign key, check, unique constraints",
			ctx,
		)
	}
}

// validatePolicyNaming validates RLS policy naming conventions
func validatePolicyNaming(name string, collector *ErrorCollector, ctx *ParseContext) {
	// Policy names should indicate their purpose
	if !strings.Contains(strings.ToLower(name), "policy") &&
		!strings.Contains(strings.ToLower(name), "access") &&
		!strings.Contains(strings.ToLower(name), "security") {

		collector.AddNamingWarning(
			fmt.Sprintf("Policy name '%s' doesn't clearly indicate its security purpose", name),
			"Consider including words like 'policy', 'access', or 'security' in RLS policy names",
			ctx,
		)
	}
}

// extractCrossSchemaReferences finds references to objects in other schemas
func extractCrossSchemaReferences(stmt *types.Statement) []types.CrossSchemaRef {
	var refs []types.CrossSchemaRef
	sql := strings.ToLower(stmt.SQL)

	// Storage schema references
	if strings.Contains(sql, "storage.buckets") {
		refs = append(refs, types.CrossSchemaRef{
			Schema:     "storage",
			ObjectType: types.TypeTable,
			ObjectName: "buckets",
		})
	}

	if strings.Contains(sql, "storage.objects") {
		refs = append(refs, types.CrossSchemaRef{
			Schema:     "storage",
			ObjectType: types.TypeTable,
			ObjectName: "objects",
		})
	}

	// Auth schema references
	authFuncs := []string{"auth.jwt()", "auth.uid()", "auth.role()"}
	for _, authFunc := range authFuncs {
		if strings.Contains(sql, authFunc) {
			funcName := strings.TrimSuffix(strings.TrimPrefix(authFunc, "auth."), "()")
			refs = append(refs, types.CrossSchemaRef{
				Schema:     "auth",
				ObjectType: types.TypeFunction,
				ObjectName: funcName,
			})
		}
	}

	return refs
}

// detectAuthPattern identifies authentication and authorization patterns
// detectAuthPattern detects generic authentication patterns.
// Vendor-specific patterns are detected by plugins via DetectAuthPattern().
// This function only sets generic categories for statements that weren't enriched by plugins.
func detectAuthPattern(stmt *types.Statement) types.AuthPatternType {
	sql := strings.ToLower(stmt.SQL)

	// JWT-based authentication (generic)
	jwtPatterns := []string{
		"auth.jwt()",
		"jwt_claim",
		"validate_jwt",
	}
	for _, pattern := range jwtPatterns {
		if strings.Contains(sql, pattern) {
			return types.AuthPatternJWT
		}
	}

	// Storage policies (generic)
	if strings.Contains(sql, "storage.") && strings.Contains(sql, "create policy") {
		return types.AuthPatternStorage
	}

	// RLS policies (generic)
	if strings.Contains(sql, "row level security") ||
		(strings.Contains(sql, "create policy") && stmt.ObjectType == types.TypePolicy) {
		return types.AuthPatternRLS
	}

	// Session-based authentication (generic)
	sessionPatterns := []string{
		"current_user",
		"session_user",
		"current_role",
	}
	for _, pattern := range sessionPatterns {
		if strings.Contains(sql, pattern) {
			return types.AuthPatternSession
		}
	}

	return types.AuthPatternNone
}

// isDynamicSQL detects dynamic SQL generation patterns
func isDynamicSQL(stmt *types.Statement) bool {
	sql := strings.ToLower(stmt.SQL)

	// Dynamic SQL patterns
	dynamicPatterns := []string{
		"execute format(",
		"execute concat(",
		"execute '",
		"$$ begin", // PL/pgSQL blocks that might contain dynamic SQL
		"foreach",  // Loop constructs
		"array[",   // Array-based dynamic generation
	}

	for _, pattern := range dynamicPatterns {
		if strings.Contains(sql, pattern) {
			return true
		}
	}

	// DO blocks with complex logic
	if stmt.ObjectType == types.TypeDoBlock && containsComplexLogic(sql) {
		return true
	}

	return false
}

// containsComplexLogic checks if a DO block contains complex dynamic logic
func containsComplexLogic(sql string) bool {
	complexPatterns := []string{
		"information_schema.tables", // Schema introspection
		"pg_proc",                   // Function introspection
		"pg_trigger",                // Trigger introspection
		"if exists (select",         // Conditional existence checks
		"foreach",                   // Iteration
		"execute format",            // Dynamic execution
		"raise notice",              // Logging/notifications
	}

	count := 0
	for _, pattern := range complexPatterns {
		if strings.Contains(sql, pattern) {
			count++
		}
	}

	// If multiple complex patterns are present, likely dynamic
	return count >= 2
}

// cleanSQL removes comments and normalizes whitespace
func cleanSQL(sql string) string {
	lines := strings.Split(sql, "\n")
	var cleanLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		// Only remove lines that start with comments
		if !strings.HasPrefix(trimmed, "--") && trimmed != "" {
			cleanLines = append(cleanLines, line)
		}
	}

	cleaned := strings.Join(cleanLines, "\n")

	// Remove multi-line comments
	cleaned = stripSQLBlockComments(cleaned)

	// Normalize whitespace but preserve line breaks for SQL
	cleaned = strings.TrimSpace(cleaned)

	return cleaned
}

// extractNestedTypesFromDoBlock extracts CREATE TYPE statements from DO blocks
func extractNestedTypesFromDoBlock(doBlockSQL string) []string {
	var nestedTypes []string
	tokens := strings.FieldsFunc(doBlockSQL, func(r rune) bool {
		return !(r == '_' || r == '.' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'))
	})

	for i := 0; i+3 < len(tokens); i++ {
		if !strings.EqualFold(tokens[i], "CREATE") || !strings.EqualFold(tokens[i+1], "TYPE") || !strings.EqualFold(tokens[i+3], "AS") {
			continue
		}

		if i+4 >= len(tokens) || !strings.EqualFold(tokens[i+4], "ENUM") {
			continue
		}

		typeName := strings.TrimSpace(tokens[i+2])
		if typeName == "" {
			continue
		}

		// Normalize schema-qualified names to bare type object for this detector.
		if strings.Contains(typeName, ".") {
			parts := strings.Split(typeName, ".")
			typeName = parts[len(parts)-1]
		}

		nestedTypes = append(nestedTypes, typeName)
	}

	return nestedTypes
}

// truncateForLog truncates a string for logging purposes
func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractAlterStatementsFromDoBlock extracts ALTER TABLE statements from DO blocks
// This is critical for handling migrations that wrap ALTER statements in IF NOT EXISTS checks
// Example:
//
//	DO $$ BEGIN
//	  IF NOT EXISTS (...) THEN
//	    ALTER TABLE foo ADD COLUMN bar TEXT;
//	  END IF;
//	END $$;
//
// Returns: ["ALTER TABLE foo ADD COLUMN bar TEXT;"]
func extractAlterStatementsFromDoBlock(doBlockSQL string) []string {
	var ddlStatements []string
	candidates := scanDoBlockForDDLStatements(doBlockSQL)

	// Helper to validate and append statements
	addIfValid := func(stmt string) {
		// Validation: Ensure the extracted statement is actual valid SQL
		// This prevents regex from picking up garbage or partial strings
		if _, err := pg_query.Parse(stmt); err == nil {
			ddlStatements = append(ddlStatements, stmt)
		} else {
			// Log warning but don't fail, maybe it's valid but parser is too strict or it's a fragment
			// For robustness we skip clearly invalid SQL to prevent downstream errors
			utils.GetDefaultLogger().WithPrefix("PARSER").Debug("Skipping invalid extracted DDL: %s (Error: %v)", truncateForLog(stmt, 50), err)
		}
	}

	for _, candidate := range candidates {
		normalized := strings.TrimSpace(normalizeSQLWhitespace(candidate))
		if normalized == "" {
			continue
		}

		if !strings.HasSuffix(normalized, ";") {
			normalized += ";"
		}

		addIfValid(normalized)
	}

	if len(ddlStatements) == 0 && strings.Contains(strings.ToUpper(doBlockSQL), "EXECUTE") && strings.Contains(strings.ToUpper(doBlockSQL), "FORMAT") {
		utils.GetDefaultLogger().WithPrefix("PARSER").Warn(
			"⚠️  DO block contains dynamic SQL (EXECUTE format()) which cannot be statically extracted. " +
				"DDL created dynamically may not appear in squashed output.")
	}

	return ddlStatements
}

// ExtractStaticDDLFromDoBlock returns statically-declared DDL statements from
// a PostgreSQL DO block. Dynamic EXECUTE expressions are intentionally not
// returned because their final SQL cannot be proven without running the block.
//
// ParseMigration preserves the DO block itself; consolidation rules use this
// helper when a safety level explicitly permits unwrapping static DDL.
func ExtractStaticDDLFromDoBlock(doBlockSQL string) []string {
	return extractAlterStatementsFromDoBlock(doBlockSQL)
}

func scanDoBlockForDDLStatements(doBlockSQL string) []string {
	body := extractDoBlockBody(doBlockSQL)
	if strings.TrimSpace(body) == "" {
		body = doBlockSQL
	}

	body = stripSQLBlockComments(body)

	statements := make([]string, 0)

	captureStart := -1
	inLineComment := false
	inBlockComment := false
	inSingleQuote := false
	inDoubleQuote := false
	inDollarQuote := false
	dollarTag := ""

	for i := 0; i < len(body); i++ {
		ch := body[i]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}

		if inBlockComment {
			if ch == '*' && i+1 < len(body) && body[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if !inSingleQuote && !inDoubleQuote {
			if !inDollarQuote && ch == '-' && i+1 < len(body) && body[i+1] == '-' {
				inLineComment = true
				i++
				continue
			}

			if !inDollarQuote && ch == '/' && i+1 < len(body) && body[i+1] == '*' {
				inBlockComment = true
				i++
				continue
			}

			if tag, ok := readDollarTag(body, i); ok {
				if inDollarQuote {
					if tag == dollarTag {
						inDollarQuote = false
						dollarTag = ""
						i += len(tag) - 1
						continue
					}
				} else {
					inDollarQuote = true
					dollarTag = tag
					i += len(tag) - 1
					continue
				}
			}
		}

		if inDollarQuote {
			continue
		}

		if ch == '\'' && !inDoubleQuote {
			if inSingleQuote && i+1 < len(body) && body[i+1] == '\'' {
				i++
				continue
			}
			inSingleQuote = !inSingleQuote
			continue
		}

		if ch == '"' && !inSingleQuote {
			if inDoubleQuote && i+1 < len(body) && body[i+1] == '"' {
				i++
				continue
			}
			inDoubleQuote = !inDoubleQuote
			continue
		}

		if inSingleQuote || inDoubleQuote {
			continue
		}

		if captureStart == -1 && isDoBlockDDLStart(body, i) {
			captureStart = i
			continue
		}

		if captureStart != -1 && ch == ';' {
			statement := strings.TrimSpace(body[captureStart : i+1])
			if statement != "" {
				statements = append(statements, statement)
			}
			captureStart = -1
		}
	}

	return statements
}

func extractDoBlockBody(doBlockSQL string) string {
	trimmed := strings.TrimSpace(doBlockSQL)
	if trimmed == "" {
		return ""
	}

	startSearch := 0
	if doIndex := strings.Index(strings.ToUpper(trimmed), "DO"); doIndex >= 0 {
		startSearch = doIndex + 2
	}

	for i := startSearch; i < len(trimmed); i++ {
		tag, ok := readDollarTag(trimmed, i)
		if !ok {
			continue
		}

		bodyStart := i + len(tag)
		if bodyStart >= len(trimmed) {
			return ""
		}

		if end := strings.LastIndex(trimmed[bodyStart:], tag); end >= 0 {
			return trimmed[bodyStart : bodyStart+end]
		}

		break
	}

	return trimmed
}

func isDoBlockDDLStart(sql string, pos int) bool {
	if hasKeywordSequenceAtCI(sql, pos, "ALTER", "TABLE") {
		return true
	}
	if hasKeywordSequenceAtCI(sql, pos, "CREATE", "UNIQUE", "INDEX") {
		return true
	}
	if hasKeywordSequenceAtCI(sql, pos, "CREATE", "INDEX") {
		return true
	}
	if hasKeywordSequenceAtCI(sql, pos, "CREATE", "TYPE") {
		return true
	}

	return false
}

func hasKeywordSequenceAtCI(sql string, pos int, words ...string) bool {
	if pos < 0 || pos >= len(sql) || len(words) == 0 {
		return false
	}

	if pos > 0 && isDoBlockIdentifierByte(sql[pos-1]) {
		return false
	}

	index := pos
	for _, word := range words {
		index = skipDoBlockWhitespace(sql, index)
		if index+len(word) > len(sql) {
			return false
		}

		segment := sql[index : index+len(word)]
		if !strings.EqualFold(segment, word) {
			return false
		}

		end := index + len(word)
		if end < len(sql) && isDoBlockIdentifierByte(sql[end]) {
			return false
		}

		index = end
	}

	return true
}

func skipDoBlockWhitespace(sql string, pos int) int {
	for pos < len(sql) {
		switch sql[pos] {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			pos++
		default:
			return pos
		}
	}
	return pos
}

func readDollarTag(sql string, pos int) (string, bool) {
	if pos < 0 || pos >= len(sql) || sql[pos] != '$' {
		return "", false
	}

	index := pos + 1
	for index < len(sql) && isDoBlockIdentifierByte(sql[index]) {
		index++
	}

	if index < len(sql) && sql[index] == '$' {
		return sql[pos : index+1], true
	}

	return "", false
}

func isDoBlockIdentifierByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func stripSQLBlockComments(sql string) string {
	if sql == "" {
		return sql
	}

	var b strings.Builder
	b.Grow(len(sql))

	inLineComment := false
	inBlockComment := false
	inSingleQuote := false
	inDoubleQuote := false

	for i := 0; i < len(sql); i++ {
		ch := sql[i]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
				b.WriteByte(ch)
			}
			continue
		}

		if inBlockComment {
			if ch == '*' && i+1 < len(sql) && sql[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if !inSingleQuote && !inDoubleQuote {
			if ch == '-' && i+1 < len(sql) && sql[i+1] == '-' {
				inLineComment = true
				i++
				continue
			}

			if ch == '/' && i+1 < len(sql) && sql[i+1] == '*' {
				inBlockComment = true
				i++
				continue
			}
		}

		if ch == '\'' && !inDoubleQuote {
			inSingleQuote = !inSingleQuote
		}
		if ch == '"' && !inSingleQuote {
			inDoubleQuote = !inDoubleQuote
		}

		b.WriteByte(ch)
	}

	return b.String()
}

func normalizeSQLWhitespace(sql string) string {
	return strings.Join(strings.Fields(sql), " ")
}

// enrichStatementWithPlugins calls all active plugins to enrich statement metadata
// This allows plugins to:
//   - Add auth pattern information (Clerk JWT v2, Supabase auth.uid())
//   - Mark critical statements (preserve auth functions, RLS policies)
//   - Add plugin-specific metadata for validation and consolidation
func enrichStatementWithPlugins(ctx context.Context, stmt *types.Statement) {
	registry := plugins.GlobalRegistry()

	// Only enrich if plugins are initialized
	if len(registry.ActivePlugins()) == 0 {
		return // No plugins active, skip enrichment
	}

	// Call enrichment on all active plugins (ordered by priority)
	if err := registry.EnrichStatement(ctx, stmt); err != nil {
		// Log error but don't fail parsing
		utils.GetDefaultLogger().WithPrefix("PARSER").Info("[parser] Plugin enrichment warning: %v", err)
	}
}

// extractExpressionDependencies recursively extracts dependencies from expression nodes
func extractExpressionDependencies(node *pg_query.Node, normalizer *ContextualNormalizer) []string {
	var deps []string

	if node == nil {
		return deps
	}

	// Handle Function Calls
	if funcCall := node.GetFuncCall(); funcCall != nil {
		funcName := extractFunctionNameWithNormalization(funcCall.Funcname, normalizer)
		if funcName != "" {
			deps = append(deps, funcName)
		}
		// Recursively check arguments
		for _, arg := range funcCall.Args {
			deps = append(deps, extractExpressionDependencies(arg, normalizer)...)
		}
	}

	// Handle Boolean Expressions (AND, OR, NOT)
	if boolExpr := node.GetBoolExpr(); boolExpr != nil {
		for _, arg := range boolExpr.Args {
			deps = append(deps, extractExpressionDependencies(arg, normalizer)...)
		}
	}

	// Handle Operator Expressions (A_Expr) e.g. a = b
	if aExpr := node.GetAExpr(); aExpr != nil {
		if aExpr.Lexpr != nil {
			deps = append(deps, extractExpressionDependencies(aExpr.Lexpr, normalizer)...)
		}
		if aExpr.Rexpr != nil {
			deps = append(deps, extractExpressionDependencies(aExpr.Rexpr, normalizer)...)
		}
	}

	// Handle Null Test (IS NULL)
	if nullTest := node.GetNullTest(); nullTest != nil {
		if nullTest.Arg != nil {
			deps = append(deps, extractExpressionDependencies(nullTest.Arg, normalizer)...)
		}
	}

	// Handle SubLinks (e.g. EXISTS (SELECT ...))
	if subLink := node.GetSubLink(); subLink != nil {
		if subLink.Subselect != nil {
			deps = append(deps, extractQueryDependencies(subLink.Subselect, normalizer)...)
		}
	}

	// Handle Type Casts
	if typeCast := node.GetTypeCast(); typeCast != nil {
		if typeCast.Arg != nil {
			deps = append(deps, extractExpressionDependencies(typeCast.Arg, normalizer)...)
		}
	}

	return deps
}
