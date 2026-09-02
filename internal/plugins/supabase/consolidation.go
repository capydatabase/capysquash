package supabase

import (
	"github.com/capysquash/pgsquash-engine/internal/plugins"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// GetConsolidationRules returns Supabase-specific consolidation rules
func (sp *SupabasePlugin) GetConsolidationRules() []plugins.ConsolidationRule {
	return []plugins.ConsolidationRule{
		{
			Name:        "supabase_rls_policies",
			Priority:    85,
			ObjectType:  types.TypePolicy,
			AuthPattern: types.AuthPatternType(AuthPatternSupabaseRLS),
			Conflicts:   []string{"general_policy_consolidation"},
			CanMerge:    sp.canMergeRLSPolicies,
			Merge:       sp.mergeRLSPolicies,
		},
		{
			Name:        "supabase_storage_policies",
			Priority:    80,
			ObjectType:  types.TypePolicy,
			AuthPattern: types.AuthPatternType(AuthPatternSupabaseStorage),
			Conflicts:   []string{"general_policy_consolidation"},
			CanMerge:    sp.canMergeStoragePolicies,
			Merge:       sp.mergeStoragePolicies,
		},
		{
			Name:        "supabase_auth_functions",
			Priority:    90,
			ObjectType:  types.TypeFunction,
			AuthPattern: types.AuthPatternType(AuthPatternSupabase),
			Conflicts:   []string{"function_deduplication"},
			CanMerge:    sp.canMergeAuthFunctions,
			Merge:       sp.mergeAuthFunctions,
		},
	}
}

// canMergeRLSPolicies checks if Supabase RLS policies can be merged
// Conservative approach: Only merge if policies are for same table and operation
func (sp *SupabasePlugin) canMergeRLSPolicies(statements []*types.Statement) bool {
	consolidator := plugins.NewPolicyConsolidator("supabase")

	if len(statements) < 2 {
		return false
	}

	// All statements must be policies
	if !consolidator.AllSameObjectType(statements, types.TypePolicy) {
		return false
	}

	// Only merge policies on the same table
	if !consolidator.AllSameTargetTable(statements) {
		return false
	}

	return false // Be conservative - don't merge RLS policies automatically
}

// mergeRLSPolicies merges Supabase RLS policies (conservative - keep all)
func (sp *SupabasePlugin) mergeRLSPolicies(statements []*types.Statement) *types.Statement {
	if len(statements) == 0 {
		return nil
	}

	// Return first statement (conservative - don't actually merge)
	return statements[0]
}

// canMergeStoragePolicies checks if Supabase Storage policies can be merged
func (sp *SupabasePlugin) canMergeStoragePolicies(statements []*types.Statement) bool {
	// Never merge storage policies - they're critical for access control
	return false
}

// mergeStoragePolicies merges Supabase Storage policies (should never be called)
func (sp *SupabasePlugin) mergeStoragePolicies(statements []*types.Statement) *types.Statement {
	if len(statements) == 0 {
		return nil
	}
	return statements[0]
}

// canMergeAuthFunctions checks if Supabase auth functions can be merged
func (sp *SupabasePlugin) canMergeAuthFunctions(statements []*types.Statement) bool {
	// Never merge auth functions - they are critical for RLS
	return false
}

// mergeAuthFunctions merges Supabase auth functions (should never be called)
func (sp *SupabasePlugin) mergeAuthFunctions(statements []*types.Statement) *types.Statement {
	if len(statements) == 0 {
		return nil
	}
	// Return last definition (most recent)
	return statements[len(statements)-1]
}
