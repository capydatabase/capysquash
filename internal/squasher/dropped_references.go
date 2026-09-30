package squasher

import (
	"fmt"
	"sort"
	"strings"

	"github.com/capydatabase/capysquash/internal/pgnames"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// stripForeignKeysToDroppedTables removes, from the statements of a table
// the baseline keeps, the foreign keys that reference a table the history
// drops. No such constraint survives the history - DROP TABLE refuses to
// drop a referenced table unless CASCADE drops the constraint with it - but
// the statements that create the referencing table still name the dropped
// table, which the baseline never creates. The columns stay as they are:
// the history's later DROP COLUMN (kept as written) has to find them, and a
// column dropped later still takes up its position in the table.
//
// A statement dropping one of the removed constraints by name is removed
// with it, so it cannot fail on a constraint that no longer exists. SQL that
// does not parse, or names no dropped table, is returned unchanged.
func stripForeignKeysToDroppedTables(sql string, droppedTables map[string]struct{}) (string, error) {
	if len(droppedTables) == 0 {
		return sql, nil
	}
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return sql, nil
	}

	references := func(constraint *pg_query.Constraint) bool {
		if constraint == nil || constraint.GetContype() != pg_query.ConstrType_CONSTR_FOREIGN || constraint.GetPktable() == nil {
			return false
		}
		table := constraint.GetPktable().GetRelname()
		if schema := constraint.GetPktable().GetSchemaname(); schema != "" {
			table = schema + "." + table
		}
		return isDroppedTableName(droppedTables, table)
	}

	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	removedNames := make(map[string]bool)

	for _, raw := range tree.GetStmts() {
		changed, removed := false, false
		switch node := raw.GetStmt().GetNode().(type) {
		case *pg_query.Node_CreateStmt:
			stmt := node.CreateStmt
			table := stmt.GetRelation().GetRelname()
			elements := stmt.GetTableElts()[:0]
			for _, element := range stmt.GetTableElts() {
				if constraint := element.GetConstraint(); references(constraint) {
					removedNames[foreignKeyName(table, constraint, nil)] = true
					changed = true
					continue
				}
				if column := element.GetColumnDef(); column != nil {
					if stripColumnForeignKeys(table, column, references, removedNames) {
						changed = true
					}
				}
				elements = append(elements, element)
			}
			stmt.TableElts = elements

		case *pg_query.Node_AlterTableStmt:
			stmt := node.AlterTableStmt
			table := stmt.GetRelation().GetRelname()
			commands := stmt.GetCmds()[:0]
			for _, command := range stmt.GetCmds() {
				cmd := command.GetAlterTableCmd()
				if cmd != nil {
					switch cmd.GetSubtype() {
					case pg_query.AlterTableType_AT_AddConstraint:
						if constraint := cmd.GetDef().GetConstraint(); references(constraint) {
							removedNames[foreignKeyName(table, constraint, nil)] = true
							changed = true
							continue
						}
					case pg_query.AlterTableType_AT_AddColumn:
						if column := cmd.GetDef().GetColumnDef(); column != nil &&
							stripColumnForeignKeys(table, column, references, removedNames) {
							changed = true
						}
					case pg_query.AlterTableType_AT_DropConstraint:
						if removedNames[cmd.GetName()] {
							changed = true
							continue
						}
					}
				}
				commands = append(commands, command)
			}
			stmt.Cmds = commands
			removed = len(commands) == 0
		}
		if !changed {
			continue
		}

		start, end, ok := statementSpan(sql, raw)
		if !ok {
			return "", fmt.Errorf("locate a statement that references a dropped table")
		}
		if removed {
			edits = append(edits, edit{start: start, end: end})
			continue
		}
		text, err := pg_query.Deparse(&pg_query.ParseResult{Stmts: []*pg_query.RawStmt{{Stmt: raw.GetStmt()}}})
		if err != nil {
			return "", fmt.Errorf("deparse a statement without its foreign keys to dropped tables: %w", err)
		}
		edits = append(edits, edit{start: start, end: end, text: text + ";"})
	}

	if len(edits) == 0 {
		return sql, nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		sql = sql[:e.start] + e.text + sql[e.end:]
	}
	return strings.TrimSpace(sql), nil
}

// stripColumnForeignKeys removes a column's REFERENCES constraints that
// match and records the names PostgreSQL gives them.
func stripColumnForeignKeys(table string, column *pg_query.ColumnDef, matches func(*pg_query.Constraint) bool, removedNames map[string]bool) bool {
	changed := false
	constraints := column.GetConstraints()[:0]
	for _, node := range column.GetConstraints() {
		if constraint := node.GetConstraint(); matches(constraint) {
			removedNames[foreignKeyName(table, constraint, []string{column.GetColname()})] = true
			changed = true
			continue
		}
		constraints = append(constraints, node)
	}
	column.Constraints = constraints
	return changed
}

// foreignKeyName is the constraint's own name, or the one PostgreSQL gives
// a foreign key created without one: table_columns_fkey.
func foreignKeyName(table string, constraint *pg_query.Constraint, columns []string) string {
	if name := constraint.GetConname(); name != "" {
		return name
	}
	if columns == nil {
		for _, attr := range constraint.GetFkAttrs() {
			columns = append(columns, attr.GetString_().GetSval())
		}
	}
	return pgnames.MakeObjectName(table, strings.Join(columns, "_"), "fkey")
}
