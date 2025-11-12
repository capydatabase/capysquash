# E2E Testing Bug Report - November 8, 2025
## pgsquash-engine v0.9.7

---

## Executive Summary

Conducted end-to-end testing across 3 production case studies (93 migration files total). Identified and fixed **1 critical bug** (extension dependency ordering). Discovered **1 additional bug** requiring attention (DROP TRIGGER syntax).

### Test Results Summary

| Project | Files | Squash | Extension Fix | Validation | Status |
|---------|-------|--------|---------------|------------|--------|
| **myroomie** | 76 → 1 | ✅ Success | ✅ Fixed | ❌ Bug #2 | PARTIAL |
| **nami ai app** | 8 → 1 | ✅ Success | N/A | ❌ Schema diff | PARTIAL |
| **vdk hub** | 9 → 1 | ✅ Success | N/A | ✅ Passed | SUCCESS |

**Overall**: 1 of 3 validations passing (33% success rate)

---

## Bug #1: Extension Dependency Ordering (CRITICAL) ✅ FIXED

### Status
**✅ FIXED** - Architectural solution implemented

### Problem
Extensions with dependencies were created in wrong order, causing validation failures:
- `earthdistance` extension requires `cube` to be installed first
- Squashed SQL created `earthdistance` BEFORE `cube`
- Error: `pq: required extension "cube" is not installed`

### Root Cause
File: `internal/squasher/unified_dependency_resolver.go:584-586`

The extension dependency analyzer only extracted what extensions PROVIDE but not what they DEPEND ON:

```go
case types.CategoryExtensions:
    info.RequiredFirst = true
    info.Provides = append(info.Provides, udr.extractExtensionProvisions(sql)...)
    // MISSING: Dependency extraction!
```

This prevented the topological sort from ordering extensions correctly.

### Context
The postprocessor that had `FixExtensionOrder()` function was completely disabled (engine.go:1720-1724) due to a previous bug where it corrupted function SQL. This meant extension ordering was never applied.

### Solution Implemented
Added extension dependency extraction to the unified dependency resolver:

```go
case types.CategoryExtensions:
    info.RequiredFirst = true
    // CRITICAL: Extract extension dependencies (e.g., earthdistance depends on cube)
    info.Dependencies = append(info.Dependencies, udr.extractExtensionDependencies(sql)...)
    info.Provides = append(info.Provides, udr.extractExtensionProvisions(sql)...)
```

**File Changed**: `internal/squasher/unified_dependency_resolver.go:587`

### Verification
**Before Fix** (myroomie squashed output):
```sql
CREATE EXTENSION IF NOT EXISTS "earthdistance";  -- Line 40 ❌ WRONG ORDER
CREATE EXTENSION IF NOT EXISTS "cube";           -- Line 44 ❌ WRONG ORDER
```

**After Fix** (myroomie squashed-FIXED output):
```sql
CREATE EXTENSION IF NOT EXISTS "cube";           -- Line 36 ✅ CORRECT
CREATE EXTENSION IF NOT EXISTS "earthdistance";  -- Line 42 ✅ CORRECT
```

### Impact
- **Severity**: CRITICAL - Blocked all migrations using earthdistance extension
- **Affected Projects**: myroomie (1 of 3 case studies)
- **Fix Type**: Architectural - Proper dependency resolution in consolidation phase
- **Future Prevention**: Extension dependencies now part of standard dependency graph

---

## Bug #2: Invalid DROP TRIGGER Syntax (HIGH) ❌ OPEN

### Status
**❌ OPEN** - Requires investigation and fix

### Problem
Generated SQL contains invalid DROP TRIGGER syntax with schema qualification:
```sql
DROP TRIGGER IF EXISTS fairrent_scores.prevent_null_fairrent_fields_trigger;
```

PostgreSQL error:
```
pq: syntax error at or near "."
```

### Root Cause
**Investigation needed**. PostgreSQL DROP TRIGGER syntax does not support schema-qualified trigger names.

Correct syntax should be:
```sql
DROP TRIGGER IF EXISTS prevent_null_fairrent_fields_trigger ON fairrent_scores;
```

### Likely Location
- Parser/deparser may be incorrectly formatting DROP TRIGGER statements
- Or consolidation logic may be incorrectly combining schema + trigger name

### Impact
- **Severity**: HIGH - Prevents myroomie validation from passing
- **Affected Projects**: myroomie (detected via validation)
- **Workaround**: None - requires code fix
- **Error Location**: Statement 413 in squashed output

### Recommended Fix
1. Search for DROP TRIGGER generation code
2. Ensure trigger name is not schema-qualified
3. Add `ON table_name` clause if needed
4. Test with myroomie case study

---

## Bug #3: Nami Schema Differences (MEDIUM) ⚠️ INVESTIGATION NEEDED

### Status
**⚠️ INVESTIGATION NEEDED** - Validation reported schema differences

### Problem
Validation failed with message: `❌ Schema differences detected!`

No detailed diff was shown in the log output (truncated).

### Impact
- **Severity**: MEDIUM - Blocks nami ai app validation
- **Affected Projects**: nami ai app (1 of 3 case studies)
- **Requires**: Detailed validation log analysis to identify specific differences

### Next Steps
1. Run validation with verbose mode
2. Capture full schema diff output
3. Identify which objects/tables differ
4. Determine if issue is consolidation logic or validation logic

---

## Successful Validation: vdk hub ✅

### Status
**✅ PASSED** - Complete validation success

### Details
- **Files**: 9 → 1 (88.9% reduction)
- **Lines**: 2,527 → 2,066 (18.2% reduction)
- **Processing Time**: 183ms
- **Extensions**: pg_trgm (1 extension, no dependencies)
- **DDL Cycles**: 6 detected (3 MEDIUM SIMPLE, 3 MEDIUM VERSIONING)
- **Validation**: **✅ Schemas are identical**

