package metadata

import (
	"reflect"
	"strings"
	"testing"
)

// fixtureMetadata builds a small but complete model: one table with columns,
// constraints, an index, a trigger and a policy, plus a view, a materialized
// view, two overloads of one function, a sequence, an enum and a domain.
func fixtureMetadata() *DatabaseMetadata {
	users := &TableMetadata{
		Name:   "users",
		Schema: "public",
		Columns: []*ColumnMetadata{
			{Name: "id", DataType: "bigint", IsIdentity: true, IdentityGeneration: "ALWAYS"},
			{Name: "email", DataType: "text", Collation: `"C"`},
			{Name: "status", DataType: "public.user_status", IsNullable: true, DefaultValue: "'active'::public.user_status"},
			{Name: "email_lower", DataType: "text", IsNullable: true, IsGenerated: true, GenerationExpr: "lower(email)"},
		},
		Constraints: []*ConstraintMetadata{
			{Name: "users_pkey", Type: "PRIMARY KEY", Definition: "PRIMARY KEY (id)"},
			{Name: "users_email_check", Type: "CHECK", Definition: "CHECK (length(email) > 3)"},
		},
		Indexes: []*IndexMetadata{
			{Name: "users_pkey", Definition: "CREATE UNIQUE INDEX users_pkey ON public.users USING btree (id)"},
			{Name: "users_email_idx", Definition: "CREATE INDEX users_email_idx ON public.users USING btree (email)"},
		},
		Triggers: []*TriggerMetadata{
			{Name: "users_touch", Definition: "CREATE TRIGGER users_touch BEFORE UPDATE ON public.users FOR EACH ROW EXECUTE FUNCTION public.touch()"},
		},
		Policies: []*PolicyMetadata{
			{Name: "users_read", Command: "SELECT", Permissive: true, Roles: []string{"public"}, Using: "(id > 0)"},
		},
		RowSecurity: true,
	}
	return &DatabaseMetadata{
		Schemas: map[string]*SchemaMetadata{
			"public": {
				Name:   "public",
				Tables: map[string]*TableMetadata{"users": users},
				Views: map[string]*ViewMetadata{
					"active_users": {Name: "active_users", Definition: " SELECT id\n   FROM public.users\n  WHERE status = 'active'::public.user_status;"},
				},
				MaterializedViews: map[string]*MaterializedViewMetadata{
					"user_counts": {ViewMetadata: &ViewMetadata{Name: "user_counts", Definition: " SELECT count(*) AS count FROM public.users;"}},
				},
				Functions: map[string][]*FunctionMetadata{
					"touch": {{Name: "touch", Signature: "", Body: "CREATE OR REPLACE FUNCTION public.touch() RETURNS trigger LANGUAGE plpgsql AS $$BEGIN RETURN NEW; END$$"}},
					"score": {
						{Name: "score", Signature: "a integer", Body: "CREATE OR REPLACE FUNCTION public.score(a integer) RETURNS integer LANGUAGE sql AS $$SELECT a$$"},
						{Name: "score", Signature: "a text", Body: "CREATE OR REPLACE FUNCTION public.score(a text) RETURNS integer LANGUAGE sql AS $$SELECT length(a)$$"},
					},
				},
				Sequences: map[string]*SequenceMetadata{
					"invoice_seq": {Name: "invoice_seq", DataType: "bigint", Start: 1000, Increment: 1, MinValue: 1, MaxValue: 9223372036854775807, Cache: 1, OwnedBy: "public.users.id"},
				},
				Types: map[string]*TypeMetadata{
					"user_status": {Name: "user_status", Type: "ENUM", Elements: []string{"active", "disabled"}},
					"email":       {Name: "email", Type: "DOMAIN", Definition: "text CONSTRAINT email_check CHECK (VALUE ~~ '%@%'::text)"},
				},
			},
		},
		Extensions: map[string]*ExtensionMetadata{
			"plpgsql":  {Name: "plpgsql", Version: "1.0"},
			"pgcrypto": {Name: "pgcrypto", Version: "1.3"},
		},
	}
}

