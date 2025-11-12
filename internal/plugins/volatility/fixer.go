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
    "regexp"
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
    pattern  *regexp.Regexp
}

// NewVolatilityFixer creates a new volatility fixer with a function registry
func NewVolatilityFixer(registry *FunctionRegistry) *VolatilityFixer {
    // Pattern: CREATE [OR REPLACE] FUNCTION [schema.]name(...) RETURNS type
    //          [LANGUAGE plpgsql] [SECURITY DEFINER]
    //          AS $$...
    //
    // We need to inject volatility marker before AS
    pattern := regexp.MustCompile(
        `(?ims)(CREATE\s+(?:OR\s+REPLACE\s+)?FUNCTION\s+(?:[a-z_][a-z0-9_]*\.)?([a-z_][a-z0-9_]*)\s*\([^)]*\)\s*RETURNS\s+(?:TABLE\s*\([^)]+\)|SETOF\s+[^\s]+|[^\s]+))((?:\s+(?:LANGUAGE\s+[a-z]+|SECURITY\s+DEFINER|SET\s+[^\s]+\s*=\s*[^\s]+))*?)(\s+AS\s+(?:\$\$|\$[a-z0-9_]*\$|\'))`,
    )

    return &VolatilityFixer{
        registry: registry,
        pattern:  pattern,
    }
}

// Fix adds volatility markers to registered functions in the SQL
// Returns the modified SQL with volatility markers added
func (vf *VolatilityFixer) Fix(sql string) (string, error) {
    matches := vf.pattern.FindAllStringSubmatchIndex(sql, -1)
    if len(matches) == 0 {
        return sql, nil // No functions found
    }

    transformedSQL := sql
    offset := 0

    for _, match := range matches {
        if len(match) < 10 {
            continue
        }

        // Extract function name (capture group 2)
        funcNameStart := match[4] + offset
        funcNameEnd := match[5] + offset
        funcName := transformedSQL[funcNameStart:funcNameEnd]

        // Check if this function needs a volatility marker
        desiredVolatility, isRegistered := vf.registry.GetVolatility(funcName)
        if !isRegistered {
            continue // Not a registered function, skip
        }

        // Extract parts from regex capture groups
        // Group 1: CREATE FUNCTION ... RETURNS type
        // Group 2: function name (nested in Group 1)
        // Group 3: modifiers (LANGUAGE/SECURITY DEFINER/SET)
        // Group 4: AS $$
        beforeModifiers := transformedSQL[match[2]+offset : match[3]+offset] // Group 1
        modifiers := transformedSQL[match[6]+offset : match[7]+offset]       // Group 3
        asKeyword := transformedSQL[match[8]+offset : match[9]+offset]       // Group 4

        // Check if volatility marker already exists
        if hasVolatilityMarker(modifiers) || hasVolatilityMarker(beforeModifiers) {
            continue
        }

        // Insert volatility marker before AS
        newModifiers := modifiers + " " + string(desiredVolatility)
        replacement := beforeModifiers + newModifiers + asKeyword

        transformedSQL = transformedSQL[:match[0]+offset] + replacement + transformedSQL[match[9]+offset:]
        offset += len(replacement) - (match[9] - match[0])
    }

    return transformedSQL, nil
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
