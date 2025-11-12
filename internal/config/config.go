package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type Config struct {
	SafetyLevel            string                   `json:"safety_level"`
	ProdDBDSN              string                   `json:"prod_db_dsn"`
	Output                 OutputConfig             `json:"output"`
	Rules                  RulesConfig              `json:"rules"`
	ExcludePatterns        []string                 `json:"exclude_patterns"`
	IncludeSchemas         []string                 `json:"include_schemas"`
	Performance            PerformanceConfig        `json:"performance"`
	ModernFeatures         ModernFeaturesConfig     `json:"modern_features"`
	ConflictResolution     ConflictResolutionConfig `json:"conflict_resolution"`
	PostgreSQLFeatures     PostgreSQLFeaturesConfig `json:"postgresql_features"`
	ThirdPartyIntegrations ThirdPartyConfig         `json:"third_party_integrations"`
	Plugins                PluginSettings           `json:"plugins"`    // Plugin system configuration
	Validation             ValidationConfig         `json:"validation"` // Docker validation configuration
	AI                     AIConfig                 `json:"ai"`         // AI-powered analysis and validation
}

type OutputConfig struct {
	Format                   string `json:"format"`
	PreserveComments         bool   `json:"preserve_comments"`
	AddConsolidationComments bool   `json:"add_consolidation_comments"`
	FileNaming               string `json:"file_naming"`
	Directory                string `json:"directory"`
}

type RulesConfig struct {
	TableOperations    TableRulesConfig    `json:"table_operations"`
	IndexOperations    IndexRulesConfig    `json:"index_operations"`
	FunctionOperations FunctionRulesConfig `json:"function_operations"`
}

type TableRulesConfig struct {
	ConsolidateCreateAlter bool `json:"consolidate_create_alter"`
	RemoveDropCreateCycles bool `json:"remove_drop_create_cycles"`
	PreserveDataOperations bool `json:"preserve_data_operations"`
}

type IndexRulesConfig struct {
	ConsolidateRecreations    bool `json:"consolidate_recreations"`
	PreserveUniqueConstraints bool `json:"preserve_unique_constraints"`
}

type FunctionRulesConfig struct {
	RemoveDuplicateDefinitions bool `json:"remove_duplicate_definitions"`
	PreserveSignatureChanges   bool `json:"preserve_signature_changes"`
}

type PerformanceConfig struct {
	StreamingThresholdMB int  `json:"streaming_threshold_mb"`
	ParallelProcessing   bool `json:"parallel_processing"`
	ShowProgress         bool `json:"show_progress"`
}

// New configuration structures for modern PostgreSQL features

type ModernFeaturesConfig struct {
	EnableVectorSupport    bool `json:"enable_vector_support"`
	EnableGeneratedColumns bool `json:"enable_generated_columns"`
	EnableEventSourcing    bool `json:"enable_event_sourcing"`
	EnableMergeStatements  bool `json:"enable_merge_statements"`
	EnableMultirangeTypes  bool `json:"enable_multirange_types"`
	EnableAdvancedRLS      bool `json:"enable_advanced_rls"`
}

type ConflictResolutionConfig struct {
	EnablePrioritySystem  bool   `json:"enable_priority_system"`
	StrictModeEnabled     bool   `json:"strict_mode_enabled"`
	AllowOverlappingRules bool   `json:"allow_overlapping_rules"`
	ConflictLogLevel      string `json:"conflict_log_level"`
}

type PostgreSQLFeaturesConfig struct {
	TargetVersion          string   `json:"target_version"`
	EnabledExtensions      []string `json:"enabled_extensions"`
	OptimizeForPerformance bool     `json:"optimize_for_performance"`
	UseModernSyntax        bool     `json:"use_modern_syntax"`
	ValidateCompatibility  bool     `json:"validate_compatibility"`
}

