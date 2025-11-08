package github

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/bradleyfalzon/ghinstallation/v2"
	"github.com/google/go-github/v57/github"
)

// AppClient wraps GitHub App authentication and provides multi-repository support
type AppClient struct {
	appID          int64
	privateKeyPath string
	privateKey     []byte // Store private key for creating installation clients
	transport      *ghinstallation.AppsTransport
	installations  map[int64]*InstallationClient // Cache installation clients
}

// InstallationClient represents a GitHub App installation for a specific repository/organization
type InstallationClient struct {
	installationID int64
	client         *github.Client
	transport      *ghinstallation.Transport
	owner          string
	repo           string
}

// AppConfig holds GitHub App configuration
type AppConfig struct {
	AppID          int64
	PrivateKeyPath string
	PrivateKey     []byte // Alternative to PrivateKeyPath
}

// NewAppClient creates a new GitHub App client
func NewAppClient(config AppConfig) (*AppClient, error) {
	if config.AppID == 0 {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"GitHub App ID is required",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Set GITHUB_APP_ID environment variable")
	}

	var transport *ghinstallation.AppsTransport
	var err error

	if len(config.PrivateKey) > 0 {
		// Use private key bytes directly
		transport, err = ghinstallation.NewAppsTransport(
			http.DefaultTransport,
			config.AppID,
			config.PrivateKey,
		)
	} else if config.PrivateKeyPath != "" {
		// Use private key from file
		transport, err = ghinstallation.NewAppsTransportKeyFromFile(
			http.DefaultTransport,
			config.AppID,
			config.PrivateKeyPath,
		)
	} else {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"GitHub App private key or private key path is required",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithSuggestion("Set GITHUB_APP_PRIVATE_KEY or GITHUB_APP_PRIVATE_KEY_PATH environment variable")
	}

	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create GitHub App transport",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("Check that private key is valid and properly formatted")
	}

	return &AppClient{
		appID:          config.AppID,
		privateKeyPath: config.PrivateKeyPath,
		privateKey:     config.PrivateKey,
		transport:      transport,
		installations:  make(map[int64]*InstallationClient),
	}, nil
}

// NewAppClientFromEnv creates a GitHub App client from environment variables
func NewAppClientFromEnv() (*AppClient, error) {
	appIDStr := os.Getenv("GITHUB_APP_ID")
	if appIDStr == "" {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"GITHUB_APP_ID environment variable not set",
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}

	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"invalid GITHUB_APP_ID format",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithSuggestion("GITHUB_APP_ID must be a numeric value")
	}

	config := AppConfig{
		AppID: appID,
	}

	// Check for private key in environment (multiline string or base64)
	if privateKey := os.Getenv("GITHUB_APP_PRIVATE_KEY"); privateKey != "" {
		config.PrivateKey = []byte(privateKey)
	} else if privateKeyBase64 := os.Getenv("GITHUB_APP_PRIVATE_KEY_BASE64"); privateKeyBase64 != "" {
		// Support base64-encoded private key for environments that don't handle multiline secrets well
		decoded, err := base64DecodePrivateKey(privateKeyBase64)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to decode base64 private key",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithSuggestion("Ensure GITHUB_APP_PRIVATE_KEY_BASE64 is properly base64-encoded")
		}
		config.PrivateKey = decoded
	} else if privateKeyPath := os.Getenv("GITHUB_APP_PRIVATE_KEY_PATH"); privateKeyPath != "" {
		config.PrivateKeyPath = privateKeyPath
	} else {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"GITHUB_APP_PRIVATE_KEY, GITHUB_APP_PRIVATE_KEY_BASE64, or GITHUB_APP_PRIVATE_KEY_PATH environment variable not set",
			errors.SeverityError,
			errors.CategoryValidation,
		)
	}

	return NewAppClient(config)
}

