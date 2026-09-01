package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseArrayType(t *testing.T) {
	t.Parallel()

	ts := NewPostgreSQLTypeSystem("17")

	arr, err := ts.ParseArrayType("integer[][]")
	require.NoError(t, err)
	assert.Equal(t, "integer", arr.ElementType)
	assert.Equal(t, 2, arr.Dimensions)

	arr, err = ts.ParseArrayType("varchar(255)[10]")
	require.NoError(t, err)
	assert.Equal(t, "varchar(255)", arr.ElementType)
	assert.Equal(t, 1, arr.Dimensions)

	_, err = ts.ParseArrayType("integer")
	assert.Error(t, err)
}

func TestNormalizeTypeNamePrecision(t *testing.T) {
	t.Parallel()

	ts := NewPostgreSQLTypeSystem("17")

	assert.Equal(t, "varchar", ts.normalizeTypeName("varchar(191)"))
	assert.Equal(t, "numeric", ts.normalizeTypeName("numeric(10, 2)"))
	assert.Equal(t, "timestamp", ts.normalizeTypeName("timestamp(6)"))
}

func TestExtractSizeFromSpec(t *testing.T) {
	t.Parallel()

	ts := NewPostgreSQLTypeSystem("17")

	size, err := ts.extractSizeFromSpec("varchar(255)")
	require.NoError(t, err)
	assert.Equal(t, 255, size)

	size, err = ts.extractSizeFromSpec("numeric(12,4)")
	require.NoError(t, err)
	assert.Equal(t, 12, size)

	_, err = ts.extractSizeFromSpec("varchar")
	assert.Error(t, err)
}
