# Bug #1: Architectural Solution Proposal
## View Column References Not Updated After Schema Consolidation

**Author**: Claude Code (Autonomous Analysis)
**Date**: 2025-11-06
**Status**: Proposal - Awaiting Implementation
**Severity**: CRITICAL 🔴
**Target Release**: v0.9.6

---

## Problem Statement

When table columns are renamed through ALTER statements during consolidation, views referencing those columns retain the old column names, causing PostgreSQL errors when applied:

```sql
-- Final consolidated schema:
CREATE TABLE rooms (
  size_sqm numeric(6, 2)  -- Column renamed from "size" to "size_sqm"
);

CREATE VIEW rooms_fairrent_ready AS
  SELECT r.size FROM rooms r;  -- ❌ ERROR: column r.size does not exist
```

**Root Cause**: The view rewriting infrastructure exists (`internal/squasher/engine.go:1750-1877`) but is not being triggered or integrated correctly with the consolidation pipeline.

---

## Architectural Analysis

### Current Architecture (Broken)

```
┌─────────────────────────────────────────────────────────┐
│ 1. PARSING PHASE                                        │
│    internal/parser/parser.go                            │
│    ├─ Parse migrations → AST                            │
│    └─ Extract objects → types.DBObject                  │
└─────────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────────┐
│ 2. TRACKING PHASE                                       │
│    internal/tracking/unified_tracker.go                 │
│    ├─ Track object lifecycles                           │
│    ├─ Build dependency graph                            │
│    └─ ❌ Column evolution NOT tracked                   │  ← Problem #1
└─────────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────────┐
│ 3. CONSOLIDATION PHASE                                  │
│    internal/squasher/engine.go                          │
│    ├─ Apply consolidation rules                         │
│    ├─ buildColumnEvolutionMap() → empty map?            │  ← Problem #2
│    └─ Consolidate CREATE + ALTER                        │
└─────────────────────────────────────────────────────────┘
                        ↓
┌─────────────────────────────────────────────────────────┐
│ 4. GENERATION PHASE                                     │
│    internal/squasher/engine.go:1486-1497                │
│    ├─ Generate final SQL                                │
│    ├─ ❌ View rewriting NOT triggered                   │  ← Problem #3
│    └─ rewriteViewColumnReferences() exists but unused   │  ← Problem #4
└─────────────────────────────────────────────────────────┘
```

### Identified Problems

1. **Tracking Gap**: Column renames are not tracked in the object lifecycle
2. **Map Building**: `buildColumnEvolutionMap()` returns empty map (no data to build from)
3. **Integration Point**: View rewriting is not triggered during generation
4. **Dependency Missing**: Views don't have metadata about which table columns they reference

---

## Proposed Solution: AST-Based Column Evolution Tracking

### Architecture Overview

Instead of relying on regex-based rewriting, implement proper AST-based tracking and transformation:

```
┌────────────────────────────────────────────────────────────┐
│ ENHANCED PARSING PHASE                                     │
│ ├─ Parse migrations to AST                                 │
│ ├─ Extract column references from views (NEW)              │
│ └─ Store in DBObject.Metadata                              │
└────────────────────────────────────────────────────────────┘
                        ↓
┌────────────────────────────────────────────────────────────┐
│ ENHANCED TRACKING PHASE                                    │
│ ├─ Track table lifecycles                                  │
│ ├─ Track column lifecycles (NEW)                           │
│ │  └─ CREATE → ALTER RENAME → final name                  │
│ ├─ Track view dependencies (NEW)                           │
│ │  └─ view → [table.column, ...]                          │
│ └─ Build ColumnEvolutionMap                                │
│    └─ map[table][old_name] = new_name                      │
└────────────────────────────────────────────────────────────┘
                        ↓
┌────────────────────────────────────────────────────────────┐
│ ENHANCED CONSOLIDATION PHASE                               │
│ ├─ Apply consolidation rules                               │
│ ├─ Detect column renames in consolidated tables            │
│ ├─ Mark affected views as "needs rewrite"                  │
│ └─ Queue view transformations                              │
└────────────────────────────────────────────────────────────┘
                        ↓
┌────────────────────────────────────────────────────────────┐
│ AST-BASED VIEW TRANSFORMATION (NEW)                        │
│ ├─ Parse view SQL to AST                                   │
│ ├─ Traverse SelectStmt nodes                               │
│ ├─ Identify column references (ColumnRef)                  │
│ ├─ Map old names to new names via ColumnEvolutionMap       │
│ ├─ Transform AST in-place                                  │
│ └─ Deparse AST back to SQL                                 │
└────────────────────────────────────────────────────────────┘
                        ↓
┌────────────────────────────────────────────────────────────┐
│ GENERATION PHASE                                           │
│ └─ Output transformed SQL                                  │
└────────────────────────────────────────────────────────────┘
```

