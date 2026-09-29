// Package buildinfo holds the version the capysquash binary was built as.
//
// main stamps it from its ldflags (-X main.version=...) through Set; every
// other package reads it from here, so nothing hardcodes a version literal.
package buildinfo

import (
	"runtime/debug"
	"strings"
)

var (
	version   string
	buildDate = "unknown"
	gitCommit = "unknown"
)

// Set records the build stamp. Empty values leave the current value alone.
func Set(v, date, commit string) {
	if v != "" {
		version = v
	}
	if date != "" {
		buildDate = date
	}
	if commit != "" {
		gitCommit = commit
	}
}

// Version returns the stamped version. Without an ldflags stamp it falls back
// to the module version Go records in the binary (go install ...@vX.Y.Z, or
// the VCS pseudo-version of a local build), and to "dev" when there is none.
func Version() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return strings.TrimPrefix(v, "v")
		}
	}
	return "dev"
}

// BuildDate returns the stamped build date, or "unknown".
func BuildDate() string { return buildDate }

// GitCommit returns the stamped commit, or "unknown".
func GitCommit() string { return gitCommit }
