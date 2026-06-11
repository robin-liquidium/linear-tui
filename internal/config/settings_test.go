package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestEnsureSettingsFileCreatesDefaults verifies missing settings are created with defaults.
func TestEnsureSettingsFileCreatesDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, "nested", "config.json")

	settings, err := EnsureSettingsFile(settingsPath)
	if err != nil {
		t.Fatalf("EnsureSettingsFile() error: %v", err)
	}

	if _, err := os.Stat(settingsPath); err != nil {
		t.Fatalf("settings file not created: %v", err)
	}

	if _, err := os.Stat(filepath.Dir(settingsPath)); err != nil {
		t.Fatalf("settings directory not created: %v", err)
	}

	assertSettingsEqual(t, settings, DefaultSettings())
}

// TestLoadSettingsAppliesDefaults verifies missing fields use default values.
func TestLoadSettingsAppliesDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, "config.json")

	data := []byte(`{"page_size":123}`)
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatalf("write settings file: %v", err)
	}

	settings, err := LoadSettings(settingsPath)
	if err != nil {
		t.Fatalf("LoadSettings() error: %v", err)
	}

	expected := DefaultSettings()
	expected.PageSize = 123
	assertSettingsEqual(t, settings, expected)
}

func TestLoadSettingsAppliesDefaultSearchDebounce(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, "config.json")

	data := []byte(`{"page_size":123}`)
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatalf("write settings file: %v", err)
	}

	settings, err := LoadSettings(settingsPath)
	if err != nil {
		t.Fatalf("LoadSettings() error: %v", err)
	}

	if settings.SearchDebounce != DefaultSearchDebounce.String() {
		t.Fatalf("SearchDebounce = %q, want %q", settings.SearchDebounce, DefaultSearchDebounce.String())
	}
}

func TestConfigFromSettingsParsesSearchDebounce(t *testing.T) {
	settings := DefaultSettings()
	settings.SearchDebounce = "450ms"

	cfg, err := ConfigFromSettings("test-key", settings)
	if err != nil {
		t.Fatalf("ConfigFromSettings() error: %v", err)
	}

	if cfg.SearchDebounce != 450*time.Millisecond {
		t.Fatalf("SearchDebounce = %s, want 450ms", cfg.SearchDebounce)
	}
}

func TestLoadSettingsPreservesIssueFilters(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, "config.json")

	data := []byte(`{
		"issue_search_query": "urgent bugs",
		"issue_sort": "priority",
		"issue_filters": {
			"team_ids": ["team-1"],
			"team_names": ["Platform"],
			"assignee_ids": ["user-1"],
			"assignee_names": ["Robin"],
			"label_ids": ["label-1"],
			"label_names": ["Bug"],
			"state_ids": ["state-1"],
			"state_names": ["Todo"],
			"project_ids": ["project-1"],
			"project_names": ["Launch"],
			"cycle_ids": ["cycle-1"],
			"cycle_names": ["Cycle 1"],
			"due_date": {"eq": "2026-06-15"},
			"estimate": {"eq": 3}
		}
	}`)
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatalf("write settings file: %v", err)
	}

	settings, err := LoadSettings(settingsPath)
	if err != nil {
		t.Fatalf("LoadSettings() error: %v", err)
	}

	expectedEstimate := 3.0
	if settings.IssueSearchQuery != "urgent bugs" {
		t.Fatalf("IssueSearchQuery = %q, want urgent bugs", settings.IssueSearchQuery)
	}
	if settings.IssueSort != "priority" {
		t.Fatalf("IssueSort = %q, want priority", settings.IssueSort)
	}
	if !reflect.DeepEqual(settings.IssueFilters.TeamIDs, []string{"team-1"}) {
		t.Fatalf("TeamIDs = %+v, want team-1", settings.IssueFilters.TeamIDs)
	}
	if settings.IssueFilters.DueDate.Eq != "2026-06-15" {
		t.Fatalf("DueDate.Eq = %q, want 2026-06-15", settings.IssueFilters.DueDate.Eq)
	}
	if settings.IssueFilters.Estimate.Eq == nil || *settings.IssueFilters.Estimate.Eq != expectedEstimate {
		t.Fatalf("Estimate.Eq = %+v, want %v", settings.IssueFilters.Estimate.Eq, expectedEstimate)
	}
}

