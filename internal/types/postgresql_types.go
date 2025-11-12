package types

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// PostgreSQLTypeSystem handles PostgreSQL-specific type operations and conversions
type PostgreSQLTypeSystem struct {
	version        string
	customTypes    map[string]*CustomType
	domains        map[string]*Domain
	compositeTypes map[string]*CompositeType
	enumTypes      map[string]*EnumType
	arrayTypes     map[string]*ArrayType
	rangeTypes     map[string]*RangeType
}

// CustomType represents a user-defined type
type CustomType struct {
	Schema      string            `json:"schema"`
	Name        string            `json:"name"`
	BaseType    string            `json:"base_type"`
	Category    TypeCategory      `json:"category"`
	Attributes  map[string]string `json:"attributes"`
	Constraints []string          `json:"constraints"`
}

// Domain represents a PostgreSQL domain
type Domain struct {
	Schema      string       `json:"schema"`
	Name        string       `json:"name"`
	BaseType    string       `json:"base_type"`
	NotNull     bool         `json:"not_null"`
	Default     string       `json:"default"`
	Constraints []Constraint `json:"constraints"`
}

// CompositeType represents a composite (row) type
type CompositeType struct {
	Schema     string      `json:"schema"`
	Name       string      `json:"name"`
	Attributes []Attribute `json:"attributes"`
}

// Attribute represents an attribute of a composite type
type Attribute struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	NotNull  bool   `json:"not_null"`
	Default  string `json:"default"`
	Position int    `json:"position"`
}

// EnumType represents an enumerated type
type EnumType struct {
	Schema string   `json:"schema"`
	Name   string   `json:"name"`
	Values []string `json:"values"`
}

// ArrayType represents an array type
type ArrayType struct {
	Schema      string `json:"schema"`
	Name        string `json:"name"`
	ElementType string `json:"element_type"`
	Dimensions  int    `json:"dimensions"`
}

// RangeType represents a range type
type RangeType struct {
	Schema         string `json:"schema"`
	Name           string `json:"name"`
	SubType        string `json:"sub_type"`
	SubTypeOpClass string `json:"sub_type_op_class"`
	Collation      string `json:"collation"`
	Canonical      string `json:"canonical"`
	SubTypeDiff    string `json:"sub_type_diff"`
}

// Constraint represents a domain constraint
type Constraint struct {
	Name       string `json:"name"`
	Expression string `json:"expression"`
	Validated  bool   `json:"validated"`
}

// TypeCategory categorizes PostgreSQL types
type TypeCategory int

const (
	BaseType TypeCategory = iota
	DomainType
	CompositeTypeCategory
	EnumTypeCategory
	ArrayTypeCategory
	RangeTypeCategory
	PseudoType
)

// TypeCompatibility represents compatibility between types
type TypeCompatibility struct {
	FromType           string             `json:"from_type"`
	ToType             string             `json:"to_type"`
	Compatible         bool               `json:"compatible"`
	RequiresCast       bool               `json:"requires_cast"`
	CastType           CastType           `json:"cast_type"`
	CompatibilityLevel CompatibilityLevel `json:"compatibility_level"`
	Warnings           []string           `json:"warnings"`
}

// CastType defines the type of cast required
type CastType int

const (
	NoCast CastType = iota
	ImplicitCast
	AssignmentCast
	ExplicitCast
)

// CompatibilityLevel defines the level of compatibility
type CompatibilityLevel int

const (
	FullyCompatible CompatibilityLevel = iota
	MostlyCompatible
	PartiallyCompatible
	Incompatible
)

// NewPostgreSQLTypeSystem creates a new PostgreSQL type system
func NewPostgreSQLTypeSystem(version string) *PostgreSQLTypeSystem {
	return &PostgreSQLTypeSystem{
		version:        version,
		customTypes:    make(map[string]*CustomType),
		domains:        make(map[string]*Domain),
		compositeTypes: make(map[string]*CompositeType),
		enumTypes:      make(map[string]*EnumType),
		arrayTypes:     make(map[string]*ArrayType),
		rangeTypes:     make(map[string]*RangeType),
	}
}

