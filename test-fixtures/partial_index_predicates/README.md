# Test fixture: partial_index_predicates
# Tests partial index consolidation and predicate normalization

This fixture tests how pgsquash handles partial indexes with WHERE clauses:

1. **Predicate normalization**: Should normalize spacing and formatting
2. **Consolidation**: Should merge similar indexes safely
3. **Validation**: Should ensure predicates are functionally equivalent

## Original migrations:

001_create_users.sql: Creates users table with status and email
002_create_active_idx.sql: Creates partial index on active users
003_create_pending_idx.sql: Creates partial index on pending users
004_create_verified_idx.sql: Creates partial index on verified users

## Expected behavior:

- Should preserve all partial indexes (can't safely merge)
- Should normalize WHERE clause formatting
- Should maintain functional equivalence
