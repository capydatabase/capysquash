package schema

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/parser"
)

// NormalizedModel is a deterministic representation of schema state.
type NormalizedModel struct {
	Lines       []string `json:"lines"`
	Fingerprint string   `json:"fingerprint"`
}

// Diff captures semantic differences between two normalized models.
type Diff struct {
	HasDifferences bool     `json:"has_differences"`
	Differences    []string `json:"differences"`
}

// BuildFromSQL creates a normalized deterministic model from SQL text.
func BuildFromSQL(filename, sqlText string) NormalizedModel {
	migration, err := parser.ParseMigration(sqlText, filename)
	if err != nil || migration == nil {
		return BuildFromLines(splitNonEmptyLines(NormalizeSQLWhitespace(sqlText)))
	}

	lines := make([]string, 0, len(migration.Statements))
	for _, stmt := range migration.Statements {
		normalized := NormalizeSQLWhitespace(stmt.SQL)
		if normalized != "" {
			lines = append(lines, normalized)
		}
	}

	if len(lines) == 0 {
		lines = splitNonEmptyLines(NormalizeSQLWhitespace(sqlText))
	}

	return BuildFromLines(lines)
}

// BuildFromLines creates a normalized model from already canonical signature lines.
func BuildFromLines(lines []string) NormalizedModel {
	normalized := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		normalized = append(normalized, trimmed)
	}

	sort.Strings(normalized)

	joined := strings.Join(normalized, "\n")
	sum := sha256.Sum256([]byte(joined))

	return NormalizedModel{
		Lines:       normalized,
		Fingerprint: hex.EncodeToString(sum[:]),
	}
}

// CompareModelsWithLabels compares two normalized models and annotates diff labels.
func CompareModelsWithLabels(left, right NormalizedModel, leftLabel, rightLabel string, maxDiffEntries int) Diff {
	leftSet := make(map[string]struct{}, len(left.Lines))
	rightSet := make(map[string]struct{}, len(right.Lines))

	for _, line := range left.Lines {
		leftSet[line] = struct{}{}
	}
	for _, line := range right.Lines {
		rightSet[line] = struct{}{}
	}

	differences := make([]string, 0)
	for line := range leftSet {
		if _, ok := rightSet[line]; !ok {
			differences = append(differences, fmt.Sprintf("only in %s: %s", leftLabel, line))
		}
	}
	for line := range rightSet {
		if _, ok := leftSet[line]; !ok {
			differences = append(differences, fmt.Sprintf("only in %s: %s", rightLabel, line))
		}
	}

	sort.Strings(differences)
	if maxDiffEntries > 0 && len(differences) > maxDiffEntries {
		truncated := differences[:maxDiffEntries]
		truncated = append(truncated, fmt.Sprintf("... %d additional differences omitted", len(differences)-maxDiffEntries))
		differences = truncated
	}

	return Diff{
		HasDifferences: len(differences) > 0,
		Differences:    differences,
	}
}

// NormalizeSQLWhitespace canonicalizes SQL text for stable comparisons.
func NormalizeSQLWhitespace(sqlText string) string {
	normalized := strings.TrimSpace(strings.ToLower(sqlText))
	if normalized == "" {
		return ""
	}

	normalized = strings.NewReplacer(
		"(", " ( ",
		")", " ) ",
		",", " , ",
		";", " ; ",
	).Replace(normalized)

	normalized = strings.Join(strings.Fields(normalized), " ")
	normalized = strings.TrimSuffix(normalized, " ;")
	return normalized
}

func splitNonEmptyLines(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	rawLines := strings.Split(value, "\n")
	lines := make([]string, 0, len(rawLines))
	for _, line := range rawLines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}
