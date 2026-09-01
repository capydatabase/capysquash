package tracking

import (
	"fmt"
	"strings"
	"time"
)

// DDLCycleType represents different types of DDL cycles that can be detected
type DDLCycleType string

const (
	SimpleCycle     DDLCycleType = "SIMPLE"     // A->B->A
	ComplexCycle    DDLCycleType = "COMPLEX"    // A->B->C->A
	DependencyCycle DDLCycleType = "DEPENDENCY" // Circular dependencies
	TransientCycle  DDLCycleType = "TRANSIENT"  // Object created and destroyed
	VersioningCycle DDLCycleType = "VERSIONING" // Object recreated with versions
	ConstraintCycle DDLCycleType = "CONSTRAINT" // Constraint dependencies
)

// DDLCycle represents a detected DDL cycle
type DDLCycle struct {
	Type          DDLCycleType        `json:"type"`
	Objects       []string            `json:"objects"`
	Operations    []DDLCycleOperation `json:"operations"`
	StartSequence int                 `json:"start_sequence"`
	EndSequence   int                 `json:"end_sequence"`
	Severity      CycleSeverity       `json:"severity"`
	CanOptimize   bool                `json:"can_optimize"`
	Description   string              `json:"description"`
	Dependencies  []string            `json:"dependencies,omitempty"`
}

// DDLCycleOperation represents an operation within a cycle
type DDLCycleOperation struct {
	Sequence  int       `json:"sequence"`
	Operation string    `json:"operation"`
	Object    string    `json:"object"`
	SQL       string    `json:"sql"`
	Timestamp time.Time `json:"timestamp"`
}

// CycleSeverity indicates how problematic a cycle is
type CycleSeverity string

const (
	SeverityLow      CycleSeverity = "LOW"      // Can be safely optimized
	SeverityMedium   CycleSeverity = "MEDIUM"   // Requires careful handling
	SeverityHigh     CycleSeverity = "HIGH"     // Potentially problematic
	SeverityCritical CycleSeverity = "CRITICAL" // Must be preserved as-is
)

// AdvancedDDLCycleDetector detects and analyzes complex DDL cycles
type AdvancedDDLCycleDetector struct {
	enableDeepScan         bool
	enableDependencyTrack  bool
	enableVersionDetection bool
	maxCycleDepth          int
	minCycleLength         int
}

// CycleDetectionConfig configures the cycle detection behavior
type CycleDetectionConfig struct {
	EnableDeepScan         bool // Perform deep analysis of cycles
	EnableDependencyTrack  bool // Track cross-object dependencies
	EnableVersionDetection bool // Detect object versioning patterns
	MaxCycleDepth          int  // Maximum depth to analyze
	MinCycleLength         int  // Minimum operations to consider a cycle
}

// NewAdvancedDDLCycleDetector creates a new advanced DDL cycle detector
func NewAdvancedDDLCycleDetector(config CycleDetectionConfig) *AdvancedDDLCycleDetector {
	return &AdvancedDDLCycleDetector{
		enableDeepScan:         config.EnableDeepScan,
		enableDependencyTrack:  config.EnableDependencyTrack,
		enableVersionDetection: config.EnableVersionDetection,
		maxCycleDepth:          maxInt(config.MaxCycleDepth, 10),
		minCycleLength:         maxInt(config.MinCycleLength, 2),
	}
}

// DetectCycles analyzes object lifecycles to identify DDL cycles
func (d *AdvancedDDLCycleDetector) DetectCycles(lifecycles map[string]*ObjectLifecycle) ([]DDLCycle, error) {
	var allCycles []DDLCycle

	// Detect different types of cycles
	simpleCycles := d.detectSimpleCycles(lifecycles)
	allCycles = append(allCycles, simpleCycles...)

	complexCycles := d.detectComplexCycles(lifecycles)
	allCycles = append(allCycles, complexCycles...)

	if d.enableDependencyTrack {
		dependencyCycles := d.detectDependencyCycles(lifecycles)
		allCycles = append(allCycles, dependencyCycles...)
	}

	transientCycles := d.detectTransientCycles(lifecycles)
	allCycles = append(allCycles, transientCycles...)

	if d.enableVersionDetection {
		versioningCycles := d.detectVersioningCycles(lifecycles)
		allCycles = append(allCycles, versioningCycles...)
	}

	constraintCycles := d.detectConstraintCycles(lifecycles)
	allCycles = append(allCycles, constraintCycles...)

	// Analyze severity and optimization potential
	d.analyzeCycles(&allCycles)

	return allCycles, nil
}

