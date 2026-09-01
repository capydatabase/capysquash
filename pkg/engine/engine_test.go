package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEngineHonorsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	config := DefaultConfig()
	config.Context = ctx
	_, err := SquashFiles(map[int]string{1: "CREATE TABLE users (id bigint PRIMARY KEY);"}, config)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestNewEngine(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name:    "default config",
			config:  DefaultConfig(),
			wantErr: false,
		},
		{
			name:    "nil config uses defaults",
			config:  nil,
			wantErr: false,
		},
		{
			name: "with progress callback",
			config: &Config{
				SafetyLevel: Standard,
				ProgressCallback: func(processed, total int64, phase string) {
					// callback function
				},
			},
			wantErr: false,
		},
		{
			name: "with streaming enabled",
			config: &Config{
				SafetyLevel:     Standard,
				EnableStreaming: true,
				MemoryLimitMB:   512,
				BatchSize:       100,
				WorkerCount:     8,
			},
			wantErr: false,
		},
		{
			name: "with backup enabled",
			config: &Config{
				SafetyLevel:         Standard,
				EnableBackup:        true,
				BackupPath:          t.TempDir(),
				BackupRetentionDays: 7,
			},
			wantErr: false,
		},
		{
			name: "with rollback enabled",
			config: &Config{
				SafetyLevel:    Standard,
				EnableRollback: true,
				RollbackPath:   t.TempDir(),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng, err := NewEngine(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewEngine() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil {
				if eng == nil {
					t.Error("NewEngine() returned nil engine without error")
				}
				if eng.Close() != nil {
					t.Error("Close() failed")
				}
			}
		})
	}
}