// RegisterCustomType registers a custom type in the system
func (pts *PostgreSQLTypeSystem) RegisterCustomType(customType *CustomType) {
	key := fmt.Sprintf("%s.%s", customType.Schema, customType.Name)
	pts.customTypes[key] = customType
}

// RegisterDomain registers a domain in the system
func (pts *PostgreSQLTypeSystem) RegisterDomain(domain *Domain) {
	key := fmt.Sprintf("%s.%s", domain.Schema, domain.Name)
	pts.domains[key] = domain
}

// RegisterCompositeType registers a composite type in the system
func (pts *PostgreSQLTypeSystem) RegisterCompositeType(compositeType *CompositeType) {
	key := fmt.Sprintf("%s.%s", compositeType.Schema, compositeType.Name)
	pts.compositeTypes[key] = compositeType
}

// RegisterEnumType registers an enum type in the system
func (pts *PostgreSQLTypeSystem) RegisterEnumType(enumType *EnumType) {
	key := fmt.Sprintf("%s.%s", enumType.Schema, enumType.Name)
	pts.enumTypes[key] = enumType
}

// IsBuiltinType checks if a type is a PostgreSQL built-in type
func (pts *PostgreSQLTypeSystem) IsBuiltinType(typeName string) bool {
	builtinTypes := map[string]bool{
		// Numeric types
		"smallint": true, "integer": true, "bigint": true,
		"decimal": true, "numeric": true,
		"real": true, "double precision": true,
		"smallserial": true, "serial": true, "bigserial": true,

		// Character types
		"character varying": true, "varchar": true,
		"character": true, "char": true,
		"text": true,

		// Binary types
		"bytea": true,

		// Date/time types
		"timestamp": true, "timestamp without time zone": true,
		"timestamp with time zone": true, "timestamptz": true,
		"date": true, "time": true,
		"time without time zone": true, "time with time zone": true,
		"interval": true,

		// Boolean type
		"boolean": true, "bool": true,

		// Geometric types
		"point": true, "line": true, "lseg": true,
		"box": true, "path": true, "polygon": true, "circle": true,

		// Network address types
		"cidr": true, "inet": true, "macaddr": true, "macaddr8": true,

		// Bit string types
		"bit": true, "bit varying": true, "varbit": true,

		// Text search types
		"tsvector": true, "tsquery": true,

		// UUID type
		"uuid": true,

		// XML type
		"xml": true,

		// JSON types
		"json": true, "jsonb": true,

		// Money type
		"money": true,

		// OID types
		"oid": true, "regproc": true, "regprocedure": true,
		"regoper": true, "regoperator": true, "regclass": true,
		"regtype": true, "regconfig": true, "regdictionary": true,

		// Pseudo types
		"any": true, "anyelement": true, "anyarray": true,
		"anynonarray": true, "anyenum": true, "anyrange": true,
		"cstring": true, "internal": true, "language_handler": true,
		"fdw_handler": true, "record": true, "trigger": true, "void": true,
	}

	// Normalize type name
	normalized := strings.ToLower(strings.TrimSpace(typeName))
	return builtinTypes[normalized]
}

// ParseArrayType parses an array type specification
func (pts *PostgreSQLTypeSystem) ParseArrayType(typeSpec string) (*ArrayType, error) {
	// Handle array syntax: type[], type[size], type[][], etc.
	arrayPattern := regexp.MustCompile(`^(.+?)(\[\d*\])+$`)
	matches := arrayPattern.FindStringSubmatch(typeSpec)

	if len(matches) < 3 {
		return nil, errors.NewTypeError(errors.ErrorCodeArraySpecError, "invalid array type specification", typeSpec)
	}

	elementType := strings.TrimSpace(matches[1])
	dimensionSpec := matches[2]

	// Count dimensions
	dimensions := strings.Count(dimensionSpec, "[")

	return &ArrayType{
		Name:        typeSpec,
		ElementType: elementType,
		Dimensions:  dimensions,
	}, nil
}

