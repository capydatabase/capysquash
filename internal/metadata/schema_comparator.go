// Package metadata provides schema comparison functionality
package metadata

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	schemamodel "github.com/capydatabase/capysquash/internal/schema"
)

// ComparisonResult is the outcome of a structural comparison between the
// expected schema (production) and the actual schema (the squashed baseline
// applied to an empty database). Both sides are DatabaseMetadata loaded by the
// same MetadataManager, so every definition was rendered by PostgreSQL itself.
//
// IsValid is false when the baseline would not reproduce production inside
// the compared schemas: any object missing on either side, any column whose
// type, nullability, default, generation expression, identity or collation
// differs, a different column order, a different row-level-security flag,
// and any constraint, index, trigger, policy, view, materialized view,
// function (per identity signature), sequence, enum (labels in order) or other
// type whose definition differs. An extension the baseline creates but
// production does not have also invalidates the result, because the baseline
// could not be applied there.
//
// Warnings never affect IsValid: an extension installed in production that the
// baseline does not create (managed platforms preinstall many), and an
// extension whose version differs.
type ComparisonResult struct {
	IsValid             bool
	MissingExtensions   []string
	TypeMismatches      []TypeMismatch
	ConstraintConflicts []ConstraintConflict
	Warnings            []string
	SchemaDrift         []SchemaDrift
}

// TypeMismatch represents a column whose data type differs.
type TypeMismatch struct {
	Object       string
	Column       string
	ExpectedType string
	ActualType   string
	IsBreaking   bool
}

// ConstraintConflict represents a constraint mismatch
type ConstraintConflict struct {
	Table          string
	ConstraintName string
	ExpectedDef    string
	ActualDef      string
	ConflictType   string // "missing" (not in the baseline), "different", "extra" (not in production)
}

// SchemaDrift represents drift between the squashed baseline and production
type SchemaDrift struct {
	Object      string
	ObjectType  string
	Description string
	DriftType   string // "missing_in_db" (baseline only), "extra_in_db" (production only), "definition_mismatch"
}

// CompareOptions scopes a structural comparison.
type CompareOptions struct {
	// Schemas limits the comparison to these schemas; empty compares every
	// schema present in either model.
	Schemas []string
	// Environment describes what the scratch database already contained before
	// the baseline was applied (platform schemas, auth compatibility objects,
	// preinstalled extensions). Those objects are excluded from both sides.
	Environment *DatabaseMetadata
}

// Kinds of catalog objects in a flattened model.
const (
	kindSchema           = "SCHEMA"
	kindTable            = "TABLE"
	kindColumn           = "COLUMN"
	kindConstraint       = "CONSTRAINT"
	kindIndex            = "INDEX"
	kindTrigger          = "TRIGGER"
	kindPolicy           = "POLICY"
	kindView             = "VIEW"
	kindMaterializedView = "MATERIALIZED VIEW"
	kindFunction         = "FUNCTION"
	kindSequence         = "SEQUENCE"
	kindType             = "TYPE"
)

// catalogObject is one comparable object with its properties in a fixed order.
type catalogObject struct {
	kind   string
	schema string
	name   string // qualified display name, unique within kind
	parent string // key of the enclosing schema or table
	table  string // enclosing table for table-level objects
	short  string // unqualified name within the table
	props  []objectProperty
}

type objectProperty struct {
	name  string
	value string
}

func (o catalogObject) key() string {
	return objectKey(o.kind, o.name)
}

func objectKey(kind, name string) string {
	return kind + "|" + name
}

