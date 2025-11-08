package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/config"
	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/capysquash/pgsquash-engine/internal/parser"
	"github.com/capysquash/pgsquash-engine/internal/squasher"
	"github.com/capysquash/pgsquash-engine/internal/tracking"
	"github.com/capysquash/pgsquash-engine/internal/types"
)

// WebhookHandler handles GitHub webhook events
type WebhookHandler struct {
	secret         string
	githubClient   *Client    // For personal access token auth
	appClient      *AppClient // For GitHub App auth (multi-repo support)
	analysisEngine *squasher.Engine
}

// NewWebhookHandler creates a new webhook handler with personal access token
func NewWebhookHandler(secret string, client *Client, engine *squasher.Engine) *WebhookHandler {
	return &WebhookHandler{
		secret:         secret,
		githubClient:   client,
		analysisEngine: engine,
	}
}

// NewWebhookHandlerWithApp creates a new webhook handler with GitHub App authentication
func NewWebhookHandlerWithApp(secret string, appClient *AppClient, engine *squasher.Engine) *WebhookHandler {
	return &WebhookHandler{
		secret:         secret,
		appClient:      appClient,
		analysisEngine: engine,
	}
}

// PullRequestEvent represents a GitHub pull request webhook event
type PullRequestEvent struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Number  int    `json:"number"`
		Head    Branch `json:"head"`
		Base    Branch `json:"base"`
		HTMLURL string `json:"html_url"`
	} `json:"pull_request"`
	Repository Repository `json:"repository"`
}

// IssueCommentEvent represents a GitHub issue comment webhook event
type IssueCommentEvent struct {
	Action  string `json:"action"`
	Issue   Issue  `json:"issue"`
	Comment struct {
		Body string `json:"body"`
	} `json:"comment"`
	Repository Repository `json:"repository"`
}

// Repository represents a GitHub repository
type Repository struct {
	FullName string `json:"full_name"`
	Owner    struct {
		Login string `json:"login"`
	} `json:"owner"`
	Name string `json:"name"`
}

// Issue represents a GitHub issue/PR
type Issue struct {
	Number      int       `json:"number"`
	PullRequest *struct{} `json:"pull_request,omitempty"`
}

// HandleWebhook processes incoming GitHub webhooks
func (h *WebhookHandler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	// Verify webhook signature
	if !h.verifySignature(r) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
	}

	// Parse event type
	eventType := r.Header.Get("X-GitHub-Event")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	switch eventType {
	case "pull_request":
		if err := h.handlePullRequest(r.Context(), body); err != nil {
			http.Error(w, fmt.Sprintf("Failed to handle pull request: %v", err), http.StatusInternalServerError)
			return
		}
	case "issue_comment":
		if err := h.handleIssueComment(r.Context(), body); err != nil {
			http.Error(w, fmt.Sprintf("Failed to handle issue comment: %v", err), http.StatusInternalServerError)
			return
		}
	default:
		w.WriteHeader(http.StatusOK)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// handlePullRequest processes pull request events
func (h *WebhookHandler) handlePullRequest(ctx context.Context, body []byte) error {
	var event PullRequestEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to parse pull request event",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Ensure webhook payload is valid JSON")
	}

	// Only process opened and synchronized events
	if event.Action != "opened" && event.Action != "synchronize" {
		return nil
	}

	// Load repository-specific .capysquash.yml configuration
	capyConfig, err := h.loadCapySquashConfig(ctx, event.Repository.FullName, event.PullRequest.Head.SHA)
	if err != nil {
		// Log error but continue with defaults
		capyConfig = config.DefaultCapySquashConfig()
	}

	// Check if analysis is enabled for this repository
	if !capyConfig.Enabled {
		return nil
	}

	// Get changed files
	files, err := h.githubClient.GetPRFiles(ctx, event.Repository.FullName, event.PullRequest.Number)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to retrieve pull request files",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("pr_number", event.PullRequest.Number).WithAdditional("repo", event.Repository.FullName)
	}

	// Filter for migration files using .capysquash.yml patterns
	migrationFiles := h.filterMigrationFilesWithConfig(files, capyConfig)
	if len(migrationFiles) == 0 {
		return nil // No migration files changed
	}

	// Check if we should analyze based on .capysquash.yml settings
	filePaths := make([]string, len(migrationFiles))
	for i, f := range migrationFiles {
		filePaths[i] = f.Filename
	}
	if !capyConfig.ShouldAnalyze(filePaths) {
		return nil
	}

	// Analyze migrations
	analysis, err := h.analyzeMigrations(ctx, event.Repository.FullName, migrationFiles, capyConfig)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeAnalysisError,
			"failed to analyze migrations",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("file_count", len(migrationFiles))
	}

	// Post analysis as PR comment (if enabled)
	if capyConfig.PRComment.Enabled {
		comment := h.formatAnalysisCommentPlatformStyle(analysis, len(migrationFiles), capyConfig)
		if err := h.githubClient.PostPRComment(ctx, event.Repository.FullName, event.PullRequest.Number, comment); err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to post analysis comment",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithAdditional("pr_number", event.PullRequest.Number)
		}
	}

	// Evaluate pass/fail thresholds and create check run
	conclusion := h.evaluateCheckConclusion(analysis, capyConfig)
	if err := h.createCheckRun(ctx, event, analysis, conclusion); err != nil {
		// Log error but don't fail the webhook - check runs are optional
		_ = err
	}

	// Check if auto-consolidation is enabled
	if capyConfig.ShouldAutoApply(event.PullRequest.Head.Ref) && analysis.ShouldConsolidate {
		// Create consolidation PR
		if err := h.createConsolidationPR(ctx, event, analysis); err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to create consolidation pull request",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}
	}

	return nil
}

