package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Environment variable names for configuration.
const (
	LinearAPIKeyEnv   = "LINEAR_API_KEY"
	LinearAPIEndpoint = "LINEAR_API_ENDPOINT"
	TimeoutEnv        = "LINEAR_TIMEOUT"
	PageSizeEnv       = "LINEAR_PAGE_SIZE"
	CacheTTLEnv       = "LINEAR_CACHE_TTL"
	LogFileEnv        = "LINEAR_LOG_FILE"
	LogLevelEnv       = "LINEAR_LOG_LEVEL"
)

// Default configuration values.
const (
	DefaultTimeout        = 30 * time.Second
	DefaultPageSize       = 50
	DefaultCacheTTL       = 5 * time.Minute
	DefaultSearchDebounce = 300 * time.Millisecond
	DefaultAPIEndpoint    = "https://api.linear.app/graphql"
	DefaultLogLevel       = "warning" // debug, info, warning, error
	ThemeLinear           = "linear"
	ThemeHighContrast     = "high_contrast"
	ThemeColorBlind       = "color_blind"
	DefaultTheme          = ThemeLinear
	DensityComfortable    = "comfortable"
	DensityCompact        = "compact"
	DefaultDensity        = DensityComfortable
	DefaultAgentProvider  = "cursor"
	DefaultAgentSandbox   = "enabled"
)

// DateFilterSettings stores a timeless Linear date filter in user settings.
type DateFilterSettings struct {
	Eq   string `json:"eq,omitempty"`
	GT   string `json:"gt,omitempty"`
	GTE  string `json:"gte,omitempty"`
	LT   string `json:"lt,omitempty"`
	LTE  string `json:"lte,omitempty"`
	Null *bool  `json:"null,omitempty"`
}

// NumberFilterSettings stores a Linear numeric filter in user settings.
type NumberFilterSettings struct {
	Eq   *float64 `json:"eq,omitempty"`
	GT   *float64 `json:"gt,omitempty"`
	GTE  *float64 `json:"gte,omitempty"`
	LT   *float64 `json:"lt,omitempty"`
	LTE  *float64 `json:"lte,omitempty"`
	Null *bool    `json:"null,omitempty"`
}

// IssueFilterSettings stores command-palette issue filters in user settings.
type IssueFilterSettings struct {
	TeamIDs       []string             `json:"team_ids,omitempty"`
	TeamNames     []string             `json:"team_names,omitempty"`
	AssigneeIDs   []string             `json:"assignee_ids,omitempty"`
	AssigneeNames []string             `json:"assignee_names,omitempty"`
	LabelIDs      []string             `json:"label_ids,omitempty"`
	LabelNames    []string             `json:"label_names,omitempty"`
	StateIDs      []string             `json:"state_ids,omitempty"`
	StateNames    []string             `json:"state_names,omitempty"`
	ProjectIDs    []string             `json:"project_ids,omitempty"`
	ProjectNames  []string             `json:"project_names,omitempty"`
	CycleIDs      []string             `json:"cycle_ids,omitempty"`
	CycleNames    []string             `json:"cycle_names,omitempty"`
	DueDate       DateFilterSettings   `json:"due_date,omitempty"`
	Estimate      NumberFilterSettings `json:"estimate,omitempty"`
}

// getDefaultLogFile returns the default log file path: $HOME/.linear-tui/app.log
func getDefaultLogFile() string {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to empty string if home directory cannot be determined
		return ""
	}
	return filepath.Join(homeDir, ".linear-tui", "app.log")
}

