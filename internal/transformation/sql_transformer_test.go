package transformation

import (
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

func TestReplaceBareLengthFunction(t *testing.T) {
	input := "SELECT length(name), char_length(name), character_length(name), my_length(name) FROM users"
	got := replaceBareLengthFunction(input)

	if !strings.Contains(got, "char_length(name)") {
		t.Fatalf("expected length(name) rewrite, got: %s", got)
	}
	if strings.Contains(got, "char_char_length") {
		t.Fatalf("unexpected double rewrite: %s", got)
	}
	if strings.Contains(got, "character_char_length") {
		t.Fatalf("unexpected rewrite of character_length: %s", got)
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
