# Unused Code Assessment Report

**Date**: 2025-11-12
**Scope**: Complete assessment of unused methods, functions, parameters, and fields in pgsquash-engine
**Approach**: Full-featured, robust analysis - implementation over blind removal

---

## Executive Summary

This report provides a comprehensive analysis of all unused code identified by linters. Each item has been assessed for:

1. **Purpose**: What was the original intent?
2. **Current State**: Why is it unused?
3. **Integration Potential**: Should it be implemented?
4. **Recommendation**: Keep & implement, or remove with justification

---

## Category 1: Deparser Functions

### 1.1 `deparseNode` (internal/squasher/deparser.go:273)

**Purpose**: Convert a single AST Node back to SQL using pg_query.Deparse

**Current State**:
- Marked with `//nolint:unused // Utility function for future node deparsing needs`
- Currently unused in codebase
- Creates a dummy ParseResult to wrap a single node

**Analysis**:
- This is a utility function that could be useful for debugging or selective node deparsing
- However, the current codebase always works with full ParseResults
- The pattern is simple enough to inline where needed

**Recommendation**: **REMOVE**
- **Reason**: Overly specific utility that adds no real value
- **Risk**: None - function is never called
- **Alternative**: If needed in future, the pattern is trivial: create ParseResult wrapper inline

---

### 1.2 `extractFirstCompleteFunction` (internal/tracking/consolidation/function_dedup_rule.go:96)

**Purpose**: Extract the first complete function definition from concatenated SQL

**Current State**:
- Complex function (100+ lines) with detailed parsing logic
- Handles multiple SQL patterns, delimiters, LANGUAGE clauses
- **Currently unused** - replaced by simpler approach in Apply() method

**Analysis**:
- The `FunctionDeduplicationRule.Apply()` method now uses `latestCreate.SQL` directly (line 66)
- Comments indicate this was intentional to avoid corruption:
  - LANGUAGE placement issues
  - LANGUAGE type (sql vs plpgsql)
  - Volatility markers
  - Security markers
- The complex extraction was abandoned in favor of preserving original SQL

**Recommendation**: **REMOVE**
- **Reason**: Superseded by better approach (preserve original SQL)
- **Risk**: None - better pattern already implemented
- **Evidence**: Comments at line 59-65 explain why extraction was abandoned

---

## Category 2: Consolidation Functions

### 2.1 `consolidateAuth0Policies` (internal/squasher/modern_patterns.go:466)

**Purpose**: Consolidate Auth0 authentication policies

**Current State**:
- Marked with `//nolint:unused // Reserved for future Auth0 pattern consolidation`
- 30+ lines of implementation
- Groups by table and policy type
- Handles JWT-based policies

**Analysis**:
- This is part of a third-party integration pattern (like Clerk, Supabase)
- Auth0 is not currently supported as a plugin
- The implementation exists but plugin registration is missing
- **Potential value**: Auth0 is a popular auth provider

**Integration Requirements**:
1. Create `internal/plugins/auth0/` package
2. Implement `Plugin` interface (12 lifecycle hooks)
3. Register plugin in plugin registry with priority ~95 (same as Clerk)
4. Add detection patterns for Auth0
5. Wire up `consolidateAuth0Policies` into plugin's consolidation rules
6. Add configuration options

**Recommendation**: **KEEP & IMPLEMENT (Phase 3-4 of Roadmap)**
- **Reason**: Aligns with roadmap goal of additional auth plugins
- **Priority**: Medium (Phase 3-4: "Auth0, NextAuth, Firebase")
- **File**: Matches CLAUDE.md roadmap exactly
- **Action**: Create issue to track implementation, keep function for now

---

### 2.2 `consolidateIndexesForTable` (internal/tracking/consolidation/multiple_create_rule.go:171)

**Purpose**: Merge indexes from all CREATE events when table has DROP→CREATE pattern

**Current State**:
- 80+ lines of implementation
- AST-based approach using IndexStmt.Relation
- Handles index deduplication and lifecycle tracking
- Well-documented problem statement (lines 159-170)

**Analysis**:
- **Critical bug fix**: Without this, indexes are lost during DROP→CREATE cycles
- The function exists and is complete
- **Should be called from `MultipleCreateRule.Apply()`** but isn't
- This is a missing integration, not unused code

**Integration Point**:
- `MultipleCreateRule.Apply()` should call this after merging CREATE statements
- Need to incorporate consolidated indexes into the final SQL

**Recommendation**: **KEEP & INTEGRATE IMMEDIATELY**
- **Reason**: This fixes a data loss bug (index loss)
- **Priority**: HIGH - should be integrated before 1.0.0
- **Risk**: Medium if not integrated (silent index loss)
- **Action**: Wire into MultipleCreateRule.Apply() method

