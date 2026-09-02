package consolidation

import (
	"fmt"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/tracking"
	"github.com/capy-base/pgsquash-engine/internal/types"

	"github.com/capy-base/pgsquash-engine/internal/errors"
)

// DOBlockEnumTypeRule consolidates DO blocks that create ENUM types
type DOBlockEnumTypeRule struct{}

// CanApply checks if the rule can be applied to the given lifecycle
func (r *DOBlockEnumTypeRule) CanApply(lifecycle *tracking.ObjectLifecycle) bool {
	// Apply to DO blocks or ENUM types created inside DO blocks
	if lifecycle.Type != types.TypeDoBlock && lifecycle.Type != types.TypeEnum {
		return false
	}

	// Check if we have at least one CREATE TYPE operation
	hasCreateType := false
	for _, event := range lifecycle.History {
		sql := strings.ToUpper(event.Statement.SQL)
		if strings.Contains(sql, "DO") && strings.Contains(sql, "CREATE TYPE") && strings.Contains(sql, "AS ENUM") {
			hasCreateType = true
			break
		}
	}

	return hasCreateType
}

// Apply applies the consolidation rule to the given lifecycle
func (r *DOBlockEnumTypeRule) Apply(lifecycle *tracking.ObjectLifecycle, engine ConsolidationEngine) (*tracking.ConsolidationResult, error) {
	if !r.CanApply(lifecycle) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation, "rule cannot be applied to lifecycle", map[string]any{"rule": "DOBlockEnumTypeRule"})
	}

	// Extract CREATE TYPE statement from DO block
	var doBlockStmts []types.Statement
	var extractedType string
	var extractedSQL string

	for _, event := range lifecycle.History {
		sql := event.Statement.SQL
		sqlUpper := strings.ToUpper(sql)

		if strings.Contains(sqlUpper, "DO") && strings.Contains(sqlUpper, "CREATE TYPE") {
			doBlockStmts = append(doBlockStmts, event.Statement)

			// Extract the CREATE TYPE statement from within the DO block
			if extractedSQL == "" {
				if typeName, enumValues, ok := extractCreateTypeEnum(sql); ok {
					extractedType = typeName

					// Clean up enum values (remove quotes, extra spaces)
					enumValues = strings.TrimSpace(enumValues)

					// Build clean CREATE TYPE statement
					extractedSQL = fmt.Sprintf("DO $$ BEGIN\n    CREATE TYPE %s AS ENUM (%s);\nEXCEPTION\n    WHEN duplicate_object THEN NULL;\nEND $$;", extractedType, enumValues)
				}
			}
		}
	}

	// If we couldn't extract a valid CREATE TYPE, preserve the DO block as-is
	if extractedSQL == "" && len(doBlockStmts) > 0 {
		extractedSQL = doBlockStmts[0].SQL
	}

	result := &tracking.ConsolidationResult{
		OriginalStatements: doBlockStmts,
		ConsolidatedSQL:    extractedSQL,
		Optimizations: []string{
			fmt.Sprintf("Consolidated %d DO block(s) with CREATE TYPE for %s", len(doBlockStmts), extractedType),
		},
		RiskLevel: tracking.RiskLevelLow,
		EstimatedSavings: tracking.SquashSavings{
			StatementsReduced: len(doBlockStmts) - 1,
			FilesAffected:     len(doBlockStmts),
			LinesReduced:      (len(doBlockStmts) - 1) * 6, // Average DO block is ~6 lines
		},
	}

	return result, nil
}

// Risk returns the risk level for this rule
func (r *DOBlockEnumTypeRule) Risk() tracking.RiskLevel {
	return tracking.RiskLevelLow
}

func extractCreateTypeEnum(sql string) (string, string, bool) {
	upper := strings.ToUpper(sql)
	createIdx := strings.Index(upper, "CREATE TYPE")
	if createIdx == -1 {
		return "", "", false
	}

	pos := createIdx + len("CREATE TYPE")
	pos = skipEnumWhitespace(sql, pos)

	typeName, next, ok := readEnumIdentifier(sql, pos)
	if !ok {
		return "", "", false
	}

	pos = skipEnumWhitespace(sql, next)
	if !hasEnumKeywordAt(sql, pos, "AS") {
		return "", "", false
	}
	pos = skipEnumWhitespace(sql, pos+len("AS"))

	if !hasEnumKeywordAt(sql, pos, "ENUM") {
		return "", "", false
	}
	pos = skipEnumWhitespace(sql, pos+len("ENUM"))

	if pos >= len(sql) || sql[pos] != '(' {
		return "", "", false
	}

	end, ok := findEnumClosingParen(sql, pos)
	if !ok || end <= pos+1 {
		return "", "", false
	}

	return strings.Trim(typeName, `"`), sql[pos+1 : end], true
}

func findEnumClosingParen(sql string, open int) (int, bool) {
	depth := 0
	inSingleQuote := false

	for i := open; i < len(sql); i++ {
		ch := sql[i]

		if inSingleQuote {
			if ch == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					i++
					continue
				}
				inSingleQuote = false
			}
			continue
		}

		if ch == '\'' {
			inSingleQuote = true
			continue
		}

		switch ch {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}

	return 0, false
}

func readEnumIdentifier(sql string, pos int) (string, int, bool) {
	if pos >= len(sql) {
		return "", 0, false
	}

	if sql[pos] == '"' {
		for i := pos + 1; i < len(sql); i++ {
			if sql[i] == '"' {
				return sql[pos : i+1], i + 1, true
			}
		}
		return "", 0, false
	}

	if !isEnumIdentifierByte(sql[pos]) {
		return "", 0, false
	}

	i := pos
	for i < len(sql) && isEnumIdentifierByte(sql[i]) {
		i++
	}

	return sql[pos:i], i, true
}

func hasEnumKeywordAt(sql string, pos int, keyword string) bool {
	if pos < 0 || pos+len(keyword) > len(sql) {
		return false
	}

	if !strings.EqualFold(sql[pos:pos+len(keyword)], keyword) {
		return false
	}

	if pos > 0 && isEnumIdentifierByte(sql[pos-1]) {
		return false
	}

	end := pos + len(keyword)
	if end < len(sql) && isEnumIdentifierByte(sql[end]) {
		return false
	}

	return true
}

func skipEnumWhitespace(sql string, pos int) int {
	for pos < len(sql) {
		switch sql[pos] {
		case ' ', '\t', '\n', '\r', '\f', '\v':
			pos++
		default:
			return pos
		}
	}
	return pos
}

func isEnumIdentifierByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}