// detectSimpleCycles detects simple A->B->A patterns
func (d *AdvancedDDLCycleDetector) detectSimpleCycles(lifecycles map[string]*ObjectLifecycle) []DDLCycle {
	var cycles []DDLCycle

	for objName, lifecycle := range lifecycles {
		if len(lifecycle.History) < d.minCycleLength {
			continue
		}

		// Look for CREATE -> DROP -> CREATE patterns
		createCount := 0
		dropCount := 0
		var operations []DDLCycleOperation

		for i, event := range lifecycle.History {
			operation := DDLCycleOperation{
				Sequence:  i,
				Operation: string(event.Operation),
				Object:    objName,
				SQL:       event.Statement.SQL,
			}

			switch event.Operation {
			case "CREATE":
				createCount++
				operations = append(operations, operation)
			case "DROP":
				dropCount++
				operations = append(operations, operation)
			}
		}

		// Simple cycle: at least one CREATE-DROP pair
		if createCount > 1 || (createCount >= 1 && dropCount >= 1) {
			cycle := DDLCycle{
				Type:          SimpleCycle,
				Objects:       []string{objName},
				Operations:    operations,
				StartSequence: 0,
				EndSequence:   len(lifecycle.History) - 1,
				CanOptimize:   d.canOptimizeSimpleCycle(operations),
				Description:   fmt.Sprintf("Simple DDL cycle detected for %s (%d creates, %d drops)", objName, createCount, dropCount),
			}
			cycles = append(cycles, cycle)
		}
	}

	return cycles
}

// detectComplexCycles detects multi-object cycles
func (d *AdvancedDDLCycleDetector) detectComplexCycles(lifecycles map[string]*ObjectLifecycle) []DDLCycle {
	var cycles []DDLCycle

	if !d.enableDeepScan {
		return cycles
	}

	// Build cross-references between objects
	objectGraph := d.buildObjectGraph(lifecycles)

	// Use graph algorithms to detect cycles
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for objName := range objectGraph {
		if !visited[objName] {
			if cyclePath := d.dfsDetectCycle(objName, objectGraph, visited, recStack, []string{}); cyclePath != nil {
				if len(cyclePath) >= d.minCycleLength {
					cycle := d.buildComplexCycle(cyclePath, lifecycles)
					cycles = append(cycles, cycle)
				}
			}
		}
	}

	return cycles
}

// detectDependencyCycles detects cycles in object dependencies
func (d *AdvancedDDLCycleDetector) detectDependencyCycles(lifecycles map[string]*ObjectLifecycle) []DDLCycle {
	var cycles []DDLCycle

	// Track foreign key relationships, constraint dependencies, etc.
	dependencies := d.buildDependencyGraph(lifecycles)

	// Find circular dependencies
	for objName, deps := range dependencies {
		for _, depName := range deps {
			if d.hasCircularDependency(objName, depName, dependencies, make(map[string]bool)) {
				cycle := DDLCycle{
					Type:         DependencyCycle,
					Objects:      []string{objName, depName},
					Severity:     SeverityHigh,
					CanOptimize:  false,
					Description:  fmt.Sprintf("Circular dependency detected between %s and %s", objName, depName),
					Dependencies: []string{objName, depName},
				}
				cycles = append(cycles, cycle)
			}
		}
	}

	return cycles
}

