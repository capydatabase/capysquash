# Changelog

All notable changes to capysquash will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

> **⚠️ Important Notice**
>
> This tool modifies SQL migration files. Always maintain backups of your original migrations before running consolidation operations. While capysquash includes validation and safety checks, you should verify the output matches your expectations. Test consolidated migrations in a non-production environment first.

---

## [Unreleased]

### Added

- Fixture `sequence_lifecycle` in the e2e suite.

### Fixed

- A sequence the history creates and drops again is left out of the baseline, with everything
  done to it (`ALTER SEQUENCE`, `COMMENT ON SEQUENCE`). Its `CREATE SEQUENCE` used to stay while
  the `DROP SEQUENCE` was dropped with it, so the baseline created a sequence the original
  history did not end with. The same holds for a sequence dropped along with the column or table
  it belongs to (a `serial` or identity column, `OWNED BY`, `DROP IDENTITY`), and a sequence the
  history detaches (`OWNED BY NONE`) before dropping its table keeps no `OWNED BY` naming that
  table, which made the baseline fail.

## [1.5.0] - 2026-09-30

### Added

- Fixtures `sequence_defaults`, `dropped_referenced_table` and `capydb_backend` in the e2e suite.
  `capydb_backend` is CapyDB's own control-plane history (64 migrations, copied unchanged;
  `scripts/sync-capydb-backend-fixture.sh` refreshes the copy from a backend checkout).

### Fixed

- A history that drops a table other tables reference, or whose tables end up referencing each
  other, gets a baseline that applies. CapyDB's own control-plane history did not produce one at
  any safety level (1.3.0 and 1.4.0 alike); it now reproduces the original catalog at all four,
  on PostgreSQL 15 to 18. The causes, all in the engine:
  - A table with a foreign key to a table the history drops was left out of the baseline with
    that table. Dropping a referenced table drops the constraint, not the referencing table. The
    kept table's statements now lose only their foreign keys to dropped tables; the columns stay,
    so a later `DROP COLUMN` leaves the same column positions.
  - One dependency cycle among the tables, types and functions of the baseline made the whole
    section fall back to alphabetical order, so tables came before the tables they reference.
    Statements are now ordered by their dependencies with the history's order breaking ties, and
    a cycle is broken at its earliest statement without moving anything that waits on it.
  - Foreign keys added with `ALTER TABLE` between two tables that end up referencing each other
    (`projects` and `instances`) were not deferred. Every foreign key between tables of a cycle,
    inline or added later, is now added once all tables exist, under the name PostgreSQL gave
    it. The old handler only looked at `CREATE TABLE`, rewrote the whole table's statements and
    named the constraints `fk_table_column`, which PostgreSQL never does.
  - A constraint a `DO` block adds conditionally was placed right after `CREATE TABLE`, before
    the `ALTER TABLE ... ADD COLUMN` that creates the column it checks (at the levels that keep
    those separate). It now goes where the history ran it among the table's statements.
  - `DO` blocks altering a dropped table landed in the constraints section and in
    `010_data.sql`.
  - `ALTER TABLE ... ADD COLUMN` recorded neither the table its inline `REFERENCES` names nor
    the column's type, so at the paranoid level the statement could run before either existed.
- A sequence named in a column default (`nextval`, `currval`, `setval`, `'name'::regclass`) is
  created before the table: the default is read for the sequence it names, and the sequence of a
  `serial` or identity column counts as created by its table. `ALTER SEQUENCE ... OWNED BY` (and
  `CREATE SEQUENCE ... OWNED BY`) comes after the table it names. The sequence used to be emitted
  wherever its name sorted.
- Columns of an enum, domain or composite type (renamed, in another schema, or added with
  `ALTER TABLE`) no longer produce "depends on X which is never created" warnings: the name is
  looked up among the types the history creates. A type nothing creates is still reported.

- `SquashDirectory` with streaming enabled (the programmatic API in `internal/engine`) no longer
  hangs. It ran a concurrent parse pipeline whose output channel was never closed, so the
  goroutines waiting for the last parsed file waited forever, and it handed the tracker the files
  in whatever order the parse workers finished them. It now reads the directory's `*.sql` files
  in name order and squashes them the way `capysquash squash --streaming` does; files in
  subdirectories are no longer read. `capysquash analyze --streaming` prints its progress, which
  it never did.

## [1.4.0] - 2026-09-29

### Added

- A `=== SCHEMAS ===` section after the roles: the schemas the history creates and keeps, created
  as written (`IF NOT EXISTS`, `AUTHORIZATION`) under their final names, preceded by the renames
  and drops of schemas the history did not create (`DROP SCHEMA public CASCADE`), in history
  order. Elements of `CREATE SCHEMA ... CREATE TABLE ...` become statements of their own.
- Fixtures `rename_schema`, `rename_types`, `rename_table_dependents`, `drop_schema_recreate`,
  `privileges_preexisting` and `privileges_set_role` in the e2e suite; `privileges_drop_rename`
  now also renames a schema, an enum and a domain with privileges and drops and recreates a
  schema.
- `Renames and schemas:` warnings for what the rewrite to final names cannot carry: text that
  names an object by a name it gave up (`DO` blocks, function bodies written as strings), a
  `NATURAL` join or `USING` list over a renamed column, a field of a renamed composite attribute,
  a statement that uses an object `DROP SCHEMA ... CASCADE` removes, a renamed range type.

### Fixed

- `SET ROLE`, `SET SESSION AUTHORIZATION` and `SET LOCAL ROLE` in a history are followed. The
  privilege model assumed the migrating role ran every statement: objects created as another role
  came out owned by the role running the baseline, default privileges set as that role were
  attributed to the migrating role, and a grant made by a role other than the owner (with a grant
  option it holds) came out with the owner as grantor. The model now tracks the role each
  statement runs as - `CURRENT_USER`, object owners, `ALTER DEFAULT PRIVILEGES`, grantors, role
  membership and superusers created by the history, `REVOKE ... CASCADE` of what a grant option
  passed on - and the baseline writes the outcome out explicitly: `ALTER ... OWNER TO` for each
  owner, `ALTER DEFAULT PRIVILEGES FOR ROLE`, and a delegated grant between `SET ROLE` and
  `RESET ROLE`, because PostgreSQL records the current role as the grantor and `GRANTED BY`
  cannot name another. The `SET ROLE` statements themselves used to land among the object
  definitions of the baseline.
- `BEGIN`, `COMMIT` and the other transaction statements of individual migrations are left out of
  the baseline. They used to land among the regrouped statements, which could leave the rest of
  the baseline in a transaction that never committed; a `ROLLBACK` is reported as a warning.
- Validation runs every migration file, and the baseline, in one database session, so session
  settings (`SET ROLE`, `BEGIN ... COMMIT`) reach the statements after them as they do with a
  migration tool; statements used to go through a connection pool one by one.
