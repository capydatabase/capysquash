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

Status: stable since 1.0.0. Use `conservative` or `paranoid` on anything that matters,
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
  policies and roles, sequences, enum/composite/domain/range types, owners,
  privileges (table, column, sequence, routine, schema and type ACLs),
  default privileges and comments.
- **Keeps the security posture.** Grants, revokes, ownership changes and
  `ALTER DEFAULT PRIVILEGES` come out as the net state the history leaves
  (see [Privileges, ownership and roles](#privileges-ownership-and-roles)).
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
| `paranoid` | Production | Preserve everything, reorder only; checks the result against production (below) |
| `conservative` | Production | Safe merges only |
| `standard` | Staging, development | Balanced consolidation (default) |
| `aggressive` | Local development | Maximum cleanup |

`paranoid` needs two databases: `PROD_DB_DSN` (production, only read, in a
read-only transaction) and `CAPYSQUASH_VALIDATION_DSN` (an empty database
capysquash may populate and reset; never production). After squashing it
applies the baseline to the validation database, reads both catalogs with the
same `pg_catalog` queries (`format_type`, `pg_get_constraintdef`,
`pg_get_indexdef`, `pg_get_functiondef`, ...), and fails when any table,
column (type, nullability, default, generation, identity, collation, order),
constraint, index, trigger, policy, view, function overload, sequence, enum or
other type in the schemas the baseline creates differs from production, and
when any owner, privilege or default privilege differs - including those of
objects that were in the validation database before the baseline ran (the
`public` schema, extension objects), wherever production has them too, since
a history grants and revokes on them. Privileges a platform granted on such
objects in production (outside the history) therefore show up as
differences. The validation database gets its owners and privileges back when
it is reset. Production schemas the baseline creates nothing in are listed as
a warning.

### Manual overrides

Two comment pragmas are honoured inside migrations:

```sql
-- capysquash:ignore            keep the next statement verbatim (also capysquash:no-merge)
-- capysquash-ignore:CSQ.SAFETY.CONCURRENT_INDEX   suppress a lint rule on this statement
```

`capysquash-ignore-next:` and `capysquash-ignore-file:` scope a lint suppression to
the next statement or the whole file. Rule codes are listed by
`capysquash lint --help`.

### Renames and schemas

A baseline emits objects by kind (types, tables, indexes, policies, ...), so a
rename cannot stay where it was in the history. Before consolidating,
capysquash rewrites the history to the names objects end with:

- Schemas, enums, composite and domain types, views, materialized views,
  sequences and indexes are created under their final names, with their final
  enum values and attributes; `ALTER ... RENAME`, `SET SCHEMA`,
  `ALTER TYPE ... RENAME VALUE` and `RENAME ATTRIBUTE` disappear. Column types,
  function signatures, casts, defaults and comparisons with a renamed enum
  value, and grants follow. A domain's unnamed `CHECK` keeps the name
  PostgreSQL derived from the domain's first name.
- A table keeps its own statements (`CREATE TABLE`, its `ALTER TABLE`s and
  its renames, including column renames and `SET SCHEMA`) as written and in
  order, because PostgreSQL names a table's constraints, indexes and sequences
  after the table and column names they had at creation. Every other
  statement - indexes, foreign keys from other tables, views, policies,
  triggers, comments, data - names the table and its columns as they end up
  and runs after them. An index created without a name gets the name
  PostgreSQL gave it; a view keeps its column names (`new AS old`, `*`
  spelled out).
- Schemas are objects: the baseline creates them in a `=== SCHEMAS ===`
  section after the roles (with `IF NOT EXISTS` and `AUTHORIZATION` as
  written). A schema the history creates and drops - `DROP SCHEMA ... CASCADE`,
  or once empty - leaves nothing in the baseline, contents included; one
  created again starts clean. Dropping or renaming a schema the history did
  not create (`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`) is
  replayed at the start of that section.

Text PostgreSQL does not rewrite either - `DO` blocks, function bodies written
as strings - is left as it is; when it names an object by a name it later
gave up, a `Renames and schemas:` warning says so, as it does for anything
else the rewrite cannot carry (a `NATURAL` join or `USING` list over a renamed
column, a field of a renamed composite attribute, a statement that stays but
uses an object `DROP SCHEMA ... CASCADE` removes, a renamed range type).

### Privileges, ownership and roles

`GRANT`/`REVOKE`, ownership changes (`ALTER ... OWNER TO`) and
`ALTER DEFAULT PRIVILEGES` are not merged into object definitions. capysquash
replays them through a model of PostgreSQL's access control lists - following
renames, `SET SCHEMA` and drops, so a dropped object's grants disappear and a
recreated one starts clean - and the baseline gets two sections:

- `=== ROLES ===` at the top: each `CREATE ROLE` of the history, run only when
  the role does not exist yet (roles belong to the whole cluster, and a
  baseline is applied to new databases in clusters that often have them), then
  role membership (`GRANT role TO role`) in history order.
- `=== PRIVILEGES ===` at the end, once every object exists: ownership
  changes; statements on objects the history does not create (the `public`
  schema, extension objects) and `ON ALL ... IN SCHEMA` statements, replayed in
  history order; then, per object, the grants and revokes that take it from
  PostgreSQL's built-in default privileges to its net state (grant options
  included; a list that would need `MAINTAIN` goes through `ALL`, which works
  on every version); finally the default privileges. Default privileges only
  reach objects created after them, so the privileges each object received at
  creation are written out as explicit grants.

The model assumes one role runs the history and the baseline, in one session,
and that objects start from the built-in defaults. Role switches in the
history are followed: after `SET ROLE`, `SET SESSION AUTHORIZATION` or
`SET LOCAL ROLE` (until the transaction block ends), created objects belong to
that role, `ALTER DEFAULT PRIVILEGES` without `FOR ROLE` is that role's, and
grants are made by it. The baseline creates everything as the role that runs
it and spells the outcome out: `ALTER ... OWNER TO` for each owner, and a
grant another role made with a grant option it holds (on an object it neither
owns nor belongs to the owner of) between `SET ROLE` and `RESET ROLE`, because
PostgreSQL records the current role as the grantor and `GRANTED BY` cannot name
another. `SET ROLE` and `SET SESSION AUTHORIZATION` themselves are not
repeated among the object definitions, and `BEGIN`/`COMMIT` of individual
migrations are left out of the baseline.
`ALTER DEFAULT PRIVILEGES FOR ROLE name` reaches history objects only when
`name` is that role, which cannot be known when squashing: the privileges that
depend on it go into a `DO` block that checks `current_user` when the baseline
runs. Anything the model cannot carry is reported as a `Privileges:` warning.

Validation compares owners and effective privileges (`relacl`, `attacl`,
`proacl`, `nspacl`, `typacl`, a `NULL` ACL counting as the `acldefault()` it
stands for) and `pg_default_acl`. Role names are compared as they are - PUBLIC,
the `pg_*` roles and every application role - except the role that owns the
database, which each side names `<database owner>`: the history and the
baseline are usually applied by that role under different names (for example
`neondb_owner` in production and `postgres` in a validation container).
Validation does not create roles: the history's own `CREATE ROLE` statements
do, and any other role a history grants to must already exist in the
validation cluster under the same name. `SCHEMA_DIFF`, which applies the
baseline and the original history one after the other in one cluster, drops
the roles the baseline created before the original history runs.

## Validation

By default `squash` validates its own output with Docker: it starts a
PostgreSQL container, applies the original history and the candidate to two
databases, and compares the catalogs (`--validation-mode TWO_DATABASES`;
`TWO_CONTAINERS` and `SCHEMA_DIFF` are the slower and faster alternatives).
Validation needs a reachable Docker daemon. `--no-validate` skips it, and
`--fail-on-diff=false` downgrades a real difference to a warning.

Before consolidating, `squash` also lints every input migration with the
`lint` rules (pre-flight). Findings are reported as warnings; `--strict` (or
`"static_validation": {"strict": true}` in the config) aborts the squash on any
finding or on a migration the rules cannot check.

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
Snapshot files carry `capysquash.catalog-snapshot.v2` (v2 added owners,
privileges and default privileges); compare snapshots taken by the same
version.

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
