// Package validation provides schema normalization for reliable schema comparison.
// It implements the pg_dump normalization pipeline as specified in the production
// readiness audit.
package validation

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// SchemaNormalizer handles normalization of pg_dump output for reliable comparison
type SchemaNormalizer struct {
	StripComments       bool
	SortBlocks          bool
	CanonicalizeFunc    bool
	RemoveOIDs          bool
	NormalizeWhitespace bool
	RemoveOwnership     bool
	RemovePrivileges    bool
}

// DefaultSchemaNormalizer returns a normalizer with default settings
func DefaultSchemaNormalizer() *SchemaNormalizer {
	return &SchemaNormalizer{
		StripComments:       true,
		SortBlocks:          true,
		CanonicalizeFunc:    true,
		RemoveOIDs:          true,
		NormalizeWhitespace: true,
		RemoveOwnership:     true,
		RemovePrivileges:    true,
	}
}

// NormalizedSchema represents a normalized database schema
type NormalizedSchema struct {
	Raw        string
	Normalized string
	Tables     []string
	Indexes    []string
	Functions  []string
	Types      []string
	Triggers   []string
	Views      []string
}

// DumpAndNormalizeSchema dumps a database schema using pg_dump and normalizes it
func (sv *SchemaValidator) DumpAndNormalizeSchema(ctx context.Context, db *sql.DB, dbName string) (*NormalizedSchema, error) {
	// Get database connection string from the sql.DB
	// We need to execute pg_dump, which requires connection details
	// For now, we'll use docker exec if we're in a container context

	// We need to determine if this is a containerized database or a direct connection
	// For the validation workflow, we'll assume it's a containerized setup

	return nil, errors.NewError(
		errors.ErrorCodeValidationFailed,
		"DumpAndNormalizeSchema requires container context",
		errors.SeverityError,
		errors.CategoryValidation,
	).WithSuggestion("Use DumpAndNormalizeContainerSchema for containerized databases")
}

// DumpAndNormalizeContainerSchema dumps and normalizes schema from a Docker container
// Uses the default "postgres" database
func (sv *SchemaValidator) DumpAndNormalizeContainerSchema(ctx context.Context, containerID string) (*NormalizedSchema, error) {
	return sv.DumpAndNormalizeContainerDatabase(ctx, containerID, "postgres")
}

// DumpAndNormalizeContainerDatabase dumps and normalizes schema from a specific database in a Docker container
func (sv *SchemaValidator) DumpAndNormalizeContainerDatabase(ctx context.Context, containerID, database string) (*NormalizedSchema, error) {
	normalizer := DefaultSchemaNormalizer()

	// Execute pg_dump with optimal flags for comparison
	cmd := exec.CommandContext(ctx, "docker", "exec", containerID,
		"pg_dump",
		"-U", "postgres",
		"-d", database,
		"--schema-only",
		"--no-owner",
		"--no-privileges",
		"--quote-all-identifiers",
		"--no-comments", // We'll handle comment stripping in normalization
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to dump schema from container",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).
			WithAdditional("container_id", containerID).
			WithAdditional("output", string(output)).
			WithSuggestion("Ensure pg_dump is available in the container and database is accessible")
	}

	raw := string(output)
	normalized, err := normalizer.Normalize(raw)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to normalize schema",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	schema := &NormalizedSchema{
		Raw:        raw,
		Normalized: normalized,
	}

	// Extract object lists for detailed comparison
	schema.extractObjects()

	return schema, nil
}

// Normalize applies all normalization rules to a schema dump
func (sn *SchemaNormalizer) Normalize(schema string) (string, error) {
	result := schema

	// 1. Strip SQL comments
	if sn.StripComments {
		result = sn.stripComments(result)
	}

	// 2. Remove ownership and privileges
	if sn.RemoveOwnership {
		result = sn.removeOwnership(result)
	}

	if sn.RemovePrivileges {
		result = sn.removePrivileges(result)
	}

	// 3. Remove OIDs
	if sn.RemoveOIDs {
		result = sn.removeOIDs(result)
	}

	// 4. Normalize whitespace
	if sn.NormalizeWhitespace {
		result = sn.normalizeWhitespace(result)
	}

	// 5. Canonicalize function definitions
	if sn.CanonicalizeFunc {
		result = sn.canonicalizeFunctions(result)
	}

	// 6. Sort blocks for consistent ordering
	if sn.SortBlocks {
		result = sn.sortBlocks(result)
	}

	return result, nil
}