- `ALTER SCHEMA ... RENAME`, `ALTER TYPE ... RENAME` (enums and composite types),
  `ALTER DOMAIN ... RENAME`, `ALTER TYPE ... RENAME VALUE` and `ALTER TYPE ... RENAME ATTRIBUTE`
  reach the baseline. The history is rewritten to final names before consolidation: every object
  is created under the name it ends with, and everything that names it - objects in a renamed
  schema, column types, function signatures, casts, defaults and comparisons with a renamed enum
  value, grants - uses that name. These statements used to be dropped with a "not carried into
  the baseline" warning, leaving a baseline that did not apply.
- `DROP SCHEMA` is carried: schemas are tracked as objects. A schema the history creates and
  drops (with `CASCADE`, or once empty) leaves nothing in the baseline, what it held included,
  and a schema created again under the same name starts from the defaults. `DROP SCHEMA` used to
  be ignored while the schema's `CREATE SCHEMA` and contents stayed.
- A table rename no longer breaks the baseline's order. An index, foreign key, view, policy,
  trigger, comment or row created before the rename named the old table and was emitted after
  the rename. A renamed table's own statements now stay together and in history order, so the
  constraint, index and sequence names PostgreSQL derived from its old name stay, and every
  dependent names the table and its columns as they end up: an index created without a name gets
  the name PostgreSQL gave it, and a view keeps its column names. Column renames are carried the
  same way, and a renamed table is no longer merged across the rename by the consolidation rules.
- Grants on a function follow its signature when a type it takes, or that type's schema, is
  renamed; they used to be reported as not in the baseline.
- The `paranoid` level compares the owners and privileges of objects that existed before the
  baseline ran - the `public` schema, extension objects - with production, wherever production has
  the object too. It used to leave every pre-existing object out, so a history's
  `REVOKE CREATE ON SCHEMA public FROM PUBLIC` or `GRANT USAGE ON SCHEMA public TO app` missing
  from the baseline went unnoticed. Resetting the validation database now also gives such objects
  their owners and privileges back, where they used to keep what the baseline changed. A failed
  paranoid check lists the differences in its error; it used to point at warnings that were never
  shown.
- `SCHEMA_DIFF` validation no longer fails a history that creates roles. It applies the squashed
  baseline and the original history one after the other in the same cluster, and roles belong to
  the cluster: the original's `CREATE ROLE` found the role the baseline had created and failed,
  which reported the equivalence as unproven. The roles the baseline created are now dropped with
  its database, so the original history runs in the same empty cluster the baseline had. Making
  the history's `CREATE ROLE` idempotent instead would validate a history other than the one
  given.
- `scripts/run-e2e.sh` skips a fixture directory without migrations (an empty `original/` left in
  a checkout) instead of failing on it.

## [1.3.0] - 2026-09-29

### Added

- The baseline keeps the history's security posture. `GRANT`/`REVOKE` (table, column, sequence,
  function and procedure overloads, schema, type and domain privileges, grant options included),
  ownership changes (`ALTER ... OWNER TO`) and `ALTER DEFAULT PRIVILEGES` are replayed through a
  model of PostgreSQL's access control lists that follows renames, `SET SCHEMA` and drops; the
  baseline ends with a `=== PRIVILEGES ===` section that takes each object it creates from the
  built-in default privileges to the net state the history leaves, once every object exists.
  Default privileges only reach objects created after them, so what each object received at
  creation is written as explicit grants and the final default privileges come last.
  `ALTER DEFAULT PRIVILEGES FOR ROLE name` applies to history objects only when `name` runs the
  migrations; the privileges that depend on it go into a `DO` block that checks `current_user`.
  Statements on objects the history does not create (the `public` schema, extension objects) and
  `ON ALL ... IN SCHEMA` statements are replayed in history order. What the model cannot carry is
  reported as a `Privileges:` warning. Every grant used to be dropped.
- A `=== ROLES ===` section at the top of the baseline: each `CREATE ROLE` of the history, run only
  when the role does not exist yet (roles are shared by every database of a cluster), then role
  membership (`GRANT role TO role`) in history order. `CREATE ROLE` used to be emitted after the
  objects that named the role (`CREATE SCHEMA ... AUTHORIZATION`), and membership was dropped.
- Catalog validation compares owners, privileges and default privileges at every level that
  validates (Docker validation, `validate-external`, and `paranoid` against production): `relacl`,
  `attacl`, `proacl`, `nspacl`, `typacl` (a `NULL` ACL counts as the `acldefault()` it stands for)
  and `pg_default_acl`, per function overload. Role names compare as they are except the role that
  owns the database, which both sides name `<database owner>`. It used to compare
  `information_schema` privilege views, which merge overloads, leave out schemas, types and
  default privileges, and hide rows from a non-superuser.
- Privilege fixtures in the e2e suite: `privileges_tables_columns`, `privileges_routines`,
  `privileges_schemas_sequences_types`, `privileges_default_privileges`, `privileges_drop_rename`.
- A schema statement the squash does not track (`ALTER SCHEMA ... RENAME`, `ALTER TYPE ... RENAME`,
  `DROP SCHEMA`) is reported as not carried into the baseline instead of vanishing silently.

### Changed

- Catalog snapshots are `capysquash.catalog-snapshot.v2`: owners, privileges and default privileges
  were added and owner names are normalized, so compare snapshots taken by the same version.
- Resetting a claimed validation database (the `paranoid` level) also removes the default
  privileges set since the claim.
- `scripts/run-e2e.sh` runs every fixture; none is skipped any more.

### Fixed

- `ALTER TABLE` statements that cannot merge into `CREATE TABLE` are replayed after it in history
  order. Only `ADD COLUMN` and `ADD CONSTRAINT` were kept; everything else was dropped - including
  `ENABLE ROW LEVEL SECURITY`, which left squashed tables without RLS.
- Enum values added `BEFORE`/`AFTER` a label, and renamed with `RENAME VALUE`, are placed where
  PostgreSQL places them; merges appended them, changing the label order. Conservative mode only
  merges true appends, and labels with a quote are escaped when rewritten.
- `REFRESH MATERIALIZED VIEW` is a data operation, run in history order after the schema; it was
  sorted among the schema objects, ahead of the unique index and the plain refresh `CONCURRENTLY`
  needs.
- `ALTER FUNCTION/PROCEDURE ... RENAME TO` is carried into the baseline; it was dropped. A function
  whose history ends with an `ALTER` keeps its `CREATE` at the `paranoid` level.
- The orphaned-statement safety net no longer removes a `COMMENT ON FUNCTION f(args)` that names a
  `public` function without its schema.
- The TUI validation view and `validate`'s post-flight static check keep a file's findings when one
  lint rule fails; the whole file used to be reported as the error only.