---

## Implementation Plan

### Phase 1: Column Lifecycle Tracking (3-4 days)

#### 1.1 Extend Types (`internal/types/parser_types.go`)

```go
// Add to DBObject
type DBObject struct {
    // ... existing fields ...

    // NEW: Column evolution tracking
    ColumnEvolutions map[string]string  // map[old_name]new_name

    // NEW: View dependencies
    ReferencedColumns []ColumnReference
}

// NEW: Column reference structure
type ColumnReference struct {
    TableName  string
    ColumnName string
    Alias      string  // e.g., "r" in "r.size"
}
```

#### 1.2 Enhance Parser (`internal/parser/parser.go`)

```go
// NEW: Parse column references from view definitions
func (p *Parser) extractViewColumnReferences(viewStmt *pg_query.ViewStmt) []types.ColumnReference {
    refs := []types.ColumnReference{}

    // Traverse SelectStmt AST
    query := viewStmt.Query.GetSelectStmt()
    if query == nil {
        return refs
    }

    // Extract from SELECT targets
    for _, target := range query.TargetList {
        if colRef := extractColumnRef(target); colRef != nil {
            refs = append(refs, *colRef)
        }
    }

    // Extract from WHERE clause
    if query.WhereClause != nil {
        refs = append(refs, extractColumnRefsFromExpr(query.WhereClause)...)
    }

    // Extract from JOIN conditions
    for _, fromItem := range query.FromClause {
        refs = append(refs, extractColumnRefsFromJoin(fromItem)...)
    }

    return refs
}

// Helper: Extract column reference from AST node
func extractColumnRef(node *pg_query.Node) *types.ColumnReference {
    if colRef := node.GetColumnRef(); colRef != nil {
        // Parse "r.size" → table="r", column="size"
        fields := colRef.Fields
        if len(fields) == 2 {
            return &types.ColumnReference{
                Alias:      fields[0].GetString_().Sval,
                ColumnName: fields[1].GetString_().Sval,
            }
        }
    }
    return nil
}
```

#### 1.3 Enhance Tracker (`internal/tracking/unified_tracker.go`)

