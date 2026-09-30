# A dropped table the kept tables referenced, and a foreign key cycle

The shape of CapyDB's own migration 019: `clusters` is referenced by tables
the history keeps (`projects` through a column it later drops, `jobs` through
a column it drops without dropping the constraint first) and is then dropped
with `CASCADE`; conditional `DO` blocks add check constraints, one on the
dropped table and one on a column added after its table was created; and
`projects` and `instances` end up referencing each other through foreign keys
added with `ALTER TABLE`, while `api_keys`, created before `projects`,
references it.

The baseline keeps `projects` and `jobs` without their foreign keys to
`clusters` (the columns stay, so the later `DROP COLUMN` leaves the same
column positions), leaves out `clusters` and everything that alters it, adds
the `DO` block's constraint after the column it checks, creates `api_keys`
after `projects`, and adds the two foreign keys between `projects` and
`instances` once both tables exist, under the names PostgreSQL gave them.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
the catalog of the result with that of the original history.