type ThirdPartyConfig struct {
	Auth0Integration       Auth0Config       `json:"auth0_integration"`
	NextAuthIntegration    NextAuthConfig    `json:"nextauth_integration"`
	SupabaseIntegration    SupabaseConfig    `json:"supabase_integration"`
	ClerkIntegration       ClerkConfig       `json:"clerk_integration"`
	VectorIntegration      VectorConfig      `json:"vector_integration"`
	PlanetScaleIntegration PlanetScaleConfig `json:"planetscale_integration"`
}

type Auth0Config struct {
	Enabled       bool     `json:"enabled"`
	Domain        string   `json:"domain"`
	CustomClaims  []string `json:"custom_claims"`
	RoleClaimPath string   `json:"role_claim_path"`
}

type NextAuthConfig struct {
	Enabled         bool     `json:"enabled"`
	SessionStrategy string   `json:"session_strategy"`
	DatabaseTables  []string `json:"database_tables"`
}

type SupabaseConfig struct {
	Enabled            bool   `json:"enabled"`
	JWTSecret          string `json:"jwt_secret"`
	EnableRLS          bool   `json:"enable_rls"`
	StorageIntegration bool   `json:"storage_integration"`
}

type ClerkConfig struct {
	Enabled               bool   `json:"enabled"`
	JWTVersion            string `json:"jwt_version"`
	OrganizationSupport   bool   `json:"organization_support"`
	PublicMetadataSupport bool   `json:"public_metadata_support"`
}

type VectorConfig struct {
	Enabled          bool     `json:"enabled"`
	DefaultIndexType string   `json:"default_index_type"`
	OptimizeQueries  bool     `json:"optimize_queries"`
	SupportedOps     []string `json:"supported_ops"`
}

type PlanetScaleConfig struct {
	Enabled                bool `json:"enabled"`
	DisableForeignKeys     bool `json:"disable_foreign_keys"`
	OptimizeForReplication bool `json:"optimize_for_replication"`
}

// PluginSettings configures the plugin system behavior
type PluginSettings struct {
	AutoDetect      bool     `json:"auto_detect"`      // Automatically detect and enable plugins (default: true)
	EnabledPlugins  []string `json:"enabled_plugins"`  // Explicitly enabled plugins (empty = auto-detect all)
	DisabledPlugins []string `json:"disabled_plugins"` // Explicitly disabled plugins
	Verbose         bool     `json:"verbose"`          // Log plugin activity (default: false)
}

// ValidationConfig configures Docker-based validation behavior
type ValidationConfig struct {
	Mode                     string `json:"mode"`                       // Validation approach: TWO_CONTAINERS, TWO_DATABASES, or SCHEMA_DIFF
	DockerImage              string `json:"docker_image"`               // PostgreSQL Docker image (default: postgres:17)
	TimeoutSeconds           int    `json:"timeout_seconds"`            // Validation timeout in seconds (default: 120)
	ContainerReadyTimeout    int    `json:"container_ready_timeout"`    // Container startup timeout in seconds (default: 150, recommended for complex migrations with many extensions)
	EnableExtensionDetection bool   `json:"enable_extension_detection"` // Auto-detect and install extensions (default: true)
	AutoInstallExtensions    bool   `json:"auto_install_extensions"`    // Automatically install detected extensions (default: true)
	EnableSQLFixes           bool   `json:"enable_sql_fixes"`           // Apply automatic SQL fixes during validation (default: false)
	EnablePreprocessing      bool   `json:"enable_preprocessing"`       // Preprocess SQL to fix common issues (e.g., deduplicate publication statements) (default: true)
	Verbose                  bool   `json:"verbose"`                    // Show detailed validation output (default: true)
}