// stripComments removes SQL comments from the schema
func (sn *SchemaNormalizer) stripComments(schema string) string {
	lines := strings.Split(schema, "\n")
	var result []string

	inMultilineComment := false

	for _, line := range lines {
		// Handle multiline comments
		if strings.Contains(line, "/*") {
			inMultilineComment = true
		}

		if inMultilineComment {
			if strings.Contains(line, "*/") {
				inMultilineComment = false
			}
			continue
		}

		// Remove single-line comments
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}

		// Skip empty lines
		if strings.TrimSpace(line) == "" {
			continue
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// removeOwnership removes OWNER TO clauses
func (sn *SchemaNormalizer) removeOwnership(schema string) string {
	// Remove ALTER ... OWNER TO statements
	ownerRe := regexp.MustCompile(`(?m)^ALTER\s+\w+.*OWNER\s+TO\s+\w+;?\s*$`)
	schema = ownerRe.ReplaceAllString(schema, "")

	// Remove inline OWNER TO clauses
	ownerInlineRe := regexp.MustCompile(`\s+OWNER\s+TO\s+\w+`)
	schema = ownerInlineRe.ReplaceAllString(schema, "")

	return schema
}

// removePrivileges removes GRANT and REVOKE statements
func (sn *SchemaNormalizer) removePrivileges(schema string) string {
	// Remove GRANT statements
	grantRe := regexp.MustCompile(`(?m)^GRANT\s+.*$`)
	schema = grantRe.ReplaceAllString(schema, "")

	// Remove REVOKE statements
	revokeRe := regexp.MustCompile(`(?m)^REVOKE\s+.*$`)
	schema = revokeRe.ReplaceAllString(schema, "")

	return schema
}

// removeOIDs removes OID references
func (sn *SchemaNormalizer) removeOIDs(schema string) string {
	// Remove WITH OIDS
	oidRe := regexp.MustCompile(`\s+WITH\s+OIDS`)
	schema = oidRe.ReplaceAllString(schema, "")

	// Remove OID = xxxx
	oidValueRe := regexp.MustCompile(`\s+OID\s*=\s*\d+`)
	schema = oidValueRe.ReplaceAllString(schema, "")

	return schema
}

// normalizeWhitespace canonicalizes whitespace for consistent comparison
func (sn *SchemaNormalizer) normalizeWhitespace(schema string) string {
	lines := strings.Split(schema, "\n")
	var result []string

	for _, line := range lines {
		// Trim trailing whitespace
		line = strings.TrimRight(line, " \t")

		// Normalize multiple spaces to single space (but preserve indentation)
		parts := strings.Fields(line)
		if len(parts) > 0 {
			// Get leading whitespace
			leadingSpace := ""
			for _, ch := range line {
				if ch == ' ' || ch == '\t' {
					leadingSpace += string(ch)
				} else {
					break
				}
			}

			// Reconstruct with normalized spaces
			line = leadingSpace + strings.Join(parts, " ")
		}

		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// canonicalizeFunctions normalizes function definitions
func (sn *SchemaNormalizer) canonicalizeFunctions(schema string) string {
	// Normalize function body formatting
	// Replace $$ delimiters variations
	dollarRe := regexp.MustCompile(`\$[a-zA-Z0-9_]*\$`)
	schema = dollarRe.ReplaceAllString(schema, "$$")

	// Normalize LANGUAGE clause (LANGUAGE plpgsql vs LANGUAGE 'plpgsql')
	langRe := regexp.MustCompile(`LANGUAGE\s+'([^']+)'`)
	schema = langRe.ReplaceAllString(schema, "LANGUAGE $1")

	return schema
}

// sortBlocks sorts top-level SQL blocks for consistent ordering
func (sn *SchemaNormalizer) sortBlocks(schema string) string {
	blocks := sn.splitIntoBlocks(schema)

	// Group blocks by type
	typeOrder := map[string]int{
		"SET":                 0,
		"CREATE EXTENSION":    1,
		"CREATE SCHEMA":       2,
		"CREATE TYPE":         3,
		"CREATE DOMAIN":       4,
		"CREATE SEQUENCE":     5,
		"CREATE TABLE":        6,
		"ALTER TABLE":         7,
		"CREATE INDEX":        8,
		"CREATE UNIQUE INDEX": 8,
		"CREATE FUNCTION":     9,
		"CREATE PROCEDURE":    9,
		"CREATE TRIGGER":      10,
		"CREATE VIEW":         11,
		"CREATE MATERIALIZED": 12,
		"CREATE POLICY":       13,
		"CREATE RULE":         14,
		"COMMENT ON":          15,
	}

	// Sort blocks by type, then by name
	sort.Slice(blocks, func(i, j int) bool {
		typeI := sn.getBlockType(blocks[i])
		typeJ := sn.getBlockType(blocks[j])

		orderI := typeOrder[typeI]
		orderJ := typeOrder[typeJ]

		if orderI != orderJ {
			return orderI < orderJ
		}

		// Same type, sort by name
		nameI := sn.extractObjectName(blocks[i])
		nameJ := sn.extractObjectName(blocks[j])
		return nameI < nameJ
	})

	return strings.Join(blocks, "\n\n")
}

// splitIntoBlocks splits schema into logical blocks (statements)
func (sn *SchemaNormalizer) splitIntoBlocks(schema string) []string {
	var blocks []string
	var currentBlock strings.Builder

	scanner := bufio.NewScanner(strings.NewReader(schema))
	inBlock := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		// Start of a new block
		if !inBlock && trimmed != "" {
			inBlock = true
		}

		if inBlock {
			currentBlock.WriteString(line)
			currentBlock.WriteString("\n")

			// End of block (semicolon at end of line)
			if strings.HasSuffix(trimmed, ";") {
				blocks = append(blocks, strings.TrimSpace(currentBlock.String()))
				currentBlock.Reset()
				inBlock = false
			}
		}
	}

	// Add any remaining block
	if currentBlock.Len() > 0 {
		blocks = append(blocks, strings.TrimSpace(currentBlock.String()))
	}

	return blocks
}

// getBlockType extracts the statement type from a block
func (sn *SchemaNormalizer) getBlockType(block string) string {
	// Get first few words
	words := strings.Fields(block)
	if len(words) == 0 {
		return "UNKNOWN"
	}

	// Handle multi-word types
	if len(words) >= 2 {
		twoWord := strings.ToUpper(words[0] + " " + words[1])
		if twoWord == "CREATE EXTENSION" || twoWord == "CREATE SCHEMA" ||
			twoWord == "CREATE TYPE" || twoWord == "CREATE DOMAIN" ||
			twoWord == "CREATE SEQUENCE" || twoWord == "CREATE TABLE" ||
			twoWord == "ALTER TABLE" || twoWord == "CREATE INDEX" ||
			twoWord == "CREATE FUNCTION" || twoWord == "CREATE PROCEDURE" ||
			twoWord == "CREATE TRIGGER" || twoWord == "CREATE VIEW" ||
			twoWord == "CREATE POLICY" || twoWord == "CREATE RULE" ||
			twoWord == "COMMENT ON" {
			return twoWord
		}

		// Handle CREATE UNIQUE INDEX
		if len(words) >= 3 && strings.ToUpper(words[0]+" "+words[1]+" "+words[2]) == "CREATE UNIQUE INDEX" {
			return "CREATE UNIQUE INDEX"
		}

		// Handle CREATE MATERIALIZED VIEW
		if len(words) >= 3 && strings.ToUpper(words[0]+" "+words[1]+" "+words[2]) == "CREATE MATERIALIZED VIEW" {
			return "CREATE MATERIALIZED"
		}
	}

	return strings.ToUpper(words[0])
}

// extractObjectName extracts the object name from a SQL block
func (sn *SchemaNormalizer) extractObjectName(block string) string {
	// Simple regex-based extraction
	patterns := []string{
		`CREATE\s+(?:OR\s+REPLACE\s+)?(?:UNIQUE\s+)?(?:INDEX|TABLE|VIEW|FUNCTION|PROCEDURE|TRIGGER|TYPE|SCHEMA|EXTENSION|SEQUENCE|DOMAIN|POLICY|RULE)\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:CONCURRENTLY\s+)?["']?([a-zA-Z0-9_\.]+)["']?`,
		`ALTER\s+TABLE\s+(?:IF\s+EXISTS\s+)?["']?([a-zA-Z0-9_\.]+)["']?`,
		`COMMENT\s+ON\s+\w+\s+["']?([a-zA-Z0-9_\.]+)["']?`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(block)
		if len(matches) > 1 {
			return matches[1]
		}
	}

	return ""
}

// extractObjects extracts categorized object lists from the normalized schema
func (ns *NormalizedSchema) extractObjects() {
	cleanNormalized := removeCommentLines(ns.Normalized)
	blocks := strings.Split(cleanNormalized, ";")

	for _, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}

		if block == "" {
			continue
		}

		block = stripLeadingComments(block)
		if block == "" {
			continue
		}

		upper := strings.ToUpper(block)

		if strings.HasPrefix(upper, "CREATE TABLE") {
			ns.Tables = append(ns.Tables, block)
		} else if strings.HasPrefix(upper, "CREATE INDEX") || strings.HasPrefix(upper, "CREATE UNIQUE INDEX") {
			ns.Indexes = append(ns.Indexes, block)
		} else if strings.HasPrefix(upper, "CREATE FUNCTION") || strings.HasPrefix(upper, "CREATE OR REPLACE FUNCTION") {
			ns.Functions = append(ns.Functions, block)
		} else if strings.HasPrefix(upper, "CREATE TYPE") {
			ns.Types = append(ns.Types, block)
		} else if strings.HasPrefix(upper, "CREATE TRIGGER") {
			ns.Triggers = append(ns.Triggers, block)
		} else if strings.HasPrefix(upper, "CREATE VIEW") || strings.HasPrefix(upper, "CREATE MATERIALIZED VIEW") {
			ns.Views = append(ns.Views, block)
		}
	}
}

// stripLeadingComments removes leading SQL comments and blank lines from a block.
func stripLeadingComments(block string) string {
	lines := strings.Split(block, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		return strings.Join(lines[i:], "\n")
	}
	return ""
}

// removeCommentLines strips full-line comments to avoid semicolon splitting artifacts.
func removeCommentLines(sql string) string {
	lines := strings.Split(sql, "\n")
	clean := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") {
			continue
		}
		clean = append(clean, line)
	}
	return strings.Join(clean, "\n")
}

