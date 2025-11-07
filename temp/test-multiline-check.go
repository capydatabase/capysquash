package main

import (
	"fmt"
	"strings"
)

// splitColumnDefinitions splits column definitions by comma, respecting nested parentheses
func splitColumnDefinitions(columnList string) []string {
	var parts []string
	var current strings.Builder
	parenDepth := 0
	braceDepth := 0
	inString := false
	var stringDelim rune

	for i, char := range columnList {
		// Handle string literals (both single and double quotes)
		if (char == '\'' || char == '"') && (i == 0 || columnList[i-1] != '\\') {
			if !inString {
				inString = true
				stringDelim = char
			} else if char == stringDelim {
				inString = false
			}
			current.WriteRune(char)
			continue
		}

		// If we're in a string, don't process special characters
		if inString {
			current.WriteRune(char)
			continue
		}

		switch char {
		case '(':
			parenDepth++
			current.WriteRune(char)
		case ')':
			parenDepth--
			current.WriteRune(char)
		case '{':
			braceDepth++
			current.WriteRune(char)
		case '}':
			braceDepth--
			current.WriteRune(char)
		case ',':
			// Only split on commas at depth 0 for both parens and braces
			if parenDepth == 0 && braceDepth == 0 {
				parts = append(parts, current.String())
				current.Reset()
			} else {
				current.WriteRune(char)
			}
		default:
			current.WriteRune(char)
		}
	}

	// Add the last part
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}

func main() {
	// Test case: property_management_scope with multi-line CHECK
	columnList := `
  id TEXT PRIMARY KEY,
  name TEXT,
  property_management_scope TEXT DEFAULT 'individual' CHECK (
    property_management_scope IN ('individual', 'professional', 'enterprise')
  ),
  managed_properties_count INTEGER DEFAULT 0,
  auth_provider TEXT
`

	parts := splitColumnDefinitions(columnList)
	
	fmt.Printf("Found %d column definitions:\n", len(parts))
	for i, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			// Extract column name
			words := strings.Fields(trimmed)
			if len(words) > 0 {
				colName := strings.ToLower(strings.Trim(words[0], "\"'"))
				fmt.Printf("%d. Column: %-30s (def length: %d chars)\n", i+1, colName, len(trimmed))
			}
		}
	}
}
