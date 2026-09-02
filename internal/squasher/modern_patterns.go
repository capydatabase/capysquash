package squasher

import (
	"fmt"
	"strings"

	"github.com/capy-base/pgsquash-engine/internal/types"
)

// ModernPatternRule defines optimization rules for modern PostgreSQL patterns
type ModernPatternRule struct {
	Name        string
	Priority    int // Higher number = higher priority
	Pattern     string
	AuthType    types.AuthPatternType
	Conflicts   []string    // Rule names that conflict with this one
	SafetyLevel SafetyLevel // Minimum safety level required
	Consolidate func([]*types.Statement) []*types.Statement
}

// ModernPatternOptimizer handles JWT v2, storage, and dynamic SQL patterns
type ModernPatternOptimizer struct {
	rules []ModernPatternRule
}

// NewModernPatternOptimizer creates an optimizer for modern PostgreSQL patterns
// NOTE: Many auth-specific rules are now handled by plugins (Clerk, Supabase, etc.)
// These rules provide fallback generic handling.
func NewModernPatternOptimizer() *ModernPatternOptimizer {
	optimizer := &ModernPatternOptimizer{
		rules: []ModernPatternRule{
			{
				Name:        "JWT_Organization_Policies",
				Priority:    100,
				AuthType:    types.AuthPatternJWT, // Generic JWT pattern
				Conflicts:   []string{"Auth_Function_Deduplication"},
				SafetyLevel: Standard,
				Consolidate: consolidateJWTV2OrgPolicies,
			},
			{
				Name:        "Session_Management",
				Priority:    90,
				AuthType:    types.AuthPatternSession, // Generic session pattern
				SafetyLevel: Conservative,
				Consolidate: consolidateNextAuthPolicies,
			},
			{
				Name:        "Vector_Index_Optimization",
				Priority:    85,
				Pattern:     "USING IVFFLAT",
				SafetyLevel: Standard,
				Consolidate: consolidateVectorIndexes,
			},
			{
				Name:        "Storage_Bucket_Policies",
				Priority:    80,
				AuthType:    types.AuthPatternStorage,
				SafetyLevel: Conservative,
				Consolidate: consolidateStoragePolicies,
			},
			{
				Name:        "Generated_Column_Optimization",
				Priority:    75,
				Pattern:     "GENERATED ALWAYS AS",
				SafetyLevel: Standard,
				Consolidate: consolidateGeneratedColumns,
			},
			{
				Name:        "Event_Sourcing_Patterns",
				Priority:    70,
				Pattern:     "EVENT_TYPE",
				SafetyLevel: Standard,
				Consolidate: consolidateEventSourcing,
			},
			{
				Name:        "Dynamic_Policy_Generation",
				Priority:    60,
				Pattern:     "EXECUTE format",
				SafetyLevel: Aggressive,
				Consolidate: consolidateDynamicPolicies,
			},
			{
				Name:        "Auth_Function_Deduplication",
				Priority:    50,
				AuthType:    types.AuthPatternJWT, // Generic JWT pattern
				Conflicts:   []string{"JWT_Organization_Policies"},
				SafetyLevel: Standard,
				Consolidate: consolidateAuthFunctions,
			},
		},
	}
	return optimizer
}

