package normalize

import (
	"strings"
	"testing"

	"github.com/capydatabase/capysquash/internal/parser"
	"github.com/capydatabase/capysquash/internal/types"
)

type normalized struct {
	statements []types.Statement
	schemas    string
	warnings   []string
}

func (n normalized) sql() string {
	parts := make([]string, 0, len(n.statements))
	for _, stmt := range n.statements {
		parts = append(parts, strings.TrimSuffix(strings.TrimSpace(stmt.SQL), ";"))
	}
	return strings.Join(parts, ";\n")
}

// run normalizes a history split into migrations.
func run(t *testing.T, migrations ...string) normalized {
	t.Helper()
	parsed := make([][]types.Statement, len(migrations))
	for i, sql := range migrations {
		migration, err := parser.ParseMigration(sql, "migration.sql")
		if err != nil {
			t.Fatalf("parse migration %d: %v", i, err)
		}
		parsed[i] = migration.Statements
	}
	n := New()
	for _, statements := range parsed {
		n.Observe(statements)
	}
	n.Finish()
	var out normalized
	for _, statements := range parsed {
		rewritten, err := n.Rewrite(statements)
		if err != nil {
			t.Fatalf("rewrite: %v", err)
		}
		out.statements = append(out.statements, rewritten...)
	}
	out.schemas = n.SchemasSQL()
	out.warnings = n.Warnings()
	return out
}

