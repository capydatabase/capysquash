package utils

import "strings"

// PublicationAddTableTarget captures the publication and relation targeted by
// an ALTER PUBLICATION ... ADD TABLE statement.
type PublicationAddTableTarget struct {
	Publication string
	Schema      string
	Table       string
}

// ParsePublicationAddTable parses ALTER PUBLICATION ... ADD TABLE statements.
//
// Supported forms include quoted identifiers, schema-qualified relations, and
// multiline SQL.
func ParsePublicationAddTable(sql string) (PublicationAddTableTarget, bool) {
	tokens := tokenizePublicationSQL(sql)
	if len(tokens) < 5 {
		return PublicationAddTableTarget{}, false
	}

	if !strings.EqualFold(tokens[0], "ALTER") || !strings.EqualFold(tokens[1], "PUBLICATION") {
		return PublicationAddTableTarget{}, false
	}

	pubName := normalizePublicationIdentifier(tokens[2])
	if pubName == "" {
		return PublicationAddTableTarget{}, false
	}

	idx := 3
	for idx+1 < len(tokens) {
		if strings.EqualFold(tokens[idx], "ADD") && strings.EqualFold(tokens[idx+1], "TABLE") {
			idx += 2
			break
		}
		idx++
	}

	if idx >= len(tokens) {
		return PublicationAddTableTarget{}, false
	}

	relation := normalizePublicationIdentifier(tokens[idx])
	if relation == "" {
		return PublicationAddTableTarget{}, false
	}

	parts := splitQualifiedIdentifier(relation)
	if len(parts) == 0 {
		return PublicationAddTableTarget{}, false
	}

	table := parts[len(parts)-1]
	schema := ""
	if len(parts) > 1 {
		schema = parts[len(parts)-2]
	}

	return PublicationAddTableTarget{
		Publication: pubName,
		Schema:      schema,
		Table:       table,
	}, true
}

func tokenizePublicationSQL(sql string) []string {
	if strings.TrimSpace(sql) == "" {
		return nil
	}

	tokens := make([]string, 0)
	var current strings.Builder
	inDoubleQuote := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		tokens = append(tokens, current.String())
		current.Reset()
	}

	for i := 0; i < len(sql); i++ {
		ch := sql[i]

		if inDoubleQuote {
			current.WriteByte(ch)
			if ch == '"' {
				if i+1 < len(sql) && sql[i+1] == '"' {
					current.WriteByte(sql[i+1])
					i++
					continue
				}
				inDoubleQuote = false
			}
			continue
		}

		switch ch {
		case '"':
			inDoubleQuote = true
			current.WriteByte(ch)
		case ' ', '\t', '\n', '\r', '\f', '\v', ',', '(', ')', ';':
			flush()
		default:
			current.WriteByte(ch)
		}
	}

	flush()
	return tokens
}

func normalizePublicationIdentifier(identifier string) string {
	trimmed := strings.TrimSpace(identifier)
	trimmed = strings.Trim(trimmed, ";")
	if trimmed == "" {
		return ""
	}
	return strings.ReplaceAll(trimmed, "\"", "")
}

func splitQualifiedIdentifier(identifier string) []string {
	parts := make([]string, 0, 2)
	var current strings.Builder
	inDoubleQuote := false

	flush := func() {
		if current.Len() == 0 {
			return
		}
		parts = append(parts, strings.ReplaceAll(current.String(), "\"", ""))
		current.Reset()
	}

	for i := 0; i < len(identifier); i++ {
		ch := identifier[i]
		if ch == '"' {
			inDoubleQuote = !inDoubleQuote
			current.WriteByte(ch)
			continue
		}

		if ch == '.' && !inDoubleQuote {
			flush()
			continue
		}

		current.WriteByte(ch)
	}

	flush()
	return parts
}
