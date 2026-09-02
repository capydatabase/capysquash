package squasher

import (
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/parser"
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/capysquash/pgsquash-engine/internal/utils"
	pg_query "github.com/pganalyze/pg_query_go/v6"
)

// UnifiedDependencyResolver provides comprehensive dependency resolution
// for both object lifecycle analysis and SQL consolidation phases
type UnifiedDependencyResolver struct {
	// Category and type ordering rules from EnhancedDependencyResolver
	categoryOrder map[types.Category]int
	typeOrder     map[types.ObjectType]int

	// SQL analysis caches from DependencyResolver
	tableDeps    map[string][]string
	schemaDeps   map[string][]string
	columnDeps   map[string][]string
	functionDeps map[string][]string
}

// NewUnifiedDependencyResolver creates a new unified dependency resolver
func NewUnifiedDependencyResolver() *UnifiedDependencyResolver {
	resolver := &UnifiedDependencyResolver{
		tableDeps:    make(map[string][]string),
		schemaDeps:   make(map[string][]string),
		columnDeps:   make(map[string][]string),
		functionDeps: make(map[string][]string),
	}

	// Define strict category ordering for PostgreSQL DDL
	resolver.categoryOrder = map[types.Category]int{
		types.CategoryExtensions:  0, // Extensions first
		types.CategoryFoundation:  1, // Schemas, types, domains, functions, tables
		types.CategoryConstraints: 2, // Primary keys, unique constraints
		types.CategoryIndexes:     3, // Indexes (after constraints)
		types.CategoryTriggers:    4, // Triggers (after functions and tables)
		types.CategorySecurity:    5, // RLS policies, grants (Before comments)
		types.CategoryComments:    6, // COMMENT ON statements (after objects exist, including policies)
		types.CategoryData:        7, // INSERT/UPDATE data operations
	}

	// Define object type ordering within categories
	resolver.typeOrder = map[types.ObjectType]int{
		// Extensions first
		types.TypeExtension: 0,

		// Foundation objects - types must come before tables that use them
		types.TypeSchema:    10,
		types.TypeEnum:      11, // ENUMs must come before tables that use them
		types.TypeComposite: 12, // Composite types before tables
		types.TypeDomain:    13, // Domains before tables
		types.TypeType:      14, // Generic types before tables
		types.TypeFunction:  15, // Functions before tables (for CHECK constraints)
		types.TypeSequence:  16,
		types.TypeTable:     17, // Tables after functions (may reference functions in CHECK constraints)

		// Constraints
		types.TypeConstraint: 20,

		// Indexes
		types.TypeIndex: 30,

		// Triggers
		types.TypeTrigger: 50,

		// Views
		types.TypeView: 60,

		// Security
		types.TypePolicy: 70,
		types.TypeRole:   71,

		// Publications, etc.
		types.TypePublication: 80,
		types.TypeComment:     90,
		// NOTE: DO blocks containing CREATE TYPE should be treated as types (order 11)
		// This is handled dynamically in getTypeOrder()
		types.TypeDoBlock: 91,
		types.TypeUnknown: 100,
	}

	return resolver
}

// ResolveLifecycleDependencies performs advanced dependency resolution for object lifecycles
// This replaces the functionality from EnhancedDependencyResolver
func (udr *UnifiedDependencyResolver) ResolveLifecycleDependencies(
	graph *tracking.DependencyGraph,
	lifecycles map[string]*tracking.ObjectLifecycle,
) ([]tracking.ObjectID, error) {
	utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Starting unified dependency resolution for %d objects", len(lifecycles))

	// Step 1: Group objects by category
	categoryGroups := udr.groupLifecyclesByCategory(lifecycles)

	// Step 2: Resolve dependencies within each category
	var orderedObjects []tracking.ObjectID
	for categoryOrder := range 8 {
		category := udr.getCategoryByOrder(categoryOrder)
		if objects, exists := categoryGroups[category]; exists {
			utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Processing category %s with %d objects", category, len(objects))

			// Sort within category using topological sort with cycle breaking
			sortedObjects, err := udr.resolveLifecycleWithinCategory(objects, category, graph)
			if err != nil {
				utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Warning: Error in category %s: %v", category, err)
				// Continue with best effort (use lifecycle-aware sorting)
				sortedObjects = udr.fallbackSortLifecycles(objects, lifecycles)
			}

			orderedObjects = append(orderedObjects, sortedObjects...)
		}
	}

	// Step 3: Final validation
	if err := udr.validateLifecycleOrdering(orderedObjects, lifecycles); err != nil {
		utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Final validation warnings: %v", err)
	}

	utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Unified dependency resolution completed with %d objects ordered", len(orderedObjects))
	return orderedObjects, nil
}

// SortConsolidationResults sorts consolidated SQL results by their dependencies within a category
// This replaces the functionality from DependencyResolver
func (udr *UnifiedDependencyResolver) SortConsolidationResults(
	categoryObjects map[string]*tracking.ConsolidationResult,
	category types.Category,
	lifecycles map[string]*tracking.ObjectLifecycle,
) []*tracking.ConsolidationResult {

	if len(categoryObjects) <= 1 {
		// Single object or empty - keep deterministic key ordering
		var results []*tracking.ConsolidationResult
		keys := make([]string, 0, len(categoryObjects))
		for key := range categoryObjects {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			results = append(results, categoryObjects[key])
		}
		return results
	}

	utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Resolving SQL dependencies for %d objects in category %s", len(categoryObjects), category)

	// Step 1: Analyze each object's dependencies and what it provides
	dependencies := make(map[string]*SQLDependencyInfo)
	for key, result := range categoryObjects {
		deps := udr.analyzeSQLDependencies(key, result, category, lifecycles)
		dependencies[key] = deps
	}

	// Step 2: Build dependency graph and topologically sort
	sorted := udr.topologicalSortSQL(dependencies, category)

	// Step 3: Convert back to results array
	var results []*tracking.ConsolidationResult
	for _, key := range sorted {
		if result, exists := categoryObjects[key]; exists {
			results = append(results, result)
		}
	}

	utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Sorted %d SQL objects by dependencies in category %s", len(results), category)
	return results
}

// SQLDependencyInfo holds dependency information for an object (renamed to avoid conflict)
type SQLDependencyInfo struct {
	ObjectKey     string
	Result        *tracking.ConsolidationResult
	Dependencies  []string // Objects this depends on
	Provides      []string // Objects this provides/creates
	RequiredFirst bool     // Must be first in category (extensions, schemas)
	RequiredLast  bool     // Must be last in category (data operations)
}

// groupLifecyclesByCategory groups objects by their PostgreSQL category
func (udr *UnifiedDependencyResolver) groupLifecyclesByCategory(lifecycles map[string]*tracking.ObjectLifecycle) map[types.Category][]tracking.ObjectID {
	groups := make(map[types.Category][]tracking.ObjectID)

	for _, lifecycle := range lifecycles {
		objectID := tracking.ObjectID{
			Type: types.ObjectType(lifecycle.Name), // Simplified mapping
			Name: lifecycle.Name,
		}

		category := udr.determineLifecycleCategory(lifecycle)
		groups[category] = append(groups[category], objectID)
	}

	return groups
}

// determineLifecycleCategory determines the PostgreSQL category for an object lifecycle
func (udr *UnifiedDependencyResolver) determineLifecycleCategory(lifecycle *tracking.ObjectLifecycle) types.Category {
	// Use existing category if available
	if lifecycle.Category != "" {
		return lifecycle.Category
	}

	// Fallback category determination based on object type from lifecycle
	if lifecycle.Type == types.TypeData || lifecycle.Type == types.TypeDoBlock {
		return types.CategoryData
	}

	objectType := strings.ToUpper(lifecycle.Name)
	switch {
	case strings.Contains(objectType, "EXTENSION"):
		return types.CategoryExtensions
	case strings.Contains(objectType, "SCHEMA") || strings.Contains(objectType, "TYPE") ||
		strings.Contains(objectType, "TABLE") || strings.Contains(objectType, "SEQUENCE") ||
		strings.Contains(objectType, "FUNCTION") || strings.Contains(objectType, "PROCEDURE"):
		// Functions must be in Foundation category to be created before tables
		// (tables may reference functions in CHECK constraints)
		return types.CategoryFoundation
	case strings.Contains(objectType, "INDEX"):
		return types.CategoryIndexes
	case strings.Contains(objectType, "TRIGGER"):
		return types.CategoryTriggers
	case strings.Contains(objectType, "VIEW"):
		return types.CategoryFoundation
	case strings.Contains(objectType, "POLICY") || strings.Contains(objectType, "GRANT"):
		return types.CategorySecurity
	default:
		return types.CategoryFoundation
	}
}

