package squasher

import (
	"strings"
	"testing"

	"github.com/capydatabase/capysquash/internal/config"
	"github.com/stretchr/testify/require"
)

// Columns of an enum, a domain or a composite type, renamed or in another
// schema, and columns added with ALTER TABLE, depend on types the history
// creates: no "which is never created" warning for them. A type nothing
// creates is still reported.
func TestSquash_NoNeverCreatedWarningForTrackedTypes(t *testing.T) {
	t.Parallel()
	migrations := map[int]string{
		1: `CREATE TYPE mood AS ENUM ('happy', 'sad');
CREATE DOMAIN email AS text CHECK (VALUE LIKE '%@%');
CREATE TYPE money_amount AS (amount numeric, currency text);
CREATE TABLE people (id int PRIMARY KEY, m mood, e email, balance money_amount);
CREATE TYPE old_status AS ENUM ('a', 'b');
CREATE DOMAIN old_positive AS int CHECK (VALUE > 0);`,
		2: `ALTER TYPE old_status RENAME TO status;
ALTER DOMAIN old_positive RENAME TO positive;
CREATE TABLE things (id int, s status, p positive);
CREATE SCHEMA app;
CREATE DOMAIN app.pos AS int CHECK (VALUE > 0);
CREATE TABLE app.t (p app.pos);
CREATE TYPE later_kind AS ENUM ('x');
ALTER TABLE people ADD COLUMN kind later_kind;`,
		3: `CREATE TABLE orphans (id int, v no_such_type);`,
	}
	e, err := NewEngine(EngineConfig{Config: &config.Config{SafetyLevel: "standard"}})
	require.NoError(t, err)
	result, err := e.SquashWithSeparateFiles(migrations)
	require.NoError(t, err)

	var neverCreated []string
	for _, warning := range result.Warnings {
		if strings.Contains(warning, "which is never created") {
			neverCreated = append(neverCreated, warning)
		}
	}
	require.Len(t, neverCreated, 1, "warnings: %v", neverCreated)
	require.Contains(t, neverCreated[0], "no_such_type")
}
