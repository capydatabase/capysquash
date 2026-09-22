# capysquash

**Squash a long PostgreSQL migration history into a clean baseline, and prove
the result is equivalent.** `capysquash` parses every migration with
PostgreSQL's own parser (`pg_query_go`), tracks each object across the whole
history, consolidates what can be consolidated at the chosen safety level, and
validates the output by building both versions in real PostgreSQL and
comparing the catalogs.

```bash
capysquash analyze migrations/*.sql                  # read-only: what would change
capysquash squash  migrations/*.sql --dry-run        # preview the baseline
capysquash squash  migrations/*.sql --output clean/  # consolidate and validate
capysquash validate migrations/ clean/               # re-check any two histories
```

capysquash is the open-source engine behind `capydb migrate squash` in
[CapyDB](https://capydb.dev). It is a standalone CLI with no account, no
service and no telemetry; CapyDB adds managed validation on top of it (see
[CapyDB-managed validation](#capydb-managed-validation)).

Status: beta. Use `conservative` or `paranoid` on anything that matters,
validate before you apply, and read the generated SQL.

## Installation

```bash
go install github.com/capydatabase/capysquash/cmd/capysquash@latest
```

`pg_query_go` is a cgo package, so the build needs a C toolchain (Xcode
command-line tools, `build-essential`, or equivalent). Native archives for
Linux and macOS are published on the
[releases page](https://github.com/capydatabase/capysquash/releases)
for tags that ran the release workflow.

There is also a container image; see [docker/README.md](docker/README.md).

## What it does

- **Consolidates** `CREATE` + `ALTER` chains, `DROP`/`CREATE` cycles, enum and
  function redefinitions, RLS policy churn and column lifecycles into one
  dependency-ordered baseline, split into DDL and data operations.
- **Proves equivalence.** Post-squash validation applies the original history
  and the candidate to two PostgreSQL databases (local Docker, or any empty
  database you point it at) and compares catalog signatures: extensions,
  tables and columns, constraints, indexes, views, functions, triggers, RLS
  policies and roles, sequences, enum/composite/domain/range types, ownership,
  grants and comments.
- **Respects your stack.** Built-in plugins detect Supabase (`auth.*`,
  `storage.*`, RLS), Clerk (JWT v2 claims), Prisma and Drizzle patterns and
  adapt consolidation and validation accordingly.
- **Static lint rules** (`capysquash lint`) for the usual migration hazards:
  non-concurrent index builds, `UPDATE`/`DELETE` without `WHERE`, constraints
  added without `NOT VALID`, breaking drops and renames, `varchar(n)` and
  `int` where `text` and `bigint` belong.
- **Streams** histories of 1000+ files without loading them into memory
  (`--streaming`), with lock-level analysis and transaction planning.
- **Interactive TUI** (`capysquash tui`) for browsing the analysis, the
  dependency graph and the configuration.

## Commands

| Command | What it does |
| --- | --- |
| `analyze <files...>` | Read-only analysis: redundancies, object counts, warnings |
| `squash <files...>` | Consolidate into `--output` (or `--dry-run`); validates afterwards unless `--no-validate` |
| `validate <original> <squashed>` | Build both directories in Docker PostgreSQL and diff the catalogs |
| `validate-external <path>` | Apply a history to an empty external database and snapshot or compare its catalog (see below) |
| `lint <files or dirs...>` | Static rules with `--fix`/`--write` autofixes and `--json` output |
| `safe <files...>` | Conservative safety + full Docker validation + backup and rollback scripts |
| `fast <files...>` | Standard safety + schema-diff validation + streaming |
| `analyze-deep <files...>` | Exhaustive analysis including DDL cycle detection and risk assessment |
| `init-config` | Write a `capysquash.config.json` documenting every option |
| `tui [dir]` | Interactive dashboard (`tui analyze`, `tui deps`, `tui config` jump to a view) |

Commands take migration **files**, not directories (`migrations/*.sql`), except
where noted. `capysquash <command> --help` is the authoritative flag reference.

### Safety levels

| `--safety` | Use for | What it does |
| --- | --- | --- |
| `paranoid` | Production | Preserve everything, reorder only |
| `conservative` | Production | Safe merges only |
| `standard` | Staging, development | Balanced consolidation (default) |
| `aggressive` | Local development | Maximum cleanup |

### Manual overrides

Two comment pragmas are honoured inside migrations:

```sql
-- capysquash:ignore            keep the next statement verbatim
-- capysquash-ignore:CSQ.SAFETY.CONCURRENT_INDEX   suppress a lint rule on this statement
```

`capysquash-ignore-next:` and `capysquash-ignore-file:` scope a lint suppression to
the next statement or the whole file. Rule codes are listed by
`capysquash lint --help`.

## Validation

By default `squash` validates its own output with Docker: it starts a
PostgreSQL container, applies the original history and the candidate to two
databases, and compares the catalogs (`--validation-mode TWO_DATABASES`;
`TWO_CONTAINERS` and `SCHEMA_DIFF` are the slower and faster alternatives).
Validation needs a reachable Docker daemon. `--no-validate` skips it, and
`--fail-on-diff=false` downgrades a real difference to a warning.

### `validate-external`

For environments without Docker, `validate-external` runs the same comparison
against a database you own. It refuses a database that is not empty (apart
from schemas you allow with `--allow-existing-schema`), reads the connection
string from an environment variable so it never appears in process listings
or output, and emits a stable JSON contract on stdout:

```bash
export CAPYSQUASH_VALIDATION_DSN='postgres://...'
capysquash validate-external migrations/ --dsn-env CAPYSQUASH_VALIDATION_DSN \
  --snapshot-output original.catalog.json --json
# reset the database, then:
capysquash validate-external clean/ --dsn-env CAPYSQUASH_VALIDATION_DSN \
  --against-snapshot original.catalog.json --json
```

The result carries `contract_version: "capysquash.external-validation.v1"`,
`success`, `phase`, `comparison_valid`, `has_differences` and `differences`.

### CapyDB-managed validation

The CapyDB CLI wraps this binary and runs `validate-external` for you in a
short-lived, isolated preview cell, so no local Docker is needed:

```bash
capydb migrate squash ./migrations --workflow safe --validation capydb --project my-project
```

`--workflow safe` maps to `conservative`, `--workflow fast` to `standard`.
See the [CapyDB docs](https://docs.capydb.dev).

## Configuration

`capysquash init-config` writes a `capysquash.config.json` that documents every
option. The CLI loads it from the working directory automatically; `--config`
points at another file, and flags override both.

```json
{
  "safety_level": "standard",
  "output": { "format": "organized", "directory": "squashed" },
  "rules": {
    "table_operations": { "consolidate_create_alter": true, "remove_drop_create_cycles": true }
  },
  "validation": { "mode": "TWO_DATABASES", "docker_image": "postgres:17" },
  "performance": { "parallel_processing": true, "streaming": true }
}
```

## Adopting a squashed baseline

Squashing produces a clean repository baseline. Marking it as applied in the
tool that owns your migration history is a separate step;
[scripts/README.md](scripts/README.md) has helpers for Prisma and Drizzle.

## Development

```bash
make build      # CGO build with version metadata
make test       # go test -race ./...
make lint       # golangci-lint (falls back to go vet)
make check      # fmt + lint + test
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for the architecture and
[internal/plugins/README.md](internal/plugins/README.md) for adding a plugin.
Docker-based tests and the `integration`-tagged tests need a Docker daemon or
a `DATABASE_URL`; everything else runs offline.

## License

MIT - see [LICENSE](LICENSE).