// resolveLifecycleWithinCategory resolves dependencies within a specific category for lifecycles
func (udr *UnifiedDependencyResolver) resolveLifecycleWithinCategory(
	objects []tracking.ObjectID,
	category types.Category,
	graph *tracking.DependencyGraph,
) ([]tracking.ObjectID, error) {
	if len(objects) <= 1 {
		return objects, nil
	}

	// Create sub-graph for this category
	subGraph := udr.createSubGraph(objects, graph)

	// Try topological sort
	if sorted, err := subGraph.TopologicalSort(); err == nil {
		return sorted, nil
	}

	// If cycles exist, break them intelligently
	return udr.breakCyclesAndSort(objects, subGraph, category)
}

// createSubGraph creates a dependency subgraph for specific objects
func (udr *UnifiedDependencyResolver) createSubGraph(objects []tracking.ObjectID, originalGraph *tracking.DependencyGraph) *tracking.DependencyGraph {
	subGraph := tracking.NewDependencyGraph()

	// Add all objects as nodes
	for _, obj := range objects {
		subGraph.AddNode(obj)
	}

	// Add edges only between objects in this subgraph
	allNodes := originalGraph.GetAllNodes()
	for _, obj := range objects {
		if node, exists := allNodes[obj]; exists {
			for _, dep := range node.Dependencies {
				// Only add edge if both objects are in this subgraph
				if udr.objectInList(dep, objects) {
					subGraph.AddEdge(obj, dep)
				}
			}
		}
	}

	return subGraph
}

// breakCyclesAndSort intelligently breaks cycles and provides best-effort sorting
func (udr *UnifiedDependencyResolver) breakCyclesAndSort(
	objects []tracking.ObjectID,
	subGraph *tracking.DependencyGraph,
	category types.Category,
) ([]tracking.ObjectID, error) {
	utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Breaking cycles in category %s with %d objects", category, len(objects))

	// Detect cycles
	cycles := subGraph.DetectCycles()
	if len(cycles) == 0 {
		// No cycles, should not happen, but handle gracefully
		return udr.fallbackSortLifecycles(objects, nil), nil
	}

	// Strategy 1: Remove least important edges in cycles
	modifiedGraph := udr.removeCyclicEdges(subGraph, cycles)
	if sorted, err := modifiedGraph.TopologicalSort(); err == nil {
		utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Successfully broke %d cycles in category %s", len(cycles), category)
		return sorted, nil
	}

	// Strategy 2: Fallback to statement-type ordering
	return udr.fallbackSortLifecycles(objects, nil), errors.NewError(
		errors.ErrorCodeDependencyError,
		"cycles broken with fallback sorting",
		errors.SeverityWarning,
		errors.CategoryDependency,
	).WithAdditional("category", string(category)).WithCanContinue(true)
}

// removeCyclicEdges removes edges that create cycles, prioritizing less important dependencies
func (udr *UnifiedDependencyResolver) removeCyclicEdges(graph *tracking.DependencyGraph, cycles [][]tracking.ObjectID) *tracking.DependencyGraph {
	modifiedGraph := udr.cloneGraph(graph)

	for _, cycle := range cycles {
		// Find the least important edge to remove
		edgeToRemove := udr.findLeastImportantLifecycleEdge(cycle)
		if edgeToRemove.from.Name != "" && edgeToRemove.to.Name != "" {
			udr.removeGraphEdge(modifiedGraph, edgeToRemove.from, edgeToRemove.to)
			utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Removed cyclic edge: %s -> %s", edgeToRemove.from.Name, edgeToRemove.to.Name)
		}
	}

	return modifiedGraph
}

// Edge represents a dependency edge
type Edge struct {
	from, to tracking.ObjectID
	weight   int // Lower weight = less important, easier to remove
}

// findLeastImportantLifecycleEdge finds the edge in a cycle that's safest to remove
func (udr *UnifiedDependencyResolver) findLeastImportantLifecycleEdge(cycle []tracking.ObjectID) Edge {
	var leastImportant Edge
	minWeight := 1000

	for i := range cycle {
		from := cycle[i]
		to := cycle[(i+1)%len(cycle)]

		weight := udr.calculateLifecycleEdgeWeight(from, to)
		if weight < minWeight {
			minWeight = weight
			leastImportant = Edge{from: from, to: to, weight: weight}
		}
	}

	return leastImportant
}

// calculateLifecycleEdgeWeight calculates the importance of a dependency edge
func (udr *UnifiedDependencyResolver) calculateLifecycleEdgeWeight(from, to tracking.ObjectID) int {
	weight := 100 // Base weight

	// Critical dependencies (never remove if possible)
	if udr.isCriticalLifecycleDependency(from, to) {
		weight += 500
	}

	// Schema dependencies are important
	if strings.Contains(string(to.Type), "SCHEMA") || strings.Contains(string(from.Type), "SCHEMA") {
		weight += 200
	}

	// Table to table dependencies are important
	if strings.Contains(string(from.Type), "TABLE") && strings.Contains(string(to.Type), "TABLE") {
		weight += 150
	}

	// Function to function dependencies can often be reordered
	if strings.Contains(string(from.Type), "FUNCTION") && strings.Contains(string(to.Type), "FUNCTION") {
		weight -= 50
	}

	// View dependencies are often safe to reorder
	if strings.Contains(string(from.Type), "VIEW") || strings.Contains(string(to.Type), "VIEW") {
		weight -= 30
	}

	return weight
}

// isCriticalLifecycleDependency checks if a dependency is critical and should not be removed
func (udr *UnifiedDependencyResolver) isCriticalLifecycleDependency(from, to tracking.ObjectID) bool {
	// Foreign key relationships
	if strings.Contains(strings.ToLower(from.Name), "foreign") ||
		strings.Contains(strings.ToLower(to.Name), "foreign") {
		return true
	}

	// Extension dependencies
	if strings.Contains(string(to.Type), "EXTENSION") {
		return true
	}

	// Schema dependencies
	if strings.Contains(string(to.Type), "SCHEMA") && !strings.Contains(string(from.Type), "SCHEMA") {
		return true
	}

	return false
}

// fallbackSortLifecycles provides object-type based sorting when topological sort fails
// Uses lifecycle-aware type ordering to handle special cases like DO blocks with CREATE TYPE
func (udr *UnifiedDependencyResolver) fallbackSortLifecycles(objects []tracking.ObjectID, lifecycles map[string]*tracking.ObjectLifecycle) []tracking.ObjectID {
	utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Using fallback sorting for %d lifecycle objects", len(objects))

	// Sort by object type order, then by name for stability
	sort.Slice(objects, func(i, j int) bool {
		// Get lifecycles for special case handling
		var lifecycleI, lifecycleJ *tracking.ObjectLifecycle
		if lifecycles != nil {
			keyI := fmt.Sprintf("%s:%s", objects[i].Type, objects[i].Name)
			keyJ := fmt.Sprintf("%s:%s", objects[j].Type, objects[j].Name)
			lifecycleI = lifecycles[keyI]
			lifecycleJ = lifecycles[keyJ]
		}

		// First, sort by type order (lifecycle-aware for DO blocks)
		orderI := udr.getTypeOrderForLifecycle(objects[i].Type, lifecycleI)
		orderJ := udr.getTypeOrderForLifecycle(objects[j].Type, lifecycleJ)

		if orderI != orderJ {
			return orderI < orderJ
		}

		// Then by name for stability
		return objects[i].Name < objects[j].Name
	})

	return objects
}

// validateLifecycleOrdering performs final validation on the ordered objects
func (udr *UnifiedDependencyResolver) validateLifecycleOrdering(
	orderedObjects []tracking.ObjectID,
	lifecycles map[string]*tracking.ObjectLifecycle,
) error {
	var warnings []string

	// Check for any obvious ordering violations
	seenObjects := make(map[string]bool)

	for _, obj := range orderedObjects {
		objKey := fmt.Sprintf("%s:%s", obj.Type, obj.Name)

		// Check if this object's dependencies have been seen
		if lifecycle, exists := lifecycles[objKey]; exists {
			for _, dep := range lifecycle.Dependencies {
				depKey := fmt.Sprintf("%s:%s", dep.DependsOn.Type, dep.DependsOn.Name)
				if !seenObjects[depKey] {
					warnings = append(warnings, fmt.Sprintf("Object %s depends on %s which appears later", objKey, depKey))
				}
			}
		}

		seenObjects[objKey] = true
	}

	if len(warnings) > 0 {
		return errors.NewError(
			errors.ErrorCodeDependencyError,
			fmt.Sprintf("ordering validation warnings: %s", strings.Join(warnings, "; ")),
			errors.SeverityWarning,
			errors.CategoryDependency,
		).WithCanContinue(true)
	}

	return nil
}

