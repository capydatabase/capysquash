# E2E Testing Bug Report - pgsquash-engine v0.9.5

**Date**: 2025-11-06
**Test Executor**: Claude Code
**Goal**: Identify and document bugs preventing production-ready migration consolidation

## Executive Summary

Tested pgsquash-engine v0.9.5 on 3 real-world projects (76, 8, and 9 migrations respectively) with various safety levels. **All 3 projects failed validation** with critical bugs that prevent drop-in replacement of original migrations.

**Critical Findings**:
- 3 distinct bug categories identified
- All bugs prevent successful schema application
- Root causes trace to AST processing and dependency tracking issues

---

## Test Environment

- **pgsquash version**: 0.9.5
- **Go version**: 1.25.4
- **PostgreSQL target**: 15+
- **Test projects**:
  1. MyRoomie (76 migrations, 1.2MB, Clerk + Supabase auth)
  2. Nami AI App (8 migrations, 104KB, Clerk auth)
  3. VDK Hub (9 migrations, 124KB, standard PostgreSQL)

---

## Bug #1: View Column Reference Not Updated After Schema Consolidation

### Severity: **CRITICAL** 🔴
### Project: MyRoomie
### Safety Level: Conservative

### Description
When consolidating migrations, view definitions are not updated to reflect column renames/changes in the final schema.

### Error Message
```
❌ Validation failed: column r.size does not exist
Statement: CREATE OR REPLACE VIEW rooms_fairrent_ready AS
  SELECT r.id, r.property_id, r.name, r.price, r.size, ...
PostgreSQL error: pq: column r.size does not exist
```

### Root Cause Analysis

**Original Schema** (migration 01_migration.sql):
```sql
CREATE TABLE IF NOT EXISTS rooms (
  size DECIMAL(10, 2) NOT NULL,
  ...
);
```

**View Definition** (migration 75_fix_security_definer_views_and_rls.sql):
```sql
CREATE VIEW rooms_fairrent_ready AS
SELECT r.size, r.price, ...
FROM rooms r
WHERE r.price > 0 AND r.size > 0;
```

**Consolidated Schema** (000_baseline.sql):
```sql
CREATE TABLE IF NOT EXISTS rooms (
  size_sqm numeric(6, 2),  -- Column renamed!
  ...
);

-- View still references old column name
CREATE OR REPLACE VIEW rooms_fairrent_ready AS
SELECT r.size, ...  -- ❌ Column doesn't exist!
FROM rooms r;
```

### Impact
- **Validation**: FAILED
- **Production Risk**: SEVERE - Schema application will fail completely
- **Data Loss Risk**: N/A (fails before data operations)

### Architecture Issue
The consolidated table has `size_sqm` but the view still references `size`. The consolidation process either:
1. Merged two incompatible CREATE TABLE statements and picked the wrong column name
2. Failed to update view dependencies when column names changed
3. Didn't track column-level dependencies across CREATE/ALTER cycles

### Location in Codebase
- **Affected Component**: `internal/tracking/unified_tracker.go` - Object lifecycle tracking
- **Related**: `internal/builder/sql.go` - View dependency resolution
- **Root**: `internal/squasher/engine.go` - Consolidation orchestration

---

## Bug #2: Function Language/Body Mismatch After Consolidation

### Severity: **CRITICAL** 🔴
### Project: Nami AI App
### Safety Level: Standard

### Description
Function consolidation generates syntactically invalid SQL by mixing `plpgsql` language declaration with SQL-style function bodies (bare SELECT without BEGIN/END).

### Error Message
```
❌ Validation failed: syntax error at or near "SELECT"
Statement:
CREATE OR REPLACE FUNCTION current_clerk_org_id()
RETURNS text
LANGUAGE plpgsql VOLATILE
AS $$
  SELECT (auth.jwt()->'o'->>'id')::TEXT;
$$;
PostgreSQL error: pq: syntax error at or near "SELECT"
```

### Root Cause Analysis

**Valid Syntax (Expected)** - Option 1 (SQL language):
```sql
CREATE OR REPLACE FUNCTION current_clerk_org_id()
RETURNS text
LANGUAGE sql VOLATILE  -- SQL language for simple SELECT
AS $$
  SELECT (auth.jwt()->'o'->>'id')::TEXT;
$$;
```

**Valid Syntax (Expected)** - Option 2 (plpgsql with proper structure):
```sql
CREATE OR REPLACE FUNCTION current_clerk_org_id()
RETURNS text
LANGUAGE plpgsql VOLATILE
AS $$
BEGIN
  RETURN (auth.jwt()->'o'->>'id')::TEXT;
END;
$$;
```

**Generated (Invalid)**:
```sql
CREATE OR REPLACE FUNCTION current_clerk_org_id()
RETURNS text
LANGUAGE plpgsql VOLATILE  -- ❌ plpgsql requires BEGIN/END
AS $$
  SELECT (auth.jwt()->'o'->>'id')::TEXT;  -- ❌ Bare SELECT invalid in plpgsql
$$;
```

