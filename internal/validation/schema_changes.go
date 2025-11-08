package validation

import (
	"fmt"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/builder"
)

// TableCreateChange represents a table creation
type TableCreateChange struct {
	ID         ObjectID
	Definition *TableDefinition
}

func (tcc *TableCreateChange) Type() ChangeType {
	return ChangeTypeCreate
}

func (tcc *TableCreateChange) Risk() RiskLevel {
	return RiskLevelSafe
}

func (tcc *TableCreateChange) Description() string {
	return fmt.Sprintf("Create table %s", tcc.ID.Name)
}

func (tcc *TableCreateChange) SQL() []string {
	b := builder.NewSQLBuilder(builder.DefaultBuildOptions())
	tableDef := convertToBuilderTableDef(tcc.Definition)
	return []string{b.CreateTable(tableDef).String()}
}

func (tcc *TableCreateChange) ObjectID() ObjectID {
	return tcc.ID
}

func (tcc *TableCreateChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"table":        tcc.Definition,
		"column_count": len(tcc.Definition.Columns),
	}
}

// TableDropChange represents a table drop
type TableDropChange struct {
	ID         ObjectID
	Definition *TableDefinition
}

func (tdc *TableDropChange) Type() ChangeType {
	return ChangeTypeDrop
}

func (tdc *TableDropChange) Risk() RiskLevel {
	return RiskLevelCritical
}

func (tdc *TableDropChange) Description() string {
	return fmt.Sprintf("Drop table %s", tdc.ID.Name)
}

func (tdc *TableDropChange) SQL() []string {
	return []string{fmt.Sprintf("DROP TABLE %s", tdc.ID.Name)}
}

func (tdc *TableDropChange) ObjectID() ObjectID {
	return tdc.ID
}

func (tdc *TableDropChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"table":        tdc.Definition,
		"column_count": len(tdc.Definition.Columns),
		"has_data":     true, // Assume table has data
	}
}

// ColumnAddChange represents adding a column
type ColumnAddChange struct {
	ID         ObjectID
	ColumnName string
	Definition *Column
}

func (cac *ColumnAddChange) Type() ChangeType {
	return ChangeTypeAlter
}

func (cac *ColumnAddChange) Risk() RiskLevel {
	if cac.Definition.IsNullable || cac.Definition.DefaultValue != "" {
		return RiskLevelLow
	}
	return RiskLevelMedium // Adding NOT NULL column without default
}

func (cac *ColumnAddChange) Description() string {
	return fmt.Sprintf("Add column %s to table %s", cac.ColumnName, cac.ID.Name)
}

func (cac *ColumnAddChange) SQL() []string {
	sql := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
		cac.ID.Name, cac.ColumnName, cac.Definition.DataType)

	if !cac.Definition.IsNullable {
		sql += " NOT NULL"
	}

	if cac.Definition.DefaultValue != "" {
		sql += fmt.Sprintf(" DEFAULT %s", cac.Definition.DefaultValue)
	}

	return []string{sql}
}

func (cac *ColumnAddChange) ObjectID() ObjectID {
	return cac.ID
}

func (cac *ColumnAddChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"column":      cac.Definition,
		"column_name": cac.ColumnName,
		"nullable":    cac.Definition.IsNullable,
		"has_default": cac.Definition.DefaultValue != "",
	}
}

// ColumnDropChange represents dropping a column
type ColumnDropChange struct {
	ID         ObjectID
	ColumnName string
	Definition *Column
}

func (cdc *ColumnDropChange) Type() ChangeType {
	return ChangeTypeDrop
}

func (cdc *ColumnDropChange) Risk() RiskLevel {
	return RiskLevelHigh // Dropping column loses data
}

func (cdc *ColumnDropChange) Description() string {
	return fmt.Sprintf("Drop column %s from table %s", cdc.ColumnName, cdc.ID.Name)
}

func (cdc *ColumnDropChange) SQL() []string {
	return []string{
		fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", cdc.ID.Name, cdc.ColumnName),
	}
}

func (cdc *ColumnDropChange) ObjectID() ObjectID {
	return cdc.ID
}

func (cdc *ColumnDropChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"column":      cdc.Definition,
		"column_name": cdc.ColumnName,
		"data_loss":   true,
	}
}

// ColumnModifyChange represents modifying a column
type ColumnModifyChange struct {
	ID             ObjectID
	ColumnName     string
	FromDefinition *Column
	ToDefinition   *Column
}