- The `enums_append_reorder`, `generated_columns_identity`, `matviews` and `rls_policies` fixtures
  had histories PostgreSQL rejects (type used before it was created; a generation expression
  reading another generated column and a missing column; `REFRESH ... CONCURRENTLY` on an
  unpopulated view without a unique index; policies calling functions and granting roles no
  migration created). The fixtures are fixed and the e2e suite runs them.

## [1.2.0] - 2026-09-29

### Added

- `scripts/run-e2e.sh` runs the end-to-end suite from any directory: it starts a throwaway
  PostgreSQL in Docker, runs every test including the `integration`-tagged ones against it, then
  squashes each fixture at the conservative, standard and aggressive levels and checks with
  `validate-external` that the baseline builds the same catalog as the original history. Fixtures
  whose original history PostgreSQL rejects are skipped with the failing statement named.
- `squash --strict` (or `"static_validation": {"strict": true}`) aborts the squash when pre-flight
  validation - the `lint` rules over the input migrations - reports any finding or cannot check a
  migration. Without it the findings stay warnings.
- `lint --fix` adds `NOT VALID` to `ADD CONSTRAINT ... CHECK/FOREIGN KEY` statements flagged by
  `CSQ.SAFETY.CONSTRAINT_NOT_VALID` (validate the constraint later with `VALIDATE CONSTRAINT`).
- `CSQ.HYGIENE.PREFER_BIGINT` also flags, and fixes, `ALTER TABLE ... ALTER COLUMN ... TYPE int`.
- A `function_overloads` fixture.
- The `paranoid` level checks the squashed baseline against production's catalog: it applies the
  baseline to an empty validation database (`CAPYSQUASH_VALIDATION_DSN` or `validation_dsn`; never
  production, which is only read, in a read-only transaction), loads both catalogs with the same
  `pg_catalog` queries and fails on any difference in tables, columns (type, nullability, default,
  generation, identity, collation, order), constraints, indexes, triggers, policies, views,
  materialized views, function overloads, sequences, enums and other types of the schemas the
  baseline creates. The validation database is claimed empty and reset afterwards. `paranoid`
  without a validation database fails closed. It used to check only that tables, functions and
  types of the same names existed.

### Changed

- `github.com/charmbracelet/ultraviolet` (indirect) moved to the 2026-09-22 pseudo-version
  `v0.0.0-20260922123528-4e49372c11f9`.
- CI lint job pins golangci-lint v2.14.0 (was v2.13.2); it runs clean on the `go 1.27.1` module.
- CI's integration job runs the `integration`-tagged tests of every package, not only
  `internal/validation`.
- Consolidation-rule metadata and `--version` report the version the binary was built as. Rules
  used to carry a hardcoded version; an unstamped build now reports the module version Go records
  instead of `1.0.0`.
- Statement line numbers (in parse errors and lint findings from the engine) point into the
  original migration file; they used to count lines of a copy with the comments removed.
- Database-backed validation replays the migrations in order inside a transaction that is always
  rolled back, one savepoint per statement, and reports every statement PostgreSQL rejects.
  Statements that cannot run in a transaction block (`CONCURRENTLY`, `VACUUM`, ...) are reported as
  not validated. It used to `EXPLAIN` only `INSERT`/`UPDATE`/`DELETE`.
- The production catalog is read from `pg_catalog` in one read-only, repeatable-read transaction
  (definitions rendered with `format_type`, `pg_get_expr`, `pg_get_constraintdef`,
  `pg_get_indexdef`, `pg_get_viewdef`, `pg_get_functiondef`, `pg_get_triggerdef`) instead of
  `information_schema`, which hid objects the role could not access and dropped type modifiers.
- The error-recovery rule wraps the consolidation rules of the safety level: a rule that fails no
  longer drops straight to the engine's fallback; the object is recovered (conservatively below
  aggressive) with the failure reported.

### Removed

- The internal index access-method rewriter (`optimizeIndexTypes`) and the placeholder
  `FormatFunctionBody`, neither of which anything called: rewriting an access method or
  reformatting a function body changes the schema a squash must reproduce.
- The SQL transformer no longer renames `substr`, `length` and `position` calls (see Fixed).

### Fixed

- Overloaded functions are separate objects. Functions were tracked by name alone, so `f(integer)`
  and `f(text)` collapsed into one: a later overload replaced an earlier one in the baseline,
  `DROP FUNCTION f(text)` dropped every overload, and comments landed on the wrong one. Functions,
  and `COMMENT ON`, `GRANT`/`REVOKE` and `DROP` of functions and procedures, are now keyed by the
  normalized input-argument types (`int`, `int4` and `integer` agree; argument names, defaults,
  `OUT`/`TABLE` columns and type modifiers are ignored, as in PostgreSQL).
- `COMMENT ON FUNCTION f`, `GRANT ... ON FUNCTION f` and `DROP FUNCTION f` written without
  arguments while `f` had one overload get that overload's argument types in the baseline, which
  PostgreSQL would otherwise reject as ambiguous once a later migration adds an overload.
- The SQL transformer reports each `COMMENT ON FUNCTION` in the output that PostgreSQL would reject
  (a signature matching no created overload, or no arguments on an overloaded name); its check was
  a placeholder. Transformation warnings now reach the squash warnings.
- The SQL transformer rewrote `substr(`, `length(` and `position(` across the whole baseline,
  including CHECK constraints, defaults and function bodies, so a squash failed its own catalog
  comparison (`length` became `char_length`) and `position(a IN b)` became `strpos(a IN b)`, which
  is not valid SQL.
- The `capysquash:ignore` and `capysquash:no-merge` pragmas work on their own comment line.
  Comment lines were stripped before statements were split, and statement *i* got the *i*-th
  comment, so a pragma above a statement was never seen. Comments now attach by position: to the
  statement they precede or sit inside, or to the statement ending on the same line
  (`CREATE ...; -- capysquash:no-merge`).
- An object whose history no rule merges - every table at the `paranoid` level - kept only its
  final CREATE: `ALTER TABLE ... ADD COLUMN` and `SET DEFAULT` statements made after it were lost.
  They are replayed as written.
- The orphaned-function safety net removed a dropped overload's comment or grant together with the
  section comment above it and left a stray `;`; it now removes exactly the statement.
- `squash --json` printed "Processing N migrations..." ahead of the JSON document whenever the
  config's `show_progress` was true (the default), so stdout did not parse; `--quiet` and `--json`
  now win over the config.
- A lint rule that errored hid the findings of every rule after it; all findings are returned with
  the rule errors.
- Validation levels were compared as strings, so `COMPREHENSIVE` skipped database validation and
  `STANDARD` ran the comprehensive-only performance checks.
- The memory statistics' peak usage was the current usage; the memory manager now keeps the
  high-water mark.