func compareFixture(t *testing.T, mutate func(expected, actual *DatabaseMetadata)) *ComparisonResult {
	t.Helper()
	expected, actual := fixtureMetadata(), fixtureMetadata()
	mutate(expected, actual)
	return CompareDatabaseMetadata(expected, actual, CompareOptions{})
}

func publicSchema(meta *DatabaseMetadata) *SchemaMetadata { return meta.Schemas["public"] }

func usersTable(meta *DatabaseMetadata) *TableMetadata { return publicSchema(meta).Tables["users"] }

func requireSingleDrift(t *testing.T, result *ComparisonResult, want SchemaDrift) {
	t.Helper()
	if result.IsValid {
		t.Fatalf("IsValid = true, want false")
	}
	if len(result.SchemaDrift) != 1 || len(result.TypeMismatches) != 0 || len(result.ConstraintConflicts) != 0 {
		t.Fatalf("want exactly one drift entry, got drift=%+v types=%+v constraints=%+v",
			result.SchemaDrift, result.TypeMismatches, result.ConstraintConflicts)
	}
	got := result.SchemaDrift[0]
	if got.Object != want.Object || got.ObjectType != want.ObjectType || got.DriftType != want.DriftType ||
		!strings.Contains(got.Description, want.Description) {
		t.Fatalf("drift = %+v, want %+v", got, want)
	}
}

func TestCompareDatabaseMetadataIdenticalModels(t *testing.T) {
	result := compareFixture(t, func(_, _ *DatabaseMetadata) {})
	if !result.IsValid {
		t.Fatalf("identical models reported differences: %+v", result)
	}
	if len(result.SchemaDrift)+len(result.TypeMismatches)+len(result.ConstraintConflicts)+len(result.MissingExtensions)+len(result.Warnings) != 0 {
		t.Fatalf("identical models produced entries: %+v", result)
	}
}

func TestCompareDatabaseMetadataIgnoresWhitespace(t *testing.T) {
	result := compareFixture(t, func(_, actual *DatabaseMetadata) {
		publicSchema(actual).Views["active_users"].Definition = "SELECT id FROM public.users WHERE status = 'active'::public.user_status"
	})
	if !result.IsValid {
		t.Fatalf("whitespace-only difference reported: %+v", result.SchemaDrift)
	}
}

func TestCompareDatabaseMetadataColumnType(t *testing.T) {
	result := compareFixture(t, func(_, actual *DatabaseMetadata) {
		usersTable(actual).Columns[1].DataType = "character varying(255)"
	})
	if result.IsValid || len(result.SchemaDrift) != 0 {
		t.Fatalf("want only a type mismatch, got %+v", result)
	}
	want := []TypeMismatch{{Object: "public.users", Column: "email", ExpectedType: "text", ActualType: "character varying(255)", IsBreaking: true}}
	if !reflect.DeepEqual(result.TypeMismatches, want) {
		t.Fatalf("type mismatches = %+v, want %+v", result.TypeMismatches, want)
	}
}

func TestCompareDatabaseMetadataColumnAttributes(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(column *ColumnMetadata)
		prop   string
	}{
		{"nullability", func(c *ColumnMetadata) { c.IsNullable = false }, "nullable differs"},
		{"default", func(c *ColumnMetadata) { c.DefaultValue = "'disabled'::public.user_status" }, "default differs"},
		{"generated", func(c *ColumnMetadata) { c.GenerationExpr = "upper(email)" }, "generation expression differs"},
		{"identity", func(c *ColumnMetadata) { c.IdentityGeneration = "BY DEFAULT" }, "identity differs"},
		{"collation", func(c *ColumnMetadata) { c.Collation = `"en_US"` }, "collation differs"},
	}
	columnFor := map[string]int{"nullability": 2, "default": 2, "generated": 3, "identity": 0, "collation": 1}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var columnName string
			result := compareFixture(t, func(_, actual *DatabaseMetadata) {
				column := usersTable(actual).Columns[columnFor[tt.name]]
				columnName = column.Name
				tt.mutate(column)
			})
			requireSingleDrift(t, result, SchemaDrift{
				Object: "public.users." + columnName, ObjectType: "COLUMN",
				DriftType: "definition_mismatch", Description: tt.prop,
			})
		})
	}
}