### Impact
- **Validation**: FAILED
- **Production Risk**: SEVERE - All affected functions will fail to create
- **Affected Functions**: 25+ functions in nami-ai-app project show this pattern

### Architecture Issue
The consolidation process is:
1. Correctly adding VOLATILE markers (as shown in log: "Added VOLATILE volatility marker to function current_clerk_org_id")
2. **INCORRECTLY** keeping `LANGUAGE plpgsql` for functions with simple SQL bodies
3. Not detecting that simple SELECT statements require `LANGUAGE sql` OR proper plpgsql structure

This suggests the AST deparser or post-processing is:
- Normalizing all functions to `plpgsql`
- Not analyzing function body complexity
- Not adjusting body structure to match language

### Location in Codebase
- **Affected Component**: `internal/postprocessing/postprocessor_ast.go` - AST-based post-processing
- **Related**: `internal/postprocessing/function_language.go` - Function language detection
- **Related**: `internal/builder/sql.go` - Function SQL generation

---

## Bug #3: Significant Schema Drift - Object Loss and Duplication

### Severity: **HIGH** 🟡
### Project: VDK Hub
### Safety Level: Standard

### Description
Validation reveals extensive schema drift between original and squashed migrations, with 226 differences including lost functions, duplicate indexes, and altered implementations.

### Validation Output Sample
```
168. Functions only in original: public.create_user_api_token
169. Functions only in original: public.generate_api_token
170. Functions only in original: public.validate_api_token
171. Functions only in squashed: public.recalculate_blueprint_stats
172. Functions only in squashed: public.get_cli_usage_summary
...
199. Functions differs: public.create_user_api_token
200. Functions differs: public.generate_api_token
201. Functions differs: public.validate_api_token
...
148. Indexes only in squashed: idx_user_command_stats_command
149. Indexes only in squashed: idx_vdk_error_logs_user_id
...
224. Views only in squashed: public.command_search_view
225. Views only in squashed: public.cli_deployment_summary
226. Views only in squashed: public.collection_contents
```

### Root Cause Analysis

**Categories of Drift**:

1. **Lost Functions** (3 instances):
   - Functions present in original migrations but missing in squashed output
   - Likely dropped during duplicate detection or consolidation

2. **New Functions** (31 instances):
   - Functions in squashed output that don't exist in original
   - Possibly from:
     - Consolidation creating helper functions
     - Plugin injections not present in original
     - Incorrect deduplication merging

3. **Modified Functions** (3 instances):
   - Same function name but different implementation
   - Consolidation may have merged multiple versions incorrectly

4. **Extra Indexes** (20+ instances):
   - Indexes created in squashed but not in original
   - Could be from:
     - Index recommendations from plugins
     - Duplicate index consolidation creating wrong names
     - Foreign key auto-index generation

5. **Extra Views** (3+ instances):
   - Views in squashed not present in original

### Impact
- **Validation**: Lists differences but continues
- **Production Risk**: HIGH - Application code may:
  - Call functions that don't exist
  - Miss functions it expects
  - Have different function behavior
  - Experience index performance differences
- **Correctness**: FAILED - Not a drop-in replacement

### Architecture Issue
The consolidation process is not maintaining semantic equivalence. Possible causes:

1. **Over-aggressive deduplication**: Removing objects that appear similar but serve different purposes
2. **Plugin interference**: Plugins adding objects without checking if they already exist
3. **Versioning issues**: Not properly tracking which version of an object is final
4. **Dependency resolution**: Creating helper objects that weren't in original

### Location in Codebase
- **Affected Component**: `internal/squasher/engine.go` - Main consolidation logic
- **Related**: `internal/tracking/unified_tracker.go` - Object lifecycle tracking
- **Related**: `internal/plugins/` - Plugin system may be over-contributing

---

## Architectural Solutions Required

### Solution 1: Enhanced View Dependency Tracking

**Problem**: Views not updated when referenced columns change

**Proposed Architecture**:

1. **Column-Level Dependency Graph**:
   ```go
   type ColumnDependency struct {
       TableName    string
       ColumnName   string
       DependentViews     []string
       DependentFunctions []string
       DependentIndexes   []string
   }
   ```

2. **View Rewriting Pipeline**:
   ```
   Parse View → Extract Column References → Check Final Schema → Rewrite SELECT
   ```

3. **Implementation Points**:
   - `internal/tracking/unified_tracker.go`: Track column-level dependencies
   - `internal/squasher/view_dependency_resolver.go` (NEW): Resolve and rewrite views
   - `internal/builder/sql.go`: Apply rewrites during SQL generation

**Test Coverage Required**:
- Column rename scenarios
- Column type change scenarios
- View chaining (view depends on another view)
- Complex view expressions

---

### Solution 2: Function Language/Body Validator

**Problem**: Function language doesn't match body structure

