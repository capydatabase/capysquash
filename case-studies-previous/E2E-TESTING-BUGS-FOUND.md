# E2E Testing Bug Report
## Date: 2025-11-06
## Tester: Claude Code (Autonomous E2E Testing)

---

## Overview
This document tracks all bugs found during comprehensive E2E testing of pgsquash-engine across multiple real-world case studies. Testing includes multiple safety levels, Docker validation, and various configuration scenarios.

---

## 🔴 BUG #1: Contradictory Validation Results for Functions

**Severity:** HIGH
**Category:** Validation Logic
**Case Study:** nami ai app
**Safety Level:** standard
**Detected:** 2025-11-06 09:52:30

### Description
The Docker-based schema validation reports contradictory results for function comparisons. The same functions appear in **all three** categories simultaneously:
1. "Functions only in original"
2. "Functions only in squashed"
3. "Functions differs"

This is logically impossible - if a function exists in both schemas, it cannot be "only in" one of them.

### Example Output
```
Functions only in original: public.current_clerk_user_id
Functions only in original: public.set_session_user
Functions only in squashed: public.current_clerk_user_id
Functions only in squashed: public.set_session_user
Functions differs: public.current_clerk_user_id
Functions differs: public.set_session_user
```

### Affected Functions
- `public.set_session_user`
- `public.get_planning_analytics`
- `public.current_clerk_org_role`
- `public.current_clerk_org_id`
- `public.current_clerk_user_id`
- `public.validate_jwt_version`

