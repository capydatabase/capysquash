package squasher

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/capydatabase/capysquash/internal/tracking"
	"github.com/capydatabase/capysquash/internal/types"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// A foreign key needs a unique constraint or a unique index on the columns
// it references at the moment it is created. The baseline creates foreign
// keys with their tables (foundation section) or once every table exists
// (constraints section), and indexes after both, so a foreign key whose
// referenced columns are only unique through CREATE UNIQUE INDEX failed with
// "there is no unique constraint matching given keys". Such an index moves
// into the statements of the table it is on, where the history created it
// among them, ahead of every foreign key that can need it.

// referencedKey is the table and the columns a foreign key references.
type referencedKey struct {
	table   string
	columns []string
}

// foreignKeyReferences lists the keys the foreign keys in sql reference by
// naming columns; a foreign key that references the primary key without
// naming it needs no index. SQL that does not parse gives nothing.
func foreignKeyReferences(sql string) []referencedKey {
	tree, err := pg_query.Parse(sql)
	if err != nil {
		return nil
	}
	var keys []referencedKey
	add := func(c *pg_query.Constraint) {
		if c == nil || c.GetContype() != pg_query.ConstrType_CONSTR_FOREIGN || len(c.GetPkAttrs()) == 0 {
			return
		}
		keys = append(keys, referencedKey{table: rangeVarIdentifier(c.GetPktable()), columns: sortedNames(c.GetPkAttrs())})
	}
	for _, raw := range tree.GetStmts() {
		switch node := raw.GetStmt().GetNode().(type) {
		case *pg_query.Node_CreateStmt:
			for _, element := range node.CreateStmt.GetTableElts() {
				add(element.GetConstraint())
				for _, c := range element.GetColumnDef().GetConstraints() {
					add(c.GetConstraint())
				}
			}
		case *pg_query.Node_AlterTableStmt:
			for _, command := range node.AlterTableStmt.GetCmds() {
				cmd := command.GetAlterTableCmd()
				add(cmd.GetDef().GetConstraint())
				for _, c := range cmd.GetDef().GetColumnDef().GetConstraints() {
					add(c.GetConstraint())
				}
			}
		}
	}
	return keys
}

// uniqueIndexKey returns the table and key columns of a CREATE UNIQUE INDEX
// that can back a foreign key: plain columns, no predicate. ok is false for
// anything else.
func uniqueIndexKey(sql string) (key referencedKey, ok bool) {
	tree, err := pg_query.Parse(sql)
	if err != nil || len(tree.GetStmts()) != 1 {
		return referencedKey{}, false
	}
	index := tree.GetStmts()[0].GetStmt().GetIndexStmt()
	if index == nil || !index.GetUnique() || index.GetWhereClause() != nil {
		return referencedKey{}, false
	}
	columns := make([]string, 0, len(index.GetIndexParams()))
	for _, param := range index.GetIndexParams() {
		name := param.GetIndexElem().GetName()
		if name == "" {
			return referencedKey{}, false
		}
		columns = append(columns, strings.ToLower(name))
	}
	sort.Strings(columns)
	return referencedKey{table: rangeVarIdentifier(index.GetRelation()), columns: columns}, true
}

// moveUniqueIndexesBackingForeignKeys finds the unique indexes the foreign
// keys in fkSQL reference and queues each for insertion among the
// statements of its table's foundation result, at its place in the history.
// A foreign key of the table on itself that consolidation merged into its
// CREATE TABLE follows the index as ALTER TABLE. It returns the keys of the
// moved index results, which the indexes section then leaves out.
func moveUniqueIndexesBackingForeignKeys(
	consolidated map[string]*tracking.ConsolidationResult,
	foundation map[string]*tracking.ConsolidationResult,
	lifecycles map[string]*tracking.ObjectLifecycle,
	fkSQL []string,
	pending map[*tracking.ConsolidationResult][]positionedSQL,
) (map[string]struct{}, error) {
	var referenced []referencedKey
	for _, sql := range fkSQL {
		referenced = append(referenced, foreignKeyReferences(sql)...)
	}
	moved := make(map[string]struct{})
	if len(referenced) == 0 {
		return moved, nil
	}

	keys := make([]string, 0, len(consolidated))
	for key := range consolidated {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lifecycle := lifecycles[key]
		if lifecycle == nil || lifecycle.Type != types.TypeIndex {
			continue
		}
		result := consolidated[key]
		index, ok := uniqueIndexKey(result.ConsolidatedSQL)
		if !ok || !slices.ContainsFunc(referenced, func(r referencedKey) bool {
			return r.table == index.table && slices.Equal(r.columns, index.columns)
		}) {
			continue
		}
		table := findTableConsolidationResult(foundation, lifecycles, index.table)
		if table == nil {
			continue // a table the history did not create: nothing to move ahead of
		}
		selfReferences, err := takeSelfReferencesOutOfCreate(table, index)
		if err != nil {
			return nil, err
		}
		position := lastCreatePosition(lifecycle)
		pending[table] = append(pending[table], positionedSQL{sql: terminateSQLStatement(result.ConsolidatedSQL), position: position})
		for _, alter := range selfReferences {
			pending[table] = append(pending[table], positionedSQL{sql: alter, position: position})
		}
		moved[key] = struct{}{}
	}
	return moved, nil
}

// takeSelfReferencesOutOfCreate takes the foreign keys of a table on its
// own columns key out of its CREATE TABLE (where consolidation can have
// merged them) and returns them as ALTER TABLE ... ADD CONSTRAINT: the
// unique index they need cannot exist before the table does.
func takeSelfReferencesOutOfCreate(table *tracking.ConsolidationResult, key referencedKey) ([]string, error) {
	tree, err := pg_query.Parse(table.ConsolidatedSQL)
	if err != nil {
		return nil, nil
	}
	var moved []foreignKeyUse
	for _, use := range foreignKeyUses(tree) {
		if tree.GetStmts()[use.statement].GetStmt().GetCreateStmt() == nil {
			continue
		}
		if canonicalTableIdentifier(use.refTable) == key.table && slices.Equal(sortedNames(use.constraint.GetPkAttrs()), key.columns) {
			moved = append(moved, use)
		}
	}
	if len(moved) == 0 {
		return nil, nil
	}
	sql, alters, err := removeForeignKeys(table.ConsolidatedSQL, tree, moved)
	if err != nil {
		return nil, fmt.Errorf("take the foreign keys of %s on itself out of its CREATE TABLE: %w", key.table, err)
	}
	table.ConsolidatedSQL = sql
	return alters, nil
}

// lastCreatePosition is where the history runs a lifecycle's last CREATE.
func lastCreatePosition(lifecycle *tracking.ObjectLifecycle) statementPosition {
	for i := len(lifecycle.History) - 1; i >= 0; i-- {
		if event := lifecycle.History[i]; event.Operation == types.OpCreate {
			return statementPosition{migration: event.Migration, line: event.Statement.Line}
		}
	}
	return lifecycleStatementPosition(lifecycle)
}

// rangeVarIdentifier is a relation name as canonicalTableIdentifier spells
// it: lower case, schema-qualified unless in public.
func rangeVarIdentifier(rv *pg_query.RangeVar) string {
	name := rv.GetRelname()
	if schema := rv.GetSchemaname(); schema != "" {
		name = schema + "." + name
	}
	return canonicalTableIdentifier(name)
}

func sortedNames(nodes []*pg_query.Node) []string {
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		names = append(names, strings.ToLower(node.GetString_().GetSval()))
	}
	sort.Strings(names)
	return names
}
