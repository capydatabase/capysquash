# pgsquash-engine - Library API

Use pgsquash as a Go library in your own applications.

## Installation

```bash
go get github.com/capysquash/pgsquash-engine
```

## Quick Start

```go
package main

import (
    "fmt"
    "log"

    "github.com/capysquash/pgsquash-engine/pkg/engine"
)

func main() {
    // Squash migrations in a directory
    result, err := engine.SquashDirectory("./migrations", nil)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Squashed %d files\n", result.FilesProcessed)
    fmt.Println(result.BaselineSQL)
}
```

## API Reference

### Configuration

#### `Config` struct

```go
type Config struct {
    SafetyLevel     SafetyLevel   // Conservative, Standard, Aggressive, Paranoid
    OutputFormat    OutputFormat  // FormatSingle, FormatSplit
    EnableStreaming bool          // Enable memory-efficient processing
    MemoryLimitMB   int          // Memory limit for streaming (default: 256)
    Verbose         bool          // Enable detailed logging
}
```

#### Safety Levels

- **`Conservative`** - Minimal consolidation, preserves most operations
- **`Standard`** - Balanced consolidation and safety (recommended)
- **`Aggressive`** - Maximum consolidation, suitable for development
- **`Paranoid`** - Preserves everything, minimal changes

#### `DefaultConfig()`

Returns a configuration with sensible defaults:

```go
config := engine.DefaultConfig()
// SafetyLevel: Standard
// OutputFormat: FormatSingle
// EnableStreaming: false
// MemoryLimitMB: 256
// Verbose: false
```

### Core Functions

#### `SquashDirectory(directory string, config *Config) (*SquashResult, error)`

Consolidates all `.sql` files in a directory.

**Parameters:**

- `directory` - Path to migrations directory
- `config` - Configuration (use `nil` for defaults)

**Returns:**

- `*SquashResult` - Results with SQL, warnings, and stats
- `error` - Error if squashing failed

**Example:**

```go
result, err := engine.SquashDirectory("./migrations", &engine.Config{
    SafetyLevel: engine.Conservative,
    Verbose: true,
})
if err != nil {
    log.Fatal(err)
}

fmt.Println(result.BaselineSQL)
fmt.Printf("Processed %d files\n", result.FilesProcessed)
fmt.Printf("Warnings: %v\n", result.Warnings)
```

#### `SquashFiles(migrations map[int]string, config *Config) (*SquashResult, error)`

Consolidates specific migration files.

**Parameters:**

- `migrations` - Map of migration order to file paths
- `config` - Configuration (use `nil` for defaults)

**Example:**

```go
migrations := map[int]string{
    1: "001_create_users.sql",
    2: "002_create_posts.sql",
    3: "003_add_indexes.sql",
}

result, err := engine.SquashFiles(migrations, nil)
```

#### `AnalyzeDirectory(directory string, config *Config) (*AnalysisResult, error)`

Analyzes migrations without making modifications.

**Parameters:**

- `directory` - Path to migrations directory
- `config` - Configuration (use `nil` for defaults)

**Returns:**

- `*AnalysisResult` - Analysis with redundancies, stats, and warnings
- `error` - Error if analysis failed

**Example:**

```go
analysis, err := engine.AnalyzeDirectory("./migrations", nil)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("Total files: %d\n", analysis.TotalFiles)
fmt.Printf("Total objects: %d\n", analysis.TotalObjects)
fmt.Printf("Redundancies: %d\n", len(analysis.Redundancies))

for _, r := range analysis.Redundancies {
    fmt.Printf("- %s: %s\n", r.ObjectName, r.Description)
}
```

### Result Types

#### `SquashResult`

```go
type SquashResult struct {
    BaselineSQL         string   // Consolidated SQL
    Warnings            []string // Warnings generated
    FilesProcessed      int      // Number of files processed
    ObjectsConsolidated int      // Number of objects consolidated
    ProcessingTime      string   // Duration of operation
}
```

#### `AnalysisResult`

```go
type AnalysisResult struct {
    TotalFiles      int                 // Number of files analyzed
    TotalStatements int                 // Total SQL statements
    TotalObjects    int                 // Total database objects
    Redundancies    []Redundancy        // Redundant operations
    ObjectsByType   map[string]int      // Object counts by type
    Warnings        []string            // Validation warnings
}
```

#### `Redundancy`

```go
type Redundancy struct {
    Type        string // Redundancy type
    ObjectName  string // Affected object
    Description string // Explanation
    Severity    string // "low", "medium", "high"
}
```

## Examples

### Example 1: Basic Squashing

```go
package main

import (
    "fmt"
    "log"
    "os"

    "github.com/capysquash/pgsquash-engine/pkg/engine"
)

func main() {
    result, err := engine.SquashDirectory("./migrations", nil)
    if err != nil {
        log.Fatal(err)
    }

    // Write to file
    err = os.WriteFile("squashed.sql", []byte(result.BaselineSQL), 0644)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("✅ Squashed %d migrations\n", result.FilesProcessed)
}
```

### Example 2: Custom Configuration

```go
package main

import (
    "fmt"
    "log"

    "github.com/capysquash/pgsquash-engine/pkg/engine"
)

func main() {
    config := &engine.Config{
        SafetyLevel:     engine.Conservative,
        OutputFormat:    engine.FormatSingle,
        EnableStreaming: false,
        Verbose:         true,
    }

    result, err := engine.SquashDirectory("./migrations", config)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.BaselineSQL)

    if len(result.Warnings) > 0 {
        fmt.Println("\nWarnings:")
        for _, w := range result.Warnings {
            fmt.Printf("  - %s\n", w)
        }
    }
}
```

