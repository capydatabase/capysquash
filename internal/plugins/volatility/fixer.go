// Package volatility provides shared function volatility marker detection and fixing.
// This package centralizes volatility marker logic to eliminate duplication across plugins.
//
// PostgreSQL Function Volatility:
//   - IMMUTABLE: Function always returns same result for same inputs (pure function)
//   - STABLE: Function returns same result within a transaction (reads database state)
//   - VOLATILE: Function can have side effects or return different results (default)
//
// Auth functions MUST be marked STABLE because they:
//   - Read session state (JWT claims)
//   - Are used in RLS policies and index predicates
//   - Return consistent values within a transaction
package volatility

import (
	"strings"
)

// VolatilityType represents PostgreSQL function volatility levels
type VolatilityType string

const (
	Immutable VolatilityType = "IMMUTABLE"
	Stable    VolatilityType = "STABLE"
	Volatile  VolatilityType = "VOLATILE"
)

// FunctionRegistry tracks functions that need volatility markers
type FunctionRegistry struct {
	// Map of function name (lowercase) to desired volatility
	functions map[string]VolatilityType
}

// NewFunctionRegistry creates a new function registry
func NewFunctionRegistry() *FunctionRegistry {
	return &FunctionRegistry{
		functions: make(map[string]VolatilityType),
	}
}

// Register adds a function to the registry with its desired volatility
func (fr *FunctionRegistry) Register(functionName string, volatility VolatilityType) {
	fr.functions[strings.ToLower(functionName)] = volatility
}

// RegisterMultiple adds multiple functions with the same volatility
func (fr *FunctionRegistry) RegisterMultiple(functionNames []string, volatility VolatilityType) {
	for _, name := range functionNames {
		fr.Register(name, volatility)
	}
}

// GetVolatility returns the desired volatility for a function, or empty if not registered
func (fr *FunctionRegistry) GetVolatility(functionName string) (VolatilityType, bool) {
	volatility, exists := fr.functions[strings.ToLower(functionName)]
	return volatility, exists
}

// IsRegistered checks if a function is in the registry
func (fr *FunctionRegistry) IsRegistered(functionName string) bool {
	_, exists := fr.functions[strings.ToLower(functionName)]
	return exists
}

// VolatilityFixer adds volatility markers to PostgreSQL functions
type VolatilityFixer struct {
	registry *FunctionRegistry
}

// NewVolatilityFixer creates a new volatility fixer with a function registry
func NewVolatilityFixer(registry *FunctionRegistry) *VolatilityFixer {
	return &VolatilityFixer{
		registry: registry,
	}
}

// Fix adds volatility markers to registered functions in the SQL
// Returns the modified SQL with volatility markers added
func (vf *VolatilityFixer) Fix(sql string) (string, error) {
	if strings.TrimSpace(sql) == "" {
		return sql, nil
	}

	// Prefer AST path for precision.
	astFixed, err := NewASTVolatilityFixer(vf.registry).Fix(sql)
	if err == nil {
		return astFixed, nil
	}

	// Fallback path for partially malformed SQL that cannot be parsed as an AST.
	// This preserves best-effort behavior without regex.
	return vf.fixByLineScanning(sql), nil
}

func (vf *VolatilityFixer) fixByLineScanning(sql string) string {
	if vf.registry == nil {
		return sql
	}

	fixed := sql
	for functionName, volatility := range vf.registry.functions {
		if functionName == "" {
			continue
		}

		if functionHasVolatilityMarker(fixed, functionName) {
			continue
		}

		rewritten, addErr := addVolatilityToFunction(fixed, functionName, volatility)
		if addErr != nil {
			continue
		}

		fixed = rewritten
	}

	return fixed
}

func functionHasVolatilityMarker(sql string, functionName string) bool {
	if strings.TrimSpace(sql) == "" || strings.TrimSpace(functionName) == "" {
		return false
	}

	lines := strings.SplitSeq(sql, "\n")
	for line := range lines {
		if !containsFunctionName(line, functionName) {
			continue
		}

		if hasVolatilityMarker(line) {
			return true
		}
	}

	return false
}

// hasVolatilityMarker checks if a string contains a volatility marker
func hasVolatilityMarker(s string) bool {
	upper := strings.ToUpper(s)
	return strings.Contains(upper, " IMMUTABLE") ||
		strings.Contains(upper, " STABLE") ||
		strings.Contains(upper, " VOLATILE")
}

// CreateClerkRegistry creates a function registry for Clerk auth functions
func CreateClerkRegistry() *FunctionRegistry {
	registry := NewFunctionRegistry()

	// All Clerk auth functions should be STABLE (read session state)
	clerkFunctions := []string{
		"clerk_user_id",
		"clerk_is_admin",
		"clerk_organization_id",
		"current_user_id",
		"current_organization_id",
		"current_organization_role",
		"current_organization_name",
	}

	registry.RegisterMultiple(clerkFunctions, Stable)
	return registry
}

// CreateSupabaseRegistry creates a function registry for Supabase auth functions
func CreateSupabaseRegistry() *FunctionRegistry {
	registry := NewFunctionRegistry()

	// All Supabase auth functions should be STABLE (read session state)
	supabaseFunctions := []string{
		"uid",  // auth.uid()
		"jwt",  // auth.jwt()
		"role", // auth.role()
	}

	registry.RegisterMultiple(supabaseFunctions, Stable)
	return registry
}

// CreateDrizzleRegistry creates a function registry for Drizzle-generated functions
func CreateDrizzleRegistry() *FunctionRegistry {
	registry := NewFunctionRegistry()
	// Drizzle rarely generates custom functions
	// If it does for generated columns, they should be IMMUTABLE (pure computation)
	// For trigger functions, they should be VOLATILE (modifies data)
	// This is handled with heuristics in the Drizzle plugin
	return registry
}

// CreatePrismaRegistry creates a function registry for Prisma-generated functions
func CreatePrismaRegistry() *FunctionRegistry {
	registry := NewFunctionRegistry()
	// Prisma doesn't typically generate custom functions
	// If detected, they should be STABLE (safe default for ORM functions)
	return registry
}
