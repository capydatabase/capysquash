package squasher

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/patterns"
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"
	"github.com/capysquash/pgsquash-engine/internal/utils"
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
		types.CategoryFoundation:  1, // Schemas, types, domains, tables
		types.CategoryConstraints: 2, // Primary keys, unique constraints
		types.CategoryIndexes:     3, // Indexes (after constraints)
		types.CategoryFunctions:   4, // Functions and procedures
		types.CategoryTriggers:    5, // Triggers (after functions)
		types.CategoryComments:    6, // COMMENT ON statements (after objects exist)
		types.CategorySecurity:    7, // RLS policies, grants
		types.CategoryData:        8, // INSERT/UPDATE data operations
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
		types.TypeSequence:  15,
		types.TypeTable:     16,

		// Constraints
		types.TypeConstraint: 20,

		// Indexes
		types.TypeIndex: 30,

		// Functions and procedures
		types.TypeFunction: 40,

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
	for categoryOrder := 0; categoryOrder < 8; categoryOrder++ {
		category := udr.getCategoryByOrder(categoryOrder)
		if objects, exists := categoryGroups[category]; exists {
			utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Processing category %s with %d objects", category, len(objects))

			// Sort within category using topological sort with cycle breaking
			sortedObjects, err := udr.resolveLifecycleWithinCategory(objects, category, graph)
			if err != nil {
				utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Warning: Error in category %s: %v", category, err)
				// Continue with best effort
				sortedObjects = udr.fallbackSortLifecycles(objects)
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
		// Single object or empty - no sorting needed
		var results []*tracking.ConsolidationResult
		for _, result := range categoryObjects {
			results = append(results, result)
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
	ObjectKey      string
	Result         *tracking.ConsolidationResult
	Dependencies   []string // Objects this depends on
	Provides       []string // Objects this provides/creates
	RequiredFirst  bool     // Must be first in category (extensions, schemas)
	RequiredLast   bool     // Must be last in category (data operations)
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
	objectType := strings.ToUpper(lifecycle.Name)
	switch {
	case strings.Contains(objectType, "EXTENSION"):
		return types.CategoryExtensions
	case strings.Contains(objectType, "SCHEMA") || strings.Contains(objectType, "TYPE") ||
		strings.Contains(objectType, "TABLE") || strings.Contains(objectType, "SEQUENCE"):
		return types.CategoryFoundation
	case strings.Contains(objectType, "INDEX"):
		return types.CategoryIndexes
	case strings.Contains(objectType, "FUNCTION") || strings.Contains(objectType, "PROCEDURE"):
		return types.CategoryFunctions
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
		return udr.fallbackSortLifecycles(objects), nil
	}

	// Strategy 1: Remove least important edges in cycles
	modifiedGraph := udr.removeCyclicEdges(subGraph, cycles)
	if sorted, err := modifiedGraph.TopologicalSort(); err == nil {
		utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Successfully broke %d cycles in category %s", len(cycles), category)
		return sorted, nil
	}

	// Strategy 2: Fallback to statement-type ordering
	return udr.fallbackSortLifecycles(objects), errors.NewError(
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

	for i := 0; i < len(cycle); i++ {
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
func (udr *UnifiedDependencyResolver) fallbackSortLifecycles(objects []tracking.ObjectID) []tracking.ObjectID {
	utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Using fallback sorting for %d lifecycle objects", len(objects))

	// Sort by object type order, then by name for stability
	sort.Slice(objects, func(i, j int) bool {
		// First, sort by type order
		orderI := udr.getTypeOrder(objects[i].Type)
		orderJ := udr.getTypeOrder(objects[j].Type)

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

	// CRITICAL: First, extract dependencies from original statements (from parser)
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
		// CRITICAL: Extract extension dependencies (e.g., earthdistance depends on cube)
		// BUG #1 REGRESSION FIX: Add extension-to-extension dependencies
		info.Dependencies = append(info.Dependencies, udr.extractExtensionDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractExtensionToExtensionDependencies(sql)...)
		info.Provides = append(info.Provides, udr.extractExtensionProvisions(sql)...)

	case types.CategoryFoundation:
		info.Dependencies = append(info.Dependencies, udr.extractSchemaDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractExtensionDependencies(sql)...)
		// Type dependencies are already extracted by parser from AST and included in stmt.Dependencies above
		// CRITICAL: Extract table-to-table dependencies (foreign keys) for correct ordering
		info.Dependencies = append(info.Dependencies, udr.extractTableDependencies(sql)...)
		// CRITICAL (Bug #9 Fix): Extract view-to-view dependencies from FROM/JOIN clauses
		// This ensures views are created in correct order when views depend on other views
		info.Dependencies = append(info.Dependencies, udr.extractViewDependencies(sql)...)
		// CRITICAL: Extract function dependencies (e.g., from CHECK constraints calling functions)
		// This ensures functions used in CHECK constraints are created before the tables
		info.Dependencies = append(info.Dependencies, udr.extractFunctionDependencies(sql)...)
		info.Provides = append(info.Provides, udr.extractTableProvisions(sql)...)
		info.Provides = append(info.Provides, udr.extractSchemaProvisions(sql)...)
		info.Provides = append(info.Provides, udr.extractTypeProvisions(sql)...)

	case types.CategoryConstraints:
		info.Dependencies = append(info.Dependencies, udr.extractTableDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractColumnDependencies(sql)...)

	case types.CategoryFunctions:
		info.Dependencies = append(info.Dependencies, udr.extractTableDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractSchemaDependencies(sql)...)
		info.Dependencies = append(info.Dependencies, udr.extractExtensionDependencies(sql)...)
		// CRITICAL: Extract function-to-function dependencies for correct ordering
		info.Dependencies = append(info.Dependencies, udr.extractFunctionDependencies(sql)...)
		// CRITICAL: Declare what functions this SQL provides for dependency resolution
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

	return result
}

// SQL extraction methods (from original DependencyResolver)

// extractTableDependencies finds table references in SQL, with special focus on foreign key dependencies
func (udr *UnifiedDependencyResolver) extractTableDependencies(sql string) []string {
	var deps []string

	// Priority 0: RETURNS SETOF table_name - Functions must be created after the table they return
	// Match: RETURNS SETOF [schema.]table_name
	returnsSetofPattern := regexp.MustCompile(`(?i)RETURNS\s+SETOF\s+(?:[\w]+\.)?(\w+)`)
	returnsMatches := returnsSetofPattern.FindAllStringSubmatch(sql, -1)
	for _, match := range returnsMatches {
		if len(match) > 1 {
			referencedTable := strings.TrimSpace(match[1])
			if referencedTable != "" {
				utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Found RETURNS SETOF dependency: function depends on table %s", referencedTable)
				deps = append(deps, referencedTable)
			}
		}
	}

	// Priority 1: FOREIGN KEY REFERENCES - These are critical for table creation order
	// Tables must be created before they can be referenced
	fkMatches := patterns.ForeignKeyPattern.FindAllStringSubmatch(sql, -1)
	for _, match := range fkMatches {
		if len(match) > 1 {
			referencedTable := strings.TrimSpace(match[1])
			if referencedTable != "" {
				utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Found foreign key dependency: table depends on %s", referencedTable)
				deps = append(deps, referencedTable)
			}
		}
	}

	// Priority 2: Direct REFERENCES clause (shorthand foreign key syntax)
	refMatches := patterns.DirectRefPattern.FindAllStringSubmatch(sql, -1)
	for _, match := range refMatches {
		if len(match) > 1 {
			referencedTable := strings.TrimSpace(match[1])
			if referencedTable != "" {
				utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Found direct reference dependency: table depends on %s", referencedTable)
				deps = append(deps, referencedTable)
			}
		}
	}

	// Priority 3: COMMENT ON statements - tables must exist before comments
	// COMMENT ON COLUMN table_name.column_name or COMMENT ON TABLE table_name
	commentMatches := patterns.CommentOnPattern.FindAllStringSubmatch(sql, -1)
	for _, match := range commentMatches {
		if len(match) > 1 {
			qualifiedName := strings.TrimSpace(match[1])
			// Extract table name from qualified name (e.g., "buddy_connections.group_name" -> "buddy_connections")
			parts := strings.Split(qualifiedName, ".")
			tableName := parts[0]
			if tableName != "" {
				utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Found COMMENT ON dependency: comment depends on table %s", tableName)
				deps = append(deps, tableName)
			}
		}
	}

	// Priority 4: Other table dependencies (for data operations)
	// Using precompiled patterns for INSERT, UPDATE, and general table references
	insertMatches := patterns.InsertIntoPattern.FindAllStringSubmatch(sql, -1)
	for _, match := range insertMatches {
		if len(match) > 1 {
			tableName := strings.TrimSpace(match[1])
			if tableName != "" && !strings.Contains(tableName, "(") {
				deps = append(deps, tableName)
			}
		}
	}

	updateMatches := patterns.UpdateTablePattern.FindAllStringSubmatch(sql, -1)
	for _, match := range updateMatches {
		if len(match) > 1 {
			tableName := strings.TrimSpace(match[1])
			if tableName != "" && !strings.Contains(tableName, "(") {
				deps = append(deps, tableName)
			}
		}
	}

	return deps
}

// extractViewDependencies finds view references in FROM/JOIN clauses
// CRITICAL FIX (Bug #9): Views can depend on other views, not just tables
// This method extracts view-to-view dependencies to ensure proper creation order
func (udr *UnifiedDependencyResolver) extractViewDependencies(sql string) []string {
	var deps []string

	// Only process if this is a CREATE VIEW statement
	if !regexp.MustCompile(`(?i)CREATE\s+(?:OR\s+REPLACE\s+)?VIEW`).MatchString(sql) {
		return deps
	}

	// Extract the view definition (everything after AS)
	asPattern := regexp.MustCompile(`(?is)CREATE\s+(?:OR\s+REPLACE\s+)?VIEW\s+[\w.]+.*?\s+AS\s+(.+)`)
	asMatch := asPattern.FindStringSubmatch(sql)
	if len(asMatch) < 2 {
		return deps
	}

	viewDef := asMatch[1]

	// Pattern 1: FROM clause - matches "FROM schema.object" or "FROM object"
	// Captures both tables and views referenced in FROM
	fromPattern := regexp.MustCompile(`(?i)\bFROM\s+(?:ONLY\s+)?(?:[\w]+\.)?(\w+)`)
	fromMatches := fromPattern.FindAllStringSubmatch(viewDef, -1)
	for _, match := range fromMatches {
		if len(match) > 1 {
			referencedObject := strings.TrimSpace(match[1])
			if referencedObject != "" && !isCommonSQLKeyword(referencedObject) {
				utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Found view FROM dependency: view depends on %s", referencedObject)
				deps = append(deps, referencedObject)
			}
		}
	}

	// Pattern 2: JOIN clause - matches "JOIN schema.object" or "JOIN object"
	// This captures INNER JOIN, LEFT JOIN, RIGHT JOIN, FULL JOIN, CROSS JOIN
	joinPattern := regexp.MustCompile(`(?i)\b(?:INNER\s+|LEFT\s+OUTER\s+|LEFT\s+|RIGHT\s+OUTER\s+|RIGHT\s+|FULL\s+OUTER\s+|FULL\s+|CROSS\s+)?JOIN\s+(?:[\w]+\.)?(\w+)`)
	joinMatches := joinPattern.FindAllStringSubmatch(viewDef, -1)
	for _, match := range joinMatches {
		if len(match) > 1 {
			referencedObject := strings.TrimSpace(match[1])
			if referencedObject != "" && !isCommonSQLKeyword(referencedObject) {
				utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Found view JOIN dependency: view depends on %s", referencedObject)
				deps = append(deps, referencedObject)
			}
		}
	}

	return deps
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

	// Look for schema qualified references
	matches := patterns.QualifiedNamePattern.FindAllStringSubmatch(sql, -1)
	for _, match := range matches {
		if len(match) > 1 {
			schema := strings.TrimSpace(match[1])
			if schema != "" && schema != "public" {
				deps = append(deps, fmt.Sprintf("schema:%s", schema))
			}
		}
	}

	return deps
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
// BUG #1 REGRESSION FIX: This ensures correct ordering of extension creation (e.g., cube before earthdistance)
func (udr *UnifiedDependencyResolver) extractExtensionToExtensionDependencies(sql string) []string {
	var deps []string

	// Map of extension name -> required extensions
	// These are hardcoded PostgreSQL extension dependencies
	extensionRequirements := map[string][]string{
		"earthdistance": {"cube"},           // earthdistance requires cube
		"postgis_tiger_geocoder": {"postgis"}, // postgis_tiger_geocoder requires postgis
		"postgis_topology": {"postgis"},     // postgis_topology requires postgis
		"postgis_raster": {"postgis"},       // postgis_raster requires postgis
		"address_standardizer": {"postgis"}, // address_standardizer requires postgis
	}

	// Extract which extension is being created
	matches := patterns.CreateExtensionPattern.FindAllStringSubmatch(sql, -1)
	for _, match := range matches {
		if len(match) > 1 {
			extensionName := strings.ToLower(strings.TrimSpace(match[1]))

			// Check if this extension has dependencies
			if requiredExtensions, exists := extensionRequirements[extensionName]; exists {
				for _, required := range requiredExtensions {
					deps = append(deps, fmt.Sprintf("extension:%s", required))
				}
			}
		}
	}

	return deps
}

// extractColumnDependencies finds column references that require prior ALTER TABLE
func (udr *UnifiedDependencyResolver) extractColumnDependencies(sql string) []string {
	var deps []string

	// Look for INSERT statements with specific columns
	matches := patterns.InsertIntoPattern.FindAllStringSubmatch(sql, -1)
	for _, match := range matches {
		if len(match) > 2 {
			tableName := strings.TrimSpace(match[1])
			columns := strings.Split(match[2], ",")
			for _, col := range columns {
				col = strings.TrimSpace(col)
				if col != "" {
					deps = append(deps, fmt.Sprintf("column:%s.%s", tableName, col))
				}
			}
		}
	}

	return deps
}

// extractFunctionDependencies finds function references
func (udr *UnifiedDependencyResolver) extractFunctionDependencies(sql string) []string {
	var deps []string

	// Look for function calls in trigger definitions (EXECUTE FUNCTION)
	matches := patterns.ExecuteFunctionPattern.FindAllStringSubmatch(sql, -1)
	for _, match := range matches {
		if len(match) > 1 {
			funcName := strings.TrimSpace(match[1])
			if funcName != "" {
				deps = append(deps, fmt.Sprintf("function:%s", funcName))
			}
		}
	}

	// Look for function calls within function bodies (function_name(...))
	// This regex matches function calls: function_name() with optional schema qualifier
	// Pattern: word characters followed by optional dot and word characters, then opening parenthesis
	callMatches := patterns.FunctionCallPattern.FindAllStringSubmatch(sql, -1)

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

	for _, match := range callMatches {
		if len(match) > 1 {
			funcName := strings.ToLower(strings.TrimSpace(match[1]))
			// Skip if already seen, is a SQL keyword, or contains dots (likely schema.table)
			if !seen[funcName] && !sqlKeywords[funcName] && funcName != "" {
				seen[funcName] = true
				deps = append(deps, fmt.Sprintf("function:%s", funcName))
			}
		}
	}

	return deps
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

	// Look for column references in SET clause
	matches := patterns.SetParameterPattern.FindAllStringSubmatch(sql, -1)
	for _, match := range matches {
		if len(match) > 1 {
			column := strings.TrimSpace(match[1])
			if column != "" {
				// Extract table name from UPDATE statement
				updateMatches := patterns.UpdateTablePattern.FindStringSubmatch(sql)
				if len(updateMatches) > 1 {
					tableName := strings.TrimSpace(updateMatches[1])
					deps = append(deps, fmt.Sprintf("column:%s.%s", tableName, column))
				}
			}
		}
	}

	return deps
}

// extractTableProvisions finds what tables/objects this SQL creates
func (udr *UnifiedDependencyResolver) extractTableProvisions(sql string) []string {
	var provides []string

	// Look for CREATE TABLE statements
	matches := patterns.CreateTableDetailPattern.FindAllStringSubmatch(sql, -1)
	for _, match := range matches {
		if len(match) > 1 {
			tableName := strings.TrimSpace(match[1])
			if tableName != "" {
				provides = append(provides, tableName)
			}
		}
	}

	// Note: Additional object types (SEQUENCE, VIEW, etc.) could be extracted here
	// if needed, but for now we focus on tables as the primary provision

	return provides
}

// extractFunctionProvisions finds what functions this SQL creates
func (udr *UnifiedDependencyResolver) extractFunctionProvisions(sql string) []string {
	var provides []string

	// Look for CREATE [OR REPLACE] FUNCTION statements
	matches := patterns.CreateFunctionDetailPattern.FindAllStringSubmatch(sql, -1)
	for _, match := range matches {
		if len(match) > 1 {
			funcName := strings.ToLower(strings.TrimSpace(match[1]))
			if funcName != "" {
				provides = append(provides, fmt.Sprintf("function:%s", funcName))
			}
		}
	}

	return provides
}

// extractSchemaProvisions finds what schemas this SQL creates
func (udr *UnifiedDependencyResolver) extractSchemaProvisions(sql string) []string {
	var provides []string

	matches := patterns.CreateSchemaDetailPattern.FindAllStringSubmatch(sql, -1)
	for _, match := range matches {
		if len(match) > 1 {
			schema := strings.TrimSpace(match[1])
			if schema != "" {
				provides = append(provides, fmt.Sprintf("schema:%s", schema))
			}
		}
	}

	return provides
}

// extractExtensionProvisions finds what extensions this SQL creates
func (udr *UnifiedDependencyResolver) extractExtensionProvisions(sql string) []string {
	var provides []string

	matches := patterns.CreateExtensionPattern.FindAllStringSubmatch(sql, -1)
	for _, match := range matches {
		if len(match) > 1 {
			extension := strings.TrimSpace(match[1])
			if extension != "" {
				provides = append(provides, fmt.Sprintf("extension:%s", extension))
			}
		}
	}

	return provides
}

// extractTypeDependencies finds type/enum references that need to be created first
func (udr *UnifiedDependencyResolver) extractTypeDependencies(sql string) []string {
	var deps []string
	sqlUpper := strings.ToUpper(sql)

	// Only scan CREATE TABLE statements for type dependencies
	if !strings.Contains(sqlUpper, "CREATE TABLE") {
		return deps
	}

	// Extract just the column definitions from CREATE TABLE
	// Match: CREATE TABLE ... ( ... column definitions ... )
	tableDefMatch := regexp.MustCompile(`(?is)CREATE\s+TABLE[^(]*\((.*?)\);?`).FindStringSubmatch(sql)
	if len(tableDefMatch) < 2 {
		return deps
	}

	columnDefs := tableDefMatch[1]

	// Now parse column definitions more carefully
	// Split by comma, but be careful of commas inside CHECK constraints
	// For simplicity, use a regex to find column definitions
	// Pattern: column_name custom_type (avoiding SQL keywords and built-in types)
	colMatches := patterns.ColumnTypePattern.FindAllStringSubmatch(columnDefs, -1)

	seenTypes := make(map[string]bool)
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

	for _, match := range colMatches {
		if len(match) > 2 {
			// match[1] is column name, match[2] is type name
			typeName := strings.ToLower(strings.TrimSpace(match[2]))

			// Skip PostgreSQL built-in types, SQL keywords, and empty strings
			if typeName == "" || postgresBuiltinTypes[typeName] || sqlKeywords[typeName] {
				continue
			}

			// Additional filtering: skip if it looks like a SQL keyword combination
			// (e.g., if "not" appears, it's likely "NOT NULL")
			if len(typeName) <= 3 {
				// Very short type names are likely keywords
				continue
			}

			// This is likely a custom type - add as dependency
			if !seenTypes[typeName] {
				seenTypes[typeName] = true
				deps = append(deps, fmt.Sprintf("type:%s", typeName))
				utils.GetDefaultLogger().WithPrefix("DEP-RESOLVER").Info("Table depends on custom type: %s", typeName)
			}
		}
	}

	return deps
}// extractTypeProvisions finds what types/enums this SQL creates
func (udr *UnifiedDependencyResolver) extractTypeProvisions(sql string) []string {
	var provides []string

	// Look for CREATE TYPE statements using EnumTypePattern
	matches := patterns.EnumTypePattern.FindAllStringSubmatch(sql, -1)
	for _, match := range matches {
		if len(match) > 1 {
			typeName := strings.TrimSpace(match[1])
			if typeName != "" {
				provides = append(provides, fmt.Sprintf("type:%s", typeName))
			}
		}
	}

	return provides
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
	if strings.HasPrefix(provision, "type:") {
		provisionType := strings.TrimPrefix(provision, "type:")
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
	// CRITICAL: Strip schema prefixes for comparison to handle both "blueprints" and "public.blueprints"
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

	// Check if this is an extension creation
	return patterns.CreateExtensionWithVersionPattern.ReplaceAllStringFunc(sql, func(match string) string {
		parts := patterns.CreateExtensionWithVersionPattern.FindStringSubmatch(match)
		if len(parts) < 4 {
			return match
		}

		prefix := parts[1]
		extensionName := parts[2]
		suffix := parts[3]

		// Add CASCADE if needed and not already present
		if cascadeExtensions[strings.ToLower(extensionName)] && !strings.Contains(strings.ToLower(match), "cascade") {
			return prefix + extensionName + " CASCADE" + suffix
		}

		return match
	})
}