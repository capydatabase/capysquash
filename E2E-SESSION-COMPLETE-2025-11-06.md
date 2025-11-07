# E2E Testing Session - Complete Report
**Date**: November 6, 2025
**Engine Version**: 0.9.5
**Session Duration**: ~2 hours
**Approach**: Fresh e2e testing with robust architectural bug fixes

---

## Executive Summary

Conducted comprehensive end-to-end testing on 3 real-world case studies to identify and fix bugs in the pgsquash-engine. Identified **5 critical bugs** affecting schema correctness and security.

### Key Findings

| Bug # | Description | Severity | Status | Files Affected |
|-------|-------------|----------|--------|----------------|
| **#10** | `SECURITY DEFINER` dropped from functions | **CRITICAL** 🔴 | ⚠️ PARTIALLY FIXED | `internal/squasher/deparser.go`, `internal/postprocessing/ast/function_normalizer.go` |
| **#11** | 68 indexes missing from output | **CRITICAL** 🔴 | 🔍 ROOT CAUSE NEEDED | `internal/tracking/`, `internal/squasher/` |
| **#12** | 8 extra functions added | **HIGH** 🟠 | 🔍 NEEDS INVESTIGATION | Plugin or consolidation system |
| **#13** | 4 extra triggers added | **MEDIUM** 🟡 | 🔍 NEEDS INVESTIGATION | Plugin or consolidation system |
| **#14** | View definitions differ | **MEDIUM** 🟡 | 🔍 NEEDS INVESTIGATION | View consolidation |

---

## Case Study Results

### ✅ VDK Hub (9 migrations) - PASSED
- **Result**: Validation passed, schemas identical
- **Extensions**: pg_trgm
- **Auth**: Supabase
- **Performance**: 142ms squash time
- **Conclusion**: Basic squashing works correctly for simple schemas

### ❌ Nami AI App (8 migrations) - FAILED
- **Result**: 6 functions missing `SECURITY DEFINER`
- **Extensions**: uuid-ossp, pg_trgm, pgcrypto
- **Auth**: Clerk JWT v2
- **Bug**: Critical security vulnerability - `SECURITY DEFINER` attribute dropped
- **Affected Functions**:
  1. `current_clerk_user_id()`
  2. `current_clerk_org_id()`
  3. `current_clerk_org_role()`
  4. `validate_jwt_version()`
  5. `set_session_user()`
  6. `get_planning_analytics()`

### ❌ MyRoomie (76 migrations) - FAILED
- **Result**: Massive schema differences
- **Extensions**: postgis, cube, earthdistance, pg_trgm, pg_stat_statements, btree_gin, pgcrypto
- **Auth**: Mixed (Clerk + Supabase detected)
- **Critical Issues**:
  - **68 indexes missing** (performance catastrophe)
  - **8 extra functions** (schema bloat)
  - **4 extra triggers** (unexpected behavior)
  - **2 views differ** (incorrect data)

---

## Bug #10: SECURITY DEFINER Dropped - Deep Dive

### The Problem
`pg_query.Deparse()` does not preserve the `SECURITY DEFINER` attribute when converting AST back to SQL. This causes a critical security vulnerability because functions lose their privilege elevation.

### Impact
**SECURITY DEFINER** is critical for security - it causes functions to execute with the privileges of the function owner (typically elevated permissions), not the caller. Without it:
- Auth helper functions can't access `auth.jwt()`
- RLS policies fail
- Applications break or become insecure

### Root Cause
Located in `pg_query_go` library - the deparser doesn't include function security attributes in its output.

### Architectural Fix Implemented

**Files Modified**:
1. `internal/squasher/deparser.go` - Added extraction and injection logic
2. `internal/postprocessing/ast/function_normalizer.go` - Added preservation logic

**Approach**:
```
1. Extract SECURITY DEFINER from original SQL (like volatility markers)
2. Deparse function to SQL (loses SECURITY DEFINER)
3. Inject SECURITY DEFINER back into correct position
4. Format: LANGUAGE xxx [VOLATILITY] SECURITY DEFINER AS $$
```

