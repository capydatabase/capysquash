# Privileges: schemas, sequences and types

Schema privileges (including the pre-existing `public` schema, whose
statements are replayed as written), `CREATE SCHEMA ... AUTHORIZATION`,
schema, sequence, type and domain ownership changes, sequence privileges
(named and through `ON ALL SEQUENCES IN SCHEMA`, which reaches a serial
column's sequence), enum, domain and range type privileges.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
owners and privileges of the result with those of the original history.