func assertContains(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func assertNotContains(t *testing.T, got string, unwanted ...string) {
	t.Helper()
	for _, u := range unwanted {
		if strings.Contains(got, u) {
			t.Errorf("unexpected %q in:\n%s", u, got)
		}
	}
}

func statementFor(t *testing.T, n normalized, fragment string) types.Statement {
	t.Helper()
	for _, stmt := range n.statements {
		if strings.Contains(stmt.SQL, fragment) {
			return stmt
		}
	}
	t.Fatalf("no statement contains %q in:\n%s", fragment, n.sql())
	return types.Statement{}
}

func TestTableRenameKeepsItsOwnStatementsAndRewritesDependents(t *testing.T) {
	n := run(t, `
CREATE TABLE public.profiles (id bigint PRIMARY KEY, display_name text);
CREATE INDEX profiles_name ON public.profiles (display_name);
CREATE INDEX ON public.profiles (lower(display_name));
ALTER TABLE public.profiles RENAME TO user_profiles;
ALTER TABLE public.user_profiles RENAME COLUMN display_name TO public_name;
CREATE TABLE public.follows (id bigint PRIMARY KEY, profile_id bigint REFERENCES public.user_profiles(id));
`)
	got := n.sql()
	assertContains(t, got,
		"CREATE TABLE public.profiles (id bigint PRIMARY KEY, display_name text)",
		"ALTER TABLE public.profiles RENAME TO user_profiles",
		"ALTER TABLE public.user_profiles RENAME COLUMN display_name TO public_name",
		"CREATE INDEX profiles_name ON public.user_profiles USING btree (public_name)",
		// PostgreSQL named the unnamed index after the table and column it had.
		"CREATE INDEX profiles_lower_idx ON public.user_profiles USING btree (lower(public_name))",
		"REFERENCES public.user_profiles(id)",
	)
	for _, fragment := range []string{"CREATE TABLE public.profiles", "RENAME TO user_profiles", "RENAME COLUMN"} {
		if stmt := statementFor(t, n, fragment); stmt.ObjectName != "public.user_profiles" {
			t.Errorf("%s is tracked as %q, want public.user_profiles", fragment, stmt.ObjectName)
		}
	}
}

func TestSchemaRenameCreatesTheSchemaUnderItsFinalName(t *testing.T) {
	n := run(t, `
CREATE SCHEMA staging;
CREATE TABLE staging.imports (id bigint PRIMARY KEY);
CREATE TYPE staging.status AS ENUM ('new', 'done');
CREATE FUNCTION staging.touch(s staging.status) RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;
ALTER SCHEMA staging RENAME TO ingest;
CREATE TABLE ingest.batches (id bigint PRIMARY KEY, status ingest.status);
`)
	if n.schemas != "CREATE SCHEMA ingest;" {
		t.Errorf("schemas section: %q", n.schemas)
	}
	got := n.sql()
	assertContains(t, got,
		"CREATE TABLE ingest.imports",
		"CREATE TYPE ingest.status AS ENUM ('new', 'done')",
		"CREATE FUNCTION ingest.touch(s ingest.status)",
		"CREATE TABLE ingest.batches (id bigint PRIMARY KEY, status ingest.status)",
	)
	assertNotContains(t, got, "staging", "CREATE SCHEMA", "RENAME")
}

func TestTypeRenamesFollowColumnsAndSignatures(t *testing.T) {
	n := run(t, `
CREATE TYPE public.mood AS ENUM ('a', 'b');
CREATE DOMAIN public.score AS int CHECK (VALUE > 0);
CREATE TYPE public.pair AS (x int, y int);
CREATE TABLE public.t (id int, m public.mood DEFAULT 'a', s score, p pair);
CREATE FUNCTION public.f(m mood) RETURNS pair LANGUAGE sql RETURN (1, 2)::pair;
CREATE FUNCTION public.g() RETURNS int LANGUAGE sql AS $$ SELECT (1, 2)::pair $$;
ALTER TYPE public.mood RENAME VALUE 'a' TO 'z';
ALTER TYPE public.mood RENAME TO feeling;
ALTER DOMAIN public.score RENAME TO points;
ALTER TYPE public.pair RENAME ATTRIBUTE x TO first;
ALTER TYPE public.pair RENAME TO couple;
CREATE TABLE public.u (m feeling DEFAULT 'z');
`)
	got := n.sql()
	assertContains(t, got,
		"CREATE TYPE public.feeling AS ENUM ('z', 'b')",
		// The CHECK keeps the name PostgreSQL gave it after the old name.
		"CREATE DOMAIN public.points AS int CONSTRAINT score_check CHECK (value > 0)",
		"CREATE TYPE public.couple AS (first int, y int)",
		"m public.feeling DEFAULT 'z'",
		"s points",
		"p couple",
		"CREATE FUNCTION public.f(m feeling) RETURNS couple",
		"RETURN (1, 2)::couple",
	)
	assertNotContains(t, got, "RENAME", "mood", "score AS")
	if !strings.Contains(strings.Join(n.warnings, "\n"), "the body of function g names pair") {
		t.Errorf("expected a warning about the string body of g, got %v", n.warnings)
	}
}

func TestDropSchemaCascadeRemovesWhatItHeld(t *testing.T) {
	n := run(t, `
CREATE SCHEMA scratch;
CREATE TABLE scratch.tmp (id int);
CREATE INDEX tmp_id ON scratch.tmp (id);
COMMENT ON TABLE scratch.tmp IS 'temporary';
INSERT INTO scratch.tmp VALUES (1);
CREATE TABLE public.keep (id int);
DROP SCHEMA scratch CASCADE;
CREATE SCHEMA IF NOT EXISTS scratch AUTHORIZATION CURRENT_USER;
CREATE TABLE scratch.again (id int);
`)
	got := n.sql()
	assertNotContains(t, got, "scratch.tmp", "tmp_id", "DROP SCHEMA", "temporary")
	assertContains(t, got, "CREATE TABLE public.keep", "CREATE TABLE scratch.again")
	if n.schemas != "CREATE SCHEMA IF NOT EXISTS scratch AUTHORIZATION CURRENT_USER;" {
		t.Errorf("schemas section: %q", n.schemas)
	}
}

func TestDropOfAPreexistingSchemaStaysInTheSchemasSection(t *testing.T) {
	n := run(t, `
CREATE TABLE public.old (id int);
DROP SCHEMA public CASCADE;
CREATE SCHEMA public;
CREATE TABLE public.t (id int);
`)
	if n.schemas != "DROP SCHEMA public CASCADE;\nCREATE SCHEMA public;" {
		t.Errorf("schemas section: %q", n.schemas)
	}
	got := n.sql()
	assertNotContains(t, got, "public.old")
	assertContains(t, got, "CREATE TABLE public.t")
}

func TestCrossSchemaDependentOfADroppedSchemaIsReported(t *testing.T) {
	n := run(t, `
CREATE SCHEMA lookup;
CREATE TABLE lookup.codes (id int PRIMARY KEY);
CREATE TABLE public.orders (id int, code int REFERENCES lookup.codes(id));
DROP SCHEMA lookup CASCADE;
`)
	if len(n.warnings) == 0 || !strings.Contains(strings.Join(n.warnings, "\n"), "lookup.codes") && !strings.Contains(strings.Join(n.warnings, "\n"), "codes") {
		t.Errorf("expected a warning about the foreign key to lookup.codes, got %v", n.warnings)
	}
}

func TestSchemaElementsBecomeQualifiedStatements(t *testing.T) {
	n := run(t, `
CREATE SCHEMA app CREATE TABLE items (id int) CREATE VIEW item_ids AS SELECT id FROM items;
`)
	if n.schemas != "CREATE SCHEMA app;" {
		t.Errorf("schemas section: %q", n.schemas)
	}
	assertContains(t, n.sql(), "CREATE TABLE app.items (id int)", "CREATE VIEW app.item_ids AS SELECT id FROM app.items")
}

func TestViewKeepsColumnNamesAcrossRenames(t *testing.T) {
	n := run(t, `
CREATE TABLE public.t (id int, col text);
CREATE VIEW public.v AS SELECT t.col, id FROM public.t;
CREATE VIEW public.w AS SELECT * FROM public.t;
ALTER TABLE public.t RENAME TO t2;
ALTER TABLE public.t2 RENAME COLUMN col TO c2;
ALTER TABLE public.t2 ADD COLUMN later int;
`)
	assertContains(t, n.sql(),
		"CREATE VIEW public.v AS SELECT t2.c2 AS col, id FROM public.t2",
		"CREATE VIEW public.w AS SELECT id, c2 AS col FROM public.t2",
	)
}

func TestTableSetSchemaIsTrackedWithTheTable(t *testing.T) {
	n := run(t, `
CREATE SCHEMA s1;
CREATE TABLE s1.imports (id bigint PRIMARY KEY);
CREATE INDEX imports_id ON s1.imports (id);
ALTER TABLE s1.imports SET SCHEMA public;
`)
	assertContains(t, n.sql(),
		"CREATE TABLE s1.imports",
		"ALTER TABLE s1.imports SET SCHEMA public",
		"CREATE INDEX imports_id ON public.imports",
	)
	if stmt := statementFor(t, n, "SET SCHEMA"); stmt.ObjectName != "public.imports" {
		t.Errorf("SET SCHEMA is tracked as %q", stmt.ObjectName)
	}
}

func TestUnchangedStatementsAreLeftAlone(t *testing.T) {
	history := `CREATE TABLE public.plain (id int);
CREATE INDEX plain_id ON public.plain (id);`
	n := run(t, history)
	migration, err := parser.ParseMigration(history, "migration.sql")
	if err != nil {
		t.Fatal(err)
	}
	for i, stmt := range n.statements {
		if stmt.SQL != migration.Statements[i].SQL {
			t.Errorf("statement %d changed: %q", i, stmt.SQL)
		}
	}
}
