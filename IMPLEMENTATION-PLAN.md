# Implementation Plan: Unused Code Integration & Removal

**Created**: 2025-11-12
**Based on**: [UNUSED-CODE-ASSESSMENT.md](UNUSED-CODE-ASSESSMENT.md)
**Target**: v0.10.0 release

---

## Phase 1: High-Priority Integrations (4-7 hours)

### Task 1.1: Integrate `consolidateIndexesForTable` (2-3 hours)

**Priority**: CRITICAL - Fixes data loss bug (indexes lost during DROP→CREATE cycles)

**Location**: [internal/tracking/consolidation/multiple_create_rule.go:171](internal/tracking/consolidation/multiple_create_rule.go#L171)

**Implementation Steps**:

1. **Review current implementation** (15 min)
   - Read `consolidateIndexesForTable` function (lines 171-270)
   - Understand index deduplication logic
   - Review AST-based table association tracking

2. **Integrate into MultipleCreateRule.Apply()** (1 hour)
   ```go
   // In MultipleCreateRule.Apply() method, after merging CREATE statements:

   // Step 1: Get consolidated CREATE statement
   mergedSQL := mergeMultipleCreateStatements(lifecycle.Name, allCreateStmts)

   // Step 2: Consolidate indexes from all CREATE events (NEW)
   consolidatedIndexes := consolidateIndexesForTable(lifecycle, engine, allCreateStmts)

   // Step 3: Append index definitions to merged SQL
   if len(consolidatedIndexes) > 0 {
       for _, indexSQL := range consolidatedIndexes {
           mergedSQL += "\n\n" + indexSQL + ";"
       }
   }
   ```

3. **Add tests** (1 hour)
   - Test case: CREATE table with indexes → DROP → CREATE table (different schema)
   - Verify: All indexes from both CREATEs are preserved
   - Test deduplication: Same index name → latest definition wins

4. **Validate with case studies** (30 min)
   - Run on `myroomie` case study
   - Run on `vdk hub` case study
   - Compare index counts before/after fix

**Testing Commands**:
```bash
# Unit tests
go test ./internal/tracking/consolidation -v -run TestMultipleCreateRule

# E2E validation
cd case-studies/myroomie
pgsquash squash migrations/*.sql --output test-with-index-fix/ --dry-run
# Count CREATE INDEX statements in output vs original
grep -c "CREATE INDEX" test-with-index-fix/*.sql
grep -c "CREATE INDEX" migrations/*.sql
```

**Expected Outcome**:
- Index loss bug is fixed
- All indexes from all CREATE events are preserved
- Deduplication prevents duplicate indexes

---

### Task 1.2: Integrate `isAuthFunction` (30 min)

**Priority**: HIGH - Improves auth function correctness

**Location**: [internal/transformation/sql_transformer.go:883](internal/transformation/sql_transformer.go#L883)

**Implementation Steps**:

1. **Add check in `determineVolatility()`** (15 min)
   ```go
   func (st *SQLTransformer) determineVolatility(functionBody string) string {
       bodyUpper := strings.ToUpper(functionBody)

       // NEW: Check if this is a known auth function (Clerk, Supabase, Auth0)
       // Auth functions should always be STABLE for RLS compatibility
       if isAuthFunction(functionBody) {
           return "STABLE"
       }

       // Existing pattern matching...
   }
   ```

2. **Extract function name from body** (10 min)
   - `isAuthFunction` expects function name, not body
   - Need to extract function name from CREATE FUNCTION statement
   - Add helper or modify `isAuthFunction` to accept full SQL

3. **Add tests** (5 min)
   - Test: Clerk functions get STABLE marker
   - Test: Supabase functions get STABLE marker
   - Test: Regular functions follow existing logic

**Testing Commands**:
```bash
# Run transformation tests
go test ./internal/transformation -v -run TestDetermineVolatility

# Manual test with Clerk function
echo "CREATE FUNCTION clerk_user_id() RETURNS TEXT AS \$\$ BEGIN RETURN auth.jwt()->>'sub'; END; \$\$ LANGUAGE plpgsql;" > /tmp/test.sql
pgsquash squash /tmp/test.sql --dry-run | grep -i "stable"
```

**Expected Outcome**:
- Auth functions (Clerk, Supabase) always get STABLE marker
- RLS policies work correctly with auth functions

---

### Task 1.3: Integrate `getTypeOrderForLifecycle` (1-2 hours)

**Priority**: HIGH - Better DO block handling

**Location**: [internal/squasher/unified_dependency_resolver.go:483](internal/squasher/unified_dependency_resolver.go#L483)

**Implementation Steps**:

1. **Audit `getTypeOrder()` calls** (30 min)
   ```bash
   # Find all calls to getTypeOrder
   grep -rn "getTypeOrder(" internal/squasher/unified_dependency_resolver.go

   # Check which calls have lifecycle available
   # Replace with getTypeOrderForLifecycle where appropriate
   ```

2. **Replace appropriate calls** (30 min)
   - In `ResolveAdvanced()` method: replace calls where lifecycle is available
   - In `buildInitialOrder()`: use lifecycle-aware version
   - Keep `getTypeOrder()` for cases without lifecycle

3. **Add tests** (30 min)
   - Test: DO block with CREATE TYPE ENUM → prioritized correctly
   - Test: Regular DO block → normal ordering
   - Verify dependency resolution logs show correct ordering

**Testing Commands**:
```bash
# Run dependency resolver tests
go test ./internal/squasher -v -run TestUnifiedDependencyResolver

# Test with DO block containing enum
cat > /tmp/test-do-enum.sql << 'EOF'
DO $$
BEGIN
    CREATE TYPE user_role AS ENUM ('admin', 'user');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE users (
    id INT PRIMARY KEY,
    role user_role NOT NULL
);
EOF

pgsquash squash /tmp/test-do-enum.sql --dry-run
# Verify DO block appears before table creation
```

**Expected Outcome**:
- DO blocks with CREATE TYPE are ordered correctly
- Dependency resolution is more accurate
- Fewer ordering conflicts

---

## Phase 2: Medium-Priority Integrations (3-4 hours)

### Task 2.1: Conditionally integrate `normalizeLanguagePosition` (1 hour)

**Priority**: MEDIUM - Consistency improvement

**Location**: [internal/transformation/sql_transformer.go:622](internal/transformation/sql_transformer.go#L622)

**Decision Point**:
- **IF** we're doing AST-based function modifications: Integrate
- **IF** we're preserving original SQL: Remove

**Implementation (if integrating)**:

1. **Review function transformation pipeline** (20 min)
   - Check `deparseWithVolatilityPreservation` (deparser.go:110)
   - Determine if we're modifying AST or preserving SQL

2. **Add call before volatility injection** (20 min)
   ```go
   func deparseWithVolatilityPreservation(tree *pg_query.ParseResult, originalSQL string) (string, error) {
       // Deparse normally
       deparsed, err := Deparse(tree)
       if err != nil {
           return "", err
       }

       // NEW: Normalize LANGUAGE position before adding volatility
       // This ensures consistent format: LANGUAGE xxx [VOLATILITY] AS $$
       deparsed = st.normalizeLanguagePosition(deparsed)

       // Extract volatility and inject...
   }
   ```

3. **Test format consistency** (20 min)

**Alternative (if removing)**:
- Remove function
- Document that we preserve original SQL format

**Decision**: Requires review of function transformation approach

---

### Task 2.2: Integrate `preprocessMigrationSQL` (2 hours)

**Priority**: MEDIUM - Validation improvement

**Location**: [internal/validation/publication_dedup.go:70](internal/validation/publication_dedup.go#L70)

**Implementation Steps**:

1. **Add configuration option** (30 min)
   ```json
   // In config.go
   type ValidationConfig struct {
       Mode                   string `json:"mode"`
       EnableExtensionDetection bool `json:"enable_extension_detection"`
       EnableSQLFixes          bool   `json:"enable_sql_fixes"`
       EnablePreprocessing     bool   `json:"enable_preprocessing"`  // NEW
   }
   ```

2. **Wire into validation pipeline** (1 hour)
   ```go
   // In validator.go, before applying migrations
   func (sv *SchemaValidator) applyMigrations(ctx context.Context, containerInfo *ContainerInfo, migrations []Migration) error {
       for _, migration := range migrations {
           sql := migration.SQL

           // NEW: Preprocess SQL if enabled
           if sv.config.EnablePreprocessing {
               sql = preprocessMigrationSQL(sql, true)  // enable publication dedup
           }

           // Apply migration...
       }
   }
   ```

3. **Add tests** (30 min)
   - Test: Duplicate publication statements are deduplicated
   - Test: Other SQL is preserved unchanged

**Testing Commands**:
```bash
# Test with duplicate publications
cat > /tmp/test-pub.sql << 'EOF'
CREATE PUBLICATION my_pub FOR ALL TABLES;
ALTER PUBLICATION my_pub ADD TABLE users;
ALTER PUBLICATION my_pub ADD TABLE users;  -- Duplicate
EOF

pgsquash validate /tmp/test-pub.sql --validation-mode TWO_CONTAINERS
# Should not fail with "relation is already member of publication"
```

**Expected Outcome**:
- Validation handles duplicate publications gracefully
- Fewer false positive validation failures

---

## Phase 3: Removals (1-2 hours)

**Priority**: MEDIUM - Code cleanup

### Removal Checklist

Each removal should be a separate git commit for easy reversal if needed.

#### 1. Remove `deparseNode` (5 min)
```bash
# File: internal/squasher/deparser.go
# Lines: 271-286
# Reason: Trivial utility, never called
git commit -m "Remove unused deparseNode utility function"
```

#### 2. Remove `extractFirstCompleteFunction` (5 min)
```bash
# File: internal/tracking/consolidation/function_dedup_rule.go
# Lines: 95-196
# Reason: Superseded by "preserve original SQL" approach
git commit -m "Remove unused extractFirstCompleteFunction (superseded)"
```

#### 3. Remove `ensureLanguageClausesPresent` (5 min)
```bash
# File: internal/postprocessing/ast/function_normalizer.go
# Lines: 413-590
# Reason: Intentionally deprecated, known to be buggy
git commit -m "Remove deprecated ensureLanguageClausesPresent"
```

#### 4. Remove `extractFunctionNameFromSQL` (5 min)
```bash
# File: internal/postprocessing/ast/function_normalizer.go
# Lines: 593-610
# Reason: Only used by deprecated function
git commit -m "Remove extractFunctionNameFromSQL (supporting deprecated function)"
```

#### 5. Remove `dumpContainerSchema` (5 min)
```bash
# File: internal/validation/validator.go
# Lines: 2370-2386
# Reason: Deprecated, replaced by DumpAndNormalizeContainerSchema
git commit -m "Remove deprecated dumpContainerSchema backward compat shim"
```

#### 6. Remove `extractColumnFromAddStatement` (5 min)
```bash
# File: internal/tracking/consolidation/create_alter_rule.go
# Lines: 257-289
# Reason: Superseded by extractMultipleAddColumnsFromAlter
git commit -m "Remove unused extractColumnFromAddStatement (superseded)"
```

### Removal Execution
```bash
# Create removal branch
git checkout -b cleanup/remove-unused-functions

# Remove each function (using Edit tool, one commit each)
# ... perform removals ...

# Verify no references remain
go build ./...
go test ./...

# Push for review
git push origin cleanup/remove-unused-functions
```

**Expected Outcome**:
- ~350 lines of unused code removed
- Reduced maintenance burden
- Clearer codebase for new contributors

---

## Phase 4: Testing & Validation (2-3 hours)

### Task 4.1: Unit Tests (1 hour)

**New test files needed**:

1. **`multiple_create_rule_test.go`**
   - Test index consolidation
   - Test deduplication
   - Test sequence preservation

2. **`sql_transformer_test.go`** (enhance existing)
   - Test auth function detection
   - Test volatility determination

3. **`unified_dependency_resolver_test.go`** (enhance existing)
   - Test DO block ordering
   - Test lifecycle-aware type ordering

**Example test**:
```go
func TestConsolidateIndexesForTable(t *testing.T) {
    tests := []struct {
        name           string
        migrations     []string
        expectedIndexes int
    }{
        {
            name: "DROP-CREATE cycle preserves indexes",
            migrations: []string{
                "CREATE TABLE users (id INT); CREATE INDEX idx_1 ON users(id);",
                "DROP TABLE users;",
                "CREATE TABLE users (id INT, name TEXT); CREATE INDEX idx_2 ON users(name);",
            },
            expectedIndexes: 2,  // Both indexes should be in final output
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Run consolidation
            // Count indexes in output
            // Assert equal to expected
        })
    }
}
```

### Task 4.2: E2E Validation (1 hour)

**Test with all case studies**:
```bash
#!/bin/bash
# Run E2E tests on all case studies

for project in myroomie "nami ai app" "vdk hub"; do
    echo "Testing $project..."
    cd "case-studies/$project"

    # Run squash with new code
    pgsquash squash migrations/*.sql --output test-output/ --safety standard

    # Validate equivalence
    pgsquash validate migrations/ test-output/ --validation-mode TWO_CONTAINERS

    # Check for index preservation
    original_indexes=$(grep -c "CREATE INDEX" migrations/*.sql)
    squashed_indexes=$(grep -c "CREATE INDEX" test-output/*.sql)

    echo "$project: Original=$original_indexes, Squashed=$squashed_indexes"

    cd ../..
done
```

### Task 4.3: Regression Testing (1 hour)

**Run existing E2E tests**:
```bash
# Run all existing E2E tests to ensure no regressions
go test ./... -v

# Run specific high-value tests
go test ./internal/squasher -v
go test ./internal/tracking/consolidation -v
go test ./internal/transformation -v
```

---

## Phase 5: Roadmap Integration (8-16 hours, Phase 3-4)

### Task 5.1: Create Auth0 Plugin (Full implementation)

**Priority**: ROADMAP PHASE 3-4 (not immediate)

**Location**: New package `internal/plugins/auth0/`

**Implementation Steps** (when prioritized):

1. **Create plugin structure** (2 hours)
   - `internal/plugins/auth0/plugin.go`
   - Implement 12 lifecycle hook methods
   - Priority: 95 (same as Clerk)

2. **Add detection patterns** (1 hour)
   - JWT claims with Auth0 patterns
   - Auth0 table structures
   - auth0_ function prefixes

3. **Wire up consolidation** (2 hours)
   - Use existing `consolidateAuth0Policies` function
   - Add to plugin's Consolidate() hook

4. **Add tests** (2 hours)
   - Detection tests
   - Consolidation tests
   - Integration tests

5. **Documentation** (1 hour)
   - Update plugin README
   - Add Auth0 detection patterns doc
   - Update CLI reference

**Defer to Phase 3-4** (see ROADMAP.md)

---

## Timeline & Milestones

### Week 1 (Immediate)
- ✅ Complete assessment (DONE)
- ⬜ Day 1: Task 1.1 (consolidateIndexesForTable)
- ⬜ Day 2: Task 1.2 + 1.3 (isAuthFunction, getTypeOrderForLifecycle)
- ⬜ Day 3: Phase 3 (removals)
- ⬜ Day 4: Task 4.1 + 4.2 (testing)
- ⬜ Day 5: Task 4.3 + documentation

### Week 2 (Medium Priority)
- ⬜ Task 2.1 (normalizeLanguagePosition decision)
- ⬜ Task 2.2 (preprocessMigrationSQL)
- ⬜ Final validation
- ⬜ Release v0.10.0

### Phase 3-4 (per ROADMAP.md)
- ⬜ Task 5.1 (Auth0 plugin)

---

## Success Criteria

### Immediate Success (v0.10.0)
- [ ] Index loss bug is fixed
- [ ] Auth functions get correct volatility
- [ ] DO block ordering is improved
- [ ] 6 unused functions removed
- [ ] All tests pass
- [ ] Case studies validate successfully
- [ ] No regressions in E2E tests

### Code Quality Success
- [ ] Test coverage increases by ~5%
- [ ] Linter warnings reduced by 10+
- [ ] Code complexity reduced (fewer LOC)
- [ ] Better documented integration points

### Roadmap Alignment
- [ ] Phase 1 items complete before 1.0.0
- [ ] Auth0 plugin groundwork laid
- [ ] Plugin architecture validated

---

## Risk Mitigation

### Risk 1: Index consolidation breaks something
**Mitigation**:
- Extensive unit tests
- E2E validation on 3 case studies
- Feature flag if needed: `enable_index_consolidation`
- Easy rollback (separate commit)

### Risk 2: Auth function detection has false positives
**Mitigation**:
- Conservative pattern matching
- Logs show when function is detected as auth
- Can be refined based on real-world usage

### Risk 3: Removals break something
**Mitigation**:
- Each removal is a separate commit
- Easy to cherry-pick revert if needed
- Thorough grep before removal confirms no usage

---

## Dependencies

### Required Before Start
- [ ] Review and approval of UNUSED-CODE-ASSESSMENT.md
- [ ] Create GitHub issues for tracking:
  - Issue #X: Integrate consolidateIndexesForTable
  - Issue #Y: Integrate isAuthFunction
  - Issue #Z: Integrate getTypeOrderForLifecycle
  - Issue #A: Remove 6 unused functions

### Parallel Work (can be done simultaneously)
- Integrations (Phase 1)
- Removals (Phase 3)

### Blocked/Sequential
- Testing (Phase 4) requires Phase 1 complete
- Roadmap items (Phase 5) require v0.10.0 release

---

## Rollback Plan

If major issues arise after integration:

1. **Immediate rollback**: Revert specific commits
2. **Feature flags**: Add config options to disable new features
3. **Version pin**: Users can stay on v0.9.5 if needed

**Rollback commits**:
```bash
# Rollback index consolidation
git revert <commit-hash-for-1.1>

# Rollback auth function detection
git revert <commit-hash-for-1.2>

# Rollback DO block ordering
git revert <commit-hash-for-1.3>
```

---

## Documentation Updates

### Files to update after implementation:

1. **CHANGELOG.md**
   ```markdown
   ## [0.10.0] - 2025-11-XX

   ### Fixed
   - Index loss bug in DROP→CREATE cycles
   - Auth function volatility detection
   - DO block dependency ordering

   ### Removed
   - 6 unused/deprecated functions (code cleanup)

   ### Improved
   - Test coverage increased by ~5%
   - Code complexity reduced
   ```

2. **docs/consolidation-rules.md**
   - Document index consolidation behavior
   - Add examples of DROP→CREATE index preservation

3. **docs/transformation.md**
   - Document auth function detection
   - List supported auth patterns (Clerk, Supabase)

4. **internal/plugins/README.md** (when Auth0 is added)
   - Add Auth0 plugin documentation

---

## Questions & Decisions

### Open Questions

1. **Q**: Should `normalizeLanguagePosition` be integrated or removed?
   - **Decision needed**: Review transformation pipeline approach
   - **Owner**: TBD
   - **Deadline**: Before Phase 2

2. **Q**: Should `cleanAlterAction` be integrated or removed?
   - **Decision needed**: Review DO block rule
   - **Owner**: TBD
   - **Deadline**: Low priority (can defer)

3. **Q**: Priority for Auth0 plugin implementation?
   - **Answer**: Phase 3-4 of roadmap (after 1.0.0)

### Decisions Made

1. **✅ Remove deprecated functions**: Approved (6 functions)
2. **✅ Integrate index consolidation**: Approved (HIGH priority)
3. **✅ Integrate auth function detection**: Approved (HIGH priority)
4. **✅ Integrate DO block ordering**: Approved (HIGH priority)

---

**Next Action**: Execute Phase 1, Task 1.1 (consolidateIndexesForTable integration)

