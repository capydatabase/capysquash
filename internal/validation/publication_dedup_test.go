package validation

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDeduplicatePublicationStatements(t *testing.T) {
	t.Parallel()

	input := `ALTER PUBLICATION app_pub ADD TABLE public.users;
ALTER PUBLICATION app_pub ADD TABLE public.users;
ALTER PUBLICATION "app_pub" ADD TABLE "public"."accounts";`

	got := deduplicatePublicationStatements(input)

	assert.Contains(t, got, "ALTER PUBLICATION app_pub ADD TABLE public.users;")
	assert.Contains(t, got, `ALTER PUBLICATION "app_pub" ADD TABLE "public"."accounts";`)
	assert.Equal(t, 2, len(splitNonEmptyLines(got)))
}

func TestParsePublicationAddTable(t *testing.T) {
	t.Parallel()

	parsed, ok := parsePublicationAddTable(`ALTER PUBLICATION "app_pub" ADD TABLE "public"."users";`)
	assert.True(t, ok)
	assert.Equal(t, "app_pub", parsed.Publication)
	assert.Equal(t, "public", parsed.Schema)
	assert.Equal(t, "users", parsed.Table)
}

func splitNonEmptyLines(sql string) []string {
	lines := make([]string, 0)
	for line := range strings.SplitSeq(sql, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	return lines
}
