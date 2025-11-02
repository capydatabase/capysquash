package parser

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/errors"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/plugins"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/types"
	"github.com/CAPYSQUASH/pgsquash-engine/internal/utils"
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
		_ = errorHandler.HandleParseError(err, parseCtx) // Log the error for tracking
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
			_ = errorHandler.HandleParseError(parseErr, parseCtx)
		}
	}

	migration := &types.Migration{
		Filename:    filename,
		Statements:  make([]types.Statement, 0),
		Size:        int64(len(content)),
		ParseErrors: make([]string, 0),
	}

	for i, stmtStr := range stmts {
		if strings.TrimSpace(stmtStr) == "" {
			continue
		}

		// BUGFIX Bug #4: Extract ALTER TABLE statements from DO blocks BEFORE parsing
		// DO blocks often wrap ALTER statements in IF NOT EXISTS checks:
		//   DO $$ BEGIN IF NOT EXISTS (...) THEN ALTER TABLE foo ADD COLUMN bar; END IF; END $$;
		// We need to extract these ALTER statements and parse them separately
		// so they can be properly tracked and consolidated
		if strings.Contains(strings.ToUpper(stmtStr), "DO") && strings.Contains(strings.ToUpper(stmtStr), "$$") {
			// Check if this DO block contains ALTER TABLE statements
			alterStmts := extractAlterStatementsFromDoBlock(stmtStr)

			if len(alterStmts) > 0 {
				// Log extraction for debugging
				utils.GetDefaultLogger().WithPrefix("PARSER").Info(
					"Extracted %d ALTER TABLE statement(s) from DO block in %s",
					len(alterStmts), filename)

				// Parse and add each extracted ALTER statement
				for _, alterSQL := range alterStmts {
					alterStmt, err := parseStatementWithNormalizationAndContext(alterSQL, i, normalizer, errorHandler, filename)
					if err != nil {
						if !errorHandler.ShouldContinue() {
							break
						}
						continue
					}

					// Assign comments and category
					alterStmt.Comments = getRelevantComments(comments, i)
					alterStmt.Category = categorizeStatement(*alterStmt)

					migration.Statements = append(migration.Statements, *alterStmt)
				}

				// Skip the original DO block - we've extracted what we need
				// The DO block itself doesn't need to be in the output
				continue
			}
		}

		// Parse the statement normally
		stmt, err := parseStatementWithNormalizationAndContext(stmtStr, i, normalizer, errorHandler, filename)
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

		// CRITICAL: If we have parse errors but NO statements were successfully parsed,
		// and we have content, this indicates complete parsing failure (e.g., missing semicolons)
		if len(migration.Statements) == 0 && len(migration.ParseErrors) > 0 {
			// Return error to indicate catastrophic failure
			return migration, errors.New(errors.ErrorCodeSyntaxError, errors.CategoryParsing, fmt.Sprintf("fatal: all statements failed to parse in %s: %d parse errors, 0 statements recovered", filename, len(migration.ParseErrors)), nil)
		}

		// Return migration with statements that did parse successfully
		// Callers can check migration.ParseErrors to see what failed
		return migration, nil
	}

	// migration is never nil here since it was initialized above
	return migration, nil
}

