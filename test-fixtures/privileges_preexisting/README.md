# Privileges: objects the history does not create

The history changes the privileges of the `public` schema, which exists before
it runs: it revokes `CREATE` and `USAGE` from PUBLIC (`CREATE` is already
revoked on PostgreSQL 15 and later, `USAGE` is not), grants `USAGE` to one
role and `CREATE` to another, and creates a table in the schema.

The baseline replays those statements in its PRIVILEGES section. At the
paranoid level, validation compares the owners and privileges of objects that
existed before the baseline ran - here the `public` schema - with production,
where it used to leave them out.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
owners, privileges and default privileges of the result with those of the
original history.
