package squasher

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInsertStatementBreaks(t *testing.T) {
	t.Parallel()

	input := "CREATE TABLE t (id BIGINT);  CREATE INDEX idx_t_id ON t(id);"
	got := insertStatementBreaks(input)

	assert.Contains(t, got, ";\n\nCREATE INDEX")
}

func TestAddTupleCommaLineBreaks(t *testing.T) {
	t.Parallel()

	input := "(id BIGINT),    (name TEXT)"
	got := addTupleCommaLineBreaks(input)

	assert.Equal(t, "(id BIGINT),\n  (name TEXT)", got)
}

func TestInjectMarkersBeforeAS(t *testing.T) {
	t.Parallel()

	input := "CREATE FUNCTION public.f() RETURNS int LANGUAGE sql AS $$ SELECT 1 $$;"

	withVolatility, err := injectVolatilityMarker(input, "STABLE")
	assert.NoError(t, err)
	assert.Contains(t, strings.ToUpper(withVolatility), "LANGUAGE SQL STABLE AS $$")

	withSecurity, err := injectSecurityDefiner(withVolatility, "SECURITY DEFINER")
	assert.NoError(t, err)
	assert.Contains(t, strings.ToUpper(withSecurity), "LANGUAGE SQL STABLE SECURITY DEFINER AS $$")
}

func TestExtractVolatilityAndSecurityMarkers(t *testing.T) {
	t.Parallel()

	input := "CREATE FUNCTION f() RETURNS int LANGUAGE plpgsql STABLE SECURITY DEFINER AS $$ BEGIN RETURN 1; END; $$;"

	assert.Equal(t, "STABLE", extractVolatilityMarker(input))
	assert.Equal(t, "SECURITY DEFINER", extractSecurityDefiner(input))
}
