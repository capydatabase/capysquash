# Privileges: SET ROLE, SET SESSION AUTHORIZATION and grantors

Parts of the history run as other roles:

- `SET ROLE psr_owner` creates a table (with an identity sequence), a view, a
  `SECURITY DEFINER` function and an enum, sets default privileges (which are
  therefore `psr_owner`'s and reach the table it creates next) and grants,
  including `SELECT ... WITH GRANT OPTION` to `psr_delegate`; `RESET ROLE`
  returns to the migrating role, whose table stays its own.
- `SET ROLE psr_delegate` passes `SELECT` on to `psr_reader`: PostgreSQL
  records `psr_delegate` as the grantor.
- `SET SESSION AUTHORIZATION psr_owner` creates a sequence;
  `SET LOCAL ROLE psr_owner` inside `BEGIN ... COMMIT` creates a type and ends
  with the transaction.

The baseline creates everything as the role that runs it and then writes the
outcome out: `ALTER ... OWNER TO psr_owner` for what that role created,
`ALTER DEFAULT PRIVILEGES FOR ROLE psr_owner`, and the grant made by
`psr_delegate` between `SET ROLE psr_delegate` and `RESET ROLE`, because
`GRANTED BY` only accepts the current role.

`scripts/run-e2e.sh` squashes this history at every safety level and compares
owners, privileges (grantors included) and default privileges of the result
with those of the original history.
