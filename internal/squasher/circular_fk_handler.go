package squasher

import (
	"fmt"
	"sort"
	"strings"

	"github.com/capydatabase/capysquash/internal/pgnames"
	"github.com/capydatabase/capysquash/internal/tracking"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// foreignKeyUse is one foreign key in the statements of a table's
// consolidation result: an inline REFERENCES of a column (in CREATE TABLE
// or ALTER TABLE ... ADD COLUMN), a table constraint of CREATE TABLE, or an
// ALTER TABLE ... ADD CONSTRAINT.
type foreignKeyUse struct {
	statement  int
	constraint *pg_query.Constraint
	column     string // set for an inline REFERENCES
	refTable   string // canonical name of the referenced table
}

// deferCyclicForeignKeys breaks the foreign key cycles between the tables
// the baseline creates. Consolidation gathers each table's statements into
// one result, so a foreign key the history added once both tables existed
// (ALTER TABLE a ADD CONSTRAINT ... REFERENCES b, with b referencing a) can
// leave two tables that each need the other to exist first. Every foreign
// key between two tables of the same cycle is taken out of its table's
// statements and returned as an ALTER TABLE ... ADD CONSTRAINT, in history
// order, to run once every table exists - under the name PostgreSQL gives
// it, so the catalog is the same. The dependencies of the results are
// updated to match.
func deferCyclicForeignKeys(tables map[string]*tracking.ConsolidationResult, lifecycles map[string]*tracking.ObjectLifecycle) ([]string, error) {
	keys := make([]string, 0, len(tables))
	for key := range tables {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return historyPositionLess(lifecycles[keys[i]], lifecycles[keys[j]], keys[i], keys[j])
	})

	trees := make(map[string]*pg_query.ParseResult, len(keys))
	uses := make(map[string][]foreignKeyUse, len(keys))
	tableOf := make(map[string]string, len(keys)) // canonical table name -> key
	for _, key := range keys {
		if lifecycle := lifecycles[key]; lifecycle != nil {
			tableOf[canonicalRelationName(lifecycle.Name)] = key
		}
	}
	edges := make(map[string][]string, len(keys))
	for _, key := range keys {
		tree, err := pg_query.Parse(tables[key].ConsolidatedSQL)
		if err != nil {
			continue
		}
		trees[key] = tree
		uses[key] = foreignKeyUses(tree)
		seen := make(map[string]bool)
		for _, use := range uses[key] {
			target, ok := tableOf[use.refTable]
			if ok && target != key && !seen[target] {
				seen[target] = true
				edges[key] = append(edges[key], target)
			}
		}
	}

	component := make(map[string]int)
	for index, members := range stronglyConnectedComponents(keys, edges) {
		if len(members) < 2 {
			continue
		}
		for _, member := range members {
			component[member] = index + 1
		}
	}
	if len(component) == 0 {
		return nil, nil
	}

	var deferred []string
	for _, key := range keys {
		if component[key] == 0 {
			continue
		}
		var moved []foreignKeyUse
		for _, use := range uses[key] {
			if target, ok := tableOf[use.refTable]; ok && target != key && component[target] == component[key] {
				moved = append(moved, use)
			}
		}
		if len(moved) == 0 {
			continue
		}
		sql, statements, err := removeForeignKeys(tables[key].ConsolidatedSQL, trees[key], moved)
		if err != nil {
			return nil, fmt.Errorf("defer the foreign keys of %s: %w", key, err)
		}
		deferred = append(deferred, statements...)
		tables[key].ConsolidatedSQL = sql

		movedTables := make(map[string]bool, len(moved))
		for _, use := range moved {
			movedTables[use.refTable] = true
		}
		dropMovedReferences(tables[key], movedTables)
	}
	return deferred, nil
}