**Proposed Architecture**:

1. **Function Body Analyzer**:
   ```go
   type FunctionBodyType int
   const (
       SimpleSQL      FunctionBodyType = iota  // Single SELECT/INSERT/UPDATE
       ComplexSQL                              // Multiple statements
       ControlFlow                             // IF/LOOP/etc
       DynamicSQL                              // EXECUTE statements
   )

   func AnalyzeFunctionBody(ast *pg_query.FunctionAST) FunctionBodyType
   ```

2. **Language Selection Logic**:
   ```
   IF body is SimpleSQL:
       LANGUAGE sql
   ELSE IF body needs plpgsql features:
       LANGUAGE plpgsql + proper BEGIN/END structure
   ```

3. **Implementation Points**:
   - `internal/postprocessing/function_language.go`: Enhance language detection
   - `internal/postprocessing/function_normalizer.go`: Normalize body structure
   - Add validation step: "Does language match body structure?"

**Test Coverage Required**:
- Simple SQL SELECT functions
- Multi-statement functions
- Functions with control flow
- Functions with dynamic SQL
- RETURNS TABLE functions
- Set-returning functions

---

### Solution 3: Semantic Equivalence Validator

**Problem**: Squashed schema != Original schema (lost/extra objects)

**Proposed Architecture**:

1. **Object Fingerprinting**:
   ```go
   type ObjectFingerprint struct {
       Type       string
       Name       string
       Signature  string  // For functions: params + return type
       HashSQL    string  // Normalized SQL hash
       Dependencies []string
   }
   ```

2. **Three-Way Comparison**:
   ```
   Original Objects → Expected Final State
   Squashed Objects → Actual Final State
   Diff → Identify: Missing, Extra, Modified
   ```

3. **Consolidation Audit Trail**:
   ```go
   type ConsolidationDecision struct {
       Action      string  // "kept", "dropped", "merged", "created"
       ObjectType  string
       ObjectName  string
       Reason      string
       SourceFiles []string
       Confidence  float64
   }
   ```

4. **Implementation Points**:
   - `internal/squasher/semantic_validator.go` (NEW): Validate semantic equivalence
   - `internal/tracking/unified_tracker.go`: Add audit trail
   - `internal/validation/schema_diff.go`: Enhance diff logic to flag critical differences

**Test Coverage Required**:
- Function versioning scenarios
- Index creation/deletion
- View modifications
- Plugin-injected objects
- Dropped object reconstruction

---

## Recommended Immediate Actions

### Priority 1 (P0 - Blocking)
1. **Fix Bug #2** - Function language/body mismatch
   - Immediate business impact: All Clerk-integrated projects fail
   - Fix: Implement function body analyzer in `function_language.go`
   - Estimated effort: 2-3 days

2. **Fix Bug #1** - View column reference updates
   - Immediate business impact: Schema application failures
   - Fix: Implement column-level dependency tracking
   - Estimated effort: 3-4 days

### Priority 2 (P1 - High)
3. **Investigate Bug #3** - Schema drift root causes
   - Deep dive into why 226 differences exist
   - Audit consolidation decision logs
   - Check plugin contributions
   - Estimated effort: 2-3 days investigation + 5-7 days fixes

### Priority 3 (P2 - Enhancement)
4. **Add Semantic Equivalence Tests**
   - Automated testing that squashed == original in behavior
   - Not just syntax, but actual PostgreSQL execution equivalence
   - Estimated effort: 4-5 days

---

## Test Coverage Gaps

Current testing appears to lack:

1. **Integration Tests**: Real-world project end-to-end flows
2. **Semantic Equivalence Tests**: Do both schemas behave identically?
3. **View Dependency Tests**: Column renames, type changes
4. **Function Language Tests**: SQL vs plpgsql body validation
5. **Plugin Interaction Tests**: Do plugins create conflicts?

**Recommendation**: Establish test suite using the 3 case study projects as regression tests.

---

## Conclusion

pgsquash-engine v0.9.5 is **not production-ready** for drop-in migration replacement. All 3 tested projects failed validation with critical SQL errors.

**Blocker Bugs**: 2 critical (syntax errors preventing schema application)
**High-Priority Bugs**: 1 high (semantic drift, possible functionality loss)

**Estimated Time to Production-Ready**: 2-3 weeks with focused effort on:
1. AST post-processing fixes
2. Dependency tracking enhancements
3. Semantic equivalence validation
4. Comprehensive test coverage

The architectural foundation (AST-based processing, plugin system, tracker) is sound. The bugs are in **integration points** where these systems interact - specifically:
- AST deparser output → Post-processing
- Column tracking → View rewriting
- Multi-source consolidation → Final SQL generation

**Next Steps**:
1. Prioritize Bug #2 fix (highest impact)
2. Implement Bug #1 solution (critical for complex schemas)
3. Deep-dive Bug #3 investigation (understand consolidation decisions)
4. Add case studies to CI/CD as regression tests
