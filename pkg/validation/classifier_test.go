package validation

import "testing"

func TestClassifyStatements(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		wantClass []StatementClass
	}{
		{
			name:      "simple select",
			sql:       "SELECT * FROM users;",
			wantClass: []StatementClass{StatementReadOnly},
		},
		{
			name:      "select with comments",
			sql:       "-- read only\nSELECT now();",
			wantClass: []StatementClass{StatementReadOnly},
		},
		{
			name:      "values",
			sql:       "VALUES (1), (2);",
			wantClass: []StatementClass{StatementReadOnly},
		},
		{
			name:      "table shorthand",
			sql:       "TABLE users;",
			wantClass: []StatementClass{StatementReadOnly},
		},
		{
			name:      "show",
			sql:       "SHOW search_path;",
			wantClass: []StatementClass{StatementReadOnly},
		},
		{
			name:      "read only cte",
			sql:       "WITH recent AS (SELECT * FROM users) SELECT * FROM recent;",
			wantClass: []StatementClass{StatementReadOnly},
		},
		{
			name:      "insert",
			sql:       "INSERT INTO users(id) VALUES (1);",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name:      "update",
			sql:       "UPDATE users SET name = 'x';",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name:      "delete",
			sql:       "DELETE FROM users;",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name:      "merge",
			sql:       "MERGE INTO users u USING staged s ON u.id = s.id WHEN MATCHED THEN UPDATE SET name = s.name;",
			wantClass: []StatementClass{StatementWrite},
		},
		// --- audit bypass cases (F-39) ---
		{
			name:      "select into creates a table",
			sql:       "SELECT * INTO users_backup FROM users;",
			wantClass: []StatementClass{StatementDDL},
		},
		{
			name:      "cte with data-modifying statement",
			sql:       "WITH updated AS (UPDATE users SET name = 'x' RETURNING *) SELECT * FROM updated;",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name:      "nested cte with delete",
			sql:       "WITH a AS (WITH b AS (DELETE FROM users RETURNING id) SELECT * FROM b) SELECT * FROM a;",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name:      "setval side effect",
			sql:       "SELECT setval('users_id_seq', 100);",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name:      "nextval side effect",
			sql:       "SELECT nextval('users_id_seq');",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name:      "qualified setval side effect",
			sql:       "SELECT pg_catalog.setval('users_id_seq', 100);",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name:      "nextval buried in expression",
			sql:       "SELECT CASE WHEN true THEN nextval('s') ELSE 0 END;",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name:      "copy from",
			sql:       "COPY users FROM '/tmp/users.csv';",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name:      "copy to",
			sql:       "COPY users TO '/tmp/users.csv';",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name:      "do block is never read only",
			sql:       "DO $$ BEGIN PERFORM 1; END $$;",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name: "dollar quoting does not break statement splitting",
			sql:  "SELECT 1; DO $body$ BEGIN DELETE FROM users; END $body$; SELECT 2;",
			wantClass: []StatementClass{
				StatementReadOnly,
				StatementWrite,
				StatementReadOnly,
			},
		},
		{
			name:      "semicolon inside string literal",
			sql:       "SELECT 'a;b' AS v;",
			wantClass: []StatementClass{StatementReadOnly},
		},
		// --- DDL ---
		{
			name:      "create table",
			sql:       "CREATE TABLE audit_log(id bigint);",
			wantClass: []StatementClass{StatementDDL},
		},
		{
			name:      "alter table",
			sql:       "ALTER TABLE users ADD COLUMN age int;",
			wantClass: []StatementClass{StatementDDL},
		},
		{
			name:      "drop table",
			sql:       "DROP TABLE users;",
			wantClass: []StatementClass{StatementDDL},
		},
		{
			name:      "truncate",
			sql:       "TRUNCATE users;",
			wantClass: []StatementClass{StatementDDL},
		},
		{
			name:      "create index",
			sql:       "CREATE INDEX idx_users_name ON users(name);",
			wantClass: []StatementClass{StatementDDL},
		},
		{
			name:      "grant",
			sql:       "GRANT SELECT ON users TO analyst;",
			wantClass: []StatementClass{StatementDDL},
		},
		{
			name:      "create table as",
			sql:       "CREATE TABLE users_copy AS SELECT * FROM users;",
			wantClass: []StatementClass{StatementDDL},
		},
		// --- maintenance / session state ---
		{
			name:      "vacuum",
			sql:       "VACUUM users;",
			wantClass: []StatementClass{StatementMaintenance},
		},
		{
			name:      "analyze",
			sql:       "ANALYZE users;",
			wantClass: []StatementClass{StatementMaintenance},
		},
		{
			name:      "reindex",
			sql:       "REINDEX TABLE users;",
			wantClass: []StatementClass{StatementMaintenance},
		},
		{
			name:      "set",
			sql:       "SET search_path TO app;",
			wantClass: []StatementClass{StatementMaintenance},
		},
		{
			name:      "begin",
			sql:       "BEGIN;",
			wantClass: []StatementClass{StatementMaintenance},
		},
		{
			name:      "lock table",
			sql:       "LOCK TABLE users IN ACCESS EXCLUSIVE MODE;",
			wantClass: []StatementClass{StatementMaintenance},
		},
		// --- EXPLAIN ---
		{
			name:      "plain explain of delete does not execute",
			sql:       "EXPLAIN DELETE FROM users;",
			wantClass: []StatementClass{StatementReadOnly},
		},
		{
			name:      "explain analyze select",
			sql:       "EXPLAIN ANALYZE SELECT * FROM users;",
			wantClass: []StatementClass{StatementReadOnly},
		},
		{
			name:      "explain analyze delete executes the delete",
			sql:       "EXPLAIN ANALYZE DELETE FROM users;",
			wantClass: []StatementClass{StatementWrite},
		},
		// --- misc write-ish ---
		{
			name:      "call",
			sql:       "CALL cleanup_users();",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name:      "execute prepared",
			sql:       "EXECUTE plan(1);",
			wantClass: []StatementClass{StatementWrite},
		},
		{
			name:      "multiple statements mixed",
			sql:       "SELECT 1; DELETE FROM users;",
			wantClass: []StatementClass{StatementReadOnly, StatementWrite},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statements, err := ClassifyStatements(tt.sql)
			if err != nil {
				t.Fatalf("ClassifyStatements(%q) error: %v", tt.sql, err)
			}
			if len(statements) != len(tt.wantClass) {
				t.Fatalf(
					"ClassifyStatements(%q) returned %d statements, want %d: %+v",
					tt.sql, len(statements), len(tt.wantClass), statements,
				)
			}
			for i, statement := range statements {
				if statement.Class != tt.wantClass[i] {
					t.Errorf(
						"statement %d (%q) classified as %q (reason: %q), want %q",
						i, statement.SQL, statement.Class, statement.Reason, tt.wantClass[i],
					)
				}
			}
		})
	}
}

func TestClassifyStatementsParseError(t *testing.T) {
	if _, err := ClassifyStatements("SELEC broken syntax"); err == nil {
		t.Fatal("expected a parse error for invalid SQL, got nil")
	}
}

func TestIsReadOnlySQL(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		want    bool
		wantErr bool
	}{
		{name: "select", sql: "SELECT 1;", want: true},
		{name: "multi read only", sql: "SELECT 1; SHOW search_path;", want: true},
		{name: "mixed", sql: "SELECT 1; DELETE FROM users;", want: false},
		{name: "select into", sql: "SELECT 1 INTO t;", want: false},
		{name: "setval", sql: "SELECT setval('s', 1);", want: false},
		{name: "empty", sql: "", want: false},
		{name: "comments only", sql: "-- nothing here\n", want: false},
		{name: "parse error", sql: "not sql at all;", want: false, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IsReadOnlySQL(tt.sql)
			if (err != nil) != tt.wantErr {
				t.Fatalf("IsReadOnlySQL(%q) error = %v, wantErr %v", tt.sql, err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("IsReadOnlySQL(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}
