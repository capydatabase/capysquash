// Package pgnames reproduces the names PostgreSQL chooses for objects a
// statement creates without naming them: the sequence of a serial or identity
// column, the index of a PRIMARY KEY, UNIQUE or EXCLUDE constraint, an index
// created without a name, and a domain's CHECK constraint. The squash needs
// them wherever it must spell out a name PostgreSQL would otherwise derive
// from a table, column or type name that has changed.
package pgnames

import (
	"fmt"
	"strconv"
)

// maxLength is NAMEDATALEN - 1.
const maxLength = 63

// ChooseRelationName follows PostgreSQL's ChooseRelationName: the name
// makeObjectName builds from name1, name2 and label, with a number appended
// to the label while taken reports the name in use.
func ChooseRelationName(name1, name2, label string, taken func(string) bool) string {
	for pass := 0; ; pass++ {
		modLabel := label
		if pass > 0 {
			modLabel = fmt.Sprintf("%s%d", label, pass)
		}
		name := MakeObjectName(name1, name2, modLabel)
		if taken == nil || !taken(name) {
			return name
		}
	}
}

// MakeObjectName follows PostgreSQL's makeObjectName: name1_name2_label
// truncated to 63 bytes by shortening the longer of name1 and name2 first.
// An empty name2 is left out.
func MakeObjectName(name1, name2, label string) string {
	overhead := 1 + len(label) + 1
	if name2 == "" {
		overhead = len(label) + 1
	}
	chars1, chars2 := len(name1), len(name2)
	for chars1+chars2 > maxLength-overhead {
		if chars1 > chars2 {
			chars1--
		} else {
			chars2--
		}
	}
	chars1 = clipRunes(name1, chars1)
	chars2 = clipRunes(name2, chars2)
	name := name1[:chars1]
	if name2 != "" {
		name += "_" + name2[:chars2]
	}
	return name + "_" + label
}

// IndexColumnNames follows PostgreSQL's ChooseIndexColumnNames: one name per
// index column (key and INCLUDE columns), a plain column's own name or
// "expr" for an expression (passed as ""), numbered where they repeat.
func IndexColumnNames(columns []string) []string {
	result := make([]string, 0, len(columns))
	for _, column := range columns {
		original := column
		if original == "" {
			original = "expr"
		}
		current := original
		for i := 1; contains(result, current); i++ {
			suffix := strconv.Itoa(i)
			current = original[:clipRunes(original, min(len(original), maxLength-len(suffix)))] + suffix
		}
		result = append(result, current)
	}
	return result
}

// IndexNameAddition follows PostgreSQL's ChooseIndexNameAddition: the
// column names joined with underscores, stopping once 63 bytes are reached.
func IndexNameAddition(columns []string) string {
	buf := ""
	for _, column := range columns {
		if buf != "" {
			buf += "_"
		}
		buf += column
		if len(buf) >= maxLength+1 {
			break
		}
	}
	return buf
}

// clipRunes shortens a byte count so it does not split a UTF-8 character.
func clipRunes(s string, n int) int {
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return n
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