// AIConfig configures AI-powered analysis and validation behavior
type AIConfig struct {
	Enabled                        bool    `json:"enabled"`                           // Enable AI features (default: false, requires API keys)
	Provider                       string  `json:"provider"`                          // AI provider: "claude", "openai", "azure-openai", or "auto" (default: "auto")
	MaxRetries                     int     `json:"max_retries"`                       // Max retry attempts for AI calls (default: 3)
	TimeoutSeconds                 int     `json:"timeout_seconds"`                   // Timeout for AI operations in seconds (default: 60)
	EnableSemanticAnalysis         bool    `json:"enable_semantic_analysis"`          // Use AI for semantic function comparison (default: false)
	EnableDeadCodeDetection        bool    `json:"enable_dead_code_detection"`        // Use AI for dead code detection (default: false)
	EnableAuthPatternDetection     bool    `json:"enable_auth_pattern_detection"`     // Use AI to detect auth patterns (default: true if enabled)
	EnablePostProcessingValidation bool    `json:"enable_post_processing_validation"` // Use AI for post-processing validation (default: false)
	EnableAutoRepair               bool    `json:"enable_auto_repair"`                // Allow AI to automatically fix issues (default: false, requires manual review)
	ConfidenceThreshold            float64 `json:"confidence_threshold"`              // Minimum confidence for AI suggestions (0.0-1.0, default: 0.85)
}

func DefaultConfig() *Config {
	return &Config{
		SafetyLevel: "standard",
		ProdDBDSN:   os.Getenv("PROD_DB_DSN"), // Default to environment variable
		Output: OutputConfig{
			Format:                   "organized",
			PreserveComments:         true,
			AddConsolidationComments: true,
			FileNaming:               "semantic",
			Directory:                "squashed",
		},
		Rules: RulesConfig{
			TableOperations: TableRulesConfig{
				ConsolidateCreateAlter: true,
				RemoveDropCreateCycles: true,
				PreserveDataOperations: true,
			},
			IndexOperations: IndexRulesConfig{
				ConsolidateRecreations:    true,
				PreserveUniqueConstraints: true,
			},
			FunctionOperations: FunctionRulesConfig{
				RemoveDuplicateDefinitions: true,
				PreserveSignatureChanges:   false,
			},
		},
		ExcludePatterns: []string{"auth.*", "extensions.*"},
		IncludeSchemas:  []string{"public"},
		Performance: PerformanceConfig{
			StreamingThresholdMB: 5,
			ParallelProcessing:   true,
			ShowProgress:         true,
		},
		ModernFeatures: ModernFeaturesConfig{
			EnableVectorSupport:    true,
			EnableGeneratedColumns: true,
			EnableEventSourcing:    true,
			EnableMergeStatements:  true,
			EnableMultirangeTypes:  true,
			EnableAdvancedRLS:      true,
		},
		ConflictResolution: ConflictResolutionConfig{
			EnablePrioritySystem:  true,
			StrictModeEnabled:     false,
			AllowOverlappingRules: false,
			ConflictLogLevel:      "warn",
		},
		PostgreSQLFeatures: PostgreSQLFeaturesConfig{
			TargetVersion:          "17",
			EnabledExtensions:      []string{"vector", "pg_stat_statements", "uuid-ossp"},
			OptimizeForPerformance: true,
			UseModernSyntax:        true,
			ValidateCompatibility:  true,
		},
		ThirdPartyIntegrations: ThirdPartyConfig{
			Auth0Integration: Auth0Config{
				Enabled:       false,
				Domain:        "",
				CustomClaims:  []string{"permissions", "role"},
				RoleClaimPath: "https://myapp.com/role",
			},
			NextAuthIntegration: NextAuthConfig{
				Enabled:         false,
				SessionStrategy: "database",
				DatabaseTables:  []string{"accounts", "sessions", "users"},
			},
			SupabaseIntegration: SupabaseConfig{
				Enabled:            true,
				JWTSecret:          "",
				EnableRLS:          true,
				StorageIntegration: true,
			},
			ClerkIntegration: ClerkConfig{
				Enabled:               false,
				JWTVersion:            "v2",
				OrganizationSupport:   true,
				PublicMetadataSupport: true,
			},
			VectorIntegration: VectorConfig{
				Enabled:          true,
				DefaultIndexType: "ivfflat",
				OptimizeQueries:  true,
				SupportedOps:     []string{"vector_cosine_ops", "vector_l2_ops", "vector_ip_ops"},
			},
			PlanetScaleIntegration: PlanetScaleConfig{
				Enabled:                false,
				DisableForeignKeys:     true,
				OptimizeForReplication: true,
			},
		},
		Plugins: PluginSettings{
			AutoDetect:      true,       // Automatically detect applicable plugins
			EnabledPlugins:  []string{}, // Empty = enable all detected plugins
			DisabledPlugins: []string{}, // Explicitly disable specific plugins
			Verbose:         false,      // Don't log plugin details by default
		},
		Validation: ValidationConfig{
			Mode:                     "TWO_DATABASES", // Best balance of speed and accuracy
			DockerImage:              "postgres:17",   // Default PostgreSQL version (latest stable)
			TimeoutSeconds:           120,             // 2 minute timeout for validation
			ContainerReadyTimeout:    300,             // 300 second timeout for container startup (sufficient for heavy extensions like postgis)
			EnableExtensionDetection: true,            // Auto-detect required extensions
			AutoInstallExtensions:    true,            // Auto-install detected extensions
			EnableSQLFixes:           false,           // Manual review recommended by default
			EnablePreprocessing:      true,            // Preprocess SQL to fix common issues (e.g., deduplicate publications)
			Verbose:                  true,            // Show detailed validation output
		},
		AI: AIConfig{
			Enabled:                        false,  // Disabled by default, requires API keys (ANTHROPIC_API_KEY, OPENAI_API_KEY, or AZURE_OPENAI_ENDPOINT)
			Provider:                       "auto", // Auto-detect best available provider (Claude > OpenAI > Azure)
			MaxRetries:                     3,      // Retry AI calls up to 3 times with exponential backoff
			TimeoutSeconds:                 60,     // 1 minute timeout for AI operations
			EnableSemanticAnalysis:         false,  // Conservative default - AI semantic analysis is experimental
			EnableDeadCodeDetection:        false,  // Conservative default - may have false positives
			EnableAuthPatternDetection:     true,   // Safe to enable when AI is on - helps detect auth patterns
			EnablePostProcessingValidation: false,  // Conservative default - post-processing AI validation is experimental
			EnableAutoRepair:               false,  // Conservative default - requires manual review
			ConfidenceThreshold:            0.85,   // Only use AI suggestions with 85%+ confidence
		},
	}
}