// ApplyModernOptimizations applies pattern-specific optimizations with priority and conflict handling
func (m *ModernPatternOptimizer) ApplyModernOptimizations(statements []*types.Statement, safetyLevel SafetyLevel) []*types.Statement {
	optimized := make([]*types.Statement, 0, len(statements))
	processed := make(map[int]bool)
	appliedRules := make(map[string]bool)

	// Sort rules by priority (highest first)
	sortedRules := make([]ModernPatternRule, len(m.rules))
	copy(sortedRules, m.rules)
	for i := 0; i < len(sortedRules)-1; i++ {
		for j := i + 1; j < len(sortedRules); j++ {
			if sortedRules[i].Priority < sortedRules[j].Priority {
				sortedRules[i], sortedRules[j] = sortedRules[j], sortedRules[i]
			}
		}
	}

	for _, rule := range sortedRules {
		// Check if rule meets safety level requirement
		if !meetsSafetyLevel(rule.SafetyLevel, safetyLevel) {
			continue
		}

		// Check for conflicts with already applied rules
		hasConflict := false
		for _, conflictRule := range rule.Conflicts {
			if appliedRules[conflictRule] {
				hasConflict = true
				break
			}
		}
		if hasConflict {
			continue
		}

		// Find statements matching this rule
		var matching []*types.Statement
		var indices []int

		for i, stmt := range statements {
			if processed[i] {
				continue
			}

			if matchesModernPattern(stmt, rule) {
				matching = append(matching, stmt)
				indices = append(indices, i)
			}
		}

		if len(matching) > 1 {
			// Apply consolidation rule
			consolidated := rule.Consolidate(matching)
			optimized = append(optimized, consolidated...)

			// Mark as processed and rule as applied
			for _, idx := range indices {
				processed[idx] = true
			}
			appliedRules[rule.Name] = true
		}
	}

	// Add remaining unprocessed statements
	for i, stmt := range statements {
		if !processed[i] {
			optimized = append(optimized, stmt)
		}
	}

	return optimized
}

// meetsSafetyLevel checks if the rule meets the minimum safety level
func meetsSafetyLevel(ruleLevel, currentLevel SafetyLevel) bool {
	levelOrder := map[SafetyLevel]int{
		Conservative: 1,
		Standard:     2,
		Aggressive:   3,
	}

	return levelOrder[currentLevel] >= levelOrder[ruleLevel]
}

// matchesModernPattern checks if a statement matches a modern pattern rule
func matchesModernPattern(stmt *types.Statement, rule ModernPatternRule) bool {
	if rule.AuthType != "" && stmt.AuthPattern == rule.AuthType {
		return true
	}

	if rule.Pattern != "" && strings.Contains(strings.ToUpper(stmt.SQL), strings.ToUpper(rule.Pattern)) {
		return true
	}

	return false
}

// consolidateJWTV2OrgPolicies consolidates JWT v2 organization-aware policies
func consolidateJWTV2OrgPolicies(statements []*types.Statement) []*types.Statement {
	if len(statements) <= 1 {
		return statements
	}

	// Group by table and policy pattern
	policyGroups := make(map[string][]*types.Statement)

	for _, stmt := range statements {
		// Check for JWT-based org policies (generic pattern, plugins set specific strings)
		if stmt.ObjectType == types.TypePolicy &&
			(stmt.AuthPattern == types.AuthPatternJWT || strings.Contains(string(stmt.AuthPattern), "jwt")) {
			// Extract table name from policy
			tableName := extractPolicyTable(stmt.SQL)
			key := fmt.Sprintf("%s_org_policy", tableName)
			policyGroups[key] = append(policyGroups[key], stmt)
		}
	}

	var consolidated []*types.Statement

	for groupKey, policies := range policyGroups {
		if len(policies) > 1 {
			// Create a comprehensive organization policy
			consolidatedPolicy := createConsolidatedOrgPolicy(policies, groupKey)
			consolidated = append(consolidated, consolidatedPolicy)
		} else {
			consolidated = append(consolidated, policies...)
		}
	}

	return consolidated
}

// consolidateStoragePolicies consolidates storage bucket policies
func consolidateStoragePolicies(statements []*types.Statement) []*types.Statement {
	if len(statements) <= 1 {
		return statements
	}

	// Group by bucket name
	bucketGroups := make(map[string][]*types.Statement)

	for _, stmt := range statements {
		if stmt.AuthPattern == types.AuthPatternStorage {
			bucketName := extractBucketName(stmt.SQL)
			if bucketName == "" {
				bucketName = "default"
			}
			bucketGroups[bucketName] = append(bucketGroups[bucketName], stmt)
		}
	}

	var consolidated []*types.Statement

	for bucketName, policies := range bucketGroups {
		if len(policies) > 1 {
			// Create comprehensive bucket policy
			consolidatedPolicy := createConsolidatedStoragePolicy(policies, bucketName)
			consolidated = append(consolidated, consolidatedPolicy)
		} else {
			consolidated = append(consolidated, policies...)
		}
	}

	return consolidated
}

