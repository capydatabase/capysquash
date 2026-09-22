package cli

import "os"

// versionInfo is set from build-time ldflags through SetVersionInfo.
var versionInfo = struct {
	version   string
	buildDate string
	gitCommit string
}{
	version:   "dev",
	buildDate: "unknown",
	gitCommit: "unknown",
}

// SetVersionInfo updates version information (called from main package)
func SetVersionInfo(version, buildDate, gitCommit string) {
	if version != "" {
		versionInfo.version = version
		rootCmd.Version = version
	}
	if buildDate != "" {
		versionInfo.buildDate = buildDate
	}
	if gitCommit != "" {
		versionInfo.gitCommit = gitCommit
	}
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