func TestConfigFromSettingsCopiesIssueFilters(t *testing.T) {
	settings := DefaultSettings()
	settings.IssueSearchQuery = "  open bugs  "
	settings.IssueSort = "  status  "
	settings.IssueFilters.TeamIDs = []string{"team-1"}

	cfg, err := ConfigFromSettings("test-key", settings)
	if err != nil {
		t.Fatalf("ConfigFromSettings() error: %v", err)
	}

	if cfg.IssueSearchQuery != "open bugs" {
		t.Fatalf("IssueSearchQuery = %q, want open bugs", cfg.IssueSearchQuery)
	}
	if cfg.IssueSort != "status" {
		t.Fatalf("IssueSort = %q, want status", cfg.IssueSort)
	}
	if !reflect.DeepEqual(cfg.IssueFilters.TeamIDs, []string{"team-1"}) {
		t.Fatalf("TeamIDs = %+v, want team-1", cfg.IssueFilters.TeamIDs)
	}
}

func TestConfigFromSettingsDefaultsBlankOptionalValues(t *testing.T) {
	settings := DefaultSettings()
	settings.APIEndpoint = ""
	settings.Timeout = "0s"
	settings.CacheTTL = ""
	settings.SearchDebounce = ""
	settings.LogLevel = ""
	settings.Theme = ""
	settings.Density = ""
	settings.AgentProvider = ""
	settings.AgentSandbox = ""

	cfg, err := ConfigFromSettings("test-key", settings)
	if err != nil {
		t.Fatalf("ConfigFromSettings() error: %v", err)
	}

	if cfg.APIEndpoint != DefaultAPIEndpoint {
		t.Fatalf("APIEndpoint = %q, want %q", cfg.APIEndpoint, DefaultAPIEndpoint)
	}
	if cfg.Timeout != DefaultTimeout {
		t.Fatalf("Timeout = %s, want %s", cfg.Timeout, DefaultTimeout)
	}
	if cfg.CacheTTL != DefaultCacheTTL {
		t.Fatalf("CacheTTL = %s, want %s", cfg.CacheTTL, DefaultCacheTTL)
	}
	if cfg.SearchDebounce != DefaultSearchDebounce {
		t.Fatalf("SearchDebounce = %s, want %s", cfg.SearchDebounce, DefaultSearchDebounce)
	}
	if cfg.LogLevel != DefaultLogLevel {
		t.Fatalf("LogLevel = %q, want %q", cfg.LogLevel, DefaultLogLevel)
	}
	if cfg.Theme != DefaultTheme {
		t.Fatalf("Theme = %q, want %q", cfg.Theme, DefaultTheme)
	}
	if cfg.Density != DefaultDensity {
		t.Fatalf("Density = %q, want %q", cfg.Density, DefaultDensity)
	}
	if cfg.AgentProvider != DefaultAgentProvider {
		t.Fatalf("AgentProvider = %q, want %q", cfg.AgentProvider, DefaultAgentProvider)
	}
	if cfg.AgentSandbox != DefaultAgentSandbox {
		t.Fatalf("AgentSandbox = %q, want %q", cfg.AgentSandbox, DefaultAgentSandbox)
	}
}

// TestLoadSettingsPreservesEmptyLogFile ensures an empty log file disables logging.
func TestLoadSettingsPreservesEmptyLogFile(t *testing.T) {
	tmpDir := t.TempDir()
	settingsPath := filepath.Join(tmpDir, "config.json")

	data := []byte(`{"log_file": ""}`)
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		t.Fatalf("write settings file: %v", err)
	}

	settings, err := LoadSettings(settingsPath)
	if err != nil {
		t.Fatalf("LoadSettings() error: %v", err)
	}

	expected := DefaultSettings()
	expected.LogFile = ""
	assertSettingsEqual(t, settings, expected)
}

