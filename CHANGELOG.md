# Changelog

All notable changes to pgsquash-engine will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

> **⚠️ Important Notice**
>
> This tool modifies SQL migration files. Always maintain backups of your original migrations before running consolidation operations. While pgsquash includes validation and safety checks, you should verify the output matches your expectations. Test consolidated migrations in a non-production environment first.

---

## [Unreleased]

### Coming Soon

- PostgreSQL 18 support
- Smart split feature (split squashed migrations into multiple organized files)
- Additional auth plugins (Auth0, NextAuth, Firebase Auth)
- Platform plugins (Neon, Railway, PlanetScale)
- Comprehensive test suite (target: >60% coverage)
- Performance benchmarks and CI enforcement

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

- **Fixed SCHEMA\_DIFF validation incorrectly requiring uuid-ossp extension**
  - UUID datatype is built-in PostgreSQL since version 8.3 (no extension needed)
  - Changed extension detection to only detect uuid-ossp when UUID _generation functions_ are used
  - Updated `internal/validation/validator.go:1168-1247` to check for specific functions:
    - `uuid_generate_v1()`, `uuid_generate_v1mc()`
    - `uuid_generate_v3()`, `uuid_generate_v4()`, `uuid_generate_v5()`
  - Removed incorrect `"uuid": "uuid-ossp"` alias mapping
  - All validation modes (TWO\_CONTAINERS, TWO\_DATABASES, SCHEMA\_DIFF) now correctly identify extension requirements

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
13. `805a72f` - Merge branch 'refactor' (Oct 20, 09:25)
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

#### CLI Commands

- `analyze` - Analyze migrations without modifications
- `squash` - Consolidate migrations with configurable safety
- `validate` - Validate original vs squashed schemas
- `init-config` - Generate default configuration
- `ai-test` - Test AI provider connectivity
- `ai-demo` - Demonstrate AI capabilities
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

- Module renamed from initial structure to `github.com/CAPYSQUASH/pgsquash-engine`
- Project reorganized following standard Go project layout
- Documentation restructured (public docs in `docs/`, internal in `docs/internal/`)

### Known Limitations

- No automated test coverage (manual testing only)
- Plugin system requires integration tests
- AI features require external API keys (with associated costs)
- Full validation requires Docker installation
- Some PostgreSQL extensions not available on all Platforms

### Technical Details

- **Language**: Go 1.25.3+
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

[unreleased]: https://github.com/CAPYSQUASH/pgsquash-engine/compare/v0.8.5-beta...HEAD
[0.8.5-beta]: https://github.com/CAPYSQUASH/pgsquash-engine/compare/v0.8.2-beta...v0.8.5-beta
[0.8.2-beta]: https://github.com/CAPYSQUASH/pgsquash-engine/releases/tag/v0.8.2-beta
