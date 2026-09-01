package utils

import (
	"strings"
)

// ExtractPolicyTargetTable extracts the target table name from a CREATE POLICY statement.
// Pattern: CREATE POLICY ... ON [schema.]table
//
// Example:
//
//	ExtractPolicyTargetTable("CREATE POLICY p1 ON public.users ...") // "public.users"
//	ExtractPolicyTargetTable("CREATE POLICY p1 ON users FOR SELECT ...") // "users"
func ExtractPolicyTargetTable(sql string) string {
	// Find " ON " keyword (case-insensitive)
	onIndex := strings.Index(strings.ToUpper(sql), " ON ")
	if onIndex == -1 {
		return ""
	}

	// Get text after " ON "
	afterOn := sql[onIndex+4:]
	parts := strings.Fields(afterOn)

	if len(parts) > 0 {
		return strings.TrimSpace(parts[0])
	}

	return ""
}

// ExtractFunctionName extracts the function name from a CREATE FUNCTION statement.
// Handles both simple names and schema-qualified names.
//
// Example:
//
//	ExtractFunctionName("CREATE FUNCTION auth.jwt() RETURNS ...") // "jwt"
//	ExtractFunctionName("CREATE OR REPLACE FUNCTION my_func() ...") // "my_func"
func ExtractFunctionName(sql string) string {
	idx := findKeywordSequence(sql, "CREATE")
	if idx == -1 {
		return ""
	}

	idx = skipWhitespace(sql, idx+len("CREATE"))
	if hasKeywordAt(sql, idx, "OR") {
		idx = skipWhitespace(sql, idx+len("OR"))
		if hasKeywordAt(sql, idx, "REPLACE") {
			idx = skipWhitespace(sql, idx+len("REPLACE"))
		}
	}

	if !hasKeywordAt(sql, idx, "FUNCTION") {
		return ""
	}

	idx = skipWhitespace(sql, idx+len("FUNCTION"))
	name, _, ok := readQualifiedIdentifier(sql, idx)
	if !ok || name == "" {
		return ""
	}

	if strings.Contains(name, ".") {
		parts := strings.Split(name, ".")
		name = parts[len(parts)-1]
	}

	return strings.Trim(name, `"`)
}

// ExtractTableName extracts the table name from a CREATE TABLE or ALTER TABLE statement.
// Handles schema-qualified names and IF NOT EXISTS clauses.
//
// Example:
//
//	ExtractTableName("CREATE TABLE public.users (...)") // "users"
//	ExtractTableName("CREATE TABLE IF NOT EXISTS accounts (...)") // "accounts"
//	ExtractTableName("ALTER TABLE users ADD COLUMN ...") // "users"
func ExtractTableName(sql string) string {
	sqlUpper := strings.ToUpper(sql)

	// Check for CREATE TABLE
	if strings.Contains(sqlUpper, "CREATE TABLE") {
		idx := findKeywordSequence(sql, "CREATE", "TABLE")
		if idx >= 0 {
			namePos := skipWhitespace(sql, idx+len("CREATE TABLE"))
			if hasKeywordAt(sql, namePos, "IF") {
				namePos = skipWhitespace(sql, namePos+len("IF"))
				if hasKeywordAt(sql, namePos, "NOT") {
					namePos = skipWhitespace(sql, namePos+len("NOT"))
					if hasKeywordAt(sql, namePos, "EXISTS") {
						namePos = skipWhitespace(sql, namePos+len("EXISTS"))
					}
				}
			}

			if qualified, _, ok := readQualifiedIdentifier(sql, namePos); ok {
				if strings.Contains(qualified, ".") {
					parts := strings.Split(qualified, ".")
					return strings.Trim(parts[len(parts)-1], `"`)
				}
				return strings.Trim(qualified, `"`)
			}
		}
	}

	// Check for ALTER TABLE
	if strings.Contains(sqlUpper, "ALTER TABLE") {
		idx := findKeywordSequence(sql, "ALTER", "TABLE")
		if idx >= 0 {
			namePos := skipWhitespace(sql, idx+len("ALTER TABLE"))
			if hasKeywordAt(sql, namePos, "IF") {
				namePos = skipWhitespace(sql, namePos+len("IF"))
				if hasKeywordAt(sql, namePos, "EXISTS") {
					namePos = skipWhitespace(sql, namePos+len("EXISTS"))
				}
			}

			if qualified, _, ok := readQualifiedIdentifier(sql, namePos); ok {
				if strings.Contains(qualified, ".") {
					parts := strings.Split(qualified, ".")
					return strings.Trim(parts[len(parts)-1], `"`)
				}
				return strings.Trim(qualified, `"`)
			}
		}
	}

	return ""
}

