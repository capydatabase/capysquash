package consolidation

import (
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/utils"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/tracking"
)

// ExternalDependencyFilterRule filters out dependencies on external schemas
type ExternalDependencyFilterRule struct {
	ExternalSchemas map[string]bool
	ExternalTables  map[string]bool
}

// NewExternalDependencyFilterRule creates a new rule with default external dependencies
func NewExternalDependencyFilterRule() *ExternalDependencyFilterRule {
	return &ExternalDependencyFilterRule{
		ExternalSchemas: map[string]bool{
			"storage":    true,
			"auth":       true,
			"realtime":   true,
			"supabase":   true,
			"extensions": true,
		},
		ExternalTables: map[string]bool{
			"storage.objects":     true,
			"storage.buckets":     true,
			"auth.users":          true,
			"auth.sessions":       true,
			"realtime.messages":   true,
			"supabase.migrations": true,
		},
	}
}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *ExternalDependencyFilterRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	// Check if this object has external dependencies that should be filtered
	for _, dep := range lifecycle.Dependencies {
		if r.isExternalDependency(dep.DependsOn.Name) {
			return true
		}
	}
	return false
}

// Apply applies the consolidation rule to the given lifecycle
func (r *ExternalDependencyFilterRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]any{"rule": "ExternalDependencyRule"})
	}

	// Filter out external dependencies to reduce warnings
	var filteredDeps []tracking.ObjectDependency
	externalDepsFiltered := 0

	for _, dep := range lifecycle.Dependencies {
		if !r.isExternalDependency(dep.DependsOn.Name) {
			filteredDeps = append(filteredDeps, dep)
		} else {
			externalDepsFiltered++
		}
	}

	// Update lifecycle dependencies
	lifecycle.Dependencies = filteredDeps

	// IMPORTANT: This rule only filters dependencies, it doesn't consolidate SQL.
	// Return nil to let other rules handle SQL consolidation.
	utils.GetDefaultLogger().WithPrefix("EXTERNAL-DEPENDENCY").Info("ExternalDependencyFilterRule filtered %d deps for %s, returning nil to allow other rules", externalDepsFiltered, lifecycle.Name)
	return nil, nil
}

// Risk returns the risk level for this rule
func (r *ExternalDependencyFilterRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow
}

func (r *ExternalDependencyFilterRule) isExternalDependency(depName string) bool {
	// Check if it's a known external table
	if r.ExternalTables[depName] {
		return true
	}

	// Check if it belongs to an external schema
	for schema := range r.ExternalSchemas {
		if strings.HasPrefix(depName, schema+".") {
			return true
		}
	}

	// Removed broad keyword matching (objects, buckets, avatars, etc.)
	// because it was too aggressive and filtered out valid internal tables
	// like "product_images" or "user_documents".
	// The schema-based check (storage.*, auth.*) is sufficient for Supabase.

	return false
}
