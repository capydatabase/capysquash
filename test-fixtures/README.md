# Test Fixtures Library

This directory contains comprehensive test fixtures for validating pgsquash functionality across different scenarios and edge cases.

## 📁 Fixture Structure

Each fixture follows a consistent structure:

```
fixture_name/
├── original/           # Original migration files

│   ├── 001_*.sql
│   ├── 002_*.sql
│   └── ...
├── expected/           # Expected output for different safety modes

│   ├── paranoid.sql    # Paranoid mode expected output

│   ├── conservative.sql # Conservative mode expected output

│   ├── standard.sql    # Standard mode expected output

│   ├── aggressive.sql  # Aggressive mode expected output

│   ├── baseline.sql    # DDL output (common across modes)

│   └── data.sql        # Data operations (if any)

└── README.md          # Fixture documentation

```

## 🧪 Available Fixtures

### 1. `enums_append_reorder`

Tests ENUM consolidation with different safety modes.

**Scenarios tested:**

- Paranoid mode: Preserves exact ALTER TYPE sequence
- Conservative mode: Merges append-only operations only
- Standard/Aggressive modes: Full consolidation of ALTER TYPE into CREATE TYPE

**Files:** 4 migrations with different ENUM evolution patterns

### 2. `fk_cycles`

Tests circular foreign key detection and resolution.

**Scenarios tested:**

- Detection of circular dependencies
- 2-phase constraint application
- Dependency ordering preservation

**Files:** 4 migrations creating complex FK relationships

### 3. `partial_index_predicates`

Tests partial index consolidation and predicate normalization.

**Scenarios tested:**

- Predicate formatting normalization
- Index consolidation safety
- WHERE clause equivalence

**Files:** 4 migrations with different partial index patterns

### 4. `rls_policies`

Tests Row Level Security policy preservation.

**Scenarios tested:**

- RLS policy consolidation
- Role-based access control
- Security constraint preservation

**Files:** 4 migrations setting up RLS policies

### 5. `matviews`

Tests materialized view handling and REFRESH operations.

**Scenarios tested:**

- Materialized view creation
- REFRESH operation separation
- Dependency ordering with MVs

**Files:** 4 migrations with MV lifecycle

### 6. `pragma_examples`

Tests manual override pragmas (` -  pgsquash:ignore`, ` -  pgsquash:no-merge`).

**Scenarios tested:**

- Pragma detection in comments
- PreserveVerbatim functionality
- Inline pragma parsing
- Data operation pragma handling

**Files:** 3 migrations demonstrating pragma usage

## Running Tests

### Unit Tests

```bash

# Run fixture validation tests

go test -v ./test-fixtures/...

# Run with specific fixture

go test -v -run TestFixture/enums_append_reorder ./test-fixtures/...
```

### Integration Tests

```bash

# Run tests with Docker validation (requires Docker)

go test -v -tags=integration ./test-fixtures/...

# Run with specific PostgreSQL version

POSTGRES_VERSION=17 go test -v -tags=integration ./test-fixtures/...
```

### Fuzzing Tests

```bash

# Run fuzzing tests (generates random DDL)

go test -v ./test-fixtures/fuzz/...

# Run with specific parameters

go test -fuzz=FuzzSquash -fuzztime=30s ./test-fixtures/fuzz/...
```

## 🔧 Test Harness

The test harness provides several testing approaches:

### 1. Fixture-Based Testing

- Load migrations from `original/` directory
- Run through squashing engine with different safety modes
- Compare output with `expected/` files
- Validate schema equivalence with Docker

### 2. Property-Based Testing (Fuzzing)

- Generate random DDL sequences
- Test squashing preserves schema equivalence
- Validate edge cases and error conditions
- Performance testing with large datasets

### 3. Validation Testing

- Schema equivalence validation
- Extension compatibility testing
- Performance benchmarking

## 🐳 Docker Integration

Tests use Docker containers for schema validation:

```bash

# Run tests with Docker validation

docker run --rm -d \
  -e POSTGRES_PASSWORD=test \
  -e POSTGRES_DB=pgsquash_test \
  -p 5432:5432 \
  postgres:17

# Run integration tests

go test -tags=integration ./test-fixtures/...
```

## 📊 Test Coverage

The fixture library provides comprehensive coverage of:

- ✅ **Safety modes** (paranoid, conservative, standard, aggressive)
- ✅ **Schema objects** (tables, indexes, views, functions, types)
- ✅ **Constraints** (foreign keys, checks, unique, primary keys)
- ✅ **Security** (RLS policies, roles, grants)
- ✅ **Extensions** (version tracking, compatibility)
- ✅ **Edge cases** (circular dependencies, partial indexes, materialized views)
- ✅ **Performance** (large datasets, memory efficiency)

## 🚀 CI/CD Integration

The fixtures are automatically tested in CI across:

- **PostgreSQL versions:** 15, 16, 17, 18 (experimental)
- **Go versions:** 1.21, 1.22
- **Test types:** unit, integration
- **Safety modes:** all four modes
- **Extensions:** pg_stat_statements, uuid-ossp, and others

## 🤝 Contributing

### Adding New Fixtures

1. Create fixture directory: `mkdir new_fixture/{original,expected}`
2. Add migrations to `original/` directory
3. Add expected outputs to `expected/` directory
4. Create `README.md` with test description
5. Update this README and `fixtures_test.go`

### Fixture Format

- **Migration files:** `NNN_description.sql` (3-digit numbers)
- **Expected files:** `safety_mode.sql` or `baseline.sql`/`data.sql`
- **README.md:** Describe scenarios, expected behavior, edge cases

## 🔍 Debugging

### Test Output

```bash

# Verbose test output

go test -v ./test-fixtures/...

# Keep temporary files for inspection

go test -v ./test-fixtures/... -args -keep-temp
```

### Validation Reports

Test failures include validation reports showing:

- Schema differences between original and squashed
- Constraint violations
- Dependency issues
- Performance metrics

## 📈 Performance Benchmarks

The test suite includes performance benchmarks:

- **Small fixtures:** < 10 migrations
- **Medium fixtures:** 10-50 migrations
- **Large fixtures:** 50+ migrations
- **Memory usage:** Streaming vs non-streaming modes
- **Validation time:** Different Docker approaches

## ✅ Validation

All fixtures validate that:

1. **Schema equivalence:** Original and squashed schemas are identical
2. **Safety compliance:** Each safety mode behaves as expected
3. **Performance:** No regressions in processing time
4. **Compatibility:** Works across PostgreSQL versions
5. **Edge case handling:** Complex scenarios handled correctly
