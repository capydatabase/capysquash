package postprocessing

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFixReturnNextWithOutParams_RecordVariable(t *testing.T) {
	t.Parallel()

	input := `CREATE OR REPLACE FUNCTION f()
RETURNS TABLE(id bigint, name text)
AS $$
BEGIN
  RETURN NEXT rec;
END;
$$ LANGUAGE plpgsql;`

	got := FixReturnNextWithOutParams(input, nil)
	assert.Contains(t, got, "RETURN QUERY SELECT rec.id, rec.name;")
}

func TestFixReturnNextWithOutParams_NoArg(t *testing.T) {
	t.Parallel()

	input := `CREATE FUNCTION f()
RETURNS TABLE(id bigint, name text)
AS $$
BEGIN
  RETURN NEXT;
END;
$$ LANGUAGE plpgsql;`

	got := FixReturnNextWithOutParams(input, nil)
	assert.Contains(t, got, "RETURN QUERY SELECT id, name;")
}

func TestFixReturnNextWithOutParams_NoChangeForNonReturnsTable(t *testing.T) {
	t.Parallel()

	input := `CREATE FUNCTION f()
RETURNS SETOF record
AS $$
BEGIN
  RETURN NEXT rec;
END;
$$ LANGUAGE plpgsql;`

	got := FixReturnNextWithOutParams(input, nil)
	assert.Equal(t, input, got)
}
