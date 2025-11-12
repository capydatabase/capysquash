package utils

import (
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/patterns"
)

// ExtractPolicyTargetTable extracts the target table name from a CREATE POLICY statement.
// Pattern: CREATE POLICY ... ON [schema.]table
//
// Example:
//   ExtractPolicyTargetTable("CREATE POLICY p1 ON public.users ...") // "public.users"
//   ExtractPolicyTargetTable("CREATE POLICY p1 ON users FOR SELECT ...") // "users"
func ExtractPolicyTargetTable(sql string) string {
    // Find " ON " keyword (case-insensitive)
    onIndex := strings.Index(strings.ToUpper(sql), " ON ")
    if onIndex == -1 {
        return ""
    }

    // Get text after " ON "
    afterOn := sql[onIndex+4:]
    parts := strings.Fields(afterOn)

    if len(parts) > 0 {
        return strings.TrimSpace(parts[0])
    }

    return ""
}

// ExtractFunctionName extracts the function name from a CREATE FUNCTION statement.
// Handles both simple names and schema-qualified names.
//
// Example:
//   ExtractFunctionName("CREATE FUNCTION auth.jwt() RETURNS ...") // "jwt"
//   ExtractFunctionName("CREATE OR REPLACE FUNCTION my_func() ...") // "my_func"
func ExtractFunctionName(sql string) string {
    // Pattern: CREATE [OR REPLACE] FUNCTION [schema.]name(
    // Using precompiled pattern for performance
    matches := patterns.FunctionPattern.FindStringSubmatch(sql)
    if len(matches) > 1 {
        return matches[1]
    }

    return ""
}

// ExtractTableName extracts the table name from a CREATE TABLE or ALTER TABLE statement.
// Handles schema-qualified names and IF NOT EXISTS clauses.
//
// Example:
//   ExtractTableName("CREATE TABLE public.users (...)") // "users"
//   ExtractTableName("CREATE TABLE IF NOT EXISTS accounts (...)") // "accounts"
//   ExtractTableName("ALTER TABLE users ADD COLUMN ...") // "users"
func ExtractTableName(sql string) string {
    sqlUpper := strings.ToUpper(sql)

    // Check for CREATE TABLE
    if strings.Contains(sqlUpper, "CREATE TABLE") {
        // Using precompiled pattern for performance
        matches := patterns.CreateTablePattern.FindStringSubmatch(sql)
        if len(matches) > 2 {
            // Return just the table name (without schema)
            return matches[2]
        }
    }

    // Check for ALTER TABLE
    if strings.Contains(sqlUpper, "ALTER TABLE") {
        // Using precompiled pattern for performance
        matches := patterns.AlterTablePattern.FindStringSubmatch(sql)
        if len(matches) > 2 {
            return matches[2]
        }
    }

    return ""
}

// ExtractSchemaName extracts the schema name from a CREATE SCHEMA statement.
//
// Example:
//   ExtractSchemaName("CREATE SCHEMA IF NOT EXISTS auth") // "auth"
//   ExtractSchemaName("CREATE SCHEMA public AUTHORIZATION ...") // "public"
func ExtractSchemaName(sql string) string {
    // Using precompiled pattern for performance
    matches := patterns.CreateSchemaPattern.FindStringSubmatch(sql)
    if len(matches) > 1 {
        return matches[1]
    }

    return ""
}

// ExtractIndexName extracts the index name from a CREATE INDEX statement.
//
// Example:
//   ExtractIndexName("CREATE INDEX idx_users_email ON users(email)") // "idx_users_email"
//   ExtractIndexName("CREATE UNIQUE INDEX CONCURRENTLY idx_pk ON t(id)") // "idx_pk"
func ExtractIndexName(sql string) string {
    // Using precompiled pattern for performance
    matches := patterns.CreateIndexPattern.FindStringSubmatch(sql)
    if len(matches) > 1 {
        return matches[1]
    }

    return ""
}

// IsDMLStatement checks if SQL is a Data Manipulation Language statement.
// Returns true for INSERT, UPDATE, DELETE, SELECT.
func IsDMLStatement(sql string) bool {
    sqlUpper := strings.ToUpper(strings.TrimSpace(sql))

    return strings.HasPrefix(sqlUpper, "INSERT ") ||
           strings.HasPrefix(sqlUpper, "UPDATE ") ||
           strings.HasPrefix(sqlUpper, "DELETE ") ||
           strings.HasPrefix(sqlUpper, "SELECT ")
}

// IsDDLStatement checks if SQL is a Data Definition Language statement.
// Returns true for CREATE, ALTER, DROP, TRUNCATE.
func IsDDLStatement(sql string) bool {
    sqlUpper := strings.ToUpper(strings.TrimSpace(sql))

    return strings.HasPrefix(sqlUpper, "CREATE ") ||
           strings.HasPrefix(sqlUpper, "ALTER ") ||
           strings.HasPrefix(sqlUpper, "DROP ") ||
           strings.HasPrefix(sqlUpper, "TRUNCATE ")
}
