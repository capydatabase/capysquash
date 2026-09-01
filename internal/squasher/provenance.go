// Package squasher provides provenance tracking for migration squashing operations.
// It generates .squashmap.json files that map original migrations to squashed output,
// enabling traceability and auditability.
package squasher

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// SquashMap represents the complete mapping of a squash operation
type SquashMap struct {
	// Metadata
	Version     string    `json:"version"`      // pgsquash version
	Timestamp   time.Time `json:"timestamp"`    // When the squash was performed
	SafetyMode  string    `json:"safety_mode"`  // Safety level used (paranoid, conservative, standard, aggressive)
	PGVersion   string    `json:"pg_version"`   // PostgreSQL version targeted
	Extensions  []string  `json:"extensions"`   // Extensions detected/required
	ContentHash string    `json:"content_hash"` // SHA256 hash of squashed output

	// Input/Output
	Inputs  []string `json:"inputs"`  // List of original migration files
	Outputs []string `json:"outputs"` // List of generated output files

	// Detailed Mappings
	Mappings []StatementMapping `json:"mappings"` // Statement-level mappings

	// Statistics
	Stats SquashStatistics `json:"statistics"` // Consolidation statistics

	// Warnings
	Warnings []string `json:"warnings,omitempty"` // Any warnings during squash
}

// StatementMapping maps an original statement to its location in the output
type StatementMapping struct {
	SourceFile      string `json:"source_file"`      // Original migration file
	SourceLine      int    `json:"source_line"`      // Line number in source file
	SourceStatement string `json:"source_statement"` // The original SQL statement (first 100 chars)

	OutputFile      string `json:"output_file"`       // Output file name
	OutputLineStart int    `json:"output_line_start"` // Starting line in output
	OutputLineEnd   int    `json:"output_line_end"`   // Ending line in output

	Action     string `json:"action"`      // What happened: "preserved", "merged", "eliminated", "reordered"
	ObjectType string `json:"object_type"` // Type of object (table, index, function, etc.)
	ObjectName string `json:"object_name"` // Name of the object
}

// SquashStatistics contains metrics about the squash operation
type SquashStatistics struct {
	OriginalStatements     int     `json:"original_statements"`     // Total statements in original migrations
	ConsolidatedStatements int     `json:"consolidated_statements"` // Total statements in output
	ReductionRate          float64 `json:"reduction_rate"`          // Percentage reduction
	FilesProcessed         int     `json:"files_processed"`         // Number of input files
	FilesGenerated         int     `json:"files_generated"`         // Number of output files
	TotalLinesOriginal     int     `json:"total_lines_original"`    // Total lines in original
	TotalLinesSquashed     int     `json:"total_lines_squashed"`    // Total lines in output
}

// ProvenanceTracker tracks provenance information during squashing
type ProvenanceTracker struct {
	squashMap   *SquashMap
	currentFile string
	currentLine int
	outputLine  int
	outputFile  string
}

// NewProvenanceTracker creates a new provenance tracker
func NewProvenanceTracker(version, safetyMode, pgVersion string, extensions []string) *ProvenanceTracker {
	return &ProvenanceTracker{
		squashMap: &SquashMap{
			Version:    version,
			Timestamp:  time.Now(),
			SafetyMode: safetyMode,
			PGVersion:  pgVersion,
			Extensions: extensions,
			Inputs:     make([]string, 0),
			Outputs:    make([]string, 0),
			Mappings:   make([]StatementMapping, 0),
			Warnings:   make([]string, 0),
		},
	}
}

// AddInputFile registers an input migration file
func (pt *ProvenanceTracker) AddInputFile(filePath string) {
	pt.squashMap.Inputs = append(pt.squashMap.Inputs, filePath)
}

// AddOutputFile registers an output file
func (pt *ProvenanceTracker) AddOutputFile(filePath string) {
	pt.squashMap.Outputs = append(pt.squashMap.Outputs, filePath)
}

// SetCurrentSource sets the current source file being processed
func (pt *ProvenanceTracker) SetCurrentSource(filePath string, line int) {
	pt.currentFile = filePath
	pt.currentLine = line
}

// SetCurrentOutput sets the current output location
func (pt *ProvenanceTracker) SetCurrentOutput(filePath string, line int) {
	pt.outputFile = filePath
	pt.outputLine = line
}

// RecordMapping records a statement mapping
func (pt *ProvenanceTracker) RecordMapping(mapping StatementMapping) {
	// Fill in current context if not specified
	if mapping.SourceFile == "" {
		mapping.SourceFile = pt.currentFile
	}
	if mapping.SourceLine == 0 {
		mapping.SourceLine = pt.currentLine
	}
	if mapping.OutputFile == "" {
		mapping.OutputFile = pt.outputFile
	}
	if mapping.OutputLineStart == 0 {
		mapping.OutputLineStart = pt.outputLine
	}

	pt.squashMap.Mappings = append(pt.squashMap.Mappings, mapping)
}

// AddWarning adds a warning to the provenance map
func (pt *ProvenanceTracker) AddWarning(warning string) {
	pt.squashMap.Warnings = append(pt.squashMap.Warnings, warning)
}