// CompareNormalizedSchemas compares two normalized schemas and returns detailed differences
func CompareNormalizedSchemas(schema1, schema2 *NormalizedSchema) *SchemaDiff {
	diff := &SchemaDiff{
		HasDifferences: false,
		Differences:    make([]string, 0),
	}

	// Simple string comparison first
	if schema1.Normalized == schema2.Normalized {
		return diff
	}

	diff.HasDifferences = true

	// Detailed comparison by object type
	diff.compareObjects("Tables", schema1.Tables, schema2.Tables)
	diff.compareObjects("Indexes", schema1.Indexes, schema2.Indexes)
	diff.compareObjects("Functions", schema1.Functions, schema2.Functions)
	diff.compareObjects("Types", schema1.Types, schema2.Types)
	diff.compareObjects("Triggers", schema1.Triggers, schema2.Triggers)
	diff.compareObjects("Views", schema1.Views, schema2.Views)

	//  If normalized schemas differ but no object-level differences were found,
	// the differences must be in uncategorized statements (ALTER, GRANT, policies, etc.)
	// Report this explicitly rather than showing empty diff output
	if len(diff.Differences) == 0 {
		// Try to identify what type of statements might be different
		diff.Differences = append(diff.Differences, "Schemas differ in uncategorized statements (ALTER, GRANT, POLICY, SEQUENCE, or other DDL)")
		diff.Differences = append(diff.Differences, "Note: Only CREATE statements are categorized for detailed comparison")

		// Provide a hint about where differences might be
		diff.addUncategorizedDiffHints(schema1.Normalized, schema2.Normalized)
	}

	return diff
}

