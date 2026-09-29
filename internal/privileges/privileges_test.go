package privileges

import (
	"strings"
	"testing"

	"github.com/capydatabase/capysquash/internal/parser"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// replay feeds a history to a fresh model.
func replayHistory(t *testing.T, self, sql string) *model {
	t.Helper()
	parsed, err := pg_query.Parse(sql)
	if err != nil {
		t.Fatalf("parse history: %v", err)
	}
	m := newModel(self)
	for _, raw := range parsed.GetStmts() {
		m.apply(raw.GetStmt())
	}
	return m
}

// section records a history and renders its PRIVILEGES section for a
// baseline that creates everything the history leaves.
func section(t *testing.T, history, baseline string) (string, []string) {
	t.Helper()
	migration, err := parser.ParseMigration(history, "001_history.sql")
	if err != nil {
		t.Fatalf("parse history: %v", err)
	}
	h := NewHistory()
	h.Record(migration.Statements)
	sql, warnings, err := h.PrivilegesSQL(baseline)
	if err != nil {
		t.Fatalf("PrivilegesSQL: %v", err)
	}
	return sql, warnings
}

func mustObject(t *testing.T, m *model, kind, schema, name, args string) *object {
	t.Helper()
	obj := m.objects[objectKey(kind, schema, name, args)]
	if obj == nil {
		t.Fatalf("%s %s.%s%s is not in the model", kind, schema, name, args)
	}
	return obj
}

func assertStatements(t *testing.T, got string, want ...string) {
	t.Helper()
	for _, statement := range want {
		if !strings.Contains(got, statement+";") {
			t.Errorf("missing %q in:\n%s", statement, got)
		}
	}
}

func assertNoStatements(t *testing.T, got string, unwanted ...string) {
	t.Helper()
	for _, statement := range unwanted {
		if strings.Contains(got, statement) {
			t.Errorf("unexpected %q in:\n%s", statement, got)
		}
	}
}

func TestDropAndRecreateForgetsTheOldGrants(t *testing.T) {
	history := `
CREATE TABLE sessions (id int);
GRANT SELECT, DELETE ON sessions TO app;
DROP TABLE sessions;
CREATE TABLE sessions (id int, token text);
GRANT INSERT ON sessions TO app;`
	got, _ := section(t, history, "CREATE TABLE sessions (id int, token text);")
	assertStatements(t, got, "GRANT INSERT ON TABLE public.sessions TO app")
	assertNoStatements(t, got, "SELECT", "DELETE")
}

func TestPrivilegesFollowRenames(t *testing.T) {
	m := replayHistory(t, "", `
CREATE SCHEMA staging;
CREATE TABLE staging.profiles (id int, display_name text);
GRANT SELECT ON staging.profiles TO reader;
GRANT UPDATE (display_name) ON staging.profiles TO writer;
ALTER TABLE staging.profiles RENAME TO user_profiles;
ALTER TABLE staging.user_profiles RENAME COLUMN display_name TO public_name;
ALTER SCHEMA staging RENAME TO ingest;
ALTER TABLE ingest.user_profiles SET SCHEMA public;
CREATE FUNCTION count_rows() RETURNS int LANGUAGE sql AS 'select 1';
GRANT EXECUTE ON FUNCTION count_rows() TO reader;
ALTER FUNCTION count_rows() RENAME TO row_count;
CREATE TYPE mood AS ENUM ('a');
REVOKE USAGE ON TYPE mood FROM PUBLIC;
ALTER TYPE mood RENAME TO feeling;
GRANT USAGE ON SCHEMA ingest TO reader;`)

	table := mustObject(t, m, kindRelation, "public", "user_profiles", "")
	if _, ok := table.acl["reader"][privSelect]; !ok {
		t.Errorf("the renamed and moved table lost SELECT for reader: %v", table.acl)
	}
	if _, ok := table.columns["public_name"]["writer"][privUpdate]; !ok {
		t.Errorf("the renamed column lost UPDATE for writer: %v", table.columns)
	}
	routine := mustObject(t, m, kindRoutine, "public", "row_count", "()")
	if _, ok := routine.acl["reader"][privExecute]; !ok {
		t.Errorf("the renamed function lost EXECUTE for reader: %v", routine.acl)
	}
	typ := mustObject(t, m, kindType, "public", "feeling", "")
	if _, ok := typ.acl[rolePublic]; ok {
		t.Errorf("the renamed type got USAGE for PUBLIC back: %v", typ.acl)
	}
	schema := mustObject(t, m, kindSchema, "", "ingest", "")
	if _, ok := schema.acl["reader"][privUsage]; !ok {
		t.Errorf("the grant on the renamed schema is not recorded: %v", schema.acl)
	}
}

func TestTableRevokeAlsoRevokesColumnPrivileges(t *testing.T) {
	m := replayHistory(t, "", `
CREATE TABLE accounts (id int, email text);
GRANT SELECT (id, email), UPDATE (email) ON accounts TO reader;
REVOKE UPDATE ON accounts FROM reader;`)
	table := mustObject(t, m, kindRelation, "public", "accounts", "")
	if _, ok := table.columns["email"]["reader"][privUpdate]; ok {
		t.Errorf("a table-level REVOKE must clear the column privilege: %v", table.columns["email"])
	}
	if _, ok := table.columns["email"]["reader"][privSelect]; !ok {
		t.Errorf("SELECT (email) must stay: %v", table.columns["email"])
	}
}

func TestOwnerChangeMergesTheOldOwnersPrivileges(t *testing.T) {
	m := replayHistory(t, "", `
CREATE TABLE accounts (id serial);
GRANT INSERT ON accounts TO app WITH GRANT OPTION;
ALTER TABLE accounts OWNER TO app;`)
	table := mustObject(t, m, kindRelation, "public", "accounts", "")
	if table.owner != "app" || !table.acl["app"][privInsert] || len(table.acl["app"]) != len(classTable.universe()) {
		t.Fatalf("owner %q acl %v: want app holding every privilege, INSERT with grant option", table.owner, table.acl)
	}
	if _, ok := table.acl[roleMigrator]; ok {
		t.Errorf("the old owner's entry must move to the new owner: %v", table.acl)
	}
	seq := mustObject(t, m, kindRelation, "public", "accounts_id_seq", "")
	if seq.owner != "app" {
		t.Errorf("the serial sequence must follow the table's owner, got %q", seq.owner)
	}

	got, _ := section(t, `
CREATE TABLE accounts (id serial);
GRANT INSERT ON accounts TO app WITH GRANT OPTION;
ALTER TABLE accounts OWNER TO app;`, "CREATE TABLE accounts (id serial);")
	assertStatements(t, got,
		"ALTER TABLE public.accounts OWNER TO app",
		"GRANT INSERT ON TABLE public.accounts TO app WITH GRANT OPTION")
	assertNoStatements(t, got, "ALTER SEQUENCE")
}

func TestDefaultPrivilegesApplyAtCreation(t *testing.T) {
	m := replayHistory(t, "", `
CREATE FUNCTION before_fn() RETURNS int LANGUAGE sql AS 'select 1';
ALTER DEFAULT PRIVILEGES REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
CREATE SCHEMA app;
ALTER DEFAULT PRIVILEGES IN SCHEMA app GRANT SELECT ON TABLES TO reader;
ALTER DEFAULT PRIVILEGES IN SCHEMA app GRANT USAGE ON SEQUENCES TO reader;
CREATE FUNCTION app.after_fn() RETURNS int LANGUAGE sql AS 'select 1';
CREATE TABLE app.orders (id bigserial);
CREATE TABLE public.elsewhere (id int);
ALTER DEFAULT PRIVILEGES IN SCHEMA app REVOKE SELECT ON TABLES FROM reader;
CREATE TABLE app.later (id int);`)

	if before := mustObject(t, m, kindRoutine, "public", "before_fn", "()"); before.acl != nil {
		t.Errorf("a function created before the defaults keeps the built-in ones: %v", before.acl)
	}
	after := mustObject(t, m, kindRoutine, "app", "after_fn", "()")
	if _, ok := after.acl[rolePublic]; ok || after.acl == nil {
		t.Errorf("the database-wide default must replace PUBLIC EXECUTE: %v", after.acl)
	}
	orders := mustObject(t, m, kindRelation, "app", "orders", "")
	if _, ok := orders.acl["reader"][privSelect]; !ok {
		t.Errorf("the schema default must add SELECT: %v", orders.acl)
	}
	seq := mustObject(t, m, kindRelation, "app", "orders_id_seq", "")
	if _, ok := seq.acl["reader"][privUsage]; !ok {
		t.Errorf("the bigserial sequence gets the sequence defaults: %v", seq.acl)
	}
	if elsewhere := mustObject(t, m, kindRelation, "public", "elsewhere", ""); elsewhere.acl != nil {
		t.Errorf("schema defaults stay in their schema: %v", elsewhere.acl)
	}
	if later := mustObject(t, m, kindRelation, "app", "later", ""); later.acl != nil {
		t.Errorf("an emptied schema default grants nothing: %v", later.acl)
	}
	if _, ok := m.defaults[defaultKey{role: roleMigrator, schema: "app", objtype: defaultsTables}]; ok {
		t.Error("an empty schema default entry must disappear, as in pg_default_acl")
	}
}

func TestDefaultPrivilegesForANamedRoleDependOnWhoRunsTheBaseline(t *testing.T) {
	history := `
CREATE SCHEMA audit;
ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA audit GRANT SELECT ON TABLES TO reader;
CREATE TABLE audit.events (id int);
GRANT INSERT ON audit.events TO writer;`
	got, _ := section(t, history, "CREATE SCHEMA audit; CREATE TABLE audit.events (id int);")

	// An object whose privileges depend on the role is rendered whole in each
	// branch, because the order of its statements matters.
	before, block, found := strings.Cut(got, "DO $capysquash$")
	if !found {
		t.Fatalf("no conditional block:\n%s", got)
	}
	assertNoStatements(t, before, "reader")
	whenPostgres, otherwise, found := strings.Cut(block, "ELSE")
	if !found || !strings.Contains(whenPostgres, "IF current_user = 'postgres' THEN") {
		t.Fatalf("no current_user branch:\n%s", block)
	}
	assertStatements(t, whenPostgres,
		"GRANT SELECT ON TABLE audit.events TO reader",
		"GRANT INSERT ON TABLE audit.events TO writer",
		"ALTER DEFAULT PRIVILEGES IN SCHEMA audit GRANT SELECT ON TABLES TO reader")
	assertStatements(t, otherwise,
		"GRANT INSERT ON TABLE audit.events TO writer",
		"ALTER DEFAULT PRIVILEGES FOR ROLE postgres IN SCHEMA audit GRANT SELECT ON TABLES TO reader")
	assertNoStatements(t, otherwise, "GRANT SELECT ON TABLE audit.events")
	if _, err := pg_query.Parse(got); err != nil {
		t.Errorf("the section does not parse: %v", err)
	}
}

func TestDiffPrivilegesReachesMaintainThroughAll(t *testing.T) {
	all := privSet{}
	for _, p := range classTable.universe() {
		all[p] = false
	}
	want := privSet{}
	for p := range all {
		if p != privDelete {
			want[p] = false
		}
	}
	got := diffPrivileges(classTable, privSet{}, want)
	if len(got) != 2 || got[0].revoke || got[0].privileges != nil || !got[1].revoke || len(got[1].privileges) != 1 || got[1].privileges[0] != privDelete {
		t.Fatalf("want GRANT ALL then REVOKE DELETE, got %+v", got)
	}

	// Revoking MAINTAIN from the owner goes through REVOKE ALL too.
	got = diffPrivileges(classTable, all, privSet{privSelect: false})
	if len(got) != 2 || !got[0].revoke || got[0].privileges != nil || got[1].revoke || got[1].privileges[0] != privSelect {
		t.Fatalf("want REVOKE ALL then GRANT SELECT, got %+v", got)
	}

	// Classes without MAINTAIN name their privileges.
	got = diffPrivileges(classSequence, privSet{}, privSet{privUsage: true, privSelect: false})
	if len(got) != 2 || got[0].privileges[0] != privSelect || !got[1].grantOption || got[1].privileges[0] != privUsage {
		t.Fatalf("want GRANT SELECT then GRANT USAGE WITH GRANT OPTION, got %+v", got)
	}
}

func TestBulkGrantsReachOnlyTheObjectsThatExistThen(t *testing.T) {
	history := `
CREATE SCHEMA app;
CREATE TABLE app.early (id int);
GRANT SELECT ON ALL TABLES IN SCHEMA app TO reader;
CREATE TABLE app.late (id int);
ALTER SCHEMA app RENAME TO core;`
	m := replayHistory(t, "", history)
	if _, ok := mustObject(t, m, kindRelation, "core", "early", "").acl["reader"]; !ok {
		t.Error("the bulk grant must reach the table that existed")
	}
	if late := mustObject(t, m, kindRelation, "core", "late", ""); late.acl != nil {
		t.Errorf("the bulk grant must not reach a later table: %v", late.acl)
	}

	got, _ := section(t, history, "CREATE SCHEMA core; CREATE TABLE core.early (id int); CREATE TABLE core.late (id int);")
	// Replayed with the schema's final name (it also reaches objects the
	// history does not create), then the later table is corrected.
	assertStatements(t, got,
		"GRANT SELECT ON ALL TABLES IN SCHEMA core TO reader",
		"REVOKE SELECT ON TABLE core.late FROM reader")
	assertNoStatements(t, got, "ON TABLE core.early")
}

func TestStatementsOnObjectsTheHistoryDoesNotCreateAreReplayed(t *testing.T) {
	history := `
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
CREATE TABLE t (id int);
GRANT SELECT ON t, pg_catalog.pg_class TO reader;
ALTER FUNCTION pg_catalog.now() OWNER TO someone;`
	got, _ := section(t, history, "CREATE TABLE t (id int);")
	// Replayed as written (deparsed), before the per-object statements.
	assertStatements(t, got,
		"REVOKE create ON SCHEMA public FROM public",
		"GRANT select ON pg_catalog.pg_class TO reader",
		"ALTER FUNCTION pg_catalog.now() OWNER TO someone",
		"GRANT SELECT ON TABLE public.t TO reader")
	if strings.Index(got, "pg_class") > strings.Index(got, "public.t") {
		t.Errorf("replayed statements come before the per-object ones:\n%s", got)
	}
}

func TestObjectsTheBaselineDoesNotCreateGetNoStatements(t *testing.T) {
	got, warnings := section(t, "CREATE TABLE t (id int); GRANT SELECT ON t TO reader;", "SELECT 1;")
	if got != "" {
		t.Errorf("statements for a table the baseline lacks:\n%s", got)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "table public.t") {
		t.Errorf("want one warning naming the table, got %v", warnings)
	}
}

func TestRolesSectionGuardsCreateRoleAndReplaysMembership(t *testing.T) {
	migration, err := parser.ParseMigration("CREATE ROLE admin LOGIN; GRANT admin TO alice; CREATE TABLE t (id int);", "001.sql")
	if err != nil {
		t.Fatal(err)
	}
	h := NewHistory()
	kept := h.Record(migration.Statements)
	if len(kept) != 1 || !strings.HasPrefix(kept[0].SQL, "CREATE TABLE") {
		t.Fatalf("the tracker must only keep CREATE TABLE, got %d statements", len(kept))
	}
	roles, err := h.RolesSQL()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = 'admin') THEN\n    CREATE ROLE admin WITH LOGIN;",
		"GRANT admin TO alice;",
	} {
		if !strings.Contains(roles, want) {
			t.Errorf("missing %q in:\n%s", want, roles)
		}
	}
	if _, err := pg_query.Parse(roles); err != nil {
		t.Errorf("the roles section does not parse: %v\n%s", err, roles)
	}
}