// CheckTypeCompatibility checks compatibility between two types
func (pts *PostgreSQLTypeSystem) CheckTypeCompatibility(fromType, toType string) *TypeCompatibility {
	compatibility := &TypeCompatibility{
		FromType:           fromType,
		ToType:             toType,
		Compatible:         false,
		RequiresCast:       false,
		CastType:           NoCast,
		CompatibilityLevel: Incompatible,
		Warnings:           make([]string, 0),
	}

	// Normalize type names
	fromNorm := pts.normalizeTypeName(fromType)
	toNorm := pts.normalizeTypeName(toType)

	// Same type
	if fromNorm == toNorm {
		compatibility.Compatible = true
		compatibility.CompatibilityLevel = FullyCompatible
		return compatibility
	}

	// Check implicit conversions
	if pts.hasImplicitConversion(fromNorm, toNorm) {
		compatibility.Compatible = true
		compatibility.RequiresCast = false
		compatibility.CastType = ImplicitCast
		compatibility.CompatibilityLevel = FullyCompatible
		return compatibility
	}

	// Check assignment conversions
	if pts.hasAssignmentConversion(fromNorm, toNorm) {
		compatibility.Compatible = true
		compatibility.RequiresCast = false
		compatibility.CastType = AssignmentCast
		compatibility.CompatibilityLevel = MostlyCompatible
		return compatibility
	}

	// Check explicit conversions
	if pts.hasExplicitConversion(fromNorm, toNorm) {
		compatibility.Compatible = true
		compatibility.RequiresCast = true
		compatibility.CastType = ExplicitCast
		compatibility.CompatibilityLevel = PartiallyCompatible

		// Add warnings for potentially lossy conversions
		if pts.isPotentiallyLossyConversion(fromNorm, toNorm) {
			compatibility.Warnings = append(compatibility.Warnings,
				"This conversion may result in data loss")
		}

		return compatibility
	}

	// Check for special cases
	compatibility = pts.checkSpecialCases(fromNorm, toNorm, compatibility)

	return compatibility
}

// normalizeTypeName normalizes PostgreSQL type names
func (pts *PostgreSQLTypeSystem) normalizeTypeName(typeName string) string {
	// Remove whitespace and convert to lowercase
	normalized := strings.ToLower(strings.TrimSpace(typeName))

	// Handle type aliases
	aliases := map[string]string{
		"int":     "integer",
		"int4":    "integer",
		"int2":    "smallint",
		"int8":    "bigint",
		"float4":  "real",
		"float8":  "double precision",
		"bool":    "boolean",
		"varchar": "character varying",
		"char":    "character",
	}

	if alias, exists := aliases[normalized]; exists {
		return alias
	}

	// Handle precision specifications (remove for compatibility checking)
	precisionPattern := regexp.MustCompile(`^(varchar|character varying|char|character|numeric|decimal|time|timestamp|interval|bit)\s*\(\d+(?:,\d+)?\)$`)
	if precisionPattern.MatchString(normalized) {
		// Extract base type
		parts := strings.Split(normalized, "(")
		return strings.TrimSpace(parts[0])
	}

	return normalized
}

// hasImplicitConversion checks for implicit type conversions
func (pts *PostgreSQLTypeSystem) hasImplicitConversion(from, to string) bool {
	implicitConversions := map[string][]string{
		"smallint":                    {"integer", "bigint", "numeric", "real", "double precision"},
		"integer":                     {"bigint", "numeric", "real", "double precision"},
		"bigint":                      {"numeric", "real", "double precision"},
		"real":                        {"double precision"},
		"numeric":                     {"real", "double precision"},
		"character":                   {"character varying", "text"},
		"character varying":           {"text"},
		"date":                        {"timestamp", "timestamp without time zone"},
		"timestamp without time zone": {"timestamp with time zone"},
	}

	conversions, exists := implicitConversions[from]
	if !exists {
		return false
	}

	for _, target := range conversions {
		if target == to {
			return true
		}
	}

	return false
}