// Helper functions

func (udr *UnifiedDependencyResolver) getCategoryByOrder(order int) types.Category {
	for category, categoryOrder := range udr.categoryOrder {
		if categoryOrder == order {
			return category
		}
	}
	return types.CategoryFoundation
}

func (udr *UnifiedDependencyResolver) getTypeOrder(objectType types.ObjectType) int {
	if order, exists := udr.typeOrder[objectType]; exists {
		return order
	}
	return 100 // Default order
}

func (udr *UnifiedDependencyResolver) getTypeOrderForLifecycle(objectType types.ObjectType, lifecycle *tracking.ObjectLifecycle) int {
	// Special handling for DO blocks that contain CREATE TYPE - treat them as types
	if objectType == types.TypeDoBlock && lifecycle != nil {
		// Check if any of the lifecycle's history contains CREATE TYPE
		for _, event := range lifecycle.History {
			sqlUpper := strings.ToUpper(event.Statement.SQL)
			if strings.Contains(sqlUpper, "CREATE TYPE") && strings.Contains(sqlUpper, "AS ENUM") {
				// This is a DO block wrapping a CREATE TYPE - prioritize it like an enum
				utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("DO block contains CREATE TYPE, treating as enum (order 11 instead of 91)")
				return 11 // Same as TypeEnum
			}
		}
	}

	return udr.getTypeOrder(objectType)
}

func (udr *UnifiedDependencyResolver) objectInList(obj tracking.ObjectID, list []tracking.ObjectID) bool {
	for _, item := range list {
		if item.Type == obj.Type && item.Name == obj.Name {
			return true
		}
	}
	return false
}

func (udr *UnifiedDependencyResolver) cloneGraph(original *tracking.DependencyGraph) *tracking.DependencyGraph {
	clone := tracking.NewDependencyGraph()

	// Get all nodes and edges from original graph and copy them
	nodes := original.GetAllNodes()
	for _, node := range nodes {
		clone.AddNode(node.ID)
		for _, dep := range node.Dependencies {
			clone.AddEdge(node.ID, dep)
		}
	}

	return clone
}

func (udr *UnifiedDependencyResolver) removeGraphEdge(graph *tracking.DependencyGraph, from, to tracking.ObjectID) {
	// Remove edge by removing the node and re-adding without this edge
	fromNode := graph.GetNode(from)
	toNode := graph.GetNode(to)

	if fromNode != nil {
		// Remove dependency from fromNode
		newDeps := make([]tracking.ObjectID, 0, len(fromNode.Dependencies))
		for _, dep := range fromNode.Dependencies {
			if dep != to {
				newDeps = append(newDeps, dep)
			}
		}
		fromNode.Dependencies = newDeps
	}

	if toNode != nil {
		// Remove dependent from toNode
		newDependents := make([]tracking.ObjectID, 0, len(toNode.Dependents))
		for _, dep := range toNode.Dependents {
			if dep != from {
				newDependents = append(newDependents, dep)
			}
		}
		toNode.Dependents = newDependents
	}
}

// SQL dependency analysis methods (from original DependencyResolver)

// analyzeSQLDependencies extracts dependencies from SQL statements
func (udr *UnifiedDependencyResolver) analyzeSQLDependencies(
	objectKey string,
	result *tracking.ConsolidationResult,
	category types.Category,
	lifecycles map[string]*tracking.ObjectLifecycle,
) *SQLDependencyInfo {

	info := &SQLDependencyInfo{
		ObjectKey:    objectKey,
		Result:       result,
		Dependencies: []string{},
		Provides:     []string{},
	}

	// These are more accurate than regex-based extraction, especially for views
	for _, stmt := range result.OriginalStatements {
		info.Dependencies = append(info.Dependencies, stmt.Dependencies...)
		// Log what dependencies we found from the parser
		if len(stmt.Dependencies) > 0 {
			utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Parser found dependencies for %s: %v", objectKey, stmt.Dependencies)
		}
	}

	sql := result.ConsolidatedSQL
	lowercaseSQL := strings.ToLower(sql)

	// Analyze based on category
	switch category {
	case types.CategoryExtensions:
		info.RequiredFirst = true
		info.Dependencies = append(info.Dependencies, udr.extractExtensionDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractExtensionToExtensionDependencies(sql)...)
		info.Provides = append(info.Provides, udr.extractExtensionProvisions(sql)...)

	case types.CategoryFoundation:
		info.Dependencies = append(info.Dependencies, udr.extractSchemaDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractExtensionDependencies(sql)...)
		// Type dependencies are already extracted by parser from AST and included in stmt.Dependencies above
		info.Dependencies = append(info.Dependencies, udr.extractTableDependencies(sql)...)
		// This ensures views are created in correct order when views depend on other views
		info.Dependencies = append(info.Dependencies, udr.extractViewDependencies(sql)...)
		// CRITICAL: Extract table dependencies from function bodies (SELECT FROM queries)
		// This ensures functions that query tables are created AFTER those tables
		info.Dependencies = append(info.Dependencies, udr.extractFunctionTableDependencies(sql)...)
		// This ensures functions used in CHECK constraints are created before the tables
		functionDeps := udr.extractFunctionDependencies(sql)
		info.Dependencies = append(info.Dependencies, functionDeps...)

		// DEBUG: Log function dependencies for tables with CHECK constraints
		if strings.Contains(strings.ToLower(objectKey), "profile") || len(functionDeps) > 0 {
			utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("🔍 Object %s function dependencies: %v", objectKey, functionDeps)
			if strings.Contains(strings.ToLower(sql), "check") {
				utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("  SQL contains CHECK constraint")
			}
		}

		info.Provides = append(info.Provides, udr.extractTableProvisions(sql)...)
		info.Provides = append(info.Provides, udr.extractSchemaProvisions(sql)...)
		info.Provides = append(info.Provides, udr.extractTypeProvisions(sql)...)
		// CRITICAL: Extract function provisions so functions can be matched as dependencies
		info.Provides = append(info.Provides, udr.extractFunctionProvisions(sql)...)

	case types.CategoryConstraints:
		info.Dependencies = append(info.Dependencies, udr.extractTableDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractColumnDependencies(sql)...)

	case types.CategoryFunctions:
		info.Dependencies = append(info.Dependencies, udr.extractTableDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractSchemaDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractExtensionDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractFunctionDependencies(sql)...)
		info.Provides = append(info.Provides, udr.extractFunctionProvisions(sql)...)

	case types.CategoryTriggers:
		info.Dependencies = append(info.Dependencies, udr.extractTableDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractFunctionDependencies(sql)...)

	case types.CategoryIndexes:
		info.Dependencies = append(info.Dependencies, udr.extractTableDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractColumnDependencies(sql)...)

	case types.CategorySecurity:
		info.Dependencies = append(info.Dependencies, udr.extractTableDependencies(sql)...)

	case types.CategoryData:
		info.RequiredLast = true
		info.Dependencies = append(info.Dependencies, udr.extractTableDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractColumnDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractSchemaDependencies(sql)...)

		// Data operations need more careful dependency analysis
		if strings.Contains(lowercaseSQL, "insert into") {
			info.Dependencies = append(info.Dependencies, udr.extractInsertDependencies(sql)...)
		}
		if strings.Contains(lowercaseSQL, "update") {
			info.Dependencies = append(info.Dependencies, udr.extractUpdateDependencies(sql)...)
		}
	}

	// Remove duplicates
	info.Dependencies = udr.removeDuplicates(info.Dependencies)
	info.Provides = udr.removeDuplicates(info.Provides)

	return info
}