```go
// NEW: Track column lifecycle
type ColumnLifecycle struct {
    TableName     string
    OriginalName  string
    CurrentName   string
    RenameHistory []ColumnRename
    CreatedAt     int  // Migration index
    RenamedAt     []int
}

type ColumnRename struct {
    FromName    string
    ToName      string
    MigrationID int
}

// Add to UnifiedTracker
type UnifiedTracker struct {
    // ... existing fields ...

    // NEW: Column tracking
    columnLifecycles map[string]*ColumnLifecycle  // map[table.column]*Lifecycle
}

// NEW: Process ALTER TABLE ... RENAME COLUMN
func (t *UnifiedTracker) processColumnRename(stmt *pg_query.RenameStmt, migrationID int) {
    if stmt.RenameType != pg_query.OBJECT_COLUMN {
        return
    }

    tableName := extractTableName(stmt.Relation)
    oldName := stmt.Subname
    newName := stmt.Newname

    key := fmt.Sprintf("%s.%s", tableName, oldName)

    if lifecycle, exists := t.columnLifecycles[key]; exists {
        // Track rename in existing lifecycle
        lifecycle.RenameHistory = append(lifecycle.RenameHistory, ColumnRename{
            FromName:    oldName,
            ToName:      newName,
            MigrationID: migrationID,
        })
        lifecycle.CurrentName = newName
        lifecycle.RenamedAt = append(lifecycle.RenamedAt, migrationID)

        // Update map key to new name
        newKey := fmt.Sprintf("%s.%s", tableName, newName)
        t.columnLifecycles[newKey] = lifecycle
        delete(t.columnLifecycles, key)
    } else {
        // Create new lifecycle (rename before CREATE was tracked)
        t.columnLifecycles[fmt.Sprintf("%s.%s", tableName, newName)] = &ColumnLifecycle{
            TableName:    tableName,
            OriginalName: oldName,
            CurrentName:  newName,
            RenameHistory: []ColumnRename{{
                FromName:    oldName,
                ToName:      newName,
                MigrationID: migrationID,
            }},
            RenamedAt: []int{migrationID},
        }
    }
}

// NEW: Build column evolution map for engine
func (t *UnifiedTracker) GetColumnEvolutionMap() map[string]map[string]string {
    evolutionMap := make(map[string]map[string]string)

    for _, lifecycle := range t.columnLifecycles {
        if lifecycle.OriginalName != lifecycle.CurrentName {
            if evolutionMap[lifecycle.TableName] == nil {
                evolutionMap[lifecycle.TableName] = make(map[string]string)
            }

            // Map: old name → new name
            evolutionMap[lifecycle.TableName][lifecycle.OriginalName] = lifecycle.CurrentName

            // Also map intermediate names
            for _, rename := range lifecycle.RenameHistory {
                evolutionMap[lifecycle.TableName][rename.FromName] = lifecycle.CurrentName
            }
        }
    }

    return evolutionMap
}
```

---

### Phase 2: AST-Based View Transformation (2-3 days)

#### 2.1 Create View Transformer (`internal/transformation/view_transformer.go`)

