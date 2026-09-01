package utils

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
)

// WarningManager handles deduplication and categorization of warnings
type WarningManager struct {
	warnings   []*errors.StructuredError
	seenHashes map[string]bool
	dedupMap   map[string]*errors.StructuredError // Maps hash to first occurrence
}

// NewWarningManager creates a new warning manager
func NewWarningManager() *WarningManager {
	return &WarningManager{
		warnings:   make([]*errors.StructuredError, 0),
		seenHashes: make(map[string]bool),
		dedupMap:   make(map[string]*errors.StructuredError),
	}
}

// AddWarning adds a warning with deduplication
// Accepts errors.StructuredError
func (wm *WarningManager) AddWarning(warning *errors.StructuredError) {
	hash := wm.hashWarning(warning)

	if !wm.seenHashes[hash] {
		wm.seenHashes[hash] = true
		wm.dedupMap[hash] = warning
		wm.warnings = append(wm.warnings, warning)
	}
}

// AddRawWarning adds a raw warning string and categorizes it automatically
func (wm *WarningManager) AddRawWarning(message string) {
	warning := wm.categorizeRawWarning(message)
	wm.AddWarning(warning)
}

// AddRawWarnings adds multiple raw warning strings
func (wm *WarningManager) AddRawWarnings(messages []string) {
	for _, msg := range messages {
		wm.AddRawWarning(msg)
	}
}

// GetWarnings returns all warnings sorted by severity and category
func (wm *WarningManager) GetWarnings() []*errors.StructuredError {
	// Sort warnings by severity (highest first), then category
	sorted := make([]*errors.StructuredError, len(wm.warnings))
	copy(sorted, wm.warnings)

	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Severity != sorted[j].Severity {
			return sorted[i].Severity > sorted[j].Severity
		}
		return sorted[i].Category < sorted[j].Category
	})

	return sorted
}

// GetWarningsByCategory returns warnings grouped by category
func (wm *WarningManager) GetWarningsByCategory() map[errors.Category][]*errors.StructuredError {
	byCategory := make(map[errors.Category][]*errors.StructuredError)

	for _, warning := range wm.warnings {
		byCategory[warning.Category] = append(byCategory[warning.Category], warning)
	}

	return byCategory
}

// GetWarningsBySeverity returns warnings grouped by severity
func (wm *WarningManager) GetWarningsBySeverity() map[errors.Severity][]*errors.StructuredError {
	bySeverity := make(map[errors.Severity][]*errors.StructuredError)

	for _, warning := range wm.warnings {
		bySeverity[warning.Severity] = append(bySeverity[warning.Severity], warning)
	}

	return bySeverity
}

// Count returns the total number of unique warnings
func (wm *WarningManager) Count() int {
	return len(wm.warnings)
}

// CountBySeverity returns counts grouped by severity
func (wm *WarningManager) CountBySeverity() map[errors.Severity]int {
	counts := make(map[errors.Severity]int)

	for _, warning := range wm.warnings {
		counts[warning.Severity]++
	}

	return counts
}

// HasCriticalWarnings returns true if any critical warnings exist
func (wm *WarningManager) HasCriticalWarnings() bool {
	for _, warning := range wm.warnings {
		if warning.Severity == errors.SeverityCritical {
			return true
		}
	}
	return false
}

// hashWarning creates a unique hash for a warning based on normalized content
func (wm *WarningManager) hashWarning(warning *errors.StructuredError) string {
	// Normalize message: lowercase, trim, remove extra spaces
	normalized := strings.ToLower(strings.TrimSpace(warning.Message))
	normalized = strings.Join(strings.Fields(normalized), " ")

	// Include category and object name in hash to avoid over-deduplication
	objectName := ""
	if warning.Context != nil && warning.Context.ObjectName != "" {
		objectName = warning.Context.ObjectName
	}

	hashInput := fmt.Sprintf("%s|%s|%s", normalized, warning.Category, objectName)

	hash := sha256.Sum256([]byte(hashInput))
	return fmt.Sprintf("%x", hash[:8]) // Use first 8 bytes for shorter hash
}

