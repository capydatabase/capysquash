package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/capy-base/pgsquash-engine/internal/errors"
	"github.com/spf13/cobra"
)

// HealthStatus represents the simple health check response (API contract)
type HealthStatus struct {
	Status    string    `json:"status"`
	Version   string    `json:"version"`
	Docker    bool      `json:"docker"`
	Timestamp time.Time `json:"timestamp"`
}

// DetailedHealthStatus represents the detailed health check response
type DetailedHealthStatus struct {
	Status    string    `json:"status"`
	Version   string    `json:"version"`
	Timestamp time.Time `json:"timestamp"`
	System    struct {
		OS     string `json:"os"`
		Arch   string `json:"arch"`
		GoVer  string `json:"go_version"`
		NumCPU int    `json:"num_cpu"`
		NumGo  int    `json:"num_goroutines"`
	} `json:"system"`
	Docker struct {
		Available bool   `json:"available"`
		Reason    string `json:"reason,omitempty"`
	} `json:"docker"`
}

var (
	healthText     bool // JSON is now default, text is opt-in
	healthDetailed bool // Detailed output with system info
)

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Health check endpoint for container orchestration",
	Long: `Provides a health check endpoint suitable for Docker HEALTHCHECK,
Kubernetes liveness/readiness probes, and other orchestration tools.

Returns exit code 0 if healthy, non-zero otherwise.
Output format can be plain text or JSON.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Check Docker availability
		dockerAvailable := false
		dockerReason := ""
		if _, err := os.Stat("/var/run/docker.sock"); err == nil {
			dockerAvailable = true
		} else {
			dockerReason = "docker socket not found"
		}

		// Output format: JSON is default, plain text is opt-in
		if healthText {
			// Plain text output (opt-in via --text flag)
			fmt.Printf("Status: healthy\n")
			fmt.Printf("Version: %s\n", getVersion())
			fmt.Printf("Docker: %v\n", dockerAvailable)
			fmt.Printf("Timestamp: %s\n", time.Now().UTC().Format(time.RFC3339))
		} else if healthDetailed {
			// Detailed JSON output with system info
			detailedStatus := DetailedHealthStatus{
				Status:    "healthy",
				Version:   getVersion(),
				Timestamp: time.Now().UTC(),
			}

			// System info
			detailedStatus.System.OS = runtime.GOOS
			detailedStatus.System.Arch = runtime.GOARCH
			detailedStatus.System.GoVer = runtime.Version()
			detailedStatus.System.NumCPU = runtime.NumCPU()
			detailedStatus.System.NumGo = runtime.NumGoroutine()

			// Docker info
			detailedStatus.Docker.Available = dockerAvailable
			detailedStatus.Docker.Reason = dockerReason

			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(detailedStatus); err != nil {
				return errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation, "failed to encode health status", nil)
			}
		} else {
			// Simple JSON output (default - matches documented API contract)
			status := HealthStatus{
				Status:    "healthy",
				Version:   getVersion(),
				Docker:    dockerAvailable,
				Timestamp: time.Now().UTC(),
			}

			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			if err := encoder.Encode(status); err != nil {
				return errors.Wrap(err, errors.ErrorCodeValidationFailed, errors.CategoryValidation, "failed to encode health status", nil)
			}
		}

		return nil
	},
}

func init() {
	healthCmd.Flags().BoolVar(&healthText, "text", false, "Output in plain text format (default is JSON)")
	healthCmd.Flags().BoolVar(&healthDetailed, "detailed", false, "Include detailed system information in JSON output")
	rootCmd.AddCommand(healthCmd)
}

// SetVersionInfo sets the version information from build-time ldflags
var versionInfo = struct {
	version   string
	buildDate string
	gitCommit string
}{
	version:   "0.9.7", // Default fallback
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

// brandName tracks the active CLI branding. It drives command help text and
// the default config file name (pgsquash.config.json vs capysquash.config.json).
var brandName = "pgsquash"

// brandDefaultConfigName returns the config file name init-config writes for
// the active brand.
func brandDefaultConfigName() string {
	if brandName == "capysquash" {
		return "capysquash.config.json"
	}
	return "pgsquash.config.json"
}

// configFileCandidates returns config file names to auto-load, in priority
// order. The branded binary prefers its own config name but still honors
// pgsquash.config.json for compatibility.
func configFileCandidates() []string {
	if brandName == "capysquash" {
		return []string{"capysquash.config.json", "pgsquash.config.json"}
	}
	return []string{"pgsquash.config.json"}
}

// resolveConfigPath resolves the config file to load: an explicit --config
// path wins; otherwise the first existing brand candidate is used (with a
// warning if multiple candidates exist). Returns "" when no config file is
// present so config.LoadConfig falls back to defaults.
func resolveConfigPath() string {
	if configPath != "" {
		return configPath
	}

	var existing []string
	for _, candidate := range configFileCandidates() {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			existing = append(existing, candidate)
		}
	}

	if len(existing) > 1 {
		fmt.Fprintf(os.Stderr, "Warning: multiple config files found (%v); using %s\n", existing, existing[0])
	}
	if len(existing) > 0 {
		return existing[0]
	}
	return ""
}

// SetBrandName updates the CLI branding (called from main package for different binaries)
func SetBrandName(name string) {
	brandName = name
	if brandName == "capysquash" {
		rootCmd.Use = "capysquash"
		rootCmd.Short = "PostgreSQL migration consolidation"
		rootCmd.Long = `Consolidate PostgreSQL migration files into
clean, production-ready SQL while preserving data integrity, respecting
dependencies, and validating safety through the pgsquash engine.`

		// Update TUI command examples
		if tuiCmd != nil {
			tuiCmd.Long = `Launch the interactive terminal user interface (TUI) for CAPYSQUASH.

The TUI provides a visual interface for:
  ► Analyzing migrations and viewing lifecycle patterns
  ► Configuring squashing settings interactively
  ► Visualizing dependency graphs
  ► Monitoring squashing progress in real-time

Examples:
  capysquash tui migrations/
  capysquash tui
`
		}

		if initConfigCmd != nil {
			initConfigCmd.Long = `Create a default capysquash.config.json file with all available options.`
		}
	}
	// Default is pgsquash branding (already set in rootCmd initialization)
}

func getVersion() string {
	// Check environment variable override first (for testing/debugging)
	if envVersion := os.Getenv("PGSQUASH_VERSION"); envVersion != "" {
		return envVersion
	}
	return versionInfo.version
}
