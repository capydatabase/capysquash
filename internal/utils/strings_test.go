package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeSQLWhitespace(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "SELECT * FROM users", NormalizeSQLWhitespace("  SELECT   *   FROM   users  "))
	assert.Equal(t, "", NormalizeSQLWhitespace(" \n\t "))
}
