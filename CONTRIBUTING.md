# Contributing to pgsquash-engine

Thank you for your interest in contributing to pgsquash-engine! This is the core technology that powers [CAPYSQUASH](https://capysquash.dev) and [capysquash-cli](https://github.com/CAPYSQUASH/capysquash-cli).

This document provides guidelines and instructions for contributing to the pgsquash-engine library.

## Code of Conduct

This project adheres to a Code of Conduct that all contributors are expected to follow. Please read [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md) before contributing.

## Getting Started

### Prerequisites

- Go 1.21 or higher
- Docker Desktop (for validation features)
- Git
- Basic understanding of PostgreSQL and SQL migrations

### Setting Up Your Development Environment

1. **Fork and Clone**
   ```bash
   git clone https://github.com/YOUR_USERNAME/pgsquash-engine.git
   cd pgsquash-engine
   ```

2. **Install Dependencies**
   ```bash
   go mod download
   ```

3. **Build the Project**
   ```bash
   go build -o pgsquash cmd/pgsquash/main.go
   ```

4. **Run Tests**
   ```bash
   go test ./...
   ```

5. **Try the CLI**
   ```bash
   ./pgsquash analyze migrations/*.sql
   ```

## How to Contribute

### Reporting Bugs

Before creating a bug report:
- Check the [existing issues](https://github.com/CAPYSQUASH/pgsquash-engine/issues) to avoid duplicates
- Collect relevant information (Go version, OS, Docker version, error messages)

When filing a bug report, include:
- **Clear title** describing the issue
- **Steps to reproduce** the problem
- **Expected behavior** vs **actual behavior**
- **Environment details** (OS, Go version, pgsquash version)
- **Sample SQL files** (if applicable and non-sensitive)
- **Error messages** and logs

### Suggesting Features

Feature requests are welcome! When suggesting a feature:
- **Check existing feature requests** to avoid duplicates
- **Explain the use case** - why would this feature be valuable?
- **Describe the expected behavior** in detail
- **Consider alternatives** you've thought about
- **Be open to discussion** - features may be refined through collaboration

### Contributing Code

#### Branch Naming

- `feature/description` - New features
- `fix/description` - Bug fixes
- `docs/description` - Documentation updates
- `refactor/description` - Code refactoring
- `test/description` - Test additions or improvements

#### Development Workflow

1. **Create an Issue** (if one doesn't exist)
   - Discuss the change before starting work
   - Get feedback from maintainers

2. **Create a Branch**
   ```bash
   git checkout -b feature/your-feature-name
   ```

3. **Make Your Changes**
   - Follow the [coding conventions](#coding-conventions)
   - Write or update tests
   - Update documentation as needed

4. **Test Your Changes**
   ```bash
   # Run all tests
   go test ./...

   # Run tests with race detector
   go test -race ./...

   # Test with coverage
   go test -cover ./...

   # Manual testing
   ./pgsquash squash examples/basic/*.sql --dry-run
   ```

5. **Commit Your Changes**
   ```bash
   git add .
   git commit -m "Add feature: description of your change"
   ```

6. **Push to Your Fork**
   ```bash
   git push origin feature/your-feature-name
   ```

7. **Create a Pull Request**
   - Use a clear, descriptive title
   - Reference related issues (e.g., "Fixes #123")
   - Describe what changed and why
   - Include testing details

### Commit Message Guidelines

Follow these conventions for commit messages:

```
<type>: <subject>

<body (optional)>

<footer (optional)>
```

**Types:**
- `feat` - New feature
- `fix` - Bug fix
- `docs` - Documentation changes
- `refactor` - Code refactoring
- `test` - Test additions or improvements
- `chore` - Build process or auxiliary tool changes

**Examples:**
```
feat: Add support for PostgreSQL 17 syntax

fix: Resolve circular dependency in FK detection

docs: Update configuration examples in README

test: Add integration tests for Supabase plugin
```

### Coding Conventions

#### Go Style

- Follow standard Go formatting: `go fmt ./...`
- Run `goimports` if available
- Use `golint` and `go vet` to catch common issues
- Follow [Effective Go](https://golang.org/doc/effective_go.html) guidelines

#### Naming Conventions

- **Exported** types/functions: `UpperCamelCase`
- **Private** types/functions: `lowerCamelCase`
- **Package names**: short, lowercase nouns (e.g., `parser`, `tracker`)
- **CLI flags**: `kebab-case` (e.g., `--dry-run`, `--safety-level`)
- **Config JSON keys**: `snake_case` (e.g., `safety_level`, `output_directory`)

#### Code Organization

- Keep functions focused and single-purpose
- Add comments for exported functions and types
- Document complex logic with inline comments
- Group related functionality into packages
- Prefer AST manipulation over string manipulation

#### Testing

- Place tests in `*_test.go` files alongside source code
- Use table-driven tests for multiple scenarios
- Aim for >= 60% code coverage
- Test edge cases and error conditions
- Use meaningful test names: `TestFunctionName_Scenario`

**Example Test:**
```go
func TestParser_ParseCreateTable(t *testing.T) {
    tests := []struct {
        name    string
        sql     string
        wantErr bool
    }{
        {
            name: "simple table",
            sql:  "CREATE TABLE users (id SERIAL PRIMARY KEY);",
            wantErr: false,
        },
        {
            name: "invalid syntax",
            sql:  "CREATE TABLE",
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            result, err := ParseSQL(tt.sql)
            if (err != nil) != tt.wantErr {
                t.Errorf("ParseSQL() error = %v, wantErr %v", err, tt.wantErr)
            }
        })
    }
}
```

### Documentation

When contributing, update relevant documentation:

- **README.md** - For user-facing feature changes
- **docs/** - For detailed documentation
- **Code comments** - For complex logic
- **CHANGELOG.md** - Will be updated by maintainers during release

## Project Architecture

Understanding the architecture will help you contribute effectively:

### Core Pipeline (5 Phases)

```
1. PARSING (internal/parser/)
   ↓ Uses pg_query_go for PostgreSQL AST

2. TRACKING (internal/tracking/)
   ↓ Tracks object lifecycles and dependencies

3. ANALYSIS (internal/squasher/)
   ↓ Resolves dependencies, detects cycles

4. CONSOLIDATION (internal/squasher/)
   ↓ Applies safety-level-appropriate rules

5. GENERATION (internal/builder/)
   ↓ Outputs organized SQL
```

### Key Packages

- **`internal/parser/`** - SQL parsing via pg_query_go
- **`internal/tracking/`** - Object lifecycle tracking (source of truth)
- **`internal/squasher/`** - Consolidation engine and rules
- **`internal/builder/`** - SQL generation with formatting
- **`internal/validation/`** - Docker-based schema validation
- **`internal/plugins/`** - Third-party integration system
- **`internal/cli/`** - CLI commands (Cobra-based)

### Important Patterns

1. **AST-First Processing** - Never manipulate raw SQL strings
2. **Tracker as Source of Truth** - Query tracker for object state
3. **Configuration-Driven** - Features should be configurable
4. **Plugin Integration** - Framework-specific logic goes in plugins

For detailed architecture documentation, see:
- [docs/architecture.md](docs/architecture.md)
- [docs/migration-consolidation-strategy.md](docs/migration-consolidation-strategy.md)
- [.github/copilot-instructions.md](.github/copilot-instructions.md)
- [AGENTS.md](AGENTS.md)

## Areas Where We Need Help

We especially welcome contributions in these areas:

### High Priority
- **Test Coverage** - Currently below 60%, need comprehensive tests
- **Documentation** - More examples and use case guides
- **Bug Fixes** - Check [issues labeled "bug"](https://github.com/CAPYSQUASH/pgsquash-engine/labels/bug)
- **Platform Support** - Testing on different operating systems

### Medium Priority
- **Plugin Development** - New auth providers (Auth0, Firebase, NextAuth)
- **Performance Optimization** - Profiling and optimization
- **Error Messages** - More helpful error messages and debugging info
- **CI/CD Improvements** - Enhanced testing and release automation

### Good First Issues
- Issues labeled [`good first issue`](https://github.com/CAPYSQUASH/pgsquash-engine/labels/good%20first%20issue)
- Documentation improvements
- Adding examples to `examples/` directory
- Writing tests for existing functionality

## Pull Request Process

1. **Update Documentation** - If your PR changes behavior, update docs
2. **Add Tests** - New features should include tests
3. **Run Tests Locally** - Ensure all tests pass before submitting
4. **Keep PRs Focused** - One feature/fix per PR when possible
5. **Respond to Feedback** - Be responsive to review comments
6. **Update CHANGELOG** - Maintainers will handle this during release

### PR Checklist

Before submitting a PR, verify:

- [ ] Code follows project conventions
- [ ] Tests pass locally (`go test ./...`)
- [ ] Tests added/updated for changes
- [ ] Documentation updated (if needed)
- [ ] Commit messages follow guidelines
- [ ] Branch is up to date with main
- [ ] No merge conflicts
- [ ] Changes tested manually with sample migrations

### Review Process

1. **Automated Checks** - CI will run tests and linters
2. **Maintainer Review** - A maintainer will review your code
3. **Feedback Loop** - Address any requested changes
4. **Approval** - Once approved, maintainers will merge
5. **Release** - Changes will be included in the next release

## Development Tips

### Quick Development Cycle

```bash
# Watch and rebuild on changes (using entr or similar)
ls **/*.go | entr -r go build -o pgsquash cmd/pgsquash/main.go

# Quick test with examples
./pgsquash squash examples/basic/*.sql --dry-run

# Validate with Docker
./pgsquash validate migrations/ squashed/
```

### Debugging

- Use `--debug` flag for verbose output
- Check `pgsquash.log` for detailed logs
- Use Go's built-in debugger (`dlv`)
- Add strategic `log.Printf()` statements

### Working with pg_query_go

The parser relies heavily on `pg_query_go`. Key resources:

- [pg_query_go documentation](https://github.com/pganalyze/pg_query_go)
- [PostgreSQL parser documentation](https://www.postgresql.org/docs/current/sql.html)
- Study existing parser code in `internal/parser/`

## Resources

- **Documentation**: [docs/](docs/)
- **Architecture**: [docs/architecture.md](docs/architecture.md)
- **CLI Reference**: [docs/cli-reference.md](docs/cli-reference.md)
- **Configuration**: [docs/configuration.md](docs/configuration.md)
- **Roadmap**: [docs/internal/roadmap/ROADMAP.md](docs/internal/roadmap/ROADMAP.md)

## Getting Help

- **GitHub Issues** - For bugs and feature requests
- **GitHub Discussions** - For questions and general discussion
- **Code Review** - Open a draft PR for early feedback

## License

By contributing to pgsquash, you agree that your contributions will be licensed under the project's [MIT License](LICENSE).

## Recognition

Contributors will be recognized in:
- GitHub contributors list
- Release notes (for significant contributions)
- Project documentation (as appropriate)

Thank you for contributing to pgsquash! 🎉
