# pgsquash Public API

This directory contains the public API packages for pgsquash-engine.

## Available Packages

### 📦 `pkg/cli` - CLI API

Programmatically run the pgsquash CLI from Go code.

```go
import "github.com/capysquash/pgsquash-engine/pkg/cli"

// Execute the CLI
cli.SetVersionInfo("0.9.7", "2025-10-21", "abc123")
cli.SetBrandName("capysquash")
plugins.RegisterDefault()

if err := cli.Execute(); err != nil {
    log.Fatal(err)
}
```

**Functions:**

- `Execute()` - Run the complete CLI
- `SetVersionInfo(version, buildDate, gitCommit)` - Set version metadata
- `SetBrandName(brandName)` - Set branding (pgsquash/capysquash)

**Use Cases:**

- Building custom CLI wrappers
- Creating branded distributions
- CI/CD integration

---

### 🔧 `pkg/engine` - Migration Engine API

Use pgsquash as a library for programmatic migration consolidation.

```go
import "github.com/capysquash/pgsquash-engine/pkg/engine"

// Squash migrations
result, err := engine.SquashDirectory("./migrations", nil)
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.BaselineSQL)
```

**Functions:**

- `SquashDirectory(dir, config)` - Squash all migrations in a directory
- `SquashFiles(migrations, config)` - Squash specific files
- `AnalyzeDirectory(dir, config)` - Analyze without squashing
- `DefaultConfig()` - Get default configuration

**Use Cases:**

- Custom migration tools
- Automated migration optimization
- Integration with existing build systems
- Batch processing workflows

[Full Documentation](./engine/README.md)

---

### 🔌 `pkg/plugins` - Plugin API

Register built-in plugins, and detect/check plugin compatibility for a set of
migrations using the exact plugin implementations and conflict-resolution
logic the squashing engine uses.

```go
import "github.com/capysquash/pgsquash-engine/pkg/plugins"

// Register all built-in plugins
if err := plugins.RegisterDefault(); err != nil {
    log.Printf("Plugin registration failed: %v", err)
}

// Detect applicable plugins from raw migration SQL
result, err := plugins.DetectPlugins(ctx, migrationSQL)

// Check compatibility (priority-based conflict resolution)
matrix, err := plugins.CheckCompatibility([]string{"supabase", "clerk"})
```

**Functions:**

- `RegisterDefault()` - Register all built-in plugins (Clerk, Supabase, Prisma, Drizzle)
- `DetectPlugins(ctx, migrations)` - Parse migrations and run each plugin's own detection
- `CheckCompatibility(pluginNames)` - Resolve conflicts by plugin priority (e.g. Clerk 95 excludes Supabase 90)
- `GetAvailablePlugins()` - Plugin metadata derived from the plugin instances

**Built-in Plugins:**

- **Clerk** - JWT v2 auth (priority 95)
- **Supabase** - RLS policies, storage, auth (priority 90)
- **Prisma** - ORM migrations (priority 75, conflicts with Drizzle)
- **Drizzle** - ORM migrations (priority 75, conflicts with Prisma)

**Use Cases:**

- CLI initialization
- Programmatic plugin detection
- Custom migration-tool wrappers

---

### 📝 `pkg/utils` - Utility API

Logging and utility functions for pgsquash integration.

```go
import "github.com/capysquash/pgsquash-engine/pkg/utils"

// Setup logging
logger := utils.NewLogger(utils.LogLevelInfo, os.Stdout)
utils.SetDefaultLogger(logger)

logger.Info("Application started")
```

**Types:**

- `Logger` - Structured logger
- `LogLevel` - Log verbosity levels

**Functions:**

- `NewLogger(level, output)` - Create logger
- `SetDefaultLogger(logger)` - Set global logger
- `GetDefaultLogger()` - Get global logger

**Log Levels:**

- `LogLevelDebug` - Detailed debugging
- `LogLevelInfo` - General information
- `LogLevelWarn` - Warnings
- `LogLevelError` - Errors
- `LogLevelFatal` - Fatal errors (exits)

**Use Cases:**

- Application logging
- Debug output
- Error reporting

---

## Quick Comparison

| Package        | Purpose                             | Use Case                                       |
| -------------- | ----------------------------------- | ---------------------------------------------- |
| **cli**        | Run CLI programmatically            | Custom CLI wrappers, branded binaries          |
| **engine**     | Library API for squashing           | Custom tools, automation, batch processing     |
| **plugins**    | Plugin registration & detection     | Platform integrations, detection endpoints     |
| **validation** | Schema & static SQL validation      | Local or caller-owned equivalence checks       |
| **errors**     | Structured error handling           | Severity/category-aware error reporting        |
| **tui**        | Terminal UI                         | Interactive squash/analyze/validate views      |
| **harness**    | AI harness context                  | Machine-readable engine context for AI tools   |
| **rules**      | Consolidation rule metadata         | Rule listing and configuration surfaces        |
| **utils**      | Logging utilities                   | Application logging, debugging                 |

