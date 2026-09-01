package validation

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	catalogsqlc "github.com/capysquash/pgsquash-engine/internal/metadata/sqlc"
	schemamodel "github.com/capysquash/pgsquash-engine/internal/schema"
)

// SchemaComparator compares two live PostgreSQL schemas using catalog signatures.
type SchemaComparator struct {
	logger Logger
}

// Logger interface to decouple from specific logging implementation
type Logger interface {
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// NewSchemaComparator creates a new schema comparator
func NewSchemaComparator(logger Logger) *SchemaComparator {
	return &SchemaComparator{
		logger: logger,
	}
}

// CompareDatabases compares two databases and returns a detailed difference report.
//
// This implementation intentionally avoids external schema-diff engines and uses
// deterministic catalog signatures gathered directly from each database.
func (sc *SchemaComparator) CompareDatabases(ctx context.Context, sourceDB, targetDB *sql.DB) (*SchemaDiff, error) {
	if sourceDB == nil || targetDB == nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"source and target databases are required for comparison",
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}

	sourceLines, err := collectSchemaSignature(ctx, sourceDB)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to collect source schema signature",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	targetLines, err := collectSchemaSignature(ctx, targetDB)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to collect target schema signature",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	return compareLineSets(sourceLines, targetLines), nil
}

func collectSchemaSignature(ctx context.Context, db *sql.DB) ([]string, error) {
	lines := make([]string, 0, 1024)
	queries := catalogsqlc.New(db)

	if err := collectExtensions(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectTableColumns(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectConstraints(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectIndexes(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectViews(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectFunctions(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectTriggers(ctx, queries, &lines); err != nil {
		return nil, err
	}
	if err := collectPolicies(ctx, queries, &lines); err != nil {
		return nil, err
	}

	sort.Strings(lines)
	return lines, nil
}

func collectExtensions(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureExtensions(ctx)
	if err != nil {
		return fmt.Errorf("list extensions: %w", err)
	}

	for _, row := range rows {
		*lines = append(*lines, fmt.Sprintf("extension|%s|%s", row.Name, row.Version))
	}

	return nil
}

func collectTableColumns(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureTableColumns(ctx)
	if err != nil {
		return fmt.Errorf("list table columns: %w", err)
	}

	for _, row := range rows {

		line := fmt.Sprintf(
			"column|%s.%s|%04d|%s|%s|notnull=%t|default=%s",
			row.SchemaName,
			row.TableName,
			row.Attnum,
			row.ColumnName,
			normalizeSQLWhitespace(row.DataType),
			row.NotNull,
			normalizeSQLWhitespace(row.DefaultExpr),
		)
		*lines = append(*lines, line)
	}

	return nil
}

func collectConstraints(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureConstraints(ctx)
	if err != nil {
		return fmt.Errorf("list constraints: %w", err)
	}

	for _, row := range rows {
		*lines = append(*lines, hashedSignature("constraint", fmt.Sprintf("%s.%s:%s", row.SchemaName, row.TableName, row.ConstraintName), row.Definition))
	}

	return nil
}

func collectIndexes(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureIndexes(ctx)
	if err != nil {
		return fmt.Errorf("list indexes: %w", err)
	}

	for _, row := range rows {
		*lines = append(*lines, hashedSignature("index", fmt.Sprintf("%s.%s:%s", row.SchemaName, row.TableName, row.IndexName), row.Definition))
	}

	return nil
}

func collectViews(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureViews(ctx)
	if err != nil {
		return fmt.Errorf("list views: %w", err)
	}

	for _, row := range rows {

		kind := "view"
		if row.RelKind == "m" {
			kind = "materialized_view"
		}

		*lines = append(*lines, hashedSignature(kind, fmt.Sprintf("%s.%s", row.SchemaName, row.ViewName), row.Definition))
	}

	return nil
}

func collectFunctions(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureFunctions(ctx)
	if err != nil {
		return fmt.Errorf("list functions: %w", err)
	}

	for _, row := range rows {

		identifier := fmt.Sprintf("%s.%s(%s)", row.SchemaName, row.FunctionName, normalizeSQLWhitespace(row.IdentityArguments))
		*lines = append(*lines, hashedSignature("function", identifier, row.Definition))
	}

	return nil
}

func collectTriggers(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignatureTriggers(ctx)
	if err != nil {
		return fmt.Errorf("list triggers: %w", err)
	}

	for _, row := range rows {
		*lines = append(*lines, hashedSignature("trigger", fmt.Sprintf("%s.%s:%s", row.SchemaName, row.TableName, row.TriggerName), row.Definition))
	}

	return nil
}

func collectPolicies(ctx context.Context, queries *catalogsqlc.Queries, lines *[]string) error {
	rows, err := queries.ListSignaturePolicies(ctx)
	if err != nil {
		return fmt.Errorf("list policies: %w", err)
	}

	for _, row := range rows {

		definition := fmt.Sprintf(
			"cmd=%s permissive=%t using=%s withcheck=%s",
			row.Command,
			row.Permissive,
			normalizeSQLWhitespace(row.UsingExpr),
			normalizeSQLWhitespace(row.CheckExpr),
		)

		*lines = append(*lines, hashedSignature("policy", fmt.Sprintf("%s.%s:%s", row.SchemaName, row.TableName, row.PolicyName), definition))
	}

	return nil
}

func hashedSignature(kind, identifier, definition string) string {
	normalized := normalizeSQLWhitespace(definition)
	return fmt.Sprintf("%s|%s|sha256:%s", kind, identifier, shortHash(normalized))
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	// 16 bytes (32 hex chars) is enough for readable, collision-resistant signatures here.
	return hex.EncodeToString(sum[:16])
}

func compareLineSets(sourceLines, targetLines []string) *SchemaDiff {
	sourceModel := schemamodel.BuildFromLines(sourceLines)
	targetModel := schemamodel.BuildFromLines(targetLines)
	diff := schemamodel.CompareModelsWithLabels(sourceModel, targetModel, "original", "squashed", 250)

	return &SchemaDiff{
		HasDifferences: diff.HasDifferences,
		Differences:    diff.Differences,
	}
}

// CompareSchemasDirectly compares two schema SQL strings directly using normalized statement sets.
func CompareSchemasDirectly(schema1, schema2 string) (*SchemaDiff, error) {
	leftModel := schemamodel.BuildFromSQL("original.sql", schema1)
	rightModel := schemamodel.BuildFromSQL("squashed.sql", schema2)
	diff := schemamodel.CompareModelsWithLabels(leftModel, rightModel, "original", "squashed", 250)

	return &SchemaDiff{
		HasDifferences: diff.HasDifferences,
		Differences:    diff.Differences,
	}, nil
}

func normalizeSQLWhitespace(sql string) string {
	return schemamodel.NormalizeSQLWhitespace(sql)
}
