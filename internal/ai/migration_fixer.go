package ai

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/fatih/color"
)

// ValidationFunc is a function that validates migrations and returns an error if validation fails
type ValidationFunc func(ctx context.Context, migrationPath string) error

// MigrationFixer uses AI to automatically fix broken migrations
type MigrationFixer struct {
	provider       Provider
	maxAttempts    int
	verbose        bool
	validationFunc ValidationFunc
}

// FixAttempt represents a single fix attempt
type FixAttempt struct {
	Attempt     int
	Error       string
	FilePath    string
	FixApplied  string
	Description string
	Success     bool
}

// FixResult contains the overall result of the fixing process
type FixResult struct {
	Success       bool
	Attempts      []FixAttempt
	FinalError    string
	TotalFixes    int
	FilesModified []string
}

// NewMigrationFixer creates a new migration fixer
func NewMigrationFixer(provider Provider, maxAttempts int, verbose bool) *MigrationFixer {
	if maxAttempts <= 0 {
		maxAttempts = 5 // Default max attempts
	}
	return &MigrationFixer{
		provider:    provider,
		maxAttempts: maxAttempts,
		verbose:     verbose,
	}
}

// WithValidation sets a validation function for the fixer
func (mf *MigrationFixer) WithValidation(validationFunc ValidationFunc) *MigrationFixer {
	mf.validationFunc = validationFunc
	return mf
}

// FixMigrationsUntilValid attempts to fix migrations until validation passes
func (mf *MigrationFixer) FixMigrationsUntilValid(
	ctx context.Context,
	migrationPath string,
	validationError error,
) (*FixResult, error) {
	result := &FixResult{
		Attempts:      make([]FixAttempt, 0),
		FilesModified: make([]string, 0),
	}

	if mf.verbose {
		color.Cyan("🤖 AI Migration Fixer started\n")
		color.Cyan("   Max attempts: %d\n", mf.maxAttempts)
		color.Cyan("   Migration path: %s\n", migrationPath)
	}

	currentError := validationError

	for attempt := 1; attempt <= mf.maxAttempts; attempt++ {
		if mf.verbose {
			color.Yellow("\n📍 Attempt %d/%d\n", attempt, mf.maxAttempts)
		}

		// If no error, we're done
		if currentError == nil {
			result.Success = true
			if mf.verbose {
				color.Green("☑ All migrations fixed successfully!\n")
			}
			return result, nil
		}

		// Analyze error and generate fix
		fix, err := mf.analyzeAndFix(ctx, currentError, migrationPath, attempt)
		if err != nil {
			result.FinalError = err.Error()
			return result, errors.NewError(
				errors.ErrorCodeTransformationFailed,
				fmt.Sprintf("failed to generate fix on attempt %d", attempt),
				errors.SeverityError,
				errors.CategoryTransformation,
			).WithInnerError(err).WithSuggestion("review migration errors and fix manually if needed")
		}

		result.Attempts = append(result.Attempts, *fix)

		if !fix.Success {
			result.FinalError = fix.Description
			continue
		}

		result.TotalFixes++
		if !contains(result.FilesModified, fix.FilePath) {
			result.FilesModified = append(result.FilesModified, fix.FilePath)
		}

		if mf.verbose {
			color.Green("☑ Fix applied: %s\n", fix.Description)
		}

		// Re-run validation if validation function is provided
		if mf.validationFunc != nil {
			if mf.verbose {
				color.Cyan("🔍 Re-running validation...\n")
			}

			currentError = mf.validationFunc(ctx, migrationPath)

			if currentError == nil {
				if mf.verbose {
					color.Green("☑ Validation passed after fix!\n")
				}
				result.Success = true
				return result, nil
			}

			if mf.verbose {
				color.Yellow("⚠️  Validation still failing: %v\n", currentError)
				color.Cyan("   Attempting next fix...\n")
			}
		} else {
			// No validation function provided - break after one fix
			// This maintains backward compatibility
			if mf.verbose {
				color.Yellow("⚠️  No validation function provided - stopping after one fix\n")
			}
			break
		}
	}

	if result.TotalFixes > 0 {
		result.Success = true
	} else {
		result.FinalError = fmt.Sprintf("could not fix migrations after %d attempts", mf.maxAttempts)
	}

	return result, nil
}