---

## Category 3: Transformation/Normalization Functions

### 3.1 `normalizeLanguagePosition` (internal/transformation/sql_transformer.go:622)

**Purpose**: Move LANGUAGE clauses from after-body to before-AS position

**Current State**:
- Comprehensive implementation with regex patterns
- Documented problem (line 604-622): pg_query.Deparse() format inconsistency
- Comment at line 739: "IMPORTANT: Call normalizeLanguagePosition() BEFORE this function"
- **Currently unused** but referenced in comments

**Analysis**:
- This addresses a real pg_query.Deparse() limitation
- The comment suggests it should be called before adding volatility markers
- May have been bypassed by the "preserve original SQL" approach
- Could still be useful for ensuring consistent format

**Integration Opportunity**:
- Could be called in `deparseWithVolatilityPreservation()` before injecting markers
- Would ensure consistent LANGUAGE positioning

**Recommendation**: **KEEP & INTEGRATE (conditional)**
- **Reason**: Addresses real pg_query limitations
- **Priority**: Low-Medium
- **Condition**: Only if we're doing AST-based function modifications
- **Alternative**: If always preserving original SQL, can remove
- **Action**: Review function transformation pipeline, decide approach

---

### 3.2 `isAuthFunction` (internal/transformation/sql_transformer.go:883)

**Purpose**: Check if function name matches known auth function patterns

**Current State**:
- Checks Clerk patterns (8 patterns)
- Checks Supabase patterns (3 patterns)
- Returns true if function is auth-related
- **Currently unused**

**Analysis**:
- This is useful for determining volatility: auth functions should be STABLE
- The `determineVolatility()` method (line 941) doesn't call this
- Could enhance volatility detection by checking function names

**Integration Opportunity**:
- Add check in `determineVolatility()`: if `isAuthFunction(funcName)`, return "STABLE"
- Would ensure auth functions always get correct volatility

**Recommendation**: **KEEP & INTEGRATE IMMEDIATELY**
- **Reason**: Improves auth function handling
- **Priority**: HIGH - enhances correctness
- **Risk**: Low
- **Action**: Add call in determineVolatility() method before pattern matching

---

### 3.3 `preprocessMigrationSQL` (internal/validation/publication_dedup.go:70)

**Purpose**: Preprocess migration SQL before validation (deduplication, fixes)

**Current State**:
- Marked `//nolint:unused // Reserved for future SQL preprocessing feature`
- Calls `deduplicatePublicationStatements()` when enabled
- Extensible design (lines 79-82: "Future: Add other preprocessing steps")

**Analysis**:
- This is a validation-time preprocessing hook
- Not currently wired into validation pipeline
- Could be useful for handling validation-specific SQL issues
- `deduplicatePublicationStatements()` already exists and works

**Integration Point**:
- Should be called in `SchemaValidator` before applying migrations
- Add configuration option: `validation.enable_preprocessing: bool`

**Recommendation**: **KEEP & INTEGRATE (Low Priority)**
- **Reason**: Useful for validation edge cases
- **Priority**: Low - validation already works well
- **Benefit**: Cleaner validation, fewer false positives
- **Action**: Add to validation pipeline with configuration flag

---

## Category 4: Validation Functions

### 4.1 `dumpContainerSchema` (internal/validation/validator.go:2371)

**Purpose**: Dump PostgreSQL schema from Docker container

**Current State**:
- Marked `//nolint:unused // Kept for backward compatibility, use DumpAndNormalizeContainerSchema instead`
- Replaced by `DumpAndNormalizeContainerSchema`
- 15 lines of implementation (simple pg_dump wrapper)

**Analysis**:
- This is a deprecated backward compatibility shim
- Better method exists: `DumpAndNormalizeContainerSchema`
- No actual callers in codebase

**Recommendation**: **REMOVE**
- **Reason**: Deprecated, replaced by better implementation
- **Risk**: None - comment says to use new method
- **Evidence**: Explicit deprecation comment

---

### 4.2 `ensureLanguageClausesPresent` (internal/postprocessing/ast/function_normalizer.go:419)

**Purpose**: Fix pg_query deparser omissions by inserting missing LANGUAGE clauses

**Current State**:
- Comment at line 45: "REMOVED: ensureLanguageClausesPresent() - it was using AST language values which could be wrong"
- Function still exists but is not called
- Known to have issues with AST language values

**Analysis**:
- This was intentionally removed from the pipeline
- AST-based approach was deemed unreliable
- Function exists as documentation of failed approach

**Recommendation**: **REMOVE**
- **Reason**: Intentionally deprecated, known to be buggy
- **Risk**: None - better approach already in use
- **Evidence**: Comment explains why it was removed

---

### 4.3 `extractFunctionNameFromSQL` (internal/postprocessing/ast/function_normalizer.go:594)

