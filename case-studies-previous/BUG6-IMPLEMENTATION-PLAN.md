# Bug #6 Implementation Plan: AST-Based Index Type Inference

## Overview

Replace the broken regex-based "safety net" for spatial index detection with proper AST-based column type tracking.

## Files Modified

1. ✅ `internal/tracking/unified_tracker.go` - Added column type tracking infrastructure
2. ⏳ `internal/tracking/unified_tracker.go` - Add extraction methods (see below)
3. ⏳ `internal/squasher/engine.go` - Remove regex safety net, use column types
4. ⏳ `internal/squasher/engine_test.go` - Add tests

## Phase 1: Column Type Extraction (COMPLETED PARTIALLY)

### ✅ Step 1.1: Add Data Structures

**File**: `internal/tracking/unified_tracker.go`

```go
// ColumnTypeInfo tracks column types for index optimization
type ColumnTypeInfo struct {
    TableName  string
    ColumnName string
    DataType   string // Full type name (e.g., "double precision[]", "point", "geometry")
    IsArray    bool   // True if column is an array type
    IsSpatial  bool   // True if column is an actual spatial type
}

// In UnifiedTracker struct:
columnTypes map[string]map[string]*ColumnTypeInfo // table -> column -> type info
```

Status: ✅ COMPLETED

### ⏳ Step 1.2: Add Helper Functions

**File**: `internal/tracking/unified_tracker.go`

Add after the constructor methods (around line 670):

```go
// IsSpatialDataType determines if a PostgreSQL type is a spatial/geometric type
func IsSpatialDataType(typeName string) bool {
    typeName = strings.ToLower(strings.TrimSpace(typeName))

    // PostGIS types
    if strings.HasPrefix(typeName, "geometry") ||
        strings.HasPrefix(typeName, "geography") {
        return true
    }

    // PostgreSQL built-in geometric types
    spatialTypes := map[string]bool{
        "point":   true,
        "line":    true,
        "lseg":    true,
        "box":     true,
        "path":    true,
        "polygon": true,
        "circle":  true,
    }

    return spatialTypes[typeName]
}

// IsArrayDataType determines if a type is an array
func IsArrayDataType(typeName string) bool {
    return strings.HasSuffix(typeName, "[]")
}

// GetBaseTypeName extracts the base type from an array type
// E.g., "double precision[]" -> "double precision"
func GetBaseTypeName(typeName string) string {
    return strings.TrimSuffix(typeName, "[]")
}
```

### ⏳ Step 1.3: Add Column Type Extraction Method

**File**: `internal/tracking/unified_tracker.go`

Add after the helper functions (around line 700):

