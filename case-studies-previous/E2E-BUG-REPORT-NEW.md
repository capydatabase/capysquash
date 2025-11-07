# E2E Testing Bug Report - pgsquash-engine
**Date**: 2025-11-06
**Tester**: Claude Code (Autonomous E2E Testing)
**Version**: Current codebase (post Bug #2, #4, #5 fixes)

---

## Executive Summary

Tested pgsquash-engine on 3 real-world projects with standard safety level:

| Project | Migrations | Result | Critical Issues |
|---------|-----------|--------|----------------|
| **MyRoomie** | 76 | ❌ **FAILED** | Bug #6: Incorrect GiST index on array columns |
| **Nami AI App** | 8 | ⚠️ **PARTIAL** | 6 function differences (schema drift) |
| **VDK Hub** | 9 | ✅ **PASSED** | None |

**Overall Status**: **1/3 projects have critical failures**, **1/3 have schema drift issues**

---

## Critical Bug Found: Bug #6 - Incorrect Index Type Inference

### Problem

The consolidation engine incorrectly adds `USING gist` to indexes on array columns, causing validation failures.

**Original Migration (01_migration.sql:2492)**:
```sql
CREATE INDEX IF NOT EXISTS idx_properties_coordinates ON properties (coordinates);
```

**Column Definition**:
```sql
CREATE TABLE properties (
  ...
  coordinates FLOAT[],  -- double precision[]
  ...
);
```

**Consolidated Output**:
```sql
CREATE INDEX IF NOT EXISTS idx_properties_coordinates ON properties USING gist (coordinates);
```

**PostgreSQL Error**:
```
pq: data type double precision[] has no default operator class for access method "gist"
```

### Root Cause

**Location**: `internal/squasher/engine.go:1710-1756`

The "safety net" logic has a three-step process:

1. **Step 1 (lines 1712-1718)**: Finds indexes on columns with spatial-sounding names (coordinates, location, geom, etc.) that have `USING btree` and replaces it with `USING gist`

2. **Step 2 (lines 1720-1728)**: Finds indexes on spatial-sounding columns with NO USING clause and adds `USING gist`

3. **Step 3 (lines 1730-1756)**: Attempts to fix the damage by detecting array columns and removing `USING gist`

**The Bug**: Step 3's array detection regex is broken (line 1736):

```go
arrayColumnPattern := regexp.MustCompile(`(?i)CREATE\s+TABLE[^;]+?(\w+)\s+(double\s+precision\[\]|float\[\]|integer\[\]|text\[\])`)
```

This pattern fails because:
- It's too greedy with `CREATE\s+TABLE[^;]+?`
- It doesn't properly capture column names from complex table definitions
- The column type is `FLOAT[]` (uppercase) but pattern expects `float\[\]`
- It doesn't handle multi-line table definitions or complex formatting

### Impact

- **Severity**: CRITICAL 🔴
- **Projects Affected**: MyRoomie (2 indexes fail), potentially others with array columns named with spatial terms
- **Deployment**: Completely breaks schema application
- **Production Readiness**: BLOCKING

### Test Evidence

**MyRoomie Project**:
```
❌ Validation failed: [ERROR:VALIDATION] code:INVALID_SQL
failed to execute statement 582 in migration
case studies/myroomie/e2e-new-test/standard/000_baseline.sql

PostgreSQL error: pq: data type double precision[] has no default
operator class for access method "gist"

Statement:
CREATE INDEX IF NOT EXISTS idx_properties_coordinates ON properties USING gist (coordinates);
```

Similarly fails for `idx_profiles_coordinates`.

---

## Architectural Solution for Bug #6

### Why Regex Fails

The current approach uses regex to:
1. Guess which columns might be spatial based on name patterns
2. Guess which columns are arrays by parsing CREATE TABLE statements
3. Apply and then undo changes based on these guesses

This is fundamentally flawed because:
- Column names don't determine column types
- Regex cannot reliably parse complex SQL syntax
- The approach is reactive (apply then fix) rather than proactive

### AST-Based Solution

**Principle**: Use the tracker's AST-based knowledge of actual column types.

**Implementation Strategy**:

#### Phase 1: Track Column Types During Processing

During the `Tracker.ProcessMigration()` phase, we already parse CREATE TABLE statements with pg_query_go. We should:

1. Build a map of column names to their actual PostgreSQL types:
```go
type ColumnTypeInfo struct {
    TableName  string
    ColumnName string
    DataType   string  // "point", "geography", "geometry", "double precision[]", etc.
    IsArray    bool
    IsSpatial  bool    // Actual spatial types: point, geography, geometry
}

// In Tracker:
columnTypes map[string]map[string]*ColumnTypeInfo  // table -> column -> info
```

2. When processing CREATE TABLE statements, extract column type information from the AST:
```go
func (t *Tracker) extractColumnTypes(stmt *pg_query.CreateStmt) {
    tableName := stmt.Relation.Relname
    for _, element := range stmt.TableElts {
        if colDef := element.GetColumnDef(); colDef != nil {
            columnName := colDef.Colname
            dataType := parseTypeName(colDef.TypeName)

            t.columnTypes[tableName][columnName] = &ColumnTypeInfo{
                TableName:  tableName,
                ColumnName: columnName,
                DataType:   dataType,
                IsArray:    isArrayType(colDef.TypeName),
                IsSpatial:  isSpatialType(colDef.TypeName),
            }
        }
    }
}
```

3. Utility functions:
```go
func isSpatialType(typeName *pg_query.TypeName) bool {
    // Check if type is actual spatial type: point, geography, geometry
    // These come from PostGIS or PostgreSQL built-ins
    typeStr := getTypeName(typeName)
    return typeStr == "point" ||
           typeStr == "geography" ||
           typeStr == "geometry" ||
           typeStr == "line" ||
           typeStr == "lseg" ||
           typeStr == "box" ||
           typeStr == "path" ||
           typeStr == "polygon" ||
           typeStr == "circle"
}

func isArrayType(typeName *pg_query.TypeName) bool {
    return typeName.ArrayBounds != nil && len(typeName.ArrayBounds) > 0
}
```

#### Phase 2: Use Type Information During Index Processing

When processing CREATE INDEX statements, check actual column types:

```go
func (e *Engine) processIndex(indexStmt *pg_query.IndexStmt) string {
    tableName := indexStmt.Relation.Relname

    // Check what columns this index is on
    for _, indexElem := range indexStmt.IndexParams {
        if indexElem.Name != "" {
            colInfo := e.tracker.GetColumnType(tableName, indexElem.Name)

            if colInfo != nil {
                if colInfo.IsSpatial {
                    // Actual spatial type - GiST is appropriate
                    if indexStmt.AccessMethod == "" || indexStmt.AccessMethod == "btree" {
                        indexStmt.AccessMethod = "gist"
                    }
                } else if colInfo.IsArray && !colInfo.IsSpatial {
                    // Array type but not spatial - remove GiST
                    if indexStmt.AccessMethod == "gist" {
                        indexStmt.AccessMethod = ""  // Default to btree
                    }
                }
            }
        }
    }

    return deparsedIndexStmt(indexStmt)
}
```

#### Phase 3: Remove Regex "Safety Net"

Delete lines 1710-1756 in `engine.go` entirely. They are:
- Unreliable (regex-based)
- Reactive (fix after breaking)
- Redundant (replaced by AST-based approach)

### Benefits of AST Solution

1. **Accurate**: Uses actual PostgreSQL types, not name guessing
2. **Proactive**: Prevents errors rather than trying to fix them
3. **Maintainable**: One source of truth (tracker's type info)
4. **Extensible**: Easy to add support for new spatial types or array handling
5. **No False Positives**: Won't incorrectly add/remove index types

### Implementation Files

**Files to Modify**:
1. `internal/tracking/tracker.go` - Add column type tracking
2. `internal/tracking/types.go` - Add ColumnTypeInfo struct
3. `internal/squasher/engine.go` - Remove regex safety net, use tracker types
4. `internal/builder/sql.go` - Use column type info when building indexes

**Estimated Effort**: 1-2 days

---

## Bug #3: Schema Drift (Function Differences)

### Problem

Nami AI App validation reports 6 function differences between original and squashed migrations:

```
❌ Schema differences detected:

1. Functions differs: public.set_session_user
2. Functions differs: public.get_planning_analytics
3. Functions differs: public.current_clerk_user_id
4. Functions differs: public.validate_jwt_version
5. Functions differs: public.current_clerk_org_id
6. Functions differs: public.current_clerk_org_role
```

### Status

⚠️ **NEEDS INVESTIGATION**

These differences could be:
1. **Formatting differences**: Whitespace, newlines, keyword case
2. **Volatility markers**: STABLE vs VOLATILE (functionally equivalent)
3. **Actual semantic differences**: Real bugs in consolidation

### Next Steps

1. Extract exact SQL for these 6 functions from both original and squashed
2. Normalize SQL (remove whitespace, standardize case)
3. If identical after normalization → Enhance schema comparison
4. If different → Identify consolidation bug

**Estimated Investigation Time**: 2-3 hours

---

## Test Results Summary

### ✅ VDK Hub - PASSED

- **9 migrations**, standard safety level
- **482 lines** of output SQL
- **Processing time**: 256ms
- **Validation**: ✅ Passed - schemas are identical
- **Extensions**: pg_trgm
- **Auth**: Supabase

**Conclusion**: Works perfectly for Supabase auth + standard PostgreSQL

### ⚠️ Nami AI App - PARTIAL

- **8 migrations**, standard safety level
- **666 lines** of output SQL
- **Processing time**: 199ms
- **Validation**: ⚠️ No syntax errors, but 6 function differences
- **Extensions**: pg_trgm, uuid-ossp
- **Auth**: Clerk JWT v2

**Conclusion**: Consolidation works, but schema comparison may be too sensitive or there are real semantic differences

### ❌ MyRoomie - FAILED

- **76 migrations**, standard safety level
- **~5000+ lines** of output SQL (estimated)
- **Processing time**: N/A (failed validation)
- **Validation**: ❌ Failed at statement 582 (idx_properties_coordinates)
- **Extensions**: btree_gin, cube, earthdistance, pg_stat_statements, pg_trgm, postgis
- **Auth**: Clerk + Supabase

**Conclusion**: Bug #6 (incorrect GiST on arrays) is blocking. Once fixed, may also hit Bug #1 (view column references) if it still exists.

---

## Recommendations

### Priority 0 (Blocking) - MUST FIX IMMEDIATELY

#### 1. Fix Bug #6: Incorrect Index Type Inference ⏱️ Est: 1-2 days

**Approach**: Implement AST-based column type tracking as described above

**Acceptance Criteria**:
- MyRoomie test passes validation
- No false-positive GiST index additions
- Column types tracked accurately via AST
- Regex "safety net" removed

**Files**:
- `internal/tracking/tracker.go` (add column type tracking)
- `internal/tracking/types.go` (add ColumnTypeInfo struct)
- `internal/squasher/engine.go` (remove regex, use tracker)
- `internal/builder/sql.go` (use column types when building)

### Priority 1 (High) - SHOULD INVESTIGATE

#### 2. Investigate Bug #3: Nami AI App Function Differences ⏱️ Est: 2-3 hours

**Approach**:
1. Extract exact SQL for the 6 differing functions
2. Normalize and compare
3. Determine if differences are real or validation artifacts

**Acceptance Criteria**:
- Clear understanding of whether differences are bugs or acceptable
- If bugs: root cause identified and documented
- If acceptable: document why and enhance validation output

### Priority 2 (Medium) - AFTER BUG #6 FIX

#### 3. Re-test MyRoomie for Bug #1 (View Column References) ⏱️ Est: 1 hour test + potential fix

Once Bug #6 is fixed, re-run MyRoomie test to see if Bug #1 (view column references not updated) still exists.

**E2E Report claimed Bug #1 was not fixed despite code being present**

---

## Code Review: Current "Safety Net" (Lines 1710-1756)

### What It's Trying To Do

```go
// Step 1: Find spatial-named columns with btree, change to gist
spatialIndexWithBtree := regexp.MustCompile(`(?i)(CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?[\w.]+\s+ON\s+[\w.]+)\s+USING\s+btree\s+(\((?:coordinates|location|geom|geography|geometry|point|position|lat_long|lat_lon|latlng|geo_point)\b[^)]*\))`)
finalSQL = spatialIndexWithBtree.ReplaceAllString(finalSQL, "$1 USING gist $2")

// Step 2: Find spatial-named columns without USING, add gist
spatialIndexNoMethod := regexp.MustCompile(`(?i)(CREATE\s+(?:UNIQUE\s+)?INDEX\s+(?:IF\s+NOT\s+EXISTS\s+)?[\w.]+\s+ON\s+[\w.]+)\s+(\((?:coordinates|location|geom|geography|geometry|point|position|lat_long|lat_lon|latlng|geo_point)\b[^)]*\)\s*(?:WHERE|;))`)
finalSQL = spatialIndexNoMethod.ReplaceAllString(finalSQL, "$1 USING gist $2")

// Step 3: Try to fix damage by detecting arrays
arrayColumnPattern := regexp.MustCompile(`(?i)CREATE\s+TABLE[^;]+?(\w+)\s+(double\s+precision\[\]|float\[\]|integer\[\]|text\[\])`)
// ... extract array columns ...
// ... remove USING gist from those columns ...
```