// topologicalSortSQL performs dependency-based sorting for SQL consolidation results
// Delegates to tracking.DependencyGraph for topological sorting to maintain single source of truth
func (udr *UnifiedDependencyResolver) topologicalSortSQL(dependencies map[string]*SQLDependencyInfo, category types.Category) []string {
	// Separate RequiredFirst and RequiredLast items for special handling
	var requiredFirst []string
	var requiredLast []string
	normalDeps := make(map[string]*SQLDependencyInfo)

	for key, info := range dependencies {
		if info.RequiredFirst {
			requiredFirst = append(requiredFirst, key)
		} else if info.RequiredLast {
			requiredLast = append(requiredLast, key)
		} else {
			normalDeps[key] = info
		}
	}
	sort.Strings(requiredFirst)
	sort.Strings(requiredLast)

	// Build a tracking.DependencyGraph for normal dependencies
	depGraph := tracking.NewDependencyGraph()
	keyToObjectID := make(map[string]tracking.ObjectID)
	objectIDToKey := make(map[tracking.ObjectID]string)

	// Add all nodes
	for key := range normalDeps {
		// Create ObjectID using the key as the name
		objID := tracking.ObjectID{
			Type: types.TypeUnknown, // Type doesn't matter for ordering
			Name: key,
		}
		depGraph.AddNode(objID)
		keyToObjectID[key] = objID
		objectIDToKey[objID] = key
	}

	// Build edges based on dependencies
	for key, info := range normalDeps {
		fromID := keyToObjectID[key]
		for _, dep := range info.Dependencies {
			// Find if any other object provides this dependency
			for otherKey, otherInfo := range normalDeps {
				if otherKey == key {
					continue
				}
				for _, provides := range otherInfo.Provides {
					if udr.dependencyMatches(dep, provides) {
						// otherKey must come before key
						toID := keyToObjectID[otherKey]
						depGraph.AddEdge(fromID, toID)
						break
					}
				}
			}
		}
	}

	// Perform topological sort using tracking.DependencyGraph
	sortedIDs, err := depGraph.TopologicalSort()
	var normalResult []string

	if err != nil {
		// Cycles detected - fall back to best-effort ordering
		utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Circular dependencies in category %s, using fallback ordering", category)
		for key := range normalDeps {
			normalResult = append(normalResult, key)
		}
		// Sort alphabetically for stability
		sort.Strings(normalResult)
	} else {
		// Convert ObjectIDs back to string keys
		for _, objID := range sortedIDs {
			if key, exists := objectIDToKey[objID]; exists {
				normalResult = append(normalResult, key)
			}
		}
	}

	// Combine: RequiredFirst + Normal + RequiredLast
	result := make([]string, 0, len(dependencies))
	result = append(result, requiredFirst...)
	result = append(result, normalResult...)
	result = append(result, requiredLast...)

	// DEBUG: Log topological sort results for CategoryFoundation
	if category == types.CategoryFoundation && len(result) > 0 {
		utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("📊 Topological sort order for %s (%d objects):", category, len(result))
		for i, key := range result {
			// Show object type if we can determine it
			objType := "unknown"
			if strings.Contains(strings.ToLower(key), "function") {
				objType = "FUNCTION"
			} else if strings.Contains(strings.ToLower(key), "table") || (!strings.Contains(key, ":") && len(key) < 50) {
				objType = "TABLE"
			}
			utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("  %d. [%s] %s", i+1, objType, key)
		}
	}

	return result
}

// SQL extraction methods (from original DependencyResolver)

// extractTableDependencies finds table references in SQL, with special focus on foreign key dependencies
func (udr *UnifiedDependencyResolver) extractTableDependencies(sql string) []string {
	var deps []string

	// AST-first extraction using the same parser pipeline used elsewhere in engine.
	if stmts := udr.parseStatementsForDependencyExtraction(sql); len(stmts) > 0 {
		for _, stmt := range stmts {
			deps = append(deps, udr.extractTableDependenciesFromStatement(stmt)...)
		}

		deps = udr.removeDuplicates(deps)
		if len(deps) > 0 {
			return deps
		}
	}

	// Fallback for SQL fragments that are not parseable as full statements.
	deps = append(deps, udr.extractReturnsSetofDependencies(sql)...)

	tokens := tokenizeSQLIdentifiers(sql)

	// Priority 1/2: REFERENCES targets from FK and shorthand REFERENCES clauses.
	for _, referencedTable := range extractIdentifiersAfterKeyword(tokens, "REFERENCES") {
		referencedTable = strings.TrimSpace(referencedTable)
		if referencedTable == "" {
			continue
		}

		utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Found REFERENCES dependency: table depends on %s", referencedTable)
		deps = append(deps, referencedTable)
	}

	// Priority 3: COMMENT ON TABLE/COLUMN target table dependency.
	for _, tableName := range extractCommentOnTableTargets(tokens) {
		if tableName == "" {
			continue
		}

		utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Found COMMENT ON dependency: comment depends on table %s", tableName)
		deps = append(deps, tableName)
	}

	// Priority 4: DML table dependencies.
	deps = append(deps, extractIdentifiersAfterKeywordSequence(tokens, "INSERT", "INTO")...)
	for _, tableName := range extractIdentifiersAfterKeyword(tokens, "UPDATE") {
		tableName = strings.ToLower(strings.TrimSpace(tableName))
		if tableName != "" {
			deps = append(deps, tableName)
		}
	}

	return udr.removeDuplicates(deps)
}

// extractViewDependencies finds view references in FROM/JOIN clauses
// This method extracts view-to-view dependencies to ensure proper creation order
func (udr *UnifiedDependencyResolver) extractViewDependencies(sql string) []string {
	var deps []string

	// AST-first: rely on parser dependency extraction for CREATE VIEW / MATVIEW.
	if stmts := udr.parseStatementsForDependencyExtraction(sql); len(stmts) > 0 {
		for _, stmt := range stmts {
			if stmt.ObjectType != types.TypeView || stmt.Operation != types.OpCreate {
				continue
			}

			for _, dep := range stmt.Dependencies {
				normalized := normalizeDependencyIdentifier(dep)
				if normalized != "" {
					deps = append(deps, normalized)
				}
			}
		}

		deps = udr.removeDuplicates(deps)
		if len(deps) > 0 {
			return deps
		}
	}

	// Fallback: token scanning for FROM/JOIN references in view definition.
	upperSQL := strings.ToUpper(sql)
	if !strings.Contains(upperSQL, "CREATE") || !strings.Contains(upperSQL, "VIEW") {
		return deps
	}

	asIndex := strings.Index(upperSQL, " AS ")
	if asIndex == -1 || asIndex+4 >= len(sql) {
		return deps
	}

	viewDef := sql[asIndex+4:]
	deps = append(deps, udr.extractTableRefsFromSQLFragment(viewDef)...)

	return udr.removeDuplicates(deps)
}

// extractFunctionTableDependencies finds table references in function bodies (FROM/JOIN clauses)
// This ensures SQL functions that query tables are created AFTER those tables exist
func (udr *UnifiedDependencyResolver) extractFunctionTableDependencies(sql string) []string {
	var deps []string

	upperSQL := strings.ToUpper(sql)
	if !strings.Contains(upperSQL, "CREATE") || !strings.Contains(upperSQL, "FUNCTION") {
		return deps
	}

	functionBody := extractDelimitedSQLBody(sql)
	if functionBody == "" {
		return deps
	}

	deps = append(deps, udr.extractTableRefsFromSQLFragment(functionBody)...)
	for _, dep := range deps {
		utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Found function body dependency: function depends on table %s", dep)
	}

	return udr.removeDuplicates(deps)
}

// isCommonSQLKeyword checks if a string is a common SQL keyword that shouldn't be treated as a table/view name
func isCommonSQLKeyword(word string) bool {
	keywords := map[string]bool{
		"select": true, "where": true, "order": true, "group": true, "having": true,
		"limit": true, "offset": true, "union": true, "except": true, "intersect": true,
		"case": true, "when": true, "then": true, "else": true, "end": true,
		"as": true, "on": true, "using": true, "lateral": true, "unnest": true,
	}
	return keywords[strings.ToLower(word)]
}

// extractSchemaDependencies finds schema references
func (udr *UnifiedDependencyResolver) extractSchemaDependencies(sql string) []string {
	var deps []string
	for _, token := range tokenizeSQLIdentifiers(sql) {
		if !strings.Contains(token, ".") {
			continue
		}

		parts := strings.Split(token, ".")
		if len(parts) < 2 {
			continue
		}

		schema := strings.TrimSpace(parts[0])
		if schema == "" || strings.EqualFold(schema, "public") || !isIdentifierToken(schema) {
			continue
		}

		deps = append(deps, fmt.Sprintf("schema:%s", strings.ToLower(schema)))
	}

	return udr.removeDuplicates(deps)
}

