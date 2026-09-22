package cli

import (
	"os"
	"testing"
)

// TestResolveConfigPath covers config file resolution: an explicit --config
// path always wins, otherwise capysquash.config.json in the working directory
// is used when present, and nothing else is picked up.
func TestResolveConfigPath(t *testing.T) {
	tests := []struct {
		name         string
		files        []string // config files created in the temp working dir
		explicitPath string   // simulates --config
		want         string
	}{
		{
			name:         "explicit --config path wins over everything",
			files:        []string{"capysquash.config.json"},
			explicitPath: "custom.config.json",
			want:         "custom.config.json",
		},
		{
			name: "no config files resolves to empty (defaults)",
			want: "",
		},
		{
			name:  "capysquash.config.json in the working directory is picked up",
			files: []string{"capysquash.config.json"},
			want:  "capysquash.config.json",
		},
		{
			name:  "other config-looking files are ignored",
			files: []string{"other.config.json"},
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// resolveConfigPath stats the candidate relative to the working
			// directory and reads the package-global configPath.
			t.Chdir(t.TempDir())

			prevConfigPath := configPath
			configPath = tt.explicitPath
			t.Cleanup(func() { configPath = prevConfigPath })

			for _, f := range tt.files {
				if err := os.WriteFile(f, []byte("{}"), 0o644); err != nil {
					t.Fatalf("failed to write %s: %v", f, err)
				}
			}

			if got := resolveConfigPath(); got != tt.want {
				t.Errorf("resolveConfigPath() = %q, want %q", got, tt.want)
			}
		})
	}
}