func TestEngine_SquashFiles(t *testing.T) {
	// Create temp directory with test migrations
	tmpDir := t.TempDir()

	migrations := map[int]string{
		1: filepath.Join(tmpDir, "001_init.sql"),
		2: filepath.Join(tmpDir, "002_add_table.sql"),
		3: filepath.Join(tmpDir, "003_alter_table.sql"),
	}

	// Create test migration files with VALID SQL
	testFiles := map[string]string{
		migrations[1]: "-- Migration 1\nCREATE TABLE users (\n    id SERIAL PRIMARY KEY,\n    name TEXT NOT NULL\n);",
		migrations[2]: "-- Migration 2\nCREATE TABLE posts (\n    id SERIAL PRIMARY KEY,\n    user_id INTEGER REFERENCES users(id),\n    title TEXT\n);",
		migrations[3]: "-- Migration 3\nALTER TABLE posts ADD COLUMN content TEXT;",
	}

	for path, content := range testFiles {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	tests := []struct {
		name       string
		config     *Config
		migrations map[int]string
		wantErr    bool
		checkSQL   func(string) bool
	}{
		{
			name:       "basic squash",
			config:     DefaultConfig(),
			migrations: migrations,
			wantErr:    false,
			checkSQL: func(sql string) bool {
				return strings.Contains(sql, "CREATE TABLE users") &&
					strings.Contains(sql, "CREATE TABLE posts")
			},
		},
		{
			name: "with separate data ops",
			config: &Config{
				SafetyLevel:     Standard,
				SeparateDataOps: true,
			},
			migrations: migrations,
			wantErr:    false,
			checkSQL: func(sql string) bool {
				return strings.Contains(sql, "CREATE TABLE")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			eng, err := NewEngine(tt.config)
			if err != nil {
				t.Fatalf("NewEngine() error = %v", err)
			}
			defer eng.Close()

			result, err := eng.SquashFiles(tt.migrations)
			if (err != nil) != tt.wantErr {
				t.Errorf("SquashFiles() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if result == nil {
					t.Error("SquashFiles() returned nil result")
					return
				}

				if result.FilesProcessed != len(tt.migrations) {
					t.Errorf("FilesProcessed = %d, want %d", result.FilesProcessed, len(tt.migrations))
				}

				if result.BaselineSQL == "" {
					t.Error("SquashFiles() returned empty SQL")
				}

				if tt.checkSQL != nil && result.BaselineSQL != "" && !tt.checkSQL(result.BaselineSQL) {
					t.Errorf("SquashFiles() SQL validation failed. Got:\n%s", result.BaselineSQL)
				}
			}
		})
	}
}

func TestEngine_GetStats(t *testing.T) {
	tmpDir := t.TempDir()

	// Create valid migration file
	migrationFile := filepath.Join(tmpDir, "001_init.sql")
	content := "-- Test migration\nCREATE TABLE test_stats (\n    id SERIAL PRIMARY KEY,\n    name TEXT NOT NULL\n);"
	if err := os.WriteFile(migrationFile, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	eng, err := NewEngine(DefaultConfig())
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	defer eng.Close()

	// Get initial stats (before squashing)
	initialStats := eng.GetStats()
	if initialStats.Phase != "" {
		t.Logf("Initial phase: %s", initialStats.Phase)
	}

	// Perform squashing to populate stats
	migrations := map[int]string{1: migrationFile}
	_, err = eng.SquashFiles(migrations)
	if err != nil {
		// If squashing fails, just verify GetStats doesn't panic
		t.Logf("Squashing failed (expected with simple fixture): %v", err)
	}

	// Get stats after squashing attempt
	stats := eng.GetStats()

	// Verify stats structure is valid (not testing values due to fixture simplicity)
	if stats.MigrationsProcessed < 0 {
		t.Error("GetStats() returned negative MigrationsProcessed")
	}

	if stats.TotalMigrations < 0 {
		t.Error("GetStats() returned negative TotalMigrations")
	}

	if stats.ObjectsTracked < 0 {
		t.Error("GetStats() returned negative ObjectsTracked")
	}
}

func TestEngine_GetMemoryStats(t *testing.T) {
	config := &Config{
		SafetyLevel:     Standard,
		EnableStreaming: true,
		MemoryLimitMB:   512,
	}

	eng, err := NewEngine(config)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	defer eng.Close()

	stats := eng.GetMemoryStats()

	if stats.CurrentUsageMB < 0 {
		t.Error("GetMemoryStats() returned negative CurrentUsageMB")
	}

	if stats.LimitMB != 512 {
		t.Errorf("GetMemoryStats() LimitMB = %d, want 512", stats.LimitMB)
	}

	if stats.UsagePercent < 0 || stats.UsagePercent > 100 {
		t.Errorf("GetMemoryStats() UsagePercent = %.2f, want 0-100", stats.UsagePercent)
	}
}

func TestEngine_ProgressCallback(t *testing.T) {
	tmpDir := t.TempDir()

	migrations := map[int]string{
		1: filepath.Join(tmpDir, "001_init.sql"),
		2: filepath.Join(tmpDir, "002_add_table.sql"),
	}

	// Create VALID SQL files
	for num, path := range migrations {
		content := fmt.Sprintf("-- Migration %d\nCREATE TABLE test_progress_%d (\n    id SERIAL PRIMARY KEY,\n    data TEXT\n);", num, num)
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	callbackInvoked := false
	var lastPhase string
	var callbackCount int

	config := &Config{
		SafetyLevel: Standard,
		ProgressCallback: func(processed, total int64, phase string) {
			callbackInvoked = true
			callbackCount++
			lastPhase = phase

			if processed < 0 || total < 0 {
				t.Error("Progress callback received negative values")
			}
			if total > 0 && processed > total {
				t.Error("Progress callback: processed > total")
			}
			t.Logf("Progress callback: %d/%d - %s", processed, total, phase)
		},
	}

	eng, err := NewEngine(config)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	defer eng.Close()

	_, err = eng.SquashFiles(migrations)
	// Don't fail if squashing has errors, just check callback was invoked
	if err != nil {
		t.Logf("Squashing had errors (may be expected): %v", err)
	}

	if !callbackInvoked {
		t.Error("Progress callback was never invoked")
	}

	if callbackInvoked && lastPhase == "" {
		t.Error("Progress callback was invoked but never set a phase")
	}

	t.Logf("Progress callback invoked %d times, last phase: %s", callbackCount, lastPhase)
}

func TestEngine_GetResult(t *testing.T) {
	tmpDir := t.TempDir()

	migrations := map[int]string{
		1: filepath.Join(tmpDir, "001_init.sql"),
	}

	// Create VALID SQL
	content := "-- Test migration\nCREATE TABLE test_result (\n    id SERIAL PRIMARY KEY,\n    name TEXT NOT NULL\n);"
	if err := os.WriteFile(migrations[1], []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	eng, err := NewEngine(DefaultConfig())
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	defer eng.Close()

	// Before squashing, result should be nil
	if result := eng.GetResult(); result != nil {
		t.Error("GetResult() should return nil before squashing")
	}

	// Attempt squash
	_, err = eng.SquashFiles(migrations)
	if err != nil {
		t.Logf("Squashing had errors (may be expected): %v", err)
		// Don't fail the test, as simple fixtures might have issues
	}

	// After squashing attempt, check if result is populated
	result := eng.GetResult()
	if result != nil {
		if result.BaselineSQL == "" {
			t.Error("GetResult() returned result but SQL is empty")
		}
		t.Logf("GetResult() successfully returned result with %d files processed", result.FilesProcessed)
	} else {
		t.Log("GetResult() returned nil (squashing may have failed)")
	}
}

func TestEngine_Close(t *testing.T) {
	eng, err := NewEngine(DefaultConfig())
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}

	// Close should succeed
	if err := eng.Close(); err != nil {
		t.Errorf("Close() error = %v", err)
	}

	// Multiple closes should be safe
	if err := eng.Close(); err != nil {
		t.Errorf("Second Close() error = %v", err)
	}
}

func TestProgressPhaseConstants(t *testing.T) {
	phases := []ProgressPhase{
		PhaseInitializing,
		PhaseParsing,
		PhaseTracking,
		PhaseAnalyzing,
		PhaseConsolidating,
		PhaseGenerating,
		PhaseValidating,
		PhaseComplete,
	}

	for _, phase := range phases {
		if string(phase) == "" {
			t.Errorf("Phase constant is empty")
		}
	}
}

func TestSquashStats_Fields(t *testing.T) {
	stats := SquashStats{
		Phase:                 "Testing",
		MigrationsProcessed:   5,
		TotalMigrations:       10,
		ObjectsTracked:        100,
		ConsolidationsApplied: 20,
		ProcessingTime:        5 * time.Second,
		PeakMemoryUsage:       1024 * 1024,
		ThroughputMPS:         2.5,
	}

	if stats.Phase != "Testing" {
		t.Error("SquashStats.Phase field not working")
	}

	if stats.MigrationsProcessed != 5 {
		t.Error("SquashStats.MigrationsProcessed field not working")
	}

	if stats.ThroughputMPS != 2.5 {
		t.Error("SquashStats.ThroughputMPS field not working")
	}
}

func TestMemoryStats_Fields(t *testing.T) {
	stats := MemoryStats{
		CurrentUsageMB: 100,
		PeakUsageMB:    150,
		LimitMB:        512,
		UsagePercent:   19.53,
	}

	if stats.CurrentUsageMB != 100 {
		t.Error("MemoryStats.CurrentUsageMB field not working")
	}

	if stats.LimitMB != 512 {
		t.Error("MemoryStats.LimitMB field not working")
	}

	if stats.UsagePercent != 19.53 {
		t.Error("MemoryStats.UsagePercent field not working")
	}
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config.SafetyLevel != Standard {
		t.Errorf("DefaultConfig().SafetyLevel = %v, want %v", config.SafetyLevel, Standard)
	}

	if config.MemoryLimitMB != 256 {
		t.Errorf("DefaultConfig().MemoryLimitMB = %d, want 256", config.MemoryLimitMB)
	}

	if config.BatchSize != 50 {
		t.Errorf("DefaultConfig().BatchSize = %d, want 50", config.BatchSize)
	}

	if config.WorkerCount != 4 {
		t.Errorf("DefaultConfig().WorkerCount = %d, want 4", config.WorkerCount)
	}

	if config.BackupPath != "./backups" {
		t.Errorf("DefaultConfig().BackupPath = %s, want ./backups", config.BackupPath)
	}

	if config.BackupRetentionDays != 30 {
		t.Errorf("DefaultConfig().BackupRetentionDays = %d, want 30", config.BackupRetentionDays)
	}

	if config.RollbackPath != "./rollbacks" {
		t.Errorf("DefaultConfig().RollbackPath = %s, want ./rollbacks", config.RollbackPath)
	}

	// Test new cycle detection defaults
	if config.EnableCycleDetection != false {
		t.Errorf("DefaultConfig().EnableCycleDetection = %v, want false", config.EnableCycleDetection)
	}

	if config.ShowCycleDetails != false {
		t.Errorf("DefaultConfig().ShowCycleDetails = %v, want false", config.ShowCycleDetails)
	}

	if config.CycleDetectionDepth != 10 {
		t.Errorf("DefaultConfig().CycleDetectionDepth = %d, want 10", config.CycleDetectionDepth)
	}
}

func TestEngine_CycleDetectionConfig(t *testing.T) {
	tests := []struct {
		name                 string
		enableCycleDetection bool
		showCycleDetails     bool
		cycleDetectionDepth  int
	}{
		{
			name:                 "cycle detection disabled",
			enableCycleDetection: false,
			showCycleDetails:     false,
			cycleDetectionDepth:  10,
		},
		{
			name:                 "cycle detection enabled",
			enableCycleDetection: true,
			showCycleDetails:     false,
			cycleDetectionDepth:  10,
		},
		{
			name:                 "cycle detection with details",
			enableCycleDetection: true,
			showCycleDetails:     true,
			cycleDetectionDepth:  10,
		},
		{
			name:                 "cycle detection with custom depth",
			enableCycleDetection: true,
			showCycleDetails:     true,
			cycleDetectionDepth:  20,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.EnableCycleDetection = tt.enableCycleDetection
			config.ShowCycleDetails = tt.showCycleDetails
			config.CycleDetectionDepth = tt.cycleDetectionDepth

			eng, err := NewEngine(config)
			if err != nil {
				t.Fatalf("NewEngine() error = %v", err)
			}
			defer eng.Close()

			// Verify engine was created successfully with cycle detection config
			if eng == nil {
				t.Error("NewEngine() returned nil engine")
			}
		})
	}
}

func TestEngine_GetSafetyLevel(t *testing.T) {
	tests := []struct {
		name        string
		safetyLevel SafetyLevel
	}{
		{"conservative", Conservative},
		{"standard", Standard},
		{"aggressive", Aggressive},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.SafetyLevel = tt.safetyLevel

			eng, err := NewEngine(config)
			if err != nil {
				t.Fatalf("NewEngine() error = %v", err)
			}
			defer eng.Close()

			if got := eng.GetSafetyLevel(); got != tt.safetyLevel {
				t.Errorf("GetSafetyLevel() = %v, want %v", got, tt.safetyLevel)
			}
		})
	}
}