- The TUI goes back to the previous view: esc, `?` on the help screen and `v` on the validation
  view used to return to the dashboard whatever view was open before. The status bar hint says
  "ESC: Back".
- While a TUI configuration field is being edited, `q`, `?` and `v` go to the field; `q` used to
  quit the program mid-edit and `?`/`v` switched views. Ctrl+C still quits.
- The TUI status bar is painted in its background colour across the full width; the gaps between
  its segments had none.
- Type-change analysis records the type a column changes from (from the earlier statements, or the
  attached database) with data-loss and reversibility, instead of "unknown".

## [1.1.0] - 2026-09-26

### Added

- The TUI's validation view is reachable: press `v` anywhere (again to go
  back). It lints every migration with the same static rules as
  `capysquash lint`, honouring `static_validation` in the config file:
  safety and breaking findings and unparseable files fail it, hygiene
  findings are listed as warnings. The key is shown in the status bar and on
  the help screen. The view existed before but nothing opened it or gave it
  results.

### Changed

- The TUI (`capysquash tui`, `tui analyze`, `tui deps`, `tui config`) runs on
  Bubble Tea v2 (`charm.land/bubbletea/v2` v2.0.10) and Lip Gloss v2
  (`charm.land/lipgloss/v2` v2.0.6), replacing Bubble Tea v1.3.10 and Lip
  Gloss v1.1.0. Views, key bindings, colours and the alternate screen are
  unchanged; screens were compared side by side against the v1 build. Colour
  downsampling for 256-colour and 16-colour terminals now comes from Bubble
  Tea's renderer instead of Lip Gloss.

### Fixed

- The TUI analysis view said "-0 files" when a squash would remove no file; it says "none" (and
  "-1 file" for one).
- The TUI configuration wizard's edit box is indented as a whole. Only its
  top border was indented, so the rest of the box sat two columns to the left.
- Esc while editing a field in the TUI configuration wizard cancels the edit,
  as the on-screen hint says. It used to leave the wizard for the dashboard
  with the edit still open.
- The TUI status bar fits on one line. Its width sum left out the bar's own
  padding, so the "q: Quit" hint wrapped onto a second line at every
  terminal width.
- `capysquash tui analyze` and `capysquash tui deps` no longer sit on their
  loading message forever. The view the TUI starts on is now entered like any
  other, so it loads its data; before, only the dashboard and the
  configuration wizard loaded on start.
- The TUI no longer draws engine log lines over its own screen. The
  analysis, dependency graph and squash views ran the parser, tracker and
  squasher, whose default logger writes to stdout; the lines landed on top
  of the TUI (the squash result screen was mostly log text). The default
  logger is silenced while the TUI runs and restored when it exits.
- `govulncheck` is clean. The Docker validation path moved from the frozen
  `github.com/docker/docker` client (GO-2026-4887, GO-2026-4883, no fix will
  ever land on that module path) to `github.com/moby/moby/client` v0.6 and
  `github.com/moby/moby/api` v1.56. Same container lifecycle, same exec and
  port handling; a failed image pull is now reported instead of being
  swallowed by the progress loop. CI runs govulncheck on every push.
- CI lint job: golangci-lint v2.12.2 is built with Go 1.26 and refuses a `go 1.27.1`
  module before linting anything; the pin is now v2.13.2.
- golangci-lint is clean (it had 66 findings once it could run): unchecked `Close`
  errors are now explicitly discarded or reported, dead helpers left behind by
  the `pkg/` removal are deleted (type-dependency scanning, volatility and
  auth-function heuristics, plugin SQL transforms, the disabled AST
  formatter, the never-implemented column type check), `WriteString(Sprintf)`
  became `Fprintf`, and two ineffectual assignments and one always-false
  comparison are gone. No behaviour change.

## [1.0.0] - 2026-09-22

First stable release. The CapySquash product this engine grew out of is
retired, and the engine takes its name: the module is `github.com/capydatabase/capysquash` and the
binary is `capysquash`. Everything that only existed for the product is gone;
what is left is a standalone CLI.

### Changed

- **Renamed from pgsquash-engine to capysquash.** The module path is
  `github.com/capydatabase/capysquash`, the entry point is `cmd/capysquash`,
  the binary and the root command are `capysquash`, the config file it
  auto-loads is `capysquash.config.json` (`init-config` writes it), the log
  level variable is `CAPYSQUASH_LOG_LEVEL`, and the manual-override pragmas
  are `-- capysquash:ignore` and `-- capysquash:no-merge`. The
  `validate-external` contract strings are `capysquash.external-validation.v1`
  and `capysquash.catalog-snapshot.v1`; a CapyDB CLI older than 1.7.0 expects
  the `pgsquash.*` names and must be upgraded alongside this release. The lint
  directives (`-- capysquash-ignore:` family) and the `CSQ.*` rule codes are
  unchanged from v0.11.0.
- The container image runs `capysquash` directly (`ENTRYPOINT ["capysquash"]`)
  instead of a setup entrypoint, and the compose files only pass
  `CAPYSQUASH_LOG_LEVEL` (the other `PGSQUASH_*` variables were read by nothing).
- The version baked into a `go build` without ldflags is the current release
  (`1.0.0`) instead of a stale `0.9.7`; release builds stamp the tag via ldflags.
- CI is one workflow: build, vet, race tests and a binary smoke test on the
  `go.mod` toolchain, `validate-external` against PostgreSQL 15-18, and
  golangci-lint. The release workflow reads its Go version from `go.mod`;
  previously it pinned 1.26.5 against a `go 1.27.1` module and failed before
  publishing any archive.
- Repository links, the GoReleaser owner and the image labels point at
  `github.com/capydatabase/capysquash`. README, CONTRIBUTING and
  SECURITY describe the standalone CLI.

### Removed

- **The public Go API.** `pkg/` is gone; the engine is consumed as the
  `capysquash` binary (the CapyDB CLI drives it through `validate-external`),
  and everything now lives under `internal/`. The `pkg/cli` branded-binary
  entry point, `pkg/rules`, `pkg/errors`, `pkg/utils`, the plugin detection
  and compatibility API, the partner-integration metrics, the `examples/`
  programs and the package READMEs went with it.
- **The AI harness.** `pkg/harness`, the deterministic context/report/artifact
  builders, the `squash --emit-context` and `--context-output` flags, and the
  `deterministic_artifact`, `deterministic_report` and `harness_context` keys
  of `squash --dry-run --json`. The remaining keys (`baseline_sql`,
  `data_operations_sql`, `auth_compatibility_sql`, `safety_level`, `warnings`)
  are unchanged.
- **The CapySquash product plumbing.** `SetBrandName` and the second, branded
  binary it produced, the `.capysquash.yml` GitHub App configuration and its
  loader, the `.github/capysquash.yml` and `.github/capysquash.config.*`
  GitHub App examples, the `test-branding.sh` script, and the `health` command
  (a container health endpoint for the retired API server).