// extractExtensionDependencies finds extension requirements
func (udr *UnifiedDependencyResolver) extractExtensionDependencies(sql string) []string {
	var deps []string
	lowercaseSQL := strings.ToLower(sql)

	// Known extensions and their indicators
	extensionIndicators := map[string][]string{
		"postgis":            {"geometry", "geography", "st_", "postgis"},
		"uuid-ossp":          {"uuid_generate", "gen_random_uuid"},
		"earthdistance":      {"earth_distance", "ll_to_earth", "cube"},
		"pg_stat_statements": {"pg_stat_statements"},
		"plpgsql":            {"$function$", "$body$"},
		"cube":               {"cube"},
		"pg_trgm":            {"similarity", "word_similarity"},
	}

	for extension, indicators := range extensionIndicators {
		for _, indicator := range indicators {
			if strings.Contains(lowercaseSQL, indicator) {
				deps = append(deps, fmt.Sprintf("extension:%s", extension))
				break
			}
		}
	}

	return deps
}

// extractExtensionToExtensionDependencies returns dependencies for CREATE EXTENSION statements
// where the extension itself requires another extension to be installed first.
// This ensures correct ordering of extension creation (e.g., cube before earthdistance)
func (udr *UnifiedDependencyResolver) extractExtensionToExtensionDependencies(sql string) []string {
	var deps []string

	// Map of extension name -> required extensions
	// These are hardcoded PostgreSQL extension dependencies
	extensionRequirements := map[string][]string{
		"earthdistance":          {"cube"},    // earthdistance requires cube
		"postgis_tiger_geocoder": {"postgis"}, // postgis_tiger_geocoder requires postgis
		"postgis_topology":       {"postgis"}, // postgis_topology requires postgis
		"postgis_raster":         {"postgis"}, // postgis_raster requires postgis
		"address_standardizer":   {"postgis"}, // address_standardizer requires postgis
	}

	for _, extensionName := range udr.extractCreatedExtensionNames(sql) {
		extensionName = strings.ToLower(strings.TrimSpace(extensionName))
		if extensionName == "" {
			continue
		}

		// Check if this extension has dependencies
		if requiredExtensions, exists := extensionRequirements[extensionName]; exists {
			for _, required := range requiredExtensions {
				deps = append(deps, fmt.Sprintf("extension:%s", required))
			}
		}
	}

	return udr.removeDuplicates(deps)
}

// extractColumnDependencies finds column references that require prior ALTER TABLE
func (udr *UnifiedDependencyResolver) extractColumnDependencies(sql string) []string {
	var deps []string

	if stmts := udr.parseStatementsForDependencyExtraction(sql); len(stmts) > 0 {
		for _, stmt := range stmts {
			if stmt.ParseTree == nil || len(stmt.ParseTree.Stmts) == 0 {
				continue
			}

			tableName := strings.TrimSpace(stmt.ObjectName)
			if tableName == "" {
				continue
			}

			for _, raw := range stmt.ParseTree.Stmts {
				insert := raw.Stmt.GetInsertStmt()
				if insert == nil {
					continue
				}

				for _, col := range insert.Cols {
					resTarget := col.GetResTarget()
					if resTarget == nil {
						continue
					}

					columnName := strings.TrimSpace(resTarget.Name)
					if columnName == "" {
						continue
					}

					deps = append(deps, fmt.Sprintf("column:%s.%s", tableName, columnName))
				}
			}
		}

		if len(deps) > 0 {
			return udr.removeDuplicates(deps)
		}
	}

	// Token fallback for partial SQL fragments.
	tokens := tokenizeSQLIdentifiers(sql)
	for _, tableName := range extractIdentifiersAfterKeywordSequence(tokens, "INSERT", "INTO") {
		for _, column := range extractInsertColumns(sql) {
			deps = append(deps, fmt.Sprintf("column:%s.%s", tableName, column))
		}
	}

	return udr.removeDuplicates(deps)
}

// extractFunctionDependencies finds function references
func (udr *UnifiedDependencyResolver) extractFunctionDependencies(sql string) []string {
	var deps []string

	tokens := tokenizeSQLIdentifiers(sql)
	for _, fn := range extractIdentifiersAfterKeywordSequence(tokens, "EXECUTE", "FUNCTION") {
		fn = strings.ToLower(strings.TrimSpace(fn))
		if fn != "" {
			deps = append(deps, fmt.Sprintf("function:%s", fn))
		}
	}

	callMatches := scanFunctionCallNames(sql)

	// Track seen functions to avoid duplicates and filter out common SQL keywords/functions
	seen := make(map[string]bool)
	sqlKeywords := map[string]bool{
		"create": true, "or": true, "replace": true, "function": true, "returns": true,
		"language": true, "as": true, "begin": true, "end": true, "declare": true,
		"if": true, "then": true, "else": true, "elsif": true, "case": true, "when": true,
		"return": true, "raise": true, "exception": true, "for": true, "loop": true,
		"while": true, "select": true, "insert": true, "update": true, "delete": true,
		"from": true, "where": true, "into": true, "values": true, "set": true,
		"now": true, "count": true, "sum": true, "avg": true, "max": true, "min": true,
		"coalesce": true, "nullif": true, "cast": true, "extract": true, "substring": true,
		"trim": true, "lower": true, "upper": true, "concat": true, "array_agg": true,
		"json_build_object": true, "jsonb_build_object": true, "to_jsonb": true,
		"string_agg": true, "row_number": true, "rank": true, "dense_rank": true,
		"gen_random_uuid": true, "uuid_generate_v4": true, "current_timestamp": true,
	}

	for _, candidate := range callMatches {
		funcName := strings.ToLower(strings.TrimSpace(candidate))
		// Skip if already seen, is a SQL keyword, or empty
		if !seen[funcName] && !sqlKeywords[funcName] && funcName != "" {
			seen[funcName] = true
			deps = append(deps, fmt.Sprintf("function:%s", funcName))
		}
	}

	return udr.removeDuplicates(deps)
}

// extractInsertDependencies analyzes INSERT statements for dependencies
func (udr *UnifiedDependencyResolver) extractInsertDependencies(sql string) []string {
	var deps []string

	// INSERT statements depend on the table existing and columns being present
	deps = append(deps, udr.extractTableDependencies(sql)...)
	deps = append(deps, udr.extractColumnDependencies(sql)...)

	return deps
}

// extractUpdateDependencies analyzes UPDATE statements
func (udr *UnifiedDependencyResolver) extractUpdateDependencies(sql string) []string {
	var deps []string

	// UPDATE statements depend on tables and columns
	deps = append(deps, udr.extractTableDependencies(sql)...)

	tableNames := extractIdentifiersAfterKeyword(tokenizeSQLIdentifiers(sql), "UPDATE")
	if len(tableNames) == 0 {
		return udr.removeDuplicates(deps)
	}

	columns := extractUpdateSetColumns(sql)
	for _, tableName := range tableNames {
		tableName = strings.TrimSpace(tableName)
		if tableName == "" {
			continue
		}

		for _, column := range columns {
			if column == "" {
				continue
			}
			deps = append(deps, fmt.Sprintf("column:%s.%s", tableName, column))
		}
	}

	return udr.removeDuplicates(deps)
}

// extractTableProvisions finds what tables/objects this SQL creates
func (udr *UnifiedDependencyResolver) extractTableProvisions(sql string) []string {
	var provides []string

	if stmts := udr.parseStatementsForDependencyExtraction(sql); len(stmts) > 0 {
		for _, stmt := range stmts {
			if stmt.ObjectType == types.TypeTable && stmt.Operation == types.OpCreate {
				tableName := strings.TrimSpace(stmt.ObjectName)
				if tableName != "" {
					provides = append(provides, tableName)
				}
			}
		}

		if len(provides) > 0 {
			return udr.removeDuplicates(provides)
		}
	}

	for _, tableName := range extractIdentifiersAfterKeywordSequence(tokenizeSQLIdentifiers(sql), "CREATE", "TABLE") {
		if tableName != "" {
			provides = append(provides, tableName)
		}
	}

	return udr.removeDuplicates(provides)
}

