package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	internalparser "github.com/capysquash/pgsquash-engine/internal/parser"
	harnesscontract "github.com/capysquash/pgsquash-engine/pkg/harness"
)

type DeterministicHarnessMigration struct {
	MigrationID string
	Sequence    int
	SQL         string
}

type DeterministicHarnessContextOptions struct {
	OutputSQLPath           string
	EngineVersion           string
	OriginalMigrationFiles  int
	ValidationStatus        string
	ValidationMode          string
	AnalysisWarnings        []string
	AnalysisRecommendations []string
	OriginalMigrations      []DeterministicHarnessMigration
}

func BuildDeterministicHarnessContext(result *SquashResult, options DeterministicHarnessContextOptions) (*harnesscontract.HarnessContextV1, error) {
	if result == nil {
		return nil, fmt.Errorf("result cannot be nil")
	}

	engineVersion := strings.TrimSpace(options.EngineVersion)
	if engineVersion == "" {
		engineVersion = strings.TrimSpace(result.ProvenanceInfoValueVersion())
	}
	if engineVersion == "" {
		engineVersion = "unknown"
	}

	safetyLevel := strings.TrimSpace(result.ProvenanceInfoValueSafetyLevel())
	if safetyLevel == "" {
		safetyLevel = "standard"
	}

	validationStatus := strings.TrimSpace(options.ValidationStatus)
	if validationStatus == "" {
		validationStatus = "unknown"
	}
	validationMode := strings.TrimSpace(options.ValidationMode)
	if validationMode == "" {
		validationMode = "TWO_DATABASES"
	}

	migrationID := strings.TrimSpace(options.OutputSQLPath)
	if migrationID == "" {
		migrationID = "000_baseline.sql"
	}

	confidence := "high"
	if strings.EqualFold(safetyLevel, "aggressive") {
		confidence = "medium"
	}

	candidate := harnesscontract.ContextSquashCandidate{
		GroupID:      "deterministic-squash-output",
		MigrationIDs: []string{migrationID},
		Confidence:   confidence,
		Reason:       fmt.Sprintf("deterministic squash output (%d statements, validation=%s/%s)", countStatements(result.BaselineSQL), validationStatus, validationMode),
		// The context represents an already-generated deterministic artifact.
		// Confirm is the only action that can be applied without inventing an
		// undefined SQL rewrite for reject/split.
		AllowedActions: []string{"confirm"},
	}

	warnings := dedupeNonEmpty(options.AnalysisWarnings)
	ambiguousCases := make([]harnesscontract.ContextAmbiguousCase, 0, len(warnings))
	for index, warning := range warnings {
		if warning == "" {
			continue
		}
		caseID := fmt.Sprintf("warn-%03d", index+1)
		riskLevel := classifyWarningRisk(warning)
		ambiguousCases = append(ambiguousCases, harnesscontract.ContextAmbiguousCase{
			Type:             "deterministic_warning",
			Summary:          warning,
			Reason:           "deterministic analyzer warning",
			RawSQLExcerpt:    "",
			Context:          "squash_finalization",
			CaseID:           caseID,
			MigrationID:      migrationID,
			RiskLevel:        riskLevel,
			AllowedDecisions: []string{"preserve", "manual_review"},
		})
	}

	migrationChain, originalStatements, dataOperations, workspaceHash, err := buildMigrationEvidence(options.OriginalMigrations, migrationID, result.BaselineSQL)
	if err != nil {
		return nil, err
	}
	schemaObjects, dependencyGraph, err := buildSchemaEvidence(result.BaselineSQL, migrationID)
	if err != nil {
		return nil, err
	}

	schemaState := harnesscontract.ContextSchemaState{
		SchemaSQLPath:  strings.TrimSpace(options.OutputSQLPath),
		SchemaSQLHash:  hashValue(result.BaselineSQL),
		StatementCount: countStatements(result.BaselineSQL),
		ObjectCount:    len(schemaObjects),
		Objects:        schemaObjects,
		Extensions:     append([]string(nil), result.Extensions...),
	}

	workspace := harnesscontract.ContextWorkspace{
		ProjectRoot:      ".",
		OutputDirectory:  resolveOutputDirectory(options.OutputSQLPath),
		OriginalFileRoot: ".",
		FileCount:        options.OriginalMigrationFiles,
		WorkspaceHash:    workspaceHash,
	}

	configSnapshot := harnesscontract.ContextConfigSnapshot{
		SafetyLevel:    safetyLevel,
		ValidationMode: validationMode,
	}

	context := &harnesscontract.HarnessContextV1{
		ContextVersion:        harnesscontract.HarnessContextV1Version,
		GeneratedAt:           time.Now().UTC(),
		EngineVersion:         engineVersion,
		Workspace:             workspace,
		ConfigSnapshot:        configSnapshot,
		SchemaState:           schemaState,
		MigrationChain:        migrationChain,
		DependencyGraph:       dependencyGraph,
		SquashCandidates:      []harnesscontract.ContextSquashCandidate{candidate},
		AmbiguousCases:        ambiguousCases,
		DeterministicWarnings: warnings,
		Metrics: harnesscontract.ContextMetrics{
			OriginalFileCount:      options.OriginalMigrationFiles,
			OriginalStatementCount: originalStatements,
			DataOperationCount:     dataOperations,
			ObjectsConsolidated:    result.ObjectsConsolidated,
			WarningsCount:          len(warnings),
			ProcessingTime:         strings.TrimSpace(result.ProcessingTime),
		},
	}

	hash, err := harnesscontract.ComputeHarnessContextHash(context)
	if err != nil {
		return nil, err
	}
	context.ContextHash = hash

	return context, nil
}