```go
// ExtractColumnTypes extracts column type information from a CREATE TABLE statement
func (t *UnifiedTracker) ExtractColumnTypes(tableName string, stmt *types.Statement) {
    if stmt.ParsedStmt == nil || stmt.ParsedStmt.Stmts == nil || len(stmt.ParsedStmt.Stmts) == 0 {
        return
    }

    // Get the first statement
    stmtNode := stmt.ParsedStmt.Stmts[0]
    if stmtNode.Stmt == nil {
        return
    }

    // Check if it's a CREATE TABLE statement
    createStmt := stmtNode.Stmt.GetCreateStmt()
    if createStmt == nil {
        return
    }

    // Ensure the table entry exists in columnTypes
    if t.columnTypes[tableName] == nil {
        t.columnTypes[tableName] = make(map[string]*ColumnTypeInfo)
    }

    // Iterate through table elements to find column definitions
    for _, element := range createStmt.TableElts {
        colDef := element.GetColumnDef()
        if colDef == nil {
            continue
        }

        columnName := colDef.Colname
        if columnName == "" {
            continue
        }

        // Extract type name
        if colDef.TypeName == nil {
            continue
        }

        typeName := extractTypeName(colDef.TypeName)
        isArray := colDef.TypeName.ArrayBounds != nil && len(colDef.TypeName.ArrayBounds) > 0

        // Check if it's a spatial type (but not an array of spatial types)
        baseTypeName := typeName
        if isArray {
            baseTypeName = GetBaseTypeName(typeName)
        }
        isSpatial := IsSpatialDataType(baseTypeName) && !isArray

        // Store column type info
        t.columnTypes[tableName][columnName] = &ColumnTypeInfo{
            TableName:  tableName,
            ColumnName: columnName,
            DataType:   typeName,
            IsArray:    isArray,
            IsSpatial:  isSpatial,
        }
    }
}

// extractTypeName converts a pg_query TypeName to a string representation
func extractTypeName(typeName *pg_query.TypeName) string {
    if typeName == nil {
        return ""
    }

    // Get the type names array
    var typeNames []string
    for _, name := range typeName.Names {
        if name.GetString_() != nil {
            typeNames = append(typeNames, name.GetString_().Sval)
        }
    }

    // Join type names (e.g., ["pg_catalog", "float8"] -> "float8")
    // Use the last element for simplicity, or join with "."
    var typStr string
    if len(typeNames) > 0 {
        typStr = typeNames[len(typeNames)-1]
    }

    // Handle common PostgreSQL type aliases
    typeMap := map[string]string{
        "float8":  "double precision",
        "float4":  "real",
        "int4":    "integer",
        "int8":    "bigint",
        "int2":    "smallint",
        "varchar": "character varying",
    }

    if mapped, ok := typeMap[typStr]; ok {
        typStr = mapped
    }

    // Add array suffix if necessary
    if typeName.ArrayBounds != nil && len(typeName.ArrayBounds) > 0 {
        typStr += "[]"
    }

    return typStr
}

// GetColumnType retrieves column type information
func (t *UnifiedTracker) GetColumnType(tableName, columnName string) *ColumnTypeInfo {
    if t.columnTypes[tableName] == nil {
        return nil
    }
    return t.columnTypes[tableName][columnName]
}
```

### ⏳ Step 1.4: Integrate Column Type Extraction in ProcessMigration

**File**: `internal/tracking/unified_tracker.go`

Find the `ProcessMigration` method (around line 700-900) and add column type extraction for CREATE TABLE operations.

Look for where CREATE_TABLE operations are handled and add:

```go
case types.OperationCreateTable:
    // Existing code for CREATE_TABLE...

    // NEW: Extract column types for index optimization
    t.ExtractColumnTypes(event.Statement.ObjectName, &event.Statement)
```

## Phase 2: Use Column Types in Engine (NOT STARTED)

### ⏳ Step 2.1: Remove Broken Regex Safety Net

**File**: `internal/squasher/engine.go`

**DELETE** lines 1710-1756 (the entire safety net block):

```go
// Step 1: Remove incorrect "USING btree" from spatial indexes
// Step 2: Add "USING gist" to spatial indexes that have no access method
// Step 3: Remove USING gist from array columns
// ... DELETE ALL OF THIS ...
```

### ⏳ Step 2.2: Add Index Type Optimization Method

**File**: `internal/squasher/engine.go`

Add a new method after the `Squash` method (around line 1700):

