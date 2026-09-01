package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// CapySquashConfig represents the .capysquash.yml configuration file
// This file is used for per-repository GitHub integration settings
// It takes precedence over pgsquash.config.json for GitHub-specific settings
type CapySquashConfig struct {
	// Core settings
	Enabled            bool   `yaml:"enabled" json:"enabled"`                         // Enable/disable pgsquash for this repository
	SafetyLevel        string `yaml:"safety_level" json:"safety_level"`               // paranoid | conservative | standard | aggressive
	MigrationThreshold int    `yaml:"migration_threshold" json:"migration_threshold"` // Minimum number of files to trigger consolidation suggestions

	// File patterns
	Include []string `yaml:"include" json:"include"` // File patterns to analyze
	Exclude []string `yaml:"exclude" json:"exclude"` // File patterns to exclude

	// PR comment settings
	PRComment PRCommentConfig `yaml:"pr_comment" json:"pr_comment"`

	// Pass/fail thresholds
	Checks ChecksConfig `yaml:"checks" json:"checks"`

	// Notification settings
	Notifications NotificationsConfig `yaml:"notifications" json:"notifications"`

	// Auto-apply settings (use with caution!)
	AutoApply AutoApplyConfig `yaml:"auto_apply" json:"auto_apply"`

	// Monorepo support
	Projects []ProjectConfig `yaml:"projects" json:"projects"`

	// Branch-specific settings
	Branches map[string]BranchConfig `yaml:"branches" json:"branches"`
}

// PRCommentConfig configures how PR comments are formatted and posted
type PRCommentConfig struct {
	Enabled                bool `yaml:"enabled" json:"enabled"`                                 // Post comments on PRs
	UpdateExisting         bool `yaml:"update_existing" json:"update_existing"`                 // Update existing comment vs create new
	IncludeStats           bool `yaml:"include_stats" json:"include_stats"`                     // Include file reduction stats
	IncludeWarnings        bool `yaml:"include_warnings" json:"include_warnings"`               // Include warnings section
	IncludeRecommendations bool `yaml:"include_recommendations" json:"include_recommendations"` // Include actionable recommendations
}

// ChecksConfig configures pass/fail thresholds for GitHub checks
type ChecksConfig struct {
	MaxWarnings         int             `yaml:"max_warnings" json:"max_warnings"`                   // Fail PR if warnings exceed this
	FailOnCritical      bool            `yaml:"fail_on_critical" json:"fail_on_critical"`           // Fail PR if critical warnings found
	FailOnWarnings      bool            `yaml:"fail_on_warnings" json:"fail_on_warnings"`           // Fail on any warnings
	FailOnDataLoss      bool            `yaml:"fail_on_data_loss" json:"fail_on_data_loss"`         // Fail on data loss operations
	MinReductionPercent int             `yaml:"min_reduction_percent" json:"min_reduction_percent"` // Require minimum file reduction percentage
	RequireOptimization bool            `yaml:"require_optimization" json:"require_optimization"`   // Fail if no migrations are optimized
	RequiredIndexes     []RequiredIndex `yaml:"required_indexes" json:"required_indexes"`           // Require specific indexes
}

// RequiredIndex specifies an index that must exist
type RequiredIndex struct {
	Table  string `yaml:"table" json:"table"`
	Column string `yaml:"column" json:"column"`
}

// NotificationsConfig configures notifications for analysis results
type NotificationsConfig struct {
	NotifyUsers  []string `yaml:"notify_users" json:"notify_users"`   // GitHub users to notify (@username)
	SlackChannel string   `yaml:"slack_channel" json:"slack_channel"` // Slack channel to post to (#channel)
}

// AutoApplyConfig configures automatic application of optimizations
type AutoApplyConfig struct {
	Enabled             bool     `yaml:"enabled" json:"enabled"`                             // Enable auto-apply
	Branches            []string `yaml:"branches" json:"branches"`                           // Branches to auto-apply on
	ExcludeBranches     []string `yaml:"exclude_branches" json:"exclude_branches"`           // Branches to never auto-apply
	RequireApprovalFrom []string `yaml:"require_approval_from" json:"require_approval_from"` // Require approval from users
}

