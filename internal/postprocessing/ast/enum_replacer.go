package ast

import (
	"github.com/capy-base/pgsquash-engine/internal/utils"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// EnumReplacer provides AST-based ENUM type reference replacement.
// It replaces regex-based word boundary matching with accurate AST traversal.
type EnumReplacer struct {
	replacements  map[string]string // eliminated ENUM -> primary ENUM
	logger        *utils.Logger
	replacedCount int
}

// NewEnumReplacer creates a new ENUM replacer with the given replacement map.
func NewEnumReplacer(replacements map[string]string) *EnumReplacer {
	return &EnumReplacer{
		replacements:  replacements,
		logger:        utils.GetDefaultLogger().WithPrefix("AST-ENUM"),
		replacedCount: 0,
	}
}

// ReplaceEnumReferences replaces ENUM type references in the given SQL using AST traversal.
// This is much more accurate than regex as it only replaces actual type references,
// not occurrences in strings, comments, or identifiers.
func (er *EnumReplacer) ReplaceEnumReferences(sql string) (string, error) {
	if len(er.replacements) == 0 {
		return sql, nil // No replacements needed
	}

	// Parse SQL to AST
	parseResult, err := pg_query.Parse(sql)
	if err != nil {
		er.logger.Info("Failed to parse SQL for ENUM replacement: %v (falling back to original)", err)
		return sql, nil
	}

	// Traverse and replace type references
	er.replacedCount = 0
	er.traverseAndReplace(parseResult)

	if er.replacedCount == 0 {
		return sql, nil // No replacements made
	}

	er.logger.Info("Replaced %d ENUM type references using AST traversal", er.replacedCount)

	// Deparse back to SQL
	deparsed, err := pg_query.Deparse(parseResult)
	if err != nil {
		er.logger.Info("Failed to deparse after ENUM replacement: %v (falling back to original)", err)
		return sql, nil
	}

	return deparsed, nil
}

// traverseAndReplace recursively traverses the AST and replaces ENUM type references.
func (er *EnumReplacer) traverseAndReplace(parseResult *pg_query.ParseResult) {
	for _, stmt := range parseResult.Stmts {
		er.visitNode(stmt.Stmt)
	}
}

// visitNode visits a node and its children, replacing ENUM references.
func (er *EnumReplacer) visitNode(node *pg_query.Node) {
	if node == nil {
		return
	}

	// Check if this is a TypeName node (represents a type reference)
	if typeName := node.GetTypeName(); typeName != nil {
		er.replaceTypeNameIfNeeded(typeName)
		return
	}

	// Check for CREATE TABLE statement (has column definitions with types)
	if createStmt := node.GetCreateStmt(); createStmt != nil {
		er.visitCreateTable(createStmt)
		return
	}

	// Check for ALTER TABLE statement (can change column types)
	if alterStmt := node.GetAlterTableStmt(); alterStmt != nil {
		er.visitAlterTable(alterStmt)
		return
	}

	// Check for CREATE FUNCTION statement (has return types and parameter types)
	if funcStmt := node.GetCreateFunctionStmt(); funcStmt != nil {
		er.visitCreateFunction(funcStmt)
		return
	}

	// Recursively visit other node types
	// This is a simplified visitor - a full implementation would handle all node types
}

// replaceTypeNameIfNeeded checks if the TypeName is an eliminated ENUM and replaces it.
func (er *EnumReplacer) replaceTypeNameIfNeeded(typeName *pg_query.TypeName) {
	if typeName == nil || len(typeName.Names) == 0 {
		return
	}

	// Get the type name (last component of qualified name)
	lastNode := typeName.Names[len(typeName.Names)-1]
	str := lastNode.GetString_()
	if str == nil {
		return
	}

	currentName := str.Sval

	// Check if this is an eliminated ENUM
	if newName, shouldReplace := er.replacements[currentName]; shouldReplace {
		// Replace the type name
		str.Sval = newName
		er.replacedCount++
		er.logger.Info("Replaced ENUM type reference: %s → %s", currentName, newName)
	}
}

// visitCreateTable visits CREATE TABLE statement and replaces ENUM types in columns.
func (er *EnumReplacer) visitCreateTable(createStmt *pg_query.CreateStmt) {
	if createStmt.TableElts == nil {
		return
	}

	for _, elt := range createStmt.TableElts {
		// Check if this is a column definition
		if colDef := elt.GetColumnDef(); colDef != nil {
			if colDef.TypeName != nil {
				er.replaceTypeNameIfNeeded(colDef.TypeName)
			}
		}
	}
}

// visitAlterTable visits ALTER TABLE statement and replaces ENUM types.
func (er *EnumReplacer) visitAlterTable(alterStmt *pg_query.AlterTableStmt) {
	if alterStmt.Cmds == nil {
		return
	}

	for _, cmd := range alterStmt.Cmds {
		alterCmd := cmd.GetAlterTableCmd()
		if alterCmd == nil {
			continue
		}

		// Check for ALTER COLUMN ... TYPE commands
		if alterCmd.Def != nil {
			if colDef := alterCmd.Def.GetColumnDef(); colDef != nil {
				if colDef.TypeName != nil {
					er.replaceTypeNameIfNeeded(colDef.TypeName)
				}
			}
		}
	}
}

// visitCreateFunction visits CREATE FUNCTION statement and replaces ENUM types.
func (er *EnumReplacer) visitCreateFunction(funcStmt *pg_query.CreateFunctionStmt) {
	// Replace return type if it's an ENUM
	if funcStmt.ReturnType != nil {
		er.replaceTypeNameIfNeeded(funcStmt.ReturnType)
	}

	// Replace parameter types if they're ENUMs
	if funcStmt.Parameters != nil {
		for _, param := range funcStmt.Parameters {
			if funcParam := param.GetFunctionParameter(); funcParam != nil {
				if funcParam.ArgType != nil {
					er.replaceTypeNameIfNeeded(funcParam.ArgType)
				}
			}
		}
	}
}

// GetReplacedCount returns the number of ENUM references replaced.
func (er *EnumReplacer) GetReplacedCount() int {
	return er.replacedCount
}
