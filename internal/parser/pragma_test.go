package parser

import (
	"testing"

	"github.com/capydatabase/capysquash/internal/types"
)

// AnalyzePragmas upper-cases the comment text before matching, so the markers
// it matches against must be upper-case too; a lower-case marker silently
// disables the pragmas.
func TestAnalyzePragmasMatchesCapysquashMarkers(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		stmt types.Statement
		want bool
	}{
		{
			name: "ignore pragma in an attached comment",
			stmt: types.Statement{Comments: []string{"-- capysquash:ignore"}},
			want: true,
		},
		{
			name: "no-merge pragma with a space in an attached comment",
			stmt: types.Statement{Comments: []string{"-- capysquash: no-merge"}},
			want: true,
		},
		{
			name: "ignore pragma inline in the statement SQL",
			stmt: types.Statement{SQL: "-- capysquash:ignore\nCREATE TABLE users (id SERIAL PRIMARY KEY);"},
			want: true,
		},
		{
			name: "plain comment",
			stmt: types.Statement{Comments: []string{"-- users"}, SQL: "CREATE TABLE users (id SERIAL PRIMARY KEY);"},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			stmt := tc.stmt
			NewStatementAnalyzer("17").AnalyzePragmas(&stmt)
			if got := stmt.Metadata.PreserveVerbatim; got != tc.want {
				t.Fatalf("PreserveVerbatim = %v, want %v", got, tc.want)
			}
		})
	}
}