// SchemaDiff represents differences between two schemas
type SchemaDiff struct {
	HasDifferences bool
	Differences    []string
}

// compareObjects compares two lists of objects
// Compare by object NAME first, not exact string match
// This prevents reporting objects as both "only in X" AND "differs"
func (sd *SchemaDiff) compareObjects(objectType string, list1, list2 []string) {
	// Build maps of name -> definition for both schemas
	nameMap1 := make(map[string]string) // name -> full definition
	nameMap2 := make(map[string]string)

	for _, obj := range list1 {
		name := extractShortName(obj)
		nameMap1[name] = obj
	}

	for _, obj := range list2 {
		name := extractShortName(obj)
		nameMap2[name] = obj
	}

	// Check for objects only in schema1 (by name)
	for name, obj1 := range nameMap1 {
		obj2, existsInSchema2 := nameMap2[name]

		if !existsInSchema2 {
			// Object doesn't exist in schema2 at all
			sd.Differences = append(sd.Differences, fmt.Sprintf("%s only in original: %s", objectType, name))
		} else if obj1 != obj2 {
			// Object exists in both but definitions differ
			// Normalize for comparison (ignore whitespace/formatting differences)
			normalized1 := normalizeForComparison(obj1)
			normalized2 := normalizeForComparison(obj2)

			if normalized1 != normalized2 {
				sd.Differences = append(sd.Differences, fmt.Sprintf("%s differs: %s", objectType, name))
			}
			// If normalized versions match, they're functionally identical - no diff reported
		}
		// If obj1 == obj2 exactly, they're identical - no action needed
	}

	// Check for objects only in schema2 (by name)
	for name := range nameMap2 {
		if _, existsInSchema1 := nameMap1[name]; !existsInSchema1 {
			// Object doesn't exist in schema1 at all
			sd.Differences = append(sd.Differences, fmt.Sprintf("%s only in squashed: %s", objectType, name))
		}
		// If it exists in schema1, we already checked it in the previous loop
	}
}

