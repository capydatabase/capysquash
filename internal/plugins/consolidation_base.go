package plugins

import (
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/capysquash/pgsquash-engine/internal/utils"
)

// BaseConsolidator provides common consolidation logic for plugins
type BaseConsolidator struct {
	name string
}

// NewBaseConsolidator creates a new base consolidator
func NewBaseConsolidator(name string) *BaseConsolidator {
	return &BaseConsolidator{
		name: name,
	}
}

// PolicyConsolidation provides common policy consolidation helpers
type PolicyConsolidator struct {
	*BaseConsolidator
}

// NewPolicyConsolidator creates a new policy consolidator
func NewPolicyConsolidator(name string) *PolicyConsolidator {
	return &PolicyConsolidator{
		BaseConsolidator: NewBaseConsolidator(name),
	}
}

// AllSameTargetTable checks if all policy statements target the same table
func (pc *PolicyConsolidator) AllSameTargetTable(statements []*types.Statement) bool {
	if len(statements) < 2 {
		return false
	}

	firstTable := utils.ExtractPolicyTargetTable(statements[0].SQL)
	if firstTable == "" {
		return false
	}

	for _, stmt := range statements[1:] {
		table := utils.ExtractPolicyTargetTable(stmt.SQL)
		if table != firstTable {
			return false
		}
	}

	return true
}

// AllSameObjectName checks if all statements have the same object name
func (pc *PolicyConsolidator) AllSameObjectName(statements []*types.Statement) bool {
	if len(statements) < 2 {
		return false
	}

	firstName := statements[0].ObjectName
	for _, stmt := range statements[1:] {
		if stmt.ObjectName != firstName {
			return false
		}
	}

	return true
}

// AllSameObjectType checks if all statements are of the same type
func (pc *PolicyConsolidator) AllSameObjectType(statements []*types.Statement, objectType types.ObjectType) bool {
	for _, stmt := range statements {
		if stmt.ObjectType != objectType {
			return false
		}
	}
	return true
}

// ExtractPolicyClauses extracts USING and WITH CHECK clauses from policies
func (pc *PolicyConsolidator) ExtractPolicyClauses(statements []*types.Statement) ([]string, []string) {
	var usingClauses []string
	var withCheckClauses []string

	for _, stmt := range statements {
		sqlUpper := strings.ToUpper(stmt.SQL)

		// Extract USING clause
		if strings.Contains(sqlUpper, "USING (") {
			usingStart := strings.Index(sqlUpper, "USING (")
			usingClause := utils.ExtractBalancedParentheses(stmt.SQL[usingStart:])
			usingClauses = append(usingClauses, usingClause)
		}

		// Extract WITH CHECK clause
		if strings.Contains(sqlUpper, "WITH CHECK (") {
			checkStart := strings.Index(sqlUpper, "WITH CHECK (")
			checkClause := utils.ExtractBalancedParentheses(stmt.SQL[checkStart:])
			withCheckClauses = append(withCheckClauses, checkClause)
		}
	}

	return usingClauses, withCheckClauses
}

// AllClausesIdentical checks if all clauses in a list are identical
func (pc *PolicyConsolidator) AllClausesIdentical(clauses []string) bool {
	if len(clauses) == 0 {
		return true
	}

	first := strings.TrimSpace(clauses[0])
	for _, clause := range clauses[1:] {
		if strings.TrimSpace(clause) != first {
			return false
		}
	}

	return true
}

// HaveSamePolicyLogic checks if policies have the same security logic
func (pc *PolicyConsolidator) HaveSamePolicyLogic(statements []*types.Statement) bool {
	usingClauses, withCheckClauses := pc.ExtractPolicyClauses(statements)

	// All USING clauses must be identical
	if len(usingClauses) > 0 && !pc.AllClausesIdentical(usingClauses) {
		return false
	}

	// All WITH CHECK clauses must be identical
	if len(withCheckClauses) > 0 && !pc.AllClausesIdentical(withCheckClauses) {
		return false
	}

	return true
}

// FunctionConsolidator provides common function consolidation helpers
type FunctionConsolidator struct {
	*BaseConsolidator
}

// NewFunctionConsolidator creates a new function consolidator
func NewFunctionConsolidator(name string) *FunctionConsolidator {
	return &FunctionConsolidator{
		BaseConsolidator: NewBaseConsolidator(name),
	}
}

// AllSameFunctionSignature checks if all functions have the same signature
func (fc *FunctionConsolidator) AllSameFunctionSignature(statements []*types.Statement) bool {
	if len(statements) < 2 {
		return false
	}

	// Extract function name and parameters for comparison
	firstName := utils.ExtractFunctionName(statements[0].SQL)
	if firstName == "" {
		return false
	}

	for _, stmt := range statements[1:] {
		name := utils.ExtractFunctionName(stmt.SQL)
		if name != firstName {
			return false
		}
	}

	return true
}

// TableConsolidator provides common table consolidation helpers
type TableConsolidator struct {
	*BaseConsolidator
}

// NewTableConsolidator creates a new table consolidator
func NewTableConsolidator(name string) *TableConsolidator {
	return &TableConsolidator{
		BaseConsolidator: NewBaseConsolidator(name),
	}
}

// HasCreateOperation checks if any statement is a CREATE operation
func (tc *TableConsolidator) HasCreateOperation(statements []*types.Statement) bool {
	for _, stmt := range statements {
		if stmt.Operation == types.OpCreate {
			return true
		}
	}
	return false
}

// AllSameTable checks if all statements target the same table
func (tc *TableConsolidator) AllSameTable(statements []*types.Statement) bool {
	if len(statements) < 2 {
		return false
	}

	firstTable := statements[0].ObjectName
	for _, stmt := range statements[1:] {
		if stmt.ObjectName != firstTable {
			return false
		}
	}

	return true
}

// ContainsKeyword checks if any statement contains a specific keyword
func (tc *TableConsolidator) ContainsKeyword(statements []*types.Statement, keyword string) bool {
	upperKeyword := strings.ToUpper(keyword)
	for _, stmt := range statements {
		if strings.Contains(strings.ToUpper(stmt.SQL), upperKeyword) {
			return true
		}
	}
	return false
}

// FilterByOperation filters statements by operation type
func (tc *TableConsolidator) FilterByOperation(statements []*types.Statement, op types.Operation) []*types.Statement {
	filtered := make([]*types.Statement, 0, len(statements))
	for _, stmt := range statements {
		if stmt.Operation == op {
			filtered = append(filtered, stmt)
		}
	}
	return filtered
}

// ConservativeMerge returns the first statement (conservative approach)
func (tc *TableConsolidator) ConservativeMerge(statements []*types.Statement) *types.Statement {
	if len(statements) == 0 {
		return nil
	}
	return statements[0]
}
