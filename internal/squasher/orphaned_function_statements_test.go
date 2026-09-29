package squasher

import (
	"strings"
	"testing"

	"github.com/capydatabase/capysquash/internal/tracking"
	"github.com/capydatabase/capysquash/internal/utils"
)

// A COMMENT naming an unqualified function with its arguments targets the
// public overload the baseline creates; the safety net used to look the
// unqualified name up and remove the comment as orphaned.
func TestRemoveOrphanedFunctionStatementsKeepsUnqualifiedOverloads(t *testing.T) {
	e := &Engine{
		logger: utils.GetDefaultLogger().WithPrefix("TEST"),
		consolidationResults: map[string]*tracking.ConsolidationResult{
			"public.f(integer)::FUNCTION": {ConsolidatedSQL: "CREATE FUNCTION f(int) RETURNS int LANGUAGE sql AS 'select 1';"},
		},
	}
	sql := "CREATE FUNCTION f(int) RETURNS int LANGUAGE sql AS 'select 1';\nCOMMENT ON FUNCTION f(int) IS 'kept';\nCOMMENT ON FUNCTION f(text) IS 'orphaned';\n"

	got := e.removeOrphanedFunctionStatements(sql)

	if !strings.Contains(got, "'kept'") {
		t.Errorf("the comment on the existing overload was removed:\n%s", got)
	}
	if strings.Contains(got, "'orphaned'") {
		t.Errorf("the comment on the missing overload was kept:\n%s", got)
	}
}
