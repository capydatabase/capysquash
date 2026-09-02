# pgsquash-engine

> A standalone open-source PostgreSQL migration consolidation engine

**Catalog-proven equivalence** via double-build validation. Intelligently reorganizes your migration history into clean, production-ready SQL-without breaking anything.

**Current version:** 0.9.7 (Beta) ⚠️

## ⚠️ Beta Release Status

Pgsquash-engine is currently in **beta** (v0.9.7) with active development toward v1.0.

### Production Use Recommendations

- ✅ **Use conservative safety modes** (`paranoid` or `conservative`) for production
- ✅ **Always validate** with `pgsquash validate` before applying to production
- ✅ **Test thoroughly** in staging environments
- ✅ **Backup production data** before applying any migrations
- ✅ **Review generated SQL** manually for critical databases

### What Works Well

- Core consolidation engine (5-phase pipeline)
- Safety levels and dependency resolution
- Docker-based schema validation
- Supabase, Clerk, Prisma, Drizzle plugin detection
- AST-based PostgreSQL parsing (pg_query_go)

### Known Limitations

- Test coverage expansion in progress
- Large migrations (500+ files) should use `--streaming` mode
- Some complex DDL edge cases may require manual review
- Catalog validation compares extensions, tables and columns, constraints,
  indexes, views, functions, triggers, RLS policies and roles, sequences,
  enum/composite/domain/range types, ownership, grants, and comments. PostgreSQL
  object classes outside this list still require manual review.
- **Streaming mode** does not run backup generation, rollback plan generation,
  SQL transformation, or paranoid database validation. Requesting
  `--backup`/`--rollback` or `--safety paranoid` together with streaming is
  rejected with an error instead of being silently skipped.

### Stability Promise

- **Public API** (`pkg/engine`) is stable - breaking changes will bump major version
- **CLI interface** is stable - flag changes will be deprecated first
- **Configuration format** stable - migration guides provided for any changes