// handleIssueComment processes comment events for bot commands
func (h *WebhookHandler) handleIssueComment(ctx context.Context, body []byte) error {
	var event IssueCommentEvent
	if err := json.Unmarshal(body, &event); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to parse issue comment event",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Ensure webhook payload is valid JSON")
	}

	// Only process comments on PRs
	if event.Issue.PullRequest == nil {
		return nil
	}

	// Check for bot commands
	command := h.parseCommand(event.Comment.Body)
	if command == "" {
		return nil
	}

	switch command {
	case "analyze":
		return h.handleAnalyzeCommand(ctx, event)
	case "consolidate":
		return h.handleConsolidateCommand(ctx, event)
	default:
		return nil
	}
}

// parseCommand extracts bot commands from comment body
func (h *WebhookHandler) parseCommand(body string) string {
	lines := strings.Split(body, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/pgsquash ") {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				return parts[1]
			}
		}
	}
	return ""
}

// handleAnalyzeCommand handles /pgsquash analyze command
func (h *WebhookHandler) handleAnalyzeCommand(ctx context.Context, event IssueCommentEvent) error {
	// Get PR files
	files, err := h.githubClient.GetPRFiles(ctx, event.Repository.FullName, event.Issue.Number)
	if err != nil {
		return err
	}

	// Load config for command-triggered analysis
	capyConfig := config.DefaultCapySquashConfig()

	migrationFiles := h.filterMigrationFiles(files)
	if len(migrationFiles) == 0 {
		if err := h.githubClient.PostPRComment(ctx, event.Repository.FullName, event.Issue.Number,
			"No migration files found in this PR."); err != nil {
			return errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to post PR comment",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithAdditional("issue_number", event.Issue.Number)
		}
		return nil
	}

	analysis, err := h.analyzeMigrations(ctx, event.Repository.FullName, migrationFiles, capyConfig)
	if err != nil {
		return err
	}

	comment := h.formatAnalysisComment(analysis, len(migrationFiles))
	return h.githubClient.PostPRComment(ctx, event.Repository.FullName, event.Issue.Number, comment)
}

// handleConsolidateCommand handles /pgsquash consolidate command
func (h *WebhookHandler) handleConsolidateCommand(ctx context.Context, event IssueCommentEvent) error {
	// Get PR details
	pr, err := h.githubClient.GetPullRequest(ctx, event.Repository.FullName, event.Issue.Number)
	if err != nil {
		return err
	}

	files, err := h.githubClient.GetPRFiles(ctx, event.Repository.FullName, event.Issue.Number)
	if err != nil {
		return err
	}

	// Load config for command-triggered consolidation
	capyConfig := config.DefaultCapySquashConfig()

	migrationFiles := h.filterMigrationFiles(files)
	analysis, err := h.analyzeMigrations(ctx, event.Repository.FullName, migrationFiles, capyConfig)
	if err != nil {
		return err
	}

	// Create consolidation PR
	return h.createConsolidationPR(ctx, PullRequestEvent{
		Repository: event.Repository,
		PullRequest: struct {
			Number  int    `json:"number"`
			Head    Branch `json:"head"`
			Base    Branch `json:"base"`
			HTMLURL string `json:"html_url"`
		}{
			Number: pr.Number,
			Head:   pr.Head,
			Base:   pr.Base,
		},
	}, analysis)
}