```go
// OptimizeIndexTypes uses column type information to set appropriate index types
func (e *Engine) OptimizeIndexTypes(sql string) string {
    if e.tracker == nil || e.tracker.columnTypes == nil {
        return sql
    }

    // Parse the SQL to find index statements
    parseResult, err := pg_query.Parse(sql)
    if err != nil {
        e.logger.Warn("Failed to parse SQL for index optimization: %v", err)
        return sql
    }

    modified := false
    for i, stmt := range parseResult.Stmts {
        indexStmt := stmt.Stmt.GetIndexStmt()
        if indexStmt == nil {
            continue
        }

        tableName := indexStmt.Relation.Relname
        if tableName == "" {
            continue
        }

        // Check each column in the index
        for _, indexElem := range indexStmt.IndexParams {
            columnName := ""
            if indexElem.Name != "" {
                columnName = indexElem.Name
            } else if indexElem.Expr != nil {
                // Handle expression indexes - skip optimization
                continue
            }

            if columnName == "" {
                continue
            }

            // Get column type info from tracker
            colInfo := e.tracker.GetColumnType(tableName, columnName)
            if colInfo == nil {
                continue
            }

            // Apply optimization rules
            currentMethod := indexStmt.AccessMethod

            if colInfo.IsSpatial {
                // Actual spatial type - GiST is appropriate
                if currentMethod == "" || currentMethod == "btree" {
                    e.logger.Info("Index optimization: Setting USING gist for spatial column %s.%s", tableName, columnName)
                    indexStmt.AccessMethod = "gist"
                    modified = true
                }
            } else if colInfo.IsArray {
                // Array type but not spatial - remove GiST, use default (btree)
                if currentMethod == "gist" {
                    e.logger.Info("Index optimization: Removing USING gist from array column %s.%s (using btree)", tableName, columnName)
                    indexStmt.AccessMethod = ""
                    modified = true
                }
            }
        }

        // Update the statement if modified
        if modified {
            parseResult.Stmts[i] = stmt
        }
    }

    if !modified {
        return sql
    }

    // Deparse back to SQL
    deparsed, err := pg_query.Deparse(parseResult)
    if err != nil {
        e.logger.Warn("Failed to deparse optimized SQL: %v", err)
        return sql
    }

    return deparsed
}
```

### ⏳ Step 2.3: Call Optimization in Squash Method

**File**: `internal/squasher/engine.go`

Find where `finalSQL` is being processed (after line 1700, before return) and add:

```go
// Apply index type optimization based on actual column types (AST-based)
finalSQL = e.OptimizeIndexTypes(finalSQL)
```

Remove the call to the old regex-based safety net (if any remaining).

## Phase 3: Testing (NOT STARTED)

### ⏳ Step 3.1: Add Unit Tests

**File**: `internal/tracking/unified_tracker_test.go`

```go
func TestColumnTypeTracking(t *testing.T) {
    tests := []struct {
        name      string
        createSQL string
        tableName string
        checks    map[string]struct {
            dataType  string
            isArray   bool
            isSpatial bool
        }
    }{
        {
            name: "Array column with spatial-sounding name",
            createSQL: `CREATE TABLE properties (
                coordinates FLOAT[],
                location VARCHAR(100)
            );`,
            tableName: "properties",
            checks: map[string]struct {
                dataType  string
                isArray   bool
                isSpatial bool
            }{
                "coordinates": {"double precision[]", true, false},
                "location":    {"character varying", false, false},
            },
        },
        {
            name: "Actual spatial types",
            createSQL: `CREATE TABLE locations (
                geo_point POINT,
                area GEOMETRY(POLYGON, 4326)
            );`,
            tableName: "locations",
            checks: map[string]struct {
                dataType  string
                isArray   bool
                isSpatial bool
            }{
                "geo_point": {"point", false, true},
                "area":      {"geometry", false, true},
            },
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            tracker := NewUnifiedTracker()

            // Parse and process the CREATE TABLE statement
            // ... implementation ...

            // Verify column types
            for colName, expected := range tt.checks {
                colInfo := tracker.GetColumnType(tt.tableName, colName)
                assert.NotNil(t, colInfo)
                assert.Equal(t, expected.dataType, colInfo.DataType)
                assert.Equal(t, expected.isArray, colInfo.IsArray)
                assert.Equal(t, expected.isSpatial, colInfo.IsSpatial)
            }
        })
    }
}
```

### ⏳ Step 3.2: Add Integration Test

**File**: `internal/squasher/engine_test.go`

