package squasher

import (
	"strings"
	"testing"

	"github.com/capydatabase/capysquash/internal/tracking"
	"github.com/capydatabase/capysquash/internal/types"
)

func TestUniqueIndexKey(t *testing.T) {
	key, ok := uniqueIndexKey("CREATE UNIQUE INDEX i ON Catalog.Skus (Sku, region)")
	if !ok || key.table != "catalog.skus" || strings.Join(key.columns, ",") != "region,sku" {
		t.Fatalf("got %+v %v", key, ok)
	}
	for _, sql := range []string{
		"CREATE INDEX i ON t (a)",
		"CREATE UNIQUE INDEX i ON t (a) WHERE a IS NOT NULL",
		"CREATE UNIQUE INDEX i ON t (lower(a))",
	} {
		if _, ok := uniqueIndexKey(sql); ok {
			t.Errorf("%s cannot back a foreign key", sql)
		}
	}
}

func TestForeignKeyReferences(t *testing.T) {
	keys := foreignKeyReferences(`CREATE TABLE c (id int REFERENCES p, code text REFERENCES p (code),
  FOREIGN KEY (a, b) REFERENCES public.p (b, a));
ALTER TABLE c ADD FOREIGN KEY (x) REFERENCES s.q (y);`)
	var got []string
	for _, key := range keys {
		got = append(got, key.table+":"+strings.Join(key.columns, ","))
	}
	if strings.Join(got, " ") != "p:code p:a,b s.q:y" {
		t.Fatalf("got %v", got)
	}
}

func TestMoveUniqueIndexesBackingForeignKeys(t *testing.T) {
	created := func(name string, typ types.ObjectType, line int) *tracking.ObjectLifecycle {
		return &tracking.ObjectLifecycle{Name: name, Type: typ, History: []tracking.LifecycleEvent{{
			Migration: "001.sql", Operation: types.OpCreate,
			Statement: types.Statement{Line: line, Filename: "001.sql"},
		}}}
	}
	table := &tracking.ConsolidationResult{
		ConsolidatedSQL: "CREATE TABLE categories (id int, slug text, parent text REFERENCES categories (slug));",
		OriginalStatements: []types.Statement{{
			SQL: "CREATE TABLE categories (id int, slug text, parent text)", Filename: "001.sql", Line: 1,
		}},
	}
	index := &tracking.ConsolidationResult{ConsolidatedSQL: "CREATE UNIQUE INDEX categories_slug_idx ON categories (slug);"}
	other := &tracking.ConsolidationResult{ConsolidatedSQL: "CREATE UNIQUE INDEX categories_id_idx ON categories (id);"}
	lifecycles := map[string]*tracking.ObjectLifecycle{
		"t": created("categories", types.TypeTable, 1),
		"i": created("categories_slug_idx", types.TypeIndex, 2),
		"o": created("categories_id_idx", types.TypeIndex, 3),
	}
	consolidated := map[string]*tracking.ConsolidationResult{"t": table, "i": index, "o": other}
	foundation := map[string]*tracking.ConsolidationResult{"t": table}
	pending := map[*tracking.ConsolidationResult][]positionedSQL{}

	moved, err := moveUniqueIndexesBackingForeignKeys(consolidated, foundation, lifecycles, []string{table.ConsolidatedSQL}, pending)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := moved["i"]; !ok || len(moved) != 1 {
		t.Fatalf("moved %v, want only the index the foreign key needs", moved)
	}
	sql := insertSQLAtHistoryPositions(table, pending[table])
	create := strings.Index(sql, "CREATE TABLE")
	idx := strings.Index(sql, "CREATE UNIQUE INDEX categories_slug_idx")
	fk := strings.Index(sql, "ADD CONSTRAINT categories_parent_fkey FOREIGN KEY (parent) REFERENCES categories (slug)")
	if create < 0 || idx < create || fk < idx || strings.Contains(sql[:idx], "REFERENCES") {
		t.Fatalf("want CREATE TABLE, then the index, then the foreign key:\n%s", sql)
	}
}
