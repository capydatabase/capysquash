package ast

import (
	"strings"
	"testing"
)

func TestFunctionNormalizer_FixLanguageOrder(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantFix bool
	}{
		{
			name: "needs reordering - VOLATILE before LANGUAGE",
			input: `CREATE FUNCTION test_func() RETURNS text
				VOLATILE AS $$ SELECT 'test' $$ LANGUAGE plpgsql;`,
			wantFix: true,
		},
		{
			name: "correct order - LANGUAGE before VOLATILE",
			input: `CREATE FUNCTION test_func() RETURNS text
				LANGUAGE plpgsql VOLATILE AS $$ SELECT 'test' $$;`,
			wantFix: false,
		},
		{
			name: "no volatility marker",
			input: `CREATE FUNCTION test_func() RETURNS text
				LANGUAGE plpgsql AS $$ SELECT 'test' $$;`,
			wantFix: false,
		},
	}

	normalizer := NewFunctionNormalizer()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalizer.NormalizeAll(tt.input)
			if err != nil {
				t.Fatalf("NormalizeAll() error = %v", err)
			}

			if tt.wantFix {
				// Should have been modified
				if result == tt.input {
					t.Errorf("Expected SQL to be modified, but it wasn't")
				}
				t.Logf("normalized SQL: %s", result)
				// Result should have LANGUAGE declared in header
				upperResult := strings.ToUpper(result)
				if !strings.Contains(upperResult, "LANGUAGE") {
					t.Errorf("Result missing expected LANGUAGE declaration")
				}
			} else {
				// Should not have been modified (or minimal changes)
				if len(result) == 0 {
					t.Errorf("Result is empty")
				}
			}
		})
	}
}

func TestFunctionNormalizer_InferMissingLanguage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "infer plpgsql from RETURNS TRIGGER",
			input: `CREATE FUNCTION test_trigger() RETURNS TRIGGER AS $$
				BEGIN
					RETURN NEW;
				END;
			$$;`,
			expected: "plpgsql",
		},
		{
			name: "infer plpgsql from BEGIN/END",
			input: `CREATE FUNCTION test_func() RETURNS void AS $$
				BEGIN
					PERFORM something();
				END;
			$$;`,
			expected: "plpgsql",
		},
		{
			name: "default to sql for simple functions",
			input: `CREATE FUNCTION test_func() RETURNS text AS $$
				SELECT 'test'
			$$;`,
			expected: "sql",
		},
	}

	normalizer := NewFunctionNormalizer()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := normalizer.NormalizeAll(tt.input)
			if err != nil {
				t.Fatalf("NormalizeAll() error = %v", err)
			}

			// Check if result contains the expected language
			if !strings.Contains(strings.ToLower(result), "language "+tt.expected) {
				t.Errorf("Expected result to contain 'LANGUAGE %s', got: %s", tt.expected, result)
			}
		})
	}
}

func TestFunctionNormalizer_RemoveDuplicateLanguage(t *testing.T) {
	input := `CREATE FUNCTION test_func() RETURNS text
		LANGUAGE plpgsql LANGUAGE plpgsql AS $$ SELECT 'test' $$;`

	normalizer := NewFunctionNormalizer()
	result, err := normalizer.NormalizeAll(input)
	if err != nil {
		t.Fatalf("NormalizeAll() error = %v", err)
	}

	// Count occurrences of "LANGUAGE"
	count := strings.Count(strings.ToUpper(result), "LANGUAGE")
	if count > 1 {
		t.Errorf("Expected only one LANGUAGE declaration, got %d: %s", count, result)
	}
}