// ExtractSchemaName extracts the schema name from a CREATE SCHEMA statement.
//
// Example:
//
//	ExtractSchemaName("CREATE SCHEMA IF NOT EXISTS auth") // "auth"
//	ExtractSchemaName("CREATE SCHEMA public AUTHORIZATION ...") // "public"
func ExtractSchemaName(sql string) string {
	idx := findKeywordSequence(sql, "CREATE", "SCHEMA")
	if idx == -1 {
		return ""
	}

	namePos := skipWhitespace(sql, idx+len("CREATE SCHEMA"))
	if hasKeywordAt(sql, namePos, "IF") {
		namePos = skipWhitespace(sql, namePos+len("IF"))
		if hasKeywordAt(sql, namePos, "NOT") {
			namePos = skipWhitespace(sql, namePos+len("NOT"))
			if hasKeywordAt(sql, namePos, "EXISTS") {
				namePos = skipWhitespace(sql, namePos+len("EXISTS"))
			}
		}
	}

	name, _, ok := readIdentifier(sql, namePos)
	if ok {
		return strings.Trim(name, `"`)
	}

	return ""
}

// ExtractIndexName extracts the index name from a CREATE INDEX statement.
//
// Example:
//
//	ExtractIndexName("CREATE INDEX idx_users_email ON users(email)") // "idx_users_email"
//	ExtractIndexName("CREATE UNIQUE INDEX CONCURRENTLY idx_pk ON t(id)") // "idx_pk"
func ExtractIndexName(sql string) string {
	idx := findKeywordSequence(sql, "CREATE")
	if idx == -1 {
		return ""
	}

	pos := skipWhitespace(sql, idx+len("CREATE"))
	if hasKeywordAt(sql, pos, "UNIQUE") {
		pos = skipWhitespace(sql, pos+len("UNIQUE"))
	}

	if !hasKeywordAt(sql, pos, "INDEX") {
		return ""
	}

	pos = skipWhitespace(sql, pos+len("INDEX"))
	if hasKeywordAt(sql, pos, "CONCURRENTLY") {
		pos = skipWhitespace(sql, pos+len("CONCURRENTLY"))
	}

	name, _, ok := readIdentifier(sql, pos)
	if ok {
		return strings.Trim(name, `"`)
	}

	return ""
}

func findKeywordSequence(sql string, words ...string) int {
	if len(words) == 0 {
		return -1
	}

	for i := 0; i < len(sql); i++ {
		if !hasKeywordAt(sql, i, words[0]) {
			continue
		}

		idx := i + len(words[0])
		matched := true
		for w := 1; w < len(words); w++ {
			idx = skipWhitespace(sql, idx)
			if !hasKeywordAt(sql, idx, words[w]) {
				matched = false
				break
			}
			idx += len(words[w])
		}

		if matched {
			return i
		}
	}

	return -1
}

func hasKeywordAt(sql string, pos int, keyword string) bool {
	if pos < 0 || pos+len(keyword) > len(sql) {
		return false
	}

	if !strings.EqualFold(sql[pos:pos+len(keyword)], keyword) {
		return false
	}

	if pos > 0 && isIdentifierByte(sql[pos-1]) {
		return false
	}

	end := pos + len(keyword)
	if end < len(sql) && isIdentifierByte(sql[end]) {
		return false
	}

	return true
}

func skipWhitespace(sql string, pos int) int {
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

func readQualifiedIdentifier(sql string, pos int) (string, int, bool) {
	first, next, ok := readIdentifier(sql, pos)
	if !ok {
		return "", 0, false
	}

	parts := []string{first}
	for next < len(sql) && sql[next] == '.' {
		next++
		part, newNext, ok := readIdentifier(sql, next)
		if !ok {
			break
		}
		parts = append(parts, part)
		next = newNext
	}

	return strings.Join(parts, "."), next, true
}

func readIdentifier(sql string, pos int) (string, int, bool) {
	if pos < 0 || pos >= len(sql) {
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

	if !isIdentifierByte(sql[pos]) {
		return "", 0, false
	}

	i := pos
	for i < len(sql) && isIdentifierByte(sql[i]) {
		i++
	}

	return sql[pos:i], i, true
}

func isIdentifierByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

// IsDMLStatement checks if SQL is a Data Manipulation Language statement.
// Returns true for INSERT, UPDATE, DELETE, SELECT.
func IsDMLStatement(sql string) bool {
	sqlUpper := strings.ToUpper(strings.TrimSpace(sql))

	return strings.HasPrefix(sqlUpper, "INSERT ") ||
		strings.HasPrefix(sqlUpper, "UPDATE ") ||
		strings.HasPrefix(sqlUpper, "DELETE ") ||
		strings.HasPrefix(sqlUpper, "SELECT ")
}

// IsDDLStatement checks if SQL is a Data Definition Language statement.
// Returns true for CREATE, ALTER, DROP, TRUNCATE.
func IsDDLStatement(sql string) bool {
	sqlUpper := strings.ToUpper(strings.TrimSpace(sql))

	return strings.HasPrefix(sqlUpper, "CREATE ") ||
		strings.HasPrefix(sqlUpper, "ALTER ") ||
		strings.HasPrefix(sqlUpper, "DROP ") ||
		strings.HasPrefix(sqlUpper, "TRUNCATE ")
}