// consolidateDynamicPolicies handles EXECUTE format() patterns
func consolidateDynamicPolicies(statements []*types.Statement) []*types.Statement {
	if len(statements) <= 1 {
		return statements
	}

	// For dynamic SQL, we focus on documenting the pattern rather than changing it
	// since the dynamic nature makes consolidation complex
	for _, stmt := range statements {
		if stmt.IsDynamic {
			stmt.Comments = append(stmt.Comments,
				"Dynamic SQL detected - review for consolidation opportunities",
				"Pattern generates multiple similar objects",
			)
		}
	}

	return statements
}

// consolidateAuthFunctions consolidates authentication-related functions
func consolidateAuthFunctions(statements []*types.Statement) []*types.Statement {
	if len(statements) <= 1 {
		return statements
	}

	// Group identical function definitions
	funcGroups := make(map[string][]*types.Statement)

	for _, stmt := range statements {
		// Check for JWT-based auth functions (generic pattern, plugins set specific strings)
		if stmt.ObjectType == types.TypeFunction &&
			(stmt.AuthPattern == types.AuthPatternJWT || strings.Contains(string(stmt.AuthPattern), "jwt") ||
				strings.Contains(string(stmt.AuthPattern), "auth")) {
			// Use function signature as key
			funcSig := extractFunctionSignature(stmt)
			funcGroups[funcSig] = append(funcGroups[funcSig], stmt)
		}
	}

	var consolidated []*types.Statement

	for _, functions := range funcGroups {
		if len(functions) > 1 {
			// Keep the most complete version
			best := functions[0]
			for _, fn := range functions[1:] {
				if len(fn.SQL) > len(best.SQL) {
					best = fn
				}
			}

			best.Comments = append(best.Comments,
				fmt.Sprintf("Consolidated %d duplicate function definitions", len(functions)),
			)
			consolidated = append(consolidated, best)
		} else {
			consolidated = append(consolidated, functions...)
		}
	}

	return consolidated
}

// Helper functions

func extractPolicyTable(sql string) string {
	// Extract table name from CREATE POLICY ... ON table_name
	parts := strings.Fields(strings.ToUpper(sql))
	for i, part := range parts {
		if part == "ON" && i+1 < len(parts) {
			return strings.ToLower(parts[i+1])
		}
	}
	return "unknown_table"
}

func extractBucketName(sql string) string {
	// Extract bucket name from bucket_id = 'bucket_name'
	if strings.Contains(sql, "bucket_id") {
		parts := strings.Split(sql, "bucket_id")
		if len(parts) > 1 {
			// Find the quoted string after bucket_id =
			remaining := parts[1]
			start := strings.Index(remaining, "'")
			if start == -1 {
				start = strings.Index(remaining, "\"")
			}
			if start != -1 {
				end := strings.Index(remaining[start+1:], remaining[start:start+1])
				if end != -1 {
					return remaining[start+1 : start+1+end]
				}
			}
		}
	}
	return ""
}

func extractFunctionSignature(stmt *types.Statement) string {
	// Extract function name and parameters for deduplication
	sql := strings.ToUpper(stmt.SQL)
	if strings.Contains(sql, "CREATE FUNCTION") || strings.Contains(sql, "CREATE OR REPLACE FUNCTION") {
		// Find the function name and parameters
		_, after, ok := strings.Cut(sql, "FUNCTION")
		if ok {
			remaining := after // Skip "FUNCTION"
			parenStart := strings.Index(remaining, "(")
			if parenStart != -1 {
				parenEnd := strings.Index(remaining, ")")
				if parenEnd != -1 && parenEnd > parenStart {
					return strings.TrimSpace(remaining[:parenEnd+1])
				}
			}
		}
	}
	return stmt.ObjectName
}

