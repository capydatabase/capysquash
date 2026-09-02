package main

import (
	"os"

	// Use public API packages
	"github.com/capy-base/pgsquash-engine/pkg/cli"
	"github.com/capy-base/pgsquash-engine/pkg/errors"
	"github.com/capy-base/pgsquash-engine/pkg/plugins"
	"github.com/capy-base/pgsquash-engine/pkg/utils"
)

// pgsquash: The PostgreSQL migration consolidation engine
//
// Intelligently consolidates migration histories using parser-grade accuracy,
// dependency-aware processing, and safety-first validation strategies.

// Version information (set via ldflags during build)
var (
	version   = "0.9.7" // Default version, can be overridden via ldflags: -ldflags "-X main.version=x.y.z"
	buildDate = "unknown"
	gitCommit = "unknown"
)

func init() {
	// Initialize global logger (use INFO level by default, can be configured via env var)
	logLevel := utils.LogLevelInfo
	if os.Getenv("PGSQUASH_LOG_LEVEL") == "debug" {
		logLevel = utils.LogLevelDebug
	}
	// Diagnostics belong on stderr so machine-readable command output on
	// stdout remains parseable by callers such as the CapyDB CLI.
	logger := utils.NewLogger(logLevel, os.Stderr)
	utils.SetDefaultLogger(logger)

	// Set version information for CLI commands
	cli.SetVersionInfo(version, buildDate, gitCommit)

	// Register all available plugins at application startup
	// This must happen before any CLI commands execute
	registerPlugins()
}

func main() {
	logger := utils.GetDefaultLogger()

	if err := cli.Execute(); err != nil {
		// Use structured error if possible
		if structErr, ok := err.(*errors.StructuredError); ok {
			logger.Error("%s", structErr.Error())
			if structErr.Severity == errors.SeverityCritical {
				os.Exit(2)
			}
		} else {
			logger.Error("Command execution failed: %v", err)
		}
		os.Exit(1)
	}
}

// registerPlugins registers all built-in plugins with the global registry
// Plugins are registered early so they're available for auto-discovery during migration processing
func registerPlugins() {
	logger := utils.GetDefaultLogger().WithPrefix("PLUGINS")

	// Register all built-in plugins using the public API
	if err := plugins.RegisterDefault(); err != nil {
		logger.Warn("Failed to register some plugins: %v", err)
	}

	// Future custom plugins can be added here if the API is extended
	// For now, all built-in plugins are registered via RegisterDefault()
}