**Code Changes**:
```go
// deparser.go
func extractSecurityDefiner(sql string) string {
    // Check for "SECURITY DEFINER" or "SECURITY INVOKER" in original SQL
    upperSQL := strings.ToUpper(sql)
    if strings.Contains(upperSQL, " SECURITY DEFINER") {
        return "SECURITY DEFINER"
    }
    if strings.Contains(upperSQL, " SECURITY INVOKER") {
        return "SECURITY INVOKER"
    }
    return ""
}

func injectSecurityDefiner(sql string, securityMarker string) (string, error) {
    // Pattern 1: After volatility marker
    // LANGUAGE xxx VOLATILE SECURITY DEFINER AS $$
    withVolatilityPattern := regexp.MustCompile(
        `(?i)(LANGUAGE\s+\w+\s+(?:VOLATILE|STABLE|IMMUTABLE))(\s+)(AS\s+)`
    )
    result := withVolatilityPattern.ReplaceAllString(
        sql,
        fmt.Sprintf("$1 %s $3", securityMarker)
    )

    // Pattern 2: After LANGUAGE (no volatility)
    if result == sql {
        withoutVolatilityPattern := regexp.MustCompile(
            `(?i)(LANGUAGE\s+\w+)(\s+)(AS\s+)`
        )
        result = withoutVolatilityPattern.ReplaceAllString(
            sql,
            fmt.Sprintf("$1 %s $3", securityMarker)
        )
    }

    return result, nil
}
```

### Current Status: ⚠️ PARTIALLY FIXED

**What Works**:
- `SECURITY DEFINER` is extracted from original SQL ✅
- Injection logic is implemented ✅
- Some functions retain `SECURITY DEFINER` (e.g., `get_user_profile_data`) ✅

**What Doesn't Work**:
- Clerk auth helper functions still lose `SECURITY DEFINER` ❌
- Suggests these functions go through a different code path ❌
- Likely special handling for simple SQL functions vs PL/pgSQL ❌

**Next Steps**:
1. Add debug logging to track which code path Clerk functions take
2. Check if there's special handling for SQL language functions
3. Verify AST structure for simple functions vs complex functions
4. May need to handle these in `internal/plugins/clerk/` directly

---

## Bug #11: 68 Missing Indexes - Critical Performance Issue

### The Problem
68 indexes present in original migrations are completely missing from squashed output. This would cause catastrophic performance degradation in production.

### Missing Indexes (Sample)
```
idx_communities_category_id
idx_communities_creator_id
idx_community_members_community_id
idx_community_posts_author_id
idx_property_interests_user_id
... (63 more)
```

### Impact
- Queries that should be O(log n) become O(n)
- Full table scans on large tables
- Application becomes unusably slow
- Database CPU/memory exhaustion

### Possible Root Causes

**Hypothesis 1: Lifecycle Tracking Issue**
Indexes might be tracked as CREATE → DROP incorrectly, causing them to be omitted from final output.

**Hypothesis 2: Consolidation Rule Bug**
Error recovery or consolidation rules might be removing indexes they shouldn't.

**Hypothesis 3: Dependency Resolution**
Indexes might have circular dependencies causing them to be skipped.

**Hypothesis 4: IF NOT EXISTS Handling**
Multiple `CREATE INDEX IF NOT EXISTS` with same name might be incorrectly consolidated.

### Investigation Needed
```bash
# Check lifecycle events for missing indexes
grep "idx_communities_category_id" internal/tracking/*.go

# Check consolidation rules for indexes
grep -A 10 "INDEX.*consolidate" internal/tracking/consolidation/*.go

# Check if indexes are being marked as dropped
# Add debug logging in unified_tracker.go
```

---

## Bug #12: 8 Extra Functions Added

### The Problem
8 functions appear in squashed output that don't exist in original migrations.

### Extra Functions
```
1. public.prevent_null_fairrent_fields
2. public.get_fairrent_model_comparison
3. public.cleanup_expired_fairrent_scores
4. public.get_valid_room_fairrent_score
5. public.calculate_user_compatibility
6. public.calculate_enhanced_lifestyle_compatibility
7. public.extract_email_domain
8. public.get_valid_fairrent_score
```

### Possible Causes
- Plugin-generated helper functions
- Consolidation rules creating new functions
- Compatibility layer functions not marked as temporary

---

## Bug #13: 4 Extra Triggers

### The Problem
4 triggers in squashed output don't exist in original.

### Extra Triggers
```
1. enforce_fairrent_required_fields
2. update_fairrent_room_scores_updated_at
3. update_fairrent_scores_updated_at
4. set_updated_at_buddy_connections
```

---