// foreignKeyUses lists the foreign keys the statements of a table result
// add.
func foreignKeyUses(tree *pg_query.ParseResult) []foreignKeyUse {
	var uses []foreignKeyUse
	add := func(statement int, constraint *pg_query.Constraint, column string) {
		if constraint.GetContype() != pg_query.ConstrType_CONSTR_FOREIGN || constraint.GetPktable() == nil {
			return
		}
		uses = append(uses, foreignKeyUse{
			statement:  statement,
			constraint: constraint,
			column:     column,
			refTable:   canonicalRelationName(rangeVarName(constraint.GetPktable())),
		})
	}
	addColumn := func(statement int, column *pg_query.ColumnDef) {
		for _, node := range column.GetConstraints() {
			if constraint := node.GetConstraint(); constraint != nil {
				add(statement, constraint, column.GetColname())
			}
		}
	}
	for i, raw := range tree.GetStmts() {
		switch node := raw.GetStmt().GetNode().(type) {
		case *pg_query.Node_CreateStmt:
			for _, element := range node.CreateStmt.GetTableElts() {
				if constraint := element.GetConstraint(); constraint != nil {
					add(i, constraint, "")
				}
				if column := element.GetColumnDef(); column != nil {
					addColumn(i, column)
				}
			}
		case *pg_query.Node_AlterTableStmt:
			for _, command := range node.AlterTableStmt.GetCmds() {
				cmd := command.GetAlterTableCmd()
				switch cmd.GetSubtype() {
				case pg_query.AlterTableType_AT_AddConstraint:
					if constraint := cmd.GetDef().GetConstraint(); constraint != nil {
						add(i, constraint, "")
					}
				case pg_query.AlterTableType_AT_AddColumn:
					if column := cmd.GetDef().GetColumnDef(); column != nil {
						addColumn(i, column)
					}
				}
			}
		}
	}
	return uses
}

// removeForeignKeys takes the given foreign keys out of the statements of
// sql (parsed as tree) and returns the statements left, with only the
// changed statements deparsed, and an ALTER TABLE ... ADD CONSTRAINT for
// each foreign key taken out.
func removeForeignKeys(sql string, tree *pg_query.ParseResult, moved []foreignKeyUse) (string, []string, error) {
	isMoved := make(map[*pg_query.Constraint]bool, len(moved))
	for _, use := range moved {
		isMoved[use.constraint] = true
	}
	keepConstraints := func(nodes []*pg_query.Node) []*pg_query.Node {
		kept := nodes[:0]
		for _, node := range nodes {
			if !isMoved[node.GetConstraint()] {
				kept = append(kept, node)
			}
		}
		return kept
	}

	var deferred []string
	type edit struct {
		start, end int
		text       string
	}
	var edits []edit
	for i, raw := range tree.GetStmts() {
		var relation *pg_query.RangeVar
		var statementUses []foreignKeyUse
		for _, use := range moved {
			if use.statement == i {
				statementUses = append(statementUses, use)
			}
		}
		if len(statementUses) == 0 {
			continue
		}

		empty := false
		switch node := raw.GetStmt().GetNode().(type) {
		case *pg_query.Node_CreateStmt:
			relation = node.CreateStmt.GetRelation()
			elements := node.CreateStmt.GetTableElts()[:0]
			for _, element := range node.CreateStmt.GetTableElts() {
				if isMoved[element.GetConstraint()] {
					continue
				}
				if column := element.GetColumnDef(); column != nil {
					column.Constraints = keepConstraints(column.GetConstraints())
				}
				elements = append(elements, element)
			}
			node.CreateStmt.TableElts = elements
		case *pg_query.Node_AlterTableStmt:
			relation = node.AlterTableStmt.GetRelation()
			commands := node.AlterTableStmt.GetCmds()[:0]
			for _, command := range node.AlterTableStmt.GetCmds() {
				cmd := command.GetAlterTableCmd()
				if cmd.GetSubtype() == pg_query.AlterTableType_AT_AddConstraint && isMoved[cmd.GetDef().GetConstraint()] {
					continue
				}
				if column := cmd.GetDef().GetColumnDef(); column != nil {
					column.Constraints = keepConstraints(column.GetConstraints())
				}
				commands = append(commands, command)
			}
			node.AlterTableStmt.Cmds = commands
			empty = len(commands) == 0
		}

		for _, use := range statementUses {
			alter, err := addForeignKeySQL(relation, use)
			if err != nil {
				return "", nil, err
			}
			deferred = append(deferred, alter)
		}

		start, end, ok := statementSpan(sql, raw)
		if !ok {
			return "", nil, fmt.Errorf("locate statement %d", i+1)
		}
		if empty {
			edits = append(edits, edit{start: start, end: end})
			continue
		}
		text, err := pg_query.Deparse(&pg_query.ParseResult{Stmts: []*pg_query.RawStmt{{Stmt: raw.GetStmt()}}})
		if err != nil {
			return "", nil, fmt.Errorf("deparse statement %d without its cyclic foreign keys: %w", i+1, err)
		}
		edits = append(edits, edit{start: start, end: end, text: text + ";"})
	}

	sort.Slice(edits, func(i, j int) bool { return edits[i].start > edits[j].start })
	for _, e := range edits {
		sql = sql[:e.start] + e.text + sql[e.end:]
	}
	return strings.TrimSpace(sql), deferred, nil
}

