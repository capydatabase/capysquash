package tracking

import (
	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// AddNode adds a node to the dependency graph
func (dg *DependencyGraph) AddNode(objectID ObjectID) {
	if _, exists := dg.nodes[objectID]; !exists {
		dg.nodes[objectID] = &DependencyNode{
			ID:           objectID,
			Dependencies: []ObjectID{},
			Dependents:   []ObjectID{},
		}
	}
}

// AddEdge adds a dependency edge to the graph
func (dg *DependencyGraph) AddEdge(from, to ObjectID) {
	dg.AddNode(from)
	dg.AddNode(to)

	// Add dependency
	fromNode := dg.nodes[from]
	if !containsObjectID(fromNode.Dependencies, to) {
		fromNode.Dependencies = append(fromNode.Dependencies, to)
	}

	// Add dependent
	toNode := dg.nodes[to]
	if !containsObjectID(toNode.Dependents, from) {
		toNode.Dependents = append(toNode.Dependents, from)
	}

	// Update edges map
	if _, exists := dg.edges[from]; !exists {
		dg.edges[from] = []ObjectID{}
	}
	if !containsObjectID(dg.edges[from], to) {
		dg.edges[from] = append(dg.edges[from], to)
	}
}

// TopologicalSort performs topological sorting of the dependency graph
func (dg *DependencyGraph) TopologicalSort() ([]ObjectID, error) {
	// Kahn's algorithm implementation
	inDegree := make(map[ObjectID]int)
	result := make([]ObjectID, 0, len(dg.nodes))
	queue := make([]ObjectID, 0)

	// Initialize in-degrees
	for id := range dg.nodes {
		inDegree[id] = len(dg.nodes[id].Dependencies)
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}

	// Process queue
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)

		// Update in-degrees of dependents
		for _, dependent := range dg.nodes[current].Dependents {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// Check for cycles
	if len(result) != len(dg.nodes) {
		return nil, errors.New(errors.ErrorCodeConsolidationFailed, errors.CategoryConsolidation,
			"circular dependencies detected",
			map[string]interface{}{
				"expected_nodes": len(dg.nodes),
				"sorted_nodes": len(result),
			})
	}

	return result, nil
}

// DetectCycles detects circular dependencies in the graph
func (dg *DependencyGraph) DetectCycles() [][]ObjectID {
	var cycles [][]ObjectID
	visited := make(map[ObjectID]bool)
	recStack := make(map[ObjectID]bool)
	path := make([]ObjectID, 0)

	for id := range dg.nodes {
		if !visited[id] {
			if cycle := dg.dfsDetectCycle(id, visited, recStack, path); cycle != nil {
				cycles = append(cycles, cycle)
			}
		}
	}

	return cycles
}

// dfsDetectCycle performs DFS to detect cycles
func (dg *DependencyGraph) dfsDetectCycle(id ObjectID, visited, recStack map[ObjectID]bool, path []ObjectID) []ObjectID {
	visited[id] = true
	recStack[id] = true
	path = append(path, id)

	for _, dep := range dg.nodes[id].Dependencies {
		if !visited[dep] {
			if cycle := dg.dfsDetectCycle(dep, visited, recStack, path); cycle != nil {
				return cycle
			}
		} else if recStack[dep] {
			// Found cycle - return the cycle path
			for i, pathID := range path {
				if pathID == dep {
					return path[i:]
				}
			}
		}
	}

	recStack[id] = false
	return nil
}

// GetDependencyChain returns the dependency chain for an object
func (dg *DependencyGraph) GetDependencyChain(objectID ObjectID) []ObjectID {
	var chain []ObjectID
	visited := make(map[ObjectID]bool)

	dg.collectDependencies(objectID, visited, &chain)
	return chain
}

// collectDependencies recursively collects dependencies
func (dg *DependencyGraph) collectDependencies(objectID ObjectID, visited map[ObjectID]bool, chain *[]ObjectID) {
	if visited[objectID] {
		return
	}

	visited[objectID] = true
	*chain = append(*chain, objectID)

	if node, exists := dg.nodes[objectID]; exists {
		for _, dep := range node.Dependencies {
			dg.collectDependencies(dep, visited, chain)
		}
	}
}

// GetLevelOrder returns objects grouped by dependency level
func (dg *DependencyGraph) GetLevelOrder() [][]ObjectID {
	// Calculate levels using topological sort
	sorted, err := dg.TopologicalSort()
	if err != nil {
		return nil
	}

	levels := make([][]ObjectID, 0)
	levelMap := make(map[ObjectID]int)

	// Calculate levels
	for _, id := range sorted {
		maxDepLevel := -1
		for _, dep := range dg.nodes[id].Dependencies {
			if level, exists := levelMap[dep]; exists && level > maxDepLevel {
				maxDepLevel = level
			}
		}
		levelMap[id] = maxDepLevel + 1
		dg.nodes[id].Level = maxDepLevel + 1
	}

	// Group by levels
	levelObjects := make(map[int][]ObjectID)
	maxLevel := 0
	for id, level := range levelMap {
		levelObjects[level] = append(levelObjects[level], id)
		if level > maxLevel {
			maxLevel = level
		}
	}

	// Convert to slice
	for i := 0; i <= maxLevel; i++ {
		if objects, exists := levelObjects[i]; exists {
			levels = append(levels, objects)
		} else {
			levels = append(levels, []ObjectID{})
		}
	}

	return levels
}

// GetNode returns a dependency node by ID
func (dg *DependencyGraph) GetNode(objectID ObjectID) *DependencyNode {
	return dg.nodes[objectID]
}

// GetAllNodes returns all nodes in the graph
func (dg *DependencyGraph) GetAllNodes() map[ObjectID]*DependencyNode {
	return dg.nodes
}

// RemoveNode removes a node and all its edges from the graph
func (dg *DependencyGraph) RemoveNode(objectID ObjectID) {
	if node, exists := dg.nodes[objectID]; exists {
		// Remove from dependents of dependencies
		for _, dep := range node.Dependencies {
			if depNode, exists := dg.nodes[dep]; exists {
				depNode.Dependents = removeObjectID(depNode.Dependents, objectID)
			}
		}

		// Remove from dependencies of dependents
		for _, dependent := range node.Dependents {
			if depNode, exists := dg.nodes[dependent]; exists {
				depNode.Dependencies = removeObjectID(depNode.Dependencies, objectID)
			}
		}

		// Remove from edges
		delete(dg.edges, objectID)
		for from, deps := range dg.edges {
			dg.edges[from] = removeObjectID(deps, objectID)
		}

		// Remove the node
		delete(dg.nodes, objectID)
	}
}

// IsEmpty returns true if the graph has no nodes
func (dg *DependencyGraph) IsEmpty() bool {
	return len(dg.nodes) == 0
}

// Size returns the number of nodes in the graph
func (dg *DependencyGraph) Size() int {
	return len(dg.nodes)
}

// HasCycles returns true if the graph contains cycles
func (dg *DependencyGraph) HasCycles() bool {
	cycles := dg.DetectCycles()
	return len(cycles) > 0
}

// Helper functions

// containsObjectID checks if slice contains ObjectID
func containsObjectID(slice []ObjectID, item ObjectID) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// removeObjectID removes ObjectID from slice
func removeObjectID(slice []ObjectID, item ObjectID) []ObjectID {
	result := make([]ObjectID, 0, len(slice))
	for _, s := range slice {
		if s != item {
			result = append(result, s)
		}
	}
	return result
}
