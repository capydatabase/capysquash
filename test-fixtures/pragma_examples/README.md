# Pragma Examples Test Fixture

This fixture tests pragma (manual override) functionality in capysquash.

## Pragma Types Tested

- **`-- capysquash:ignore`**: Preserves statements verbatim, prevents consolidation
- **`-- capysquash:no-merge`**: Preserves statements but allows them to be merged with similar statements
- **Inline pragmas**: Pragmas can be placed inline with SQL statements

## Migration Files

1. **001_create_users.sql**: Table creation with ` -  capysquash:ignore` pragma
2. **002_create_posts.sql**: Table and index creation with ` -  capysquash:no-merge` pragma
3. **003_add_data.sql**: Data operations with ` -  capysquash:ignore` pragma

## Expected Behavior

- **Paranoid mode**: All pragmas respected, minimal consolidation
- **Conservative mode**: Pragmas respected, some safe consolidation
- **Standard mode**: Pragmas respected, more consolidation where safe
- **Aggressive mode**: Pragmas respected, maximum consolidation

## Pragma Effects

- Statements with `-- capysquash:ignore` should have `PreserveVerbatim = true`
- Statements with `-- capysquash:no-merge` should also have `PreserveVerbatim = true`
- Data operations with pragmas should be preserved in separate files
- Pragma detection should work in both comment blocks and inline comments

## Testing Commands

```bash

# Test pragma detection

go test -v ./test-fixtures/... -run TestFixture/pragma_examples

# Test with specific safety mode

capysquash squash test-fixtures/pragma_examples/original/ --output test_output/ --dry-run
```