// extractFunctionProvisions finds what functions this SQL creates
func (udr *UnifiedDependencyResolver) extractFunctionProvisions(sql string) []string {
	var provides []string

	if stmts := udr.parseStatementsForDependencyExtraction(sql); len(stmts) > 0 {
		for _, stmt := range stmts {
			if stmt.ObjectType != types.TypeFunction || stmt.Operation != types.OpCreate {
				continue
			}

			funcName := strings.ToLower(strings.TrimSpace(stmt.ObjectName))
			if funcName != "" {
				provides = append(provides, fmt.Sprintf("function:%s", funcName))
			}
		}

		if len(provides) > 0 {
			return udr.removeDuplicates(provides)
		}
	}

	for _, fn := range extractIdentifiersAfterKeywordSequence(tokenizeSQLIdentifiers(sql), "CREATE", "FUNCTION") {
		fn = strings.ToLower(strings.TrimSpace(fn))
		if fn != "" {
			provides = append(provides, fmt.Sprintf("function:%s", fn))
		}
	}

	for _, fn := range extractIdentifiersAfterKeywordSequence(tokenizeSQLIdentifiers(sql), "CREATE", "OR", "REPLACE", "FUNCTION") {
		fn = strings.ToLower(strings.TrimSpace(fn))
		if fn != "" {
			provides = append(provides, fmt.Sprintf("function:%s", fn))
		}
	}

	return udr.removeDuplicates(provides)
}

// extractSchemaProvisions finds what schemas this SQL creates
func (udr *UnifiedDependencyResolver) extractSchemaProvisions(sql string) []string {
	var provides []string

	for _, schema := range extractIdentifiersAfterKeywordSequence(tokenizeSQLIdentifiers(sql), "CREATE", "SCHEMA") {
		schema = strings.TrimSpace(schema)
		if schema == "" || strings.EqualFold(schema, "if") {
			continue
		}
		provides = append(provides, fmt.Sprintf("schema:%s", schema))
	}

	for _, schema := range extractIdentifiersAfterKeywordSequence(tokenizeSQLIdentifiers(sql), "CREATE", "SCHEMA", "IF", "NOT", "EXISTS") {
		schema = strings.TrimSpace(schema)
		if schema != "" {
			provides = append(provides, fmt.Sprintf("schema:%s", schema))
		}
	}

	return udr.removeDuplicates(provides)
}

// extractExtensionProvisions finds what extensions this SQL creates
func (udr *UnifiedDependencyResolver) extractExtensionProvisions(sql string) []string {
	var provides []string

	for _, extension := range udr.extractCreatedExtensionNames(sql) {
		extension = strings.TrimSpace(extension)
		if extension != "" {
			provides = append(provides, fmt.Sprintf("extension:%s", extension))
		}
	}

	return udr.removeDuplicates(provides)
}

// extractTypeDependencies finds type/enum references that need to be created first
func (udr *UnifiedDependencyResolver) extractTypeDependencies(sql string) []string {
	var deps []string

	if stmts := udr.parseStatementsForDependencyExtraction(sql); len(stmts) > 0 {
		seenTypes := make(map[string]struct{})

		for _, stmt := range stmts {
			if stmt.ObjectType != types.TypeTable || stmt.Operation != types.OpCreate || stmt.ParseTree == nil {
				continue
			}

			for _, raw := range stmt.ParseTree.Stmts {
				createStmt := raw.Stmt.GetCreateStmt()
				if createStmt == nil {
					continue
				}

				for _, tableElt := range createStmt.TableElts {
					colDef := tableElt.GetColumnDef()
					if colDef == nil || colDef.TypeName == nil {
						continue
					}

					typeName := strings.ToLower(strings.TrimSpace(typeNameFromAST(colDef.TypeName)))
					if typeName == "" || isBuiltInOrKeywordType(typeName) {
						continue
					}

					if _, exists := seenTypes[typeName]; exists {
						continue
					}

					seenTypes[typeName] = struct{}{}
					deps = append(deps, fmt.Sprintf("type:%s", typeName))
					utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Table depends on custom type: %s", typeName)
				}
			}
		}

		deps = udr.removeDuplicates(deps)
		if len(deps) > 0 {
			return deps
		}
	}

	// Fallback for SQL fragments that cannot be parsed as full CREATE TABLE statements.
	seenTypes := make(map[string]struct{})
	for _, typeName := range scanPotentialCustomTypes(sql) {
		typeName = strings.ToLower(strings.TrimSpace(typeName))
		if typeName == "" || isBuiltInOrKeywordType(typeName) {
			continue
		}

		if _, exists := seenTypes[typeName]; exists {
			continue
		}

		seenTypes[typeName] = struct{}{}
		deps = append(deps, fmt.Sprintf("type:%s", typeName))
	}

	return udr.removeDuplicates(deps)
}

func (udr *UnifiedDependencyResolver) parseStatementsForDependencyExtraction(sql string) []types.Statement {
	migration, err := parser.ParseMigration(sql, "__dependency_extraction__.sql")
	if err != nil || migration == nil || len(migration.Statements) == 0 {
		return nil
	}

	return migration.Statements
}

func (udr *UnifiedDependencyResolver) extractTableDependenciesFromStatement(stmt types.Statement) []string {
	deps := make([]string, 0)

	for _, dep := range stmt.Dependencies {
		normalized := normalizeDependencyIdentifier(dep)
		if normalized != "" {
			deps = append(deps, normalized)
		}
	}

	if stmt.IsDataOp {
		normalized := normalizeDependencyIdentifier(stmt.ObjectName)
		if normalized != "" {
			deps = append(deps, normalized)
		}
	}

	if stmt.ObjectType == types.TypeFunction {
		deps = append(deps, udr.extractReturnsSetofDependencies(stmt.SQL)...)
	}

	return deps
}

func (udr *UnifiedDependencyResolver) extractReturnsSetofDependencies(sql string) []string {
	deps := make([]string, 0)
	tokens := tokenizeSQLIdentifiers(sql)

	for i := 0; i+2 < len(tokens); i++ {
		if !strings.EqualFold(tokens[i], "RETURNS") || !strings.EqualFold(tokens[i+1], "SETOF") {
			continue
		}

		normalized := normalizeDependencyIdentifier(tokens[i+2])
		if normalized == "" {
			continue
		}

		utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Found RETURNS SETOF dependency: function depends on table %s", normalized)
		deps = append(deps, normalized)
	}

	return udr.removeDuplicates(deps)
}

func (udr *UnifiedDependencyResolver) extractTableRefsFromSQLFragment(fragment string) []string {
	tokens := tokenizeSQLIdentifiers(fragment)
	deps := make([]string, 0)

	for i := range tokens {
		token := strings.ToUpper(tokens[i])
		if token != "FROM" && token != "JOIN" {
			continue
		}

		j := i + 1
		for j < len(tokens) && isTableReferenceModifier(tokens[j]) {
			j++
		}
		if j >= len(tokens) {
			continue
		}

		referenced := normalizeDependencyIdentifier(tokens[j])
		if referenced == "" || isCommonSQLKeyword(referenced) {
			continue
		}

		deps = append(deps, referenced)
	}

	return udr.removeDuplicates(deps)
}

func extractDelimitedSQLBody(sql string) string {
	upperSQL := strings.ToUpper(sql)
	asIndex := strings.Index(upperSQL, "AS")
	if asIndex == -1 {
		return ""
	}

	tail := sql[asIndex+2:]
	dollarStart := strings.Index(tail, "$")
	if dollarStart == -1 {
		return ""
	}

	tail = tail[dollarStart:]
	dollarEnd := strings.Index(tail[1:], "$")
	if dollarEnd == -1 {
		return ""
	}

	tag := tail[:dollarEnd+2]
	bodyStart := asIndex + 2 + dollarStart + len(tag)
	if bodyStart >= len(sql) {
		return ""
	}

	bodyEndRelative := strings.Index(sql[bodyStart:], tag)
	if bodyEndRelative == -1 {
		return ""
	}

	return sql[bodyStart : bodyStart+bodyEndRelative]
}

func tokenizeSQLIdentifiers(sql string) []string {
	if strings.TrimSpace(sql) == "" {
		return nil
	}

	raw := strings.FieldsFunc(sql, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.')
	})

	tokens := make([]string, 0, len(raw))
	for _, token := range raw {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		tokens = append(tokens, token)
	}

	return tokens
}

func isTableReferenceModifier(token string) bool {
	switch strings.ToUpper(strings.TrimSpace(token)) {
	case "ONLY", "LATERAL", "INNER", "LEFT", "RIGHT", "FULL", "OUTER", "CROSS":
		return true
	default:
		return false
	}
}