func (cmc *ColumnModifyChange) Type() ChangeType {
	return ChangeTypeModify
}

func (cmc *ColumnModifyChange) Risk() RiskLevel {
	// Analyze the specific changes to determine risk
	if cmc.FromDefinition.DataType != cmc.ToDefinition.DataType {
		return RiskLevelHigh // Type change
	}
	if cmc.FromDefinition.IsNullable && !cmc.ToDefinition.IsNullable {
		return RiskLevelMedium // Making column NOT NULL
	}
	if !cmc.FromDefinition.IsNullable && cmc.ToDefinition.IsNullable {
		return RiskLevelLow // Making column nullable
	}
	return RiskLevelLow
}

func (cmc *ColumnModifyChange) Description() string {
	changes := []string{}

	if cmc.FromDefinition.DataType != cmc.ToDefinition.DataType {
		changes = append(changes, fmt.Sprintf("type %s → %s",
			cmc.FromDefinition.DataType, cmc.ToDefinition.DataType))
	}

	if cmc.FromDefinition.IsNullable != cmc.ToDefinition.IsNullable {
		if cmc.ToDefinition.IsNullable {
			changes = append(changes, "nullable")
		} else {
			changes = append(changes, "not null")
		}
	}

	if cmc.FromDefinition.DefaultValue != cmc.ToDefinition.DefaultValue {
		changes = append(changes, "default value")
	}

	return fmt.Sprintf("Modify column %s.%s (%s)",
		cmc.ID.Name, cmc.ColumnName, strings.Join(changes, ", "))
}

func (cmc *ColumnModifyChange) SQL() []string {
	var statements []string

	// Type change
	if cmc.FromDefinition.DataType != cmc.ToDefinition.DataType {
		statements = append(statements,
			fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s TYPE %s",
				cmc.ID.Name, cmc.ColumnName, cmc.ToDefinition.DataType))
	}

	// Nullable change
	if cmc.FromDefinition.IsNullable != cmc.ToDefinition.IsNullable {
		if cmc.ToDefinition.IsNullable {
			statements = append(statements,
				fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP NOT NULL",
					cmc.ID.Name, cmc.ColumnName))
		} else {
			statements = append(statements,
				fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL",
					cmc.ID.Name, cmc.ColumnName))
		}
	}

	// Default value change
	if cmc.FromDefinition.DefaultValue != cmc.ToDefinition.DefaultValue {
		if cmc.ToDefinition.DefaultValue == "" {
			statements = append(statements,
				fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s DROP DEFAULT",
					cmc.ID.Name, cmc.ColumnName))
		} else {
			statements = append(statements,
				fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s",
					cmc.ID.Name, cmc.ColumnName, cmc.ToDefinition.DefaultValue))
		}
	}

	return statements
}

func (cmc *ColumnModifyChange) ObjectID() ObjectID {
	return cmc.ID
}

func (cmc *ColumnModifyChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"column_name":     cmc.ColumnName,
		"from_definition": cmc.FromDefinition,
		"to_definition":   cmc.ToDefinition,
		"type_change":     cmc.FromDefinition.DataType != cmc.ToDefinition.DataType,
		"nullable_change": cmc.FromDefinition.IsNullable != cmc.ToDefinition.IsNullable,
		"default_change":  cmc.FromDefinition.DefaultValue != cmc.ToDefinition.DefaultValue,
	}
}

// IndexCreateChange represents index creation
type IndexCreateChange struct {
	ID         ObjectID
	Definition *IndexDefinition
}

func (icc *IndexCreateChange) Type() ChangeType {
	return ChangeTypeCreate
}

func (icc *IndexCreateChange) Risk() RiskLevel {
	if icc.Definition.IsUnique {
		return RiskLevelMedium // Unique index might fail if data violates constraint
	}
	return RiskLevelLow
}

func (icc *IndexCreateChange) Description() string {
	indexType := "index"
	if icc.Definition.IsUnique {
		indexType = "unique index"
	}
	if icc.Definition.IsPrimary {
		indexType = "primary key"
	}
	return fmt.Sprintf("Create %s %s on %s", indexType, icc.ID.Name, icc.Definition.Table.Name)
}

