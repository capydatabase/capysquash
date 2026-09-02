package clerk

import (
	"github.com/capy-base/pgsquash-engine/internal/plugins"
	"github.com/capy-base/pgsquash-engine/internal/types"
)

// GetConsolidationRules returns Clerk-specific consolidation rules
func (cp *ClerkPlugin) GetConsolidationRules() []plugins.ConsolidationRule {
	return []plugins.ConsolidationRule{
		{
			Name:        "clerk_jwt_v2_policies",
			Priority:    100,
			ObjectType:  types.TypePolicy,
			AuthPattern: types.AuthPatternType(AuthPatternClerkJWTV2),
			Conflicts:   []string{"general_policy_consolidation"},
			CanMerge:    cp.canMergeJWTV2Policies,
			Merge:       cp.mergeJWTV2Policies,
		},
		{
			Name:        "clerk_auth_functions",
			Priority:    95,
			ObjectType:  types.TypeFunction,
			AuthPattern: types.AuthPatternType(AuthPatternClerk),
			Conflicts:   []string{"function_deduplication"},
			CanMerge:    cp.canMergeAuthFunctions,
			Merge:       cp.mergeAuthFunctions,
		},
	}
}

// canMergeJWTV2Policies checks if Clerk JWT v2 RLS policies can be merged
// Conservative approach: Only merge if policies are identical
func (cp *ClerkPlugin) canMergeJWTV2Policies(statements []*types.Statement) bool {
	consolidator := plugins.NewPolicyConsolidator("clerk")

	// Check basic requirements
	if len(statements) < 2 {
		return false
	}

	// All statements must be policies with same object name
	if !consolidator.AllSameObjectType(statements, types.TypePolicy) {
		return false
	}

	if !consolidator.AllSameObjectName(statements) {
		return false
	}

	// Check if policy definitions are semantically identical
	return consolidator.HaveSamePolicyLogic(statements)
}

// mergeJWTV2Policies merges identical Clerk JWT v2 policies
func (cp *ClerkPlugin) mergeJWTV2Policies(statements []*types.Statement) *types.Statement {
	if len(statements) == 0 {
		return nil
	}

	// Use the first statement as base, but ensure it's CREATE POLICY
	baseStmt := statements[0]
	if baseStmt.Operation != types.OpCreate {
		// Find a CREATE statement if available
		for _, stmt := range statements {
			if stmt.Operation == types.OpCreate {
				baseStmt = stmt
				break
			}
		}
	}

	// Return the consolidated policy (keep first/CREATE version)
	return baseStmt
}

// canMergeAuthFunctions checks if Clerk auth helper functions can be merged
// Auth functions should NEVER be merged - they are critical
func (cp *ClerkPlugin) canMergeAuthFunctions(statements []*types.Statement) bool {
	// Never merge auth functions - each definition might have subtle differences
	// that affect RLS policy behavior
	return false
}

// mergeAuthFunctions merges Clerk auth functions (should never be called)
func (cp *ClerkPlugin) mergeAuthFunctions(statements []*types.Statement) *types.Statement {
	// This should never be called since canMergeAuthFunctions returns false
	// But if it is, return the last definition (most recent)
	if len(statements) == 0 {
		return nil
	}
	return statements[len(statements)-1]
}
