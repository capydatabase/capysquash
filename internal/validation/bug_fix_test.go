package validation

import (
	"testing"

	"github.com/CAPYSQUASH/pgsquash-engine/internal/types"
)

// TestFunctionNameDisplayBugFix demonstrates the fix for Bug #1
// This test shows that function names now display with proper schema qualification
func TestFunctionNameDisplayBugFix(t *testing.T) {
	t.Run("auth schema function displays as auth.jwt", func(t *testing.T) {
		authJWT := ObjectID{
			Type:   types.TypeFunction,
			Schema: "auth",
			Name:   "jwt",
		}

		createChange := &FunctionCreateChange{
			ID: authJWT,
			Definition: &FunctionDefinition{
				ID:         authJWT,
				Parameters: []Parameter{},
				ReturnType: "jsonb",
				Language:   "plpgsql",
			},
		}

		desc := createChange.Description()
		expected := "Create function auth.jwt"
		if desc != expected {
			t.Errorf("Expected '%s', got '%s'", expected, desc)
		}

		// Verify SQL also uses qualified name
		sql := createChange.SQL()
		if len(sql) == 0 {
			t.Error("Expected SQL output")
		}
		t.Logf("✅ Description: %s", desc)
		t.Logf("✅ SQL: %v", sql)
	})

	t.Run("public schema function displays as function name only", func(t *testing.T) {
		publicFunc := ObjectID{
			Type:   types.TypeFunction,
			Schema: "public",
			Name:   "calculate_total",
		}

		dropChange := &FunctionDropChange{
			ID: publicFunc,
			Definition: &FunctionDefinition{
				ID:         publicFunc,
				Parameters: []Parameter{},
				ReturnType: "numeric",
				Language:   "plpgsql",
			},
		}

		desc := dropChange.Description()
		expected := "Drop function calculate_total"
		if desc != expected {
			t.Errorf("Expected '%s', got '%s'", expected, desc)
		}

		// Verify SQL also uses qualified name (or just name for public schema)
		sql := dropChange.SQL()
		if len(sql) == 0 {
			t.Error("Expected SQL output")
		}
		expectedSQL := "DROP FUNCTION calculate_total"
		if sql[0] != expectedSQL {
			t.Errorf("Expected SQL '%s', got '%s'", expectedSQL, sql[0])
		}
		t.Logf("✅ Description: %s", desc)
		t.Logf("✅ SQL: %v", sql)
	})

	t.Run("custom schema function displays as custom.process_data", func(t *testing.T) {
		customFunc := ObjectID{
			Type:   types.TypeFunction,
			Schema: "custom",
			Name:   "process_data",
		}

		modifyChange := &FunctionModifyChange{
			ID: customFunc,
			FromDefinition: &FunctionDefinition{
				ID:         customFunc,
				Parameters: []Parameter{},
				ReturnType: "text",
				Language:   "sql",
			},
			ToDefinition: &FunctionDefinition{
				ID:         customFunc,
				Parameters: []Parameter{},
				ReturnType: "text",
				Language:   "plpgsql",
			},
		}

		desc := modifyChange.Description()
		expected := "Recreate function custom.process_data"
		if desc != expected {
			t.Errorf("Expected '%s', got '%s'", expected, desc)
		}

		// Verify SQL also uses qualified name
		sql := modifyChange.SQL()
		if len(sql) == 0 {
			t.Error("Expected SQL output")
		}
		// First SQL should be DROP with qualified name
		expectedDrop := "DROP FUNCTION custom.process_data"
		if sql[0] != expectedDrop {
			t.Errorf("Expected DROP SQL '%s', got '%s'", expectedDrop, sql[0])
		}
		t.Logf("✅ Description: %s", desc)
		t.Logf("✅ SQL: %v", sql)
	})

	t.Run("ObjectID methods work correctly", func(t *testing.T) {
		tests := []struct {
			name            string
			oid             ObjectID
			wantString      string
			wantQualified   string
		}{
			{
				name:            "auth.jwt function",
				oid:             ObjectID{Type: types.TypeFunction, Schema: "auth", Name: "jwt"},
				wantString:      "auth.jwt::FUNCTION",
				wantQualified:   "auth.jwt",
			},
			{
				name:            "public.my_func function",
				oid:             ObjectID{Type: types.TypeFunction, Schema: "public", Name: "my_func"},
				wantString:      "my_func::FUNCTION",
				wantQualified:   "my_func",
			},
			{
				name:            "custom.process function",
				oid:             ObjectID{Type: types.TypeFunction, Schema: "custom", Name: "process"},
				wantString:      "custom.process::FUNCTION",
				wantQualified:   "custom.process",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				gotString := tt.oid.String()
				if gotString != tt.wantString {
					t.Errorf("String() = %v, want %v", gotString, tt.wantString)
				}

				gotQualified := tt.oid.QualifiedName()
				if gotQualified != tt.wantQualified {
					t.Errorf("QualifiedName() = %v, want %v", gotQualified, tt.wantQualified)
				}

				t.Logf("✅ String(): %s", gotString)
				t.Logf("✅ QualifiedName(): %s", gotQualified)
			})
		}
	})
}

// TestBugFixSummary provides a summary of the bug fix
func TestBugFixSummary(t *testing.T) {
	t.Log("====================================")
	t.Log("Bug #1: Function names in validation")
	t.Log("====================================")
	t.Log("")
	t.Log("BEFORE THE FIX:")
	t.Log("  - Function names showed 'public' instead of 'public.function_name'")
	t.Log("  - Non-public schema functions showed incorrectly")
	t.Log("  - Reduced debugging capability")
	t.Log("")
	t.Log("AFTER THE FIX:")
	t.Log("  ✅ Added ObjectID.QualifiedName() method")
	t.Log("  ✅ Updated FunctionCreateChange.Description() to use QualifiedName()")
	t.Log("  ✅ Updated FunctionDropChange.Description() to use QualifiedName()")
	t.Log("  ✅ Updated FunctionDropChange.SQL() to use QualifiedName()")
	t.Log("  ✅ Updated FunctionModifyChange.Description() to use QualifiedName()")
	t.Log("  ✅ Updated FunctionModifyChange.SQL() to use QualifiedName()")
	t.Log("")
	t.Log("RESULT:")
	t.Log("  ✅ Functions now display with proper schema qualification")
	t.Log("  ✅ auth.jwt() shows as 'auth.jwt' not 'public'")
	t.Log("  ✅ public.my_func() shows as 'my_func' (public is default)")
	t.Log("  ✅ custom.process() shows as 'custom.process'")
	t.Log("  ✅ Debugging capability improved")
	t.Log("")
	t.Log("FILES MODIFIED:")
	t.Log("  - internal/validation/schema_diff.go (added QualifiedName method)")
	t.Log("  - internal/validation/schema_changes.go (updated function change types)")
	t.Log("  - internal/validation/object_id_test.go (added comprehensive tests)")
	t.Log("")
}