func (icc *IndexCreateChange) SQL() []string {
	b := builder.NewSQLBuilder(builder.DefaultBuildOptions())
	indexDef := convertToBuilderIndexDef(icc.Definition)
	return []string{b.CreateIndex(indexDef).String()}
}

func (icc *IndexCreateChange) ObjectID() ObjectID {
	return icc.ID
}

func (icc *IndexCreateChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"index":     icc.Definition,
		"is_unique": icc.Definition.IsUnique,
		"method":    icc.Definition.Method,
		"columns":   icc.Definition.Columns,
	}
}

// IndexDropChange represents index drop
type IndexDropChange struct {
	ID         ObjectID
	Definition *IndexDefinition
}

func (idc *IndexDropChange) Type() ChangeType {
	return ChangeTypeDrop
}

func (idc *IndexDropChange) Risk() RiskLevel {
	if idc.Definition.IsPrimary || idc.Definition.IsUnique {
		return RiskLevelHigh // Dropping unique/primary key affects constraints
	}
	return RiskLevelLow
}

func (idc *IndexDropChange) Description() string {
	return fmt.Sprintf("Drop index %s", idc.ID.Name)
}

func (idc *IndexDropChange) SQL() []string {
	return []string{fmt.Sprintf("DROP INDEX %s", idc.ID.Name)}
}

func (idc *IndexDropChange) ObjectID() ObjectID {
	return idc.ID
}

func (idc *IndexDropChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"index":      idc.Definition,
		"is_unique":  idc.Definition.IsUnique,
		"is_primary": idc.Definition.IsPrimary,
	}
}

// IndexModifyChange represents index modification
type IndexModifyChange struct {
	ID             ObjectID
	FromDefinition *IndexDefinition
	ToDefinition   *IndexDefinition
}

func (imc *IndexModifyChange) Type() ChangeType {
	return ChangeTypeRecreate
}

func (imc *IndexModifyChange) Risk() RiskLevel {
	return RiskLevelMedium
}

func (imc *IndexModifyChange) Description() string {
	return fmt.Sprintf("Recreate index %s", imc.ID.Name)
}

func (imc *IndexModifyChange) SQL() []string {
	b := builder.NewSQLBuilder(builder.DefaultBuildOptions())
	indexDef := convertToBuilderIndexDef(imc.ToDefinition)
	return []string{
		fmt.Sprintf("DROP INDEX %s", imc.ID.Name),
		b.Reset().CreateIndex(indexDef).String(),
	}
}

func (imc *IndexModifyChange) ObjectID() ObjectID {
	return imc.ID
}

func (imc *IndexModifyChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"from_definition": imc.FromDefinition,
		"to_definition":   imc.ToDefinition,
	}
}

// FunctionCreateChange represents function creation
type FunctionCreateChange struct {
	ID         ObjectID
	Definition *FunctionDefinition
}

func (fcc *FunctionCreateChange) Type() ChangeType {
	return ChangeTypeCreate
}

func (fcc *FunctionCreateChange) Risk() RiskLevel {
	return RiskLevelSafe
}

func (fcc *FunctionCreateChange) Description() string {
	return fmt.Sprintf("Create function %s", fcc.ID.QualifiedName())
}

func (fcc *FunctionCreateChange) SQL() []string {
	b := builder.NewSQLBuilder(builder.DefaultBuildOptions())
	funcDef := convertToBuilderFunctionDef(fcc.Definition)
	return []string{b.CreateFunction(funcDef).String()}
}

func (fcc *FunctionCreateChange) ObjectID() ObjectID {
	return fcc.ID
}

func (fcc *FunctionCreateChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"function":    fcc.Definition,
		"language":    fcc.Definition.Language,
		"return_type": fcc.Definition.ReturnType,
		"param_count": len(fcc.Definition.Parameters),
	}
}

// FunctionDropChange represents function drop
type FunctionDropChange struct {
	ID         ObjectID
	Definition *FunctionDefinition
}

func (fdc *FunctionDropChange) Type() ChangeType {
	return ChangeTypeDrop
}

func (fdc *FunctionDropChange) Risk() RiskLevel {
	return RiskLevelMedium // Dropping function might break dependencies
}

func (fdc *FunctionDropChange) Description() string {
	return fmt.Sprintf("Drop function %s", fdc.ID.QualifiedName())
}

func (fdc *FunctionDropChange) SQL() []string {
	return []string{fmt.Sprintf("DROP FUNCTION %s", fdc.ID.QualifiedName())}
}

