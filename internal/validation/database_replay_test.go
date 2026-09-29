package validation

import (
	"testing"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

func TestNonTransactionalReason(t *testing.T) {
	tests := []struct {
		sql     string
		skipped bool
	}{
		{"CREATE INDEX CONCURRENTLY i ON t (a)", true},
		{"CREATE INDEX i ON t (a)", false},
		{"DROP INDEX CONCURRENTLY i", true},
		{"DROP INDEX i", false},
		{"REINDEX (CONCURRENTLY) INDEX i", true},
		{"REINDEX INDEX i", false},
		{"VACUUM t", true},
		{"ANALYZE t", false},
		{"CREATE DATABASE d", true},
		{"ALTER SYSTEM SET work_mem = '64MB'", true},
		{"BEGIN", true},
		{"COMMIT", true},
		{"CREATE TABLE t (id int)", false},
		{"ALTER TYPE mood ADD VALUE 'meh'", false},
	}
	for _, tc := range tests {
		t.Run(tc.sql, func(t *testing.T) {
			tree, err := pg_query.Parse(tc.sql)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := nonTransactionalReason(tree.GetStmts()[0].GetStmt())
			if (got != "") != tc.skipped {
				t.Fatalf("nonTransactionalReason(%q) = %q, skipped want %v", tc.sql, got, tc.skipped)
			}
		})
	}
}

// The level names do not sort by strength ("COMPREHENSIVE" < "THOROUGH"), so
// comparing the strings made the strongest level skip database validation.
func TestValidationLevelAtLeast(t *testing.T) {
	order := []ValidationLevel{ValidationLevelBasic, ValidationLevelStandard, ValidationLevelThorough, ValidationLevelComprehensive}
	for i, l := range order {
		for j, min := range order {
			if got, want := l.AtLeast(min), i >= j; got != want {
				t.Errorf("%s.AtLeast(%s) = %v, want %v", l, min, got, want)
			}
		}
	}
}