// parseStatementWithNormalizationAndContext parses a statement with context and error handling
func parseStatementWithNormalizationAndContext(sql string, line int, normalizer *ContextualNormalizer, errorHandler *ErrorHandler, filename string) (*types.Statement, error) {
	defer errorHandler.Recovery(filename, line)

	parsed, err := pg_query.Parse(sql)
	if err != nil {
		parseCtx := errorHandler.CreateContext(filename, line, nil)
		parseCtx.StatementText = sql
		_ = errorHandler.HandleParseError(err, parseCtx) // Log the error for tracking
		return nil, err
	}

	if len(parsed.Stmts) == 0 {
		parseCtx := errorHandler.CreateContext(filename, line, nil)
		parseCtx.StatementText = sql
		err := errors.New(errors.ErrorCodeSyntaxError, errors.CategoryParsing, "no statements found", nil)
		_ = errorHandler.HandleValidationError(err.Error(), parseCtx) // Log the validation error
		return nil, err
	}

	// Use actual statement location from pg_query instead of loop index
	statementLine := line // Default to passed line (loop index) as fallback
	if len(parsed.Stmts) > 0 && parsed.Stmts[0].StmtLocation > 0 {
		statementLine = int(parsed.Stmts[0].StmtLocation)
	}

	stmt := &types.Statement{
		SQL:       strings.TrimSpace(sql),
		ParseTree: parsed,
		Line:      statementLine,
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
	// Use PostgreSQL 15 as default version (can be overridden by config later)
	analyzer := NewStatementAnalyzer("15")
	analyzer.AnalyzeStatement(stmt)
	analyzer.AnalyzePragmas(stmt)

	return stmt, nil
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

	case *pg_query.Node_DropStmt:
		stmt.Operation = types.OpDrop
		if len(node.DropStmt.Objects) > 0 {
			stmt.ObjectType = mapObjectType(node.DropStmt.RemoveType)
			if list := node.DropStmt.Objects[0]; list != nil {
				stmt.ObjectName = extractObjectNameWithNormalization(list, normalizer)
			}
		}

	case *pg_query.Node_IndexStmt:
		stmt.ObjectType = types.TypeIndex
		stmt.Operation = types.OpCreate
		stmt.ObjectName = normalizer.NormalizeIdentifier(node.IndexStmt.Idxname)
		if node.IndexStmt.Relation != nil {
			stmt.Dependencies = []string{getTableNameWithNormalization(node.IndexStmt.Relation, normalizer)}
		}

	case *pg_query.Node_CreateFunctionStmt:
		stmt.ObjectType = types.TypeFunction
		stmt.Operation = types.OpCreate
		if node.CreateFunctionStmt.Funcname != nil {
			stmt.ObjectName = extractFunctionNameWithNormalization(node.CreateFunctionStmt.Funcname, normalizer)
		}

	case *pg_query.Node_CreateTrigStmt:
		stmt.ObjectType = types.TypeTrigger
		stmt.Operation = types.OpCreate
		stmt.ObjectName = normalizer.NormalizeIdentifier(node.CreateTrigStmt.Trigname)
		if node.CreateTrigStmt.Relation != nil {
			stmt.Dependencies = []string{getTableNameWithNormalization(node.CreateTrigStmt.Relation, normalizer)}
		}

	case *pg_query.Node_ViewStmt:
		stmt.ObjectType = types.TypeView
		stmt.Operation = types.OpCreate
		if node.ViewStmt.View != nil {
			stmt.ObjectName = getTableNameWithNormalization(node.ViewStmt.View, normalizer)
		}
		// CRITICAL FIX: Extract table dependencies from the view's SELECT query
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
		stmt.ObjectName = normalizer.NormalizeIdentifier(node.CreatePolicyStmt.PolicyName)
		if node.CreatePolicyStmt.Table != nil {
			stmt.Dependencies = []string{getTableNameWithNormalization(node.CreatePolicyStmt.Table, normalizer)}
		}
		// Check for RESTRICTIVE policies
		if strings.Contains(stmt.SQL, "AS RESTRICTIVE") {
			stmt.Comments = append(stmt.Comments, "-- RESTRICTIVE policy")
		}

	case *pg_query.Node_InsertStmt:
		stmt.Operation = types.OpInsert
		stmt.IsDataOp = true
		stmt.ObjectName = getTableNameWithNormalization(node.InsertStmt.Relation, normalizer)
		// Check for ON CONFLICT clause
		if node.InsertStmt.OnConflictClause != nil {
			stmt.Comments = append(stmt.Comments, "-- Contains ON CONFLICT clause")
		}

	case *pg_query.Node_UpdateStmt:
		stmt.Operation = types.OpUpdate
		stmt.IsDataOp = true
		stmt.ObjectName = getTableNameWithNormalization(node.UpdateStmt.Relation, normalizer)

	case *pg_query.Node_DeleteStmt:
		stmt.Operation = types.OpDelete
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

	case *pg_query.Node_CreateExtensionStmt:
		stmt.ObjectType = types.TypeExtension
		stmt.Operation = types.OpCreate
		stmt.ObjectName = normalizer.NormalizeIdentifier(node.CreateExtensionStmt.Extname)

	case *pg_query.Node_CommentStmt:
		// CRITICAL FIX (Bug #5 revised): COMMENT statements should always be TypeComment
		// The object being commented on is tracked via Dependencies, not ObjectType
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
			// Regular DO block without types
			stmt.ObjectType = types.TypeDoBlock
			stmt.Operation = types.Operation("DO_BLOCK")
			stmt.ObjectName = "anonymous_block"
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
	}
}

