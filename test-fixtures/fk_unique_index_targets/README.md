# Foreign keys to columns made unique by an index

Foreign keys whose referenced columns are unique only through a
`CREATE UNIQUE INDEX`, not a `UNIQUE` or `PRIMARY KEY` constraint: written in
`CREATE TABLE`, added with `ALTER TABLE` (multi-column, in another column
order than the index), referencing the table itself, a table in another
schema, and between two tables that end up referencing each other.

PostgreSQL needs the index when it creates the foreign key. The baseline
creates each such index with the statements of its table, where the history
created it, instead of in the indexes section after every foreign key.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
the catalog of the result with that of the original history.
