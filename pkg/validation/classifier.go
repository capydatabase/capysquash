package validation

import (
	"encoding/json"
	"fmt"
	"strings"

	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// StatementClass describes the side-effect profile of a single SQL statement.
type StatementClass string

const (
	// StatementReadOnly statements cannot modify data, schema, or server state.
	StatementReadOnly StatementClass = "read_only"
	// StatementWrite statements modify data (or may do so, e.g. DO blocks,
	// procedure calls, and side-effecting functions such as setval/nextval).
	StatementWrite StatementClass = "write"
	// StatementDDL statements modify schema objects (CREATE/ALTER/DROP/...,
	// including SELECT ... INTO which creates a table).
	StatementDDL StatementClass = "ddl"
	// StatementMaintenance statements affect server or session state without
	// directly writing user data (VACUUM, SET, BEGIN/COMMIT, LOCK, ...).
	// They are NOT read-only.
	StatementMaintenance StatementClass = "maintenance"
)

// ClassifiedStatement is one statement extracted from a SQL script together
// with its classification.
type ClassifiedStatement struct {
	// SQL is the exact statement text as it appeared in the input.
	SQL string `json:"sql"`
	// Class is the side-effect classification.
	Class StatementClass `json:"class"`
	// Reason explains why a non-read-only class was assigned.
	Reason string `json:"reason,omitempty"`
}

// IsReadOnly reports whether the statement is safe on a read-only connection.
func (c ClassifiedStatement) IsReadOnly() bool {
	return c.Class == StatementReadOnly
}

// sideEffectFunctions are functions that mutate database or cluster state even
// when invoked from a plain SELECT target list. Detecting these closes the
// classic "SELECT setval(...)" bypass of naive read-only gates.
var sideEffectFunctions = map[string]struct{}{
	"setval":                                 {},
	"nextval":                                {},
	"lo_creat":                               {},
	"lo_create":                              {},
	"lo_import":                              {},
	"lo_unlink":                              {},
	"pg_advisory_lock":                       {},
	"pg_advisory_lock_shared":                {},
	"pg_advisory_xact_lock":                  {},
	"pg_advisory_xact_lock_shared":           {},
	"pg_cancel_backend":                      {},
	"pg_terminate_backend":                   {},
	"pg_reload_conf":                         {},
	"pg_rotate_logfile":                      {},
	"pg_switch_wal":                          {},
	"pg_create_restore_point":                {},
	"pg_promote":                             {},
	"pg_create_physical_replication_slot":    {},
	"pg_create_logical_replication_slot":     {},
	"pg_drop_replication_slot":               {},
	"pg_replication_origin_create":           {},
	"pg_replication_origin_drop":             {},
	"pg_logical_emit_message":                {},
	"pg_stat_reset":                          {},
	"pg_stat_reset_shared":                   {},
	"pg_stat_reset_single_table_counters":    {},
	"pg_stat_reset_single_function_counters": {},
	"dblink_exec":                            {},
	"set_config":                             {},
}

// dataModifyingNodes are parse-tree node types that modify data wherever they
// appear (e.g. inside a WITH clause of an otherwise plain SELECT).
var dataModifyingNodes = map[string]string{
	"InsertStmt":   "INSERT",
	"UpdateStmt":   "UPDATE",
	"DeleteStmt":   "DELETE",
	"MergeStmt":    "MERGE",
	"CopyStmt":     "COPY",
	"TruncateStmt": "TRUNCATE",
	"DoStmt":       "DO block",
	"CallStmt":     "CALL",
}

// ddlNameFragments classify utility statements by node-type name. Any node
// whose type name contains one of these fragments manipulates schema objects.
var ddlNameFragments = []string{
	"Create", "Alter", "Drop", "Rename", "Grant", "Revoke", "Comment",
	"Define", "Index", "Truncate", "Refresh", "Import", "SecLabel",
	"Reassign", "Composite", "Enum", "Range",
}

// maintenanceNodeTypes are session/server state statements: not read-only, but
// they neither write user data nor change schema.
var maintenanceNodeTypes = map[string]struct{}{
	"TransactionStmt": {},
	"VariableSetStmt": {},
	"LockStmt":        {},
	"ListenStmt":      {},
	"UnlistenStmt":    {},
	"NotifyStmt":      {},
	"DeallocateStmt":  {},
	"DiscardStmt":     {},
	"CheckPointStmt":  {},
	"VacuumStmt":      {},
	"ReindexStmt":     {},
	"ClusterStmt":     {},
	"LoadStmt":        {},
}

type rawStatement struct {
	Stmt         map[string]json.RawMessage `json:"stmt"`
	StmtLocation int                        `json:"stmt_location"`
	StmtLen      int                        `json:"stmt_len"`
}

type parseTree struct {
	Stmts []rawStatement `json:"stmts"`
}

// ClassifyStatements parses a SQL script with the real PostgreSQL parser
// (pg_query) and classifies every top-level statement. Statement splitting is
// therefore correct in the presence of dollar-quoted bodies, string literals,
// and comments. A parse failure returns an error; callers implementing safety
// gates must fail closed on error.
func ClassifyStatements(sql string) ([]ClassifiedStatement, error) {
	rawJSON, err := pg_query.ParseToJSON(sql)
	if err != nil {
		return nil, fmt.Errorf("parse SQL: %w", err)
	}

	var tree parseTree
	if err := json.Unmarshal([]byte(rawJSON), &tree); err != nil {
		return nil, fmt.Errorf("decode parse tree: %w", err)
	}

	statements := make([]ClassifiedStatement, 0, len(tree.Stmts))
	for _, raw := range tree.Stmts {
		text := statementText(sql, raw)

		nodeType, body, err := singleNode(raw.Stmt)
		if err != nil {
			return nil, fmt.Errorf("decode statement node: %w", err)
		}

		class, reason := classifyNode(nodeType, body)
		statements = append(statements, ClassifiedStatement{
			SQL:    text,
			Class:  class,
			Reason: reason,
		})
	}

	return statements, nil
}

// IsReadOnlySQL reports whether every statement in the script is read-only.
// Empty scripts (no statements after comment stripping) report false so that
// gates never treat unparseable-or-empty input as safe. Parse errors are
// returned to the caller, which must fail closed.
func IsReadOnlySQL(sql string) (bool, error) {
	statements, err := ClassifyStatements(sql)
	if err != nil {
		return false, err
	}
	if len(statements) == 0 {
		return false, nil
	}
	for _, statement := range statements {
		if !statement.IsReadOnly() {
			return false, nil
		}
	}
	return true, nil
}

// classifyNode maps one parse-tree node to a statement class.
func classifyNode(nodeType string, body json.RawMessage) (StatementClass, string) {
	switch nodeType {
	case "SelectStmt":
		return classifySelect(body)
	case "VariableShowStmt":
		return StatementReadOnly, ""
	case "ExplainStmt":
		return classifyExplain(body)
	case "InsertStmt", "UpdateStmt", "DeleteStmt", "MergeStmt":
		return StatementWrite, dataModifyingNodes[nodeType] + " statement"
	case "CopyStmt":
		// COPY FROM writes table data; COPY TO writes server-side files when a
		// filename is given. Treated as write in both directions.
		return StatementWrite, "COPY statement"
	case "DoStmt":
		// DO blocks execute arbitrary procedural code; conservatively write.
		return StatementWrite, "DO block (arbitrary procedural code)"
	case "CallStmt":
		return StatementWrite, "CALL statement (procedures may write)"
	case "ExecuteStmt":
		return StatementWrite, "EXECUTE of a prepared statement (target unknown)"
	case "FetchStmt", "ClosePortalStmt":
		return StatementReadOnly, ""
	case "DeclareCursorStmt":
		return classifyWrapped(body, "query")
	case "PrepareStmt":
		return classifyWrapped(body, "query")
	case "TruncateStmt":
		return StatementDDL, "TRUNCATE statement"
	}

	if _, ok := maintenanceNodeTypes[nodeType]; ok {
		return StatementMaintenance, nodeType + " affects session or server state"
	}

	for _, fragment := range ddlNameFragments {
		if strings.Contains(nodeType, fragment) {
			return StatementDDL, nodeType + " modifies schema objects"
		}
	}

	// Unknown statement types fail closed as writes.
	return StatementWrite, "unrecognized statement type " + nodeType + " (fail closed)"
}

// classifySelect handles SELECT/VALUES/TABLE statements, catching the bypass
// cases: SELECT ... INTO, data-modifying CTEs, and side-effecting functions.
func classifySelect(body json.RawMessage) (StatementClass, string) {
	var node map[string]any
	if err := json.Unmarshal(body, &node); err != nil {
		return StatementWrite, "undecodable SELECT node (fail closed)"
	}

	if _, hasInto := node["intoClause"]; hasInto {
		return StatementDDL, "SELECT ... INTO creates a table"
	}

	if reason := findHazard(node); reason != "" {
		return StatementWrite, reason
	}

	return StatementReadOnly, ""
}

// classifyExplain returns read-only for plain EXPLAIN and defers to the inner
// statement when ANALYZE is requested (EXPLAIN ANALYZE executes the query).
func classifyExplain(body json.RawMessage) (StatementClass, string) {
	var node struct {
		Query   map[string]json.RawMessage `json:"query"`
		Options []struct {
			DefElem struct {
				Defname string `json:"defname"`
			} `json:"DefElem"`
		} `json:"options"`
	}
	if err := json.Unmarshal(body, &node); err != nil {
		return StatementWrite, "undecodable EXPLAIN node (fail closed)"
	}

	analyze := false
	for _, option := range node.Options {
		if strings.EqualFold(option.DefElem.Defname, "analyze") {
			analyze = true
			break
		}
	}
	if !analyze {
		return StatementReadOnly, ""
	}

	nodeType, inner, err := singleNode(node.Query)
	if err != nil {
		return StatementWrite, "EXPLAIN ANALYZE with undecodable inner statement (fail closed)"
	}
	class, reason := classifyNode(nodeType, inner)
	if class == StatementReadOnly {
		return StatementReadOnly, ""
	}
	return class, "EXPLAIN ANALYZE executes the inner statement: " + reason
}

// classifyWrapped classifies the node stored under the given key (used for
// DECLARE CURSOR and PREPARE, which wrap an inner statement).
func classifyWrapped(body json.RawMessage, key string) (StatementClass, string) {
	var node map[string]json.RawMessage
	if err := json.Unmarshal(body, &node); err != nil {
		return StatementWrite, "undecodable statement node (fail closed)"
	}
	inner, ok := node[key]
	if !ok {
		return StatementWrite, "wrapped statement without inner query (fail closed)"
	}
	var wrapped map[string]json.RawMessage
	if err := json.Unmarshal(inner, &wrapped); err != nil {
		return StatementWrite, "undecodable inner statement (fail closed)"
	}
	nodeType, innerBody, err := singleNode(wrapped)
	if err != nil {
		return StatementWrite, "undecodable inner statement (fail closed)"
	}
	return classifyNode(nodeType, innerBody)
}

// findHazard walks a decoded parse subtree and returns a reason when it
// contains a data-modifying node (e.g. an INSERT inside a CTE) or a call to a
// side-effecting function (e.g. setval/nextval).
func findHazard(node any) string {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if description, ok := dataModifyingNodes[key]; ok {
				return description + " embedded in query (e.g. data-modifying CTE)"
			}
			if key == "FuncCall" {
				if name := sideEffectFunctionName(child); name != "" {
					return "call to side-effecting function " + name
				}
			}
			if reason := findHazard(child); reason != "" {
				return reason
			}
		}
	case []any:
		for _, child := range value {
			if reason := findHazard(child); reason != "" {
				return reason
			}
		}
	}
	return ""
}

