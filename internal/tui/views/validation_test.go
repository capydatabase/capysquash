package views

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/capydatabase/capysquash/internal/tui/viewtypes"
	"github.com/capydatabase/capysquash/internal/validation"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// failingRule stands in for a rule that errors on every input.
type failingRule struct{}

func (failingRule) Code() string                           { return "CSQ.TEST.ALWAYS_FAILS" }
func (failingRule) Name() string                           { return "always fails" }
func (failingRule) Category() validation.ViolationCategory { return validation.CategorySafety }
func (failingRule) Check(string, *pg_query.ParseResult) ([]validation.Violation, error) {
	return nil, errors.New("boom")
}

// A rule that errors must not hide the findings of the rules that ran: the
// view reports the error and every finding of the same file.
func TestValidationViewKeepsFindingsWhenARuleFails(t *testing.T) {
	validation.RegisterRule(failingRule{})

	dir := t.TempDir()
	migration := "CREATE TABLE accounts (id int PRIMARY KEY, name varchar(20));\nDROP TABLE accounts;\n"
	if err := os.WriteFile(filepath.Join(dir, "001_accounts.sql"), []byte(migration), 0o600); err != nil {
		t.Fatal(err)
	}

	view := NewValidationView(dir, filepath.Join(dir, "missing-config.json"))
	msg, ok := view.runValidation().(viewtypes.ValidationResultMsg)
	if !ok {
		t.Fatalf("runValidation returned %T", view.runValidation())
	}

	all := strings.Join(append(append([]string{}, msg.Errors...), msg.Warnings...), "\n")
	for _, want := range []string{"CSQ.TEST.ALWAYS_FAILS", "boom", "CSQ.BREAKING.DROP_TABLE", "CSQ.HYGIENE.PREFER_BIGINT"} {
		if !strings.Contains(all, want) {
			t.Errorf("result lacks %q:\n%s", want, all)
		}
	}
	if msg.Success {
		t.Error("a failing rule must fail the validation")
	}
}