- The API server pointers under `docker/api-server/`, the container
  entrypoint that drove commands that no longer exist (`validate-config`,
  `workflow`, `validate --docker`), the duplicated `docker/engine` and
  `docker/dev-environment` stacks, `docker/scripts/`, the unreferenced init
  SQL, and the `deploy.sh`, `init-pgsquash.sh`, `validate.sh` and
  `build.sh` shell scripts. `Makefile` replaces the build scripts.
- The `develop`, `cli-split` and `refactor` branches (all merged or
  superseded by `main`).

### Fixed

- `go test ./...` failed on `test-fixtures/` since v0.10.0: the fixture and
  fuzz suites still imported the pre-rename `github.com/capy-base/...` path,
  and `go.mod` required the module itself.
- `docker/postgres/Dockerfile` is now tracked. A `docker/` line in
  `.gitignore` had kept it out of the repository, so `compose.yaml` could not
  build `postgres-primary` from a fresh clone.

## [0.11.0] - 2026-09-09

### Fixed

- **The development stack could not start PostgreSQL at all.** `postgres:18`
  images place the cluster in a major-version subdirectory below a single mount
  at `/var/lib/postgresql`; the compose files still mounted the data volume at
  `/var/lib/postgresql/data` (the pre-18 convention), so the entrypoint aborted
  and the container restart-looped. The primary database and the dev-environment
  stack now mount at `/var/lib/postgresql`. The 15/16/17 services in
  `compose.testing.yaml` correctly keep the old path.
- The `pgsquash` service is a one-shot CLI - it runs a command and exits - but
  carried `restart: unless-stopped` and a healthcheck, which crash-looped it
  under `docker compose up`. It is now `restart: "no"` with no healthcheck, and
  the files document `docker compose run --rm pgsquash <command>` as the way to
  drive it.
- Build arguments defaulted to `${BUILD_DATE:-$(date -u ...)}` and
  `${GIT_COMMIT:-$(git rev-parse ...)}`. Compose does not run subshells, so
  those command substitutions were baked into the image as literal strings.
  They are static defaults (`unknown`) now; CI passes real values.
- `docker/postgres/Dockerfile` referenced `${PG_VERSION}` in its `LABEL`
  without re-declaring the argument inside the stage, so
  `org.pgsquash.postgres.version` was always empty.
- **pgAdmin could never start.** `PGADMIN_EMAIL` defaulted to
  `admin@pgsquash.localhost` (and `admin@pgsquash.local` in the dev-environment
  stack); pgAdmin rejects reserved domains outright - "the part after the
  @-sign is a special-use or reserved name" - and exits, so the container
  restart-looped forever. The default is now `admin@pgsquash.dev`, in the
  compose files and in `.env.example`.
- pgAdmin also published the wrong port: it cannot bind the privileged port 80
  under `no-new-privileges` and silently falls back to 8080, so the published
  mapping pointed at a port nothing was listening on. `PGADMIN_LISTEN_PORT` is
  now pinned to 8080 and the mapping matches.
- Removed the `./docker/pgadmin/servers.json` bind mount. That file has never
  existed in the repository, so Docker created an empty *directory* at the
  source path and mounted it over pgAdmin's config file.
- **Docker-based validation could never reach the Docker daemon.** The socket is
  `root:root` mode 0660 and the image runs as uid 10001, so
  `docker/validation/with-validation.yml` - whose entire purpose is
  Docker-driven validation - got "permission denied". That service now runs as
  `user: "0:0"`. Mounting the socket already confers host-root, so this gives
  away nothing the mount had not; `no-new-privileges` and `cap_drop: [ALL]`
  stay on. The core and dev-environment stacks keep their non-root uid (they
  mount `~/.ssh` into `/home/pgsquash`) and now say so where the socket is
  mounted.

### Changed

- **Every Dockerfile and Compose file was rebuilt on current Docker conventions
  (Engine 29 / Compose 5 / BuildKit 0.33).** The main `Dockerfile` is now a
  proper multi-stage build (`base` → `deps` → `build` → `runtime`) behind a
  `# syntax=docker/dockerfile:1` frontend, which the file previously lacked
  entirely. It uses a read-only bind mount of the source
  instead of `COPY . .` and BuildKit cache mounts for the module and build
  caches, and it takes its Go toolchain from `golang:1.27.1-trixie` rather than
  installing an out-of-date Go tarball into Ubuntu with `wget`. Everything that
  ships is staged into `/out` by the build stage. CGO stays enabled and the
  image is deliberately *not* cross-compiled, because `pg_query_go` links
  `libpg_query`. The runtime user is now a numeric uid/gid (10001).
- `docker-compose.yml`, `docker-compose.testing.yml` and
  `docker-compose.tools.yml` are now `compose.yaml`, `compose.testing.yaml` and
  `compose.tools.yaml` - the canonical filenames, so the core stack needs no
  `-f` flag. Obsolete `version:` keys, hard-coded `container_name`s and the
  fixed `172.20.0.0/16` subnet are gone; services gained `no-new-privileges`,
  `cap_drop: [ALL]` where the workload allows it, rotating `local` log drivers,
  memory limits, `init: true`, and healthchecks with `start_interval` so a
  database is marked healthy as soon as it is ready. Published ports are bound
  to `127.0.0.1` instead of every interface, and pgAdmin and Filebrowser now sit
  behind a `tools` profile so they do not start by default.
- The overlays under `docker/` (`dev-environment/full-stack.yml`,
  `engine/quick-start.yml`, `validation/with-validation.yml`) got the same
  treatment. Their filenames are unchanged, since they are always invoked with
  `-f` and are referenced by name from the READMEs.

### Known issues

- `docker/api-server/` (its `Dockerfile` and `docker-compose.yml`) builds
  `./cmd/api-server`, which no longer exists in this repository. It has been
  left untouched rather than modernized or deleted - it is dead as it stands.

## [0.10.0] - 2026-09-02

### Added

- `pgsquash validate-external` for applying a migration path to a caller-owned
  empty PostgreSQL database, capturing a portable catalog snapshot, and
  comparing a second build against it. The command refuses non-empty databases,
  supports DSNs through an environment variable, and emits the stable
  `pgsquash.external-validation.v1` JSON contract.
- Public catalog snapshot types and comparison helpers in `pkg/validation`.
- Catalog signatures for sequences, custom types and domains, relation and
  function ownership, row-security flags, policy roles, grants, and comments.

### Changed

- Migration execution now honors context cancellation for compatibility SQL and
  every migration statement.
- CLI diagnostics use stderr so JSON output on stdout remains machine-readable.
- Release automation now publishes only native GitHub archives and checksums;
  obsolete Homebrew and container-registry publication paths were removed.