func (fdc *FunctionDropChange) ObjectID() ObjectID {
	return fdc.ID
}

func (fdc *FunctionDropChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"function": fdc.Definition,
	}
}

// FunctionModifyChange represents function modification
type FunctionModifyChange struct {
	ID             ObjectID
	FromDefinition *FunctionDefinition
	ToDefinition   *FunctionDefinition
}

func (fmc *FunctionModifyChange) Type() ChangeType {
	return ChangeTypeRecreate
}

func (fmc *FunctionModifyChange) Risk() RiskLevel {
	// Signature changes are high risk
	if len(fmc.FromDefinition.Parameters) != len(fmc.ToDefinition.Parameters) ||
		fmc.FromDefinition.ReturnType != fmc.ToDefinition.ReturnType {
		return RiskLevelHigh
	}
	return RiskLevelMedium
}

func (fmc *FunctionModifyChange) Description() string {
	return fmt.Sprintf("Recreate function %s", fmc.ID.QualifiedName())
}

func (fmc *FunctionModifyChange) SQL() []string {
	b := builder.NewSQLBuilder(builder.DefaultBuildOptions())
	funcDef := convertToBuilderFunctionDef(fmc.ToDefinition)
	return []string{
		fmt.Sprintf("DROP FUNCTION %s", fmc.ID.QualifiedName()),
		b.Reset().CreateFunction(funcDef).String(),
	}
}

func (fmc *FunctionModifyChange) ObjectID() ObjectID {
	return fmc.ID
}

func (fmc *FunctionModifyChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"from_definition": fmc.FromDefinition,
		"to_definition":   fmc.ToDefinition,
		"signature_change": len(fmc.FromDefinition.Parameters) != len(fmc.ToDefinition.Parameters) ||
			fmc.FromDefinition.ReturnType != fmc.ToDefinition.ReturnType,
	}
}

// ConstraintAddChange represents constraint addition
type ConstraintAddChange struct {
	ID             ObjectID
	ConstraintName string
	Definition     *Constraint
}

func (cac *ConstraintAddChange) Type() ChangeType {
	return ChangeTypeAlter
}

func (cac *ConstraintAddChange) Risk() RiskLevel {
	switch cac.Definition.Type {
	case "CHECK", "UNIQUE", "PRIMARY KEY":
		return RiskLevelMedium // Might fail if data violates constraint
	case "FOREIGN KEY":
		return RiskLevelHigh // Might fail if referential integrity violated
	default:
		return RiskLevelLow
	}
}

func (cac *ConstraintAddChange) Description() string {
	return fmt.Sprintf("Add %s constraint %s to %s",
		cac.Definition.Type, cac.ConstraintName, cac.ID.Name)
}

func (cac *ConstraintAddChange) SQL() []string {
	return []string{fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s",
		cac.ID.Name, cac.ConstraintName, formatConstraintDef(cac.Definition))}
}

func (cac *ConstraintAddChange) ObjectID() ObjectID {
	return cac.ID
}

func (cac *ConstraintAddChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"constraint":      cac.Definition,
		"constraint_name": cac.ConstraintName,
		"constraint_type": cac.Definition.Type,
	}
}

// ConstraintDropChange represents constraint drop
type ConstraintDropChange struct {
	ID             ObjectID
	ConstraintName string
	Definition     *Constraint
}

func (cdc *ConstraintDropChange) Type() ChangeType {
	return ChangeTypeDrop
}

func (cdc *ConstraintDropChange) Risk() RiskLevel {
	switch cdc.Definition.Type {
	case "PRIMARY KEY", "UNIQUE":
		return RiskLevelHigh // Removing uniqueness constraints
	case "FOREIGN KEY":
		return RiskLevelMedium // Removing referential integrity
	case "CHECK":
		return RiskLevelLow // Removing data validation
	default:
		return RiskLevelLow
	}
}

func (cdc *ConstraintDropChange) Description() string {
	return fmt.Sprintf("Drop %s constraint %s from %s",
		cdc.Definition.Type, cdc.ConstraintName, cdc.ID.Name)
}

func (cdc *ConstraintDropChange) SQL() []string {
	return []string{
		fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s",
			cdc.ID.Name, cdc.ConstraintName),
	}
}

func (cdc *ConstraintDropChange) ObjectID() ObjectID {
	return cdc.ID
}