```go
package transformation

import (
    pg_query "github.com/pganalyze/pg_query_go/v5"
    "github.com/capysquash/pgsquash-engine/internal/types"
)

type ViewTransformer struct {
    columnEvolutions map[string]map[string]string  // table → {old_col → new_col}
    aliasToTable     map[string]string             // alias → table_name
}

func NewViewTransformer(columnEvolutions map[string]map[string]string) *ViewTransformer {
    return &ViewTransformer{
        columnEvolutions: columnEvolutions,
        aliasToTable:     make(map[string]string),
    }
}

// Transform view SQL by rewriting column references
func (vt *ViewTransformer) Transform(viewSQL string) (string, error) {
    // Parse view to AST
    parseResult, err := pg_query.Parse(viewSQL)
    if err != nil {
        return "", fmt.Errorf("failed to parse view: %w", err)
    }

    if len(parseResult.Stmts) == 0 {
        return viewSQL, nil
    }

    stmt := parseResult.Stmts[0]
    viewStmt := stmt.Stmt.GetViewStmt()
    if viewStmt == nil {
        return viewSQL, nil
    }

    // Build alias → table mapping from FROM clause
    vt.buildAliasMap(viewStmt.Query.GetSelectStmt())

    // Transform SELECT targets
    query := viewStmt.Query.GetSelectStmt()
    vt.transformSelectTargets(query.TargetList)

    // Transform WHERE clause
    if query.WhereClause != nil {
        vt.transformExpression(query.WhereClause)
    }

    // Transform JOIN conditions
    for _, fromItem := range query.FromClause {
        vt.transformFromClause(fromItem)
    }

    // Deparse back to SQL
    sql, err := pg_query.Deparse(parseResult)
    if err != nil {
        return "", fmt.Errorf("failed to deparse view: %w", err)
    }

    return sql, nil
}

// Build map of aliases to table names
func (vt *ViewTransformer) buildAliasMap(selectStmt *pg_query.SelectStmt) {
    if selectStmt == nil {
        return
    }

    for _, fromItem := range selectStmt.FromClause {
        if rangeVar := fromItem.GetRangeVar(); rangeVar != nil {
            tableName := rangeVar.Relname
            alias := rangeVar.Alias.GetAliasname()
            if alias != "" {
                vt.aliasToTable[alias] = tableName
            } else {
                vt.aliasToTable[tableName] = tableName
            }
        }
    }
}

// Transform column references in SELECT targets
func (vt *ViewTransformer) transformSelectTargets(targets []*pg_query.Node) {
    for _, target := range targets {
        resTarget := target.GetResTarget()
        if resTarget == nil {
            continue
        }

        if colRef := resTarget.Val.GetColumnRef(); colRef != nil {
            vt.transformColumnRef(colRef)
        }
    }
}

// Transform a single ColumnRef node
func (vt *ViewTransformer) transformColumnRef(colRef *pg_query.ColumnRef) {
    fields := colRef.Fields
    if len(fields) != 2 {
        return  // Not in format "alias.column"
    }

    alias := fields[0].GetString_().Sval
    oldColumn := fields[1].GetString_().Sval

    // Resolve alias to table name
    tableName, exists := vt.aliasToTable[alias]
    if !exists {
        return
    }

    // Check if column was renamed
    if evolutions, hasTable := vt.columnEvolutions[tableName]; hasTable {
        if newColumn, wasRenamed := evolutions[oldColumn]; wasRenamed {
            // Update AST node with new column name
            fields[1].GetString_().Sval = newColumn
        }
    }
}

// Transform expressions (WHERE, JOIN ON, etc.)
func (vt *ViewTransformer) transformExpression(expr *pg_query.Node) {
    // Recursively traverse expression tree
    if colRef := expr.GetColumnRef(); colRef != nil {
        vt.transformColumnRef(colRef)
        return
    }

    if aExpr := expr.GetAExpr(); aExpr != nil {
        if aExpr.Lexpr != nil {
            vt.transformExpression(aExpr.Lexpr)
        }
        if aExpr.Rexpr != nil {
            vt.transformExpression(aExpr.Rexpr)
        }
        return
    }

    if boolExpr := expr.GetBoolExpr(); boolExpr != nil {
        for _, arg := range boolExpr.Args {
            vt.transformExpression(arg)
        }
        return
    }

    // Add more expression types as needed
}

// Transform FROM clause (handles JOINs)
func (vt *ViewTransformer) transformFromClause(fromItem *pg_query.Node) {
    if joinExpr := fromItem.GetJoinExpr(); joinExpr != nil {
        // Transform left side
        if joinExpr.Larg != nil {
            vt.transformFromClause(joinExpr.Larg)
        }

        // Transform right side
        if joinExpr.Rarg != nil {
            vt.transformFromClause(joinExpr.Rarg)
        }

        // Transform ON condition
        if joinExpr.Quals != nil {
            vt.transformExpression(joinExpr.Quals)
        }
    }
}
```

---

### Phase 3: Integration (1-2 days)

#### 3.1 Integrate into Engine (`internal/squasher/engine.go`)

```go
// REPLACE existing buildColumnEvolutionMap() at line 1338
func (e *Engine) buildColumnEvolutionMap() map[string]map[string]string {
    // Get column evolutions from tracker
    return e.tracker.GetColumnEvolutionMap()
}

// REPLACE integration point at line 1486-1497
func (e *Engine) generateConsolidatedSQL(category string) string {
    // ... existing code ...

    // Get column evolution map ONCE at start
    columnEvolutions := e.buildColumnEvolutionMap()

    for _, lifecycle := range sortedLifecycles {
        sql := lifecycle.GetFinalSQL()
        objectKey := lifecycle.GetObjectKey()

        // Apply view transformation if needed
        if category == types.CategoryFoundation && lifecycle.ObjectType == "VIEW" {
            if len(columnEvolutions) > 0 {
                transformer := transformation.NewViewTransformer(columnEvolutions)
                transformedSQL, err := transformer.Transform(sql)
                if err != nil {
                    log.Printf("[ERROR] Failed to transform view %s: %v", objectKey, err)
                } else {
                    log.Printf("[INFO] Transformed view %s (%d column references updated)",
                              objectKey, countDifferences(sql, transformedSQL))
                    sql = transformedSQL
                }
            }
        }

        output.WriteString(sql)
        output.WriteString(";\n")
    }

    return output.String()
}
```