func buildMigrationEvidence(inputs []DeterministicHarnessMigration, fallbackID, fallbackSQL string) ([]harnesscontract.ContextMigrationNode, int, int, string, error) {
	if len(inputs) == 0 {
		return []harnesscontract.ContextMigrationNode{{
			MigrationID: fallbackID,
			Sequence:    1,
			Checksum:    hashValue(fallbackSQL),
			SquashSafe:  true,
		}}, countStatements(fallbackSQL), 0, hashValue(fallbackID + "\x00" + fallbackSQL), nil
	}

	ordered := append([]DeterministicHarnessMigration(nil), inputs...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].Sequence == ordered[j].Sequence {
			return ordered[i].MigrationID < ordered[j].MigrationID
		}
		return ordered[i].Sequence < ordered[j].Sequence
	})
	chain := make([]harnesscontract.ContextMigrationNode, 0, len(ordered))
	workspaceParts := make([]string, 0, len(ordered)*2)
	totalStatements := 0
	dataOperations := 0
	for index, input := range ordered {
		migrationID := strings.TrimSpace(input.MigrationID)
		if migrationID == "" {
			migrationID = fmt.Sprintf("migration-%04d.sql", index+1)
		}
		parsed, err := internalparser.ParseMigrationWithContext(context.Background(), input.SQL, migrationID)
		if err != nil {
			return nil, 0, 0, "", fmt.Errorf("parse migration evidence %s: %w", migrationID, err)
		}
		operations := map[string]struct{}{}
		objects := map[string]struct{}{}
		semantic := map[string]struct{}{}
		riskFlags := map[string]struct{}{}
		squashSafe := true
		for _, statement := range parsed.Statements {
			totalStatements++
			operations[string(statement.Operation)] = struct{}{}
			if statement.ObjectName != "" {
				objects[qualifiedObjectID(statement.Schema, statement.ObjectName, string(statement.ObjectType))] = struct{}{}
			}
			semantic[string(statement.Category)] = struct{}{}
			if statement.IsDataOp {
				dataOperations++
			}
			if statement.Operation == "DROP" {
				riskFlags["destructive_operation"] = struct{}{}
				squashSafe = false
			}
			if statement.IsDynamic {
				riskFlags["dynamic_sql"] = struct{}{}
				squashSafe = false
			}
			if statement.Metadata.RequiresNoTransaction {
				riskFlags["requires_no_transaction"] = struct{}{}
			}
		}
		sequence := input.Sequence
		if sequence == 0 {
			sequence = index + 1
		}
		chain = append(chain, harnesscontract.ContextMigrationNode{
			MigrationID:        migrationID,
			Sequence:           sequence,
			Checksum:           hashValue(input.SQL),
			OperationTypes:     sortedKeys(operations),
			AffectedObjects:    sortedKeys(objects),
			SemanticOperations: sortedKeys(semantic),
			Reversible:         false,
			SquashSafe:         squashSafe,
			RiskFlags:          sortedKeys(riskFlags),
		})
		workspaceParts = append(workspaceParts, migrationID, input.SQL)
	}
	return chain, totalStatements, dataOperations, hashValue(strings.Join(workspaceParts, "\x00")), nil
}

