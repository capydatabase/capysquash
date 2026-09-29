package pgnames

import (
	"strings"
	"testing"
)

func TestChooseRelationNameFollowsPostgreSQL(t *testing.T) {
	never := func(string) bool { return false }
	if got := ChooseRelationName("orders", "id", "seq", never); got != "orders_id_seq" {
		t.Errorf("got %s", got)
	}
	long := strings.Repeat("t", 60)
	if got := ChooseRelationName(long, "id", "seq", never); got != strings.Repeat("t", 56)+"_id_seq" || len(got) != 63 {
		t.Errorf("truncation: got %s (%d)", got, len(got))
	}
	taken := func(name string) bool { return name == "orders_id_seq" }
	if got := ChooseRelationName("orders", "id", "seq", taken); got != "orders_id_seq1" {
		t.Errorf("collision: got %s", got)
	}
	if got := ChooseRelationName("orders", "", "pkey", never); got != "orders_pkey" {
		t.Errorf("no second name: got %s", got)
	}
}

func TestIndexNamesFollowPostgreSQL(t *testing.T) {
	columns := IndexColumnNames([]string{"a", "", "", "a"})
	if strings.Join(columns, ",") != "a,expr,expr1,a1" {
		t.Errorf("column names: %v", columns)
	}
	if got := ChooseRelationName("t", IndexNameAddition(columns), "idx", nil); got != "t_a_expr_expr1_a1_idx" {
		t.Errorf("index name: %s", got)
	}
}
