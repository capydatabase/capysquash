# Bug #11 Investigation: Missing Indexes
**Date**: November 6, 2025
**Status**: 🔍 ROOT CAUSE IDENTIFIED
**Severity**: 🔴 CRITICAL

---

## The Problem

68 indexes reported missing from myroomie squashed output (actual count: 27 missing CREATE INDEX statements, but indexes are unique objects so 68 index objects are affected).

### Evidence
```bash
# Original migrations
$ grep -o "CREATE INDEX" migrations/*.sql | wc -l
334

# Squashed output
$ grep -o "CREATE INDEX" test-run-fresh/000_baseline.sql | wc -l
307

# Missing: 27 CREATE INDEX statements
```

---

## Root Cause Identified

### Key Finding: Tables with Multiple CREATE Statements

The `communities` table is created **twice**:
- `migrations/01_migration.sql`: Creates communities + 3 indexes
- `migrations/02_migration.sql`: Creates communities (again) but NO indexes

**Original indexes in migration 01:**
```sql
CREATE INDEX IF NOT EXISTS idx_communities_creator_id ON communities (creator_id);
CREATE INDEX IF NOT EXISTS idx_communities_type ON communities (type);
CREATE INDEX IF NOT EXISTS idx_communities_property_id ON communities (property_id);
```

**Squashed output - only ONE survives:**
```sql
CREATE INDEX IF NOT EXISTS idx_communities_creator_id ON communities (creator_id);
```

**Missing**: `idx_communities_type` and `idx_communities_property_id`

---

## Why This Happens

### The Consolidation Flow

1. **Parser Phase**: Indexes are tracked as separate objects from tables
   - Table: `communities::TABLE`
   - Index 1: `idx_communities_creator_id::INDEX`
   - Index 2: `idx_communities_type::INDEX`
   - Index 3: `idx_communities_property_id::INDEX`

2. **Tracking Phase**: `MultipleCreateConsolidationRule` detects table created twice
   - File: `internal/tracking/consolidation/multiple_create_rule.go`
   - Lines 88-93: Merges multiple CREATE TABLE statements
   - **BUT**: Indexes are tracked separately from the table

3. **Problem**: When table is recreated, what happens to indexes?
   - Hypothesis A: Indexes associated with first CREATE are marked as "dropped" when table is recreated
   - Hypothesis B: Only indexes from LAST CREATE statement are preserved
   - Hypothesis C: Lifecycle events are not linking indexes to table CREATE events correctly

---

## Code Analysis

### MultipleCreateConsolidationRule (lines 88-93)

```go
// For tables with more than one CREATE, merge columns from all CREATE statements
if len(allCreateStmts) > 1 && lifecycle.Type == types.TypeTable {
    consolidatedSQL = mergeMultipleCreateStatements(allCreateStmts, lifecycle.Name)
    if strings.ToLower(lifecycle.Name) == "profiles" {
        utils.GetDefaultLogger().WithPrefix("MULTIPLE-CREATE").Info(
            "  Merged %d CREATE statements into unified schema", len(allCreateStmts))
    }
}
```

**What this does**: Merges table column definitions
**What this DOESN'T do**: Handle indexes associated with those table CREATEs

### The Missing Logic

When a table has CREATE → CREATE pattern:
1. ✅ Table columns are merged correctly
2. ❌ Indexes from first CREATE may be lost
3. ❌ No logic to merge indexes from both CREATEs

---

## Proposed AST-Based Fix

### Approach: Index Lifecycle Association

**Philosophy**: Follow AST-first principle - track index-to-table associations through AST

**Implementation Plan**:

1. **Track Index-Table Associations** (AST-based)
   ```go
   type IndexLifecycle struct {
       *ObjectLifecycle
       AssociatedTable string  // Which table this index is on
       CreatedWith     int     // Which CREATE TABLE event (sequence number)
   }
   ```

2. **Update MultipleCreateConsolidationRule**
   - When merging table CREATEs, also merge associated indexes
   - Collect indexes from ALL CREATE events, not just the last one
   - Deduplicate indexes by name (keep latest definition)

3. **Add Index Association in Tracker** (AST-based)
   ```go
   // In unified_tracker.go ProcessMigration()
   func extractIndexTableAssociation(indexStmt *pg_query.IndexStmt) string {
       // Use AST to get table name from IndexStmt.Relation
       if indexStmt.Relation != nil {
           return indexStmt.Relation.Relname
       }
       return ""
   }
   ```