// detectTransientCycles detects objects that are created and destroyed within the same lifecycle
func (d *AdvancedDDLCycleDetector) detectTransientCycles(lifecycles map[string]*ObjectLifecycle) []DDLCycle {
	var cycles []DDLCycle

	for objName, lifecycle := range lifecycles {
		hasCreate := false
		hasDrop := false
		var operations []DDLCycleOperation

		for i, event := range lifecycle.History {
			switch event.Operation {
			case "CREATE":
				hasCreate = true
			case "DROP":
				hasDrop = true
			}

			operations = append(operations, DDLCycleOperation{
				Sequence:  i,
				Operation: string(event.Operation),
				Object:    objName,
				SQL:       event.Statement.SQL,
			})
		}

		// Transient: object created and dropped in same migration set
		if hasCreate && hasDrop {
			cycle := DDLCycle{
				Type:          TransientCycle,
				Objects:       []string{objName},
				Operations:    operations,
				StartSequence: 0,
				EndSequence:   len(lifecycle.History) - 1,
				Severity:      SeverityLow,
				CanOptimize:   true,
				Description:   fmt.Sprintf("Transient object %s (created and dropped)", objName),
			}
			cycles = append(cycles, cycle)
		}
	}

	return cycles
}

// detectVersioningCycles detects objects that follow versioning patterns
func (d *AdvancedDDLCycleDetector) detectVersioningCycles(lifecycles map[string]*ObjectLifecycle) []DDLCycle {
	var cycles []DDLCycle

	// Group objects by base name (removing version suffixes)
	versionGroups := make(map[string][]string)

	for objName := range lifecycles {
		baseName := d.extractBaseName(objName)
		versionGroups[baseName] = append(versionGroups[baseName], objName)
	}

	// Look for versioning patterns
	for baseName, versions := range versionGroups {
		if len(versions) > 1 {
			cycle := DDLCycle{
				Type:        VersioningCycle,
				Objects:     versions,
				Severity:    SeverityMedium,
				CanOptimize: d.canOptimizeVersioningCycle(versions, lifecycles),
				Description: fmt.Sprintf("Object versioning detected for %s (%d versions)", baseName, len(versions)),
			}
			cycles = append(cycles, cycle)
		}
	}

	return cycles
}

// detectConstraintCycles detects cycles in constraint definitions
func (d *AdvancedDDLCycleDetector) detectConstraintCycles(lifecycles map[string]*ObjectLifecycle) []DDLCycle {
	var cycles []DDLCycle

	constraintDeps := d.buildConstraintDependencies(lifecycles)

	for constraintName, deps := range constraintDeps {
		// Check if constraint references itself through other constraints
		if d.hasConstraintCycle(constraintName, deps, constraintDeps, make(map[string]bool)) {
			cycle := DDLCycle{
				Type:        ConstraintCycle,
				Objects:     append([]string{constraintName}, deps...),
				Severity:    SeverityCritical,
				CanOptimize: false,
				Description: fmt.Sprintf("Constraint cycle involving %s", constraintName),
			}
			cycles = append(cycles, cycle)
		}
	}

	return cycles
}

// Helper methods

func (d *AdvancedDDLCycleDetector) buildObjectGraph(lifecycles map[string]*ObjectLifecycle) map[string][]string {
	graph := make(map[string][]string)

	for objName, lifecycle := range lifecycles {
		var references []string

		for _, event := range lifecycle.History {
			// Extract references from SQL (simplified)
			refs := d.extractObjectReferences(event.Statement.SQL)
			references = append(references, refs...)
		}

		graph[objName] = references
	}

	return graph
}

func (d *AdvancedDDLCycleDetector) dfsDetectCycle(node string, graph map[string][]string, visited, recStack map[string]bool, path []string) []string {
	visited[node] = true
	recStack[node] = true
	path = append(path, node)

	for _, neighbor := range graph[node] {
		if !visited[neighbor] {
			if cyclePath := d.dfsDetectCycle(neighbor, graph, visited, recStack, path); cyclePath != nil {
				return cyclePath
			}
		} else if recStack[neighbor] {
			// Found cycle - return path from neighbor to current node
			cycleStart := -1
			for i, pathNode := range path {
				if pathNode == neighbor {
					cycleStart = i
					break
				}
			}
			if cycleStart >= 0 {
				return append(path[cycleStart:], neighbor)
			}
		}
	}

	recStack[node] = false
	return nil
}