// categorizeRawWarning analyzes a raw warning message and assigns category/severity
func (wm *WarningManager) categorizeRawWarning(message string) *errors.StructuredError {
	msgLower := strings.ToLower(message)

	// Default category and severity
	category := errors.CategoryGeneral
	severity := errors.SeverityWarning // Default to Warning instead of Low

	// Categorize based on keywords
	switch {
	case strings.Contains(msgLower, "backup"):
		category = errors.CategoryBackup
		severity = errors.SeverityInfo
	case strings.Contains(msgLower, "rollback"):
		category = errors.CategoryRollback
		severity = errors.SeverityInfo
	case strings.Contains(msgLower, "cycle") || strings.Contains(msgLower, "circular"):
		category = errors.CategoryCycle
		severity = errors.SeverityError
		if strings.Contains(msgLower, "critical") {
			severity = errors.SeverityCritical
		}
	case strings.Contains(msgLower, "depends on") || strings.Contains(msgLower, "dependency"):
		category = errors.CategoryDependency
		severity = errors.SeverityWarning
		if strings.Contains(msgLower, "never created") || strings.Contains(msgLower, "missing") {
			severity = errors.SeverityError
		}
	case strings.Contains(msgLower, "optimization") || strings.Contains(msgLower, "consolidated"):
		category = errors.CategoryOptimization
		severity = errors.SeverityInfo
	case strings.Contains(msgLower, "transformation") || strings.Contains(msgLower, "modernization"):
		category = errors.CategoryTransformation
		severity = errors.SeverityInfo
	case strings.Contains(msgLower, "validation") || strings.Contains(msgLower, "schema diff"):
		category = errors.CategoryValidation
		severity = errors.SeverityWarning
	case strings.Contains(msgLower, "risk") || strings.Contains(msgLower, "unsafe"):
		category = errors.CategoryRisk
		severity = errors.SeverityError
	}

	// Create the structured error
	warning := errors.NewWarning(category, message)
	warning.Severity = severity
	warning.CanContinue = true

	// Extract object name if present (simplified heuristic)
	var objectName string
	if strings.Contains(msgLower, "object ") {
		parts := strings.Split(message, "Object ")
		if len(parts) > 1 {
			objParts := strings.Fields(parts[1])
			if len(objParts) > 0 {
				objectName = objParts[0]
			}
		}
	} else if strings.Contains(msgLower, "table ") {
		parts := strings.Split(message, "table ")
		if len(parts) > 1 {
			objParts := strings.Fields(parts[1])
			if len(objParts) > 0 {
				objectName = objParts[0]
			}
		}
	} else if strings.Contains(msgLower, "function ") {
		parts := strings.Split(message, "function ")
		if len(parts) > 1 {
			objParts := strings.Fields(parts[1])
			if len(objParts) > 0 {
				objectName = objParts[0]
			}
		}
	}

	if objectName != "" {
		warning = warning.WithObject(objectName, "") // Set object name
	}

	return warning
}

// FormatWarnings returns a formatted string representation of all warnings
func (wm *WarningManager) FormatWarnings() string {
	if len(wm.warnings) == 0 {
		return ""
	}

	var output strings.Builder
	bySeverity := wm.GetWarningsBySeverity()

	// Critical warnings
	if critical := bySeverity[errors.SeverityCritical]; len(critical) > 0 {
		output.WriteString(fmt.Sprintf("\n🔴 Critical (%d):\n", len(critical)))
		for _, w := range critical {
			output.WriteString(fmt.Sprintf("  ► %s\n", w.Message))
			if w.Suggestion != "" {
				output.WriteString(fmt.Sprintf("    → %s\n", w.Suggestion))
			}
		}
	}

	// Error severity warnings (maps to High in old system)
	if errSev := bySeverity[errors.SeverityError]; len(errSev) > 0 {
		output.WriteString(fmt.Sprintf("\n🟠 Error (%d):\n", len(errSev)))
		for _, w := range errSev {
			output.WriteString(fmt.Sprintf("  ► %s\n", w.Message))
			if w.Suggestion != "" {
				output.WriteString(fmt.Sprintf("    → %s\n", w.Suggestion))
			}
		}
	}

	// Warning severity (maps to Medium/Low in old system)
	if warn := bySeverity[errors.SeverityWarning]; len(warn) > 0 {
		output.WriteString(fmt.Sprintf("\n🟡 Warning (%d):\n", len(warn)))
		for _, w := range warn {
			output.WriteString(fmt.Sprintf("  ► %s\n", w.Message))
		}
	}

	// Info severity
	if info := bySeverity[errors.SeverityInfo]; len(info) > 0 {
		output.WriteString(fmt.Sprintf("\nℹ️  Info (%d):\n", len(info)))
		for _, w := range info {
			output.WriteString(fmt.Sprintf("  ► %s\n", w.Message))
		}
	}

	return output.String()
}