func extractIdentifiersAfterKeyword(tokens []string, keyword string) []string {
	if len(tokens) == 0 {
		return nil
	}

	results := make([]string, 0)
	for i := 0; i+1 < len(tokens); i++ {
		if !strings.EqualFold(tokens[i], keyword) {
			continue
		}

		candidate := strings.TrimSpace(tokens[i+1])
		if candidate == "" {
			continue
		}

		results = append(results, candidate)
	}

	return removeDuplicateStrings(results)
}

func extractIdentifiersAfterKeywordSequence(tokens []string, keywords ...string) []string {
	if len(tokens) == 0 || len(keywords) == 0 {
		return nil
	}

	results := make([]string, 0)
	for i := range tokens {
		index := i
		matched := true
		for _, keyword := range keywords {
			if index >= len(tokens) || !strings.EqualFold(tokens[index], keyword) {
				matched = false
				break
			}
			index++
		}

		if !matched || index >= len(tokens) {
			continue
		}

		candidate := strings.TrimSpace(tokens[index])
		if candidate == "" {
			continue
		}

		results = append(results, candidate)
	}

	return removeDuplicateStrings(results)
}

func extractCommentOnTableTargets(tokens []string) []string {
	results := make([]string, 0)
	for i := 0; i+3 < len(tokens); i++ {
		if !strings.EqualFold(tokens[i], "COMMENT") || !strings.EqualFold(tokens[i+1], "ON") {
			continue
		}

		targetKind := strings.ToUpper(strings.TrimSpace(tokens[i+2]))
		if targetKind != "TABLE" && targetKind != "COLUMN" {
			continue
		}

		qualifiedName := strings.TrimSpace(tokens[i+3])
		if qualifiedName == "" {
			continue
		}

		parts := strings.Split(qualifiedName, ".")
		tableName := strings.TrimSpace(parts[0])
		if tableName != "" {
			results = append(results, tableName)
		}
	}

	return removeDuplicateStrings(results)
}

func extractInsertColumns(sql string) []string {
	upper := strings.ToUpper(sql)
	insertIntoIndex := strings.Index(upper, "INSERT")
	if insertIntoIndex == -1 {
		return nil
	}

	parenStart := strings.Index(sql[insertIntoIndex:], "(")
	if parenStart == -1 {
		return nil
	}
	parenStart += insertIntoIndex

	parenEnd := findClosingParen(sql, parenStart)
	if parenEnd == -1 || parenEnd <= parenStart+1 {
		return nil
	}

	columnSlice := sql[parenStart+1 : parenEnd]
	parts := strings.Split(columnSlice, ",")
	columns := make([]string, 0, len(parts))
	for _, part := range parts {
		candidate := strings.TrimSpace(strings.Trim(part, `"`))
		if candidate != "" {
			columns = append(columns, candidate)
		}
	}

	return removeDuplicateStrings(columns)
}

func scanFunctionCallNames(sql string) []string {
	if strings.TrimSpace(sql) == "" {
		return nil
	}

	names := make([]string, 0)
	for i := 0; i < len(sql); i++ {
		if !isIdentifierTokenByte(sql[i]) {
			continue
		}
		if i > 0 && isIdentifierTokenByte(sql[i-1]) {
			continue
		}

		start := i
		for i < len(sql) && (isIdentifierTokenByte(sql[i]) || sql[i] == '.') {
			i++
		}

		name := strings.TrimSpace(sql[start:i])
		if name == "" {
			continue
		}

		j := i
		for j < len(sql) && unicode.IsSpace(rune(sql[j])) {
			j++
		}
		if j < len(sql) && sql[j] == '(' {
			names = append(names, name)
		}

		i--
	}

	return removeDuplicateStrings(names)
}

func extractUpdateSetColumns(sql string) []string {
	stmts := strings.Split(sql, ";")
	columns := make([]string, 0)

	for _, stmt := range stmts {
		upper := strings.ToUpper(stmt)
		setIndex := strings.Index(upper, " SET ")
		if setIndex == -1 {
			continue
		}

		setClauseStart := setIndex + len(" SET ")
		whereIndex := strings.Index(upper[setClauseStart:], " WHERE ")
		setClauseEnd := len(stmt)
		if whereIndex >= 0 {
			setClauseEnd = setClauseStart + whereIndex
		}

		setClause := stmt[setClauseStart:setClauseEnd]
		assignments := strings.SplitSeq(setClause, ",")
		for assignment := range assignments {
			before, _, ok := strings.Cut(assignment, "=")
			if !ok {
				continue
			}

			left := strings.TrimSpace(before)
			if left == "" {
				continue
			}

			if strings.Contains(left, ".") {
				parts := strings.Split(left, ".")
				left = strings.TrimSpace(parts[len(parts)-1])
			}

			left = strings.Trim(left, `"`)
			if left != "" {
				columns = append(columns, left)
			}
		}
	}

	return removeDuplicateStrings(columns)
}

func isIdentifierToken(token string) bool {
	if token == "" {
		return false
	}

	for i := 0; i < len(token); i++ {
		if !isIdentifierTokenByte(token[i]) {
			return false
		}
	}

	return true
}

func isIdentifierTokenByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func findClosingParen(sql string, openIndex int) int {
	if openIndex < 0 || openIndex >= len(sql) || sql[openIndex] != '(' {
		return -1
	}

	depth := 0
	inSingleQuote := false
	for i := openIndex; i < len(sql); i++ {
		ch := sql[i]

		if inSingleQuote {
			if ch == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					i++
					continue
				}
				inSingleQuote = false
			}
			continue
		}

		switch ch {
		case '\'':
			inSingleQuote = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}

	return -1
}

func removeDuplicateStrings(items []string) []string {
	if len(items) <= 1 {
		return items
	}

	seen := make(map[string]struct{}, len(items))
	unique := make([]string, 0, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item)
		if key == "" {
			continue
		}
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, key)
	}

	return unique
}

func (udr *UnifiedDependencyResolver) extractCreatedExtensionNames(sql string) []string {
	names := make([]string, 0)

	if stmts := udr.parseStatementsForDependencyExtraction(sql); len(stmts) > 0 {
		for _, stmt := range stmts {
			if stmt.ObjectType == types.TypeExtension && stmt.Operation == types.OpCreate {
				extName := strings.TrimSpace(stmt.ObjectName)
				if extName != "" {
					names = append(names, extName)
				}
			}
		}

		if len(names) > 0 {
			return udr.removeDuplicates(names)
		}
	}

	tokens := tokenizeSQLIdentifiers(sql)
	names = append(names, extractIdentifiersAfterKeywordSequence(tokens, "CREATE", "EXTENSION")...)
	names = append(names, extractIdentifiersAfterKeywordSequence(tokens, "CREATE", "EXTENSION", "IF", "NOT", "EXISTS")...)

	return udr.removeDuplicates(names)
}

func scanPotentialCustomTypes(sql string) []string {
	tokens := tokenizeSQLIdentifiers(sql)
	if len(tokens) < 2 {
		return nil
	}

	blocked := map[string]bool{
		"create": true, "table": true, "alter": true, "add": true, "column": true,
		"constraint": true, "primary": true, "foreign": true, "references": true,
		"check": true, "default": true, "not": true, "null": true, "unique": true,
		"key": true, "as": true, "enum": true, "type": true, "on": true,
		"delete": true, "update": true, "set": true, "if": true, "exists": true,
		"returns": true, "function": true,
	}

	typesFound := make([]string, 0)
	for i := 0; i+1 < len(tokens); i++ {
		left := strings.ToLower(strings.TrimSpace(tokens[i]))
		right := strings.ToLower(strings.TrimSpace(tokens[i+1]))

		if left == "" || right == "" {
			continue
		}

		if blocked[left] || blocked[right] {
			continue
		}

		if !isIdentifierToken(left) || !isIdentifierToken(right) {
			continue
		}

		typesFound = append(typesFound, right)
	}

	return removeDuplicateStrings(typesFound)
}

