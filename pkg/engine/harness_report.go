package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const DeterministicHarnessReportV1Version = "1.0.0"

type DeterministicHarnessReportV1 struct {
	ReportVersion string    `json:"report_version"`
	GeneratedAt   time.Time `json:"generated_at"`
	EngineVersion string    `json:"engine_version"`
	SafetyLevel   string    `json:"safety_level"`

	Input struct {
		OriginalMigrationFiles int `json:"original_migration_files"`
	} `json:"input"`

	Output struct {
		SchemaSQLPath       string `json:"schema_sql_path,omitempty"`
		SchemaSQLHash       string `json:"schema_sql_hash"`
		StatementCount      int    `json:"statement_count"`
		DataOperationsHash  string `json:"data_operations_hash,omitempty"`
		WarningsCount       int    `json:"warnings_count"`
		ObjectsConsolidated int    `json:"objects_consolidated"`
		ProcessingTime      string `json:"processing_time"`
	} `json:"output"`

	Validation struct {
		Status string `json:"status"`
		Mode   string `json:"mode,omitempty"`
	} `json:"validation"`

	Analysis struct {
		Warnings        []string `json:"warnings,omitempty"`
		Recommendations []string `json:"recommendations,omitempty"`
	} `json:"analysis"`
}

type DeterministicHarnessReportOptions struct {
	OutputSQLPath           string
	EngineVersion           string
	OriginalMigrationFiles  int
	ValidationStatus        string
	ValidationMode          string
	AnalysisWarnings        []string
	AnalysisRecommendations []string
}

func BuildDeterministicHarnessReport(result *SquashResult, options DeterministicHarnessReportOptions) (*DeterministicHarnessReportV1, error) {
	if result == nil {
		return nil, fmt.Errorf("result cannot be nil")
	}

	report := &DeterministicHarnessReportV1{
		ReportVersion: DeterministicHarnessReportV1Version,
		GeneratedAt:   time.Now().UTC(),
		EngineVersion: options.EngineVersion,
		SafetyLevel:   strings.TrimSpace(result.ProvenanceInfoValueSafetyLevel()),
	}

	if report.EngineVersion == "" {
		report.EngineVersion = strings.TrimSpace(result.ProvenanceInfoValueVersion())
		if report.EngineVersion == "" {
			report.EngineVersion = "unknown"
		}
	}

	if report.SafetyLevel == "" {
		report.SafetyLevel = "standard"
	}

	report.Input.OriginalMigrationFiles = options.OriginalMigrationFiles
	if report.Input.OriginalMigrationFiles == 0 {
		report.Input.OriginalMigrationFiles = result.FilesProcessed
	}

	report.Output.SchemaSQLPath = strings.TrimSpace(options.OutputSQLPath)
	report.Output.SchemaSQLHash = hashString(result.BaselineSQL)
	report.Output.StatementCount = countStatements(result.BaselineSQL)
	report.Output.WarningsCount = len(result.Warnings)
	report.Output.ObjectsConsolidated = result.ObjectsConsolidated
	report.Output.ProcessingTime = result.ProcessingTime
	if strings.TrimSpace(result.DataOperationsSQL) != "" {
		report.Output.DataOperationsHash = hashString(result.DataOperationsSQL)
	}

	status := strings.TrimSpace(options.ValidationStatus)
	if status == "" {
		status = "unknown"
	}
	report.Validation.Status = status
	report.Validation.Mode = strings.TrimSpace(options.ValidationMode)

	if len(options.AnalysisWarnings) > 0 {
		report.Analysis.Warnings = dedupeNonEmpty(options.AnalysisWarnings)
	}
	if len(options.AnalysisRecommendations) > 0 {
		report.Analysis.Recommendations = dedupeNonEmpty(options.AnalysisRecommendations)
	}

	return report, nil
}

func WriteDeterministicHarnessReport(path string, report *DeterministicHarnessReportV1) error {
	if report == nil {
		return fmt.Errorf("report cannot be nil")
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("path cannot be empty")
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, body, 0o644)
}

func LoadDeterministicHarnessReport(path string) (*DeterministicHarnessReportV1, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var report DeterministicHarnessReportV1
	if err := json.Unmarshal(body, &report); err != nil {
		return nil, err
	}

	if report.ReportVersion != DeterministicHarnessReportV1Version {
		return nil, fmt.Errorf("unsupported deterministic harness report version: %s", report.ReportVersion)
	}

	return &report, nil
}

func (r *SquashResult) ProvenanceInfoValueVersion() string {
	if r == nil || r.ProvenanceInfo == nil {
		return ""
	}
	return strings.TrimSpace(r.ProvenanceInfo.Version)
}

func (r *SquashResult) ProvenanceInfoValueSafetyLevel() string {
	if r == nil || r.ProvenanceInfo == nil {
		return ""
	}
	return strings.TrimSpace(r.ProvenanceInfo.SafetyLevel)
}

func hashString(value string) string {
	h := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(h[:])
}

func countStatements(sql string) int {
	parts := strings.Split(sql, ";")
	count := 0
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			count++
		}
	}
	if count == 0 && strings.TrimSpace(sql) != "" {
		return 1
	}
	return count
}

func dedupeNonEmpty(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}
