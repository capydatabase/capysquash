package consolidation

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParsePublicationAddTable(t *testing.T) {
	t.Parallel()

	parsed, ok := parsePublicationAddTable(`ALTER PUBLICATION "app_pub" ADD TABLE "public"."users";`)
	assert.True(t, ok)
	assert.Equal(t, "app_pub", parsed.Publication)
	assert.Equal(t, "public", parsed.Schema)
	assert.Equal(t, "users", parsed.Table)
}

func TestParsePublicationAddTable_NoSchema(t *testing.T) {
	t.Parallel()

	parsed, ok := parsePublicationAddTable(`ALTER PUBLICATION app_pub ADD TABLE users;`)
	assert.True(t, ok)
	assert.Equal(t, "app_pub", parsed.Publication)
	assert.Equal(t, "", parsed.Schema)
	assert.Equal(t, "users", parsed.Table)
}