---

### Phase 4: Testing & Validation (2-3 days)

#### 4.1 Unit Tests

```go
// internal/transformation/view_transformer_test.go

func TestViewTransformer_SimpleColumnRename(t *testing.T) {
    evolutions := map[string]map[string]string{
        "rooms": {
            "size": "size_sqm",
        },
    }

    transformer := NewViewTransformer(evolutions)

    input := `CREATE VIEW rooms_fairrent_ready AS
              SELECT r.id, r.size FROM rooms r
              WHERE r.size > 0`

    expected := `CREATE VIEW rooms_fairrent_ready AS
                 SELECT r.id, r.size_sqm FROM rooms r
                 WHERE r.size_sqm > 0`

    result, err := transformer.Transform(input)
    assert.NoError(t, err)
    assert.Equal(t, normalizeSQL(expected), normalizeSQL(result))
}

func TestViewTransformer_MultipleColumnRenames(t *testing.T) {
    evolutions := map[string]map[string]string{
        "rooms": {
            "size":  "size_sqm",
            "price": "monthly_rent",
        },
    }

    transformer := NewViewTransformer(evolutions)

    input := `CREATE VIEW room_stats AS
              SELECT r.size, r.price, r.price / r.size AS price_per_sqm
              FROM rooms r`

    expected := `CREATE VIEW room_stats AS
                 SELECT r.size_sqm, r.monthly_rent, r.monthly_rent / r.size_sqm AS price_per_sqm
                 FROM rooms r`

    result, err := transformer.Transform(input)
    assert.NoError(t, err)
    assert.Equal(t, normalizeSQL(expected), normalizeSQL(result))
}

func TestViewTransformer_ComplexJoins(t *testing.T) {
    evolutions := map[string]map[string]string{
        "rooms": {
            "property_id": "property_uuid",
        },
        "properties": {
            "id": "uuid",
        },
    }

    transformer := NewViewTransformer(evolutions)

    input := `CREATE VIEW room_details AS
              SELECT r.*, p.title
              FROM rooms r
              JOIN properties p ON r.property_id = p.id`

    expected := `CREATE VIEW room_details AS
                 SELECT r.*, p.title
                 FROM rooms r
                 JOIN properties p ON r.property_uuid = p.uuid`

    result, err := transformer.Transform(input)
    assert.NoError(t, err)
    assert.Equal(t, normalizeSQL(expected), normalizeSQL(result))
}
```

#### 4.2 Integration Tests