**Purpose**: Extract function name from CREATE FUNCTION line

**Current State**:
- Simple regex-based extraction
- Part of FunctionNormalizer
- Not currently called

**Analysis**:
- Helper for `ensureLanguageClausesPresent()` which is also unused
- Part of deprecated AST-based normalization approach

**Recommendation**: **REMOVE**
- **Reason**: Supporting function for deprecated approach
- **Risk**: None
- **Evidence**: Only useful for ensureLanguageClausesPresent

---

## Category 5: Utility Functions

### 5.1 `extractColumnFromAddStatement` (internal/tracking/consolidation/create_alter_rule.go:258)

**Purpose**: Extract column definition from ALTER TABLE ADD COLUMN (single column)

**Current State**:
- Not called in codebase
- 32 lines of implementation
- Superseded by `extractMultipleAddColumnsFromAlter` (line 293)

**Analysis**:
- `extractMultipleAddColumnsFromAlter` IS being used (line 160)
- Single-column variant is redundant
- Multi-column variant handles both cases

**Recommendation**: **REMOVE**
- **Reason**: Superseded by multi-column version
- **Risk**: None - better function already in use
- **Evidence**: extractMultipleAddColumnsFromAlter handles single columns too

---

### 5.2 `cleanAlterAction` (internal/tracking/consolidation/do_block_alter_table_rule.go:180)

**Purpose**: Remove IF NOT EXISTS logic and extract ALTER action

**Current State**:
- 34 lines of implementation
- Handles ADD COLUMN, ADD CONSTRAINT, ROW LEVEL SECURITY
- Not called in codebase

**Analysis**:
- This is for extracting clean ALTER actions from DO blocks
- DO block rule may not be using this extraction method
- Could be leftover from refactoring

**Integration Opportunity**:
- Check if `DoBlockAlterTableRule` should use this
- May improve DO block consolidation

**Recommendation**: **INVESTIGATE THEN DECIDE**
- **Priority**: Low
- **Action**: Check if DoBlockAlterTableRule could benefit
- **Fallback**: Remove if not needed

---

---

## Category 6: Unused Parameters

### 6.1 `lifecycle` parameter (various)

**Context**: Need to identify specific function

**Recommendation**: Pending function identification

---

### 6.2 `ctx` parameter (various)

**Context**: Context parameters are often required by interface even if unused

**Analysis**:
- Go best practice: keep context parameters even if unused
- Enables future tracing, cancellation, deadline support

**Recommendation**: **KEEP ALL**
- **Reason**: Required for future observability, idiomatic Go
- **Action**: Suppress linter warnings with `//nolint:unused`

---

### 6.3 `lifecycles` parameter

**Context**: Need specific location

**Recommendation**: Pending identification

---

### 6.4 `sql` parameter

**Context**: Need specific location

**Recommendation**: Pending identification

---

### 6.5 `tableName` variable

**Context**: Need specific location

**Recommendation**: Pending identification

---

### 6.6 `engine` parameter (ConsolidationEngine interface)

**Context**: Likely in consolidation rules

**Analysis**:
- Rules may not need engine but interface requires it
- Common pattern for extensibility

**Recommendation**: **KEEP**
- **Reason**: Interface extensibility
- **Action**: Add comment explaining why unused

---

### 6.7 `id` parameter (type int)

**Context**: Need specific location

**Recommendation**: Pending identification

---

### 6.8 `backupPath` parameter (type string)

**Context**: Likely in validation or backup logic

**Analysis**: May be for future backup feature

**Recommendation**: **Pending feature analysis**

---

### 6.9 `result` parameter (TransformationResult)

**Context**: Likely in transformation pipeline

**Analysis**: May be for transformation result processing

**Recommendation**: **Pending transformation pipeline analysis**

---

## Category 7: Struct Fields

### 7.1 `ColumnEvolutions` in ConsolidationResult (internal/tracking/unified_tracker.go:557)

**Purpose**: Track column rename/evolution operations

**Current State**:
- **FALSE POSITIVE**: This field IS being used!
- Populated by multiple consolidation rules:
  - `multiple_create_rule.go:136`
  - `column_evolution_rule.go:94`
  - `separate_alter_rule.go:123`
  - `advanced_column_lifecycle_rule.go:171`
  - `drop_create_rule.go:133`
- Consumed in `engine.go:2183` (iteration over evolutions)

**Analysis**:
- Linter warning is incorrect
- Field has proper nil check (engine.go:2178)
- Actively used in column lifecycle tracking

**Recommendation**: **KEEP - FALSE POSITIVE**
- **Reason**: Field is actively used
- **Action**: Linter should be suppressed or ignore this warning

---

## Category 8: Dependency Resolver

