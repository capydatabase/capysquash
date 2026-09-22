# Security Policy

## Supported versions

Only the latest release receives security fixes.

## Reporting a vulnerability

Do not open a public issue for a security problem. Report it privately through
[GitHub security advisories](https://github.com/capydatabase/capysquash/security/advisories/new)
with a description, reproduction steps and the impact you see. You will get an
acknowledgement, a fix or a plan, and credit in the advisory unless you prefer
otherwise.

## Things worth knowing

- `capysquash` parses SQL and, on request, executes it against databases you
  point it at: Docker containers it starts itself, or the database behind
  `validate-external`. It never executes migrations against a database you
  did not name.
- `validate-external` refuses a non-empty database and reads the connection
  string from an environment variable so that it stays out of argument lists,
  snapshots and JSON output.
- Docker-based validation needs the Docker socket, which is host-root
  equivalent. Keep that to development machines.
- Backup generation shells out to `pg_dump`; rollback scripts are plain SQL
  files. Review both before relying on them.
- `--branch-check` runs `git` in the working directory.

Run `govulncheck ./...` to check the dependency tree for known
vulnerabilities.