// filterMigrationFiles filters for SQL migration files (legacy, uses basic logic)
func (h *WebhookHandler) filterMigrationFiles(files []PRFile) []PRFile {
	migrations := []PRFile{}
	for _, file := range files {
		if strings.HasSuffix(strings.ToLower(file.Filename), ".sql") {
			migrations = append(migrations, file)
		}
	}
	return migrations
}

// filterMigrationFilesWithConfig filters migration files based on .capysquash.yml patterns
func (h *WebhookHandler) filterMigrationFilesWithConfig(files []PRFile, capyConfig *config.CapySquashConfig) []PRFile {
	migrations := []PRFile{}

	// Standard migration paths (platform defaults)
	standardPaths := []string{
		"migrations/",
		"db/migrations/",
		"db/migrate/",
		"supabase/migrations/",
		"prisma/migrations/",
	}

	for _, file := range files {
		// Must be a SQL file
		if !strings.HasSuffix(strings.ToLower(file.Filename), ".sql") {
			continue
		}

		// Check if file matches include patterns
		included := false

		// Check against .capysquash.yml include patterns
		for _, pattern := range capyConfig.Include {
			if matchPattern(file.Filename, pattern) {
				included = true
				break
			}
		}

		// Also check standard paths if no explicit include patterns matched
		if !included && len(capyConfig.Include) == 0 {
			for _, path := range standardPaths {
				if strings.HasPrefix(file.Filename, path) {
					included = true
					break
				}
			}
		}

		if !included {
			continue
		}

		// Check if file matches exclude patterns
		excluded := false
		for _, pattern := range capyConfig.Exclude {
			if matchPattern(file.Filename, pattern) {
				excluded = true
				break
			}
		}

		if included && !excluded {
			migrations = append(migrations, file)
		}
	}

	return migrations
}

// matchPattern matches a file path against a glob pattern
func matchPattern(path, pattern string) bool {
	// Support ** for recursive matching
	if strings.Contains(pattern, "**") {
		// Convert ** pattern to simple prefix/suffix matching
		parts := strings.Split(pattern, "**")
		if len(parts) == 2 {
			prefix := strings.TrimSuffix(parts[0], "/")
			suffix := strings.TrimPrefix(parts[1], "/")

			if prefix != "" && !strings.HasPrefix(path, prefix) {
				return false
			}
			if suffix != "" && !strings.HasSuffix(path, suffix) {
				return false
			}
			return true
		}
	}

	// Use filepath.Match for simple patterns
	matched, _ := filepath.Match(pattern, path)
	if matched {
		return true
	}

	// Also try matching against the basename
	matched, _ = filepath.Match(pattern, filepath.Base(path))
	return matched
}

// loadCapySquashConfig loads .capysquash.yml from repository
func (h *WebhookHandler) loadCapySquashConfig(ctx context.Context, repo, sha string) (*config.CapySquashConfig, error) {
	// Try to load .capysquash.yml from various locations
	possiblePaths := []string{
		".capysquash.yml",
		".capysquash.yaml",
		".github/.capysquash.yml",
		".github/.capysquash.yaml",
	}

	for _, path := range possiblePaths {
		content, err := h.githubClient.GetFileContent(ctx, repo, path, sha)
		if err != nil {
			continue // File doesn't exist, try next
		}

		// Parse YAML content
		var capyConfig config.CapySquashConfig
		if err := config.ParseCapySquashYAML([]byte(content), &capyConfig); err != nil {
			continue // Invalid YAML, try next file
		}

		return &capyConfig, nil
	}

	// No config found, return default
	return config.DefaultCapySquashConfig(), nil
}

// AnalysisResult contains migration analysis results
type AnalysisResult struct {
	OriginalCount       int
	OptimizedCount      int
	ConsolidationRatio  float64
	ShouldConsolidate   bool
	Redundancies        []tracking.RedundancyReport
	Warnings            []string
	ConsolidatedSQL     string
	HasCriticalWarnings bool // True if any critical warnings found
	HasDataLoss         bool // True if data loss operations detected
}

