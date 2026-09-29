# Privileges: default privileges

`ALTER DEFAULT PRIVILEGES` only reaches objects created after it, and the
squash reorders creation, so the baseline turns the defaults each object got
at creation into explicit grants and sets the final default privileges at the
end. Covered: database-wide defaults (which replace the built-in ones) and
schema defaults (which add to them) for tables, sequences (a `bigserial`
column's sequence), functions, types and schemas; objects created before the
defaults; defaults changed after objects exist; defaults for another role; and
`FOR ROLE postgres`, which applies only when postgres runs the migrations - the
baseline decides that with `current_user` when it is applied.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
owners, privileges and default privileges of the result with those of the
original history.
