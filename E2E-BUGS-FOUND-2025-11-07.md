# E2E Testing Bug Report - 2025-11-07

## Test Summary

**Date**: November 7, 2025
**Test Cases**: 6 (MyRoomie x2, Nami AI x2, VDK Hub x2)
**Total SQL Lines Tested**: ~421,000
**Safety Levels Tested**: Standard, Aggressive

### Test Results Overview

| Project | Safety Level | Exit Code | Output Created | Validation |
|---------|-------------|-----------|----------------|------------|
| MyRoomie | Standard | 0 (Success) | ✅ 000_baseline.sql (431k), 010_data.sql (200k) | ❌ Port exhaustion |
| MyRoomie | Aggressive | 0 (Success) | ✅ Files created | ❌ Port exhaustion |
| Nami AI | Standard | 0 (Success) | ✅ 000_baseline.sql, 010_data.sql | ❌ Port exhaustion |
| Nami AI | Aggressive | 0 (Success) | ✅ Files created | ❌ Port exhaustion |
| VDK Hub | Standard | 0 (Success) | ✅ 000_baseline.sql, 010_data.sql | ❌ Port exhaustion |
| VDK Hub | Aggressive | 0 (Success) | ✅ Files created | ❌ Port exhaustion |

## Bugs Identified

### BUG #1: False Positive Dependency Warning (HIGH PRIORITY)

**Severity**: Medium (False positive - doesn't affect output correctness)
**Category**: Validation/Warning System
**Status**: Confirmed

**Symptom**:
```
🟠 High Severity (1):
  ► Object public_roommate_listings_with_profiles::VIEW depends on public_profiles which is never created
```

**Root Cause**:
The dependency checker/warning system doesn't recognize `CREATE OR REPLACE VIEW` as creating an object. It only tracks plain `CREATE VIEW` statements.

**Evidence**:
1. In MyRoomie standard test, warning claims `public_profiles` is never created
2. Actual squashed SQL shows:
   - Line 1590: `CREATE OR REPLACE VIEW public_profiles` ✅
   - Line 2676: `CREATE OR REPLACE VIEW public_roommate_listings_with_profiles` ✅
   - Line 2700: Correctly joins to `public_profiles`
3. Objects ARE in correct dependency order
4. SQL is syntactically correct

**Impact**:
- User confusion: False positive warnings create distrust
- No actual functional impact - SQL executes correctly

**Location in Codebase**:
- `internal/validation/validator.go` - Dependency checker
- Likely in the object tracking logic that identifies "created" objects

**Recommended Fix**:
Update dependency validator to recognize these equivalent forms:
- `CREATE TABLE`
- `CREATE TABLE IF NOT EXISTS`
- `CREATE OR REPLACE VIEW`
- `CREATE OR REPLACE FUNCTION`

All should be treated as "creating" the object for dependency purposes.

---

### BUG #2: Docker Validation Port Exhaustion (CRITICAL)

**Severity**: High (Blocks validation on all tests)
**Category**: Docker/Validation Infrastructure
**Status**: Confirmed

**Symptom**:
```
❌ Validation failed: [ERROR:VALIDATION] code:VALIDATION_FAILED failed to find available port
suggestion: Stop some Docker containers to free up ports, or increase MaxPortSearchAttempts in config
   Inner error: [ERROR:VALIDATION] code:VALIDATION_FAILED no available ports found after
   checking 1000 ports starting from 15432
```

**Root Cause**:
The port allocation system cannot find available ports even after checking 1000 ports (15432-16432).

**Possible Causes**:
1. **Port range too restrictive**: Starting at 15432 may conflict with other services
2. **Port detection logic flawed**: May not be properly checking if ports are actually free
3. **Docker network configuration**: May have stale port bindings from previous runs
4. **Race condition**: Between port check and Docker container start

**Impact**:
- ALL validation tests fail
- Cannot verify schema equivalence between original and squashed migrations
- Defeats the purpose of end-to-end testing with Docker validation

**Location in Codebase**:
- `internal/validation/` - Validation module
- Docker port allocation logic
- Configuration: `MaxPortSearchAttempts` setting

**Recommended Fixes** (in priority order):

1. **Immediate**: Use dynamic port allocation (let Docker assign ports)
   ```go
   // Instead of manually searching for ports 15432-16432
   // Use "-p 0:5432" to let Docker allocate a random available port
   // Then query Docker to get the actual assigned port
   ```

2. **Short-term**: Expand port range and add cleanup
   - Start from a higher port (e.g., 25432 to avoid common conflicts)
   - Increase range (check 5000 ports instead of 1000)
   - Add cleanup of stale Docker containers before validation

3. **Long-term**: Architectural improvement
   - Use Docker Compose with automatic network management
   - Use Docker's built-in port allocation
   - Add retry logic with exponential backoff
   - Implement proper cleanup in defer/finally blocks

**Debugging Commands**:
```bash
# Check what's using ports in the range
lsof -i :15432-16432

# List Docker containers that might be holding ports
docker ps -a | grep postgres

# Clean up stale containers
docker container prune -f
```

---

## Additional Observations

### Positive Findings

1. **BUG #11 Fix Working**: Parser correctly extracts ALTER TABLE statements from IF blocks
2. **Plugin Detection**: Both Clerk and Supabase plugins detected and activated correctly
3. **Extension Detection**: Proper detection of PostGIS, pg_trgm, cube, earthdistance, etc.
4. **Error Recovery**: Aggressive mode shows "error_recovery_applied" working correctly
5. **DDL Cycle Detection**: Successfully identifies transient and versioning cycles

### SQL Output Quality

All tests generated syntactically valid SQL:
- Proper dependency ordering (views depend on tables, functions depend on types)
- Extensions listed at the top
- Data operations separated into 010_data.sql
- Consolidation maps created (.squashmap.json)

### Performance

- MyRoomie (76 migrations): ~36 seconds
- Nami AI (8 migrations): ~152ms
- VDK Hub (9 migrations): ~188ms

## Recommendations

### Priority 1: Fix BUG #2 (Port Exhaustion)
This is blocking all validation testing and should be fixed immediately. Without Docker validation, we cannot verify schema equivalence.

### Priority 2: Fix BUG #1 (False Positive Warning)
While not breaking functionality, this creates user confusion and erodes trust in the tool.

### Priority 3: Add Tests for Edge Cases
Based on this E2E testing, add unit tests for:
- CREATE OR REPLACE recognition in dependency tracking
- Docker port allocation under various conditions
- Plugin detection with mixed authentication services

## Next Steps

1. Fix BUG #2 with dynamic port allocation approach
2. Fix BUG #1 by updating dependency validator
3. Re-run all 6 test cases to verify fixes
4. Attempt actual Docker validation to verify schema equivalence
5. Add regression tests to prevent these bugs from recurring