// hasAssignmentConversion checks for assignment type conversions
func (pts *PostgreSQLTypeSystem) hasAssignmentConversion(from, to string) bool {
	assignmentConversions := map[string][]string{
		"bigint":                      {"integer", "smallint"},
		"integer":                     {"smallint"},
		"double precision":            {"real", "numeric"},
		"real":                        {"numeric"},
		"text":                        {"character varying", "character"},
		"character varying":           {"character"},
		"timestamp with time zone":    {"timestamp without time zone", "date"},
		"timestamp without time zone": {"date"},
	}

	conversions, exists := assignmentConversions[from]
	if !exists {
		return false
	}

	for _, target := range conversions {
		if target == to {
			return true
		}
	}

	return false
}

// hasExplicitConversion checks for explicit type conversions
func (pts *PostgreSQLTypeSystem) hasExplicitConversion(from, to string) bool {
	// Most types can be explicitly converted to text
	if to == "text" || to == "character varying" {
		return true
	}

	// Text can be explicitly converted to most types (with potential errors)
	if from == "text" || from == "character varying" {
		return pts.IsBuiltinType(to)
	}

	// Numeric type conversions
	numericTypes := []string{"smallint", "integer", "bigint", "numeric", "real", "double precision"}
	fromIsNumeric := pts.isInSlice(from, numericTypes)
	toIsNumeric := pts.isInSlice(to, numericTypes)

	if fromIsNumeric && toIsNumeric {
		return true
	}

	// Date/time conversions
	dateTimeTypes := []string{"date", "timestamp", "timestamp without time zone", "timestamp with time zone", "time", "time without time zone", "time with time zone"}
	fromIsDateTime := pts.isInSlice(from, dateTimeTypes)
	toIsDateTime := pts.isInSlice(to, dateTimeTypes)

	if fromIsDateTime && toIsDateTime {
		return true
	}

	return false
}

// isPotentiallyLossyConversion checks if a conversion might lose data
func (pts *PostgreSQLTypeSystem) isPotentiallyLossyConversion(from, to string) bool {
	lossyConversions := map[string][]string{
		"bigint":                   {"integer", "smallint"},
		"integer":                  {"smallint"},
		"double precision":         {"real"},
		"numeric":                  {"integer", "smallint", "bigint"},
		"text":                     {"character varying", "character"},
		"timestamp with time zone": {"timestamp without time zone"},
		"timestamp":                {"date"},
	}

	conversions, exists := lossyConversions[from]
	if !exists {
		return false
	}

	return pts.isInSlice(to, conversions)
}

// checkSpecialCases handles special compatibility cases
func (pts *PostgreSQLTypeSystem) checkSpecialCases(from, to string, compatibility *TypeCompatibility) *TypeCompatibility {
	// Array type compatibility
	if strings.Contains(from, "[]") && strings.Contains(to, "[]") {
		fromElement := strings.ReplaceAll(from, "[]", "")
		toElement := strings.ReplaceAll(to, "[]", "")

		elementCompatibility := pts.CheckTypeCompatibility(fromElement, toElement)
		if elementCompatibility.Compatible {
			compatibility.Compatible = true
			compatibility.RequiresCast = elementCompatibility.RequiresCast
			compatibility.CastType = elementCompatibility.CastType
			compatibility.CompatibilityLevel = elementCompatibility.CompatibilityLevel
			compatibility.Warnings = append(compatibility.Warnings, "Array element type conversion")
		}
	}

	// JSON/JSONB compatibility
	if (from == "json" && to == "jsonb") || (from == "jsonb" && to == "json") {
		compatibility.Compatible = true
		compatibility.RequiresCast = true
		compatibility.CastType = ExplicitCast
		compatibility.CompatibilityLevel = MostlyCompatible
	}

	return compatibility
}

