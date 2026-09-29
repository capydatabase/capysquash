# Privileges: drops and renames

Privileges follow each object's identity through the history: a table dropped
and recreated loses its old grants, renamed tables and columns keep theirs,
`SET SCHEMA` moves a table with its privileges, and
`GRANT ... ON ALL TABLES IN SCHEMA` reaches only the tables that existed when
it ran. A function rename is covered by `privileges_routines`.

`003_schemas_types.sql` renames a schema with privileges, table privileges and
default privileges in it (a table created after the rename receives them),
renames an enum and a domain with privileges, and drops a schema with
privileges and recreates it, which must start from the defaults.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
owners, privileges and default privileges of the result with those of the
original history.