func TestCompareDatabaseMetadataColumnPresenceAndOrder(t *testing.T) {
	result := compareFixture(t, func(expected, actual *DatabaseMetadata) {
		usersTable(expected).Columns = append(usersTable(expected).Columns, &ColumnMetadata{Name: "legacy", DataType: "text", IsNullable: true})
		usersTable(actual).Columns = append(usersTable(actual).Columns, &ColumnMetadata{Name: "added", DataType: "text", IsNullable: true})
	})
	got := driftSummary(result)
	want := []string{
		"missing_in_db COLUMN public.users.added",
		"extra_in_db COLUMN public.users.legacy",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("drift = %v, want %v", got, want)
	}

	result = compareFixture(t, func(_, actual *DatabaseMetadata) {
		cols := usersTable(actual).Columns
		cols[1], cols[2] = cols[2], cols[1]
	})
	requireSingleDrift(t, result, SchemaDrift{
		Object: "public.users", ObjectType: "TABLE", DriftType: "definition_mismatch",
		Description: "column order differs: production (id, email, status, email_lower), baseline (id, status, email, email_lower)",
	})
}

func TestCompareDatabaseMetadataConstraints(t *testing.T) {
	result := compareFixture(t, func(expected, actual *DatabaseMetadata) {
		usersTable(actual).Constraints[1].Definition = "CHECK (length(email) > 5)"
		usersTable(expected).Constraints = append(usersTable(expected).Constraints,
			&ConstraintMetadata{Name: "users_email_key", Type: "UNIQUE", Definition: "UNIQUE (email)"})
		usersTable(actual).Constraints = append(usersTable(actual).Constraints,
			&ConstraintMetadata{Name: "users_status_check", Type: "CHECK", Definition: "CHECK (status IS NOT NULL)"})
	})
	want := []ConstraintConflict{
		{Table: "public.users", ConstraintName: "users_email_check", ExpectedDef: "check ( length ( email ) > 3 )", ActualDef: "check ( length ( email ) > 5 )", ConflictType: "different"},
		{Table: "public.users", ConstraintName: "users_email_key", ExpectedDef: "unique ( email )", ConflictType: "missing"},
		{Table: "public.users", ConstraintName: "users_status_check", ActualDef: "check ( status is not null )", ConflictType: "extra"},
	}
	if result.IsValid || len(result.SchemaDrift) != 0 || !reflect.DeepEqual(result.ConstraintConflicts, want) {
		t.Fatalf("constraint conflicts = %+v (drift %+v), want %+v", result.ConstraintConflicts, result.SchemaDrift, want)
	}
}