func buildSchemaEvidence(sql, migrationID string) ([]harnesscontract.ContextSchemaObject, harnesscontract.ContextDependencyGraph, error) {
	parsed, err := internalparser.ParseMigrationWithContext(context.Background(), sql, migrationID)
	if err != nil {
		return nil, harnesscontract.ContextDependencyGraph{}, fmt.Errorf("parse generated schema evidence: %w", err)
	}
	objectKinds := map[string]string{}
	edgeSet := map[string]harnesscontract.ContextDependencyEdge{}
	for _, statement := range parsed.Statements {
		if statement.ObjectName == "" {
			continue
		}
		objectID := qualifiedObjectID(statement.Schema, statement.ObjectName, string(statement.ObjectType))
		objectKinds[objectID] = string(statement.ObjectType)
		for _, dependency := range statement.Dependencies {
			dependencyID := strings.TrimSpace(dependency)
			if dependencyID == "" {
				continue
			}
			key := objectID + "\x00" + dependencyID
			edgeSet[key] = harnesscontract.ContextDependencyEdge{From: objectID, To: dependencyID, Type: "statement_dependency"}
		}
	}
	objectIDs := make([]string, 0, len(objectKinds))
	for id := range objectKinds {
		objectIDs = append(objectIDs, id)
	}
	sort.Strings(objectIDs)
	objects := make([]harnesscontract.ContextSchemaObject, 0, len(objectIDs))
	nodes := make([]harnesscontract.ContextDependencyNode, 0, len(objectIDs))
	for _, id := range objectIDs {
		objects = append(objects, harnesscontract.ContextSchemaObject{ID: id, Kind: objectKinds[id]})
		nodes = append(nodes, harnesscontract.ContextDependencyNode{ID: id, Kind: objectKinds[id]})
	}
	edgeKeys := make([]string, 0, len(edgeSet))
	for key := range edgeSet {
		edgeKeys = append(edgeKeys, key)
	}
	sort.Strings(edgeKeys)
	edges := make([]harnesscontract.ContextDependencyEdge, 0, len(edgeKeys))
	for _, key := range edgeKeys {
		edges = append(edges, edgeSet[key])
	}
	return objects, harnesscontract.ContextDependencyGraph{Nodes: nodes, Edges: edges, Cycles: detectStringCycles(edges)}, nil
}

func qualifiedObjectID(schema, name, kind string) string {
	if strings.TrimSpace(schema) == "" {
		schema = "public"
	}
	return strings.ToLower(strings.TrimSpace(kind)) + ":" + schema + "." + name
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return result
}

func detectStringCycles(edges []harnesscontract.ContextDependencyEdge) [][]string {
	adjacency := map[string][]string{}
	for _, edge := range edges {
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
	}
	for node := range adjacency {
		sort.Strings(adjacency[node])
	}
	visited := map[string]bool{}
	onStack := map[string]int{}
	path := []string{}
	cycles := [][]string{}
	var visit func(string)
	visit = func(node string) {
		if position, ok := onStack[node]; ok {
			cycles = append(cycles, append([]string(nil), path[position:]...))
			return
		}
		if visited[node] {
			return
		}
		visited[node] = true
		onStack[node] = len(path)
		path = append(path, node)
		for _, dependency := range adjacency[node] {
			visit(dependency)
		}
		path = path[:len(path)-1]
		delete(onStack, node)
	}
	nodes := make([]string, 0, len(adjacency))
	for node := range adjacency {
		nodes = append(nodes, node)
	}
	sort.Strings(nodes)
	for _, node := range nodes {
		visit(node)
	}
	return cycles
}

func hashValue(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func resolveOutputDirectory(outputPath string) string {
	trimmed := strings.TrimSpace(outputPath)
	if trimmed == "" {
		return "."
	}
	return filepath.Dir(trimmed)
}

func classifyWarningRisk(warning string) string {
	message := strings.ToLower(strings.TrimSpace(warning))
	if message == "" {
		return "low"
	}
	if strings.Contains(message, "drop") || strings.Contains(message, "truncate") || strings.Contains(message, "data loss") {
		return "high"
	}
	if strings.Contains(message, "lock") || strings.Contains(message, "constraint") || strings.Contains(message, "dependency") {
		return "medium"
	}
	return "low"
}
