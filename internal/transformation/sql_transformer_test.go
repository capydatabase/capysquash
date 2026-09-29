package transformation

import (
	"context"
	"strings"
	"testing"
)

func TestConvertInsertToSelect(t *testing.T) {
	tr := NewSQLTransformer(nil)

	got := tr.convertInsertToSelect("INSERT INTO public.users (id, email) VALUES (1, 'a@example.com');")
	if !strings.Contains(got, "-- INSERT validation: SELECT 1 as id, 'a@example.com' as email -- FROM public.users") {
		t.Fatalf("unexpected insert conversion: %s", got)
	}
}

func TestConvertUpdateToSelect(t *testing.T) {
	tr := NewSQLTransformer(nil)

	got := tr.convertUpdateToSelect("UPDATE users SET email = 'b@example.com', updated_at = NOW() WHERE id = 42;")
	if !strings.Contains(got, "-- UPDATE validation: SELECT 'b@example.com' as email, NOW() as updated_at FROM users WHERE id = 42") {
		t.Fatalf("unexpected update conversion: %s", got)
	}
}

func TestConvertDeleteToSelect(t *testing.T) {
	tr := NewSQLTransformer(nil)

	got := tr.convertDeleteToSelect("DELETE FROM users WHERE id = 42;")
	if got != "-- DELETE validation: SELECT COUNT(*) FROM users WHERE id = 42" {
		t.Fatalf("unexpected delete conversion: %s", got)
	}
}

// Modern-syntax transformation leaves function calls alone: renaming them
// changes catalog definitions, and position(a IN b) -> strpos(a IN b) is not
// even valid SQL.
func TestTransformKeepsFunctionCallsVerbatim(t *testing.T) {
	cfg := DefaultTransformationConfig()
	tr := NewSQLTransformer(cfg)
	sql := `CREATE TABLE users (name text CHECK (length(name) > 3), code text CHECK (position('x' IN code) = 0), tag text DEFAULT substr('abcdef', 1, 3));`

	result, err := tr.Transform(context.Background(), sql)
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if result.TransformedSQL != sql {
		t.Fatalf("Transform changed function calls:\n%s", result.TransformedSQL)
	}
}

func TestStatementDetectors(t *testing.T) {
	if !isInsertStatement("  INSERT INTO x VALUES (1)") {
		t.Fatal("expected insert detector true")
	}
	if !isUpdateStatement("update x set a = 1") {
		t.Fatal("expected update detector true")
	}
	if !isDeleteStatement("delete from x") {
		t.Fatal("expected delete detector true")
	}
	if !isDropTableStatement("DROP TABLE users") {
		t.Fatal("expected drop table detector true")
	}
	if !isDropColumnStatement("ALTER TABLE users DROP COLUMN legacy") {
		t.Fatal("expected drop column detector true")
	}
	if !isAlterTypeStatement("ALTER TABLE users ALTER COLUMN age TYPE bigint") {
		t.Fatal("expected alter type detector true")
	}
}

func TestExtractFunctionName(t *testing.T) {
	tr := NewSQLTransformer(nil)

	got := tr.extractFunctionName("CREATE OR REPLACE FUNCTION public.normalize_email(input text) RETURNS text AS $$ SELECT input $$ LANGUAGE sql;")
	if got != "normalize_email" {
		t.Fatalf("expected normalize_email, got %s", got)
	}
}

func TestWhereAndSelectHeuristics(t *testing.T) {
	if !hasSimpleWhereEquality("SELECT * FROM users WHERE id = 1") {
		t.Fatal("expected simple where equality true")
	}
	if hasSimpleWhereEquality("SELECT * FROM users WHERE id != 1") {
		t.Fatal("expected simple where equality false for !=")
	}
	if !isSelectStarFromSingleTable("SELECT * FROM users;") {
		t.Fatal("expected select-star detector true")
	}
	if isSelectStarFromSingleTable("SELECT * FROM users WHERE id = 1;") {
		t.Fatal("expected select-star detector false with WHERE")
	}
}

func TestFixCommentSyntaxReportsCommentsPostgreSQLWouldReject(t *testing.T) {
	tr := NewSQLTransformer(nil)
	sql := `CREATE FUNCTION public.f(a int) RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;
CREATE FUNCTION f(a text) RETURNS int LANGUAGE sql AS $$ SELECT 2 $$;
CREATE FUNCTION g() RETURNS int LANGUAGE sql AS $$ SELECT 3 $$;
COMMENT ON FUNCTION f(integer) IS 'ok';
COMMENT ON FUNCTION f(bigint) IS 'no such overload';
COMMENT ON FUNCTION public.f IS 'ambiguous';
COMMENT ON FUNCTION g IS 'unique, fine';
COMMENT ON FUNCTION extensions.uuid_generate_v4() IS 'not created here, not checked';`

	result := &TransformationResult{}
	if got := tr.fixCommentSyntax(sql, result); got != sql {
		t.Fatalf("fixCommentSyntax changed the SQL:\n%s", got)
	}
	if len(result.Warnings) != 2 {
		t.Fatalf("warnings = %q, want the bigint and the ambiguous comment", result.Warnings)
	}
	if !strings.Contains(result.Warnings[0], "public.f(bigint) matches no created overload ((integer), (text))") {
		t.Errorf("first warning = %q", result.Warnings[0])
	}
	if !strings.Contains(result.Warnings[1], "public.f names no arguments but 2 overloads are created") {
		t.Errorf("second warning = %q", result.Warnings[1])
	}
}
