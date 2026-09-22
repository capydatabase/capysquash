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

[Unreleased]: https://github.com/capydatabase/capysquash/compare/v1.0.0...HEAD
[1.0.0]: https://github.com/capydatabase/capysquash/compare/v0.11.0...v1.0.0
[0.11.0]: https://github.com/capydatabase/capysquash/compare/v0.10.0...v0.11.0
[0.10.0]: https://github.com/capydatabase/capysquash/compare/v0.9.7...v0.10.0
[0.9.7]: https://github.com/capydatabase/capysquash/compare/v0.8.5-beta...v0.9.7
[0.8.5-beta]: https://github.com/capydatabase/capysquash/compare/v0.8.2-beta...v0.8.5-beta
[0.8.2-beta]: https://github.com/capydatabase/capysquash/releases/tag/v0.8.2-beta
