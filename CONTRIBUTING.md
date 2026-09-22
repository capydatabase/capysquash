# Contributing to capysquash

Thanks for helping. This document covers setup, the shape of the code, and
what a good change looks like. Please read [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)
first.

## Setup

- Go per `go.mod`, plus a C toolchain (`pg_query_go` is cgo)
- Docker, for validation features and the Docker-backed tests
- `golangci-lint` v2 (optional; `make lint` falls back to `go vet`)

```bash
git clone https://github.com/capydatabase/capysquash
cd capysquash
make build
./capysquash analyze test-fixtures/rls_policies/original/*.sql
make check
```

`go test -race ./...` runs offline; tests that need Docker skip when no daemon
is reachable, and the `integration`-tagged tests under `internal/validation`
need a `DATABASE_URL`.

## Layout

```text
cmd/capysquash/            CLI entrypoint (version via ldflags, plugin registration)
internal/cli/            cobra commands: analyze, squash, validate, validate-external, lint, safe, fast, analyze-deep, init-config, tui
internal/parser/         pg_query_go parsing, normalization, statement analysis, pragmas
internal/tracking/       object lifecycles, dependency graph, consolidation rules (the source of truth)
internal/squasher/       the engine: dependency resolution, cycle handling, consolidation, provenance
internal/builder/        SQL generation and formatting
internal/postprocessing/ AST post-processing of generated SQL
internal/validation/     Docker and external-database validation, catalog snapshots, static lint rules
internal/plugins/        Supabase, Clerk, Prisma, Drizzle plugins (+ builtin/ registration)
internal/engine/         programmatic façade used by the fixture and fuzz suites
internal/tui/            bubbletea terminal UI
test-fixtures/           golden migration histories + fuzz tests
docker/                  validation PostgreSQL image and compose overlays
scripts/                 Prisma and Drizzle baseline helpers
```

The pipeline is parse → track → analyze → consolidate → generate. Two rules
follow from it:

1. Work on the AST and the tracker, never on SQL strings.
2. Framework-specific behaviour belongs in a plugin, not in the core.

The engine is consumed as a **binary**. There is no supported Go import path;
everything lives under `internal/`. The CapyDB CLI drives it through the
`validate-external` JSON contract (`capysquash.external-validation.v1`) and the
`squash` flags; changing either is a breaking change and needs a changelog
entry and a version bump.

## Making a change

- Open an issue first for anything beyond a small fix, so the approach can be
  agreed before you spend time on it.
- Add or update tests. Golden histories under `test-fixtures/` are the best
  place for consolidation behaviour; `internal/cli` has end-to-end command
  tests that run the real cobra tree.
- Keep `make check` green: `gofmt -s`, `golangci-lint`, `go test -race`.
- Update `README.md` for user-facing changes and add a line under
  `## [Unreleased]` in `CHANGELOG.md`.
- Commit messages follow Conventional Commits (`feat:`, `fix:`, `docs:`,
  `refactor:`, `test:`, `chore:`).

## Pull requests

Keep one change per PR, reference the issue, and say how you tested it. CI
runs build, vet, tests, the lint job and `validate-external` against
PostgreSQL 15 to 18.

## License

Contributions are licensed under the project's [MIT License](LICENSE).