// TestConfigFromSettingsValidation checks invalid settings are rejected.
func TestConfigFromSettingsValidation(t *testing.T) {
	base := DefaultSettings()

	tests := []struct {
		name   string
		mutate func(Settings) Settings
	}{
		{
			name: "invalid timeout",
			mutate: func(settings Settings) Settings {
				settings.Timeout = "not-a-duration"
				return settings
			},
		},
		{
			name: "invalid cache ttl",
			mutate: func(settings Settings) Settings {
				settings.CacheTTL = "bad-duration"
				return settings
			},
		},
		{
			name: "invalid search debounce",
			mutate: func(settings Settings) Settings {
				settings.SearchDebounce = "bad-duration"
				return settings
			},
		},
		{
			name: "zero search debounce",
			mutate: func(settings Settings) Settings {
				settings.SearchDebounce = "0s"
				return settings
			},
		},
		{
			name: "negative search debounce",
			mutate: func(settings Settings) Settings {
				settings.SearchDebounce = "-1ms"
				return settings
			},
		},
		{
			name: "page size too low",
			mutate: func(settings Settings) Settings {
				settings.PageSize = 0
				return settings
			},
		},
		{
			name: "page size too high",
			mutate: func(settings Settings) Settings {
				settings.PageSize = 300
				return settings
			},
		},
		{
			name: "invalid log level",
			mutate: func(settings Settings) Settings {
				settings.LogLevel = "verbose"
				return settings
			},
		},
		{
			name: "invalid theme",
			mutate: func(settings Settings) Settings {
				settings.Theme = "rainbow"
				return settings
			},
		},
		{
			name: "invalid density",
			mutate: func(settings Settings) Settings {
				settings.Density = "ultra"
				return settings
			},
		},
		{
			name: "invalid agent provider",
			mutate: func(settings Settings) Settings {
				settings.AgentProvider = "unknown"
				return settings
			},
		},
		{
			name: "invalid agent sandbox",
			mutate: func(settings Settings) Settings {
				settings.AgentSandbox = "maybe"
				return settings
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := tt.mutate(base)
			_, err := ConfigFromSettings("test-key", settings)
			if err == nil {
				t.Errorf("ConfigFromSettings() expected error for %s", tt.name)
			}
		})
	}
}

// TestConfigFromSettingsRequiresAPIKey verifies API key is mandatory.
func TestConfigFromSettingsRequiresAPIKey(t *testing.T) {
	_, err := ConfigFromSettings("", DefaultSettings())
	if err == nil {
		t.Error("ConfigFromSettings() expected error when API key is empty")
	}
}

func TestConfigFromSettingsAPIKeyFromSettings(t *testing.T) {
	settings := DefaultSettings()
	settings.LinearAPIKey = "settings-key"

	cfg, err := ConfigFromSettings("", settings)
	if err != nil {
		t.Fatalf("ConfigFromSettings() error: %v", err)
	}
	if cfg.LinearAPIKey != "settings-key" {
		t.Fatalf("LinearAPIKey = %q, want %q", cfg.LinearAPIKey, "settings-key")
	}
}

// TestDefaultSettingsAgentDefaults verifies agent defaults are set.
func TestDefaultSettingsAgentDefaults(t *testing.T) {
	settings := DefaultSettings()
	if settings.AgentProvider != DefaultAgentProvider {
		t.Errorf("AgentProvider = %q, want %q", settings.AgentProvider, DefaultAgentProvider)
	}
	if settings.AgentSandbox != DefaultAgentSandbox {
		t.Errorf("AgentSandbox = %q, want %q", settings.AgentSandbox, DefaultAgentSandbox)
	}
	if settings.AgentModel != "" {
		t.Errorf("AgentModel = %q, want empty string", settings.AgentModel)
	}
	if settings.AgentWorkspace != "" {
		t.Errorf("AgentWorkspace = %q, want empty string", settings.AgentWorkspace)
	}
	if settings.Theme != DefaultTheme {
		t.Errorf("Theme = %q, want %q", settings.Theme, DefaultTheme)
	}
	if settings.Density != DefaultDensity {
		t.Errorf("Density = %q, want %q", settings.Density, DefaultDensity)
	}
}

// assertSettingsEqual compares settings values in tests.
func assertSettingsEqual(t *testing.T, got Settings, want Settings) {
	t.Helper()

	if !reflect.DeepEqual(got, want) {
		t.Errorf("Settings mismatch: got %+v, want %+v", got, want)
	}
}