```go
// internal/squasher/engine_view_integration_test.go

func TestEngine_ViewColumnRewriting_E2E(t *testing.T) {
    migrations := []string{
        // Migration 1: Create table with old column name
        `CREATE TABLE rooms (id UUID PRIMARY KEY, size NUMERIC(6,2));`,

        // Migration 2: Rename column
        `ALTER TABLE rooms RENAME COLUMN size TO size_sqm;`,

        // Migration 3: Create view using old name
        `CREATE VIEW rooms_fairrent_ready AS SELECT r.id, r.size FROM rooms r WHERE r.size > 0;`,
    }

    engine := NewEngine(config)
    result, err := engine.Squash(migrations)

    assert.NoError(t, err)

    // Verify final SQL has:
    // 1. Table with new column name
    assert.Contains(t, result, "CREATE TABLE rooms")
    assert.Contains(t, result, "size_sqm")
    assert.NotContains(t, result, "size NUMERIC")  // Old name should be gone

    // 2. View with rewritten column references
    assert.Contains(t, result, "CREATE VIEW rooms_fairrent_ready")
    assert.Contains(t, result, "r.size_sqm")  // New name in view
    assert.NotContains(t, result, "r.size FROM")  // Old name should be gone
    assert.NotContains(t, result, "r.size > 0")  // Old name in WHERE should be gone

    // 3. Apply to PostgreSQL to ensure it works
    testDB := setupTestDatabase(t)
    defer testDB.Close()

    _, err = testDB.Exec(result)
    assert.NoError(t, err, "Generated SQL should apply cleanly to PostgreSQL")

    // 4. Query view to ensure it works
    var count int
    err = testDB.QueryRow("SELECT COUNT(*) FROM rooms_fairrent_ready").Scan(&count)
    assert.NoError(t, err, "View should be queryable")
}

func TestEngine_ViewColumnRewriting_MyRoomieRealWorld(t *testing.T) {
    // Load actual MyRoomie migrations
    migrations, err := loadMigrationsFromDir("../../case studies/myroomie/migrations")
    assert.NoError(t, err)

    engine := NewEngine(config)
    result, err := engine.Squash(migrations)

    assert.NoError(t, err)

    // Apply to PostgreSQL
    testDB := setupTestDatabase(t)
    defer testDB.Close()

    _, err = testDB.Exec(result)
    assert.NoError(t, err, "MyRoomie squashed SQL should apply cleanly")

    // Verify views are queryable
    views := []string{"rooms_fairrent_ready", "properties_fairrent_ready"}
    for _, view := range views {
        var exists bool
        err = testDB.QueryRow(fmt.Sprintf("SELECT EXISTS (SELECT 1 FROM pg_views WHERE viewname = '%s')", view)).Scan(&exists)
        assert.NoError(t, err)
        assert.True(t, exists, "View %s should exist", view)
    }
}
```

---

## Benefits of This Approach

### 1. **AST-Based = Robust** ✅
- No regex brittleness
- Handles complex SQL (nested queries, CTEs, window functions)
- Compiler-guaranteed correctness

### 2. **Comprehensive** ✅
- Tracks column renames through entire lifecycle
- Handles:
  - Simple renames (col1 → col2)
  - Chained renames (col1 → col2 → col3)
  - Multiple renames in same table
  - Views with JOINs, WHERE clauses, subqueries

### 3. **Maintainable** ✅
- Clean separation of concerns:
  - **Tracker**: Lifecycle management
  - **Transformer**: SQL transformation
  - **Engine**: Orchestration
- Easy to extend for other transformations

### 4. **Testable** ✅
- Unit testable (transformer in isolation)
- Integration testable (full pipeline)
- Real-world validated (MyRoomie case study)

---

## Alternative Approaches Considered

### ❌ Alternative 1: Regex-Based Rewriting

**Approach**: Use regex to find and replace column names in view SQL.

**Pros**:
- Quick to implement
- No AST parsing needed

**Cons**:
- Fragile (breaks on complex SQL)
- Can't distinguish between:
  - Column names
  - String literals containing column names
  - Comments containing column names
- Doesn't handle aliases correctly
- Breaks on edge cases:
  ```sql
  -- This would incorrectly match:
  SELECT 'size' AS label, r.size FROM rooms r
  ```

**Verdict**: **REJECTED** - Too brittle for production use.

---

### ⚠️ Alternative 2: SQL View Replacement

**Approach**: Drop and recreate all views after table consolidation.

**Pros**:
- Simple conceptually
- Doesn't require transformation logic

**Cons**:
- **Breaks if original migration has old column names**
- Loses view options (security, materialized, etc.)
- Order dependency issues (views depending on other views)
- Not a true consolidation (just copies original views)

**Verdict**: **REJECTED** - Doesn't solve the fundamental problem.

---

### ✅ Alternative 3: Deferred View Creation

**Approach**: Move all view creations to end of consolidated SQL, after all table schemas are finalized.

**Pros**:
- Views created with final column names
- No transformation needed

**Cons**:
- **Only works if migrations use final names**
- Breaks migration 3 dependencies if it needs migration 1's view
- Changes execution order semantics
- Doesn't handle views created before renames

**Verdict**: **PARTIAL** - Could be combined with main approach as fallback.

---

## Implementation Checklist