func TestOwnsOnlyPrivilegeStatements(t *testing.T) {
	cases := map[string]bool{
		"GRANT SELECT ON t TO r": true,
		"REVOKE r FROM u":        true,
		"ALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO r": true,
		"CREATE ROLE r":                                                   true,
		"ALTER TABLE t OWNER TO r":                                        true,
		"ALTER FUNCTION f() OWNER TO r":                                   true,
		"ALTER TABLE t ADD COLUMN c int, OWNER TO r":                      false,
		"ALTER PUBLICATION p OWNER TO r":                                  false,
		"ALTER TABLE t ENABLE ROW LEVEL SECURITY":                         false,
		"CREATE TABLE t (id int)":                                         false,
		"ALTER DEFAULT PRIVILEGES REVOKE ALL ON TYPES FROM r":             true,
		"GRANT CONNECT ON DATABASE app TO r":                              true,
		"ALTER SCHEMA s OWNER TO r":                                       true,
		"ALTER DOMAIN d OWNER TO r":                                       true,
		"ALTER MATERIALIZED VIEW m OWNER TO r":                            true,
		"ALTER SEQUENCE s OWNER TO r":                                     true,
		"ALTER LARGE OBJECT 12 OWNER TO r":                                false,
		"ALTER FOREIGN DATA WRAPPER w OWNER TO r":                         false,
		"ALTER DEFAULT PRIVILEGES FOR ROLE a GRANT USAGE ON SCHEMAS TO r": true,
		"SET ROLE r":                  true,
		"RESET ROLE":                  true,
		"SET LOCAL ROLE r":            true,
		"SET SESSION AUTHORIZATION r": true,
		"RESET SESSION AUTHORIZATION": true,
		"SET search_path TO app":      false,
		"RESET ALL":                   false,
	}
	for sql, want := range cases {
		parsed, err := pg_query.Parse(sql)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		if got := Owns(parsed.GetStmts()[0].GetStmt()); got != want {
			t.Errorf("Owns(%q) = %t, want %t", sql, got, want)
		}
	}
}