func createConsolidatedOrgPolicy(policies []*types.Statement, groupKey string) *types.Statement {
	// Create a new comprehensive policy statement
	// Use the auth pattern from the first policy (will be vendor-specific string from plugin)
	authPattern := types.AuthPatternJWT
	if len(policies) > 0 && policies[0].AuthPattern != "" {
		authPattern = policies[0].AuthPattern
	}

	consolidated := &types.Statement{
		ObjectType:  types.TypePolicy,
		ObjectName:  groupKey + "_consolidated",
		Operation:   types.OpCreate,
		AuthPattern: authPattern,
		Comments:    []string{fmt.Sprintf("Consolidated %d organization policies", len(policies))},
	}

	// Build comprehensive policy SQL (simplified example)
	tableName := extractPolicyTable(policies[0].SQL)
	consolidated.SQL = fmt.Sprintf(`CREATE POLICY "%s_comprehensive_org_access" ON %s
  FOR ALL TO authenticated
  USING (
    -- Own records
    user_id = auth.jwt()->>'sub'
    OR
    -- Organization members (JWT v2)
    auth.jwt()->'o'->>'id' = organization_id
    OR
    -- Organization admins (JWT v2)
    auth.jwt()->'o'->>'role' IN ('admin', 'owner')
    OR
    -- Global admins
    auth.jwt()->>'role' = 'admin'
  )
  WITH CHECK (
    -- Same logic for modifications
    user_id = auth.jwt()->>'sub'
    OR auth.jwt()->'o'->>'role' IN ('admin', 'owner')
    OR auth.jwt()->>'role' = 'admin'
  );`, consolidated.ObjectName, tableName)

	return consolidated
}

func createConsolidatedStoragePolicy(policies []*types.Statement, bucketName string) *types.Statement {
	consolidated := &types.Statement{
		ObjectType:  types.TypePolicy,
		ObjectName:  bucketName + "_comprehensive_storage_policy",
		Operation:   types.OpCreate,
		AuthPattern: types.AuthPatternStorage,
		Comments:    []string{fmt.Sprintf("Consolidated %d storage policies for bucket: %s", len(policies), bucketName)},
	}

	// Build comprehensive storage policy SQL
	consolidated.SQL = fmt.Sprintf(`CREATE POLICY "%s_comprehensive_access" ON storage.objects
  FOR ALL TO authenticated
  USING (
    bucket_id = '%s'
    AND (
      -- User folder access (JWT v2 compatible)
      (storage.foldername(name))[1] = auth.jwt()->>'sub'
      OR
      -- Organization shared access (JWT v2)
      (storage.foldername(name))[1] = auth.jwt()->'o'->>'id'
      OR
      -- Admin access
      auth.jwt()->>'role' = 'admin'
      OR auth.jwt()->'o'->>'role' IN ('admin', 'owner')
    )
  )
  WITH CHECK (
    bucket_id = '%s'
    AND (
      (storage.foldername(name))[1] = auth.jwt()->>'sub'
      OR auth.jwt()->'o'->>'role' IN ('admin', 'owner')
      OR auth.jwt()->>'role' = 'admin'
    )
  );`, consolidated.ObjectName, bucketName, bucketName)

	return consolidated
}

// New consolidation functions for modern PostgreSQL patterns

// consolidateNextAuthPolicies consolidates NextAuth session management policies
func consolidateNextAuthPolicies(statements []*types.Statement) []*types.Statement {
	if len(statements) <= 1 {
		return statements
	}

	// NextAuth typically involves accounts, sessions, and users tables
	tableGroups := make(map[string][]*types.Statement)

	for _, stmt := range statements {
		// Check for session-based auth patterns (NextAuth would be detected as session pattern)
		if stmt.AuthPattern == types.AuthPatternSession || strings.Contains(string(stmt.AuthPattern), "nextauth") {
			if strings.Contains(strings.ToLower(stmt.SQL), "accounts") {
				tableGroups["accounts"] = append(tableGroups["accounts"], stmt)
			} else if strings.Contains(strings.ToLower(stmt.SQL), "sessions") {
				tableGroups["sessions"] = append(tableGroups["sessions"], stmt)
			} else if strings.Contains(strings.ToLower(stmt.SQL), "users") {
				tableGroups["users"] = append(tableGroups["users"], stmt)
			}
		}
	}

	var consolidated []*types.Statement

	for table, statements := range tableGroups {
		if len(statements) > 1 {
			consolidatedPolicy := createConsolidatedNextAuthPolicy(statements, table)
			consolidated = append(consolidated, consolidatedPolicy)
		} else {
			consolidated = append(consolidated, statements...)
		}
	}

	return consolidated
}

