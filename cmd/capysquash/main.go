package main

import (
	"os"

	"github.com/capydatabase/capysquash/internal/cli"
	"github.com/capydatabase/capysquash/internal/errors"
	"github.com/capydatabase/capysquash/internal/plugins/builtin"
	"github.com/capydatabase/capysquash/internal/utils"
)

// Version information, set via ldflags at build time:
//
//	-ldflags "-X main.version=x.y.z -X main.buildDate=... -X main.gitCommit=..."
var (
	version   = "dev"
	buildDate = "unknown"
	gitCommit = "unknown"
)

func init() {
	logLevel := utils.LogLevelInfo
	if os.Getenv("CAPYSQUASH_LOG_LEVEL") == "debug" {
		logLevel = utils.LogLevelDebug
	}
	// Diagnostics belong on stderr so machine-readable command output on
	// stdout remains parseable by callers such as the CapyDB CLI.
	utils.SetDefaultLogger(utils.NewLogger(logLevel, os.Stderr))

	cli.SetVersionInfo(version, buildDate, gitCommit)

	// Plugins must be registered before any command executes.
	if err := builtin.RegisterDefault(); err != nil {
		utils.GetDefaultLogger().WithPrefix("PLUGINS").Warn("Failed to register some plugins: %v", err)
	}
}

func main() {
	logger := utils.GetDefaultLogger()

	if err := cli.Execute(); err != nil {
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