// ProjectConfig configures a project in a monorepo
type ProjectConfig struct {
	Name        string   `yaml:"name" json:"name"`                 // Project name
	Include     []string `yaml:"include" json:"include"`           // File patterns for this project
	SafetyLevel string   `yaml:"safety_level" json:"safety_level"` // Safety level for this project
}

// BranchConfig configures branch-specific settings
type BranchConfig struct {
	SafetyLevel    string `yaml:"safety_level" json:"safety_level"`         // Safety level for this branch
	FailOnWarnings bool   `yaml:"fail_on_warnings" json:"fail_on_warnings"` // Fail on warnings for this branch
}

// DefaultCapySquashConfig returns default configuration for .capysquash.yml
func DefaultCapySquashConfig() *CapySquashConfig {
	return &CapySquashConfig{
		Enabled:            true,
		SafetyLevel:        "standard",
		MigrationThreshold: 15,
		Include: []string{
			"migrations/**/*.sql",
			"db/migrate/*.sql",
		},
		Exclude: []string{
			"**/seeds/**",
			"**/fixtures/**",
			"**/*_rollback.sql",
		},
		PRComment: PRCommentConfig{
			Enabled:                true,
			UpdateExisting:         true,
			IncludeStats:           true,
			IncludeWarnings:        true,
			IncludeRecommendations: true,
		},
		Checks: ChecksConfig{
			MaxWarnings:         5,
			FailOnCritical:      true,
			FailOnWarnings:      false,
			FailOnDataLoss:      true,
			MinReductionPercent: 0,
			RequireOptimization: false,
			RequiredIndexes:     []RequiredIndex{},
		},
		Notifications: NotificationsConfig{
			NotifyUsers:  []string{},
			SlackChannel: "",
		},
		AutoApply: AutoApplyConfig{
			Enabled:             false,
			Branches:            []string{},
			ExcludeBranches:     []string{"main", "master", "production"},
			RequireApprovalFrom: []string{},
		},
		Projects: []ProjectConfig{},
		Branches: map[string]BranchConfig{},
	}
}

// LoadCapySquashConfig loads .capysquash.yml from the current directory or specified path
func LoadCapySquashConfig(path string) (*CapySquashConfig, error) {
	// If no path specified, look for .capysquash.yml in current directory
	if path == "" {
		possiblePaths := []string{
			".capysquash.yml",
			".capysquash.yaml",
			".github/.capysquash.yml",
			".github/.capysquash.yaml",
		}

		var foundPath string
		for _, p := range possiblePaths {
			if _, err := os.Stat(p); err == nil {
				foundPath = p
				break
			}
		}

		if foundPath == "" {
			// No config file found, return default
			return DefaultCapySquashConfig(), nil
		}
		path = foundPath
	}

	// Read the file
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read .capysquash.yml: %w", err)
	}

	// Parse YAML
	var config CapySquashConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse .capysquash.yml: %w", err)
	}

	// Apply defaults for any missing values
	applyCapySquashDefaults(&config)

	return &config, nil
}

// LoadCapySquashConfigFromRepo loads .capysquash.yml from a repository directory
func LoadCapySquashConfigFromRepo(repoPath string) (*CapySquashConfig, error) {
	possiblePaths := []string{
		filepath.Join(repoPath, ".capysquash.yml"),
		filepath.Join(repoPath, ".capysquash.yaml"),
		filepath.Join(repoPath, ".github", ".capysquash.yml"),
		filepath.Join(repoPath, ".github", ".capysquash.yaml"),
	}

	for _, path := range possiblePaths {
		if _, err := os.Stat(path); err == nil {
			return LoadCapySquashConfig(path)
		}
	}

	// No config found, return default
	return DefaultCapySquashConfig(), nil
}