// mergeConfigs merges two Config structs, with loaded values taking precedence over defaults
// For zero values in loaded, uses the default values to preserve defaults for optional fields
func mergeConfigs(loaded, defaults *Config) *Config {
	result := *loaded // Start with loaded config

	// Merge string fields if empty in loaded
	if result.SafetyLevel == "" {
		result.SafetyLevel = defaults.SafetyLevel
	}
	if result.ProdDBDSN == "" {
		result.ProdDBDSN = defaults.ProdDBDSN
	}

	// Merge nested struct fields
	result.Output = mergeOutputConfig(loaded.Output, defaults.Output)
	result.Rules = mergeRulesConfig(loaded.Rules, defaults.Rules)
	result.Performance = mergePerformanceConfig(loaded.Performance, defaults.Performance)
	result.ModernFeatures = mergeModernFeaturesConfig(loaded.ModernFeatures, defaults.ModernFeatures)
	result.ConflictResolution = mergeConflictResolutionConfig(loaded.ConflictResolution, defaults.ConflictResolution)
	result.PostgreSQLFeatures = mergePostgreSQLFeaturesConfig(loaded.PostgreSQLFeatures, defaults.PostgreSQLFeatures)
	result.ThirdPartyIntegrations = mergeThirdPartyConfig(loaded.ThirdPartyIntegrations, defaults.ThirdPartyIntegrations)
	result.Plugins = mergePluginSettings(loaded.Plugins, defaults.Plugins)
	result.Validation = mergeValidationConfig(loaded.Validation, defaults.Validation)
	result.AI = mergeAIConfig(loaded.AI, defaults.AI)

	// Merge slices if empty in loaded
	if len(result.ExcludePatterns) == 0 {
		result.ExcludePatterns = defaults.ExcludePatterns
	}
	if len(result.IncludeSchemas) == 0 {
		result.IncludeSchemas = defaults.IncludeSchemas
	}

	return &result
}

