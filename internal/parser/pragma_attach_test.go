package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseForComments(t *testing.T, src string) []statementComments {
	t.Helper()
	m, err := ParseMigration(src, "test.sql")
	require.NoError(t, err)
	out := make([]statementComments, 0, len(m.Statements))
	for _, s := range m.Statements {
		out = append(out, statementComments{
			name:     s.ObjectName,
			line:     s.Line,
			comments: s.Comments,
			verbatim: s.Metadata.PreserveVerbatim,
		})
	}
	return out
}

type statementComments struct {
	name     string
	line     int
	comments []string
	verbatim bool
}

// Comments attach by position, not by index: two comments above the first
// statement and none above the second must not leak onto the second.
func TestCommentsAttachToTheStatementTheyPrecede(t *testing.T) {
	got := parseForComments(t, `-- capysquash:ignore
-- keep the users table as written
CREATE TABLE users (id int);

CREATE TABLE posts (id int);
`)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"-- capysquash:ignore", "-- keep the users table as written"}, got[0].comments)
	assert.True(t, got[0].verbatim)
	assert.Empty(t, got[1].comments)
	assert.False(t, got[1].verbatim)
}

func TestTrailingSameLineCommentAttachesToThePreviousStatement(t *testing.T) {
	got := parseForComments(t, `CREATE TABLE users (id int); -- capysquash:no-merge
CREATE TABLE posts (id int);
`)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"-- capysquash:no-merge"}, got[0].comments)
	assert.True(t, got[0].verbatim)
	assert.Empty(t, got[1].comments)
	assert.False(t, got[1].verbatim)
}

func TestCommentInsideAStatementAttachesToIt(t *testing.T) {
	got := parseForComments(t, `CREATE TABLE users (
    id int, /* capysquash:ignore */
    name text
);
CREATE TABLE posts (id int);
`)
	require.Len(t, got, 2)
	assert.Equal(t, []string{"/* capysquash:ignore */"}, got[0].comments)
	assert.True(t, got[0].verbatim)
	assert.Empty(t, got[1].comments)
}

func TestCommentsAfterTheLastStatementAttachToNothing(t *testing.T) {
	got := parseForComments(t, `CREATE TABLE users (id int);

-- capysquash:ignore
`)
	require.Len(t, got, 1)
	assert.Empty(t, got[0].comments)
	assert.False(t, got[0].verbatim)
}

// A pragma-looking line inside a function body is part of the body literal,
// not a comment on the statement.
func TestCommentsInsideDollarQuotedBodiesDoNotAttach(t *testing.T) {
	got := parseForComments(t, `CREATE FUNCTION f() RETURNS int AS $$
BEGIN
    -- capysquash:ignore
    RETURN 1;
END;
$$ LANGUAGE plpgsql;
`)
	require.Len(t, got, 1)
	assert.Empty(t, got[0].comments)
}

func TestStatementLinesPointIntoTheOriginalFile(t *testing.T) {
	got := parseForComments(t, `-- header

-- more header
CREATE TABLE users (id int);

/* block
   comment */
CREATE TABLE posts (id int);
`)
	require.Len(t, got, 2)
	assert.Equal(t, 4, got[0].line)
	assert.Equal(t, 8, got[1].line)
}

// The pragma fixture documents that these statements are preserved verbatim.
func TestPragmaFixtureStatementsArePreservedVerbatim(t *testing.T) {
	dir := filepath.Join("..", "..", "test-fixtures", "pragma_examples", "original")
	verbatim := map[string]bool{}
	for _, name := range []string{"001_create_users.sql", "002_create_posts.sql", "003_add_data.sql"} {
		content, err := os.ReadFile(filepath.Join(dir, name))
		require.NoError(t, err)
		m, err := ParseMigration(string(content), name)
		require.NoError(t, err)
		for _, s := range m.Statements {
			verbatim[name+":"+s.ObjectName] = s.Metadata.PreserveVerbatim
		}
	}

	assert.Equal(t, map[string]bool{
		"001_create_users.sql:public.users":             true,
		"002_create_posts.sql:public.posts":             false,
		"002_create_posts.sql:public.idx_posts_user_id": false,
		"002_create_posts.sql:public.idx_posts_title":   true,
		"003_add_data.sql:public.users":                 true,
		"003_add_data.sql:public.posts":                 false,
	}, verbatim)
}