func TestQuoteIdent(t *testing.T) {
	for in, want := range map[string]string{
		"accounts": "accounts",
		"user":     `"user"`,
		"select":   `"select"`,
		"Mixed":    `"Mixed"`,
		`a"b`:      `"a""b"`,
		"with 1":   `"with 1"`,
	} {
		if got := quoteIdent(in); got != want {
			t.Errorf("quoteIdent(%q) = %s, want %s", in, got, want)
		}
	}
}

func TestRoutineGrantsFollowRenamedArgumentTypes(t *testing.T) {
	history := `
CREATE SCHEMA staging;
CREATE TYPE public.mood AS ENUM ('a');
CREATE TYPE staging.kind AS ENUM ('b');
CREATE FUNCTION public.describe(m mood, k staging.kind, ms mood[]) RETURNS text LANGUAGE sql RETURN 'x';
GRANT EXECUTE ON FUNCTION public.describe(mood, staging.kind, mood[]) TO reader;
ALTER TYPE public.mood RENAME TO feeling;
ALTER SCHEMA staging RENAME TO ingest;`
	baseline := `
CREATE SCHEMA ingest;
CREATE TYPE public.feeling AS ENUM ('a');
CREATE TYPE ingest.kind AS ENUM ('b');
CREATE FUNCTION public.describe(m feeling, k ingest.kind, ms feeling[]) RETURNS text LANGUAGE sql RETURN 'x';`
	sql, warnings := section(t, history, baseline)
	assertStatements(t, sql, "GRANT ALL ON FUNCTION public.describe(feeling,ingest.kind,feeling[]) TO reader")
	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestObjectsCreatedUnderSetRoleBelongToThatRole(t *testing.T) {
	history := `
CREATE SCHEMA app AUTHORIZATION app_owner;
SET ROLE app_owner;
CREATE TABLE app.accounts (id bigserial PRIMARY KEY);
CREATE FUNCTION app.answer() RETURNS int LANGUAGE sql AS 'select 42';
CREATE SCHEMA owned_by_current AUTHORIZATION CURRENT_USER;
GRANT SELECT ON app.accounts TO reader;
RESET ROLE;
CREATE TABLE app.audit (id int);`
	baseline := `
CREATE SCHEMA app AUTHORIZATION app_owner;
CREATE SCHEMA owned_by_current;
CREATE TABLE app.accounts (id bigserial PRIMARY KEY);
CREATE FUNCTION app.answer() RETURNS int LANGUAGE sql AS 'select 42';
CREATE TABLE app.audit (id int);`
	got, warnings := section(t, history, baseline)
	assertStatements(t, got,
		"ALTER TABLE app.accounts OWNER TO app_owner",
		"ALTER FUNCTION app.answer() OWNER TO app_owner",
		"ALTER SCHEMA owned_by_current OWNER TO app_owner",
		// The owner granted it: no role switch needed.
		"GRANT SELECT ON TABLE app.accounts TO reader")
	assertNoStatements(t, got, "app.audit OWNER", "accounts_id_seq OWNER", "SET ROLE")
	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
}

func TestDefaultPrivilegesUnderSetRoleAreThatRolesAndReachItsObjects(t *testing.T) {
	history := `
SET ROLE app_owner;
ALTER DEFAULT PRIVILEGES GRANT SELECT ON TABLES TO reader;
CREATE TABLE t (id int);
RESET ROLE;
CREATE TABLE mine (id int);`
	got, _ := section(t, history, "CREATE TABLE t (id int); CREATE TABLE mine (id int);")
	assertStatements(t, got,
		"ALTER TABLE public.t OWNER TO app_owner",
		"GRANT SELECT ON TABLE public.t TO reader",
		"ALTER DEFAULT PRIVILEGES FOR ROLE app_owner GRANT SELECT ON TABLES TO reader")
	assertNoStatements(t, got, "public.mine TO reader")
}

func TestAGrantByAnotherRoleIsMadeAsThatRole(t *testing.T) {
	history := `
CREATE TABLE t (id int);
GRANT SELECT, UPDATE ON t TO delegate WITH GRANT OPTION;
SET ROLE delegate;
GRANT SELECT ON t TO reader;
GRANT INSERT ON t TO nobody;
RESET ROLE;`
	got, warnings := section(t, history, "CREATE TABLE t (id int);")
	want := "SET ROLE delegate;\nGRANT SELECT ON TABLE public.t TO reader;\nRESET ROLE;"
	if !strings.Contains(got, want) {
		t.Errorf("missing the delegated grant %q in:\n%s", want, got)
	}
	assertStatements(t, got, "GRANT SELECT, UPDATE ON TABLE public.t TO delegate WITH GRANT OPTION")
	assertNoStatements(t, got, "nobody")
	if len(warnings) != 1 || !strings.Contains(warnings[0], "without holding a grant option") {
		t.Errorf("want one warning about the grant without a grant option, got %v", warnings)
	}
}

func TestRevokingAGrantOptionTakesBackWhatWasGrantedWithIt(t *testing.T) {
	history := `
CREATE TABLE t (id int);
GRANT SELECT ON t TO delegate WITH GRANT OPTION;
SET ROLE delegate;
GRANT SELECT ON t TO reader;
RESET ROLE;
REVOKE GRANT OPTION FOR SELECT ON t FROM delegate CASCADE;`
	got, _ := section(t, history, "CREATE TABLE t (id int);")
	assertStatements(t, got, "GRANT SELECT ON TABLE public.t TO delegate")
	assertNoStatements(t, got, "reader", "SET ROLE", "WITH GRANT OPTION")
}

func TestAMemberOfTheOwnerGrantsAsTheOwner(t *testing.T) {
	history := `
CREATE ROLE app_owner;
CREATE ROLE deployer;
GRANT app_owner TO deployer;
CREATE TABLE t (id int);
ALTER TABLE t OWNER TO app_owner;
SET ROLE deployer;
GRANT SELECT ON t TO reader;
RESET ROLE;`
	got, _ := section(t, history, "CREATE TABLE t (id int);")
	assertStatements(t, got, "ALTER TABLE public.t OWNER TO app_owner", "GRANT SELECT ON TABLE public.t TO reader")
	assertNoStatements(t, got, "SET ROLE")
}

func TestSessionAuthorizationAndLocalRole(t *testing.T) {
	history := `
SET SESSION AUTHORIZATION app_owner;
CREATE TABLE by_session (id int);
SET ROLE NONE;
CREATE TABLE still_session (id int);
RESET SESSION AUTHORIZATION;
BEGIN;
SET LOCAL ROLE local_owner;
CREATE TABLE by_local (id int);
COMMIT;
CREATE TABLE after_commit (id int);
SET LOCAL ROLE ignored_owner;
CREATE TABLE outside_transaction (id int);`
	baseline := `CREATE TABLE by_session (id int); CREATE TABLE still_session (id int); CREATE TABLE by_local (id int);
CREATE TABLE after_commit (id int); CREATE TABLE outside_transaction (id int);`
	got, warnings := section(t, history, baseline)
	assertStatements(t, got,
		"ALTER TABLE public.by_session OWNER TO app_owner",
		"ALTER TABLE public.still_session OWNER TO app_owner",
		"ALTER TABLE public.by_local OWNER TO local_owner")
	assertNoStatements(t, got, "after_commit OWNER", "outside_transaction OWNER", "ignored_owner")
	if len(warnings) != 1 || !strings.Contains(warnings[0], "SET LOCAL ROLE outside") {
		t.Errorf("want one warning about SET LOCAL outside a transaction, got %v", warnings)
	}
}