func enhanceExtensionStatementWithCascade(sql string, cascadeExtensions map[string]bool) string {
	trimmed := strings.TrimSpace(sql)
	if trimmed == "" {
		return sql
	}

	if !strings.Contains(strings.ToUpper(trimmed), "CREATE") || !strings.Contains(strings.ToUpper(trimmed), "EXTENSION") {
		return sql
	}

	if hasKeyword(tokenizeSQLIdentifiers(trimmed), "CASCADE") {
		return sql
	}

	extensionNames := extractIdentifiersAfterKeywordSequence(tokenizeSQLIdentifiers(trimmed), "CREATE", "EXTENSION")
	extensionNames = append(extensionNames, extractIdentifiersAfterKeywordSequence(tokenizeSQLIdentifiers(trimmed), "CREATE", "EXTENSION", "IF", "NOT", "EXISTS")...)

	for _, extensionName := range extensionNames {
		if !cascadeExtensions[strings.ToLower(strings.TrimSpace(extensionName))] {
			continue
		}

		base := strings.TrimSuffix(trimmed, ";")
		return base + " CASCADE;"
	}

	return sql
}

func hasKeyword(tokens []string, keyword string) bool {
	for _, token := range tokens {
		if strings.EqualFold(token, keyword) {
			return true
		}
	}
	return false
}

func normalizeDependencyIdentifier(dep string) string {
	trimmed := strings.TrimSpace(dep)
	if trimmed == "" {
		return ""
	}

	upper := strings.ToUpper(trimmed)
	if strings.HasPrefix(upper, "REFERENCES:") || strings.HasPrefix(upper, "TABLE:") {
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) == 2 {
			trimmed = strings.TrimSpace(parts[1])
		}
	} else if strings.Contains(trimmed, ":") {
		// Non-table dependency classes (schema:, function:, extension:, type:, column:, constraint:).
		return ""
	}

	if trimmed == "" || strings.Contains(trimmed, "(") {
		return ""
	}

	if strings.Contains(trimmed, ".") {
		parts := strings.Split(trimmed, ".")
		trimmed = parts[len(parts)-1]
	}

	return strings.ToLower(strings.TrimSpace(trimmed))
}

func typeNameFromAST(typeName *pg_query.TypeName) string {
	if typeName == nil || len(typeName.Names) == 0 {
		return ""
	}

	last := typeName.Names[len(typeName.Names)-1]
	strNode := last.GetString_()
	if strNode == nil {
		return ""
	}

	return strNode.Sval
}

func isBuiltInOrKeywordType(typeName string) bool {
	postgresBuiltinTypes := map[string]bool{
		"text": true, "varchar": true, "char": true, "character": true,
		"integer": true, "int": true, "int2": true, "int4": true, "int8": true,
		"smallint": true, "bigint": true, "serial": true, "bigserial": true,
		"numeric": true, "decimal": true, "real": true, "double": true, "float": true,
		"boolean": true, "bool": true, "date": true, "time": true,
		"timestamp": true, "timestamptz": true, "interval": true,
		"json": true, "jsonb": true, "uuid": true, "bytea": true,
		"array": true, "hstore": true, "xml": true, "money": true,
		"point": true, "line": true, "lseg": true, "box": true, "path": true,
		"polygon": true, "circle": true, "cidr": true, "inet": true, "macaddr": true,
		"tsvector": true, "tsquery": true, "geometry": true, "geography": true,
	}

	sqlKeywords := map[string]bool{
		"table": true, "not": true, "null": true, "check": true, "constraint": true,
		"primary": true, "foreign": true, "key": true, "references": true,
		"unique": true, "default": true, "exists": true, "if": true, "enable": true,
		"disable": true, "cascade": true, "restrict": true, "on": true, "delete": true,
		"update": true, "no": true, "action": true, "set": true, "add": true,
		"alter": true, "drop": true, "row": true, "level": true, "security": true,
	}

	normalized := strings.ToLower(strings.TrimSpace(typeName))
	if normalized == "" || len(normalized) <= 3 {
		return true
	}

	return postgresBuiltinTypes[normalized] || sqlKeywords[normalized]
}

// extractTypeProvisions finds what types/enums this SQL creates
func (udr *UnifiedDependencyResolver) extractTypeProvisions(sql string) []string {
	var provides []string

	if stmts := udr.parseStatementsForDependencyExtraction(sql); len(stmts) > 0 {
		for _, stmt := range stmts {
			if stmt.Operation != types.OpCreate {
				continue
			}

			if stmt.ObjectType != types.TypeEnum && stmt.ObjectType != types.TypeType && stmt.ObjectType != types.TypeDomain && stmt.ObjectType != types.TypeComposite {
				continue
			}

			typeName := strings.TrimSpace(stmt.ObjectName)
			if typeName != "" {
				provides = append(provides, fmt.Sprintf("type:%s", typeName))
			}
		}

		if len(provides) > 0 {
			return udr.removeDuplicates(provides)
		}
	}

	for _, typeName := range extractIdentifiersAfterKeywordSequence(tokenizeSQLIdentifiers(sql), "CREATE", "TYPE") {
		typeName = strings.TrimSpace(typeName)
		if typeName != "" {
			provides = append(provides, fmt.Sprintf("type:%s", typeName))
		}
	}

	return udr.removeDuplicates(provides)
}

// dependencyMatches checks if a dependency requirement matches a provision
func (udr *UnifiedDependencyResolver) dependencyMatches(dependency, provision string) bool {
	// Handle different types of dependencies
	if strings.HasPrefix(dependency, "schema:") && strings.HasPrefix(provision, "schema:") {
		return dependency == provision
	}
	if strings.HasPrefix(dependency, "extension:") && strings.HasPrefix(provision, "extension:") {
		return dependency == provision
	}
	if strings.HasPrefix(dependency, "function:") && strings.HasPrefix(provision, "function:") {
		return dependency == provision
	}
	if strings.HasPrefix(dependency, "type:") && strings.HasPrefix(provision, "type:") {
		return dependency == provision
	}

	// Type dependencies from parser don't have "type:" prefix
	// Match bare type names against "type:typename" provisions
	if after, ok := strings.CutPrefix(provision, "type:"); ok {
		provisionType := after
		if dependency == provisionType {
			utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Matched type dependency: %s requires type:%s", dependency, provisionType)
			return true
		}
	}

	if strings.HasPrefix(dependency, "column:") {
		// For column dependencies, we need table to exist first
		parts := strings.Split(dependency, ":")
		if len(parts) > 1 {
			tablePart := strings.Split(parts[1], ".")[0]
			return provision == tablePart
		}
	}

	// Direct table name match (handle schema qualifications)
	depName := dependency
	provName := provision

	// Remove schema prefix if present (e.g., "public.blueprints" -> "blueprints")
	if strings.Contains(depName, ".") {
		parts := strings.SplitN(depName, ".", 2)
		if len(parts) == 2 {
			depName = parts[1] // Take the table name part
		}
	}
	if strings.Contains(provName, ".") {
		parts := strings.SplitN(provName, ".", 2)
		if len(parts) == 2 {
			provName = parts[1] // Take the table name part
		}
	}

	// Match without schema qualifiers
	// This allows "blueprints" to match "public.blueprints"
	matched := depName == provName
	if matched {
		utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Matched table dependency: %s requires %s (normalized: %s == %s)", dependency, provision, depName, provName)
	}
	return matched
}

// removeDuplicates removes duplicate strings from slice
func (udr *UnifiedDependencyResolver) removeDuplicates(slice []string) []string {
	keys := make(map[string]bool)
	var result []string

	for _, item := range slice {
		if !keys[item] {
			keys[item] = true
			result = append(result, item)
		}
	}

	return result
}

// EnhanceExtensionSQL adds CASCADE to extension creation where needed
func (udr *UnifiedDependencyResolver) EnhanceExtensionSQL(sql string) string {
	// Extensions that typically require CASCADE
	cascadeExtensions := map[string]bool{
		"earthdistance": true,
		"postgis":       true,
	}

	if strings.TrimSpace(sql) == "" {
		return sql
	}

	statements, err := pg_query.SplitWithScanner(sql, true)
	if err != nil || len(statements) == 0 {
		return enhanceExtensionStatementWithCascade(sql, cascadeExtensions)
	}

	enhanced := make([]string, 0, len(statements))
	changed := false

	for _, stmt := range statements {
		trimmed := strings.TrimSpace(stmt)
		if trimmed == "" {
			continue
		}

		updated := enhanceExtensionStatementWithCascade(trimmed, cascadeExtensions)
		if updated != trimmed {
			changed = true
		}

		enhanced = append(enhanced, strings.TrimSuffix(strings.TrimSpace(updated), ";"))
	}

	if !changed {
		return sql
	}

	if len(enhanced) == 0 {
		return sql
	}

	return strings.Join(enhanced, ";\n") + ";"
}