### Example 3: Large Dataset with Streaming

```go
package main

import (
    "fmt"
    "log"

    "github.com/capysquash/pgsquash-engine/pkg/engine"
)

func main() {
    config := &engine.Config{
        SafetyLevel:     engine.Standard,
        EnableStreaming: true,
        MemoryLimitMB:   512,
        Verbose:         true,
    }

    result, err := engine.SquashDirectory("./large_migrations", config)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("Processed %d files with streaming\n", result.FilesProcessed)
    fmt.Printf("Processing time: %s\n", result.ProcessingTime)
}
```

### Example 4: Analysis Only

```go
package main

import (
    "fmt"
    "log"

    "github.com/capysquash/pgsquash-engine/pkg/engine"
)

func main() {
    analysis, err := engine.AnalyzeDirectory("./migrations", nil)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("📊 Migration Analysis\n")
    fmt.Printf("Files: %d\n", analysis.TotalFiles)
    fmt.Printf("Statements: %d\n", analysis.TotalStatements)
    fmt.Printf("Objects: %d\n", analysis.TotalObjects)

    fmt.Println("\nObjects by type:")
    for objType, count := range analysis.ObjectsByType {
        fmt.Printf("  %s: %d\n", objType, count)
    }

    if len(analysis.Redundancies) > 0 {
        fmt.Printf("\n⚠️  Found %d redundancies:\n", len(analysis.Redundancies))
        for _, r := range analysis.Redundancies {
            fmt.Printf("  [%s] %s: %s\n", r.Severity, r.ObjectName, r.Description)
        }
    }
}
```

### Example 5: Specific Files

```go
package main

import (
    "fmt"
    "log"

    "github.com/capysquash/pgsquash-engine/pkg/engine"
)

func main() {
    migrations := map[int]string{
        1: "migrations/001_create_schema.sql",
        2: "migrations/002_create_users.sql",
        3: "migrations/003_create_posts.sql",
        4: "migrations/004_add_indexes.sql",
    }

    config := &engine.Config{
        SafetyLevel: engine.Aggressive,
        Verbose:     true,
    }

    result, err := engine.SquashFiles(migrations, config)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Println(result.BaselineSQL)
}
```

### Example 6: Integration with CI/CD

```go
package main

import (
    "fmt"
    "log"
    "os"

    "github.com/capysquash/pgsquash-engine/pkg/engine"
)

func main() {
    // Analyze first
    analysis, err := engine.AnalyzeDirectory("./migrations", nil)
    if err != nil {
        log.Fatal(err)
    }

    // Fail CI if too many redundancies
    if len(analysis.Redundancies) > 10 {
        fmt.Printf("❌ Too many redundancies: %d (threshold: 10)\n", len(analysis.Redundancies))
        os.Exit(1)
    }

    // Squash if analysis passed
    result, err := engine.SquashDirectory("./migrations", &engine.Config{
        SafetyLevel: engine.Conservative,
    })
    if err != nil {
        log.Fatal(err)
    }

    // Write output
    err = os.WriteFile("squashed/migration.sql", []byte(result.BaselineSQL), 0644)
    if err != nil {
        log.Fatal(err)
    }

    fmt.Printf("✅ CI passed - %d files squashed\n", result.FilesProcessed)
}
```

## Error Handling

All functions return errors that should be checked:

```go
result, err := engine.SquashDirectory("./migrations", nil)
if err != nil {
    // Handle specific error types
    log.Printf("Squashing failed: %v", err)
    return err
}
```

## Performance Tips

### For Small Datasets (< 100 files)

```go
config := &engine.Config{
    SafetyLevel:     engine.Standard,
    EnableStreaming: false, // Faster for small datasets
}
```

### For Large Datasets (> 100 files)

```go
config := &engine.Config{
    SafetyLevel:     engine.Standard,
    EnableStreaming: true,  // Memory-efficient
    MemoryLimitMB:   512,   // Adjust based on available RAM
}
```

### For Development

```go
config := &engine.Config{
    SafetyLevel: engine.Aggressive, // Maximum consolidation
    Verbose:     true,               // See what's happening
}
```

### For Production

```go
config := &engine.Config{
    SafetyLevel: engine.Conservative, // Careful consolidation
}
```

## Thread Safety

The library is thread-safe for concurrent operations on different migration sets:

```go
// Safe: Different directories
go engine.SquashDirectory("./migrations1", nil)
go engine.SquashDirectory("./migrations2", nil)

// Safe: Different file sets
go engine.SquashFiles(migrations1, nil)
go engine.SquashFiles(migrations2, nil)
```

## Comparison: Library vs CLI

| Feature           | Library API              | CLI                |
| ----------------- | ------------------------ | ------------------ |
| **Use Case**      | Programmatic integration | Command-line usage |
| **Configuration** | Go structs               | JSON config file   |
| **Output**        | In-memory results        | Files on disk      |
| **Integration**   | Import as package        | Shell scripts      |
| **Flexibility**   | Full Go control          | Command flags      |

## Next Steps

- See [`pkg/cli`](../cli/README.md) for CLI API
- See [`pkg/plugins`](../plugins/README.md) for plugin system
- See [`pkg/utils`](../utils/README.md) for utilities

## Support

- 📖 [Full Documentation](https://capysquash.dev/docs)
- 🐛 [Report Issues](https://github.com/capysquash/pgsquash-engine/issues)
- 💬 [Discussions](https://github.com/capysquash/pgsquash-engine/discussions)
