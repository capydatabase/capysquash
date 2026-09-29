package consolidation

import (
	"errors"
	"testing"

	"github.com/capydatabase/capysquash/internal/tracking"
	"github.com/capydatabase/capysquash/internal/types"
)

// stubRule is a delegate with a fixed outcome that records whether it ran.
type stubRule struct {
	applies bool
	result  *tracking.ConsolidationResult
	err     error
	ran     bool
}

func (r *stubRule) CanApply(*tracking.ObjectLifecycle) bool { return r.applies }
func (r *stubRule) Risk() tracking.RiskLevel                 { return tracking.RiskLevelLow }
func (r *stubRule) Apply(*tracking.ObjectLifecycle, ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	r.ran = true
	return r.result, r.err
}

func twoStepTableLifecycle() *tracking.ObjectLifecycle {
	return &tracking.ObjectLifecycle{
		Key:  "public.users::TABLE",
		Name: "public.users",
		Type: types.TypeTable,
		History: []tracking.LifecycleEvent{
			{Operation: types.OpCreate, Statement: types.Statement{SQL: "CREATE TABLE users (id int)", Operation: types.OpCreate, ObjectType: types.TypeTable}},
			{Operation: types.OpAlter, Statement: types.Statement{SQL: "ALTER TABLE users ADD COLUMN name text", Operation: types.OpAlter, ObjectType: types.TypeTable}},
		},
	}
}

func TestErrorRecoveryRunsTheFirstApplicableDelegate(t *testing.T) {
	skipped := &stubRule{applies: false, result: &tracking.ConsolidationResult{ConsolidatedSQL: "SKIPPED;"}}
	empty := &stubRule{applies: true}
	winner := &stubRule{applies: true, result: &tracking.ConsolidationResult{ConsolidatedSQL: "CREATE TABLE users (id int, name text);"}}
	after := &stubRule{applies: true, result: &tracking.ConsolidationResult{ConsolidatedSQL: "LATER;"}}

	rule := NewErrorRecoveryRule(3, "conservative", true, skipped, empty, winner, after)
	result, err := rule.Apply(twoStepTableLifecycle(), nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.ConsolidatedSQL != "CREATE TABLE users (id int, name text);" {
		t.Fatalf("ConsolidatedSQL = %q, want the winning delegate's", result.ConsolidatedSQL)
	}
	if skipped.ran || !empty.ran || !winner.ran || after.ran {
		t.Fatalf("ran: skipped=%v empty=%v winner=%v after=%v", skipped.ran, empty.ran, winner.ran, after.ran)
	}
}

// A failing delegate is recovered from: conservative recovery replays the
// original statements instead of dropping the object or the error escaping.
func TestErrorRecoveryRecoversFromAFailingDelegate(t *testing.T) {
	failing := &stubRule{applies: true, err: errors.New("merge failed")}
	after := &stubRule{applies: true, result: &tracking.ConsolidationResult{ConsolidatedSQL: "LATER;"}}

	rule := NewErrorRecoveryRule(3, "conservative", true, failing, after)
	result, err := rule.Apply(twoStepTableLifecycle(), nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := "CREATE TABLE users (id int);\nALTER TABLE users ADD COLUMN name text;"
	if result.ConsolidatedSQL != want {
		t.Fatalf("ConsolidatedSQL = %q, want %q", result.ConsolidatedSQL, want)
	}
	if after.ran {
		t.Fatal("a delegate after the failing one ran")
	}
	if len(result.Warnings) == 0 {
		t.Fatal("recovery did not report the original error")
	}
}

func TestErrorRecoveryWithoutADelegateResultUsesTheDefaultConsolidation(t *testing.T) {
	rule := NewErrorRecoveryRule(3, "conservative", true, &stubRule{applies: false})

	single := &tracking.ObjectLifecycle{
		Name: "public.f",
		Type: types.TypeFunction,
		History: []tracking.LifecycleEvent{
			{Operation: types.OpCreate, Statement: types.Statement{SQL: "CREATE FUNCTION f() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;", Operation: types.OpCreate}},
		},
	}
	result, err := rule.Apply(single, nil)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if result.ConsolidatedSQL != single.History[0].Statement.SQL {
		t.Fatalf("ConsolidatedSQL = %q, want the original single-version SQL", result.ConsolidatedSQL)
	}
}

func TestErrorRecoveryYieldsNothingForADroppedObject(t *testing.T) {
	rule := NewErrorRecoveryRule(3, "conservative", true)
	dropped := twoStepTableLifecycle()
	dropped.History = append(dropped.History, tracking.LifecycleEvent{
		Operation: types.OpDrop,
		Statement: types.Statement{SQL: "DROP TABLE users", Operation: types.OpDrop},
	})

	result, err := rule.Apply(dropped, nil)
	if err != nil || result != nil {
		t.Fatalf("Apply = (%v, %v), want (nil, nil)", result, err)
	}
}

func TestRulesListsWrappedDelegatesBeforeTheWrapper(t *testing.T) {
	a, b := &stubRule{}, &stubRule{}
	recovery := NewErrorRecoveryRule(3, "conservative", true, a, b)

	engine := NewConsolidationRuleEngine()
	engine.AddRule(recovery)

	rules := engine.Rules()
	if len(rules) != 3 || rules[0] != a || rules[1] != b || rules[2] != recovery {
		t.Fatalf("Rules() = %v, want [a b recovery]", rules)
	}
}
