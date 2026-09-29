# Renames: tables with indexes, foreign keys, grants and other dependents

Two tables are created with everything that hangs off a table - named and
unnamed indexes (one on an expression), an identity and a serial column,
UNIQUE and CHECK constraints, a foreign key between them, a view, a policy, a
trigger with `UPDATE OF` and `WHEN`, column and table grants, a column
comment and a row of data - and then both tables and some of their columns
are renamed. Later migrations add a table whose foreign keys and an index
name the new names.

The baseline keeps each renamed table's own statements (CREATE TABLE, its
renames) in order, so the constraint, index and sequence names PostgreSQL
derived from the old names stay; every dependent statement names the tables
and columns as they end up and runs after them. The unnamed indexes get the
names PostgreSQL gave them, spelled out; the view keeps its column names.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
the catalog of the result, owners and privileges included, with that of the
original history.
