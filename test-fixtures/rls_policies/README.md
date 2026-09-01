# Test fixture: rls_policies

# Tests Row Level Security policy consolidation

This fixture tests how pgsquash handles RLS policies and role-based access:

1. **Policy preservation**: Should preserve USING vs WITH CHECK clauses
2. **Role consolidation**: Should handle role creation and assignment
3. **Security patterns**: Should maintain security semantics

## Original migrations:

001_create_organizations.sql: Creates organizations table.
002_create_users.sql: Creates users table with org_id.
003_setup_rls.sql: Enables RLS and creates policies.
004_add_admin_role.sql: Creates admin role and policies.

## Expected behavior:

- Should preserve all RLS policies
- Should maintain role relationships
- Should keep security constraints intact
