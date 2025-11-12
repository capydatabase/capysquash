package validation

import (
	"strings"
	"testing"
)

func TestExtractShortNameHandlesDottedIdentifiers(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		definition string
		expected   string
	}{
		{
			name:       "unquoted schema and table",
			definition: "CREATE TABLE public.admin_action_logs (id bigint NOT NULL);",
			expected:   "public.admin_action_logs",
		},
		{
			name:       "quoted table identifier",
			definition: "CREATE TABLE public.\"admin_action_logs\" (id bigint NOT NULL);",
			expected:   "public.admin_action_logs",
		},
		{
			name:       "quoted schema and table identifiers",
			definition: "CREATE TABLE \"public\".\"admin_action_logs\" (id bigint NOT NULL);",
			expected:   "public.admin_action_logs",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := extractShortName(tc.definition); got != tc.expected {
				t.Fatalf("extractShortName(%q) = %q, expected %q", tc.definition, got, tc.expected)
			}
		})
	}
}

func TestExtractObjectsStripsLeadingComments(t *testing.T) {
	t.Parallel()

	ns := &NormalizedSchema{
		Normalized: `-- Name: public.rooms; Type: TABLE; Schema: public
-- some descriptive comment
CREATE TABLE public.rooms (
  id uuid PRIMARY KEY
);`,
	}

	ns.extractObjects()

	if len(ns.Tables) != 1 {
		t.Fatalf("expected 1 table, got %d (tables=%v)", len(ns.Tables), ns.Tables)
	}

	expectedPrefix := "CREATE TABLE public.rooms"
	if !strings.HasPrefix(ns.Tables[0], expectedPrefix) {
		t.Fatalf("expected table block to start with %q, got %q", expectedPrefix, ns.Tables[0])
	}
}