func (d *AdvancedDDLCycleDetector) buildDependencyGraph(lifecycles map[string]*ObjectLifecycle) map[string][]string {
	dependencies := make(map[string][]string)

	for objName, lifecycle := range lifecycles {
		var deps []string

		for _, event := range lifecycle.History {
			// Extract foreign key references, constraint dependencies
			if strings.Contains(strings.ToUpper(event.Statement.SQL), "FOREIGN KEY") {
				refs := d.extractForeignKeyReferences(event.Statement.SQL)
				deps = append(deps, refs...)
			}
		}

		if len(deps) > 0 {
			dependencies[objName] = deps
		}
	}

	return dependencies
}

func (d *AdvancedDDLCycleDetector) hasCircularDependency(start, current string, dependencies map[string][]string, visited map[string]bool) bool {
	if visited[current] {
		return current == start
	}

	visited[current] = true

	for _, dep := range dependencies[current] {
		if d.hasCircularDependency(start, dep, dependencies, visited) {
			return true
		}
	}

	return false
}

func (d *AdvancedDDLCycleDetector) buildComplexCycle(cyclePath []string, lifecycles map[string]*ObjectLifecycle) DDLCycle {
	var operations []DDLCycleOperation
	startSeq := int(^uint(0) >> 1) // Max int
	endSeq := 0

	for _, objName := range cyclePath {
		if lifecycle, exists := lifecycles[objName]; exists {
			for i, event := range lifecycle.History {
				operations = append(operations, DDLCycleOperation{
					Sequence:  i,
					Operation: string(event.Operation),
					Object:    objName,
					SQL:       event.Statement.SQL,
				})

				if i < startSeq {
					startSeq = i
				}
				if i > endSeq {
					endSeq = i
				}
			}
		}
	}

	return DDLCycle{
		Type:          ComplexCycle,
		Objects:       cyclePath,
		Operations:    operations,
		StartSequence: startSeq,
		EndSequence:   endSeq,
		Severity:      SeverityMedium,
		CanOptimize:   d.canOptimizeComplexCycle(cyclePath),
		Description:   fmt.Sprintf("Complex DDL cycle involving %d objects: %s", len(cyclePath), strings.Join(cyclePath, " -> ")),
	}
}

func (d *AdvancedDDLCycleDetector) analyzeCycles(cycles *[]DDLCycle) {
	for i := range *cycles {
		cycle := &(*cycles)[i]

		// Assign severity based on cycle characteristics
		cycle.Severity = d.calculateCycleSeverity(cycle)

		// Determine optimization potential
		cycle.CanOptimize = d.canOptimizeCycle(cycle)
	}
}

func (d *AdvancedDDLCycleDetector) calculateCycleSeverity(cycle *DDLCycle) CycleSeverity {
	// Base severity on cycle type
	switch cycle.Type {
	case TransientCycle:
		return SeverityLow
	case SimpleCycle:
		return SeverityMedium
	case VersioningCycle:
		return SeverityMedium
	case ComplexCycle:
		if len(cycle.Objects) > 5 {
			return SeverityHigh
		}
		return SeverityMedium
	case DependencyCycle, ConstraintCycle:
		return SeverityCritical
	default:
		return SeverityMedium
	}
}

func (d *AdvancedDDLCycleDetector) canOptimizeCycle(cycle *DDLCycle) bool {
	switch cycle.Severity {
	case SeverityLow:
		return true
	case SeverityMedium:
		return cycle.Type != DependencyCycle && cycle.Type != ConstraintCycle
	case SeverityHigh, SeverityCritical:
		return false
	default:
		return false
	}
}

func (d *AdvancedDDLCycleDetector) canOptimizeSimpleCycle(operations []DDLCycleOperation) bool {
	// Can optimize if final state is different from initial
	if len(operations) == 0 {
		return false
	}

	// Simple heuristic: if ends with CREATE, can probably optimize
	lastOp := operations[len(operations)-1]
	return lastOp.Operation == "CREATE"
}