// GetInstallationClient returns a client authenticated for a specific installation
func (a *AppClient) GetInstallationClient(ctx context.Context, installationID int64) (*InstallationClient, error) {
	// Check cache first
	if client, exists := a.installations[installationID]; exists {
		return client, nil
	}

	// Create installation transport
	var transport *ghinstallation.Transport
	var err error

	if a.privateKeyPath != "" {
		transport, err = ghinstallation.NewKeyFromFile(
			http.DefaultTransport,
			a.appID,
			installationID,
			a.privateKeyPath,
		)
	} else {
		// Get private key from transport (private key bytes)
		// Note: ghinstallation.AppsTransport doesn't expose PrivateKey() method
		// We need to use the original private key from the AppClient
		transport, err = ghinstallation.New(
			http.DefaultTransport,
			a.appID,
			installationID,
			a.privateKey,
		)
	}

	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create installation transport",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("installation_id", installationID)
	}

	client := github.NewClient(&http.Client{Transport: transport})

	installationClient := &InstallationClient{
		installationID: installationID,
		client:         client,
		transport:      transport,
	}

	// Cache the client
	a.installations[installationID] = installationClient

	return installationClient, nil
}

// GetInstallationForRepo finds the installation ID for a specific repository
func (a *AppClient) GetInstallationForRepo(ctx context.Context, owner, repo string) (int64, error) {
	appClient := github.NewClient(&http.Client{Transport: a.transport})

	installation, _, err := appClient.Apps.FindRepositoryInstallation(ctx, owner, repo)
	if err != nil {
		return 0, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to find installation for repository",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("owner", owner).WithAdditional("repo", repo).WithSuggestion("Ensure GitHub App is installed on this repository")
	}

	return installation.GetID(), nil
}

// GetInstallationClientForRepo gets an authenticated client for a specific repository
func (a *AppClient) GetInstallationClientForRepo(ctx context.Context, owner, repo string) (*InstallationClient, error) {
	installationID, err := a.GetInstallationForRepo(ctx, owner, repo)
	if err != nil {
		return nil, err
	}

	client, err := a.GetInstallationClient(ctx, installationID)
	if err != nil {
		return nil, err
	}

	// Store repo info
	client.owner = owner
	client.repo = repo

	return client, nil
}

// ListInstallations lists all installations of the GitHub App
func (a *AppClient) ListInstallations(ctx context.Context) ([]*github.Installation, error) {
	appClient := github.NewClient(&http.Client{Transport: a.transport})

	opts := &github.ListOptions{PerPage: 100}
	var allInstallations []*github.Installation

	for {
		installations, resp, err := appClient.Apps.ListInstallations(ctx, opts)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to list installations",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err)
		}

		allInstallations = append(allInstallations, installations...)

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allInstallations, nil
}

// GetAppInfo returns information about the GitHub App
func (a *AppClient) GetAppInfo(ctx context.Context) (*github.App, error) {
	appClient := github.NewClient(&http.Client{Transport: a.transport})

	app, _, err := appClient.Apps.Get(ctx, "")
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get app information",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}

	return app, nil
}

// InstallationClient methods that wrap the standard Client methods

// GetPRFiles retrieves files changed in a pull request
func (ic *InstallationClient) GetPRFiles(ctx context.Context, owner, repo string, prNumber int) ([]PRFile, error) {
	opts := &github.ListOptions{PerPage: 100}
	var allFiles []PRFile

	for {
		files, resp, err := ic.client.PullRequests.ListFiles(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to list pull request files",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithAdditional("owner", owner).WithAdditional("repo", repo).WithAdditional("pr_number", prNumber)
		}

		for _, file := range files {
			allFiles = append(allFiles, PRFile{
				Filename: file.GetFilename(),
				SHA:      file.GetSHA(),
				Status:   file.GetStatus(),
			})
		}

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	return allFiles, nil
}

// GetPullRequest retrieves pull request details
func (ic *InstallationClient) GetPullRequest(ctx context.Context, owner, repo string, prNumber int) (*PullRequest, error) {
	pr, _, err := ic.client.PullRequests.Get(ctx, owner, repo, prNumber)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get pull request details",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("owner", owner).WithAdditional("repo", repo).WithAdditional("pr_number", prNumber)
	}

	return &PullRequest{
		Number: pr.GetNumber(),
		Head: Branch{
			Ref: pr.GetHead().GetRef(),
			SHA: pr.GetHead().GetSHA(),
		},
		Base: Branch{
			Ref: pr.GetBase().GetRef(),
			SHA: pr.GetBase().GetSHA(),
		},
	}, nil
}