### Root Cause Analysis
The validation logic is likely comparing functions by multiple attributes (name, signature, body) but reporting them incorrectly when differences are detected. The comparison may be:
1. Finding functions with same names in both schemas (so they shouldn't be "only in" either)
2. Detecting differences in implementation/formatting
3. Incorrectly reporting them as "only in" when they actually just "differ"

### Impact
- **User Confusion:** Users cannot trust validation output
- **False Negatives:** Real differences may be hidden in noise
- **Workflow Disruption:** Users don't know if squashing succeeded

### Proposed Solution (Architectural)
1. **Normalize function comparison:** Before comparing, normalize:
   - Case sensitivity (lowercase keywords)
   - Whitespace and formatting
   - Comment removal for comparison purposes
   - Volatility markers if not functionally significant

2. **Fix reporting logic:** A function should appear in ONLY ONE of:
   - "Only in original" (function doesn't exist in squashed)
   - "Only in squashed" (function doesn't exist in original)
   - "Differs" (function exists in both but with differences)

3. **Add detailed diff output:** When functions "differ", show:
   - What specifically changed (signature, body, attributes)
   - Side-by-side comparison
   - Whether difference is functionally significant

### Location
`internal/validation/` - validation logic and comparison functions

---

## 🔴 BUG #2: Poor SQL Formatting in Generated Output (Single-Line Functions)

**Severity:** HIGH
**Category:** SQL Generation / Deparser
**Case Study:** nami ai app
**Safety Level:** standard
**Detected:** 2025-11-06 09:52:30

### Description
The squashed SQL output concatenates CREATE FUNCTION statements without proper newlines, making the output nearly unreadable and violating SQL formatting conventions.

### Example Comparison

**Original Migration (Readable):**
```sql
CREATE OR REPLACE FUNCTION current_clerk_user_id()
RETURNS TEXT AS $$
  SELECT (auth.jwt()->>'sub')::TEXT;
$$ LANGUAGE SQL STABLE SECURITY DEFINER;

CREATE OR REPLACE FUNCTION current_clerk_org_id()
RETURNS TEXT AS $$
  SELECT (auth.jwt()->'o'->>'id')::TEXT;
$$ LANGUAGE SQL STABLE SECURITY DEFINER;
```

**Squashed Output (Unreadable):**
```sql
$$; CREATE OR REPLACE FUNCTION current_clerk_user_id() RETURNS text LANGUAGE sql VOLATILE AS $$
BEGIN
  RETURN (auth.jwt() ->> 'sub')::text;
END;
$$; CREATE OR REPLACE FUNCTION current_clerk_org_id() RETURNS text LANGUAGE sql VOLATILE AS $$
```

### Issues Identified
1. **No newlines between statements** - All on one continuous line
2. **Missing blank lines** between logical sections
3. **Inconsistent capitalization** - Original uses uppercase keywords, output uses lowercase
4. **Poor readability** - Impossible to review or debug
5. **Breaks git diffs** - Changes span entire lines

### Impact
- **Unusable for Code Review:** Cannot review squashed SQL
- **Hard to Debug:** When validation fails, can't identify issues
- **Poor Git History:** Diffs are meaningless
- **Violates Best Practices:** No SQL style guide allows this formatting
- **Team Rejection:** Teams will reject poorly formatted output

### Root Cause Analysis
The issue is in either:
1. **`internal/squasher/deparser.go`** - Deparsing from AST without proper formatting
2. **`internal/builder/`** - SQL builder not inserting proper newlines/formatting
3. **Postprocessing** - Formatting step removing necessary whitespace

The builder is likely using `strings.Join()` or similar without proper delimiters.

### Proposed Solution (Architectural)
1. **Implement AST-aware SQL formatter:**
   ```go
   type SQLFormatter struct {
       IndentSize    int
       MaxLineLength int
       Style         FormattingStyle // COMPACT, READABLE, EXPANDED
   }
   ```

2. **Add formatting rules:**
   - 2 blank lines between CREATE FUNCTION statements
   - 1 blank line between CREATE TABLE and next statement
   - Proper indentation for function bodies
   - Uppercase keywords (CREATE, RETURNS, LANGUAGE, etc.)
   - Line wrapping at reasonable lengths (<120 chars)

3. **Use existing formatters:**
   - Consider integrating `pg_format` or similar
   - Or implement subset of formatting rules
   - Make formatting configurable via config

4. **Separate concerns:**
   - Deparser: AST → SQL (correct syntax)
   - Formatter: SQL → Pretty SQL (readability)
   - Keep these as separate, testable stages

### Location
- `internal/squasher/deparser.go` - Function deparsing
- `internal/builder/sql.go` - SQL building and formatting
- `internal/postprocessing/postprocessor.go` - Post-processing

### Additional Notes
This also affects other statement types (CREATE TABLE, CREATE INDEX, etc.) but is most noticeable with functions due to their multi-line nature.

---

## 🟡 BUG #3: STABLE → VOLATILE Volatility Marker Changes

**Severity:** MEDIUM
**Category:** Function Transformation
**Case Study:** nami ai app
**Safety Level:** standard
**Detected:** 2025-11-06 09:52:30

### Description
Functions marked as `STABLE` in the original migrations are being changed to `VOLATILE` in the squashed output. While this is safer (VOLATILE is more restrictive), it may have performance implications and is causing validation failures.

### Example
**Original:**
```sql
$$ LANGUAGE SQL STABLE SECURITY DEFINER;
```

**Squashed:**
```sql
$$ LANGUAGE sql VOLATILE SECURITY DEFINER;
```

### Analysis
From the logs, this appears intentional:
```
☑ Added VOLATILE volatility marker to function update_memory_card_search_vector for index predicate compatibility
☑ Added STABLE volatility marker to function set_session_user for index predicate compatibility
```

The system is adding volatility markers "for index predicate compatibility" - this suggests the postprocessor is trying to ensure functions work correctly with partial indexes.

### Questions
1. **Is this behavior documented?** Users should know their functions will be modified
2. **Is it always necessary?** Should only apply when functions are used in index predicates
3. **Can it break things?** Changing STABLE to VOLATILE can:
   - Prevent query optimization
   - Reduce performance
   - Change caching behavior
4. **Why is validation failing?** If this is intentional, validator should account for it

### Impact
- **Performance Regression:** VOLATILE functions cannot be optimized as aggressively
- **Validation Failures:** Causes "differs" reports even when functionally equivalent
- **Unexpected Behavior:** Users don't expect function attributes to change

### Proposed Solution
1. **Make it configurable:**
   ```json
   "function_volatility": {
     "auto_adjust": true,
     "preserve_original": false,
     "apply_only_when_needed": true
   }
   ```

2. **Apply selectively:**
   - Only change volatility for functions used in index predicates
   - Preserve original volatility otherwise
   - Document why change was made in comments

3. **Update validator:**
   - Consider volatility marker changes as "equivalent" in validation
   - OR add a "strict" vs "lenient" validation mode

4. **Document clearly:**
   - Add warnings when functions are modified
   - Explain why in output
   - Provide option to disable

### Location
- `internal/postprocessing/postprocessor.go` - Volatility marker adjustment
- `internal/plugins/volatility/` - Volatility detection plugin

---

## Testing Progress

### Case Study 1: nami ai app ✅ In Progress
- [x] Standard safety level without AI
- [ ] Standard safety level with AI
- [ ] Conservative safety level
- [ ] Aggressive safety level
- [ ] Docker validation analysis

### Case Study 2: vdk hub ⏳ Pending
- [ ] Initial testing

### Case Study 3: myroomie ⏳ Pending
- [ ] Initial testing

---

## Test Environment
- **Engine Version:** 0.9.5
- **Docker:** 28.5.2
- **PostgreSQL:** 15
- **Platform:** macOS Darwin 25.1.0
- **Date:** 2025-11-06

---

## Next Steps
1. Continue E2E testing with remaining case studies
2. Test different safety levels
3. Test with AI features enabled
4. Investigate deparser/builder code for formatting issues
5. Propose architectural fixes for validation logic

---

## 🔴 BUG #4: Empty ConsolidatedSQL for DO Blocks

**Severity:** LOW-MEDIUM
**Category:** Consolidation Engine
**Case Study:** vdk hub, myroomie
**Safety Level:** standard
**Detected:** 2025-11-06 09:55:16

### Description
When processing anonymous DO blocks, the consolidation engine produces empty `ConsolidatedSQL` and skips them. This is reported in the log as:

```
☑ Object anonymous_block::DO_BLOCK has empty ConsolidatedSQL, skipping
```

### Analysis
DO blocks often contain important operations like:
- Role creation
- Extension installation
- Schema setup
- Conditional DDL

Skipping them entirely could lose critical functionality.

### Questions
1. **Why is ConsolidatedSQL empty?** Is the consolidation rule not handling DO blocks?
2. **Should DO blocks be consolidated?** Or should they be preserved as-is?
3. **What's in these blocks?** Need to check original migrations to see what's being lost

### Impact
- **Loss of Functionality:** Critical setup code may be skipped
- **Silent Failure:** No error, just a warning, so users may not notice
- **Production Issues:** Missing roles, extensions, or schema setup

### Proposed Solution
1. **Preserve DO blocks by default** - Don't consolidate them, just copy them through
2. **Extract DDL from DO blocks** - The parser already does this (Bug #2 logs show extraction)
3. **Flag for user decision:**
   ```json
   "do_block_handling": {
     "strategy": "preserve" | "extract_ddl" | "skip",
     "warn_when_skipped": true
   }
   ```

### Location
- `internal/squasher/engine.go` - Consolidation logic
- `internal/tracking/consolidation/` - Consolidation rules

---

## 🔴 BUG #5: Missing Role Creation in Squashed Output (CRITICAL)

**Severity:** CRITICAL
**Category:** SQL Generation / Role Management
**Case Study:** myroomie (76 migrations)
**Safety Level:** standard
**Detected:** 2025-11-06 09:56:00

### Description
The squashed SQL creates RLS policies that reference PostgreSQL roles (`authenticated`, `anon`, `service_role`) but **never creates these roles**. This causes validation and production deployments to fail with:

```
ERROR: pq: role "authenticated" does not exist

Statement 792:
CREATE POLICY user_actions_own ON user_actions 
  TO authenticated 
  USING (user_id = clerk_user_id()) 
  WITH CHECK (user_id = clerk_user_id());
```

### Root Cause
The original migrations likely contain role creation code (probably in DO blocks or early setup), but the squashing process either:
1. **Skips the role creation** (related to Bug #4 - DO blocks being skipped)
2. **Places it too late** in the output (after policies that use it)
3. **Loses it during consolidation**

### Example Missing Code
Original migrations probably have something like:
```sql
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'authenticated') THEN
    CREATE ROLE authenticated NOLOGIN;
  END IF;
END $$;
```

### Impact
**CRITICAL - DEPLOYMENT BREAKING:**
- ✗ Squashed migrations **cannot be applied** to any database
- ✗ Production deployments **will fail** completely
- ✗ No recovery without manual role creation
- ✗ Users lose trust in the tool
- ✗ This makes pgsquash **unusable for production**

### Affected Roles
Based on Supabase/PostgreSQL patterns:
- `authenticated` - For authenticated users
- `anon` - For anonymous access
- `service_role` - For service/admin access
- `authenticator` - For connection pooling (sometimes)

### Proposed Solution (Architectural)

#### 1. **Role Dependency Tracking**
Add role tracking to the dependency graph:

```go
type RoleDependency struct {
    RoleName      string
    CreationSQL   string
    UsedByObjects []string // Policies, grants that need this role
    MustExistFirst bool
}
```

#### 2. **Ensure Roles Created First**
In `internal/builder/sql.go`, add a "roles" section that comes before everything:

```go
func (b *Builder) Build() string {
    sections := []string{
        b.buildExtensions(),
        b.buildRoles(),           // NEW: Roles before policies
        b.buildSchemas(),
        b.buildTables(),
        // ...
        b.buildPolicies(),
    }
}
```

#### 3. **Extract Role Creation from DO Blocks**
The parser should specifically extract and preserve role creation:

```go
func (p *Parser) extractRoleCreation(doBlock string) []string {
    // Look for CREATE ROLE patterns in DO blocks
    // Preserve them as standalone DDL
}
```

#### 4. **Inject Standard Roles**
For Supabase/PostgreSQL projects, auto-inject standard roles if detected:

```go
const supabaseRolesTemplate = `
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'anon') THEN
    CREATE ROLE anon NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'authenticated') THEN
    CREATE ROLE authenticated NOLOGIN;
  END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'service_role') THEN
    CREATE ROLE service_role NOLOGIN;
  END IF;
END $$;
`
```

### Validation Enhancement
The validator should check for this explicitly:

```go
func (v *Validator) validateRoleReferences(sql string) error {
    referencedRoles := extractRoleReferences(sql) // From policies, grants
    createdRoles := extractRoleCreations(sql)     // From CREATE ROLE
    
    missing := setDiff(referencedRoles, createdRoles)
    if len(missing) > 0 {
        return fmt.Errorf("SQL references roles that are never created: %v", missing)
    }
    return nil
}
```

### Location
- `internal/parser/parser.go` - Role extraction from DO blocks
- `internal/tracking/unified_tracker.go` - Role dependency tracking
- `internal/builder/sql.go` - Role creation section
- `internal/plugins/supabase/` - Supabase-specific role injection
- `internal/validation/validator.go` - Role reference validation

### Additional Notes
This bug shows that the current approach of relying solely on consolidation rules is insufficient. The system needs **architectural knowledge** of PostgreSQL and frameworks like Supabase to ensure generated SQL is complete and valid.