// normalizeForComparison normalizes SQL for comparison purposes
// Removes formatting differences that don't affect functionality
func normalizeForComparison(sql string) string {
	// Convert to lowercase for case-insensitive comparison
	normalized := strings.ToLower(sql)

	// Remove excessive whitespace
	normalized = regexp.MustCompile(`\s+`).ReplaceAllString(normalized, " ")

	// Remove SQL comments (-- style and /* */ style)
	normalized = regexp.MustCompile(`--[^\n]*`).ReplaceAllString(normalized, "")
	normalized = regexp.MustCompile(`/\*.*?\*/`).ReplaceAllString(normalized, "")

	// Trim surrounding whitespace
	normalized = strings.TrimSpace(normalized)

	return normalized
}

// addUncategorizedDiffHints analyzes normalized schemas to provide hints about uncategorized differences
// Helper function to identify what types of uncategorized statements differ
func (sd *SchemaDiff) addUncategorizedDiffHints(norm1, norm2 string) {
	// Count different statement types in each schema
	statementTypes := []string{
		"ALTER TABLE",
		"ALTER SEQUENCE",
		"CREATE POLICY",
		"CREATE SEQUENCE",
		"GRANT",
		"REVOKE",
		"COMMENT ON",
		"ALTER DEFAULT PRIVILEGES",
	}

	for _, stmtType := range statementTypes {
		count1 := strings.Count(strings.ToUpper(norm1), stmtType)
		count2 := strings.Count(strings.ToUpper(norm2), stmtType)

		if count1 != count2 {
			sd.Differences = append(sd.Differences,
				fmt.Sprintf("  - %s statements: %d in original, %d in squashed", stmtType, count1, count2))
		}
	}

	// If still no hints, provide line count difference
	if len(sd.Differences) <= 2 { // Only the initial 2 messages
		lines1 := strings.Count(norm1, "\n")
		lines2 := strings.Count(norm2, "\n")
		sd.Differences = append(sd.Differences,
			fmt.Sprintf("  - Schema line count: %d in original, %d in squashed (difference: %d lines)",
				lines1, lines2, abs(lines1-lines2)))
	}
}

// abs returns the absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// extractShortName extracts just the object name from a full definition
func extractShortName(definition string) string {
	// Extract name from CREATE statements (case-insensitive). Handle dotted identifiers where each
	// segment may be individually quoted (e.g. "public"."UserProfile").
	patterns := []string{
		`(?i)CREATE\s+(?:OR\s+REPLACE\s+)?(?:UNIQUE\s+)?(?:INDEX|TABLE|VIEW|FUNCTION|PROCEDURE|TRIGGER|TYPE|SCHEMA|EXTENSION|SEQUENCE|DOMAIN|POLICY|RULE)\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:CONCURRENTLY\s+)?((?:"[^"]+"|[a-zA-Z0-9_]+)(?:\.(?:"[^"]+"|[a-zA-Z0-9_]+))*)`,
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		matches := re.FindStringSubmatch(definition)
		if len(matches) > 1 {
			return cleanIdentifier(matches[1])
		}
	}

	// Return first 50 chars if we can't extract name
	if len(definition) > 50 {
		return definition[:50] + "..."
	}
	return definition
}

// cleanIdentifier removes redundant quoting around identifier segments while preserving dotted qualifiers.
func cleanIdentifier(identifier string) string {
	parts := strings.Split(identifier, ".")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if len(part) >= 2 && part[0] == '"' && part[len(part)-1] == '"' {
			part = part[1 : len(part)-1]
		}
		parts[i] = part
	}
	return strings.Join(parts, ".")
}

// FormatDiff formats the schema diff for display
func (sd *SchemaDiff) FormatDiff() string {
	if !sd.HasDifferences {
		return "✅ No schema differences detected"
	}

	var buf bytes.Buffer
	buf.WriteString("❌ Schema differences detected:\n\n")

	for i, diff := range sd.Differences {
		buf.WriteString(fmt.Sprintf("%d. %s\n", i+1, diff))
	}

	return buf.String()
}
