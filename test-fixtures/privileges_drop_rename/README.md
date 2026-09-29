# Privileges: drops and renames

Privileges follow each object's identity through the history: a table dropped
and recreated loses its old grants, renamed tables and columns keep theirs,
`SET SCHEMA` moves a table with its privileges, and
`GRANT ... ON ALL TABLES IN SCHEMA` reaches only the tables that existed when
it ran. A function rename is covered by `privileges_routines`.

Schema and type renames and dropped schemas are not here: the squash does not
carry `ALTER SCHEMA/TYPE ... RENAME` or `DROP SCHEMA` into the baseline yet (it
warns that the statement is not carried). The privilege model follows those
renames and drops; its unit tests cover them.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
owners, privileges and default privileges of the result with those of the
original history.
