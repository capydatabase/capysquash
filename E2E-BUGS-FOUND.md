# E2E Testing - Bugs Found

**Date**: 2025-11-06
**Testing Session**: Comprehensive E2E testing across 3 case studies

## Test Summary

| Case Study | Migrations | Test Type | Result | Bugs Found |
|------------|-----------|-----------|---------|------------|
| myroomie | 17 files | Standard | ❌ FAIL | BUG #1 |
| myroomie | 17 files | Conservative | ❌ FAIL | BUG #3 |
| nami ai app | 8 files | Standard | ❌ FAIL | BUG #2 |
| nami ai app | 8 files | Aggressive | ❌ FAIL | BUG #2 |
| vdk hub | 9 files | Standard | ✅ PASS | None |

## Bugs Identified

### BUG #1: Supabase Storage Schema Not Created

**Severity**: HIGH
**Test Case**: myroomie - standard mode
**Category**: Dependency Tracking / Schema Management

**Symptom**:
```
❌ Validation failed: pq: schema "storage" does not exist
Statement:
CREATE POLICY avatar_upload_own ON storage.objects FOR INSERT ...
```

**Root Cause**:
The engine creates RLS policies for Supabase storage (`storage.objects`) but doesn't ensure the `storage` schema exists. The Supabase plugin detects storage-related patterns but fails to inject the schema creation.

**Impact**:
- Squashed migrations cannot be applied
- Complete migration failure on fresh database

**Related Code**:
- `internal/plugins/supabase/` - Storage detection and compatibility layer
- `internal/tracking/unified_tracker.go` - Schema dependency tracking

**Expected Behavior**:
When storage-related policies are detected, the engine should:
1. Detect `storage.objects` references
2. Create the `storage` schema before any storage-related objects
3. Properly order schema creation in dependency graph

---

### BUG #2: Function Schema Differences After Squashing

**Severity**: HIGH
**Test Case**: nami ai app - standard & aggressive modes
**Category**: Function Consolidation / AST Processing

**Symptom**:
```
❌ Schema differences detected:
1. Functions differs: public.current_clerk_org_role
2. Functions differs: public.current_clerk_org_id
3. Functions differs: public.get_planning_analytics
4. Functions differs: public.current_clerk_user_id
5. Functions differs: public.set_session_user
6. Functions differs: public.validate_jwt_version
```

**Root Cause**:
The volatility marker system (BUG3-FIX) is modifying function definitions in ways that change their signature or behavior. The functions are being marked with STABLE volatility when they shouldn't be, or the AST-to-SQL conversion is producing different output than the original.

**Pattern Analysis**:
- All affected functions are Clerk auth-related or have STABLE markers added
- Issue occurs in both standard and aggressive modes
- Functions work but are not byte-identical to originals

**Impact**:
- Schemas are not equivalent after squashing
- Could cause subtle runtime issues
- Breaks the guarantee that squashed migrations are drop-in replacements

**Related Code**:
- `internal/plugins/auth/compatibility.go` - BUG3-FIX auth function volatility forcing
- `internal/postprocessing/ast/function_normalizer.go` - Function AST processing
- `internal/squasher/deparser.go` - AST-to-SQL conversion

**Expected Behavior**:
Functions should be:
1. Consolidated to their latest version
2. Preserve exact signature and behavior
3. Pass byte-for-byte comparison with originals (or functionally equivalent)

---

### BUG #3: Index on Non-Existent Column

**Severity**: MEDIUM
**Test Case**: myroomie - conservative mode
**Category**: Column Lifecycle Tracking / Dependency Management

**Symptom**:
```
❌ Validation failed: pq: column "compatibility_score" does not exist
Statement:
CREATE INDEX IF NOT EXISTS idx_matches_compatibility ON matches (compatibility_score);
```

**Root Cause**:
The consolidation engine is creating an index on a column that was dropped in a later migration. The tracker doesn't properly handle the lifecycle:
1. Column `compatibility_score` created
2. Index `idx_matches_compatibility` created on that column
3. Column `compatibility_score` dropped (via ALTER TABLE DROP COLUMN)
4. Index creation is still present in consolidated output

**Impact**:
- Migration fails when applied
- Index definitions are orphaned from their columns
- Conservative mode should be extra careful about this

**Related Code**:
- `internal/tracking/consolidation/drop_create_rule.go` - DROP/CREATE cycle handling
- `internal/tracking/unified_tracker.go` - Column lifecycle tracking
- `internal/squasher/unified_dependency_resolver.go` - Index-column dependencies

**Expected Behavior**:
When a column is dropped, the engine should:
1. Detect all dependent indexes on that column
2. Remove index definitions from consolidated output
3. Track column lifecycle: CREATE → ALTER → DROP
4. Clean up orphaned index definitions

---

## Bug Priority & Impact Matrix

| Bug ID | Severity | Frequency | Fix Complexity | Priority |
|--------|----------|-----------|----------------|----------|
| BUG #1 | HIGH | Common (Supabase projects) | MEDIUM | P0 |
| BUG #2 | HIGH | Common (Clerk projects) | HIGH | P0 |
| BUG #3 | MEDIUM | Uncommon (complex schemas) | MEDIUM | P1 |

## Common Patterns

### Pattern 1: Schema Dependencies Not Tracked
- BUG #1 demonstrates missing schema creation
- Root issue: Schema-level dependencies not in tracker

### Pattern 2: Function AST Roundtrip Issues
- BUG #2 shows AST → SQL conversion problems
- Volatility markers changing function signatures

### Pattern 3: Cascade Cleanup Incomplete
- BUG #3 shows orphaned index definitions
- When column drops, dependent objects should cascade

## Next Steps

1. **Analyze Bugs**: Deep dive into root causes with code review
2. **Design Solutions**: Architect robust fixes (AST-first, not regex)
3. **Implement Fixes**: Apply architectural solutions
4. **Re-test**: Run full E2E suite again to verify fixes
5. **Regression Tests**: Add test cases for each bug to prevent recurrence

## Test Logs

All test logs are available:
- `e2e-myroomie-standard.log`
- `e2e-nami-standard.log`
- `e2e-nami-aggressive.log`
- `e2e-vdk-standard.log`
