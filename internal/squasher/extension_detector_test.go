package squasher

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCreateExtensionStatements_Multiline(t *testing.T) {
	t.Parallel()

	content := `-- setup
CREATE EXTENSION IF NOT EXISTS "postgis"
VERSION '3.5.0'
WITH SCHEMA public;

CREATE EXTENSION pg_trgm;`

	refs := parseCreateExtensionStatements(content)
	require.Len(t, refs, 2)

	assert.Equal(t, "postgis", refs[0].Name)
	assert.Equal(t, "3.5.0", refs[0].Version)
	assert.Equal(t, "public", refs[0].Schema)
	assert.Equal(t, 2, refs[0].Line)

	assert.Equal(t, "pg_trgm", refs[1].Name)
	assert.Equal(t, "", refs[1].Version)
	assert.Equal(t, "", refs[1].Schema)
}

func TestDetectExtensionRefs(t *testing.T) {
	t.Parallel()

	detector := NewExtensionDetector()
	content := `CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION postgis VERSION '3.5.0' WITH SCHEMA public;`

	refs := detector.DetectExtensionRefs(content)
	require.Len(t, refs, 2)

	assert.Equal(t, "uuid-ossp", refs[0].Name)
	assert.Equal(t, "postgis", refs[1].Name)
	assert.Equal(t, "3.5.0", refs[1].Version)
	assert.Equal(t, "public", refs[1].Schema)
}

func TestDetectExtensionsInContent_Indicators(t *testing.T) {
	t.Parallel()

	detector := NewExtensionDetector()
	content := `SELECT uuid_generate_v4();
SELECT ST_Point(1, 2);`

	extensions := detector.detectExtensionsInContent(content)
	assert.Contains(t, extensions, "uuid-ossp")
	assert.Contains(t, extensions, "postgis")
}

func TestDetectAuthService_ClerkPatterns(t *testing.T) {
	t.Parallel()

	detector := NewExtensionDetector()

	migrations := map[int]string{
		1: `CREATE OR REPLACE FUNCTION clerk_user_id() RETURNS text AS $$
BEGIN
  RETURN current_clerk_claims()->>'sub';
END;
$$ LANGUAGE plpgsql;`,
	}

	authService := detector.detectAuthService(migrations)
	assert.Equal(t, AuthServiceClerk, authService)
}

func TestAnalyzeMigrations_ClerkCompatibilityContainsCoreHelpers(t *testing.T) {
	t.Parallel()

	detector := NewExtensionDetector()
	migrations := map[int]string{
		1: `SELECT clerk_user_id(), current_clerk_claims();`,
	}

	analysis := detector.AnalyzeMigrations(migrations)
	assert.Equal(t, AuthServiceClerk, analysis.AuthService)

	sql := strings.ToLower(analysis.AuthCompatibilitySQL)
	assert.Contains(t, sql, "create or replace function current_clerk_claims()")
	assert.Contains(t, sql, "create or replace function clerk_user_id()")
	assert.Contains(t, sql, "create or replace function current_clerk_user_id()")
	assert.Contains(t, sql, "create or replace function clerk_is_admin()")
	assert.Contains(t, sql, "create or replace function is_authenticated()")
	assert.Contains(t, sql, "create or replace function user_has_valid_mfa()")
}