// Helper functions for merging nested config structs
func mergeOutputConfig(loaded, defaults OutputConfig) OutputConfig {
	if loaded.Format == "" {
		loaded.Format = defaults.Format
	}
	if loaded.FileNaming == "" {
		loaded.FileNaming = defaults.FileNaming
	}
	if loaded.Directory == "" {
		loaded.Directory = defaults.Directory
	}
	return loaded
}

func mergeRulesConfig(loaded, defaults RulesConfig) RulesConfig {
	return loaded // Boolean fields use zero values correctly
}

func mergePerformanceConfig(loaded, defaults PerformanceConfig) PerformanceConfig {
	if loaded.StreamingThresholdMB == 0 {
		loaded.StreamingThresholdMB = defaults.StreamingThresholdMB
	}
	return loaded
}

func mergeModernFeaturesConfig(loaded, defaults ModernFeaturesConfig) ModernFeaturesConfig {
	return loaded // Boolean fields use zero values correctly
}

func mergeConflictResolutionConfig(loaded, defaults ConflictResolutionConfig) ConflictResolutionConfig {
	if loaded.ConflictLogLevel == "" {
		loaded.ConflictLogLevel = defaults.ConflictLogLevel
	}
	return loaded
}

func mergePostgreSQLFeaturesConfig(loaded, defaults PostgreSQLFeaturesConfig) PostgreSQLFeaturesConfig {
	if loaded.TargetVersion == "" {
		loaded.TargetVersion = defaults.TargetVersion
	}
	if len(loaded.EnabledExtensions) == 0 {
		loaded.EnabledExtensions = defaults.EnabledExtensions
	}
	return loaded
}

func mergeThirdPartyConfig(loaded, defaults ThirdPartyConfig) ThirdPartyConfig {
	// Merge each nested integration config
	loaded.Auth0Integration = mergeAuth0Config(loaded.Auth0Integration, defaults.Auth0Integration)
	loaded.NextAuthIntegration = mergeNextAuthConfig(loaded.NextAuthIntegration, defaults.NextAuthIntegration)
	loaded.SupabaseIntegration = mergeSupabaseConfig(loaded.SupabaseIntegration, defaults.SupabaseIntegration)
	loaded.ClerkIntegration = mergeClerkConfig(loaded.ClerkIntegration, defaults.ClerkIntegration)
	loaded.VectorIntegration = mergeVectorConfig(loaded.VectorIntegration, defaults.VectorIntegration)
	loaded.PlanetScaleIntegration = mergePlanetScaleConfig(loaded.PlanetScaleIntegration, defaults.PlanetScaleIntegration)
	return loaded
}

func mergeAuth0Config(loaded, defaults Auth0Config) Auth0Config {
	if loaded.Domain == "" {
		loaded.Domain = defaults.Domain
	}
	if loaded.RoleClaimPath == "" {
		loaded.RoleClaimPath = defaults.RoleClaimPath
	}
	if len(loaded.CustomClaims) == 0 {
		loaded.CustomClaims = defaults.CustomClaims
	}
	return loaded
}

func mergeNextAuthConfig(loaded, defaults NextAuthConfig) NextAuthConfig {
	if loaded.SessionStrategy == "" {
		loaded.SessionStrategy = defaults.SessionStrategy
	}
	if len(loaded.DatabaseTables) == 0 {
		loaded.DatabaseTables = defaults.DatabaseTables
	}
	return loaded
}

func mergeSupabaseConfig(loaded, defaults SupabaseConfig) SupabaseConfig {
	if loaded.JWTSecret == "" {
		loaded.JWTSecret = defaults.JWTSecret
	}
	return loaded
}

func mergeClerkConfig(loaded, defaults ClerkConfig) ClerkConfig {
	if loaded.JWTVersion == "" {
		loaded.JWTVersion = defaults.JWTVersion
	}
	return loaded
}