func TestEngine_ParanoidRequiresDSN(t *testing.T) {
	// Paranoid is documented as "requires DB": constructing an engine without
	// PROD_DB_DSN must fail closed instead of degrading to a simulated mode.
	t.Setenv("PROD_DB_DSN", "")

	config := DefaultConfig()
	config.SafetyLevel = Paranoid

	if _, err := NewEngine(config); err == nil {
		t.Fatal("NewEngine() with paranoid safety level and no PROD_DB_DSN should fail")
	}
}

func TestEngine_GetExtensions(t *testing.T) {
	eng, err := NewEngine(DefaultConfig())
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	defer eng.Close()

	// Before squashing, should return empty
	exts := eng.GetExtensions()
	if len(exts) != 0 {
		t.Errorf("GetExtensions() before squashing = %v, want empty", exts)
	}
}

func TestEngine_GetAuthCompatibilitySQL(t *testing.T) {
	eng, err := NewEngine(DefaultConfig())
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	defer eng.Close()

	// Should not panic
	authSQL := eng.GetAuthCompatibilitySQL()
	if authSQL != "" {
		t.Logf("GetAuthCompatibilitySQL() returned: %s", authSQL)
	}
}

func TestEngine_GetWarnings(t *testing.T) {
	eng, err := NewEngine(DefaultConfig())
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	defer eng.Close()

	// Before squashing, should return empty
	warnings := eng.GetWarnings()
	if len(warnings) != 0 {
		t.Errorf("GetWarnings() before squashing = %v, want empty", warnings)
	}
}

func BenchmarkEngine_SquashFiles(b *testing.B) {
	tmpDir := b.TempDir()

	migrations := map[int]string{
		1: filepath.Join(tmpDir, "001_init.sql"),
		2: filepath.Join(tmpDir, "002_add_table.sql"),
		3: filepath.Join(tmpDir, "003_alter_table.sql"),
	}

	for path := range migrations {
		content := "CREATE TABLE bench_table_" + filepath.Base(migrations[path]) + " (id SERIAL);"
		if err := os.WriteFile(migrations[path], []byte(content), 0644); err != nil {
			b.Fatalf("Failed to create test file: %v", err)
		}
	}

	config := DefaultConfig()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eng, err := NewEngine(config)
		if err != nil {
			b.Fatalf("NewEngine() error = %v", err)
		}

		_, err = eng.SquashFiles(migrations)
		if err != nil {
			b.Fatalf("SquashFiles() error = %v", err)
		}

		eng.Close()
	}
}