// consolidateVectorIndexes consolidates vector index operations
func consolidateVectorIndexes(statements []*types.Statement) []*types.Statement {
	if len(statements) <= 1 {
		return statements
	}

	// Group by table and column
	indexGroups := make(map[string][]*types.Statement)

	for _, stmt := range statements {
		if stmt.ObjectType == types.TypeIndex &&
			(strings.Contains(strings.ToUpper(stmt.SQL), "USING IVFFLAT") ||
				strings.Contains(strings.ToUpper(stmt.SQL), "USING HNSW")) {

			// Extract table and column from index statement
			tableName := extractIndexTable(stmt.SQL)
			key := fmt.Sprintf("%s_vector_index", tableName)
			indexGroups[key] = append(indexGroups[key], stmt)
		}
	}

	var consolidated []*types.Statement

	for groupKey, indexes := range indexGroups {
		if len(indexes) > 1 {
			consolidatedIndex := createConsolidatedVectorIndex(indexes, groupKey)
			consolidated = append(consolidated, consolidatedIndex)
		} else {
			consolidated = append(consolidated, indexes...)
		}
	}

	return consolidated
}

// consolidateGeneratedColumns consolidates generated column operations
func consolidateGeneratedColumns(statements []*types.Statement) []*types.Statement {
	if len(statements) <= 1 {
		return statements
	}

	// Group by table
	tableGroups := make(map[string][]*types.Statement)

	for _, stmt := range statements {
		if strings.Contains(strings.ToUpper(stmt.SQL), "GENERATED ALWAYS AS") {
			tableName := extractAlterTable(stmt.SQL)
			if tableName != "" {
				tableGroups[tableName] = append(tableGroups[tableName], stmt)
			}
		}
	}

	var consolidated []*types.Statement

	for table, statements := range tableGroups {
		if len(statements) > 1 {
			consolidatedStmt := createConsolidatedGeneratedColumns(statements, table)
			consolidated = append(consolidated, consolidatedStmt)
		} else {
			consolidated = append(consolidated, statements...)
		}
	}

	return consolidated
}

// consolidateEventSourcing consolidates event sourcing patterns
func consolidateEventSourcing(statements []*types.Statement) []*types.Statement {
	if len(statements) <= 1 {
		return statements
	}

	// Group event sourcing related statements
	var eventTables []*types.Statement
	var eventFunctions []*types.Statement
	var eventTriggers []*types.Statement

	for _, stmt := range statements {
		sqlLower := strings.ToLower(stmt.SQL)
		if strings.Contains(sqlLower, "event_type") && strings.Contains(sqlLower, "event_data") {
			switch stmt.ObjectType {
			case types.TypeTable:
				eventTables = append(eventTables, stmt)
			case types.TypeFunction:
				eventFunctions = append(eventFunctions, stmt)
			case types.TypeTrigger:
				eventTriggers = append(eventTriggers, stmt)
			}
		}
	}

	var consolidated []*types.Statement

	// Consolidate each type separately
	if len(eventTables) > 1 {
		consolidatedTable := createConsolidatedEventTable(eventTables)
		consolidated = append(consolidated, consolidatedTable)
	} else {
		consolidated = append(consolidated, eventTables...)
	}

	if len(eventFunctions) > 1 {
		consolidatedFunction := createConsolidatedEventFunction(eventFunctions)
		consolidated = append(consolidated, consolidatedFunction)
	} else {
		consolidated = append(consolidated, eventFunctions...)
	}

	consolidated = append(consolidated, eventTriggers...)

	return consolidated
}

// Helper functions for new consolidation patterns