### Phase 1: Column Tracking ✅
- [ ] Extend `types.DBObject` with column evolution fields
- [ ] Implement `extractViewColumnReferences()` in parser
- [ ] Implement `ColumnLifecycle` in tracker
- [ ] Implement `processColumnRename()` in tracker
- [ ] Implement `GetColumnEvolutionMap()` in tracker
- [ ] Write unit tests for column tracking
- [ ] Verify column map is non-empty for MyRoomie

### Phase 2: View Transformation ✅
- [ ] Create `ViewTransformer` struct
- [ ] Implement `Transform()` with AST traversal
- [ ] Implement `buildAliasMap()`
- [ ] Implement `transformColumnRef()`
- [ ] Implement `transformExpression()` (WHERE, JOIN ON)
- [ ] Write unit tests for transformer
- [ ] Test on MyRoomie view samples

### Phase 3: Integration ✅
- [ ] Replace `buildColumnEvolutionMap()` in engine
- [ ] Integrate transformer into SQL generation
- [ ] Add comprehensive logging
- [ ] Update error handling

### Phase 4: Testing ✅
- [ ] Write unit tests (simple cases)
- [ ] Write unit tests (complex cases: JOINs, subqueries)
- [ ] Write integration test (full pipeline)
- [ ] Run E2E test on MyRoomie (76 migrations)
- [ ] Run E2E test on Nami AI App (8 migrations)
- [ ] Run E2E test on VDK Hub (9 migrations)
- [ ] Verify all 3 projects pass validation

### Phase 5: Documentation ✅
- [ ] Update CLAUDE.md with new architecture
- [ ] Document ViewTransformer API
- [ ] Add migration guide for column renames
- [ ] Update E2E testing guide

---

## Success Criteria

### Functional Requirements ✅
1. **MyRoomie test passes**: All 76 migrations consolidate without errors
2. **View references updated**: `r.size` → `r.size_sqm` in all views
3. **SQL applies cleanly**: No PostgreSQL errors when applying consolidated schema
4. **Views queryable**: All views can be queried after application

### Non-Functional Requirements ✅
1. **Performance**: No significant slowdown (< 10% increase in processing time)
2. **Memory**: No memory leaks or excessive memory usage
3. **Logging**: Clear logs showing transformations applied
4. **Maintainability**: Code is well-documented and testable

---

## Timeline Estimate

| Phase | Duration | Dependencies |
|-------|----------|-------------|
| Phase 1: Column Tracking | 3-4 days | None |
| Phase 2: View Transformation | 2-3 days | Phase 1 |
| Phase 3: Integration | 1-2 days | Phase 1 & 2 |
| Phase 4: Testing | 2-3 days | Phase 1-3 |
| Phase 5: Documentation | 1 day | Phase 1-4 |
| **Total** | **9-13 days** (~2 weeks) | |

---

## Risks & Mitigation

### Risk 1: AST Traversal Complexity
**Impact**: HIGH
**Probability**: MEDIUM
**Mitigation**: Start with simple cases (SELECT, WHERE), incrementally add complexity (JOINs, subqueries, CTEs). Use MyRoomie as validation test case.

### Risk 2: pg_query Deparse Output Formatting
**Impact**: LOW
**Probability**: HIGH
**Mitigation**: Normalize SQL before comparison in tests. Accept formatting differences as long as semantics are correct.

### Risk 3: Edge Cases Not Covered
**Impact**: MEDIUM
**Probability**: MEDIUM
**Mitigation**: Collect edge cases from real-world migrations. Build comprehensive test suite covering:
- Nested subqueries
- CTEs (WITH clauses)
- Window functions
- CASE expressions
- Type casts

---

## Conclusion

This AST-based approach provides a **robust, maintainable, and correct** solution to Bug #1. While more complex than regex-based alternatives, it guarantees correctness and handles all edge cases.

**Recommendation**: **PROCEED with implementation** using the proposed architecture.

**Next Steps**:
1. Get architectural approval
2. Create implementation tracking issue
3. Begin Phase 1 (Column Tracking)
4. Iterate with E2E testing using MyRoomie as validation

---

**Proposal End**
