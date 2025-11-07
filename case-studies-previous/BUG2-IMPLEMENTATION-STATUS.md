# BUG #2 Implementation Status

**Date**: 2025-11-06
**Bug**: Function Schema Differences After Squashing
**Status**: PARTIALLY IMPLEMENTED - Needs Deeper Fix

## Changes Made

### 1. Removed Volatility Markers from Auth Mocks ✅
**File**: `internal/plugins/auth/compatibility.go`

**What was done**:
- Removed all STABLE volatility markers from mock auth functions (Clerk, Supabase, Auth0, Firebase)
- These test mocks were incorrectly influencing production function behavior

**Result**: Test mocks no longer leak volatility hints to production code

### 2. Disabled Automatic Volatility Marker Addition ✅
**File**: `internal/transformation/sql_transformer.go`

**What was done**:
- Disabled automatic addition of STABLE markers in `fixFunctionVolatilityMarkers()`
- Added `continue` statement to bypass the code that forced STABLE for auth functions

**Result**: Functions preserve their original volatility markers (or lack thereof)

### 3. Disabled Function Normalization ✅
**File**: `internal/postprocessing/ast/function_normalizer.go`

**What was done**:
- Completely disabled AST-based normalization in `NormalizeAll()`
- Functions are returned unchanged without any AST modifications

**Result**: Prevents volatility and security markers from being lost during AST round-tripping

## Current Problem

### Symptom
Functions are STILL being modified somewhere in the pipeline:
- LANGUAGE changes from `sql` → `plpgsql`
- Format changes from trailing (`$$ LANGUAGE SQL STABLE`) to leading (`LANGUAGE plpgsql AS $$`)
- STABLE and SECURITY DEFINER markers are lost

### Example
**Original**:
```sql
CREATE OR REPLACE FUNCTION current_clerk_org_id()
RETURNS TEXT AS $$
  SELECT (auth.jwt()->'o'->>'id')::TEXT;
$$ LANGUAGE SQL STABLE SECURITY DEFINER;
```

**After Squashing**:
```sql
CREATE OR REPLACE FUNCTION current_clerk_org_id()
RETURNS TEXT
LANGUAGE plpgsql AS $$
  SELECT (auth.jwt()->'o'->>'id')::TEXT;
$$;
-- Missing: STABLE, SECURITY DEFINER, wrong LANGUAGE
```

### Root Cause
The corruption happens BEFORE the normalizer, likely during:
1. **Consolidation phase** (`internal/squasher/engine.go`) - Where lifecycles are consolidated
2. **Deparsing phase** (`internal/squasher/deparser.go`) - Where AST is converted back to SQL
3. **Transformation pipeline** - Where functions go through multiple processing steps

## The Core Issue

Functions are being run through AST round-tripping (Parse → Deparse) which doesn't preserve:
- Exact formatting
- Trailing clause order
- Some function attributes

Even though we disabled the normalizer, the AST → SQL conversion itself is causing the corruption.

## Architectural Solution Needed

### The Right Approach (Not Yet Implemented)

**Location**: `internal/squasher/engine.go` or consolidation rules

Add bypass logic for single-version functions:

```go
// Pseudo-code for proper fix
func (e *Engine) consolidateObject(lifecycle *ObjectLifecycle) (*ConsolidationResult, error) {
    // CRITICAL: If object has only ONE history event (never modified)
    if len(lifecycle.History) == 1 {
        e.logger.Info("Object %s has single version - preserving original SQL exactly", lifecycle.Name)

        return &ConsolidationResult{
            OriginalStatements: []Statement{lifecycle.History[0].Statement},
            ConsolidatedSQL:    lifecycle.History[0].Statement.SQL, // EXACT preservation
            Optimizations:      []string{"Preserved original (no changes)"},
            RiskLevel:          RiskLevelNone,
        }, nil
    }

    // Object has multiple versions - apply normal consolidation
    // ... existing consolidation logic with AST processing
}
```

### Where to Implement

Need to find where:
1. `ConsolidationResult` is created for each lifecycle
2. `ConsolidatedSQL` field is populated
3. Functions are being deparsed from AST

**Files to investigate**:
- `internal/tracking/consolidation/*.go` - Consolidation rules
- `internal/squasher/engine.go` - Main consolidation orchestration
- `internal/squasher/deparser.go` - AST → SQL conversion

### The Fix Strategy

1. **Early Bypass**: Before any AST processing, check if object has single version
2. **Exact Preservation**: Use original SQL string directly, skip all processing
3. **Multi-Version Consolidation**: Only apply AST processing when truly needed (multiple CREATE/ALTER)

## Testing

### Current Test Results
After our changes:
- ✅ No more automatic STABLE marker additions (reduced from 28 to 1 transformation)
- ❌ Functions still differ in schema validation
- ❌ LANGUAGE still changing from sql → plpgsql

### What We Need
```
Test: case studies/nami ai app/migrations/*.sql
Expected: 0 function differences
Actual: 6 functions still differ
```

## Next Steps

1. **Find consolidation entry point** - Where `ConsolidatedSQL` is populated
2. **Add single-version bypass** - Check `len(lifecycle.History) == 1`
3. **Preserve original SQL exactly** - Skip AST round-trip for single-version objects
4. **Test with nami ai app** - Verify 0 schema differences

## Time Investment

- **Time Spent**: ~3 hours
- **Fixes Applied**: 3/4 planned steps
- **Remaining Work**: 1-2 hours to find and implement the consolidation bypass

## Summary

We've successfully disabled the automatic modifications (volatility markers, normalizer), but discovered a deeper issue: the AST round-trip itself is corrupting functions. The solution is to bypass consolidation entirely for single-version objects and preserve their original SQL exactly.

The architectural solution is sound, we just need to find the right place in the codebase to implement the early bypass logic.