func TestCompareDatabaseMetadataDefinitions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(actual *DatabaseMetadata)
		want   SchemaDrift
	}{
		{
			name: "index",
			mutate: func(a *DatabaseMetadata) {
				usersTable(a).Indexes[1].Definition = "CREATE INDEX users_email_idx ON public.users USING hash (email)"
			},
			want: SchemaDrift{Object: "public.users.users_email_idx", ObjectType: "INDEX", Description: "definition differs"},
		},
		{
			name: "view",
			mutate: func(a *DatabaseMetadata) {
				publicSchema(a).Views["active_users"].Definition = "SELECT id FROM public.users"
			},
			want: SchemaDrift{Object: "public.active_users", ObjectType: "VIEW", Description: "definition differs"},
		},
		{
			name: "materialized view",
			mutate: func(a *DatabaseMetadata) {
				publicSchema(a).MaterializedViews["user_counts"].Definition = "SELECT 1 AS count"
			},
			want: SchemaDrift{Object: "public.user_counts", ObjectType: "MATERIALIZED VIEW", Description: "definition differs"},
		},
		{
			name: "trigger",
			mutate: func(a *DatabaseMetadata) {
				usersTable(a).Triggers[0].Definition = "CREATE TRIGGER users_touch AFTER UPDATE ON public.users FOR EACH ROW EXECUTE FUNCTION public.touch()"
			},
			want: SchemaDrift{Object: "public.users.users_touch", ObjectType: "TRIGGER", Description: "definition differs"},
		},
		{
			name:   "policy roles",
			mutate: func(a *DatabaseMetadata) { usersTable(a).Policies[0].Roles = []string{"authenticated"} },
			want:   SchemaDrift{Object: "public.users.users_read", ObjectType: "POLICY", Description: "roles differs: production public, baseline authenticated"},
		},
		{
			name:   "policy using",
			mutate: func(a *DatabaseMetadata) { usersTable(a).Policies[0].Using = "true" },
			want:   SchemaDrift{Object: "public.users.users_read", ObjectType: "POLICY", Description: "using differs"},
		},
		{
			name:   "policy with check",
			mutate: func(a *DatabaseMetadata) { usersTable(a).Policies[0].WithCheck = "(id > 1)" },
			want:   SchemaDrift{Object: "public.users.users_read", ObjectType: "POLICY", Description: "with check differs: production (none), baseline"},
		},
		{
			name:   "policy permissive",
			mutate: func(a *DatabaseMetadata) { usersTable(a).Policies[0].Permissive = false },
			want:   SchemaDrift{Object: "public.users.users_read", ObjectType: "POLICY", Description: "permissive differs"},
		},
		{
			name:   "row level security",
			mutate: func(a *DatabaseMetadata) { usersTable(a).RowSecurity = false },
			want:   SchemaDrift{Object: "public.users", ObjectType: "TABLE", Description: "row level security differs"},
		},
		{
			name:   "sequence",
			mutate: func(a *DatabaseMetadata) { publicSchema(a).Sequences["invoice_seq"].Start = 1 },
			want:   SchemaDrift{Object: "public.invoice_seq", ObjectType: "SEQUENCE", Description: "start differs: production 1000, baseline 1"},
		},
		{
			name:   "sequence owner",
			mutate: func(a *DatabaseMetadata) { publicSchema(a).Sequences["invoice_seq"].OwnedBy = "" },
			want:   SchemaDrift{Object: "public.invoice_seq", ObjectType: "SEQUENCE", Description: "owned by differs: production public.users.id, baseline (none)"},
		},
		{
			name: "enum label order",
			mutate: func(a *DatabaseMetadata) {
				publicSchema(a).Types["user_status"].Elements = []string{"disabled", "active"}
			},
			want: SchemaDrift{Object: "public.user_status", ObjectType: "TYPE", Description: `enum labels differs: production "active", "disabled", baseline "disabled", "active"`},
		},
		{
			name: "enum label case",
			mutate: func(a *DatabaseMetadata) {
				publicSchema(a).Types["user_status"].Elements = []string{"Active", "disabled"}
			},
			want: SchemaDrift{Object: "public.user_status", ObjectType: "TYPE", Description: "enum labels differs"},
		},
		{
			name: "domain",
			mutate: func(a *DatabaseMetadata) {
				publicSchema(a).Types["email"].Definition = "text"
			},
			want: SchemaDrift{Object: "public.email", ObjectType: "TYPE", Description: "definition differs"},
		},
		{
			name: "function overload",
			mutate: func(a *DatabaseMetadata) {
				publicSchema(a).Functions["score"][1].Body = "CREATE OR REPLACE FUNCTION public.score(a text) RETURNS integer LANGUAGE sql AS $$SELECT 0$$"
			},
			want: SchemaDrift{Object: "public.score(a text)", ObjectType: "FUNCTION", Description: "definition differs"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := compareFixture(t, func(_, actual *DatabaseMetadata) { tt.mutate(actual) })
			tt.want.DriftType = "definition_mismatch"
			requireSingleDrift(t, result, tt.want)
		})
	}
}

func TestCompareDatabaseMetadataOverloadsAreDistinct(t *testing.T) {
	result := compareFixture(t, func(_, actual *DatabaseMetadata) {
		// The baseline lost the text overload but kept the integer one.
		publicSchema(actual).Functions["score"] = publicSchema(actual).Functions["score"][:1]
	})
	requireSingleDrift(t, result, SchemaDrift{
		Object: "public.score(a text)", ObjectType: "FUNCTION", DriftType: "extra_in_db",
		Description: "exists in production but is not created by the baseline",
	})
}