// CompareDatabaseMetadata compares expected (production) against actual (the
// generated baseline) and returns every difference in a deterministic order.
func CompareDatabaseMetadata(expected, actual *DatabaseMetadata, opts CompareOptions) *ComparisonResult {
	result := &ComparisonResult{
		MissingExtensions:   []string{},
		TypeMismatches:      []TypeMismatch{},
		ConstraintConflicts: []ConstraintConflict{},
		Warnings:            []string{},
		SchemaDrift:         []SchemaDrift{},
	}

	compareExtensions(expected, actual, opts.Environment, result)

	expectedObjects := flattenMetadata(expected)
	actualObjects := flattenMetadata(actual)
	var environment map[string]catalogObject
	if opts.Environment != nil {
		environment = flattenMetadata(opts.Environment)
	}
	inScope := func(o catalogObject) bool {
		if _, preexisting := environment[o.key()]; preexisting {
			return false
		}
		return len(opts.Schemas) == 0 || slices.Contains(opts.Schemas, o.schema)
	}
	expectedObjects = filterObjects(expectedObjects, inScope)
	actualObjects = filterObjects(actualObjects, inScope)

	keys := make([]string, 0, len(expectedObjects)+len(actualObjects))
	for key := range expectedObjects {
		keys = append(keys, key)
	}
	for key := range actualObjects {
		if _, ok := expectedObjects[key]; !ok {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)

	for _, key := range keys {
		exp, inExpected := expectedObjects[key]
		act, inActual := actualObjects[key]
		switch {
		case inExpected && inActual:
			compareObject(exp, act, result)
		case inActual:
			// Report the outermost missing object only.
			if _, parentOnlyHere := actualObjects[act.parent]; parentOnlyHere && !hasKey(expectedObjects, act.parent) {
				continue
			}
			reportOneSided(act, "missing_in_db", result)
		default:
			if _, parentOnlyHere := expectedObjects[exp.parent]; parentOnlyHere && !hasKey(actualObjects, exp.parent) {
				continue
			}
			reportOneSided(exp, "extra_in_db", result)
		}
	}

	compareColumnOrder(expected, actual, expectedObjects, actualObjects, result)

	result.IsValid = len(result.MissingExtensions) == 0 &&
		len(result.TypeMismatches) == 0 &&
		len(result.ConstraintConflicts) == 0 &&
		len(result.SchemaDrift) == 0
	return result
}

func hasKey(objects map[string]catalogObject, key string) bool {
	_, ok := objects[key]
	return ok
}

func filterObjects(objects map[string]catalogObject, keep func(catalogObject) bool) map[string]catalogObject {
	filtered := make(map[string]catalogObject, len(objects))
	for key, object := range objects {
		if keep(object) {
			filtered[key] = object
		}
	}
	return filtered
}

func compareExtensions(expected, actual, environment *DatabaseMetadata, result *ComparisonResult) {
	preinstalled := func(name string) bool {
		if environment == nil {
			return false
		}
		_, ok := environment.Extensions[name]
		return ok
	}

	for _, name := range sortedKeys(actual.Extensions) {
		if preinstalled(name) {
			continue
		}
		prod, ok := expected.Extensions[name]
		if !ok {
			result.MissingExtensions = append(result.MissingExtensions, name)
			continue
		}
		if prod.Version != actual.Extensions[name].Version {
			result.Warnings = append(result.Warnings, fmt.Sprintf(
				"Extension %s version differs: production %s, baseline %s",
				name, prod.Version, actual.Extensions[name].Version))
		}
	}
	for _, name := range sortedKeys(expected.Extensions) {
		if _, ok := actual.Extensions[name]; ok || preinstalled(name) {
			continue
		}
		result.Warnings = append(result.Warnings, fmt.Sprintf(
			"Extension %s is installed in production but not created by the baseline", name))
	}
}

func compareObject(expected, actual catalogObject, result *ComparisonResult) {
	for i, exp := range expected.props {
		act := actual.props[i]
		if exp.value == act.value {
			continue
		}
		switch {
		case expected.kind == kindColumn && exp.name == "type":
			result.TypeMismatches = append(result.TypeMismatches, TypeMismatch{
				Object:       expected.table,
				Column:       expected.short,
				ExpectedType: exp.value,
				ActualType:   act.value,
				IsBreaking:   true,
			})
		case expected.kind == kindConstraint:
			result.ConstraintConflicts = append(result.ConstraintConflicts, ConstraintConflict{
				Table:          expected.table,
				ConstraintName: expected.short,
				ExpectedDef:    exp.value,
				ActualDef:      act.value,
				ConflictType:   "different",
			})
		default:
			result.SchemaDrift = append(result.SchemaDrift, SchemaDrift{
				Object:     expected.name,
				ObjectType: expected.kind,
				Description: fmt.Sprintf("%s differs: production %s, baseline %s",
					exp.name, displayValue(exp.value), displayValue(act.value)),
				DriftType: "definition_mismatch",
			})
		}
	}
}

func reportOneSided(object catalogObject, driftType string, result *ComparisonResult) {
	if object.kind == kindConstraint {
		conflict := ConstraintConflict{
			Table:          object.table,
			ConstraintName: object.short,
		}
		if driftType == "missing_in_db" {
			conflict.ActualDef = object.props[0].value
			conflict.ConflictType = "extra"
		} else {
			conflict.ExpectedDef = object.props[0].value
			conflict.ConflictType = "missing"
		}
		result.ConstraintConflicts = append(result.ConstraintConflicts, conflict)
		return
	}

	description := fmt.Sprintf("%s %s is created by the baseline but does not exist in production", object.kind, object.name)
	if driftType == "extra_in_db" {
		description = fmt.Sprintf("%s %s exists in production but is not created by the baseline", object.kind, object.name)
	}
	result.SchemaDrift = append(result.SchemaDrift, SchemaDrift{
		Object:      object.name,
		ObjectType:  object.kind,
		Description: description,
		DriftType:   driftType,
	})
}

// compareColumnOrder reports tables whose shared columns appear in a different
// order. Missing and extra columns are already reported individually.
func compareColumnOrder(expected, actual *DatabaseMetadata, expectedObjects, actualObjects map[string]catalogObject, result *ComparisonResult) {
	for _, schemaName := range sortedKeys(expected.Schemas) {
		actualSchema, ok := actual.Schemas[schemaName]
		if !ok {
			continue
		}
		expectedSchema := expected.Schemas[schemaName]
		for _, tableName := range sortedKeys(expectedSchema.Tables) {
			name := schemaName + "." + tableName
			if !hasKey(expectedObjects, objectKey(kindTable, name)) || !hasKey(actualObjects, objectKey(kindTable, name)) {
				continue
			}
			actualTable, ok := actualSchema.Tables[tableName]
			if !ok {
				continue
			}
			expectedOrder, actualOrder := sharedColumnOrder(expectedSchema.Tables[tableName], actualTable)
			if !slices.Equal(expectedOrder, actualOrder) {
				result.SchemaDrift = append(result.SchemaDrift, SchemaDrift{
					Object:     name,
					ObjectType: kindTable,
					Description: fmt.Sprintf("column order differs: production (%s), baseline (%s)",
						strings.Join(expectedOrder, ", "), strings.Join(actualOrder, ", ")),
					DriftType: "definition_mismatch",
				})
			}
		}
	}
}

func sharedColumnOrder(expected, actual *TableMetadata) ([]string, []string) {
	inActual := make(map[string]bool, len(actual.Columns))
	for _, column := range actual.Columns {
		inActual[column.Name] = true
	}
	inExpected := make(map[string]bool, len(expected.Columns))
	expectedOrder := make([]string, 0, len(expected.Columns))
	for _, column := range expected.Columns {
		inExpected[column.Name] = true
		if inActual[column.Name] {
			expectedOrder = append(expectedOrder, column.Name)
		}
	}
	actualOrder := make([]string, 0, len(actual.Columns))
	for _, column := range actual.Columns {
		if inExpected[column.Name] {
			actualOrder = append(actualOrder, column.Name)
		}
	}
	return expectedOrder, actualOrder
}

// flattenMetadata turns a metadata model into comparable objects keyed by kind
// and qualified name. SQL text is whitespace-normalized; identifiers, enum
// labels and role names are compared verbatim.
func flattenMetadata(meta *DatabaseMetadata) map[string]catalogObject {
	objects := make(map[string]catalogObject)
	add := func(o catalogObject) { objects[o.key()] = o }
	sql := schemamodel.NormalizeSQLWhitespace

	for schemaName, schema := range meta.Schemas {
		schemaKey := objectKey(kindSchema, schemaName)
		add(catalogObject{kind: kindSchema, schema: schemaName, name: schemaName})

		for tableName, table := range schema.Tables {
			tableQualified := schemaName + "." + tableName
			tableKey := objectKey(kindTable, tableQualified)
			add(catalogObject{
				kind: kindTable, schema: schemaName, name: tableQualified, parent: schemaKey,
				props: []objectProperty{{"row level security", strconv.FormatBool(table.RowSecurity)}},
			})
			child := func(kind, name string, props ...objectProperty) {
				add(catalogObject{
					kind: kind, schema: schemaName, name: tableQualified + "." + name,
					parent: tableKey, table: tableQualified, short: name, props: props,
				})
			}
			for _, c := range table.Columns {
				child(kindColumn, c.Name,
					objectProperty{"type", c.DataType},
					objectProperty{"nullable", strconv.FormatBool(c.IsNullable)},
					objectProperty{"default", sql(c.DefaultValue)},
					objectProperty{"generation expression", sql(c.GenerationExpr)},
					objectProperty{"identity", c.IdentityGeneration},
					objectProperty{"collation", c.Collation},
				)
			}
			for _, c := range table.Constraints {
				child(kindConstraint, c.Name, objectProperty{"definition", sql(c.Definition)})
			}
			for _, i := range table.Indexes {
				child(kindIndex, i.Name, objectProperty{"definition", sql(i.Definition)})
			}
			for _, t := range table.Triggers {
				child(kindTrigger, t.Name, objectProperty{"definition", sql(t.Definition)})
			}
			for _, p := range table.Policies {
				child(kindPolicy, p.Name,
					objectProperty{"command", p.Command},
					objectProperty{"permissive", strconv.FormatBool(p.Permissive)},
					objectProperty{"roles", strings.Join(p.Roles, ",")},
					objectProperty{"using", sql(p.Using)},
					objectProperty{"with check", sql(p.WithCheck)},
				)
			}
		}

		top := func(kind, name string, props ...objectProperty) {
			add(catalogObject{kind: kind, schema: schemaName, name: schemaName + "." + name, parent: schemaKey, props: props})
		}
		for name, view := range schema.Views {
			top(kindView, name, objectProperty{"definition", sql(view.Definition)})
		}
		for name, view := range schema.MaterializedViews {
			top(kindMaterializedView, name, objectProperty{"definition", sql(view.Definition)})
		}
		for name, overloads := range schema.Functions {
			for _, fn := range overloads {
				top(kindFunction, name+"("+fn.Signature+")", objectProperty{"definition", sql(fn.Body)})
			}
		}
		for name, seq := range schema.Sequences {
			top(kindSequence, name,
				objectProperty{"data type", seq.DataType},
				objectProperty{"start", strconv.FormatInt(seq.Start, 10)},
				objectProperty{"increment", strconv.FormatInt(seq.Increment, 10)},
				objectProperty{"min value", strconv.FormatInt(seq.MinValue, 10)},
				objectProperty{"max value", strconv.FormatInt(seq.MaxValue, 10)},
				objectProperty{"cache", strconv.FormatInt(seq.Cache, 10)},
				objectProperty{"cycle", strconv.FormatBool(seq.Cycle)},
				objectProperty{"owned by", seq.OwnedBy},
			)
		}
		for name, typ := range schema.Types {
			labels := make([]string, len(typ.Elements))
			for i, label := range typ.Elements {
				labels[i] = strconv.Quote(label)
			}
			top(kindType, name,
				objectProperty{"kind", typ.Type},
				objectProperty{"definition", sql(typ.Definition)},
				objectProperty{"enum labels", strings.Join(labels, ", ")},
			)
		}
	}
	return objects
}

func displayValue(value string) string {
	if value == "" {
		return "(none)"
	}
	return value
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
