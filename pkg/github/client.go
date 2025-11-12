package github

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/capysquash/pgsquash-engine/internal/errors"
	"github.com/google/go-github/v57/github"
	"golang.org/x/oauth2"
	"gopkg.in/yaml.v3"
)

// Client is a GitHub API client wrapping go-github library
type Client struct {
	client *github.Client
	token  string
}

// NewClient creates a new GitHub client using go-github library
func NewClient(token string) *Client {
	// Create OAuth2 token source
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: token},
	)
	httpClient := oauth2.NewClient(context.Background(), ts)

	// Create GitHub client
	ghClient := github.NewClient(httpClient)

	return &Client{
		client: ghClient,
		token:  token,
	}
}

// PRFile represents a file in a pull request
type PRFile struct {
	Filename string `json:"filename"`
	SHA      string `json:"sha"`
	Status   string `json:"status"`
}

// PullRequest represents a GitHub pull request
type PullRequest struct {
	Number int    `json:"number"`
	Head   Branch `json:"head"`
	Base   Branch `json:"base"`
}

// Branch represents a git branch
type Branch struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// RepoConfig represents pgsquash configuration in a repository
type RepoConfig struct {
	AutoAnalyze        bool   `yaml:"auto_analyze"`
	AutoPR             bool   `yaml:"auto_pr"`
	MigrationThreshold int    `yaml:"migration_threshold"`
	SafetyLevel        string `yaml:"safety_level"`
}