- Project documentation now describes the engine as a standalone OSS component
  and documents CapyDB-managed validation.

### Removed

- The unused GitHub App/webhook package and its authentication dependencies.
- The managed subscription feature catalog and `features` CLI command; these
  described the retired hosted product rather than an OSS engine capability.
- Archived CapySquash platform manifests, Docker publishing, and self-analysis
  workflows from the engine repository.

---

## [0.9.7] - 2026-07-07

Correctness overhaul across the safety ladder, validation, output pipeline, and public API.

### Changed

- **Safety ladder rebuilt**: safety levels now map to a single, consistent rule
  set; `ParseSafetyLevel` is exposed on the public engine API and rules
  configuration is available on `engine.Config`.
- **Validation outcome semantics**: validation results now distinguish
  infrastructure failures from genuine schema differences instead of collapsing
  both into a generic failure.
- **Schema comparison** is performed via catalog-signature queries
  (`SchemaComparator`), not `pg_dump` text diffing; `SCHEMA_DIFF` mode is a real
  implementation rather than a placeholder.
- **Dry-run purity**: `--dry-run` no longer writes any files or mutates state.
- **Staged atomic writes**: output files are written to a staging location and
  moved into place atomically, so a failed run cannot leave partial output.
- **Streaming guards**: streaming mode enforces memory-limit and batch-size
  invariants instead of silently degrading.
- **Config precedence** fixed: explicit `--config` > project config > embedded
  defaults, applied uniformly across CLI and library entry points.
- **Plugin detection delegates to the real plugins**
  (`pkg/plugins.DetectPlugins`): migrations are parsed with the engine parser
  and each built-in plugin's own `Detect` runs, replacing a divergent
  substring-matching reimplementation that missed `auth.uid()` (Supabase) and
  Clerk JWT v2 patterns.
- **Plugin compatibility is computed, not hardcoded**
  (`pkg/plugins.CheckCompatibility`): results come from the registry's
  priority-based conflict resolution (Clerk 95 excludes Supabase 90;
  Prisma/Drizzle mutually exclusive) via a new exported
  `internal/plugins.ResolveConflicts`.
- **Built-in plugins are registered by default** in library entry points;
  `plugins.RegisterDefault()` remains for explicit initialization.
- **go-github upgraded v57 → v88**, matching the version pulled by
  `ghinstallation` v2.19.0 so binaries ship a single copy of the library.
  (v89 exists but would reintroduce a duplicate until ghinstallation bumps.)
- CI validation workflow now runs against `postgres:18`.
- `cmd/pgsquash` consumes `pkg/errors` (public API) instead of
  `internal/errors`; critical errors still exit with code 2.

### Removed

- Dead public surface in `pkg/github`: the personal-access-token `Client`,
  `OAuthHandler`, `AppAuthHandler`/`InstallationConfig`, and the entire
  `TokenStorage` family (zero consumers). The GitHub App / installation /
  check-run path is unchanged.
- `WebhookHandler` no longer embeds an analysis engine or PAT client; it is a
  signature-verifying receiver (`NewWebhookHandler(secret)`). Webhook event
  processing lives in capysquash-api.
- Exported example functions from `pkg/validation` (runnable examples live in
  `examples/`).
- Hardcoded plugin metadata tables in `pkg/plugins` (descriptions, versions,
  pattern strings); plugin info now derives from the plugin instances.
- The AI analysis subsystem (`internal/ai`) and its configuration surface.

### Fixed

- `pkg/validation` violation-category constants now alias
  `internal/validation` instead of shadowing them with duplicate literals.
- Documentation: lowercase module path (`github.com/capysquash/...`) across all
  docs, corrected TUI API references (`Launch`/`LaunchWithView`/`NewModel`),
  corrected GitHub client method names, and architecture docs updated to match
  the actual `pkg`/`internal` contract.

---

## [0.8.5-beta] - 2025-10-20

### Added - Interactive TUI (2025-10-18)

#### Terminal User Interface

- **Full-featured TUI built with Bubble Tea** - Beautiful, interactive terminal interface for migration management

- **Dashboard View**: Migration statistics, detected plugins, configuration status

- **Analysis View**: Tabbed interface showing overview, lifecycle patterns, dependencies, and issues

- **Configuration Wizard**: Interactive field editing for safety levels, rules, and plugin settings

- **Dependency Graph**: Object dependency visualization with forward/reverse modes

- **Progress View**: Real-time squashing progress with statistics and completion summary

- **Help System**: Comprehensive keyboard shortcuts and usage information

- **Multiple access methods** for flexibility:

- `pgsquash tui [dir]` - Launch TUI dashboard

- `pgsquash analyze [files] --tui` - Launch in analysis view

- `pgsquash squash [files] --tui` - Launch in squashing view

- `pgsquash tui analyze [dir]` - Direct to analysis

- `pgsquash tui config` - Direct to configuration wizard

- `pgsquash tui deps [dir]` - Direct to dependency graph

- **Full keyboard navigation**:

- Global: `q/Ctrl+C` (quit), `ESC` (dashboard), `?` (help)

- Navigation: Arrow keys or `j/k/h/l` (vim-style)

- Actions: `Enter/Space` (select), `r` (refresh), `s` (save)

- Context-sensitive help in each view

- **Modern styling with lipgloss**:

- Color-coded status (success/warning/error)

- Responsive layouts

- Progress bars and statistics

- Bordered containers with visual hierarchy

### Changed - Major Infrastructure Refactor (2025-10-18)

#### Docker Infrastructure Cleanup

- **Simplified Docker Compose Structure** - Reduced from bloated 11-service setup to clean modular architecture
- **Core services** reduced from 11 to 2 (pgsquash + postgres-primary)
- **Removed 7 non-integrated services**: Redis, MinIO, Grafana, Prometheus, Traefik (moved pgAdmin & Filebrowser to tools)
- **Created modular compose files**:
- `docker-compose.yml` - 2 core services (\~500MB RAM)
- `docker-compose.testing.yml` - PostgreSQL 17, 15, 13 for multi-version testing (+1GB RAM)
- `docker-compose.tools.yml` - pgAdmin + Filebrowser for development (+300MB RAM)
- **Performance improvements**: 75% faster startup (15s vs 60s), 75% less RAM usage
- **Better organization**: Each service now has clear purpose and integration status

#### Documentation Overhaul

- **Rewrote `docker/README.md`** (516 lines) - Complete rewrite reflecting new simplified structure

- Removed references to non-existent `docker/web-app/` directory (clarified separate repository)

- Added comprehensive tables showing all compose files and services

- Updated resource estimates and usage patterns

- Added migration guide references

- **Rewrote `docker/dev-environment/README.md`** (115 lines) - Simplified to redirect to root compose files

- Removed outdated 11-service documentation

- Clear usage instructions for new modular structure

