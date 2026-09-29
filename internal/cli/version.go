package cli

import (
	"os"

	"github.com/capydatabase/capysquash/internal/buildinfo"
)

// SetVersionInfo records the build stamp from main's ldflags. Empty values
// keep the buildinfo fallback.
func SetVersionInfo(version, buildDate, gitCommit string) {
	buildinfo.Set(version, buildDate, gitCommit)
	rootCmd.Version = buildinfo.Version()
}

// defaultConfigFileName is the config file init-config writes and every
// command auto-loads from the working directory.
const defaultConfigFileName = "capysquash.config.json"

// resolveConfigPath resolves the config file to load: an explicit --config
// path wins; otherwise capysquash.config.json in the working directory is used
// when present. Returns "" when no config file is present so
// config.LoadConfig falls back to defaults.
func resolveConfigPath() string {
	if configPath != "" {
		return configPath
	}
	if info, err := os.Stat(defaultConfigFileName); err == nil && !info.IsDir() {
		return defaultConfigFileName
	}
	return ""
}
