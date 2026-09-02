package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildDeterministicHarnessReport(t *testing.T) {
	result := &SquashResult{
		BaselineSQL:         "CREATE TABLE users(id int);ALTER TABLE users ADD COLUMN email text;",
		DataOperationsSQL:   "UPDATE users SET email = '';",
		Warnings:            []string{"warning-1", "warning-1", ""},
		FilesProcessed:      4,
		ObjectsConsolidated: 3,
		ProcessingTime:      "1.23s",
		ProvenanceInfo: &ProvenanceInfo{
			Version:     "0.9.7",
			SafetyLevel: "conservative",
		},
	}

	report, err := BuildDeterministicHarnessReport(result, DeterministicHarnessReportOptions{
		OutputSQLPath:           "./out/000_baseline.sql",
		ValidationStatus:        "passed",
		ValidationMode:          "TWO_DATABASES",
		AnalysisWarnings:        []string{"warning-1", "warning-1", "warning-2"},
		AnalysisRecommendations: []string{"rec-1", "rec-1"},
	})
	if err != nil {
		t.Fatalf("BuildDeterministicHarnessReport failed: %v", err)
	}

	if report.ReportVersion != DeterministicHarnessReportV1Version {
		t.Fatalf("unexpected report version: %s", report.ReportVersion)
	}
	if report.Output.StatementCount != 2 {
		t.Fatalf("expected 2 statements, got %d", report.Output.StatementCount)
	}
	if report.Output.SchemaSQLHash == "" || report.Output.DataOperationsHash == "" {
		t.Fatalf("expected output hashes to be populated")
	}
	if report.Validation.Status != "passed" {
		t.Fatalf("expected validation status passed, got %s", report.Validation.Status)
	}
	if len(report.Analysis.Warnings) != 2 {
		t.Fatalf("expected deduped warnings length 2, got %d", len(report.Analysis.Warnings))
	}
	if len(report.Analysis.Recommendations) != 1 {
		t.Fatalf("expected deduped recommendations length 1, got %d", len(report.Analysis.Recommendations))
	}
}

func TestWriteAndLoadDeterministicHarnessReport(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "harness-report-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	report := &DeterministicHarnessReportV1{
		ReportVersion: DeterministicHarnessReportV1Version,
		EngineVersion: "0.9.7",
		SafetyLevel:   "standard",
	}

	reportPath := filepath.Join(tmpDir, "deterministic-harness-report.v1.json")
	if err := WriteDeterministicHarnessReport(reportPath, report); err != nil {
		t.Fatalf("WriteDeterministicHarnessReport failed: %v", err)
	}

	loaded, err := LoadDeterministicHarnessReport(reportPath)
	if err != nil {
		t.Fatalf("LoadDeterministicHarnessReport failed: %v", err)
	}

	if loaded.ReportVersion != report.ReportVersion {
		t.Fatalf("report version mismatch: got %s want %s", loaded.ReportVersion, report.ReportVersion)
	}
}

func TestValidateDeterministicHarnessArtifactRejectsTampering(t *testing.T) {
	result := &SquashResult{BaselineSQL: "CREATE TABLE users (id bigint PRIMARY KEY);"}
	report, err := BuildDeterministicHarnessReport(result, DeterministicHarnessReportOptions{
		ValidationStatus: "engine_basic_passed",
		ValidationMode:   "engine_basic",
	})
	if err != nil {
		t.Fatalf("build report: %v", err)
	}
	artifact, err := BuildDeterministicHarnessArtifact(result, report)
	if err != nil {
		t.Fatalf("build artifact: %v", err)
	}
	artifact.BaselineSQL = "CREATE TABLE admins (id bigint PRIMARY KEY);"
	if _, err := ValidateDeterministicHarnessArtifact(context.Background(), artifact, nil); err == nil {
		t.Fatal("expected hash mismatch to reject tampered SQL")
	}
}