// sideEffectFunctionName extracts the (unqualified, lowercased) function name
// from a FuncCall node and reports it when it is a known side-effecting
// function.
func sideEffectFunctionName(funcCall any) string {
	call, ok := funcCall.(map[string]any)
	if !ok {
		return ""
	}
	names, ok := call["funcname"].([]any)
	if !ok || len(names) == 0 {
		return ""
	}
	last, ok := names[len(names)-1].(map[string]any)
	if !ok {
		return ""
	}
	stringNode, ok := last["String"].(map[string]any)
	if !ok {
		return ""
	}
	name, ok := stringNode["sval"].(string)
	if !ok {
		return ""
	}
	name = strings.ToLower(name)
	if _, hazardous := sideEffectFunctions[name]; hazardous {
		return name
	}
	return ""
}

// singleNode unwraps the {"NodeType": {...}} single-key encoding used by the
// pg_query JSON parse tree.
func singleNode(node map[string]json.RawMessage) (string, json.RawMessage, error) {
	if len(node) != 1 {
		return "", nil, fmt.Errorf("expected exactly one node key, got %d", len(node))
	}
	for nodeType, body := range node {
		return nodeType, body, nil
	}
	return "", nil, fmt.Errorf("unreachable")
}

// statementText extracts the original text for one parsed statement using the
// parser-reported location and length, falling back to the whole input.
func statementText(sql string, raw rawStatement) string {
	start := raw.StmtLocation
	if start < 0 || start > len(sql) {
		start = 0
	}
	end := len(sql)
	if raw.StmtLen > 0 && start+raw.StmtLen <= len(sql) {
		end = start + raw.StmtLen
	}
	return strings.TrimSpace(sql[start:end])
}