// analyzeAndFix analyzes a validation error and generates a fix
func (mf *MigrationFixer) analyzeAndFix(
	ctx context.Context,
	validationError error,
	migrationPath string,
	attempt int,
) (*FixAttempt, error) {
	fixAttempt := &FixAttempt{
		Attempt: attempt,
		Error:   validationError.Error(),
	}

	// Parse the error to understand what went wrong
	errorAnalysis := mf.parseValidationError(validationError)

	// Read relevant migration files
	migrations, err := mf.readMigrationFiles(migrationPath)
	if err != nil {
		return fixAttempt, err
	}

	// Build AI prompt
	promptContent := mf.buildFixPrompt(errorAnalysis, migrations)

	// Call AI provider to get fix suggestion
	if mf.provider == nil {
		return fixAttempt, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"AI provider not initialized",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("check that AI provider API keys are configured")
	}

	// Use custom analysis type for migration fixing
	aiRequest := &AnalysisRequest{
		Type:        "migration_fix", // Custom type for migration fixing
		Content:     promptContent,
		Context:     fmt.Sprintf("Attempt %d of %d", attempt, mf.maxAttempts),
		MaxTokens:   2000,
		Temperature: 0.3, // Low temperature for consistent fixes
	}

	aiResponse, err := mf.provider.Analyze(ctx, aiRequest)
	if err != nil {
		return fixAttempt, errors.NewError(
			errors.ErrorCodeAnalysisError,
			"AI analysis failed",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("check AI provider connectivity and rate limits")
	}

	if mf.verbose {
		color.Cyan("🤖 AI suggestion (confidence: %.2f):\n%s\n", aiResponse.Confidence, aiResponse.Result)
	}

	// Parse AI response
	fix, err := mf.parseAIResponse(aiResponse.Result)
	if err != nil {
		return fixAttempt, errors.NewError(
			errors.ErrorCodeTransformationFailed,
			"failed to parse AI response",
			errors.SeverityError,
			errors.CategoryTransformation,
		).WithInnerError(err).WithSuggestion("AI response format may be invalid")
	}

	fixAttempt.FilePath = fix.FilePath
	fixAttempt.FixApplied = fix.SQL
	fixAttempt.Description = fix.Description

	// Check confidence threshold
	if aiResponse.Confidence < 0.75 {
		fixAttempt.Description = fmt.Sprintf("Low confidence fix (%.2f): %s", aiResponse.Confidence, fix.Description)
		if mf.verbose {
			color.Yellow("⚠️  AI confidence is low (%.2f), skipping automatic fix\n", aiResponse.Confidence)
		}
		return fixAttempt, nil // Don't apply low-confidence fixes
	}

	// Apply the fix
	if err := mf.applyFix(fix, migrationPath); err != nil {
		fixAttempt.Description = fmt.Sprintf("Failed to apply fix: %v", err)
		return fixAttempt, err
	}

	fixAttempt.Success = true
	return fixAttempt, nil
}

// ErrorAnalysis contains parsed error information
type ErrorAnalysis struct {
	ErrorType    string // "duplicate_trigger", "duplicate_function", "missing_table", etc.
	ObjectType   string // "trigger", "function", "table", etc.
	ObjectName   string
	TableName    string
	FilePath     string
	ErrorMessage string
}

// parseValidationError extracts structured information from validation error
func (mf *MigrationFixer) parseValidationError(err error) *ErrorAnalysis {
	analysis := &ErrorAnalysis{
		ErrorMessage: err.Error(),
	}

	errorStr := err.Error()

	// Parse "trigger X for relation Y already exists"
	triggerRegex := regexp.MustCompile(`trigger "(\w+)" for relation "(\w+)" already exists`)
	if matches := triggerRegex.FindStringSubmatch(errorStr); len(matches) == 3 {
		analysis.ErrorType = "duplicate_trigger"
		analysis.ObjectType = "trigger"
		analysis.ObjectName = matches[1]
		analysis.TableName = matches[2]
		return analysis
	}

	// Parse "function X already exists"
	functionRegex := regexp.MustCompile(`function "?(\w+)"? already exists`)
	if matches := functionRegex.FindStringSubmatch(errorStr); len(matches) == 2 {
		analysis.ErrorType = "duplicate_function"
		analysis.ObjectType = "function"
		analysis.ObjectName = matches[1]
		return analysis
	}

	// Parse "relation X is already member of publication Y"
	publicationRegex := regexp.MustCompile(`relation "(\w+)" is already member of publication "(\w+)"`)
	if matches := publicationRegex.FindStringSubmatch(errorStr); len(matches) == 3 {
		analysis.ErrorType = "duplicate_publication_member"
		analysis.ObjectType = "publication"
		analysis.TableName = matches[1]
		analysis.ObjectName = matches[2]
		return analysis
	}

	// Parse file path from error
	filePathRegex := regexp.MustCompile(`migrations/(\d+[^:]+\.sql)`)
	if matches := filePathRegex.FindStringSubmatch(errorStr); len(matches) == 2 {
		analysis.FilePath = matches[1]
	}

	// Generic unknown error
	analysis.ErrorType = "unknown"
	return analysis
}

// MigrationFile represents a migration file
type MigrationFile struct {
	Path    string
	Name    string
	Content string
}

// readMigrationFiles reads all migration files
func (mf *MigrationFixer) readMigrationFiles(migrationPath string) ([]MigrationFile, error) {
	var migrations []MigrationFile

	err := filepath.Walk(migrationPath, func(path string, info os.FileInfo, err error) error {
		if err != nil || !strings.HasSuffix(path, ".sql") || info.IsDir() {
			return err
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		migrations = append(migrations, MigrationFile{
			Path:    path,
			Name:    filepath.Base(path),
			Content: string(content),
		})

		return nil
	})

	return migrations, err
}

// buildFixPrompt creates the AI prompt for fixing
func (mf *MigrationFixer) buildFixPrompt(analysis *ErrorAnalysis, migrations []MigrationFile) string {
	var prompt strings.Builder

	prompt.WriteString("You are a PostgreSQL migration fixer. Analyze the error and provide a fix.\n\n")
	prompt.WriteString("ERROR ANALYSIS:\n")
	prompt.WriteString(fmt.Sprintf("- Error Type: %s\n", analysis.ErrorType))
	prompt.WriteString(fmt.Sprintf("- Object Type: %s\n", analysis.ObjectType))
	prompt.WriteString(fmt.Sprintf("- Object Name: %s\n", analysis.ObjectName))
	if analysis.TableName != "" {
		prompt.WriteString(fmt.Sprintf("- Table Name: %s\n", analysis.TableName))
	}
	prompt.WriteString(fmt.Sprintf("- Error Message: %s\n\n", analysis.ErrorMessage))

	prompt.WriteString("MIGRATION FILES:\n")
	for i, mig := range migrations {
		prompt.WriteString(fmt.Sprintf("\n--- File %d: %s ---\n", i+1, mig.Name))
		// Only include first 1000 chars to avoid token limits
		content := mig.Content
		if len(content) > 1000 {
			content = content[:1000] + "\n... (truncated)"
		}
		prompt.WriteString(content)
		prompt.WriteString("\n")
	}

	prompt.WriteString("\nPROVIDE FIX:\n")
	prompt.WriteString("Based on the error, identify which migration file needs to be fixed.\n")
	prompt.WriteString("Provide the fix in this exact format:\n")
	prompt.WriteString("FILE: <filename>\n")
	prompt.WriteString("DESCRIPTION: <what you're fixing>\n")
	prompt.WriteString("FIX_SQL:\n")
	prompt.WriteString("<the SQL to add/modify>\n")
	prompt.WriteString("END_FIX\n\n")

	prompt.WriteString("Common fixes:\n")
	prompt.WriteString("- For duplicate triggers: Add 'DROP TRIGGER IF EXISTS trigger_name ON table_name;' before CREATE TRIGGER\n")
	prompt.WriteString("- For duplicate functions: Change 'CREATE FUNCTION' to 'CREATE OR REPLACE FUNCTION'\n")
	prompt.WriteString("- For duplicate publication members: Remove duplicate 'ALTER PUBLICATION ... ADD TABLE ...'\n")

	return prompt.String()
}

// AIFix represents a fix suggested by AI
type AIFix struct {
	FilePath    string
	Description string
	SQL         string
}

// parseAIResponse parses the AI response into a structured fix
func (mf *MigrationFixer) parseAIResponse(response string) (*AIFix, error) {
	fix := &AIFix{}

	// Extract FILE
	fileRegex := regexp.MustCompile(`FILE:\s*(.+)`)
	if matches := fileRegex.FindStringSubmatch(response); len(matches) == 2 {
		fix.FilePath = strings.TrimSpace(matches[1])
	} else {
		return nil, errors.NewError(
			errors.ErrorCodeTransformationFailed,
			"could not parse FILE from AI response",
			errors.SeverityError,
			errors.CategoryTransformation,
		).WithSuggestion("AI response should contain 'FILE: <filename>'")
	}

	// Extract DESCRIPTION
	descRegex := regexp.MustCompile(`DESCRIPTION:\s*(.+)`)
	if matches := descRegex.FindStringSubmatch(response); len(matches) == 2 {
		fix.Description = strings.TrimSpace(matches[1])
	}

	// Extract FIX_SQL
	sqlRegex := regexp.MustCompile(`(?s)FIX_SQL:\s*\n(.*?)\nEND_FIX`)
	if matches := sqlRegex.FindStringSubmatch(response); len(matches) == 2 {
		fix.SQL = strings.TrimSpace(matches[1])
	} else {
		return nil, errors.NewError(
			errors.ErrorCodeTransformationFailed,
			"could not parse FIX_SQL from AI response",
			errors.SeverityError,
			errors.CategoryTransformation,
		).WithSuggestion("AI response should contain 'FIX_SQL:...END_FIX' block")
	}

	return fix, nil
}

// applyFix applies the suggested fix to the migration file
func (mf *MigrationFixer) applyFix(fix *AIFix, migrationPath string) error {
	// Construct full path
	fullPath := filepath.Join(migrationPath, fix.FilePath)

	// Read current content
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeTransformationFailed,
			"failed to read file",
			errors.SeverityError,
			errors.CategoryTransformation,
		).WithInnerError(err).WithFile(fullPath)
	}

	// Apply fix (for now, just prepend the fix SQL)
	// A more sophisticated approach would parse and insert at the right location
	newContent := fix.SQL + "\n\n" + string(content)

	// Backup original
	backupPath := fullPath + ".backup"
	if err := os.WriteFile(backupPath, content, 0644); err != nil {
		return errors.NewError(
			errors.ErrorCodeBackupGenerationFailed,
			"failed to create backup",
			errors.SeverityError,
			errors.CategoryBackup,
		).WithInnerError(err).WithFile(backupPath)
	}

	// Write fixed content
	if err := os.WriteFile(fullPath, []byte(newContent), 0644); err != nil {
		return errors.NewError(
			errors.ErrorCodeTransformationFailed,
			"failed to write fix",
			errors.SeverityError,
			errors.CategoryTransformation,
		).WithInnerError(err).WithFile(fullPath)
	}

	if mf.verbose {
		color.Green("☑ Applied fix to %s (backup created at %s)\n", fullPath, backupPath)
	}

	return nil
}

// Helper function
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