### Success Factors
1. Single extension (pg_trgm) with no dependencies
2. No complex DROP TRIGGER statements
3. Clean Supabase authentication integration
4. Proper dependency ordering for all objects

This validates that the core consolidation engine works correctly when:
- Extension dependencies are simple
- SQL syntax is standard
- Object lifecycles are clean

---

## Consolidation Metrics

### Overall Performance
- **Total Files Processed**: 93 migration files
- **Total Squashed Files**: 3 baseline files
- **File Reduction**: 96.8% (93 → 3 files)
- **Total Original Lines**: ~32,821 lines
- **Total Squashed Lines**: ~16,992 lines
- **Line Reduction**: 48.2%
- **Processing Time**: < 3 seconds total
- **Success Rate**: 100% squashing, 33% validation

### Per-Project Breakdown

#### myroomie (Large Supabase Project)
- **Original**: 76 files, ~27,934 lines
- **Squashed**: 1 file, ~12,935 lines (53.7% reduction)
- **Extensions**: 7 (postgis, cube, earthdistance, pg_trgm, pg_stat_statements, btree_gin, pgcrypto)
- **Objects Tracked**: 182
- **Processing Time**: ~2.5 seconds
- **Validation**: ❌ Failed (Bug #2 - DROP TRIGGER syntax)

#### nami ai app (Medium Clerk Project)
- **Original**: 8 files, 2,360 lines
- **Squashed**: 1 file, 1,991 lines (15.6% reduction)
- **Extensions**: 3 (uuid-ossp, pg_trgm, pgcrypto)
- **Objects Tracked**: 182
- **Processing Time**: 138ms
- **Validation**: ❌ Failed (Bug #3 - Schema differences)

#### vdk hub (Medium Supabase Project)
- **Original**: 9 files, 2,527 lines
- **Squashed**: 1 file, 2,066 lines (18.2% reduction)
- **Extensions**: 1 (pg_trgm)
- **Objects Tracked**: 350
- **Processing Time**: 183ms
- **Validation**: ✅ PASSED

---

## Technical Details

### Environment
- **Tool Version**: pgsquash 0.9.7
- **Safety Level**: Standard
- **Validation Mode**: TWO_DATABASES (Docker-based)
- **Container Timeout**: 180 seconds
- **PostgreSQL Version**: 17

### Features Tested
- ✅ AST-based SQL parsing (pg_query_go)
- ✅ Object lifecycle tracking
- ✅ Dependency resolution and topological sorting
- ✅ Plugin auto-detection (Supabase, Clerk)
- ✅ Extension detection and ordering (after fix)
- ✅ DDL cycle detection
- ✅ Docker-based schema validation
- ⚠️ DROP TRIGGER consolidation (bug found)
- ⚠️ Schema difference detection (needs investigation)

### Plugins Detected
- **Supabase**: myroomie, vdk hub (auto-detected)
- **Clerk**: nami ai app (auto-detected, JWT v2)

---

## Recommendations

### Immediate Actions (Pre-YC Application)
1. ✅ **DONE**: Fix Bug #1 (Extension dependency ordering) - COMPLETED
2. ❌ **TODO**: Fix Bug #2 (DROP TRIGGER syntax) - HIGH PRIORITY
3. ⚠️ **TODO**: Investigate Bug #3 (Nami schema differences) - MEDIUM PRIORITY

### Short-Term Improvements
1. Add comprehensive test coverage for extension dependencies
2. Improve DROP TRIGGER parsing/generation logic
3. Enhanced validation diff output for easier debugging
4. Add regression tests for all 3 case studies

### Long-Term Enhancements
1. Re-enable safe postprocessing (extension ordering) without function corruption
2. Improve validation error messages with detailed diffs
3. Add automated fix suggestions for common SQL errors
4. Expand test matrix to more safety levels (conservative, aggressive)

---

## YC Application Readiness

### Strengths to Highlight
✅ **Excellent consolidation metrics**: 96.8% file reduction, 48.2% line reduction
✅ **Fast processing**: < 3 seconds for 93 files
✅ **Production validation**: Tested on 3 real production codebases
✅ **Multi-framework support**: Supabase + Clerk auto-detected
✅ **Architectural fixes**: AST-based, not regex-based solutions
✅ **One successful validation**: vdk hub proves core engine works

### Known Limitations (Transparency)
❌ **2 bugs blocking full validation**: DROP TRIGGER syntax, schema differences
❌ **Validation success rate**: 33% (1 of 3 projects)
⚠️ **Work in progress**: Still refining consolidation rules

### Positioning Strategy
- Lead with consolidation metrics (96.8% file reduction)
- Emphasize speed (< 3 seconds processing)
- Show production validation (3 real codebases)
- Be transparent about 2 open bugs
- Highlight architectural approach (AST-based)
- Demonstrate plugin system flexibility
- Prove validation works (vdk hub success)

---

## Files Modified

### Bug #1 Fix
**File**: `internal/squasher/unified_dependency_resolver.go`
**Line**: 587
**Change**: Added extension dependency extraction
**Commit Message**: Fix Bug #1: Add extension dependency analysis for correct ordering

---

## Next Steps

1. **Commit Bug #1 fix** with proper git message
2. **Investigate Bug #2** (DROP TRIGGER syntax)
3. **Investigate Bug #3** (Nami schema differences)
4. **Re-run all case studies** after Bug #2 fix
5. **Generate final YC metrics** with updated results
6. **Document lessons learned** for future development

---

**Report Generated**: 2025-11-08
**Author**: Claude Code
**Session**: E2E Testing & YC Metrics Generation
**Status**: Bug #1 Fixed, 2 Bugs Open, 1 Validation Passing
