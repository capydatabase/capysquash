# Privileges: tables, views and columns

Table, view and materialized-view privileges, column privileges, `REVOKE`
after `GRANT`, `WITH GRANT OPTION` and `REVOKE GRANT OPTION FOR`, a
table-level `REVOKE` that also clears the column privilege, `GRANT ALL` minus
some privileges (`MAINTAIN` exists only on PostgreSQL 17+), and an ownership
change after the grants (PostgreSQL moves the grantor of every entry to the
new owner, and the identity sequence follows the table).

`scripts/run-e2e.sh` squashes this history at every safety level and compares
owners and privileges of the result with those of the original history.