// addForeignKeySQL is the ALTER TABLE ... ADD CONSTRAINT that adds a
// foreign key taken out of a statement on relation. An inline REFERENCES
// becomes a table constraint on its column, named as PostgreSQL names it
// (table_column_fkey) unless it has a name of its own; a table constraint
// without a name gets the same name from PostgreSQL when it is added.
func addForeignKeySQL(relation *pg_query.RangeVar, use foreignKeyUse) (string, error) {
	constraint := use.constraint
	if use.column != "" {
		constraint.FkAttrs = []*pg_query.Node{pg_query.MakeStrNode(use.column)}
		if constraint.GetConname() == "" {
			constraint.Conname = pgnames.MakeObjectName(relation.GetRelname(), use.column, "fkey")
		}
	}
	target := &pg_query.RangeVar{
		Schemaname:     relation.GetSchemaname(),
		Relname:        relation.GetRelname(),
		Inh:            true,
		Relpersistence: "p",
	}
	stmt := &pg_query.Node{Node: &pg_query.Node_AlterTableStmt{AlterTableStmt: &pg_query.AlterTableStmt{
		Relation: target,
		Objtype:  pg_query.ObjectType_OBJECT_TABLE,
		Cmds: []*pg_query.Node{{Node: &pg_query.Node_AlterTableCmd{AlterTableCmd: &pg_query.AlterTableCmd{
			Subtype:  pg_query.AlterTableType_AT_AddConstraint,
			Def:      &pg_query.Node{Node: &pg_query.Node_Constraint{Constraint: constraint}},
			Behavior: pg_query.DropBehavior_DROP_RESTRICT,
		}}}},
	}}}
	text, err := pg_query.Deparse(&pg_query.ParseResult{Stmts: []*pg_query.RawStmt{{Stmt: stmt}}})
	if err != nil {
		return "", fmt.Errorf("deparse the deferred foreign key of %s: %w", relation.GetRelname(), err)
	}
	return text + ";", nil
}

// dropMovedReferences removes, from the dependencies of a result's
// statements, the references to tables whose foreign keys were deferred, so
// the ordering no longer waits for them.
func dropMovedReferences(result *tracking.ConsolidationResult, movedTables map[string]bool) {
	for i, stmt := range result.OriginalStatements {
		var kept []string
		for _, dep := range stmt.Dependencies {
			if name, ok := strings.CutPrefix(dep, "REFERENCES:"); ok && movedTables[canonicalRelationName(name)] {
				continue
			}
			kept = append(kept, dep)
		}
		result.OriginalStatements[i].Dependencies = kept
	}
}

func rangeVarName(rv *pg_query.RangeVar) string {
	if rv.GetSchemaname() != "" {
		return rv.GetSchemaname() + "." + rv.GetRelname()
	}
	return rv.GetRelname()
}

// canonicalRelationName is a relation name in lower case with its schema,
// public when it has none.
func canonicalRelationName(name string) string {
	name = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), `"`, ""))
	if !strings.Contains(name, ".") {
		name = "public." + name
	}
	return name
}

// stronglyConnectedComponents returns the strongly connected components of
// the graph given by nodes and edges (Tarjan's algorithm), each listing its
// members in the order of nodes.
func stronglyConnectedComponents(nodes []string, edges map[string][]string) [][]string {
	position := make(map[string]int, len(nodes))
	for i, node := range nodes {
		position[node] = i
	}
	index := make(map[string]int, len(nodes))
	low := make(map[string]int, len(nodes))
	onStack := make(map[string]bool, len(nodes))
	var stack []string
	var components [][]string
	next := 0

	var visit func(node string)
	visit = func(node string) {
		index[node] = next
		low[node] = next
		next++
		stack = append(stack, node)
		onStack[node] = true
		for _, target := range edges[node] {
			if _, known := position[target]; !known {
				continue
			}
			if _, seen := index[target]; !seen {
				visit(target)
				low[node] = min(low[node], low[target])
			} else if onStack[target] {
				low[node] = min(low[node], index[target])
			}
		}
		if low[node] != index[node] {
			return
		}
		var component []string
		for {
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[top] = false
			component = append(component, top)
			if top == node {
				break
			}
		}
		sort.Slice(component, func(i, j int) bool { return position[component[i]] < position[component[j]] })
		components = append(components, component)
	}
	for _, node := range nodes {
		if _, seen := index[node]; !seen {
			visit(node)
		}
	}
	return components
}