func mergeVectorConfig(loaded, defaults VectorConfig) VectorConfig {
	if loaded.DefaultIndexType == "" {
		loaded.DefaultIndexType = defaults.DefaultIndexType
	}
	if len(loaded.SupportedOps) == 0 {
		loaded.SupportedOps = defaults.SupportedOps
	}
	return loaded
}

func mergePlanetScaleConfig(loaded, defaults PlanetScaleConfig) PlanetScaleConfig {
	return loaded // Boolean fields use zero values correctly
}

func mergePluginSettings(loaded, defaults PluginSettings) PluginSettings {
	return loaded // Lists and booleans use zero values correctly
}

func mergeValidationConfig(loaded, defaults ValidationConfig) ValidationConfig {
	if loaded.Mode == "" {
		loaded.Mode = defaults.Mode
	}
	if loaded.DockerImage == "" {
		loaded.DockerImage = defaults.DockerImage
	}
	if loaded.TimeoutSeconds == 0 {
		loaded.TimeoutSeconds = defaults.TimeoutSeconds
	}
	if loaded.ContainerReadyTimeout == 0 {
		loaded.ContainerReadyTimeout = defaults.ContainerReadyTimeout
	}
	return loaded
}

func mergeAIConfig(loaded, defaults AIConfig) AIConfig {
	if loaded.Provider == "" {
		loaded.Provider = defaults.Provider
	}
	if loaded.MaxRetries == 0 {
		loaded.MaxRetries = defaults.MaxRetries
	}
	if loaded.TimeoutSeconds == 0 {
		loaded.TimeoutSeconds = defaults.TimeoutSeconds
	}
	if loaded.ConfidenceThreshold == 0 {
		loaded.ConfidenceThreshold = defaults.ConfidenceThreshold
	}
	return loaded
}

func LoadConfig(configPath string) (*Config, error) {
	defaults := DefaultConfig()

	if configPath == "" {
		configPath = "pgsquash.config.json"
	}

	if _, err := os.Stat(configPath); err == nil {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return nil, err
		}

		// Unmarshal into a new config to avoid overwriting defaults
		loaded := &Config{}
		if err := json.Unmarshal(data, loaded); err != nil {
			// Check if this is a JSON syntax error
			if syntaxErr, ok := err.(*json.SyntaxError); ok {
				// Calculate line number and context for the error
				lineNum, colNum, contextLine := getJSONErrorContext(data, syntaxErr.Offset)
				return nil, &JSONSyntaxError{
					Path:    configPath,
					Line:    lineNum,
					Column:  colNum,
					Context: contextLine,
					Message: syntaxErr.Error(),
				}
			}
			// Check if this is a JSON type error
			if typeErr, ok := err.(*json.UnmarshalTypeError); ok {
				lineNum, colNum, contextLine := getJSONErrorContext(data, typeErr.Offset)
				return nil, &JSONTypeError{
					Path:     configPath,
					Line:     lineNum,
					Column:   colNum,
					Context:  contextLine,
					Field:    typeErr.Field,
					Expected: typeErr.Type.String(),
					Got:      typeErr.Value,
				}
			}
			// Unknown JSON error
			return nil, &GenericJSONError{
				Path:    configPath,
				Message: err.Error(),
			}
		}

		// Merge loaded config with defaults (loaded values take precedence)
		cfg := mergeConfigs(loaded, defaults)

		// Validate config values
		if err := cfg.Validate(); err != nil {
			return nil, &ConfigValidationError{
				Path:   configPath,
				Errors: err.(*ValidationErrors).Errors,
			}
		}

		return cfg, nil
	}

	return defaults, nil
}