// isInSlice checks if a string is in a slice
func (pts *PostgreSQLTypeSystem) isInSlice(str string, slice []string) bool {
	for _, item := range slice {
		if item == str {
			return true
		}
	}
	return false
}

// GetTypeSize estimates the storage size of a type
func (pts *PostgreSQLTypeSystem) GetTypeSize(typeName string) (int, error) {
	normalized := pts.normalizeTypeName(typeName)

	typeSizes := map[string]int{
		"boolean":                     1,
		"smallint":                    2,
		"integer":                     4,
		"bigint":                      8,
		"real":                        4,
		"double precision":            8,
		"numeric":                     -1, // Variable
		"character":                   -1, // Variable
		"character varying":           -1, // Variable
		"text":                        -1, // Variable
		"bytea":                       -1, // Variable
		"timestamp":                   8,
		"timestamp without time zone": 8,
		"timestamp with time zone":    8,
		"date":                        4,
		"time":                        8,
		"interval":                    16,
		"uuid":                        16,
		"json":                        -1, // Variable
		"jsonb":                       -1, // Variable
		"point":                       16,
		"box":                         32,
		"circle":                      24,
		"inet":                        -1, // Variable (7-19 bytes)
		"cidr":                        -1, // Variable (7-19 bytes)
		"macaddr":                     6,
		"macaddr8":                    8,
	}

	if size, exists := typeSizes[normalized]; exists {
		return size, nil
	}

	// Handle precision specifications
	if strings.Contains(typeName, "(") {
		baseType := strings.Split(normalized, "(")[0]
		if size, exists := typeSizes[baseType]; exists {
			if size == -1 {
				// Try to extract size from specification
				return pts.extractSizeFromSpec(typeName)
			}
			return size, nil
		}
	}

	return -1, errors.NewTypeError(errors.ErrorCodeTypeNotFound, "unknown type", typeName)
}

// extractSizeFromSpec extracts size information from type specification
func (pts *PostgreSQLTypeSystem) extractSizeFromSpec(typeSpec string) (int, error) {
	// Extract size from varchar(n), char(n), numeric(p,s), etc.
	pattern := regexp.MustCompile(`\((\d+)(?:,\d+)?\)`)
	matches := pattern.FindStringSubmatch(typeSpec)

	if len(matches) > 1 {
		size, err := strconv.Atoi(matches[1])
		if err != nil {
			return -1, err
		}
		return size, nil
	}

	return -1, errors.NewTypeError(errors.ErrorCodeSizeExtractionError, "cannot extract size from type specification", typeSpec)
}

// ValidateEnumValue validates that a value is valid for an enum type
func (pts *PostgreSQLTypeSystem) ValidateEnumValue(enumTypeName, value string) error {
	enumType, exists := pts.enumTypes[enumTypeName]
	if !exists {
		return errors.NewTypeError(errors.ErrorCodeTypeNotFound, "enum type not found", enumTypeName)
	}

	for _, validValue := range enumType.Values {
		if validValue == value {
			return nil
		}
	}

	return errors.NewTypeError(errors.ErrorCodeEnumValidationError, "value is not valid for enum type", enumTypeName).WithAdditional("value", value).WithAdditional("valid_values", enumType.Values)
}

// GetCompositeTypeAttributes returns the attributes of a composite type
func (pts *PostgreSQLTypeSystem) GetCompositeTypeAttributes(typeName string) ([]Attribute, error) {
	compositeType, exists := pts.compositeTypes[typeName]
	if !exists {
		return nil, errors.NewTypeError(errors.ErrorCodeCompositeTypeError, "composite type not found", typeName)
	}

	return compositeType.Attributes, nil
}

// IsCompatibleArrayDimensions checks if array dimensions are compatible
func (pts *PostgreSQLTypeSystem) IsCompatibleArrayDimensions(from, to *ArrayType) bool {
	// PostgreSQL allows assignment between different array dimensions in some cases
	// Generally, you can assign a lower-dimensional array to a higher-dimensional one
	return from.Dimensions <= to.Dimensions
}