We’re committed to a stable v1.0 release. **Questions?** [Open an issue](https://github.com/capysquash/pgsquash-engine/issues/new/choose)

---

## What is this?

Pgsquash-engine is both a CLI and a Go library for consolidating PostgreSQL
migration histories. CapyDB uses the binary as its schema-cleanup engine while
keeping database provisioning and managed validation in the CapyDB CLI.

## About pgsquash-engine

Intelligently consolidates and optimizes your migration history while preserving dependencies, respecting safety constraints, and validating every change. Works with your existing setup-Supabase projects, Prisma schemas, Clerk auth. No migration rewrites, no new syntax to learn. Just cleaner, safer SQL.

**Beta** (v0.9.7) with comprehensive validation, safety modes, and catalog-based equivalence checks - see the Beta Release Status section above for production-use recommendations.

## What it does

- **Intelligently consolidates** 100-300+ migration files into clean, organized output
- **Catalog-proven equivalence** via double-build validation-proves the output produces an identical schema by running both versions through PostgreSQL and comparing the results
- **Dependency-aware** processing that automatically resolves and orders statements safely
- **Safety-first** approach with multiple levels from paranoid (production) to aggressive (dev)
- **Schema validation** against your original schema using Docker containers
- **Deterministic static analysis** for quality/safety checks and reproducible harness inputs
- **Streaming architecture** for memory-efficient processing-processes migrations incrementally without loading entire history into memory (tested with 1000+ migration files)
- **Lock level analysis** with PostgreSQL transaction planning and conflict detection
- **Branch safety warnings** with git integration and protected branch enforcement
- **Manual override pragmas** (`-- pgsquash:ignore`) for complex edge cases
- **Auto-detection** of Supabase (RLS policies, storage), Clerk (JWT v2), Prisma, and Drizzle patterns

## Interactive mode

Pgsquash-engine includes a built-in TUI for a visual interface:

```bash

# Launch the dashboard (both commands work identically)

pgsquash tui migrations/
capysquash tui migrations/

# Or add --tui to any command

pgsquash analyze migrations/ --tui
pgsquash squash migrations/ --tui

# Jump to specific views

pgsquash tui analyze migrations/     # analysis

pgsquash tui config                  # settings

pgsquash tui deps migrations/        # dependency graph

```

The TUI gives you a dashboard with stats, live analysis, a config wizard, dependency visualization, and real-time progress tracking. Press `?` For keyboard shortcuts.

## Installation

Download a native archive from [GitHub Releases](https://github.com/capysquash/pgsquash-engine/releases), or build the CLI/library from source:

```bash

# As a Go library

go get github.com/capysquash/pgsquash-engine

# Or build from source

git clone https://github.com/capysquash/pgsquash-engine
cd pgsquash-engine
go build -o pgsquash cmd/pgsquash/main.go
```

## Quick Start

### Using pgsquash directly

**Building with Supabase or Clerk?**

```bash

# Auto-detects auth schemas, RLS policies, storage buckets

pgsquash analyze migrations/*.sql

# Preview consolidation (doesn't change files)

pgsquash squash migrations/*.sql --dry-run

# Consolidate and validate against your real schema

pgsquash squash migrations/*.sql --output clean/
```

### Using CapyDB-managed validation

CapyDB can validate with one isolated preview cell, so local Docker is not
required. The cell is reset between the original and candidate builds and is
deleted after the comparison.

```bash
capydb migrate squash migrations/ \
  --workflow safe \
  --validation capydb \
  --project my-project \
  --output clean/
```

> **Works with Supabase:** Auto-detects `auth.users`, `storage.buckets`, and RLS policies
> **Clerk-ready:** Preserves JWT v2 organization claims and user metadata

### Managing a team?

```bash

# Safe mode for production deploys

pgsquash safe migrations/*.sql --output production/

# Validate before merging PRs

pgsquash validate migrations/ clean/

```

### Working on multiple projects?

```bash

# Share config across team with version control

pgsquash init-config  # creates pgsquash.config.json

# Consistent squashing across projects

pgsquash squash migrations/*.sql  # uses config automatically

```

### Just need it to work?

```bash

# Five-minute setup

pgsquash analyze migrations/*.sql
pgsquash squash migrations/*.sql --dry-run
pgsquash squash migrations/*.sql --output clean/

# Advanced features

pgsquash squash migrations/*.sql --explain        # Show detailed consolidation plan (implies --dry-run)

pgsquash squash migrations/*.sql --branch-check   # Branch safety check

```

## Common workflows

```bash

# For production: safe and conservative

pgsquash safe migrations/*.sql --output production/

# For development: more aggressive optimization

pgsquash fast migrations/*.sql --output dev/

# Just analyze without changing anything

pgsquash analyze-deep migrations/*.sql
```

## Safety modes

Pick the mode that matches your risk tolerance:

| Mode         | When to use        | What it does      | Typical reduction |
| ------------ | ------------------ | ----------------- | ----------------- |
| Paranoid     | Production systems | Minimal changes   | 15-25%            |
| Conservative | Production         | Safe merges only  | 20-35%            |
| Standard     | Staging/testing    | Balanced approach | 35-50%            |
| Aggressive   | Local development  | Maximum cleanup   | 50-70%            |

## Example output

Here’s what consolidated SQL looks like:

```sql
-- Generated by pgsquash (standard mode)

-- === EXTENSIONS ===
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "vector";

-- === FOUNDATION ===
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) NOT NULL UNIQUE,
    name VARCHAR(255),
    status VARCHAR(50) DEFAULT 'active',
    created_at TIMESTAMP DEFAULT NOW()
);

-- === INDEXES ===
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_status ON users(status) WHERE status = 'active';

-- === SECURITY ===
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
CREATE POLICY users_select ON users FOR SELECT USING (true);
```

## Integration Scripts

Ready-to-use scripts for popular migration tools:

```bash

# Apply squashed migrations and mark as applied in Prisma

scripts/prisma-baseline.sh

# Reset Drizzle migrations and apply squashed versions

scripts/drizzle-reset.sh

# GitHub Actions workflow for automated validation

.github/workflows/pgsquash-validate.yml
```

See [scripts/README.md](scripts/README.md) for detailed usage and setup instructions.

## Documentation

- [Public Go API](pkg/engine/README.md)
- [Plugin development](internal/plugins/README.md)
- [Integration scripts](scripts/README.md)
- `pgsquash --help` for the current CLI contract

## Configuration

Generate a starter config file:

```bash
pgsquash init-config
```

Example `pgsquash.config.json`:

```json
{
  "safety_level": "standard",
  "output": {
    "format": "organized",
    "directory": "squashed"
  },
  "rules": {
    "table_operations": {
      "consolidate_create_alter": true,
      "remove_drop_create_cycles": true
    }
  },
  "performance": {
    "parallel_processing": true,
    "streaming": true
  }
}
```

The generated file documents every supported option and its default.

## Building from source

```bash

# Clone and build

git clone https://github.com/capysquash/pgsquash-engine
cd pgsquash-engine
go mod tidy
go build -o pgsquash cmd/pgsquash/main.go

# Run tests

go test ./...

# Try it out

./pgsquash analyze test_migrations/*.sql
```

The codebase is organized as:

```
cmd/
└── pgsquash/           # CLI entry point

internal/
├── parser/             # SQL parsing via pg_query_go
├── tracking/           # Object lifecycle tracking
├── squasher/           # Consolidation logic
├── validation/         # Catalog and Docker validation
├── plugins/            # Plugin system
└── transformation/     # SQL transformations

pkg/
└── engine/             # Public Go API for library usage

```

## License

MIT License - see LICENSE file.

## Links

- **GitHub**: <https://github.com/capysquash/pgsquash-engine>
- **Issues**: <https://github.com/capysquash/pgsquash-engine/issues>