// analyzeMigrations performs analysis on migration files
func (h *WebhookHandler) analyzeMigrations(ctx context.Context, repo string, files []PRFile, capyConfig *config.CapySquashConfig) (*AnalysisResult, error) {
	// Download file contents
	migrations := make([]types.Migration, 0, len(files))

	for i, file := range files {
		content, err := h.githubClient.GetFileContent(ctx, repo, file.Filename, file.SHA)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to retrieve file content",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithFile(file.Filename).WithAdditional("sha", file.SHA)
		}

		m, err := parser.ParseMigration(content, file.Filename)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeSyntaxError,
				"failed to parse migration file",
				errors.SeverityError,
				errors.CategoryParsing,
			).WithInnerError(err).WithFile(file.Filename)
		}
		m.Sequence = i + 1
		migrations = append(migrations, *m)
	}

	// Track objects and find redundancies
	tracker := tracking.NewTracker()
	for i, m := range migrations {
		tracker.ProcessMigration(&m, i)
	}

	redundancies := tracker.GetRedundantObjects()
	stats := tracker.GetStatistics()

	// Run squashing to get optimized count and SQL
	migrationMap := make(map[int]string)
	for i, file := range files {
		content, _ := h.githubClient.GetFileContent(ctx, repo, file.Filename, file.SHA)
		migrationMap[i+1] = content
	}

	consolidatedSQL, warnings, err := h.analysisEngine.Squash(migrationMap)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeConsolidationFailed,
			"failed to squash migrations",
			errors.SeverityError,
			errors.CategoryConsolidation,
		).WithInnerError(err).WithAdditional("migration_count", len(migrationMap))
	}

	// Calculate consolidation ratio
	optimizedCount := strings.Count(consolidatedSQL, "\n")
	originalCount := stats.TotalStatements
	ratio := 0.0
	if originalCount > 0 {
		ratio = float64(optimizedCount) / float64(originalCount)
	}

	return &AnalysisResult{
		OriginalCount:      len(migrations),
		OptimizedCount:     optimizedCount,
		ConsolidationRatio: ratio,
		ShouldConsolidate:  len(migrations) >= 15 || ratio < 0.7,
		Redundancies:       redundancies,
		Warnings:           warnings,
		ConsolidatedSQL:    consolidatedSQL,
	}, nil
}

// formatAnalysisComment formats analysis results as a PR comment (legacy format)
func (h *WebhookHandler) formatAnalysisComment(analysis *AnalysisResult, fileCount int) string {
	var comment strings.Builder

	comment.WriteString("## 🔍 pgsquash Migration Analysis\n\n")
	comment.WriteString(fmt.Sprintf("**Migration Files:** %d\n", fileCount))
	comment.WriteString(fmt.Sprintf("**Consolidation Ratio:** %.1f%%\n\n", analysis.ConsolidationRatio*100))

	if analysis.ShouldConsolidate {
		comment.WriteString("### ☑ Consolidation Recommended\n\n")
		comment.WriteString("This PR can benefit from migration consolidation:\n")
		comment.WriteString(fmt.Sprintf("- Original statements: %d\n", analysis.OriginalCount))
		comment.WriteString(fmt.Sprintf("- After consolidation: ~%d statements\n", analysis.OptimizedCount))
		comment.WriteString(fmt.Sprintf("- Savings: ~%d statements\n\n", analysis.OriginalCount-analysis.OptimizedCount))
	} else {
		comment.WriteString("### ℹ️ Consolidation Status\n\n")
		comment.WriteString("Migrations appear well-optimized. No consolidation needed.\n\n")
	}

	if len(analysis.Redundancies) > 0 {
		comment.WriteString("### 🔄 Redundancies Found\n\n")
		for _, r := range analysis.Redundancies {
			comment.WriteString(fmt.Sprintf("- **%s** (%s): %s\n", r.Object, r.Type, r.Explanation))
		}
		comment.WriteString("\n")
	}

	if len(analysis.Warnings) > 0 {
		comment.WriteString("### ⚠️ Warnings\n\n")
		for _, w := range analysis.Warnings {
			comment.WriteString(fmt.Sprintf("- %s\n", w))
		}
		comment.WriteString("\n")
	}

	comment.WriteString("---\n")
	comment.WriteString("💡 **Commands:**\n")
	comment.WriteString("- `/pgsquash analyze` - Re-analyze migrations\n")
	comment.WriteString("- `/pgsquash consolidate` - Create consolidation PR\n")

	return comment.String()
}

