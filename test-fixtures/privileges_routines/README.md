# Privileges: functions and procedures

Overloads with different privileges (identity is name plus argument types),
`REVOKE EXECUTE ... FROM PUBLIC`, a grant with grant option, a function named
without arguments, `CREATE OR REPLACE` keeping privileges, a rename carrying
them, ownership changes, and `REVOKE ... ON ALL FUNCTIONS IN SCHEMA` that must
not reach a function created afterwards.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
owners and privileges of the result with those of the original history.
