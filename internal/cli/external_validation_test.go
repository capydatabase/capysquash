package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateExternalRequiresOneSnapshotMode(t *testing.T) {
	migrations := t.TempDir()
	t.Setenv("TEST_VALIDATION_DSN", "postgres://example.invalid/db")

	err := executeCLI(t,
		"validate-external", migrations,
		"--dsn-env", "TEST_VALIDATION_DSN",
		"--json",
	)
	if err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("expected snapshot mode error, got %v", err)
	}
}

func TestValidateExternalDoesNotExposeDSNInJSONError(t *testing.T) {
	migrations := t.TempDir()
	secretDSN := "postgres://secret-user:secret-password@127.0.0.1:1/db"
	t.Setenv("TEST_VALIDATION_DSN", secretDSN)

	var output bytes.Buffer
	previousOutput := rootCmd.OutOrStdout()
	rootCmd.SetOut(&output)
	t.Cleanup(func() { rootCmd.SetOut(previousOutput) })

	err := executeCLI(t,
		"validate-external", migrations,
		"--dsn-env", "TEST_VALIDATION_DSN",
		"--snapshot-output", filepath.Join(t.TempDir(), "snapshot.json"),
		"--json",
	)
	if err == nil {
		t.Fatal("expected connection failure")
	}
	if strings.Contains(output.String(), secretDSN) || strings.Contains(output.String(), "secret-password") {
		t.Fatalf("JSON result exposed the validation DSN: %s", output.String())
	}

	var result externalValidationResult
	if decodeErr := json.Unmarshal(output.Bytes(), &result); decodeErr != nil {
		t.Fatalf("decode JSON result: %v\n%s", decodeErr, output.String())
	}
	if result.ContractVersion != externalValidationContractVersion || result.Success {
		t.Fatalf("unexpected result: %+v", result)
	}
}
