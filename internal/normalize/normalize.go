// Package normalize rewrites a migration history so that the consolidation
// pipeline, which groups statements by object and emits them by category,
// never has to order statements around a rename or a dropped schema.
//
// The history is read twice. The first pass simulates it to learn the name
// every object ends with; the second rewrites each statement so that it
// names objects by those final names:
//
//   - Schemas, types (enums, composites, domains), views, materialized views,
//     sequences and indexes are created under their final names, and the
//     statements that renamed them (ALTER ... RENAME, SET SCHEMA, RENAME
//     VALUE, RENAME ATTRIBUTE) are taken out.
//   - Tables keep the statements that create and alter them as written, with
//     their renames: PostgreSQL derives the names of a table's constraints,
//     indexes and sequences from the table and column names at the moment it
//     creates them, and a baseline that created the table under its final
//     name would name them differently. Those statements are tracked under
//     the table's final name, so the pipeline emits them together and in
//     order; every other statement names the table and its columns as they
//     end up, so it can run after that block. An index created without a
//     name gets the name PostgreSQL gave it, spelled out.
//   - Schemas leave the pipeline: CREATE SCHEMA, and the ALTER SCHEMA ...
//     RENAME and DROP SCHEMA of schemas the history did not create, make the
//     baseline's SCHEMAS section (SchemasSQL). A schema the history creates
//     and drops, with DROP SCHEMA ... CASCADE or once empty, disappears with
//     everything created in it.
//
// What cannot be rewritten is reported as a warning (Warnings) rather than
// guessed; validation then shows the consequence.
package normalize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/capydatabase/capysquash/internal/parser"
	"github.com/capydatabase/capysquash/internal/types"
	pg_query "github.com/pganalyze/pg_query_go/v6"
	"google.golang.org/protobuf/proto"
)

// Normalizer runs the two passes. Call Observe for every migration in
// history order, then Finish, then Rewrite for every migration again in the
// same order.
type Normalizer struct {
	st       *state
	second   bool
	warnings []string
	warned   map[string]bool

	schemaOps     []string  // ALTER SCHEMA ... RENAME / DROP SCHEMA of schemas the history did not create
	schemaCreates []created // CREATE SCHEMA of schemas the history creates
	renamedNames  []string  // names objects had before a rename, for the DO block check
}

type created struct {
	sql    string
	schema *schema
}

// New returns a normalizer ready for the first pass.
func New() *Normalizer {
	return &Normalizer{st: newState(nil), warned: map[string]bool{}}
}

// Observe feeds one migration's statements to the first pass.
func (n *Normalizer) Observe(statements []types.Statement) {
	for _, stmt := range statements {
		if root := statementNode(stmt); root != nil {
			n.apply(stmt, root)
		}
	}
}

// Finish ends the first pass.
func (n *Normalizer) Finish() {
	n.st = newState(n.st.entities)
	n.second = true
}

// Rewrite returns one migration's statements rewritten to final names, for
// the consolidation pipeline. Statements that only rename, and schema
// statements, are left out.
func (n *Normalizer) Rewrite(statements []types.Statement) ([]types.Statement, error) {
	if !n.second {
		return nil, fmt.Errorf("normalize: Rewrite called before Finish")
	}
	var out []types.Statement
	for _, stmt := range statements {
		rewritten, err := n.process(stmt)
		if err != nil {
			return nil, err
		}
		out = append(out, rewritten...)
	}
	return out, nil
}

// SchemasSQL returns the SCHEMAS section of the baseline: the renames and
// drops of schemas the history did not create, in history order, then the
// schemas the history creates and keeps, under their final names.
func (n *Normalizer) SchemasSQL() string {
	var sb strings.Builder
	for _, sql := range n.schemaOps {
		sb.WriteString(sql + ";\n")
	}
	for _, c := range n.schemaCreates {
		if c.schema.final != nil && c.schema.final.dead {
			continue
		}
		sb.WriteString(c.sql + ";\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

// Warnings lists what the rewrite could not carry, sorted.
func (n *Normalizer) Warnings() []string {
	out := append([]string(nil), n.warnings...)
	sort.Strings(out)
	return out
}

func (n *Normalizer) warn(format string, args ...any) {
	if !n.second {
		return
	}
	message := fmt.Sprintf(format, args...)
	if !n.warned[message] {
		n.warned[message] = true
		n.warnings = append(n.warnings, message)
	}
}

func statementNode(stmt types.Statement) *pg_query.Node {
	if stmt.ParseTree == nil || len(stmt.ParseTree.GetStmts()) == 0 {
		return nil
	}
	return stmt.ParseTree.GetStmts()[0].GetStmt()
}

func deparse(node *pg_query.Node) (string, error) {
	return pg_query.Deparse(&pg_query.ParseResult{Stmts: []*pg_query.RawStmt{{Stmt: node}}})
}

// result is what one statement (or one element of a CREATE SCHEMA) becomes.
type result struct {
	node    *pg_query.Node
	remove  bool
	split   bool      // an element taken out of CREATE SCHEMA: always rendered
	own     *relation // the table this statement creates or alters
	subject any       // the object whose loss to DROP SCHEMA also removes the statement
}

// apply runs one statement through the simulation, on a copy of its parse
// tree that the second pass rewrites.
func (n *Normalizer) apply(stmt types.Statement, root *pg_query.Node) []result {
	clone, ok := proto.Clone(root).(*pg_query.Node)
	if !ok {
		return []result{{node: root}}
	}
	r := &rewriter{n: n, s: n.st, rewrite: n.second, sql: firstLine(stmt.SQL)}
	return r.statement(clone)
}

// process rewrites one statement in the second pass.
func (n *Normalizer) process(stmt types.Statement) ([]types.Statement, error) {
	root := statementNode(stmt)
	if root == nil {
		return []types.Statement{stmt}, nil
	}
	results := n.apply(stmt, root)

	var out []types.Statement
	for _, res := range results {
		if res.remove || killed(res.subject) {
			continue
		}
		next := stmt
		if res.split || !proto.Equal(res.node, root) {
			sql, err := deparse(res.node)
			if err != nil {
				return nil, fmt.Errorf("render rewritten statement %q: %w", firstLine(stmt.SQL), err)
			}
			next, err = parser.ReparseStatement(stmt, sql)
			if err != nil {
				return nil, err
			}
		}
		if own := res.own; own != nil && own.isRenamed() {
			// Track every statement of a renamed table under the name it
			// ends with, so they stay one object emitted in history order.
			next.ObjectType = types.TypeTable
			next.ObjectName = parser.QualifiedTableName(own.finalSchema().name, own.finalName())
			next.Schema = own.finalSchema().name
			if next.Operation != types.OpCreate && next.Operation != types.OpDrop {
				next.Operation = types.OpAlter
			}
			next.Category = types.CategoryFoundation
		}
		out = append(out, next)
	}
	return out, nil
}

// killed reports an object DROP SCHEMA removes by the end of the history,
// and a sequence the history drops.
func killed(subject any) bool {
	switch o := subject.(type) {
	case *relation:
		return o != nil && (o.isKilled() || o.isDroppedSequence())
	case *typ:
		return o != nil && o.isKilled()
	case *routine:
		return o != nil && o.final != nil && o.final.killed
	case *extension:
		return o != nil && o.final != nil && o.final.killed
	case *schema:
		return o != nil && o.final != nil && o.final.dead
	}
	return false
}

func firstLine(sql string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(sql), "\n")
	return line
}