// formatAnalysisCommentPlatformStyle formats analysis results matching CAPYSQUASH platform style
func (h *WebhookHandler) formatAnalysisCommentPlatformStyle(analysis *AnalysisResult, fileCount int, capyConfig *config.CapySquashConfig) string {
	var comment strings.Builder

	// Determine status emoji based on warnings and thresholds
	statusEmoji := "☑"
	statusText := "Analysis Successful"
	if len(analysis.Warnings) > 0 {
		if len(analysis.Warnings) > capyConfig.Checks.MaxWarnings || analysis.HasCriticalWarnings {
			statusEmoji = "☒"
			statusText = "Analysis Failed"
		} else {
			statusEmoji = "⚠️"
			statusText = "Analysis Completed with Warnings"
		}
	}

	// Header matching platform style
	comment.WriteString(fmt.Sprintf("## %s CAPYSQUASH Migration Analysis\n\n", statusEmoji))
	comment.WriteString(fmt.Sprintf("**Status**: %s\n", statusText))
	comment.WriteString(fmt.Sprintf("**Migration Files**: %d\n", fileCount))

	// Calculate consolidation percentage
	reductionPercent := 0.0
	if fileCount > 0 {
		reductionPercent = float64(fileCount-1) / float64(fileCount) * 100
	}
	comment.WriteString(fmt.Sprintf("**Potential Consolidation**: %d → %d files (%.0f%% reduction)\n\n",
		fileCount, 1, reductionPercent))

	// Analysis output section (if configured)
	if capyConfig.PRComment.IncludeStats {
		comment.WriteString("### 📊 Analysis Results\n\n")
		comment.WriteString(fmt.Sprintf("- **Original files**: %d migration files\n", fileCount))
		if analysis.ShouldConsolidate {
			comment.WriteString("- **Optimized**: 1 consolidated file\n")
			comment.WriteString(fmt.Sprintf("- **Time saved**: ~%d seconds per deployment\n", fileCount*10)) // Rough estimate
		} else {
			comment.WriteString("- **Status**: Migrations already optimized\n")
		}
		comment.WriteString("\n")
	}

	// Warnings section (if configured and warnings exist)
	if capyConfig.PRComment.IncludeWarnings && len(analysis.Warnings) > 0 {
		comment.WriteString("### ⚠️ Warnings\n\n")
		for i, w := range analysis.Warnings {
			if i >= 10 {
				comment.WriteString(fmt.Sprintf("_...and %d more warnings_\n", len(analysis.Warnings)-10))
				break
			}
			comment.WriteString(fmt.Sprintf("%d. %s\n", i+1, w))
		}
		comment.WriteString("\n")
	}

	// Recommendations section (if configured)
	if capyConfig.PRComment.IncludeRecommendations && fileCount >= capyConfig.MigrationThreshold {
		comment.WriteString("### 💡 Recommendation\n\n")
		comment.WriteString(fmt.Sprintf("You have %d migration files. Consider using `pgsquash squash` to consolidate them.\n\n", fileCount))
		comment.WriteString("```bash\n")
		comment.WriteString("pgsquash squash migrations/*.sql --output consolidated/ --safety standard\n")
		comment.WriteString("```\n\n")
		comment.WriteString("[View detailed analysis →](https://capysquash.dev/analyze)\n\n")
	}

	// Footer
	comment.WriteString("---\n")
	comment.WriteString("_Powered by [CAPYSQUASH](https://capysquash.dev) 🦫_\n")

	return comment.String()
}

// evaluateCheckConclusion determines the check run conclusion based on thresholds
func (h *WebhookHandler) evaluateCheckConclusion(analysis *AnalysisResult, capyConfig *config.CapySquashConfig) string {
	checks := capyConfig.Checks

	// Check for critical warnings
	if checks.FailOnCritical && analysis.HasCriticalWarnings {
		return "failure"
	}

	// Check warning count threshold
	if checks.MaxWarnings > 0 && len(analysis.Warnings) > checks.MaxWarnings {
		return "failure"
	}

	// Check if any warnings exist (strict mode)
	if checks.FailOnWarnings && len(analysis.Warnings) > 0 {
		return "failure"
	}

	// Check for data loss operations
	if checks.FailOnDataLoss && analysis.HasDataLoss {
		return "failure"
	}

	// Check minimum reduction percentage
	if checks.MinReductionPercent > 0 {
		reductionPercent := 0.0
		if analysis.OriginalCount > 0 {
			reductionPercent = float64(analysis.OriginalCount-analysis.OptimizedCount) / float64(analysis.OriginalCount) * 100
		}
		if reductionPercent < float64(checks.MinReductionPercent) {
			return "neutral"
		}
	}

	// Check if optimization is required
	if checks.RequireOptimization && !analysis.ShouldConsolidate {
		return "neutral"
	}

	// All checks passed
	if len(analysis.Warnings) > 0 {
		return "neutral" // Has warnings but within thresholds
	}
	return "success"
}

