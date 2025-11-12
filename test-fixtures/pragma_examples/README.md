# Pragma Examples Test Fixture

This fixture tests pragma (manual override) functionality in pgsquash.

## Pragma Types Tested

- **`-- pgsquash:ignore`**: Preserves statements verbatim, prevents consolidation
- **`-- pgsquash:no-merge`**: Preserves statements but allows them to be merged with similar statements
- **Inline pragmas**: Pragmas can be placed inline with SQL statements

## Migration Files

1. **001_create_users.sql**: Table creation with `-- pgsquash:ignore` pragma
2. **002_create_posts.sql**: Table and index creation with `-- pgsquash:no-merge` pragma
3. **003_add_data.sql**: Data operations with `-- pgsquash:ignore` pragma

## Expected Behavior

- **Paranoid mode**: All pragmas respected, minimal consolidation
- **Conservative mode**: Pragmas respected, some safe consolidation
- **Standard mode**: Pragmas respected, more consolidation where safe
- **Aggressive mode**: Pragmas respected, maximum consolidation

## Pragma Effects

- Statements with `-- pgsquash:ignore` should have `PreserveVerbatim = true`
- Statements with `-- pgsquash:no-merge` should also have `PreserveVerbatim = true`
- Data operations with pragmas should be preserved in separate files
- Pragma detection should work in both comment blocks and inline comments

## Testing Commands

```bash
# Test pragma detection
go test -v ./test-fixtures/... -run TestFixture/pragma_examples

# Test with specific safety mode
pgsquash squash test-fixtures/pragma_examples/original/ --output test_output/ --dry-run
```