func TestCompareDatabaseMetadataReportsOutermostMissingObject(t *testing.T) {
	result := compareFixture(t, func(expected, actual *DatabaseMetadata) {
		delete(publicSchema(actual).Tables, "users")
		actual.Schemas["audit"] = &SchemaMetadata{
			Name: "audit",
			Tables: map[string]*TableMetadata{"events": {
				Name: "events", Schema: "audit",
				Columns:     []*ColumnMetadata{{Name: "id", DataType: "bigint"}},
				Constraints: []*ConstraintMetadata{{Name: "events_pkey", Type: "PRIMARY KEY", Definition: "PRIMARY KEY (id)"}},
			}},
		}
	})
	got := driftSummary(result)
	want := []string{
		"missing_in_db SCHEMA audit",
		"extra_in_db TABLE public.users",
	}
	if !reflect.DeepEqual(got, want) || len(result.ConstraintConflicts) != 0 || len(result.TypeMismatches) != 0 {
		t.Fatalf("drift = %v constraints = %+v, want only %v", got, result.ConstraintConflicts, want)
	}
}

func TestCompareDatabaseMetadataExtensions(t *testing.T) {
	result := compareFixture(t, func(expected, actual *DatabaseMetadata) {
		actual.Extensions["pgcrypto"].Version = "1.4"
		actual.Extensions["citext"] = &ExtensionMetadata{Name: "citext", Version: "1.6"}
		expected.Extensions["pg_stat_statements"] = &ExtensionMetadata{Name: "pg_stat_statements", Version: "1.11"}
	})
	if result.IsValid {
		t.Fatal("an extension missing from production must invalidate the result")
	}
	if !reflect.DeepEqual(result.MissingExtensions, []string{"citext"}) {
		t.Fatalf("missing extensions = %v", result.MissingExtensions)
	}
	wantWarnings := []string{
		"Extension pgcrypto version differs: production 1.3, baseline 1.4",
		"Extension pg_stat_statements is installed in production but not created by the baseline",
	}
	if !reflect.DeepEqual(result.Warnings, wantWarnings) {
		t.Fatalf("warnings = %q, want %q", result.Warnings, wantWarnings)
	}

	result = compareFixture(t, func(expected, actual *DatabaseMetadata) {
		actual.Extensions["pgcrypto"].Version = "1.4"
	})
	if !result.IsValid || len(result.Warnings) != 1 {
		t.Fatalf("a version difference must only warn: %+v", result)
	}
}

func TestCompareDatabaseMetadataScopeAndEnvironment(t *testing.T) {
	expected, actual := fixtureMetadata(), fixtureMetadata()
	// Production has a platform schema the migrations never touch.
	expected.Schemas["auth"] = &SchemaMetadata{Name: "auth", Tables: map[string]*TableMetadata{
		"users": {Name: "users", Schema: "auth", Columns: []*ColumnMetadata{{Name: "id", DataType: "uuid"}}},
	}}
	// The scratch database got an auth compatibility function in public
	// before the baseline ran.
	environment := &DatabaseMetadata{
		Schemas: map[string]*SchemaMetadata{"public": {Name: "public", Functions: map[string][]*FunctionMetadata{
			"current_user_id": {{Name: "current_user_id", Body: "CREATE FUNCTION public.current_user_id() ..."}},
		}}},
		Extensions: map[string]*ExtensionMetadata{"plpgsql": {Name: "plpgsql", Version: "1.0"}},
	}
	publicSchema(actual).Functions["current_user_id"] = environment.Schemas["public"].Functions["current_user_id"]

	result := CompareDatabaseMetadata(expected, actual, CompareOptions{Schemas: []string{"public"}, Environment: environment})
	if !result.IsValid {
		t.Fatalf("out-of-scope schema and environment objects must be ignored: %+v", result)
	}

	result = CompareDatabaseMetadata(expected, actual, CompareOptions{Environment: environment})
	if got := driftSummary(result); !reflect.DeepEqual(got, []string{"extra_in_db SCHEMA auth"}) {
		t.Fatalf("unscoped comparison drift = %v", got)
	}
}