func (cdc *ConstraintDropChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"constraint":      cdc.Definition,
		"constraint_name": cdc.ConstraintName,
		"constraint_type": cdc.Definition.Type,
	}
}

// ConstraintModifyChange represents constraint modification
type ConstraintModifyChange struct {
	ID             ObjectID
	ConstraintName string
	FromDefinition *Constraint
	ToDefinition   *Constraint
}

func (cmc *ConstraintModifyChange) Type() ChangeType {
	return ChangeTypeRecreate
}

func (cmc *ConstraintModifyChange) Risk() RiskLevel {
	return RiskLevelMedium
}

func (cmc *ConstraintModifyChange) Description() string {
	return fmt.Sprintf("Recreate %s constraint %s on %s",
		cmc.ToDefinition.Type, cmc.ConstraintName, cmc.ID.Name)
}

func (cmc *ConstraintModifyChange) SQL() []string {
	return []string{
		fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT %s",
			cmc.ID.Name, cmc.ConstraintName),
		fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s %s",
			cmc.ID.Name, cmc.ConstraintName, formatConstraintDef(cmc.ToDefinition)),
	}
}

func (cmc *ConstraintModifyChange) ObjectID() ObjectID {
	return cmc.ID
}

func (cmc *ConstraintModifyChange) Details() map[string]interface{} {
	return map[string]interface{}{
		"constraint_name": cmc.ConstraintName,
		"from_definition": cmc.FromDefinition,
		"to_definition":   cmc.ToDefinition,
	}
}

// Conversion functions to builder types

func convertToBuilderTableDef(table *TableDefinition) *builder.TableDefinition {
	columns := make([]*builder.ColumnDefinition, 0, len(table.Columns))
	for colName, col := range table.Columns {
		columns = append(columns, &builder.ColumnDefinition{
			Name:     colName,
			DataType: col.DataType,
			NotNull:  !col.IsNullable,
			Default:  col.DefaultValue,
		})
	}

	return &builder.TableDefinition{
		Schema:  "", // Will be set by caller if needed
		Name:    table.ID.Name,
		Columns: columns,
	}
}

func convertToBuilderIndexDef(index *IndexDefinition) *builder.IndexDefinition {
	columns := make([]*builder.IndexColumn, len(index.Columns))
	for i, colName := range index.Columns {
		columns[i] = &builder.IndexColumn{
			Name: colName,
		}
	}

	return &builder.IndexDefinition{
		Schema:  "",
		Table:   index.Table.Name,
		Name:    index.ID.Name,
		Unique:  index.IsUnique,
		Method:  strings.ToUpper(index.Method),
		Columns: columns,
		Where:   index.Where,
	}
}

func convertToBuilderFunctionDef(function *FunctionDefinition) *builder.FunctionDefinition {
	params := make([]*builder.ParameterDefinition, len(function.Parameters))
	for i, param := range function.Parameters {
		params[i] = &builder.ParameterDefinition{
			Name:    param.Name,
			Type:    param.DataType,
			Default: param.Default,
		}
	}

	return &builder.FunctionDefinition{
		Schema:     "",
		Name:       function.ID.Name,
		Parameters: params,
		ReturnType: function.ReturnType,
		Language:   function.Language,
		Body:       fmt.Sprintf("$$\n%s\n$$", function.Body),
		Volatility: function.Volatility,
		Security:   function.Security,
	}
}

func formatConstraintDef(constraint *Constraint) string {
	switch constraint.Type {
	case "PRIMARY KEY":
		return fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(constraint.Columns, ", "))
	case "UNIQUE":
		return fmt.Sprintf("UNIQUE (%s)", strings.Join(constraint.Columns, ", "))
	case "CHECK":
		return fmt.Sprintf("CHECK (%s)", constraint.Expression)
	case "FOREIGN KEY":
		sql := fmt.Sprintf("FOREIGN KEY (%s) REFERENCES %s (%s)",
			strings.Join(constraint.Columns, ", "),
			constraint.RefTable,
			strings.Join(constraint.RefColumns, ", "))
		if constraint.OnUpdate != "" {
			sql += fmt.Sprintf(" ON UPDATE %s", constraint.OnUpdate)
		}
		if constraint.OnDelete != "" {
			sql += fmt.Sprintf(" ON DELETE %s", constraint.OnDelete)
		}
		return sql
	default:
		return ""
	}
}