### 8.1 `getTypeOrderForLifecycle` (internal/squasher/unified_dependency_resolver.go:483)

**Purpose**: Get type order with special handling for DO blocks containing CREATE TYPE

**Current State**:
- Handles DO block → CREATE TYPE detection
- Returns type order for dependency resolution
- Logs when DO block is treated as enum
- **Currently unused**

**Analysis**:
- This is more sophisticated than `getTypeOrder()`
- Provides lifecycle-aware ordering
- Special case handling for DO blocks with enums
- **Should likely be used instead of `getTypeOrder()` in some places**

**Integration Opportunity**:
- Review calls to `getTypeOrder()` in UnifiedDependencyResolver
- Replace with `getTypeOrderForLifecycle()` where lifecycle is available
- Improves DO block handling

**Recommendation**: **KEEP & INTEGRATE**
- **Reason**: Better DO block handling
- **Priority**: Medium
- **Benefit**: More accurate dependency ordering for DO blocks
- **Action**: Audit getTypeOrder() calls, replace where appropriate

---

## Summary of Recommendations

### Immediate Actions (High Priority)

1. **`consolidateIndexesForTable`** - INTEGRATE into MultipleCreateRule
   - **Impact**: Fixes index loss bug
   - **Effort**: 2-4 hours
   - **File**: [internal/tracking/consolidation/multiple_create_rule.go](internal/tracking/consolidation/multiple_create_rule.go)

2. **`isAuthFunction`** - INTEGRATE into determineVolatility
   - **Impact**: Correct auth function volatility
   - **Effort**: 30 minutes
   - **File**: [internal/transformation/sql_transformer.go](internal/transformation/sql_transformer.go)

3. **`getTypeOrderForLifecycle`** - INTEGRATE into dependency resolution
   - **Impact**: Better DO block handling
   - **Effort**: 1-2 hours
   - **File**: [internal/squasher/unified_dependency_resolver.go](internal/squasher/unified_dependency_resolver.go)

### Short-term Actions (Medium Priority)

4. **`normalizeLanguagePosition`** - INTEGRATE conditionally
   - **Impact**: Consistent function formatting
   - **Effort**: 1 hour
   - **Decision**: Review transformation pipeline first

5. **`preprocessMigrationSQL`** - INTEGRATE with config flag
   - **Impact**: Cleaner validation
   - **Effort**: 2 hours
   - **File**: [internal/validation/publication_dedup.go](internal/validation/publication_dedup.go)

### Roadmap Actions (Phase 3-4)

6. **`consolidateAuth0Policies`** - CREATE Auth0 plugin
   - **Impact**: Auth0 support (popular provider)
   - **Effort**: 8-16 hours (full plugin)
   - **Roadmap**: Matches Phase 3-4 goals

### Remove Immediately

7. **`deparseNode`** - REMOVE (trivial utility)
8. **`extractFirstCompleteFunction`** - REMOVE (superseded)
9. **`ensureLanguageClausesPresent`** - REMOVE (deprecated)
10. **`extractFunctionNameFromSQL`** - REMOVE (supporting deprecated function)
11. **`dumpContainerSchema`** - REMOVE (deprecated)
12. **`extractColumnFromAddStatement`** - REMOVE (superseded by multi-column version)

### Pending Analysis (Low Priority)

13. **`cleanAlterAction`** - Check if DO block rule should use this
14. **Various unused parameters** - Review and add proper comments explaining why unused

### False Positives

15. **`ColumnEvolutions`** - KEEP (actively used, linter error)

---

## Next Steps

1. ✅ Complete this assessment document
2. ⬜ Get approval on recommendations
3. ⬜ Create GitHub issues for:
   - Integration tasks (HIGH priority)
   - Auth0 plugin (Phase 3-4)
4. ⬜ Execute removals (with git commits for each)
5. ⬜ Execute integrations (with tests)
6. ⬜ Update documentation

---

## Metrics

- **Total unused items assessed**: 23
- **Should integrate**: 5 (HIGH: 3, MEDIUM: 2)
- **Should remove**: 6
- **Phase 3-4 (Roadmap)**: 1
- **False positives**: 1
- **Pending low-priority analysis**: 2

**Estimated effort**:
- **Immediate integrations (HIGH)**: 4-7 hours
- **Medium priority integrations**: 3-4 hours
- **Removals**: 1-2 hours
- **Testing/validation**: 2-3 hours
- **Total high-value work**: ~10-16 hours

**Impact Assessment**:
- **Bug fixes**: 1 (index loss in multiple CREATE cycles)
- **Correctness improvements**: 2 (auth function volatility, DO block ordering)
- **Code quality**: 6 functions removed (reduce maintenance burden)
- **Future capability**: 1 (Auth0 plugin groundwork)