func categorizeStatement(stmt types.Statement) types.Category {
	if stmt.IsDataOp {
		return types.CategoryData
	}

	// CRITICAL FIX: Check Operation first before ObjectType
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
	case types.TypeConstraint:
		return types.CategoryConstraints
	case types.TypeIndex:
		return types.CategoryIndexes
	case types.TypeFunction, types.TypeDoBlock:
		return types.CategoryFunctions
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

	if rangeVar.Schemaname != "" {
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
		return strings.Join(parts, ".")
	}
	return ""
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
	// CRITICAL FIX (Bug #5): Map all PostgreSQL object types that can have COMMENTs
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

	// Type assert to pg_query.ParseResult
	parseResult, ok := stmt.ParseTree.(*pg_query.ParseResult)
	if !ok || parseResult == nil || len(parseResult.Stmts) == 0 {
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
	keywordManager := NewVersionedKeywordManager(15) // PostgreSQL 15
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
	multiCommentPattern := regexp.MustCompile(`/\*[\s\S]*?\*/`)
	cleaned = multiCommentPattern.ReplaceAllString(cleaned, "")

	// Normalize whitespace but preserve line breaks for SQL
	cleaned = strings.TrimSpace(cleaned)

	return cleaned
}

// extractNestedTypesFromDoBlock extracts CREATE TYPE statements from DO blocks
func extractNestedTypesFromDoBlock(doBlockSQL string) []string {
	var nestedTypes []string

	// Pattern to match CREATE TYPE ... AS ENUM statements within DO blocks
	enumPattern := regexp.MustCompile(`CREATE\s+TYPE\s+([a-zA-Z_][a-zA-Z0-9_]*)\s+AS\s+ENUM`)
	matches := enumPattern.FindAllStringSubmatch(doBlockSQL, -1)

	for _, match := range matches {
		if len(match) > 1 {
			nestedTypes = append(nestedTypes, match[1]) // Type name
		}
	}

	return nestedTypes
}

// extractAlterStatementsFromDoBlock extracts ALTER TABLE statements from DO blocks
// This is critical for handling migrations that wrap ALTER statements in IF NOT EXISTS checks
// Example:
//   DO $$ BEGIN
//     IF NOT EXISTS (...) THEN
//       ALTER TABLE foo ADD COLUMN bar TEXT;
//     END IF;
//   END $$;
// Returns: ["ALTER TABLE foo ADD COLUMN bar TEXT;"]
func extractAlterStatementsFromDoBlock(doBlockSQL string) []string {
	var alterStatements []string

	// Strategy: Extract complete ALTER TABLE statements from DO blocks
	// Must handle:
	// 1. Simple columns: ALTER TABLE foo ADD COLUMN bar TEXT;
	// 2. Complex columns: ALTER TABLE foo ADD COLUMN bar TEXT GENERATED ALWAYS AS (...) STORED;
	// 3. Constraints: ALTER TABLE foo ADD CONSTRAINT ...;
	// 4. RLS: ALTER TABLE foo ENABLE ROW LEVEL SECURITY;

	// Pattern 1: Extract ALTER TABLE ... ADD COLUMN (handles multi-line GENERATED columns)
	// Captures everything from ALTER TABLE to the semicolon
	addColumnPattern := regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(\S+)\s+ADD\s+COLUMN\s+([^;]+);`)
	matches := addColumnPattern.FindAllStringSubmatch(doBlockSQL, -1)

	for _, match := range matches {
		if len(match) >= 3 {
			tableName := strings.TrimSpace(match[1])
			columnDef := strings.TrimSpace(match[2])

			// Normalize whitespace (collapse newlines and multiple spaces)
			columnDef = regexp.MustCompile(`\s+`).ReplaceAllString(columnDef, " ")
			columnDef = strings.TrimSpace(columnDef)

			if columnDef != "" {
				stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s;", tableName, columnDef)
				alterStatements = append(alterStatements, stmt)
			}
		}
	}

	// Pattern 2: Extract ALTER TABLE ... ADD CONSTRAINT
	addConstraintPattern := regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(\S+)\s+ADD\s+CONSTRAINT\s+([^;]+);`)
	constraintMatches := addConstraintPattern.FindAllStringSubmatch(doBlockSQL, -1)

	for _, match := range constraintMatches {
		if len(match) >= 3 {
			tableName := strings.TrimSpace(match[1])
			constraintDef := strings.TrimSpace(match[2])

			// Normalize whitespace
			constraintDef = regexp.MustCompile(`\s+`).ReplaceAllString(constraintDef, " ")
			constraintDef = strings.TrimSpace(constraintDef)

			if constraintDef != "" {
				stmt := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s;", tableName, constraintDef)
				alterStatements = append(alterStatements, stmt)
			}
		}
	}

	// Pattern 3: Extract other ALTER TABLE operations (ENABLE/DISABLE RLS, etc.)
	otherAlterPattern := regexp.MustCompile(`(?is)ALTER\s+TABLE\s+(\S+)\s+((?:ENABLE|DISABLE)\s+ROW\s+LEVEL\s+SECURITY)\s*;`)
	otherMatches := otherAlterPattern.FindAllStringSubmatch(doBlockSQL, -1)

	for _, match := range otherMatches {
		if len(match) >= 3 {
			tableName := strings.TrimSpace(match[1])
			alterAction := strings.TrimSpace(match[2])

			stmt := fmt.Sprintf("ALTER TABLE %s %s;", tableName, alterAction)
			alterStatements = append(alterStatements, stmt)
		}
	}

	return alterStatements
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