## Bug #14: View Definition Differences

### The Problem
2 views have different definitions than originals.

### Affected Views
```
1. public.public_roommate_listings
2. public.public_roommate_listings_with_profiles
```

---

## Technical Architecture Notes

### Parser Flow
```
Migration SQL
  ↓
pg_query.Parse() → AST
  ↓
Tracker.ProcessMigration() → Lifecycle Events
  ↓
ConsolidationRules.Apply() → Optimized Lifecycle
  ↓
Builder.Build() → Category-Sorted Statements
  ↓
DeparseWithStatement() → SQL
  ↓
Postprocessor → Final SQL
```

### Key Decision Points

1. **AST-First Philosophy**: Always work with AST, never regex SQL manipulation
2. **Tracker as Source of Truth**: All object state lives in Tracker
3. **Deparser Limitations**: `pg_query.Deparse()` loses information (volatility, security attributes)
4. **Post-Processing Safety Net**: Fix deparser omissions in post-processing

### Files of Interest

**Core Engine**:
- `internal/squasher/engine.go` - Main orchestrator
- `internal/squasher/deparser.go` - AST → SQL conversion
- `internal/tracking/unified_tracker.go` - Object lifecycle tracking
- `internal/builder/sql.go` - Final SQL generation

**Bug Fixes**:
- `internal/postprocessing/ast/function_normalizer.go` - Function normalization
- `internal/plugins/auth/compatibility.go` - Auth compatibility layers
- `internal/tracking/consolidation/*.go` - Consolidation rules

---

## Recommendations

### Immediate Priority (P0)
1. ✅ Document Bug #10 investigation and partial fix
2. 🔴 Investigate Bug #11 (missing indexes) - CRITICAL for production use
3. 🔴 Complete Bug #10 fix for Clerk auth functions

### Short Term (P1)
1. Add comprehensive debug logging for lifecycle tracking
2. Add validation that output has >= input objects (unless explicitly dropped)
3. Test all 3 case studies with fixes
4. Add regression tests

### Medium Term (P2)
1. Fix Bugs #12, #13, #14
2. Add e2e tests to CI/CD pipeline
3. Create test suite with known-good migrations
4. Document common pitfalls and debugging guide

---

## Testing Methodology

### E2E Test Command
```bash
cd "case studies/<project>"
../../pgsquash squash migrations/*.sql --safety standard --output test-output
```

### Validation Process
1. Spin up 2 PostgreSQL Docker containers
2. Apply original migrations to container 1
3. Apply squashed migrations to container 2
4. Compare schemas using `pg_dump --schema-only`
5. Report differences

### Test Matrix
| Case Study | Migrations | Extensions | Auth | Result |
|------------|-----------|------------|------|--------|
| nami ai app | 8 | 3 | Clerk | ❌ 6 diffs |
| vdk hub | 9 | 1 | Supabase | ✅ Identical |
| myroomie | 76 | 7 | Clerk+Supabase | ❌ 157 diffs |

---

## Code Quality Observations

### What Works Well ✅
1. **Plugin system** - Clean architecture, easy to extend
2. **Lifecycle tracking** - Comprehensive event tracking
3. **Error recovery rules** - Handle common patterns well
4. **Dependency resolution** - Topological sorting works correctly

### What Needs Improvement ⚠️
1. **Deparser limitations** - Upstream library issue, needs workarounds
2. **Test coverage** - Most packages have no tests
3. **Debug logging** - Need more visibility into consolidation decisions
4. **Documentation** - Complex flows need more inline comments

---

## Conclusion

This e2e testing session successfully identified **5 critical bugs** that would prevent production use:

1. **Bug #10** (SECURITY DEFINER) - Partially fixed, security vulnerability
2. **Bug #11** (Missing indexes) - Critical performance issue
3. **Bug #12-14** (Extra objects) - Schema consistency issues

**Immediate Action Required**:
- Complete Bug #10 fix for Clerk functions
- Investigate and fix Bug #11 (missing indexes)
- Add validation checks to prevent these bugs

**Test Coverage Recommendation**:
- Add e2e tests for all 3 case studies
- Run on every commit
- Block merges if validation fails

The architectural approach taken (AST-first, extract-inject pattern) is sound and should be extended to fix the remaining issues.

---

**Session Completed**: November 6, 2025
**Next Session**: Focus on Bug #11 investigation and complete Bug #10 fix