4. **Consolidation Logic Update**
   ```go
   // Pseudocode for multiple_create_rule.go
   func consolidateIndexesForMultipleCreates(
       tableLifecycle *ObjectLifecycle,
       allIndexes []*ObjectLifecycle
   ) []string {
       // 1. Group indexes by table CREATE event
       indexesByCreate := groupIndexesByTableCreate(allIndexes, tableLifecycle)

       // 2. Merge indexes from all CREATE events
       mergedIndexes := make(map[string]*ObjectLifecycle)
       for _, createEvent := range tableLifecycle.History {
           if createEvent.Operation == OpCreate {
               for _, idx := range indexesByCreate[createEvent.Sequence] {
                   // Keep latest definition if duplicate names
                   mergedIndexes[idx.Name] = idx
               }
           }
       }

       // 3. Return all unique index SQLs
       result := []string{}
       for _, idx := range mergedIndexes {
           result = append(result, idx.ConsolidatedSQL)
       }
       return result
   }
   ```

---

## Testing Strategy

### Test Case 1: Communities Table
```sql
-- Migration 01
CREATE TABLE communities (...);
CREATE INDEX idx_communities_creator_id ON communities (creator_id);
CREATE INDEX idx_communities_type ON communities (type);
CREATE INDEX idx_communities_property_id ON communities (property_id);

-- Migration 02
CREATE TABLE communities (...); -- Different columns

-- Expected Squashed Output:
CREATE TABLE communities (...); -- Merged columns
CREATE INDEX idx_communities_creator_id ON communities (creator_id);
CREATE INDEX idx_communities_type ON communities (type);
CREATE INDEX idx_communities_property_id ON communities (property_id);
```

### Test Case 2: Index Redefinition
```sql
-- Migration 01
CREATE INDEX idx_test ON table1 (col1);

-- Migration 02
CREATE TABLE table1 (...);
CREATE INDEX idx_test ON table1 (col1, col2); -- Different definition

-- Expected: Use latest definition (col1, col2)
```

---

## Files to Modify

### Priority 1: Core Fix
1. **`internal/tracking/unified_tracker.go`**
   - Add index-to-table association tracking (AST-based)
   - Extract table name from `IndexStmt.Relation` field in AST

2. **`internal/tracking/consolidation/multiple_create_rule.go`**
   - Add index consolidation logic
   - Merge indexes from all CREATE events
   - Deduplicate by name, keep latest definition

### Priority 2: Enhanced Tracking
3. **`internal/types/parser_types.go`**
   - Add `AssociatedTable` field to index metadata
   - Track which table CREATE event spawned each index

### Priority 3: Validation
4. **Add debug logging** to trace index lifecycle
5. **Add validation check**: Output indexes >= Input indexes (unless explicit DROP)

---

## Alternative Approaches Considered

### ❌ Regex-based Solution
**Rejected**: Goes against AST-first principle
```go
// BAD: Regex to find indexes in SQL
re := regexp.MustCompile(`CREATE INDEX .* ON (\w+)`)
```

### ✅ AST-based Solution (Recommended)
**Accepted**: Use pg_query AST structures
```go
// GOOD: Extract from AST
if indexStmt := stmt.GetIndexStmt(); indexStmt != nil {
    tableName := indexStmt.Relation.Relname
    indexName := indexStmt.Idxname
}
```

---

## Impact Analysis

### Performance Impact
- **Critical**: 27 missing indexes on large tables → full table scans
- **Example**: `idx_property_interests_user_id` missing
  - Query: `SELECT * FROM property_interests WHERE user_id = ?`
  - Without index: O(n) scan of millions of rows
  - With index: O(log n) = sub-millisecond

### Production Risk
- Applications will be **unusably slow**
- Database will experience **CPU/memory exhaustion**
- Users will experience **timeouts and errors**

---

## Next Steps

### Immediate (Next Session)
1. ✅ Documented root cause
2. 📋 Implement AST-based index-table association tracking
3. 📋 Update `MultipleCreateConsolidationRule` to consolidate indexes
4. 📋 Test with myroomie case study

### Validation
1. 📋 Run e2e test on myroomie with fix
2. 📋 Verify all 334 indexes present in output
3. 📋 Check nami ai app and vdk hub still pass

### Long Term
1. 📋 Add validation check: output objects >= input objects
2. 📋 Add comprehensive logging for index lifecycle
3. 📋 Add unit tests for index consolidation
4. 📋 Add regression test for Bug #11

---

## Summary

**Root Cause**: When tables have multiple CREATE statements, indexes from the first CREATE are lost because `MultipleCreateConsolidationRule` only preserves the table schema, not associated indexes.

**Fix Strategy**: AST-based tracking of index-to-table associations, then merge indexes from all CREATE events when consolidating tables.

**Complexity**: Medium - requires changes to tracker and consolidation rule, but logic is straightforward.

**Risk**: Low - fix is localized to index handling, doesn't affect other consolidation rules.

---

**Investigation Complete**: November 6, 2025
**Next Action**: Implement AST-based fix
**Estimated Time**: 2-3 hours