// createCheckRun creates a GitHub check run for the analysis
func (h *WebhookHandler) createCheckRun(ctx context.Context, event PullRequestEvent, analysis *AnalysisResult, conclusion string) error {
	// Create check run output
	title := "Migration Analysis Complete"
	summary := fmt.Sprintf("Analyzed %d migration files", analysis.OriginalCount)
	if analysis.ShouldConsolidate {
		summary += ", found potential for consolidation"
	}

	var output strings.Builder
	output.WriteString(fmt.Sprintf("**Files analyzed**: %d\n", analysis.OriginalCount))
	output.WriteString(fmt.Sprintf("**Warnings**: %d\n", len(analysis.Warnings)))
	output.WriteString(fmt.Sprintf("**Consolidation potential**: %s\n", func() string {
		if analysis.ShouldConsolidate {
			return "Yes"
		}
		return "No"
	}()))

	checkRun := &CheckRun{
		Name:       "pgsquash/migration-analysis",
		HeadSHA:    event.PullRequest.Head.SHA,
		Status:     "completed",
		Conclusion: conclusion,
		Output: &CheckRunOutput{
			Title:   title,
			Summary: summary,
			Text:    output.String(),
		},
	}

	repo := fmt.Sprintf("%s/%s", event.Repository.Owner.Login, event.Repository.Name)
	_, err := h.githubClient.CreateCheckRun(ctx, repo, checkRun)
	return err
}

// createConsolidationPR creates a new PR with consolidated migrations
func (h *WebhookHandler) createConsolidationPR(ctx context.Context, event PullRequestEvent, analysis *AnalysisResult) error {
	// Create a new branch
	baseBranch := event.PullRequest.Base.Ref
	newBranch := fmt.Sprintf("pgsquash/consolidate-pr-%d", event.PullRequest.Number)

	if err := h.githubClient.CreateBranch(ctx, event.Repository.FullName, newBranch, event.PullRequest.Head.SHA); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create consolidation branch",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("branch", newBranch).WithAdditional("sha", event.PullRequest.Head.SHA)
	}

	// Commit consolidated migration
	commitMsg := fmt.Sprintf("Consolidate migrations from PR #%d\n\nAutomatic consolidation by pgsquash bot", event.PullRequest.Number)
	filename := "migrations/001_consolidated.sql"

	if err := h.githubClient.CreateOrUpdateFile(ctx, event.Repository.FullName, filename, analysis.ConsolidatedSQL, commitMsg, newBranch); err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to commit consolidated file",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithFile(filename).WithAdditional("branch", newBranch)
	}

	// Create PR
	prTitle := fmt.Sprintf("🤖 Consolidated migrations from PR #%d", event.PullRequest.Number)
	prBody := fmt.Sprintf(`## Automated Migration Consolidation

This PR consolidates migrations from #%d.

### Summary
- Original migrations: %d files
- Consolidated to: 1 file
- Statement reduction: %d → %d

### Changes
- Removed redundant operations
- Optimized dependency order
- Preserved data integrity

---
*Generated by [pgsquash](https://github.com/CAPYSQUASH/pgsquash)*
`, event.PullRequest.Number, analysis.OriginalCount, analysis.OriginalCount, analysis.OptimizedCount)

	prNumber, err := h.githubClient.CreatePullRequest(ctx, event.Repository.FullName, prTitle, prBody, newBranch, baseBranch)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create consolidation pull request",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("head_branch", newBranch).WithAdditional("base_branch", baseBranch)
	}

	// Comment on original PR
	linkComment := fmt.Sprintf("☑ Created consolidation PR: #%d", prNumber)
	return h.githubClient.PostPRComment(ctx, event.Repository.FullName, event.PullRequest.Number, linkComment)
}

// verifySignature validates the webhook signature
func (h *WebhookHandler) verifySignature(r *http.Request) bool {
	signature := r.Header.Get("X-Hub-Signature-256")
	if signature == "" {
		return false
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return false
	}
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	mac := hmac.New(sha256.New, []byte(h.secret))
	mac.Write(body)
	expectedMAC := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(signature), []byte(expectedMAC))
}