- Links to comprehensive documentation

- **Archived 4 outdated Docker docs** to `archive/docker-old-docs/`:

- `DOCKER_INFRASTRUCTURE_AUDIT.md` (629 lines) - Referenced old 11-service setup

- `DOCKER_DEPLOYMENT_GUIDE.md` (1,021 lines) - Outdated deployment patterns

- `DOCKER_QUICK_REFERENCE.md` (492 lines) - Based on old structure

- `DOCKER_BEST_PRACTICES.md` (867 lines) - Referenced removed services

- **Created comprehensive audit documentation**:

- `DOCKER_INFRASTRUCTURE_CHANGES.md` - Complete migration guide (421 lines)

- `DOCKER_DEPLOYMENT_ANALYSIS.md` - Strategic analysis of deployment needs

- `DOCKER_AUDIT_EXECUTIVE_SUMMARY.md` - Executive summary of findings

- `DOCKER_DOCUMENTATION_AUDIT_REPORT.md` - Line-by-line documentation audit

- `DOCKER_SUBDIRECTORY_AUDIT_ACTION_PLAN.md` - Detailed action plan with code examples

- `DOCKER_CLEANUP_SUMMARY.md` - Summary of all cleanup operations

#### Error Handling Consolidation (Completed)

- **Completed monolithic tracker refactor** - Finished what was "In Progress"

- Migrated 67 of 101 files to centralized `internal/errors/` package

- Removed 8 deprecated error files from old locations

- Deleted 3 backup files polluting source tree

- Removed 1 empty directory (`internal/tracking/lifecycle/`)

- Zero broken references after refactor

- Health score: 87/100 (Production Ready)

- **Unified error taxonomy** across entire codebase

- Extended `internal/errors/errors.go` with 7 additional categories from WarningManager

- Refactored `internal/utils/warning_manager.go` to use `errors.StructuredError`

- Single source of truth for severity levels (Info/Warning/Error/Critical)

- Maintained backward compatibility via type aliases

#### Configuration & Testing Improvements

- **Updated configuration documentation**:
- Added missing plugins section to `docs/configuration.md`
- Added validation configuration details
- Added AI configuration examples
- Documented all third-party integration options

### Fixed - Critical Bugs (2025-10-18)

#### UUID Extension Detection Bug

- **Fixed SCHEMA_DIFF validation incorrectly requiring uuid-ossp extension**
- UUID datatype is built-in PostgreSQL since version 8.3 (no extension needed)
- Changed extension detection to only detect uuid-ossp when UUID _generation functions_ are used
- Updated `internal/validation/validator.go:1168-1247` to check for specific functions:
- `uuid_generate_v1()`, `uuid_generate_v1mc()`
- `uuid_generate_v3()`, `uuid_generate_v4()`, `uuid_generate_v5()`
- Removed incorrect `"uuid": "uuid-ossp"` alias mapping
- All validation modes (TWO_CONTAINERS, TWO_DATABASES, SCHEMA_DIFF) now correctly identify extension requirements

#### Test Script Fixes

- **Fixed Test 12.3.1 (Full Migration Workflow)**
- Removed invalid `--backup` and `--rollback` flags from `safe` command
- These features are built into the `safe` command workflow, not separate flags
- Test now passes successfully

### Added - Clarifications (2025-10-18)

#### Documentation Improvements

- **Added UUID clarification to `docker/init-scripts/init-db.sql`**

- Comprehensive comment explaining UUID datatype vs uuid-ossp extension

- Clear guidance on when uuid-ossp extension is actually needed

- Educational content for developers

- **Added docker-compose.testing.yml reference to `multi-version-test.sh`**

- Noted that users can also use modular testing compose for manual tests

- Clarified that script provides automated testing with more versions

#### API Server Documentation

- **Created `docker/api-server/README.md`** (321 lines)
- Complete API endpoint documentation
- GitHub webhook setup guide
- OAuth flow explanation
- Production deployment examples
- Security best practices
- Environment variable reference

### Removed - Cleanup (2025-10-18)

#### Duplicate Files

- **Removed `docker/validation/init-scripts/`** (entire directory)
- Complete byte-for-byte duplicate of `docker/init-scripts/`
- Canonical location (`docker/init-scripts/`) maintained

#### Redundant Services

- **Removed Redis from `docker/dev-environment/full-stack.yml`**
- Zero code integration confirmed (grep search across codebase)
- Removed service definition (21 lines)
- Removed redis-data volume
- Updated usage examples

### Changed - Refactoring (2025-10-16)

- **Internal Architecture**: Unified error taxonomy system across codebase
- Extended `internal/errors/errors.go` with 7 additional categories from WarningManager
- Refactored `internal/utils/warning_manager.go` to use `errors.StructuredError`
- Maintained backward compatibility via type aliases and deprecated markers
- Single source of truth for severity (Info/Warning/Error/Critical) and categories

### Added - Infrastructure (2025-10-16)

- Created subdirectory structure for tracking domain split: `lifecycle/`, `consolidation/`, `analysis/`, `recovery/`

### Detailed Commit History (v0.8.2-beta → v0.8.5-beta)

**23 commits from October 7-20, 2025** by Dominikos Pritis:

1. `9d88e83` - Update to v0.8.2-beta and improve cross-compilation (Oct 7, 08:32)
2. `ca8138c` - Use error-ignoring defer for resource cleanup (Oct 7, 08:41)
3. `f06e537` - Improve resource cleanup and error messages (Oct 7, 12:25)
4. `1a3f7d6` - Refactor conditional logic to use switch statements (Oct 7, 12:34)
5. `af2716f` - Refactor event operation checks to switch statements (Oct 7, 12:46)
6. `62dfe6c` - Disable multi-platform build job in CI workflow (Oct 7, 13:04)
7. `d61ac94` - Add architecture documentation and update references (Oct 7, 13:14)
8. `32e45ee` - Improve SQL consolidation and extension detection logic (Oct 7, 14:48)
9. `5ff8cfa` - Add validation config and Supabase auth.users stub (Oct 7, 16:19)
10. `5ccaf77` - Fix critical production-blocking bugs in AI workflows and consolidation (Oct 17, 12:18)
11. `87e7980` - Add CAPYSQUASH/GitHub integration and refactor docs (Oct 20, 09:24)
12. `4c7c3be` - Update .gitignore (Oct 20, 09:24)
13. `805a72f` - Merge branch ‘refactor’ (Oct 20, 09:25)
14. `93d26b8` - Update main.go (Oct 20, 09:26)
15. `a9e8afa` - Update .gitignore and add symlink for api-server (Oct 20, 09:29)
16. `b8d2de8` - Enable GitHub App multi-repo support and Azure OpenAI default (Oct 20, 10:24)
17. `b5fa2e7` - Improve documentation formatting and tables (Oct 20, 10:34)
18. `f370fbd` - Improve error handling and code robustness across modules (Oct 20, 11:26)
19. `c5d5473` - Update schema_comparator.go (Oct 20, 11:36)
20. `8099442` - Update circular_fk_handler.go (Oct 20, 11:38)
21. `40bd9cb` - Update release workflow and Docker image references (Oct 20, 11:42)
22. `b72c5c2` - Improve error handling and logging in API server (Oct 20, 11:55)
23. `dfe2b49` - Switch to docker-container driver in CI workflow (Oct 20, 11:58)