func (d *AdvancedDDLCycleDetector) canOptimizeComplexCycle(cyclePath []string) bool {
	// Complex cycles are harder to optimize safely
	return len(cyclePath) <= 3
}

func (d *AdvancedDDLCycleDetector) canOptimizeVersioningCycle(versions []string, lifecycles map[string]*ObjectLifecycle) bool {
	// Can optimize if all versions end in same final state
	for _, version := range versions {
		if lifecycle, exists := lifecycles[version]; exists {
			if len(lifecycle.History) > 0 {
				lastOp := lifecycle.History[len(lifecycle.History)-1]
				if lastOp.Operation != "CREATE" {
					return false
				}
			}
		}
	}
	return true
}

func (d *AdvancedDDLCycleDetector) extractBaseName(objName string) string {
	// Remove common version suffixes: _v1, _v2, _2023, etc.
	patterns := []string{"_v", "_V", "_version", "_VERSION"}

	baseName := objName
	for _, pattern := range patterns {
		if idx := strings.Index(baseName, pattern); idx > 0 {
			baseName = baseName[:idx]
			break
		}
	}

	return baseName
}

func (d *AdvancedDDLCycleDetector) extractObjectReferences(sql string) []string {
	var refs []string
	upperSQL := strings.ToUpper(sql)

	// Simple reference extraction (would be more sophisticated in practice)
	keywords := []string{"REFERENCES", "FROM", "JOIN", "UPDATE", "INSERT INTO"}

	for _, keyword := range keywords {
		if strings.Contains(upperSQL, keyword) {
			// Extract table names after keywords
			parts := strings.Fields(upperSQL)
			for i, part := range parts {
				if part == keyword && i+1 < len(parts) {
					refs = append(refs, strings.ToLower(parts[i+1]))
				}
			}
		}
	}

	return refs
}

func (d *AdvancedDDLCycleDetector) extractForeignKeyReferences(sql string) []string {
	var refs []string
	upperSQL := strings.ToUpper(sql)

	if strings.Contains(upperSQL, "REFERENCES") {
		parts := strings.Fields(upperSQL)
		for i, part := range parts {
			if part == "REFERENCES" && i+1 < len(parts) {
				refs = append(refs, strings.ToLower(parts[i+1]))
			}
		}
	}

	return refs
}

func (d *AdvancedDDLCycleDetector) buildConstraintDependencies(lifecycles map[string]*ObjectLifecycle) map[string][]string {
	constraints := make(map[string][]string)

	for _, lifecycle := range lifecycles {
		for _, event := range lifecycle.History {
			if strings.Contains(strings.ToUpper(event.Statement.SQL), "CONSTRAINT") {
				constraintName := d.extractConstraintName(event.Statement.SQL)
				if constraintName != "" {
					deps := d.extractConstraintDependencies(event.Statement.SQL)
					constraints[constraintName] = deps
				}
			}
		}
	}

	return constraints
}

func (d *AdvancedDDLCycleDetector) hasConstraintCycle(start string, deps []string, constraints map[string][]string, visited map[string]bool) bool {
	for _, dep := range deps {
		if visited[dep] {
			continue
		}

		if dep == start {
			return true
		}

		visited[dep] = true
		if childDeps, exists := constraints[dep]; exists {
			if d.hasConstraintCycle(start, childDeps, constraints, visited) {
				return true
			}
		}
	}

	return false
}

func (d *AdvancedDDLCycleDetector) extractConstraintName(sql string) string {
	upperSQL := strings.ToUpper(sql)
	parts := strings.Fields(upperSQL)

	for i, part := range parts {
		if part == "CONSTRAINT" && i+1 < len(parts) {
			return strings.ToLower(parts[i+1])
		}
	}

	return ""
}

func (d *AdvancedDDLCycleDetector) extractConstraintDependencies(sql string) []string {
	// Simplified - would extract actual constraint dependencies
	return d.extractObjectReferences(sql)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
