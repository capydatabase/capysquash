# TUI Package - Public API for pgsquash Terminal User Interface

The `pkg/tui` package provides the Terminal User Interface (TUI) implementation for pgsquash and external CLI wrappers.

## Overview

This package provides a complete, production-ready TUI with:

- ✅ Interactive migration analysis
- ✅ Real-time dependency visualization
- ✅ Configuration wizard
- ✅ Progress monitoring
- ✅ Keyboard-driven navigation

**As of version 0.9.7+**, the TUI implementation is fully public (moved from `internal/tui` to `pkg/tui`), making it easy to integrate into any Go application or build custom TUI-based tools.

## Installation

```bash
go get github.com/capysquash/pgsquash-engine/pkg/tui
```

## Quick Start

### Simple Usage

The easiest way to launch the TUI:

```go
package main

import (
    "log"
    "github.com/capysquash/pgsquash-engine/pkg/tui"
)

func main() {
    if err := tui.Launch("./migrations", "pgsquash.config.json"); err != nil {
        log.Fatal(err)
    }
}
```

### Advanced Usage

For more control, create the model directly and use custom Bubbletea options:

```go
package main

import (
    "log"
    "github.com/capysquash/pgsquash-engine/pkg/tui"
    tea "github.com/charmbracelet/bubbletea"
)

func main() {
    // Create TUI model directly
    model := tui.NewModel("./migrations", "pgsquash.config.json")

    // Custom Bubbletea options
    opts := []tea.ProgramOption{
        tea.WithAltScreen(),
        tea.WithMouseCellMotion(),
    }

    // Run with custom options
    p := tea.NewProgram(model, opts...)
    if _, err := p.Run(); err != nil {
        log.Fatalf("TUI error: %v", err)
    }
}
```

### Direct View Navigation

Launch directly into a specific view:

```go
package main

import (
    "log"
    "github.com/capysquash/pgsquash-engine/pkg/tui"
)

func main() {
    // Launch directly into analysis view
    err := tui.LaunchWithView(
        "./migrations",
        "",
        tui.ViewAnalysis,
    )
    if err != nil {
        log.Fatal(err)
    }
}
```

## Available Views

| View                 | Constant                  | Description                                         |
| -------------------- | ------------------------- | --------------------------------------------------- |
| **Dashboard**        | `tui.ViewDashboard`       | Main dashboard with quick actions and overview      |
| **Analysis**         | `tui.ViewAnalysis`        | Detailed migration analysis with lifecycle patterns |
| **Configuration**    | `tui.ViewConfig`          | Interactive configuration wizard                    |
| **Dependency Graph** | `tui.ViewDependencyGraph` | Visual dependency graph visualization               |
| **Progress**         | `tui.ViewProgress`        | Real-time operation progress tracking               |
| **Validation**       | `tui.ViewValidation`      | Schema validation results                           |
| **Help**             | `tui.ViewHelp`            | Keyboard shortcuts and help documentation           |

## API Reference

### Core Functions

#### `NewModel(migrationDir, configPath string) *Model`

Creates a new TUI model. This is the primary way to create a TUI instance.

```go
model := tui.NewModel("./migrations", "pgsquash.config.json")
```

#### `Launch(migrationDir, configPath string) error`

Convenience function that creates and runs a TUI in one step with default settings.

```go
if err := tui.Launch("./migrations", "pgsquash.config.json"); err != nil {
    log.Fatal(err)
}
```

#### `LaunchWithView(migrationDir, configPath string, view ViewType) error`

Convenience function that creates and runs a TUI, immediately navigating to the specified view.

```go
// Launch directly into analysis view
if err := tui.LaunchWithView("./migrations", "", tui.ViewAnalysis); err != nil {
    log.Fatal(err)
}
```

### Types

#### `Model`

```go
type Model struct {
    // Public fields and methods - see model.go
}
```

The main TUI application model. Implements the Bubbletea `tea.Model` interface.

#### `ViewType`

```go
type ViewType = viewtypes.ViewType
```

Represents different views in the TUI. Use the exported constants (`ViewDashboard`, `ViewAnalysis`, `ViewValidation`, etc.).

#### Custom Bubbletea Options

`Model` implements the Bubbletea `tea.Model` interface, so advanced users run
it directly through `tea.NewProgram` with any program options:

```go
model := tui.NewModel("./migrations", "pgsquash.config.json")

opts := []tea.ProgramOption{
    tea.WithAltScreen(),
    tea.WithMouseCellMotion(),
}

p := tea.NewProgram(model, opts...)
if _, err := p.Run(); err != nil {
    log.Fatal(err)
}
```

## Integration Examples

### Cobra CLI Integration

```go
package main

import (
    "fmt"
    "os"

    "github.com/capysquash/pgsquash-engine/pkg/tui"
    "github.com/spf13/cobra"
)

var tuiCmd = &cobra.Command{
    Use:   "tui [migrations-dir]",
    Short: "Launch interactive TUI",
    RunE:  runTUI,
}

func runTUI(cmd *cobra.Command, args []string) error {
    migrationDir := "."
    if len(args) > 0 {
        migrationDir = args[0]
    }

    // Verify directory exists
    if _, err := os.Stat(migrationDir); err != nil {
        if os.IsNotExist(err) {
            return fmt.Errorf("migration directory does not exist: %s", migrationDir)
        }
        return fmt.Errorf("failed to access migration directory: %w", err)
    }

    configPath, _ := cmd.Flags().GetString("config")
    return tui.Launch(migrationDir, configPath)
}
```

