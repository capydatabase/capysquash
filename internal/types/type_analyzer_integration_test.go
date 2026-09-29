//go:build integration

package types

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A column the migrations never defined takes its previous type from the
// database.
func TestAnalyzeMigrationTypesReadsThePreviousTypeFromTheDatabase(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL is required for integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	schema := fmt.Sprintf("capysquash_types_%d", time.Now().UnixNano())
	_, err = db.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA %s; CREATE TABLE %s.orders (total numeric(12,2))", schema, schema))
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = db.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE") })

	ta := NewTypeAnalyzer(NewPostgreSQLTypeSystem("17"), db)
	analysis, err := ta.AnalyzeMigrationTypes(ctx, parsedStatements(t,
		fmt.Sprintf("ALTER TABLE %s.orders ALTER COLUMN total TYPE integer", schema),
	))
	require.NoError(t, err)
	require.Len(t, analysis.TypeChanges, 1)
	assert.Equal(t, "numeric(12,2)", analysis.TypeChanges[0].FromType)
	assert.True(t, analysis.TypeChanges[0].DataLoss)
	assert.Empty(t, analysis.Warnings)
}
