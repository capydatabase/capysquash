package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractFunctionName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "jwt", ExtractFunctionName("CREATE FUNCTION auth.jwt() RETURNS text AS $$ SELECT 'x'; $$ LANGUAGE sql;"))
	assert.Equal(t, "my_func", ExtractFunctionName("CREATE OR REPLACE FUNCTION my_func(a int) RETURNS int AS $$ SELECT a; $$ LANGUAGE sql;"))
	assert.Equal(t, "quoted_name", ExtractFunctionName(`CREATE FUNCTION "public"."quoted_name"() RETURNS int AS $$ SELECT 1; $$ LANGUAGE sql;`))
}

func TestExtractTableName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "users", ExtractTableName("CREATE TABLE public.users (id bigint);"))
	assert.Equal(t, "accounts", ExtractTableName("CREATE TABLE IF NOT EXISTS accounts (id bigint);"))
	assert.Equal(t, "users", ExtractTableName("ALTER TABLE users ADD COLUMN email text;"))
	assert.Equal(t, "users", ExtractTableName("ALTER TABLE IF EXISTS public.users ADD COLUMN email text;"))
}

func TestExtractSchemaAndIndexName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "auth", ExtractSchemaName("CREATE SCHEMA IF NOT EXISTS auth;"))
	assert.Equal(t, "public", ExtractSchemaName("CREATE SCHEMA public AUTHORIZATION postgres;"))
	assert.Equal(t, "idx_users_email", ExtractIndexName("CREATE INDEX idx_users_email ON users(email);"))
	assert.Equal(t, "idx_pk", ExtractIndexName("CREATE UNIQUE INDEX CONCURRENTLY idx_pk ON t(id);"))
}