// PostPRComment posts a comment on a pull request
func (ic *InstallationClient) PostPRComment(ctx context.Context, owner, repo string, prNumber int, body string) error {
	comment := &github.IssueComment{
		Body: github.String(body),
	}

	_, _, err := ic.client.Issues.CreateComment(ctx, owner, repo, prNumber, comment)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create pull request comment",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("pr_number", prNumber)
	}

	return nil
}

// GetFileContent retrieves the content of a file at a specific ref
func (ic *InstallationClient) GetFileContent(ctx context.Context, owner, repo, path, ref string) (string, error) {
	opts := &github.RepositoryContentGetOptions{Ref: ref}

	fileContent, _, _, err := ic.client.Repositories.GetContents(ctx, owner, repo, path, opts)
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get file content",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithFile(path).WithAdditional("ref", ref)
	}

	if fileContent == nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"file not found",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithFile(path)
	}

	content, err := fileContent.GetContent()
	if err != nil {
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to decode file content",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithFile(path)
	}

	return content, nil
}

// CreateCommitStatus creates a commit status (check run) for a specific commit
func (ic *InstallationClient) CreateCommitStatus(ctx context.Context, owner, repo, sha string, status *CommitStatus) error {
	repoStatus := &github.RepoStatus{
		State:       github.String(status.State),
		TargetURL:   github.String(status.TargetURL),
		Description: github.String(status.Description),
		Context:     github.String(status.Context),
	}

	_, _, err := ic.client.Repositories.CreateStatus(ctx, owner, repo, sha, repoStatus)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create commit status",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("sha", sha).WithAdditional("state", status.State)
	}

	return nil
}

// CreateCheckRun creates a check run (preferred over commit status for GitHub Apps)
func (ic *InstallationClient) CreateCheckRun(ctx context.Context, owner, repo string, checkRun *CheckRun) (*github.CheckRun, error) {
	opts := github.CreateCheckRunOptions{
		Name:    checkRun.Name,
		HeadSHA: checkRun.HeadSHA,
	}

	if checkRun.Status != "" {
		opts.Status = github.String(checkRun.Status)
	}
	if checkRun.Conclusion != "" {
		opts.Conclusion = github.String(checkRun.Conclusion)
	}
	if checkRun.CompletedAt != nil {
		opts.CompletedAt = &github.Timestamp{Time: *checkRun.CompletedAt}
	}
	if checkRun.DetailsURL != "" {
		opts.DetailsURL = github.String(checkRun.DetailsURL)
	}
	if checkRun.ExternalID != "" {
		opts.ExternalID = github.String(checkRun.ExternalID)
	}

	if checkRun.Output != nil {
		opts.Output = &github.CheckRunOutput{
			Title:   github.String(checkRun.Output.Title),
			Summary: github.String(checkRun.Output.Summary),
		}
		if checkRun.Output.Text != "" {
			opts.Output.Text = github.String(checkRun.Output.Text)
		}
	}

	run, _, err := ic.client.Checks.CreateCheckRun(ctx, owner, repo, opts)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create check run",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("name", checkRun.Name).WithAdditional("sha", checkRun.HeadSHA)
	}

	return run, nil
}

// UpdateCheckRun updates an existing check run
func (ic *InstallationClient) UpdateCheckRun(ctx context.Context, owner, repo string, checkRunID int64, checkRun *CheckRun) (*github.CheckRun, error) {
	opts := github.UpdateCheckRunOptions{
		Name: checkRun.Name,
	}

	if checkRun.Status != "" {
		opts.Status = github.String(checkRun.Status)
	}
	if checkRun.Conclusion != "" {
		opts.Conclusion = github.String(checkRun.Conclusion)
	}
	if checkRun.CompletedAt != nil {
		opts.CompletedAt = &github.Timestamp{Time: *checkRun.CompletedAt}
	}
	if checkRun.DetailsURL != "" {
		opts.DetailsURL = github.String(checkRun.DetailsURL)
	}
	if checkRun.ExternalID != "" {
		opts.ExternalID = github.String(checkRun.ExternalID)
	}

	if checkRun.Output != nil {
		opts.Output = &github.CheckRunOutput{
			Title:   github.String(checkRun.Output.Title),
			Summary: github.String(checkRun.Output.Summary),
		}
		if checkRun.Output.Text != "" {
			opts.Output.Text = github.String(checkRun.Output.Text)
		}
	}

	run, _, err := ic.client.Checks.UpdateCheckRun(ctx, owner, repo, checkRunID, opts)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to update check run",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("check_run_id", checkRunID)
	}

	return run, nil
}

