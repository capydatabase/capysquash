package transaction

import (
	"strings"
	"testing"

	"github.com/capysquash/pgsquash-engine/internal/types"
)

func TestPlanTransactions_WarnsOnMultipleAlterTableTargetsInBatch(t *testing.T) {
	t.Parallel()

	planner := NewTransactionPlanner("17")
	statements := []types.Statement{
		{
			SQL:        "ALTER TABLE public.users ADD COLUMN name text;",
			ObjectType: types.TypeTable,
			ObjectName: "public.users",
			Operation:  types.OpAlter,
			Metadata: types.StatementMetadata{
				LockLevel: types.LockAccessExclusive,
			},
		},
		{
			SQL:        "ALTER TABLE public.accounts ADD COLUMN status text;",
			ObjectType: types.TypeTable,
			ObjectName: "public.accounts",
			Operation:  types.OpAlter,
			Metadata: types.StatementMetadata{
				LockLevel: types.LockAccessExclusive,
			},
		},
	}

	plan := planner.PlanTransactions(statements)

	found := false
	for _, w := range plan.Warnings {
		if strings.Contains(w, "alters 2 different tables in one transaction") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected multiple-table ALTER warning, got: %v", plan.Warnings)
	}
}

func TestPlanTransactions_DoesNotWarnForSingleAlterTableTarget(t *testing.T) {
	t.Parallel()

	planner := NewTransactionPlanner("17")
	statements := []types.Statement{
		{
			SQL:        "ALTER TABLE public.users ADD COLUMN name text;",
			ObjectType: types.TypeTable,
			ObjectName: "public.users",
			Operation:  types.OpAlter,
			Metadata: types.StatementMetadata{
				LockLevel: types.LockAccessExclusive,
			},
		},
		{
			SQL:        "ALTER TABLE public.users ADD COLUMN bio text;",
			ObjectType: types.TypeTable,
			ObjectName: "public.users",
			Operation:  types.OpAlter,
			Metadata: types.StatementMetadata{
				LockLevel: types.LockAccessExclusive,
			},
		},
	}

	plan := planner.PlanTransactions(statements)

	for _, w := range plan.Warnings {
		if strings.Contains(w, "different tables in one transaction") {
			t.Fatalf("did not expect multiple-table ALTER warning, got: %v", plan.Warnings)
		}
	}
}

func TestPlanTransactions_WarnsOnAddConstraintWithoutNotValid(t *testing.T) {
	t.Parallel()

	planner := NewTransactionPlanner("17")
	statements := []types.Statement{
		{
			SQL:        "ALTER TABLE public.users ADD CONSTRAINT users_email_key UNIQUE (email);",
			ObjectType: types.TypeTable,
			ObjectName: "public.users",
			Operation:  types.OpAlter,
			Metadata: types.StatementMetadata{
				LockLevel: types.LockAccessExclusive,
			},
		},
	}

	plan := planner.PlanTransactions(statements)

	found := false
	for _, w := range plan.Warnings {
		if strings.Contains(w, "without NOT VALID") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected ADD CONSTRAINT warning, got: %v", plan.Warnings)
	}
}

func TestPlanTransactions_DoesNotWarnOnAddConstraintWithNotValid(t *testing.T) {
	t.Parallel()

	planner := NewTransactionPlanner("17")
	statements := []types.Statement{
		{
			SQL:        "ALTER TABLE public.users ADD CONSTRAINT users_email_key UNIQUE (email) NOT VALID;",
			ObjectType: types.TypeTable,
			ObjectName: "public.users",
			Operation:  types.OpAlter,
			Metadata: types.StatementMetadata{
				LockLevel: types.LockShareUpdateExclusive,
			},
		},
	}

	plan := planner.PlanTransactions(statements)

	for _, w := range plan.Warnings {
		if strings.Contains(w, "without NOT VALID") {
			t.Fatalf("did not expect ADD CONSTRAINT warning, got: %v", plan.Warnings)
		}
	}
}

func TestPlanTransactions_WarnsOnNonConcurrentCreateIndex(t *testing.T) {
	t.Parallel()

	planner := NewTransactionPlanner("17")
	statements := []types.Statement{
		{
			SQL:        "CREATE INDEX idx_users_email ON public.users (email);",
			ObjectType: types.TypeIndex,
			ObjectName: "public.idx_users_email",
			Operation:  types.OpCreate,
			Metadata: types.StatementMetadata{
				Concurrent: false,
				LockLevel:  types.LockShare,
			},
		},
	}

	plan := planner.PlanTransactions(statements)

	found := false
	for _, w := range plan.Warnings {
		if strings.Contains(w, "without CONCURRENTLY") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected non-concurrent index warning, got: %v", plan.Warnings)
	}
}

func TestPlanTransactions_DoesNotWarnOnConcurrentCreateIndex(t *testing.T) {
	t.Parallel()

	planner := NewTransactionPlanner("17")
	statements := []types.Statement{
		{
			SQL:        "CREATE INDEX CONCURRENTLY idx_users_email ON public.users (email);",
			ObjectType: types.TypeIndex,
			ObjectName: "public.idx_users_email",
			Operation:  types.OpCreate,
			Metadata: types.StatementMetadata{
				Concurrent: true,
				LockLevel:  types.LockShareUpdateExclusive,
			},
		},
	}

	plan := planner.PlanTransactions(statements)

	for _, w := range plan.Warnings {
		if strings.Contains(w, "without CONCURRENTLY") {
			t.Fatalf("did not expect non-concurrent index warning, got: %v", plan.Warnings)
		}
	}
}
