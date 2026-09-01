package engine

import (
	"context"
	"fmt"
	"strings"

	internalparser "github.com/capysquash/pgsquash-engine/internal/parser"
	internaltypes "github.com/capysquash/pgsquash-engine/internal/types"
	harnesscontract "github.com/capysquash/pgsquash-engine/pkg/harness"
	publicvalidation "github.com/capysquash/pgsquash-engine/pkg/validation"
)

const DeterministicHarnessArtifactV1Version = "1.0.0"

// DeterministicHarnessArtifactV1 is the engine-owned output carried through the
// advisory harness. The model may make decisions about this artifact, but never
// creates or rewrites its SQL.
type DeterministicHarnessArtifactV1 struct {
	ArtifactVersion     string                        `json:"artifact_version"`
	BaselineSQL         string                        `json:"baseline_sql"`
	DataOperationsSQL   string                        `json:"data_operations_sql,omitempty"`
	DeterministicReport *DeterministicHarnessReportV1 `json:"deterministic_report"`
}

type DeterministicArtifactValidationV1 struct {
	Valid          bool   `json:"valid"`
	SchemaSQLHash  string `json:"schema_sql_hash"`
	StatementCount int    `json:"statement_count"`
	ValidationMode string `json:"validation_mode"`
}

func BuildDeterministicHarnessArtifact(
	result *SquashResult,
	report *DeterministicHarnessReportV1,
) (*DeterministicHarnessArtifactV1, error) {
	if result == nil {
		return nil, fmt.Errorf("result cannot be nil")
	}
	if report == nil {
		return nil, fmt.Errorf("deterministic report cannot be nil")
	}
	artifact := &DeterministicHarnessArtifactV1{
		ArtifactVersion:     DeterministicHarnessArtifactV1Version,
		BaselineSQL:         result.BaselineSQL,
		DataOperationsSQL:   result.DataOperationsSQL,
		DeterministicReport: report,
	}
	if _, err := ValidateDeterministicHarnessArtifact(context.Background(), artifact, nil); err != nil {
		return nil, err
	}
	return artifact, nil
}

// ValidateDeterministicHarnessArtifact verifies that the transported bytes,
// deterministic report and context all describe the same parseable SQL output.
// This is the final deterministic acceptance boundary for managed execution.
func ValidateDeterministicHarnessArtifact(
	ctx context.Context,
	artifact *DeterministicHarnessArtifactV1,
	harnessContext *harnesscontract.HarnessContextV1,
) (*DeterministicArtifactValidationV1, error) {
	if artifact == nil {
		return nil, fmt.Errorf("deterministic artifact is required")
	}
	if artifact.ArtifactVersion != DeterministicHarnessArtifactV1Version {
		return nil, fmt.Errorf("unsupported deterministic artifact version: %s", artifact.ArtifactVersion)
	}
	if strings.TrimSpace(artifact.BaselineSQL) == "" {
		return nil, fmt.Errorf("deterministic artifact baseline_sql is required")
	}
	if artifact.DeterministicReport == nil {
		return nil, fmt.Errorf("deterministic artifact report is required")
	}
	report := artifact.DeterministicReport
	if report.ReportVersion != DeterministicHarnessReportV1Version {
		return nil, fmt.Errorf("unsupported deterministic report version: %s", report.ReportVersion)
	}

	actualHash := hashString(artifact.BaselineSQL)
	actualStatementCount := countStatements(artifact.BaselineSQL)
	if report.Output.SchemaSQLHash != actualHash {
		return nil, fmt.Errorf("deterministic report schema hash does not match baseline_sql")
	}
	if report.Output.StatementCount != actualStatementCount {
		return nil, fmt.Errorf("deterministic report statement count does not match baseline_sql")
	}
	if strings.TrimSpace(artifact.DataOperationsSQL) == "" {
		if strings.TrimSpace(report.Output.DataOperationsHash) != "" {
			return nil, fmt.Errorf("deterministic report contains data operations hash without data_operations_sql")
		}
	} else if report.Output.DataOperationsHash != hashString(artifact.DataOperationsSQL) {
		return nil, fmt.Errorf("deterministic report data operations hash does not match data_operations_sql")
	}

	if harnessContext != nil {
		recomputedContextHash, err := harnesscontract.ComputeHarnessContextHash(harnessContext)
		if err != nil {
			return nil, fmt.Errorf("compute harness context hash: %w", err)
		}
		if strings.TrimSpace(harnessContext.ContextHash) == "" || harnessContext.ContextHash != recomputedContextHash {
			return nil, fmt.Errorf("harness context hash is missing or does not match context contents")
		}
		if harnessContext.SchemaState.SchemaSQLHash != actualHash {
			return nil, fmt.Errorf("harness context schema hash does not match baseline_sql")
		}
		if harnessContext.SchemaState.StatementCount != actualStatementCount {
			return nil, fmt.Errorf("harness context statement count does not match baseline_sql")
		}
	}

	migration, err := internalparser.ParseMigrationWithContext(ctx, artifact.BaselineSQL, "managed_harness_artifact.sql")
	if err != nil {
		return nil, fmt.Errorf("deterministic artifact SQL parse failed: %w", err)
	}
	validatorConfig := publicvalidation.DefaultConfig()
	validatorConfig.Level = publicvalidation.LevelBasic
	validatorConfig.ValidateDependencies = true
	validatorConfig.ValidateExpressions = true
	validatorConfig.ValidateConstraints = true
	validatorConfig.ValidatePermissions = false
	validatorConfig.ValidatePerformance = false
	validatorConfig.Verbose = false
	validator := publicvalidation.NewValidator(validatorConfig)
	validationResult, err := validator.ValidateMigrations(ctx, []*internaltypes.Migration{migration})
	if err != nil {
		return nil, fmt.Errorf("deterministic artifact validation failed: %w", err)
	}
	if !validationResult.Success || len(validationResult.Errors) > 0 {
		return nil, fmt.Errorf("deterministic artifact failed validation: %d error(s)", len(validationResult.Errors))
	}

	return &DeterministicArtifactValidationV1{
		Valid:          true,
		SchemaSQLHash:  actualHash,
		StatementCount: actualStatementCount,
		ValidationMode: "engine_basic",
	}, nil
}