// GetPRFiles retrieves files changed in a pull request with automatic pagination
func (c *Client) GetPRFiles(ctx context.Context, repo string, prNumber int) ([]PRFile, error) {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

	// Use go-github's ListFiles with automatic pagination
	opts := &github.ListOptions{
		PerPage: 100, // Maximum per page
	}

	var allFiles []PRFile
	for {
		files, resp, err := c.client.PullRequests.ListFiles(ctx, owner, repoName, prNumber, opts)
		if err != nil {
			return nil, errors.NewError(
				errors.ErrorCodeValidationFailed,
				"failed to list pull request files",
				errors.SeverityError,
				errors.CategoryValidation,
			).WithInnerError(err).WithAdditional("owner", owner).WithAdditional("repo", repoName).WithAdditional("pr_number", prNumber)
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
func (c *Client) GetPullRequest(ctx context.Context, repo string, prNumber int) (*PullRequest, error) {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

	pr, _, err := c.client.PullRequests.Get(ctx, owner, repoName, prNumber)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get pull request details",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("owner", owner).WithAdditional("repo", repoName).WithAdditional("pr_number", prNumber)
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

// GetFileContent retrieves the content of a file
func (c *Client) GetFileContent(ctx context.Context, repo, path, ref string) (string, error) {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return "", err
	}

	opts := &github.RepositoryContentGetOptions{
		Ref: ref,
	}

	fileContent, _, _, err := c.client.Repositories.GetContents(ctx, owner, repoName, path, opts)
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
		// Fallback to manual base64 decoding if GetContent() fails
		if fileContent.Encoding != nil && *fileContent.Encoding == "base64" && fileContent.Content != nil {
			decoded, decodeErr := base64.StdEncoding.DecodeString(
				strings.ReplaceAll(*fileContent.Content, "\n", ""),
			)
			if decodeErr != nil {
				return "", errors.NewError(
					errors.ErrorCodeValidationFailed,
					"failed to decode file content",
					errors.SeverityError,
					errors.CategoryValidation,
				).WithInnerError(decodeErr).WithFile(path)
			}
			return string(decoded), nil
		}
		return "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to get file content",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithFile(path)
	}

	return content, nil
}

// PostPRComment posts a comment on a pull request
func (c *Client) PostPRComment(ctx context.Context, repo string, prNumber int, comment string) error {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return err
	}

	issueComment := &github.IssueComment{
		Body: github.String(comment),
	}

	_, _, err = c.client.Issues.CreateComment(ctx, owner, repoName, prNumber, issueComment)
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

// GetRepoConfig retrieves pgsquash configuration from repository
func (c *Client) GetRepoConfig(ctx context.Context, repo, ref string) (*RepoConfig, error) {
	content, err := c.GetFileContent(ctx, repo, ".github/pgsquash.yml", ref)
	if err != nil {
		// Return default config if file doesn't exist
		return &RepoConfig{
			AutoAnalyze:        true,
			AutoPR:             false,
			MigrationThreshold: 15,
			SafetyLevel:        "standard",
		}, nil
	}

	var config RepoConfig
	if err := yaml.Unmarshal([]byte(content), &config); err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to parse repository config",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithFile(".github/pgsquash.yml")
	}

	return &config, nil
}

// CreateBranch creates a new branch
func (c *Client) CreateBranch(ctx context.Context, repo, branch, sha string) error {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return err
	}

	ref := &github.Reference{
		Ref: github.String("refs/heads/" + branch),
		Object: &github.GitObject{
			SHA: github.String(sha),
		},
	}

	_, _, err = c.client.Git.CreateRef(ctx, owner, repoName, ref)
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
func (c *Client) CreateOrUpdateFile(ctx context.Context, repo, path, content, message, branch string) error {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return err
	}

	// Check if file exists to get SHA
	var sha *string
	opts := &github.RepositoryContentGetOptions{Ref: branch}
	existingFile, _, _, err := c.client.Repositories.GetContents(ctx, owner, repoName, path, opts)
	if err == nil && existingFile != nil {
		sha = existingFile.SHA
	}

	// Create or update file options
	fileOpts := &github.RepositoryContentFileOptions{
		Message: github.String(message),
		Content: []byte(content),
		Branch:  github.String(branch),
		SHA:     sha, // nil for create, populated for update
	}

	_, _, err = c.client.Repositories.CreateFile(ctx, owner, repoName, path, fileOpts)
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
func (c *Client) CreatePullRequest(ctx context.Context, repo, title, body, head, base string) (int, error) {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return 0, err
	}

	newPR := &github.NewPullRequest{
		Title: github.String(title),
		Head:  github.String(head),
		Base:  github.String(base),
		Body:  github.String(body),
	}

	pr, _, err := c.client.PullRequests.Create(ctx, owner, repoName, newPR)
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

// GetRateLimit returns the current API rate limit status
func (c *Client) GetRateLimit(ctx context.Context) (*github.RateLimits, error) {
	limits, _, err := c.client.RateLimit.Get(ctx)
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

// CreateCommitStatus creates a commit status for a specific commit
func (c *Client) CreateCommitStatus(ctx context.Context, repo, sha string, status *CommitStatus) error {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return err
	}

	repoStatus := &github.RepoStatus{
		State:       github.String(status.State),
		TargetURL:   github.String(status.TargetURL),
		Description: github.String(status.Description),
		Context:     github.String(status.Context),
	}

	_, _, err = c.client.Repositories.CreateStatus(ctx, owner, repoName, sha, repoStatus)
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

// CreateCheckRun creates a GitHub Check Run (preferred for GitHub Apps)
func (c *Client) CreateCheckRun(ctx context.Context, repo string, checkRun *CheckRun) (*github.CheckRun, error) {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

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

	run, _, err := c.client.Checks.CreateCheckRun(ctx, owner, repoName, opts)
	if err != nil {
		return nil, errors.NewError(
			errors.ErrorCodeValidationFailed,
			"failed to create check run",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithInnerError(err).WithAdditional("check_name", checkRun.Name).WithAdditional("repo", repo)
	}

	return run, nil
}

// UpdateCheckRun updates an existing check run
func (c *Client) UpdateCheckRun(ctx context.Context, repo string, checkRunID int64, checkRun *CheckRun) (*github.CheckRun, error) {
	owner, repoName, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

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

	run, _, err := c.client.Checks.UpdateCheckRun(ctx, owner, repoName, checkRunID, opts)
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

// parseRepo parses "owner/repo" format into owner and repo strings
func parseRepo(repo string) (string, string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return "", "", errors.NewError(
			errors.ErrorCodeValidationFailed,
			"invalid repository format",
			errors.SeverityError,
			errors.CategoryValidation,
		).WithAdditional("repo", repo).WithSuggestion("Expected format: 'owner/repo'")
	}
	return parts[0], parts[1], nil
}

// SetHTTPClient allows setting a custom HTTP client (useful for testing)
func (c *Client) SetHTTPClient(httpClient *http.Client) {
	c.client = github.NewClient(httpClient)
}