### Why It Fails

1. **Name-based type guessing**: "coordinates" doesn't mean it's a spatial type
2. **Broken fix attempt**: Regex can't reliably parse CREATE TABLE statements
3. **Reactive approach**: Break first, fix later (instead of doing it right the first time)
4. **Case sensitivity**: `FLOAT[]` doesn't match `float\[\]`
5. **Complex SQL**: Can't handle multi-line definitions, comments, nested structures

---

## Production Readiness Assessment

**Current Status**: **NOT PRODUCTION-READY** for all use cases

**Working**:
- ✅ Simple projects without spatial indexes or array columns (VDK Hub)
- ✅ Function language/body corrections (Bug #2: FIXED in previous work)
- ✅ Supabase auth detection and compatibility
- ✅ Clerk JWT v2 detection and compatibility

**Blocking**:
- ❌ Projects with array columns named with spatial terms (Bug #6: CRITICAL)
- ⚠️ Projects with schema drift detection (Bug #3: Needs investigation)

**Estimated Time to Production-Ready**:
- **Minimum**: 1-2 days (fix Bug #6 with AST solution)
- **Recommended**: 3-4 days (fix Bug #6 + investigate Bug #3 + re-test)
- **Optimal**: 1 week (all above + add regression tests + verify Bug #1 status)

---

## Test Execution Details

**Environment**:
- Go version: 1.25.4
- PostgreSQL target: 15
- Platform: darwin/arm64 (macOS)
- Docker: postgres:15 image for validation

**Test Commands**:
```bash
./pgsquash squash "./case studies/myroomie/migrations"/*.sql \
  --safety standard \
  --output "./case studies/myroomie/e2e-new-test/standard"

./pgsquash squash "./case studies/nami ai app/migrations"/*.sql \
  --safety standard \
  --output "./case studies/nami ai app/e2e-new-test/standard"

./pgsquash squash "./case studies/vdk hub/migrations"/*.sql \
  --safety standard \
  --output "./case studies/vdk hub/e2e-new-test/standard"
```

**Validation Mode**: TWO_DATABASES (two databases in single container)

**Test Duration**: ~3 minutes total for all 3 projects

---

**Report End**