## Installation

```bash

# Get the entire package

go get github.com/capysquash/pgsquash-engine

# Import specific packages

import "github.com/capysquash/pgsquash-engine/pkg/cli"
import "github.com/capysquash/pgsquash-engine/pkg/engine"
import "github.com/capysquash/pgsquash-engine/pkg/plugins"
import "github.com/capysquash/pgsquash-engine/pkg/utils"
```

## Examples

### Example 1: CLI Wrapper

```go
package main

import (
    "os"
    "github.com/capysquash/pgsquash-engine/pkg/cli"
    "github.com/capysquash/pgsquash-engine/pkg/plugins"
    "github.com/capysquash/pgsquash-engine/pkg/utils"
)

func main() {
    // Setup
    logger := utils.NewLogger(utils.LogLevelInfo, os.Stdout)
    utils.SetDefaultLogger(logger)

    // Configure
    cli.SetVersionInfo("0.9.7", "2025-10-21", "abc123")
    cli.SetBrandName("myapp")

    // Register plugins
    plugins.RegisterDefault()

    // Run CLI
    if err := cli.Execute(); err != nil {
        logger.Error("CLI failed: %v", err)
        os.Exit(1)
    }
}
```

### Example 2: Library Usage

```go
package main

import (
    "fmt"
    "log"
    "github.com/capysquash/pgsquash-engine/pkg/engine"
)

func main() {
    // Analyze migrations
    analysis, err := engine.AnalyzeDirectory("./migrations", nil)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Found %d redundancies\n", len(analysis.Redundancies))

    // Squash if needed
    if len(analysis.Redundancies) > 0 {
        result, err := engine.SquashDirectory("./migrations", &engine.Config{
            SafetyLevel: engine.Conservative,
        })
        if err != nil {
            log.Fatal(err)
        }

        fmt.Println("Squashed SQL:")
        fmt.Println(result.BaselineSQL)
    }
}
```

### Example 3: Custom Integration

```go
package main

import (
    "log"
    "github.com/capysquash/pgsquash-engine/pkg/engine"
    "github.com/capysquash/pgsquash-engine/pkg/plugins"
    "github.com/capysquash/pgsquash-engine/pkg/utils"
)

func main() {
    // Setup logging with custom prefix
    logger := utils.NewLogger(utils.LogLevelDebug, os.Stdout)
    utils.SetDefaultLogger(logger)

    logger.Info("Starting custom migration processor")

    // Register plugins for third-party detection
    if err := plugins.RegisterDefault(); err != nil {
        logger.Warn("Plugin setup: %v", err)
    }

    // Process migrations with streaming for large datasets
    config := &engine.Config{
        SafetyLevel:     engine.Standard,
        EnableStreaming: true,
        MemoryLimitMB:   512,
        Verbose:         true,
    }

    result, err := engine.SquashDirectory("./migrations", config)
    if err != nil {
        log.Fatal(err)
    }

    logger.Info("Processed %d files", result.FilesProcessed)
}
```

## Architecture

```
pkg/                      # Public API

├── cli/                  # CLI execution: Execute(), SetVersionInfo(), SetBrandName()

├── engine/               # Library API: SquashDirectory(), AnalyzeDirectory(), harness builders

├── plugins/              # Plugin registration, detection, compatibility

├── validation/           # Local/external schema validation + static SQL rules

├── errors/               # Structured error types, severities, categories

├── tui/                  # Terminal UI: Launch(), LaunchWithView(), NewModel()

├── harness/              # AI harness context types

├── rules/                # Consolidation rule metadata

└── utils/                # Logger, NewLogger(), etc.

internal/                 # Private implementation (not importable from other modules)

├── cli/                  # CLI implementation

├── parser/               # SQL parsing

├── squasher/             # Consolidation engine

├── tracking/             # Dependency tracking

└── ...                   # Other internal packages

```

## Why This Architecture?

### Public API (`pkg/`)

- ✅ Stable, versioned interface
- ✅ Documented for external use
- ✅ Safe to import in other projects
- ✅ Breaking changes are avoided

`pkg/` is the public API boundary. Some functionality (e.g. validation, the
TUI, harness builders) has its single implementation in `pkg/` and is consumed
by `internal/` packages — there is deliberately no duplication between the two
trees.

### Internal Implementation (`internal/`)

- ✅ Free to refactor
- ✅ Can change without breaking external code
- ✅ Not accessible from external projects
- ✅ Implementation details hidden

## Versioning

The public API follows semantic versioning:

- **Major** version: Breaking API changes
- **Minor** version: New features, backward compatible
- **Patch** version: Bug fixes

**Current version:** `v0.9.7`

## Support

- 🐛 [Report Issues](https://github.com/capysquash/pgsquash-engine/issues)
- 💬 [Discussions](https://github.com/capysquash/pgsquash-engine/discussions)

## License

MIT License - see LICENSE file for details.
