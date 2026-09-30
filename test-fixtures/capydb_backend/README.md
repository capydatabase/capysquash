# CapyDB's control-plane history

The migrations of CapyDB's own control plane
(`backend/internal/store/migrations/*.sql` in the CapyDB workspace), copied
unchanged from backend commit `a1bf6da` (2026-09-30), 64 files. A real history
of the control plane's schema changes since its first migration: a table that other tables reference is
replaced and dropped (`clusters`, migration 019), tables end up referencing
each other through foreign keys added with `ALTER TABLE`, conditional `DO`
blocks add and replace check constraints (some `NOT VALID`, validated later),
columns are added, dropped and given new defaults, and some migrations
backfill data.

capysquash 1.3.0 and 1.4.0 did not produce a baseline that applied at any
safety level; this fixture keeps it that way.

`scripts/run-e2e.sh` squashes the history at every safety level and compares
the catalog of the result with that of the original history.

To refresh the copy from a CapyDB workspace checkout (optional; the fixture
does not have to follow the backend):

```bash
scripts/sync-capydb-backend-fixture.sh ../backend
```