// Config holds runtime configuration for the application.
type Config struct {
	// LinearAPIKey is the API key for authenticating with Linear.
	LinearAPIKey string

	// APIEndpoint is the Linear GraphQL API endpoint (useful for testing).
	APIEndpoint string

	// Timeout is the HTTP request timeout for API calls.
	Timeout time.Duration

	// PageSize is the default number of items to fetch per page.
	PageSize int

	// CacheTTL is the time-to-live for cached team metadata.
	CacheTTL time.Duration

	// SearchDebounce is the delay before live search refreshes issue results.
	SearchDebounce time.Duration

	// LogFile is the path to the log file (empty to disable logging).
	LogFile string

	// LogLevel is the minimum log level (debug, info, warning, error).
	LogLevel string

	// Theme controls the active UI theme.
	Theme string

	// Density controls the UI spacing density.
	Density string

	// AgentProvider selects the agent CLI provider (cursor or claude).
	AgentProvider string

	// AgentSandbox configures sandboxing for the agent CLI (enabled or disabled).
	AgentSandbox string

	// AgentModel selects the agent model when supported by the provider.
	AgentModel string

	// AgentWorkspace is the default workspace path for agent runs.
	AgentWorkspace string

	// IncludeCompleted controls whether completed/canceled issues are included.
	IncludeCompleted bool

	// Panel visibility preferences.
	ShowNavigation  bool
	ShowMyIssues    bool
	ShowOtherIssues bool

	// IssueSearchQuery is the persisted text search applied to issue queries.
	IssueSearchQuery string

	// IssueSort is the persisted issue ordering selected in the TUI.
	IssueSort string

	// IssueFilters are the persisted command-palette filters applied to issue queries.
	IssueFilters IssueFilterSettings
}

// LoadFromEnv loads configuration from environment variables.
// Returns an error if LINEAR_API_KEY is not set.
// Other values use sensible defaults if not specified.
func LoadFromEnv() (Config, error) {
	apiKey := os.Getenv(LinearAPIKeyEnv)
	if apiKey == "" {
		return Config{}, fmt.Errorf("%s environment variable is not set", LinearAPIKeyEnv)
	}

	cfg := Config{
		LinearAPIKey:     apiKey,
		APIEndpoint:      DefaultAPIEndpoint,
		Timeout:          DefaultTimeout,
		PageSize:         DefaultPageSize,
		CacheTTL:         DefaultCacheTTL,
		SearchDebounce:   DefaultSearchDebounce,
		LogFile:          getDefaultLogFile(), // Default: $HOME/.linear-tui/app.log
		LogLevel:         DefaultLogLevel,
		Theme:            DefaultTheme,
		Density:          DefaultDensity,
		AgentProvider:    DefaultAgentProvider,
		AgentSandbox:     DefaultAgentSandbox,
		AgentModel:       "",
		AgentWorkspace:   "",
		IncludeCompleted: false,
		ShowNavigation:   true,
		ShowMyIssues:     true,
		ShowOtherIssues:  true,
		IssueSearchQuery: "",
		IssueSort:        "",
		IssueFilters:     IssueFilterSettings{},
	}

	// Parse optional API endpoint override.
	if endpoint := os.Getenv(LinearAPIEndpoint); endpoint != "" {
		cfg.APIEndpoint = endpoint
	}

	// Parse optional timeout.
	if timeoutStr := os.Getenv(TimeoutEnv); timeoutStr != "" {
		timeout, err := parseDuration(timeoutStr, TimeoutEnv)
		if err != nil {
			return Config{}, err
		}
		cfg.Timeout = timeout
	}

	// Parse optional page size.
	if pageSizeStr := os.Getenv(PageSizeEnv); pageSizeStr != "" {
		pageSize, err := strconv.Atoi(pageSizeStr)
		if err != nil {
			return Config{}, fmt.Errorf("invalid %s value %q: %w", PageSizeEnv, pageSizeStr, err)
		}
		if err := validatePageSize(pageSize, PageSizeEnv); err != nil {
			return Config{}, err
		}
		cfg.PageSize = pageSize
	}

	// Parse optional cache TTL.
	if cacheTTLStr := os.Getenv(CacheTTLEnv); cacheTTLStr != "" {
		cacheTTL, err := parseDuration(cacheTTLStr, CacheTTLEnv)
		if err != nil {
			return Config{}, err
		}
		cfg.CacheTTL = cacheTTL
	}

	// Parse optional log file path.
	// If LINEAR_LOG_FILE is set to empty string, disable logging.
	// If not set, use default: $HOME/.linear-tui/app.log
	if logFile, ok := os.LookupEnv(LogFileEnv); ok {
		if logFile == "" {
			cfg.LogFile = "" // Explicitly disable logging
		} else {
			cfg.LogFile = logFile
		}
	}
	// If LINEAR_LOG_FILE is not set, cfg.LogFile already has the default value

	// Parse optional log level.
	if logLevel := os.Getenv(LogLevelEnv); logLevel != "" {
		if err := validateLogLevel(logLevel, LogLevelEnv); err != nil {
			return Config{}, err
		}
		cfg.LogLevel = logLevel
	}

	return cfg, nil
}