### Fallback to Non-Interactive

```go
func runAnalysis(migrationDir string) error {
    // Try interactive TUI first
    if isTTY() {
        if err := tui.Launch(migrationDir, ""); err == nil {
            return nil
        }
    }

    // Fallback to non-interactive mode
    return runNonInteractiveAnalysis(migrationDir)
}

func isTTY() bool {
    stat, _ := os.Stdout.Stat()
    return (stat.Mode() & os.ModeCharDevice) != 0
}
```

### With Error Recovery

```go
func launchTUIWithRecovery(migrationDir string) error {
    defer func() {
        if r := recover(); r != nil {
            log.Printf("TUI panic recovered: %v", r)
            log.Println("Falling back to non-interactive mode...")
            runNonInteractiveMode()
        }
    }()

    return tui.Launch(migrationDir, "pgsquash.config.json")
}
```

## Keyboard Shortcuts

### Global Shortcuts (All Views)

| Key                | Action                       |
| ------------------ | ---------------------------- |
| `q`, `Ctrl+C`      | Quit the TUI                 |
| `?`                | Toggle help view             |
| `ESC`              | Return to dashboard          |
| `Tab`              | Cycle focus between elements |
| `↑`/`↓` or `j`/`k` | Navigate up/down             |
| `Enter`            | Select/confirm               |

### View-Specific Shortcuts

View-specific keyboard shortcuts are displayed in the help view (press `?` from any view).

## Terminal Requirements

The TUI requires:

- ✅ Terminal with ANSI color support
- ✅ Minimum terminal size: 80x24
- ✅ UTF-8 encoding support

The TUI automatically detects terminal capabilities and adjusts rendering for the best experience.

## Error Handling

The TUI returns errors for:

- Missing or inaccessible migration directories
- Invalid configuration files
- Terminal compatibility issues
- Internal TUI failures

Always check and handle errors appropriately:

```go
if err := tui.Launch("./migrations", ""); err != nil {
    log.Printf("TUI failed: %v", err)
    // Fallback to non-interactive mode
    runNonInteractive()
}
```

## Configuration

The TUI reads configuration from the specified config file (JSON format). If no config file exists, the TUI will use sensible defaults and offer to create one through the configuration wizard.

Example configuration structure:

```json
{
  "safety_level": "standard",
  "validation": {
    "mode": "TWO_DATABASES"
  }
}
```

## Thread Safety

⚠️ **Important**: The TUI `Model` is not thread-safe. Each instance should be used from a single goroutine. The underlying Bubbletea framework handles concurrency internally through its message passing system.

## Dependencies

This package uses:

- `github.com/charmbracelet/bubbletea` - TUI framework
- `github.com/charmbracelet/lipgloss` - Styling and layout

These dependencies are handled automatically through the internal implementation and are not exposed in the public API.

## Examples

See the [examples](./examples/) directory for complete, runnable examples:

- **[simple](./examples/simple/main.go)** - Basic TUI usage
- **[advanced](./examples/advanced/main.go)** - Cobra CLI integration with subcommands

Run examples:

```bash

# Simple example

go run pkg/tui/examples/simple/main.go ./migrations

# Advanced example (Cobra integration)

go run pkg/tui/examples/advanced/main.go tui ./migrations
go run pkg/tui/examples/advanced/main.go tui analyze ./migrations
```

## Architecture

```
pkg/tui/              # Fully public TUI implementation

├── api.go            # Convenience functions (Launch, LaunchWithView)

├── doc.go            # Package documentation

├── model.go          # TUI application model (Bubbletea)

├── types.go          # Type re-exports

├── styles/           # Visual styling with Lipgloss

├── views/            # Different TUI views (dashboard, analysis, etc.)

├── viewtypes/        # View type definitions and messages

└── examples/         # Usage examples

```

**Note:** As of version 0.9.7+, the entire TUI implementation is public. There is no longer an `internal/tui` package - everything needed to build, customize, or extend the TUI is in `pkg/tui`.

## What Changed (0.9.7+ Migration Notes)

If you were using the TUI before version 0.9.7:

**Before (0.8.x - 0.9.4):**

- TUI implementation was in `internal/tui` (not accessible)
- Public API in `pkg/tui` was a wrapper with `tui.New(tui.Options{...})`
- Limited ability to customize or extend

**Now (0.9.7+):**

- Entire TUI is in `pkg/tui` (fully accessible)
- Simplified API: `tui.NewModel()`, `tui.Launch()`, `tui.LaunchWithView()`
- Full access to all views, styles, and types
- Can build custom views or extend existing ones

## Support

- 📖 [Full Documentation](https://pkg.go.dev/github.com/capysquash/pgsquash-engine/pkg/tui)
- 🐛 [Report Issues](https://github.com/capysquash/pgsquash-engine/issues)
- 💬 [Discussions](https://github.com/capysquash/pgsquash-engine/discussions)

## License

MIT License - see [LICENSE](../../LICENSE) for details.