// Validate checks if the config values are valid
func (c *Config) Validate() error {
	errs := &ValidationErrors{
		Errors: make([]string, 0),
	}

	// Validate safety_level
	validSafetyLevels := map[string]bool{
		"paranoid":     true,
		"conservative": true,
		"standard":     true,
		"aggressive":   true,
	}
	if !validSafetyLevels[c.SafetyLevel] {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid safety_level '%s' (must be one of: paranoid, conservative, standard, aggressive)", c.SafetyLevel))
	}

	// Validate output.format
	validFormats := map[string]bool{
		"organized":  true,
		"sequential": true,
		"minimal":    true,
	}
	if !validFormats[c.Output.Format] {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid output.format '%s' (must be one of: organized, sequential, minimal)", c.Output.Format))
	}

	// Validate output.file_naming
	validFileNaming := map[string]bool{
		"semantic":   true,
		"sequential": true,
		"timestamp":  true,
	}
	if c.Output.FileNaming != "" && !validFileNaming[c.Output.FileNaming] {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid output.file_naming '%s' (must be one of: semantic, sequential, timestamp)", c.Output.FileNaming))
	}

	// Validate conflict_resolution.conflict_log_level
	validLogLevels := map[string]bool{
		"debug": true,
		"info":  true,
		"warn":  true,
		"error": true,
	}
	if !validLogLevels[c.ConflictResolution.ConflictLogLevel] {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid conflict_resolution.conflict_log_level '%s' (must be one of: debug, info, warn, error)", c.ConflictResolution.ConflictLogLevel))
	}

	// Validate validation.mode
	validValidationModes := map[string]bool{
		"TWO_CONTAINERS": true,
		"TWO_DATABASES":  true,
		"SCHEMA_DIFF":    true,
	}
	if !validValidationModes[c.Validation.Mode] {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid validation.mode '%s' (must be one of: TWO_CONTAINERS, TWO_DATABASES, SCHEMA_DIFF)", c.Validation.Mode))
	}

	// Validate ai.provider
	validAIProviders := map[string]bool{
		"auto":         true,
		"claude":       true,
		"openai":       true,
		"azure-openai": true,
	}
	if !validAIProviders[c.AI.Provider] {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid ai.provider '%s' (must be one of: auto, claude, openai, azure-openai)", c.AI.Provider))
	}

	// Validate ai.confidence_threshold
	if c.AI.ConfidenceThreshold < 0.0 || c.AI.ConfidenceThreshold > 1.0 {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid ai.confidence_threshold %.2f (must be between 0.0 and 1.0)", c.AI.ConfidenceThreshold))
	}

	// Validate numeric ranges for performance settings
	if c.Performance.StreamingThresholdMB < 0 {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid performance.streaming_threshold_mb %d (cannot be negative)", c.Performance.StreamingThresholdMB))
	}

	// Validate numeric ranges for validation settings
	if c.Validation.TimeoutSeconds <= 0 {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid validation.timeout_seconds %d (must be positive)", c.Validation.TimeoutSeconds))
	}

	if c.Validation.ContainerReadyTimeout <= 0 {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid validation.container_ready_timeout %d (must be positive)", c.Validation.ContainerReadyTimeout))
	}

	// Validate numeric ranges for AI settings
	if c.AI.MaxRetries < 0 {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid ai.max_retries %d (cannot be negative)", c.AI.MaxRetries))
	}

	if c.AI.TimeoutSeconds <= 0 {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid ai.timeout_seconds %d (must be positive)", c.AI.TimeoutSeconds))
	}

	// Validate third_party_integrations.clerk_integration.jwt_version
	validJWTVersions := map[string]bool{
		"v1": true,
		"v2": true,
	}
	if c.ThirdPartyIntegrations.ClerkIntegration.JWTVersion != "" &&
		!validJWTVersions[c.ThirdPartyIntegrations.ClerkIntegration.JWTVersion] {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid third_party_integrations.clerk_integration.jwt_version '%s' (must be one of: v1, v2)", c.ThirdPartyIntegrations.ClerkIntegration.JWTVersion))
	}

	// Validate third_party_integrations.nextauth_integration.session_strategy
	validSessionStrategies := map[string]bool{
		"database": true,
		"jwt":      true,
	}
	if c.ThirdPartyIntegrations.NextAuthIntegration.SessionStrategy != "" &&
		!validSessionStrategies[c.ThirdPartyIntegrations.NextAuthIntegration.SessionStrategy] {
		errs.Errors = append(errs.Errors,
			fmt.Sprintf("invalid third_party_integrations.nextauth_integration.session_strategy '%s' (must be one of: database, jwt)", c.ThirdPartyIntegrations.NextAuthIntegration.SessionStrategy))
	}

	if len(errs.Errors) > 0 {
		return errs
	}

	return nil
}

