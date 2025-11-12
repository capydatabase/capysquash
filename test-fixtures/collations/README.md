# Collations Test Fixture

This fixture tests collation handling in PostgreSQL migration consolidation.

## Scenarios Tested

- **Column-level collations**: VARCHAR columns with specific collations
- **Index collations**: Indexes that use specific collation rules
- **Collation-aware constraints**: CHECK and UNIQUE constraints using collations
- **Cross-table collation consistency**: Ensuring collations are preserved correctly

## Migration Files

1. **001_create.sql**: Creates tables and indexes with various collations
2. **002_collations.sql**: Adds more tables and modifies existing ones with collations
3. **003_constraints.sql**: Adds collation-aware constraints and indexes

## Expected Behavior

- **Paranoid mode**: Preserves exact collation specifications, no consolidation
- **Conservative mode**: Consolidates only identical collation patterns
- **Standard mode**: Normalizes collation syntax, consolidates similar patterns
- **Aggressive mode**: Merges tables and indexes, preserves collation semantics

## Edge Cases

- Case-insensitive unique constraints using collations
- Mixed collation usage within the same table
- Collation-aware indexes and their dependencies
- Constraint expressions that use collation functions