// CreateBranch creates a new branch
func (ic *InstallationClient) CreateBranch(ctx context.Context, owner, repo, branch, sha string) error {
	ref := &github.Reference{
		Ref: github.String("refs/heads/" + branch),
		Object: &github.GitObject{
			SHA: github.String(sha),
		},
	}

	_, _, err := ic.client.Git.CreateRef(ctx, owner, repo, ref)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create branch",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("branch", branch).WithAdditional("sha", sha)
	}

	return nil
}

// CreateOrUpdateFile creates or updates a file in a repository
func (ic *InstallationClient) CreateOrUpdateFile(ctx context.Context, owner, repo, path, content, message, branch string) error {
	// Check if file exists to get SHA
	var sha *string
	opts := &github.RepositoryContentGetOptions{Ref: branch}
	existingFile, _, _, err := ic.client.Repositories.GetContents(ctx, owner, repo, path, opts)
	if err == nil && existingFile != nil {
		sha = existingFile.SHA
	}

	// Create or update file options
	fileOpts := &github.RepositoryContentFileOptions{
		Message: github.String(message),
		Content: []byte(content),
		Branch:  github.String(branch),
		SHA:     sha,
	}

	_, _, err = ic.client.Repositories.CreateFile(ctx, owner, repo, path, fileOpts)
	if err != nil {
		return errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create or update file",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithFile(path).WithAdditional("branch", branch)
	}

	return nil
}

// CreatePullRequest creates a new pull request
func (ic *InstallationClient) CreatePullRequest(ctx context.Context, owner, repo, title, body, head, base string) (int, error) {
	newPR := &github.NewPullRequest{
		Title: github.String(title),
		Head:  github.String(head),
		Base:  github.String(base),
		Body:  github.String(body),
	}

	pr, _, err := ic.client.PullRequests.Create(ctx, owner, repo, newPR)
	if err != nil {
		return 0, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create pull request",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("head", head).WithAdditional("base", base)
	}

	return pr.GetNumber(), nil
}

// CommitStatus represents a commit status
type CommitStatus struct {
	State       string // pending, success, error, failure
	TargetURL   string
	Description string
	Context     string
}

// CheckRun represents a GitHub Check Run
type CheckRun struct {
	Name        string
	HeadSHA     string
	Status      string // queued, in_progress, completed
	Conclusion  string // success, failure, neutral, cancelled, skipped, timed_out, action_required
	StartedAt   *time.Time
	CompletedAt *time.Time
	DetailsURL  string
	ExternalID  string
	Output      *CheckRunOutput
}

// CheckRunOutput represents the output of a check run
type CheckRunOutput struct {
	Title   string
	Summary string
	Text    string
}

// GetRateLimit returns the current API rate limit status for the installation
func (ic *InstallationClient) GetRateLimit(ctx context.Context) (*github.RateLimits, error) {
	limits, _, err := ic.client.RateLimit.Get(ctx)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get rate limit",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err)
	}
	return limits, nil
}

// Helper function to format installation info
func (ic *InstallationClient) String() string {
	return fmt.Sprintf("InstallationClient{installationID: %d, owner: %s, repo: %s}",
		ic.installationID, ic.owner, ic.repo)
}

// base64DecodePrivateKey decodes a base64-encoded private key
// Handles both standard base64 and base64 with whitespace/newlines
func base64DecodePrivateKey(encoded string) ([]byte, error) {
	// Remove any whitespace/newlines that might have been added
	encoded = strings.ReplaceAll(encoded, "\n", "")
	encoded = strings.ReplaceAll(encoded, "\r", "")
	encoded = strings.ReplaceAll(encoded, " ", "")
	encoded = strings.ReplaceAll(encoded, "\t", "")

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	return decoded, nil
}
