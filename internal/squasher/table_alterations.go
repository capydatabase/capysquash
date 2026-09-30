package squasher

import (
	"sort"
	"strings"

	"github.com/capydatabase/capysquash/internal/tracking"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// statementPosition is where a statement stands in the history: its
// migration, then its line.
type statementPosition struct {
	migration string
	line      int
}

func (p statementPosition) before(other statementPosition) bool {
	if p.migration != other.migration {
		return p.migration < other.migration
	}
	return p.line < other.line
}

// positionedSQL is a statement to insert among a table's statements at the
// place the history ran it.
type positionedSQL struct {
	sql      string
	position statementPosition
}

// lifecycleStatementPosition is where the history runs the first statement
// of a lifecycle.
func lifecycleStatementPosition(lifecycle *tracking.ObjectLifecycle) statementPosition {
	if lifecycle == nil || len(lifecycle.History) == 0 {
		return statementPosition{}
	}
	event := lifecycle.History[0]
	return statementPosition{migration: event.Migration, line: event.Statement.Line}
}

// insertSQLAtHistoryPositions inserts statements into a table's
// consolidation result where the history ran them: after the last of the
// table's statements the history ran before them, and never before its
// CREATE TABLE. A constraint a DO block adds can name a column a later
// ALTER TABLE added, and a later statement of the table (VALIDATE
// CONSTRAINT) can need the constraint, so neither the end nor the spot
// right after CREATE TABLE fits every case. Each of the table's statements
// takes the position of the history statement it is (the consolidated
// CREATE TABLE, which is none of them, stands where the table is created).
func insertSQLAtHistoryPositions(result *tracking.ConsolidationResult, inserts []positionedSQL) string {
	sql := result.ConsolidatedSQL
	tree, err := pg_query.Parse(sql)
	if err != nil || len(tree.GetStmts()) == 0 {
		var statements []string
		for _, insert := range inserts {
			statements = append(statements, insert.sql)
		}
		if inserted, ok := insertSQLAfterCreateTable(sql, strings.Join(statements, "\n")); ok {
			return inserted
		}
		return sql
	}

	positions := make(map[string]statementPosition, len(result.OriginalStatements))
	var first statementPosition
	for i, stmt := range result.OriginalStatements {
		position := statementPosition{migration: stmt.Filename, line: stmt.Line}
		if i == 0 {
			first = position
		}
		if fingerprint, err := pg_query.Fingerprint(stmt.SQL); err == nil {
			if _, seen := positions[fingerprint]; !seen {
				positions[fingerprint] = position
			}
		}
	}

	type slot struct {
		end      int
		position statementPosition
		create   bool
	}
	slots := make([]slot, 0, len(tree.GetStmts()))
	previous := first
	for _, raw := range tree.GetStmts() {
		start, end, ok := statementSpan(sql, raw)
		if !ok {
			return sql
		}
		position := previous
		if fingerprint, err := pg_query.Fingerprint(sql[start:end]); err == nil {
			if known, ok := positions[fingerprint]; ok {
				position = known
			}
		}
		slots = append(slots, slot{end: end, position: position, create: raw.GetStmt().GetCreateStmt() != nil})
		previous = position
	}

	ordered := append([]positionedSQL(nil), inserts...)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].position.before(ordered[j].position) })
	byEnd := make(map[int][]string)
	var ends []int
	for _, insert := range ordered {
		at := -1
		for i, s := range slots {
			if s.create || s.position.before(insert.position) {
				at = i
			}
		}
		if at < 0 {
			at = 0
		}
		end := slots[at].end
		if _, seen := byEnd[end]; !seen {
			ends = append(ends, end)
		}
		byEnd[end] = append(byEnd[end], strings.TrimSpace(insert.sql))
	}
	sort.Sort(sort.Reverse(sort.IntSlice(ends)))
	for _, end := range ends {
		sql = sql[:end] + "\n\n" + strings.Join(byEnd[end], "\n") + sql[end:]
	}
	return sql
}