**Key themes**: Error handling consolidation, GitHub integration, Azure OpenAI support, documentation improvements, CI/CD refinements

---

## [0.8.2-beta] - 2025-10-07

### Added

#### Core Engine

- PostgreSQL parser integration using `pg_query_go/v6` for 100% accurate SQL parsing
- Multi-phase processing pipeline (Parse → Track → Analyze → Consolidate → Generate)
- Four safety levels: Paranoid, Conservative, Standard, and Aggressive
- Dependency-aware consolidation with automatic topological sorting
- Object lifecycle tracking across migrations

#### Plugin System

- Auto-discovery plugin architecture with priority-based execution
- **Clerk Plugin** (Priority: 95) - JWT v2 organization claims, helper function volatility markers
- **Supabase Plugin** (Priority: 90) - Auth schema detection, RLS policy consolidation, storage bucket policies
- **Prisma Plugin** (Priority: 75) - Migration table preservation, enum protection, VARCHAR(191) optimization
- **Drizzle Plugin** (Priority: 75) - IDENTITY column support, generated column preservation, modern SQL patterns
- Plugin extensibility with simple registration API

#### Validation System

- Three validation modes:
- `TWO_CONTAINERS` - Most accurate (separate containers)
- `TWO_DATABASES` - Best balance (shared container)
- `SCHEMA_DIFF` - Fastest (SQL diff comparison)
- Automatic PostgreSQL extension detection and installation
- Docker-based schema validation with isolation guarantees

#### AI Integration

- Multi-provider support (Claude/Anthropic, OpenAI, Azure OpenAI)
- Semantic function analysis and equivalence detection
- Dead code identification
- Authentication pattern recognition
- Performance optimization suggestions
- Optional opt-in features (requires API keys)

#### Performance

- Streaming mode for large migration sets (500+ files)
- Memory-efficient batch processing
- Configurable worker pools
- Progress tracking and throughput monitoring
- Automatic batch size calculation

#### Transformation System

- Function volatility marker injection (IMMUTABLE/STABLE/VOLATILE)
- Backup generation (schema-only, data-only, full)
- Rollback script generation
- SQL modernization (SERIAL → IDENTITY conversion)
- Auth pattern standardization

#### GitHub Integration

- Automated PR analysis via webhooks
- Bot commands: `/pgsquash analyze`, `/pgsquash consolidate`
- Auto-consolidation when migration threshold met
- Configurable via `.github/pgsquash.yml`
- API server for webhook handling

#### Standardized Workflows

- `safe` command - Production workflow (conservative, full validation, backups)
- `fast` command - Development workflow (balanced optimization, quick validation)
- `analyze-deep` command - Deep analysis without modifications

#### CLI Commands (historical snapshot for this release section)

- `analyze` - Analyze migrations without modifications
- `squash` - Consolidate migrations with configurable safety
- `validate` - Validate original vs squashed schemas
- `init-config` - Generate default configuration
- `ai-test` - Historical preview command (not part of current OSS runtime command surface)
- `ai-demo` - Historical preview command (not part of current OSS runtime command surface)
- `health` - Health check endpoint
- `version` - Display version information

#### Configuration

- Comprehensive JSON configuration system
- Auto-generation via `init-config`
- Per-plugin configuration options
- Performance tuning parameters
- Modern PostgreSQL feature toggles
- Third-party integration settings

### Documentation

- Comprehensive README with quick start guide
- Detailed CLI reference documentation
- Safety levels guide with use case recommendations
- Configuration reference with all options
- Architecture documentation with system design
- AI features guide
- GitHub integration setup guide
- Troubleshooting guide
- Docker deployment documentation
- Production deployment guide

### Changed

- Module renamed from initial structure to `github.com/capysquash/pgsquash-engine`
- Project reorganized following standard Go project layout
- Documentation restructured (public docs in `docs/`, internal in `docs/internal/`)

### Known Limitations

- No automated test coverage (manual testing only)
- Plugin system requires integration tests
- AI features require external API keys (with associated costs)
- Full validation requires Docker installation
- Some PostgreSQL extensions not available on all Platforms

### Technical Details

- **Language**: go 1.26.5+
- **PostgreSQL Support**: Versions 12-17
- **Key Dependencies**:
- `github.com/pganalyze/pg_query_go/v6` - PostgreSQL parser
- `github.com/spf13/cobra` - CLI framework
- `github.com/anthropics/anthropic-sdk-go` - Claude API
- `github.com/docker/docker` - Container validation
- `github.com/fatih/color` - Terminal output
- `github.com/lib/pq` - PostgreSQL driver

## Roadmap

### Phase 1 (Week 1)

- Test coverage >60%
- Example projects
- CI test enforcement

### Phase 2 (Week 2)

- Performance benchmarks
- Security audit
- Multi-Platform binaries

### Phase 3 (Week 3-4)

- Additional auth plugins (Auth0, NextAuth)
- Platform plugins (Neon, Railway)
- PostgreSQL 18 support
- 1.0.0 release

### Post-1.0.0 Features

- **Smart Split Feature** (v1.3.0) - Split squashed migrations into multiple organized files
- Category-based, dependency-level, and size-based splitting strategies
- Enables better code review, parallel migrations, and incremental deployment
- CLI: `pgsquash squash --split category` or `--split hybrid`

[Unreleased]: https://github.com/capydatabase/capysquash/compare/v1.5.0...HEAD
[1.5.0]: https://github.com/capydatabase/capysquash/compare/v1.4.0...v1.5.0
[1.4.0]: https://github.com/capydatabase/capysquash/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/capydatabase/capysquash/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/capydatabase/capysquash/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/capydatabase/capysquash/compare/v1.0.0...v1.1.0
[1.0.0]: https://github.com/capydatabase/capysquash/compare/v0.11.0...v1.0.0
[0.11.0]: https://github.com/capydatabase/capysquash/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/capydatabase/capysquash/compare/v0.9.7...v0.10.0
[0.9.7]: https://github.com/capydatabase/capysquash/compare/v0.8.5-beta...v0.9.7
[0.8.5-beta]: https://github.com/capydatabase/capysquash/compare/v0.8.2-beta...v0.8.5-beta
[0.8.2-beta]: https://github.com/capydatabase/capysquash/releases/tag/v0.8.2-beta