```go
func TestIndexTypeOptimization(t *testing.T) {
    tests := []struct {
        name           string
        migrations     []string
        expectedIndex  string // The index statement we expect in output
        shouldContain  string // What the index should have
        shouldNotContain string // What the index should NOT have
    }{
        {
            name: "Array column with spatial name should NOT use GiST",
            migrations: []string{
                `CREATE TABLE properties (coordinates FLOAT[]);`,
                `CREATE INDEX idx_coords ON properties (coordinates);`,
            },
            expectedIndex:    "idx_coords",
            shouldNotContain: "USING gist",
        },
        {
            name: "Actual spatial column SHOULD use GiST",
            migrations: []string{
                `CREATE TABLE locations (geo_point POINT);`,
                `CREATE INDEX idx_geo ON locations (geo_point);`,
            },
            expectedIndex:  "idx_geo",
            shouldContain:  "USING gist",
        },
    }

    // ... implementation ...
}
```

### ⏳ Step 3.3: Run E2E Tests

After implementation:

```bash
# Rebuild
go build -o pgsquash cmd/pgsquash/main.go

# Test MyRoomie (should now pass)
./pgsquash squash "./case studies/myroomie/migrations"/*.sql \
  --safety standard \
  --output "./case studies/myroomie/e2e-fixed/standard"

# Should see:
# ✅ Validation passed - schemas are identical
```

## Phase 4: Validation (NOT STARTED)

### ⏳ Step 4.1: Verify MyRoomie Fix

Expected result:
- No more "data type double precision[] has no default operator class for access method 'gist'" errors
- `idx_properties_coordinates` should NOT have `USING gist`
- `idx_profiles_coordinates` should NOT have `USING gist`
- Validation should pass

### ⏳ Step 4.2: Verify No Regressions

Test all 3 projects:
- MyRoomie: Should now PASS ✅
- Nami AI App: Should still have same 6 function differences (unchanged) ⚠️
- VDK Hub: Should still PASS ✅

### ⏳ Step 4.3: Test with Actual Spatial Types

Create a test migration with real PostGIS:

```sql
-- Migration 1
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE TABLE locations (
  id SERIAL PRIMARY KEY,
  name TEXT,
  coordinates GEOMETRY(POINT, 4326)
);

-- Migration 2
CREATE INDEX idx_locations_coords ON locations USING gist (coordinates);
```

Expected: Index should keep `USING gist` because `coordinates` is `GEOMETRY`, not `FLOAT[]`.

## Implementation Checklist

- [x] Add ColumnTypeInfo struct to tracking/unified_tracker.go
- [x] Add columnTypes field to UnifiedTracker
- [x] Initialize columnTypes in NewUnifiedTracker()
- [ ] Add IsSpatialDataType() helper function
- [ ] Add IsArrayDataType() helper function
- [ ] Add GetBaseTypeName() helper function
- [ ] Add ExtractColumnTypes() method
- [ ] Add extractTypeName() helper function
- [ ] Add GetColumnType() getter method
- [ ] Integrate ExtractColumnTypes in ProcessMigration
- [ ] Remove regex safety net from engine.go (lines 1710-1756)
- [ ] Add OptimizeIndexTypes() method to engine.go
- [ ] Call OptimizeIndexTypes() in Squash() method
- [ ] Add unit tests for column type tracking
- [ ] Add integration tests for index optimization
- [ ] Run E2E tests on all 3 case studies
- [ ] Verify MyRoomie passes validation
- [ ] Verify no regressions in other projects

## Benefits of This Solution

1. **Accurate**: Uses actual PostgreSQL types from AST, not name guessing
2. **Proactive**: Prevents errors during consolidation, not after
3. **Maintainable**: Single source of truth (tracker's column types)
4. **Extensible**: Easy to add support for new spatial types
5. **No False Positives**: Won't incorrectly modify index types
6. **Type-Safe**: Works with pg_query_go's type system

## Estimated Effort

- **Remaining Implementation**: 2-4 hours
- **Testing**: 1-2 hours
- **Total**: 3-6 hours

## Next Steps

1. Implement the remaining helper functions in Step 1.2
2. Implement the extraction method in Step 1.3
3. Integrate extraction in ProcessMigration (Step 1.4)
4. Remove broken regex and add optimization (Phase 2)
5. Add tests (Phase 3)
6. Validate with E2E tests (Phase 4)