// applyCapySquashDefaults fills in missing values with defaults
func applyCapySquashDefaults(config *CapySquashConfig) {
	defaults := DefaultCapySquashConfig()

	if config.SafetyLevel == "" {
		config.SafetyLevel = defaults.SafetyLevel
	}

	if config.MigrationThreshold == 0 {
		config.MigrationThreshold = defaults.MigrationThreshold
	}

	if len(config.Include) == 0 {
		config.Include = defaults.Include
	}

	if len(config.Exclude) == 0 {
		config.Exclude = defaults.Exclude
	}

	// Set PRComment defaults if not set
	if !config.PRComment.Enabled && !config.PRComment.UpdateExisting {
		config.PRComment = defaults.PRComment
	}

	// Set Checks defaults
	if config.Checks.MaxWarnings == 0 && !config.Checks.FailOnCritical {
		config.Checks = defaults.Checks
	}

	// Set AutoApply defaults
	if len(config.AutoApply.ExcludeBranches) == 0 {
		config.AutoApply.ExcludeBranches = defaults.AutoApply.ExcludeBranches
	}
}

// MergeWithEngineConfig merges CapySquashConfig into the engine Config
// CAPYSQUASH settings take precedence for overlapping fields
func (c *CapySquashConfig) MergeWithEngineConfig(engineConfig *Config) *Config {
	// Create a copy of the engine config
	merged := *engineConfig

	// Override safety level if set in capysquash config
	if c.SafetyLevel != "" {
		merged.SafetyLevel = c.SafetyLevel
	}

	// Override exclude patterns if set
	if len(c.Exclude) > 0 {
		merged.ExcludePatterns = c.Exclude
	}

	return &merged
}

// GetProjectConfig returns the project config for a given set of file paths (for monorepo support)
func (c *CapySquashConfig) GetProjectConfig(files []string) *ProjectConfig {
	if len(c.Projects) == 0 {
		return nil
	}

	// Find the first project that matches any of the files
	for _, project := range c.Projects {
		for _, file := range files {
			for _, pattern := range project.Include {
				// Simple glob matching (can be enhanced with filepath.Match)
				if matchesGlob(file, pattern) {
					return &project
				}
			}
		}
	}

	return nil
}

// GetBranchConfig returns branch-specific config if it exists
func (c *CapySquashConfig) GetBranchConfig(branchName string) *BranchConfig {
	if config, ok := c.Branches[branchName]; ok {
		return &config
	}
	return nil
}

// ShouldAnalyze determines if pgsquash should analyze the given files
func (c *CapySquashConfig) ShouldAnalyze(files []string) bool {
	if !c.Enabled {
		return false
	}

	// Check if any file matches include patterns and not exclude patterns
	for _, file := range files {
		included := false
		excluded := false

		// Check include patterns
		for _, pattern := range c.Include {
			if matchesGlob(file, pattern) {
				included = true
				break
			}
		}

		// Check exclude patterns
		for _, pattern := range c.Exclude {
			if matchesGlob(file, pattern) {
				excluded = true
				break
			}
		}

		if included && !excluded {
			return true
		}
	}

	return false
}

// ShouldAutoApply determines if auto-apply should be used for the given branch
func (c *CapySquashConfig) ShouldAutoApply(branchName string) bool {
	if !c.AutoApply.Enabled {
		return false
	}

	// Check if branch is excluded
	for _, excluded := range c.AutoApply.ExcludeBranches {
		if branchName == excluded {
			return false
		}
	}

	// If specific branches are listed, check if current branch is in the list
	if len(c.AutoApply.Branches) > 0 {
		for _, allowed := range c.AutoApply.Branches {
			if branchName == allowed {
				return true
			}
		}
		return false
	}

	// No specific branches listed, auto-apply is enabled for all non-excluded branches
	return true
}

// matchesGlob is a simple glob matcher (supports ** and *)
func matchesGlob(path, pattern string) bool {
	// Simple implementation - can be enhanced with filepath.Match or doublestar library
	// For now, just support basic patterns
	if pattern == "**" || pattern == "*" {
		return true
	}

	// Check if pattern contains path separators
	if filepath.Dir(pattern) != "." {
		// Pattern has directory components, match the full path
		matched, _ := filepath.Match(pattern, path)
		return matched
	}

	// Pattern is just a filename, match against basename
	matched, _ := filepath.Match(pattern, filepath.Base(path))
	return matched
}

// ParseCapySquashYAML parses YAML content into CapySquashConfig
func ParseCapySquashYAML(data []byte, config *CapySquashConfig) error {
	if err := yaml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("failed to parse YAML: %w", err)
	}
	applyCapySquashDefaults(config)
	return nil
}