// The baseline grants and revokes on objects that existed before it ran
// (the public schema): their owners and privileges are compared too, even
// in a schema the baseline creates nothing in, while pre-existing objects
// only one side has stay out.
func TestCompareDatabaseMetadataComparesPrivilegesOfPreexistingObjects(t *testing.T) {
	publicDefault := PrivilegeMetadata{Kind: "schema", Schema: "public", Name: "public", Owner: "pg_database_owner",
		ACL: "PUBLIC=USAGE/pg_database_owner,pg_database_owner=CREATE/pg_database_owner,pg_database_owner=USAGE/pg_database_owner"}
	extensionFunction := PrivilegeMetadata{Kind: "routine", Schema: "extensions", Name: "uuid_nil()", Owner: DatabaseOwnerRole,
		ACL: "PUBLIC=EXECUTE/<database owner>"}
	environment := &DatabaseMetadata{
		Schemas:    map[string]*SchemaMetadata{"public": {Name: "public"}},
		Privileges: []PrivilegeMetadata{publicDefault, extensionFunction},
	}
	revoked := publicDefault
	revoked.ACL = "pg_database_owner=CREATE/pg_database_owner,pg_database_owner=USAGE/pg_database_owner,reader=USAGE/pg_database_owner"

	expected, actual := fixtureMetadata(), fixtureMetadata()
	expected.Privileges = []PrivilegeMetadata{revoked}
	actual.Privileges = []PrivilegeMetadata{publicDefault, extensionFunction}
	result := CompareDatabaseMetadata(expected, actual, CompareOptions{Schemas: []string{"app"}, Environment: environment})
	requireSingleDrift(t, result, SchemaDrift{Object: "schema public", ObjectType: kindPrivileges, DriftType: "definition_mismatch", Description: "reader=USAGE"})

	actual.Privileges = []PrivilegeMetadata{revoked, extensionFunction}
	if result := CompareDatabaseMetadata(expected, actual, CompareOptions{Schemas: []string{"app"}, Environment: environment}); !result.IsValid {
		t.Fatalf("matching privileges of a pre-existing object must compare equal: %+v", result.SchemaDrift)
	}
}

func TestCompareDatabaseMetadataIsDeterministic(t *testing.T) {
	mutate := func(expected, actual *DatabaseMetadata) {
		for _, name := range []string{"t3", "t1", "t2"} {
			publicSchema(expected).Tables[name] = &TableMetadata{Name: name, Schema: "public"}
		}
		for _, name := range []string{"z_view", "a_view", "m_view"} {
			publicSchema(actual).Views[name] = &ViewMetadata{Name: name, Definition: "SELECT 1"}
		}
		usersTable(actual).Columns[0].DataType = "integer"
		usersTable(actual).Columns[1].DataType = "varchar"
	}
	first := driftSummary(compareFixture(t, mutate))
	want := []string{
		"extra_in_db TABLE public.t1",
		"extra_in_db TABLE public.t2",
		"extra_in_db TABLE public.t3",
		"missing_in_db VIEW public.a_view",
		"missing_in_db VIEW public.m_view",
		"missing_in_db VIEW public.z_view",
	}
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("drift = %v, want %v", first, want)
	}
	for range 20 {
		result := compareFixture(t, mutate)
		if got := driftSummary(result); !reflect.DeepEqual(got, first) {
			t.Fatalf("drift order changed: %v vs %v", got, first)
		}
		if result.TypeMismatches[0].Column != "email" || result.TypeMismatches[1].Column != "id" {
			t.Fatalf("type mismatch order changed: %+v", result.TypeMismatches)
		}
	}
}

func driftSummary(result *ComparisonResult) []string {
	summary := make([]string, 0, len(result.SchemaDrift))
	for _, drift := range result.SchemaDrift {
		summary = append(summary, drift.DriftType+" "+drift.ObjectType+" "+drift.Object)
	}
	return summary
}
