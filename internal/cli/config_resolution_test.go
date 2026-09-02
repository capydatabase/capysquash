package cli

import (
	"os"
	"strings"
	"testing"
)

// TestResolveConfigPath covers config file resolution for both brands:
// an explicit --config path always wins, the capysquash brand prefers
// capysquash.config.json over pgsquash.config.json (warning when both exist),
// and the pgsquash brand never picks up capysquash.config.json.
func TestResolveConfigPath(t *testing.T) {
	const warnSubstr = "multiple config files found"

	tests := []struct {
		name         string
		brand        string
		files        []string // config files created in the temp working dir
		explicitPath string   // simulates --config
		want         string
		wantWarning  bool
	}{
		{
			name:         "explicit --config path wins over everything",
			brand:        "capysquash",
			files:        []string{"capysquash.config.json", "pgsquash.config.json"},
			explicitPath: "custom.config.json",
			want:         "custom.config.json",
		},
		{
			name:  "no config files resolves to empty (defaults)",
			brand: "pgsquash",
			want:  "",
		},
		{
			name:  "pgsquash brand picks pgsquash.config.json",
			brand: "pgsquash",
			files: []string{"pgsquash.config.json"},
			want:  "pgsquash.config.json",
		},
		{
			name:  "pgsquash brand ignores capysquash.config.json",
			brand: "pgsquash",
			files: []string{"capysquash.config.json"},
			want:  "",
		},
		{
			name:  "pgsquash brand with both files picks pgsquash.config.json without warning",
			brand: "pgsquash",
			files: []string{"capysquash.config.json", "pgsquash.config.json"},
			want:  "pgsquash.config.json",
		},
		{
			name:  "capysquash brand picks capysquash.config.json",
			brand: "capysquash",
			files: []string{"capysquash.config.json"},
			want:  "capysquash.config.json",
		},
		{
			name:  "capysquash brand falls back to pgsquash.config.json",
			brand: "capysquash",
			files: []string{"pgsquash.config.json"},
			want:  "pgsquash.config.json",
		},
		{
			name:        "capysquash brand prefers capysquash.config.json with warning when both exist",
			brand:       "capysquash",
			files:       []string{"capysquash.config.json", "pgsquash.config.json"},
			want:        "capysquash.config.json",
			wantWarning: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// resolveConfigPath stats candidates relative to the working
			// directory and reads the package-global brandName/configPath.
			t.Chdir(t.TempDir())

			prevBrand := brandName
			prevConfigPath := configPath
			brandName = tt.brand
			configPath = tt.explicitPath
			t.Cleanup(func() {
				brandName = prevBrand
				configPath = prevConfigPath
			})

			for _, f := range tt.files {
				if err := os.WriteFile(f, []byte("{}"), 0o644); err != nil {
					t.Fatalf("failed to write %s: %v", f, err)
				}
			}

			var got string
			stderr := captureStderr(t, func() {
				got = resolveConfigPath()
			})

			if got != tt.want {
				t.Errorf("resolveConfigPath() = %q, want %q", got, tt.want)
			}
			if tt.wantWarning && !strings.Contains(stderr, warnSubstr) {
				t.Errorf("expected stderr warning containing %q, got %q", warnSubstr, stderr)
			}
			if !tt.wantWarning && strings.Contains(stderr, warnSubstr) {
				t.Errorf("unexpected multiple-config warning on stderr: %q", stderr)
			}
		})
	}
}

// TestBrandDefaultConfigName pins the init-config filename per brand.
func TestBrandDefaultConfigName(t *testing.T) {
	tests := []struct {
		brand string
		want  string
	}{
		{brand: "pgsquash", want: "pgsquash.config.json"},
		{brand: "capysquash", want: "capysquash.config.json"},
		{brand: "somethingelse", want: "pgsquash.config.json"},
	}

	for _, tt := range tests {
		t.Run(tt.brand, func(t *testing.T) {
			prev := brandName
			brandName = tt.brand
			t.Cleanup(func() { brandName = prev })

			if got := brandDefaultConfigName(); got != tt.want {
				t.Errorf("brandDefaultConfigName() = %q, want %q", got, tt.want)
			}
		})
	}
}