func createConsolidatedNextAuthPolicy(statements []*types.Statement, table string) *types.Statement {
	// Use the auth pattern from the first statement (will be vendor-specific string from plugin)
	authPattern := types.AuthPatternSession
	if len(statements) > 0 && statements[0].AuthPattern != "" {
		authPattern = statements[0].AuthPattern
	}

	consolidated := &types.Statement{
		ObjectType:  types.TypePolicy,
		ObjectName:  table + "_nextauth_comprehensive",
		Operation:   types.OpCreate,
		AuthPattern: authPattern,
		Comments:    []string{fmt.Sprintf("Consolidated NextAuth policies for %s table", table)},
	}

	consolidated.SQL = fmt.Sprintf(`CREATE POLICY "%s_nextauth_access" ON %s
  FOR ALL TO authenticated
  USING (
    -- NextAuth session-based access
    EXISTS (
      SELECT 1 FROM sessions s
      WHERE s.user_id = %s.user_id
      AND s.expires > NOW()
      AND s.session_token = current_setting('app.session_token', true)
    )
  );`, consolidated.ObjectName, table, table)

	return consolidated
}

func createConsolidatedVectorIndex(indexes []*types.Statement, groupKey string) *types.Statement {
	consolidated := &types.Statement{
		ObjectType: types.TypeIndex,
		ObjectName: groupKey + "_optimized",
		Operation:  types.OpCreate,
		Comments:   []string{fmt.Sprintf("Consolidated %d vector indexes", len(indexes))},
	}

	// Use the most performant index method (HNSW > IVFFlat)
	indexMethod := "IVFFLAT"
	for _, idx := range indexes {
		if strings.Contains(strings.ToUpper(idx.SQL), "USING HNSW") {
			indexMethod = "HNSW"
			break
		}
	}

	tableName := extractIndexTable(indexes[0].SQL)
	consolidated.SQL = fmt.Sprintf(`CREATE INDEX "%s" ON %s
  USING %s (embedding vector_cosine_ops)
  WITH (m = 16, ef_construction = 64);`, consolidated.ObjectName, tableName, indexMethod)

	return consolidated
}

func createConsolidatedGeneratedColumns(statements []*types.Statement, table string) *types.Statement {
	consolidated := &types.Statement{
		ObjectType: types.TypeTable,
		ObjectName: table + "_generated_columns",
		Operation:  types.OpAlter,
		Comments:   []string{fmt.Sprintf("Consolidated %d generated columns for %s", len(statements), table)},
	}

	// Combine all generated columns into a single ALTER statement
	var alterClauses []string
	for _, stmt := range statements {
		// Extract the ADD COLUMN clause
		sql := strings.ToUpper(stmt.SQL)
		if strings.Contains(sql, "ADD COLUMN") {
			start := strings.Index(sql, "ADD COLUMN")
			clause := stmt.SQL[start:]
			alterClauses = append(alterClauses, clause)
		}
	}

	consolidated.SQL = fmt.Sprintf("ALTER TABLE %s %s;", table, strings.Join(alterClauses, ", "))

	return consolidated
}

func createConsolidatedEventTable(tables []*types.Statement) *types.Statement {
	// Take the most comprehensive table definition
	best := tables[0]
	for _, table := range tables[1:] {
		if len(table.SQL) > len(best.SQL) {
			best = table
		}
	}

	best.Comments = append(best.Comments,
		fmt.Sprintf("Consolidated %d event sourcing table definitions", len(tables)))

	return best
}

func createConsolidatedEventFunction(functions []*types.Statement) *types.Statement {
	// Take the most recent function definition
	best := functions[len(functions)-1]
	best.Comments = append(best.Comments,
		fmt.Sprintf("Consolidated %d event sourcing functions", len(functions)))

	return best
}

// Helper extraction functions

func extractIndexTable(sql string) string {
	// Extract table name from CREATE INDEX ... ON table_name
	parts := strings.Fields(strings.ToUpper(sql))
	for i, part := range parts {
		if part == "ON" && i+1 < len(parts) {
			return strings.ToLower(parts[i+1])
		}
	}
	return "unknown_table"
}

func extractAlterTable(sql string) string {
	// Extract table name from ALTER TABLE table_name
	parts := strings.Fields(strings.ToUpper(sql))
	for i, part := range parts {
		if part == "TABLE" && i+1 < len(parts) {
			return strings.ToLower(parts[i+1])
		}
	}
	return ""
}
