package prisma

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOptimizeVarcharLength(t *testing.T) {
	t.Parallel()

	plugin := NewPrismaPlugin()
	input := "CREATE TABLE users (name VARCHAR(191), email VARCHAR(191));"

	got := plugin.optimizeVarcharLength(input)
	assert.Equal(t, "CREATE TABLE users (name VARCHAR(255), email VARCHAR(255));", got)
}

func TestFixFunctionVolatility(t *testing.T) {
	t.Parallel()

	plugin := NewPrismaPlugin()
	input := `CREATE FUNCTION public.normalize_email(input text)
RETURNS text
LANGUAGE sql
AS $$ SELECT lower(input); $$;`

	got, err := plugin.FixFunctionVolatility(context.Background(), input)
	assert.NoError(t, err)
	assert.Contains(t, got, "LANGUAGE sql STABLE AS $$")

	existing := `CREATE FUNCTION public.f() RETURNS text LANGUAGE sql IMMUTABLE AS $$ SELECT 'x'; $$;`
	unchanged, err := plugin.FixFunctionVolatility(context.Background(), existing)
	assert.NoError(t, err)
	assert.Equal(t, existing, unchanged)
}

func TestExtractPrismaComment(t *testing.T) {
	t.Parallel()

	plugin := NewPrismaPlugin()
	action, target, found := plugin.ExtractPrismaComment("-- CreateTable users\nCREATE TABLE users(id bigint);")

	assert.True(t, found)
	assert.Equal(t, "CreateTable", action)
	assert.Equal(t, "users", target)
}

func TestIsPrismaGeneratedSQL(t *testing.T) {
	t.Parallel()

	plugin := NewPrismaPlugin()
	assert.True(t, plugin.IsPrismaGeneratedSQL("-- CreateIndex users_email_idx\nCREATE INDEX users_email_idx ON users(email);"))
	assert.True(t, plugin.IsPrismaGeneratedSQL("CREATE TABLE _prisma_migrations(id text);"))
	assert.False(t, plugin.IsPrismaGeneratedSQL("CREATE TABLE users(id bigint);"))
}
