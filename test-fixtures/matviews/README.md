# Test fixture: matviews

# Tests materialized view handling and REFRESH operations

This fixture tests how pgsquash handles materialized views:

1. **MV creation**: Should preserve CREATE MATERIALIZED VIEW statements
2. **REFRESH detection**: Should identify REFRESH MATERIALIZED VIEW operations
3. **Dependency ordering**: Should place MVs after their source tables
4. **Lock analysis**: Should correctly identify lock levels for concurrent refresh

## Original migrations:

001_create_base_tables.sql: Creates source tables.
002_create_matview\.sql: Creates materialized view.
003_add_refresh_function.sql: Creates function for refreshing MV.
004_schedule_refresh.sql: Creates trigger for automatic refresh.

## Expected behavior:

- Should preserve CREATE MATERIALIZED VIEW
- Should handle REFRESH operations correctly
- Should maintain dependency ordering
