package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/capy-base/pgsquash-engine/internal/config"
	engineapi "github.com/capy-base/pgsquash-engine/pkg/engine"
)

// TestSquashFlagValidationEndToEnd drives the real cobra command tree and
// verifies that invalid flag combinations are hard errors (never silent
// no-ops) and that a rejected run creates no output directory.
func TestSquashFlagValidationEndToEnd(t *testing.T) {
	tests := []struct {
		name       string
		extraArgs  []string
		wantSubstr string
	}{
		{
			name:       "unknown --safety value is a hard error, not silent no-consolidation",
			extraArgs:  []string{"--dry-run", "--safety", "bananas"},
			wantSubstr: "invalid safety level",
		},
		{
			name:       "invalid --validation-mode is rejected",
			extraArgs:  []string{"--dry-run", "--validation-mode", "BOGUS"},
			wantSubstr: "invalid validation mode",
		},
		{
			name:       "--backup-path without --backup is rejected",
			extraArgs:  []string{"--i-know-what-im-doing", "--backup-path", "my-backups"},
			wantSubstr: "--backup-path requires --backup",
		},
		{
			name:       "--streaming with explicit --transform is rejected",
			extraArgs:  []string{"--dry-run", "--streaming", "--transform"},
			wantSubstr: "--transform is not supported in streaming mode",
		},
		{
			name:       "--streaming with --backup is rejected",
			extraArgs:  []string{"--i-know-what-im-doing", "--streaming", "--backup"},
			wantSubstr: "not supported in streaming mode",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			t.Setenv("PROD_DB_DSN", "")

			files := writeFixtureMigrations(t, filepath.Join(root, "migrations"))
			outDir := filepath.Join(root, "out")

			args := append([]string{"squash"}, files...)
			args = append(args, "--output", outDir)
			args = append(args, tt.extraArgs...)

			err := executeCLI(t, args...)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tt.wantSubstr)
			}
			if !strings.Contains(err.Error(), tt.wantSubstr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tt.wantSubstr)
			}

			if _, statErr := os.Stat(outDir); !os.IsNotExist(statErr) {
				t.Errorf("output directory %s must not exist after a rejected run (stat err: %v)", outDir, statErr)
			}
		})
	}
}

// TestApplySafetyOverride covers the --safety override seam directly.
func TestApplySafetyOverride(t *testing.T) {
	tests := []struct {
		name      string
		level     string
		wantErr   bool
		wantLevel string // expected cfg.SafetyLevel after the call
	}{
		{name: "empty flag keeps config value", level: "", wantErr: false, wantLevel: "standard"},
		{name: "conservative is applied", level: "conservative", wantErr: false, wantLevel: "conservative"},
		{name: "aggressive is applied", level: "aggressive", wantErr: false, wantLevel: "aggressive"},
		{name: "paranoid is applied", level: "paranoid", wantErr: false, wantLevel: "paranoid"},
		{name: "unknown level is rejected", level: "bananas", wantErr: true, wantLevel: "standard"},
		{name: "typo'd level is rejected", level: "standrd", wantErr: true, wantLevel: "standard"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			err := applySafetyOverride(cfg, tt.level)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("applySafetyOverride(%q) = nil, want error", tt.level)
				}
				if !strings.Contains(err.Error(), "invalid safety level") {
					t.Fatalf("error = %q, want it to contain %q", err.Error(), "invalid safety level")
				}
			} else if err != nil {
				t.Fatalf("applySafetyOverride(%q) = %v, want nil", tt.level, err)
			}
			if cfg.SafetyLevel != tt.wantLevel {
				t.Errorf("cfg.SafetyLevel = %q, want %q", cfg.SafetyLevel, tt.wantLevel)
			}
		})
	}
}

// TestApplyValidationModeOverride covers the --validation-mode override seam,
// including case/whitespace normalization.
func TestApplyValidationModeOverride(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		wantErr  bool
		wantMode string
	}{
		{name: "empty flag keeps config value", mode: "", wantErr: false, wantMode: "TWO_DATABASES"},
		{name: "TWO_CONTAINERS accepted", mode: "TWO_CONTAINERS", wantErr: false, wantMode: "TWO_CONTAINERS"},
		{name: "lowercase normalized", mode: "schema_diff", wantErr: false, wantMode: "SCHEMA_DIFF"},
		{name: "surrounding whitespace trimmed", mode: "  two_databases ", wantErr: false, wantMode: "TWO_DATABASES"},
		{name: "unknown mode rejected", mode: "THREE_CONTAINERS", wantErr: true, wantMode: "TWO_DATABASES"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Validation.Mode = "TWO_DATABASES"
			err := applyValidationModeOverride(cfg, tt.mode)
			if tt.wantErr && err == nil {
				t.Fatalf("applyValidationModeOverride(%q) = nil, want error", tt.mode)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("applyValidationModeOverride(%q) = %v, want nil", tt.mode, err)
			}
			if cfg.Validation.Mode != tt.wantMode {
				t.Errorf("cfg.Validation.Mode = %q, want %q", cfg.Validation.Mode, tt.wantMode)
			}
		})
	}
}

// TestEngineParseSafetyLevel covers the public safety-level parser
// (pkg/engine.ParseSafetyLevel) that API/Studio/CLI wrappers are documented to
// use: case-insensitive, whitespace-tolerant, and strict about unknown values.
func TestEngineParseSafetyLevel(t *testing.T) {
	valid := []struct {
		input string
		want  engineapi.SafetyLevel
	}{
		{"conservative", "conservative"},
		{"standard", "standard"},
		{"aggressive", "aggressive"},
		{"paranoid", "paranoid"},
		{"Standard", "standard"},
		{"CONSERVATIVE", "conservative"},
		{"AgGrEsSiVe", "aggressive"},
		{"  paranoid  ", "paranoid"},
		{"\tstandard\n", "standard"},
	}
	for _, tt := range valid {
		t.Run("valid/"+strings.TrimSpace(tt.input), func(t *testing.T) {
			got, err := engineapi.ParseSafetyLevel(tt.input)
			if err != nil {
				t.Fatalf("ParseSafetyLevel(%q) unexpected error: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseSafetyLevel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}

	invalid := []string{
		"",
		"   ",
		"bananas",
		"safe",
		"standard extra",
		"para noid",
		"conservative,standard",
	}
	for _, input := range invalid {
		t.Run("invalid/"+strings.ReplaceAll(input, " ", "_"), func(t *testing.T) {
			if _, err := engineapi.ParseSafetyLevel(input); err == nil {
				t.Errorf("ParseSafetyLevel(%q) = nil error, want rejection", input)
			}
		})
	}
}
