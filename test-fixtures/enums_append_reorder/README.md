# Test fixture: enums_append_reorder
# Tests ENUM consolidation with different safety modes

This fixture tests how pgsquash handles ENUM type evolution:

1. **Paranoid mode**: Should preserve exact ALTER TYPE sequence
2. **Conservative mode**: Should merge only append-only operations
3. **Standard/Aggressive modes**: Should merge all ALTER TYPE into CREATE TYPE

## Original migrations:

001_create.sql: Creates user_status ENUM with initial values
002_add_active.sql: Adds 'active' value (safe append)
003_add_inactive.sql: Adds 'inactive' value (safe append)
004_reorder_simulation.sql: Simulates a reorder scenario (should not merge in conservative mode)

## Expected behavior by safety mode:

**Paranoid**: All files preserved as-is
**Conservative**: 002 and 003 merged into CREATE TYPE, 004 preserved separately
**Standard/Aggressive**: All ALTER TYPE statements merged into single CREATE TYPE