// SetStatistics sets the consolidation statistics
func (pt *ProvenanceTracker) SetStatistics(stats SquashStatistics) {
	pt.squashMap.Stats = stats

	// Calculate reduction rate
	if stats.OriginalStatements > 0 {
		reduction := float64(stats.OriginalStatements-stats.ConsolidatedStatements) / float64(stats.OriginalStatements) * 100
		pt.squashMap.Stats.ReductionRate = reduction
	}
}

// ComputeContentHash computes the SHA256 hash of the squashed output
func (pt *ProvenanceTracker) ComputeContentHash(content string) {
	hash := sha256.Sum256([]byte(content))
	pt.squashMap.ContentHash = "sha256:" + hex.EncodeToString(hash[:])
}

// WriteSquashMap writes the .squashmap.json file to the specified directory
func (pt *ProvenanceTracker) WriteSquashMap(outputDir string) error {
	squashmapPath := filepath.Join(outputDir, ".squashmap.json")

	// Marshal to pretty JSON
	data, err := json.MarshalIndent(pt.squashMap, "", "  ")
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			"failed to marshal squashmap to JSON",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err)
	}

	// Write to file
	if err := os.WriteFile(squashmapPath, data, 0644); err != nil {
		return errors.NewError(
			errors.ErrorCodeSQLGenerationFailed,
			"failed to write squashmap file",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithFile(squashmapPath).
			WithInnerError(err).
			WithSuggestion("Check directory permissions and disk space")
	}

	return nil
}

// GetSquashMap returns the current squash map
func (pt *ProvenanceTracker) GetSquashMap() *SquashMap {
	return pt.squashMap
}

// LoadSquashMap loads a .squashmap.json file from disk
func LoadSquashMap(filePath string) (*SquashMap, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to read squashmap file",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithFile(filePath).
			WithInnerError(err).
			WithSuggestion("Ensure the squashmap file exists and is readable")
	}

	var squashMap SquashMap
	if err := json.Unmarshal(data, &squashMap); err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to parse squashmap JSON",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithFile(filePath).
			WithInnerError(err).
			WithSuggestion("Squashmap file may be corrupted")
	}

	return &squashMap, nil
}

// FormatSquashMap formats the squash map for human-readable display
func (sm *SquashMap) FormatSquashMap() string {
	var output strings.Builder

	output.WriteString("=== Squash Map ===\n\n")
	output.WriteString(fmt.Sprintf("Version:      %s\n", sm.Version))
	output.WriteString(fmt.Sprintf("Timestamp:    %s\n", sm.Timestamp.Format(time.RFC3339)))
	output.WriteString(fmt.Sprintf("Safety Mode:  %s\n", sm.SafetyMode))
	output.WriteString(fmt.Sprintf("PG Version:   %s\n", sm.PGVersion))
	output.WriteString(fmt.Sprintf("Extensions:   %v\n", sm.Extensions))
	output.WriteString(fmt.Sprintf("Content Hash: %s\n\n", sm.ContentHash))

	output.WriteString("=== Input/Output ===\n\n")
	output.WriteString(fmt.Sprintf("Input Files:  %d\n", len(sm.Inputs)))
	for _, input := range sm.Inputs {
		output.WriteString(fmt.Sprintf("  - %s\n", input))
	}

	output.WriteString(fmt.Sprintf("\nOutput Files: %d\n", len(sm.Outputs)))
	for _, out := range sm.Outputs {
		output.WriteString(fmt.Sprintf("  - %s\n", out))
	}

	output.WriteString("\n=== Statistics ===\n\n")
	output.WriteString(fmt.Sprintf("Original Statements:     %d\n", sm.Stats.OriginalStatements))
	output.WriteString(fmt.Sprintf("Consolidated Statements: %d\n", sm.Stats.ConsolidatedStatements))
	output.WriteString(fmt.Sprintf("Reduction Rate:          %.2f%%\n", sm.Stats.ReductionRate))
	output.WriteString(fmt.Sprintf("Files Processed:         %d\n", sm.Stats.FilesProcessed))
	output.WriteString(fmt.Sprintf("Files Generated:         %d\n", sm.Stats.FilesGenerated))

	if len(sm.Warnings) > 0 {
		output.WriteString("\n=== Warnings ===\n\n")
		for i, warning := range sm.Warnings {
			output.WriteString(fmt.Sprintf("%d. %s\n", i+1, warning))
		}
	}

	return output.String()
}

// VerifyContentHash verifies the content hash matches the squashed output
func (sm *SquashMap) VerifyContentHash(content string) bool {
	hash := sha256.Sum256([]byte(content))
	expectedHash := "sha256:" + hex.EncodeToString(hash[:])
	return expectedHash == sm.ContentHash
}

// FindMappingForSource finds all mappings from a specific source file
func (sm *SquashMap) FindMappingForSource(sourceFile string) []StatementMapping {
	var mappings []StatementMapping
	for _, mapping := range sm.Mappings {
		if mapping.SourceFile == sourceFile {
			mappings = append(mappings, mapping)
		}
	}
	return mappings
}

// FindMappingForOutput finds all mappings to a specific output location
func (sm *SquashMap) FindMappingForOutput(outputFile string, line int) []StatementMapping {
	var mappings []StatementMapping
	for _, mapping := range sm.Mappings {
		if mapping.OutputFile == outputFile &&
			line >= mapping.OutputLineStart &&
			line <= mapping.OutputLineEnd {
			mappings = append(mappings, mapping)
		}
	}
	return mappings
}