// ValidationErrors contains multiple validation errors
type ValidationErrors struct {
	Errors []string
}

func (e *ValidationErrors) Error() string {
	return fmt.Sprintf("configuration validation failed: %s", strings.Join(e.Errors, "; "))
}

// ConfigValidationError represents config validation errors with helpful messages
type ConfigValidationError struct {
	Path   string
	Errors []string
}

func (e *ConfigValidationError) Error() string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Configuration validation errors in %s:\n\n", e.Path))
	for i, err := range e.Errors {
		sb.WriteString(fmt.Sprintf("  %d. %s\n", i+1, err))
	}
	sb.WriteString("\nPlease fix these errors and try again.\n")
	sb.WriteString("Run 'pgsquash init-config' to generate a valid configuration file.\n")
	return sb.String()
}

// JSONSyntaxError represents a JSON syntax error with helpful context
type JSONSyntaxError struct {
	Path    string
	Line    int
	Column  int
	Context string
	Message string
}

func (e *JSONSyntaxError) Error() string {
	return fmt.Sprintf(
		"JSON syntax error in %s at line %d, column %d:\n"+
			"  %s\n"+
			"  %s^\n"+
			"  %s\n\n"+
			"Common fixes:\n"+
			"  ► Check for missing or extra commas\n"+
			"  ► Ensure all strings are properly quoted\n"+
			"  ► Verify brackets {} and [] are balanced\n"+
			"  ► Remove trailing commas before closing brackets",
		e.Path, e.Line, e.Column,
		e.Context,
		strings.Repeat(" ", e.Column-1),
		e.Message,
	)
}

// JSONTypeError represents a JSON type mismatch error
type JSONTypeError struct {
	Path     string
	Line     int
	Column   int
	Context  string
	Field    string
	Expected string
	Got      string
}

func (e *JSONTypeError) Error() string {
	return fmt.Sprintf(
		"JSON type error in %s at line %d, column %d:\n"+
			"  Field '%s' expects type %s but got %s\n"+
			"  %s\n"+
			"  %s^\n\n"+
			"Fix: Ensure the value matches the expected type",
		e.Path, e.Line, e.Column,
		e.Field, e.Expected, e.Got,
		e.Context,
		strings.Repeat(" ", e.Column-1),
	)
}

// GenericJSONError represents other JSON parsing errors
type GenericJSONError struct {
	Path    string
	Message string
}

func (e *GenericJSONError) Error() string {
	return fmt.Sprintf("Error parsing JSON config %s: %s", e.Path, e.Message)
}

// getJSONErrorContext extracts the line number, column number, and context line for a JSON error
func getJSONErrorContext(data []byte, offset int64) (line int, col int, context string) {
	if offset < 0 || offset > int64(len(data)) {
		return 0, 0, ""
	}

	line = 1
	col = 1
	lineStart := 0

	for i := 0; i < int(offset); i++ {
		if data[i] == '\n' {
			line++
			col = 1
			lineStart = i + 1
		} else {
			col++
		}
	}

	// Extract the line containing the error
	lineEnd := lineStart
	for lineEnd < len(data) && data[lineEnd] != '\n' {
		lineEnd++
	}

	context = string(data[lineStart:lineEnd])
	return line, col, context
}

func (c *Config) SaveToFile(path string) error {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	// Prepend a comment block to the JSON file
	commentHeader := `{
  "_comment_file_naming": "File naming strategies: 'semantic' (default) names files by content type (tables, indexes, functions), 'sequential' uses numbered format (001_, 002_), 'timestamp' uses migration timestamps",
`

	// Remove the opening brace from the JSON data and prepend our comment
	dataStr := string(data)
	if len(dataStr) > 0 && dataStr[0] == '{' {
		dataStr = commentHeader + dataStr[1:]
	}

	return os.WriteFile(path, []byte(dataStr), 0644)
}
