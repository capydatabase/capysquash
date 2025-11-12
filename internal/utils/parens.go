package utils

import (
    "strings"
)

// ExtractBalancedParentheses extracts text within balanced parentheses from the given string.
// Starts from the first '(' and returns everything up to and including the matching ')'.
//
// This is commonly used for:
//   - Extracting RLS policy USING clauses
//   - Extracting RLS policy WITH CHECK clauses
//   - Extracting function parameters
//   - Extracting composite type definitions
//
// Example:
//   ExtractBalancedParentheses("USING (id = current_user_id())") // "(id = current_user_id())"
//   ExtractBalancedParentheses("FUNCTION foo(a INT, b INT) RETURNS") // "(a INT, b INT)"
//
// Returns:
//   - The balanced parentheses substring (including the parentheses)
//   - Empty string if no balanced parentheses found
func ExtractBalancedParentheses(text string) string {
    depth := 0
    start := strings.Index(text, "(")

    if start == -1 {
        return ""
    }

    for i := start; i < len(text); i++ {
        switch text[i] {
        case '(':
            depth++
        case ')':
            depth--
            if depth == 0 {
                return text[start : i+1]
            }
        }
    }

    // If we get here, parentheses are unbalanced
    // Return from start to end
    return text[start:]
}

// ExtractAllBalancedParentheses finds all balanced parentheses groups in the text.
// Returns a slice of strings, each containing a balanced parentheses group.
//
// Example:
//   ExtractAllBalancedParentheses("foo(a) bar(b, c)") // ["(a)", "(b, c)"]
func ExtractAllBalancedParentheses(text string) []string {
    var results []string
    currentPos := 0

    for currentPos < len(text) {
        // Find next opening paren
        start := strings.Index(text[currentPos:], "(")
        if start == -1 {
            break
        }

        start += currentPos
        depth := 0
        found := false

        for i := start; i < len(text); i++ {
            switch text[i] {
            case '(':
                depth++
            case ')':
                depth--
                if depth == 0 {
                    results = append(results, text[start:i+1])
                    currentPos = i + 1
                    found = true
                    goto nextParen
                }
            }
        }

    nextParen:
        if !found {
            // Unbalanced, move past this paren
            currentPos = start + 1
        }
    }

    return results
}

// HasBalancedParentheses checks if the text has properly balanced parentheses.
// Returns true if all parentheses are balanced, false otherwise.
//
// Example:
//   HasBalancedParentheses("(a (b c))") // true
//   HasBalancedParentheses("(a (b c)") // false
func HasBalancedParentheses(text string) bool {
    depth := 0

    for _, ch := range text {
        switch ch {
        case '(':
            depth++
        case ')':
            depth--
            if depth < 0 {
                return false
            }
        }
    }

    return depth == 0
}

// StripOutermostParentheses removes the outermost pair of parentheses if present.
// If the text doesn't start and end with matching parentheses, returns the original text.
//
// Example:
//   StripOutermostParentheses("(id = 1)") // "id = 1"
//   StripOutermostParentheses("((id = 1))") // "(id = 1)"
//   StripOutermostParentheses("id = 1") // "id = 1"
func StripOutermostParentheses(text string) string {
    trimmed := strings.TrimSpace(text)

    if len(trimmed) < 2 {
        return text
    }

    if trimmed[0] == '(' && trimmed[len(trimmed)-1] == ')' {
        // Verify these are actually matching parentheses
        depth := 0
        for i, ch := range trimmed {
            switch ch {
            case '(':
                depth++
            case ')':
                depth--
                // If we hit zero before the end, these aren't the outermost pair
                if depth == 0 && i < len(trimmed)-1 {
                    return text
                }
            }
        }

        if depth == 0 {
            return trimmed[1 : len(trimmed)-1]
        }
    }

    return text
}
