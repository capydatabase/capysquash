package tracking

import (
	"reflect"
	"testing"

	"github.com/capysquash/pgsquash-engine/internal/types"
)

func TestDependencyGraphTopologicalSortDeterministic(t *testing.T) {
	t.Parallel()

	dg := NewDependencyGraph()

	usersSchemaB := ObjectID{Type: types.TypeTable, Schema: "b", Name: "users"}
	usersSchemaA := ObjectID{Type: types.TypeTable, Schema: "a", Name: "users"}
	accounts := ObjectID{Type: types.TypeTable, Schema: "public", Name: "accounts"}

	dg.AddNode(usersSchemaB)
	dg.AddNode(usersSchemaA)
	dg.AddNode(accounts)

	first, err := dg.TopologicalSort()
	if err != nil {
		t.Fatalf("first topological sort failed: %v", err)
	}

	second, err := dg.TopologicalSort()
	if err != nil {
		t.Fatalf("second topological sort failed: %v", err)
	}

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("topological sort must be deterministic: first=%v second=%v", first, second)
	}

	expected := []ObjectID{accounts, usersSchemaA, usersSchemaB}
	if !reflect.DeepEqual(first, expected) {
		t.Fatalf("unexpected deterministic order: got=%v expected=%v", first, expected)
	}
}

func TestDependencyGraphDetectCyclesDeterministic(t *testing.T) {
	t.Parallel()

	dg := NewDependencyGraph()

	a := ObjectID{Type: types.TypeTable, Schema: "public", Name: "a"}
	b := ObjectID{Type: types.TypeTable, Schema: "public", Name: "b"}

	dg.AddEdge(a, b)
	dg.AddEdge(b, a)

	first := dg.DetectCycles()
	second := dg.DetectCycles()

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("cycle detection must be deterministic: first=%v second=%v", first, second)
	}

	if len(first) == 0 {
		t.Fatal("expected at least one cycle to be detected")
	}
}
