package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/ylcn91/pilot/internal/approval"
	"github.com/ylcn91/pilot/internal/autopilot"
	"github.com/ylcn91/pilot/internal/budget"
	"github.com/ylcn91/pilot/internal/executor"
	"github.com/ylcn91/pilot/internal/gateway"
	"github.com/ylcn91/pilot/internal/logging"
	"github.com/ylcn91/pilot/internal/quality"
	"github.com/ylcn91/pilot/internal/tunnel"
	"github.com/ylcn91/pilot/internal/webhooks"
)

// Config represents the main Pilot configuration loaded from YAML.
// It includes settings for the gateway, adapters, orchestrator, memory, projects, and more.
// Use Load to read from a file or DefaultConfig for sensible defaults.
type Config struct {
	Version        string                  `yaml:"version"`
	Gateway        *gateway.Config         `yaml:"gateway"`
	Auth           *gateway.AuthConfig     `yaml:"auth"`
	Adapters       *AdaptersConfig         `yaml:"adapters"`
	Orchestrator   *OrchestratorConfig     `yaml:"orchestrator"`
	Executor       *executor.BackendConfig `yaml:"executor"`
	Memory         *MemoryConfig           `yaml:"memory"`
	Projects       []*ProjectConfig        `yaml:"projects"`
	DefaultProject string                  `yaml:"default_project"`
	Dashboard      *DashboardConfig        `yaml:"dashboard"`
	Alerts         *AlertsConfig           `yaml:"alerts"`
	Budget         *budget.Config          `yaml:"budget"`
	Logging        *logging.Config         `yaml:"logging"`
	Approval       *approval.Config        `yaml:"approval"`
	Quality        *quality.Config         `yaml:"quality"`
	Tunnel         *tunnel.Config          `yaml:"tunnel"`
	Webhooks       *webhooks.Config        `yaml:"webhooks"`
	TeamID         string                  `yaml:"team_id"` // Optional team ID for scoping execution
	Team           *TeamConfig             `yaml:"team"`
	Architect      *ArchitectConfig        `yaml:"architect,omitempty"`  // Proactive Architect pipeline (SCAN/PROPOSE/EMIT)
	Guardrails     *GuardrailsConfig       `yaml:"guardrails,omitempty"` // Per-PR architectural guardrails (report-only by default)
}

// DefaultConfig returns a new Config instance with sensible default values.
// The gateway binds to localhost:9090, recording is enabled, and common
// alert rules are pre-configured but disabled.
func DefaultConfig() *Config {
	homeDir, _ := os.UserHomeDir()
	return &Config{
		Version: "1.0",
		Gateway: &gateway.Config{
			Host:         "127.0.0.1",
			Port:         9090,
			CodexRuntime: gateway.DefaultCodexRuntimeConfig(),
		},
		Auth: &gateway.AuthConfig{
			Type: gateway.AuthTypeClaudeCode,
		},
		Adapters: defaultAdaptersConfig(),
		Orchestrator: &OrchestratorConfig{
			Model:         "claude-sonnet-4-6",
			MaxConcurrent: 2,
			DailyBrief: &DailyBriefConfig{
				Enabled:  false,
				Schedule: "0 9 * * 1-5", // 9 AM weekdays
				Timezone: "America/New_York",
				Channels: []BriefChannelConfig{},
				Content: BriefContentConfig{
					IncludeMetrics:     true,
					IncludeErrors:      true,
					MaxItemsPerSection: 10,
				},
				Filters: BriefFilterConfig{
					Projects: []string{},
				},
			},
			Execution: DefaultExecutionConfig(),
			Autopilot: autopilot.DefaultConfig(),
		},
		Executor: executor.DefaultBackendConfig(),
		Memory: &MemoryConfig{
			Path:         filepath.Join(homeDir, ".pilot", "data"),
			CrossProject: true,
			Learning:     DefaultLearningConfig(),
		},
		Projects: []*ProjectConfig{},
		Dashboard: &DashboardConfig{
			RefreshInterval: 1000,
			ShowLogs:        true,
		},
		Alerts: &AlertsConfig{
			Enabled:  false,
			Channels: []AlertChannelConfig{},
			Rules:    defaultAlertRules(),
			Defaults: AlertDefaultsConfig{
				Cooldown:           5 * time.Minute,
				DefaultSeverity:    "warning",
				SuppressDuplicates: true,
			},
		},
		Budget:   budget.DefaultConfig(),
		Logging:  logging.DefaultConfig(),
		Approval: approval.DefaultConfig(),
		Quality:  quality.DefaultConfig(),
		Tunnel:   tunnel.DefaultConfig(),
		Webhooks: webhooks.DefaultConfig(),
	}
}

func MemoryPathOrDefault(config *Config) string {
	if config != nil && config.Memory != nil {
		if strings.TrimSpace(config.Memory.Path) != "" {
			return config.Memory.Path
		}
	}
	defaultConfig := DefaultConfig()
	if defaultConfig.Memory == nil {
		return ""
	}
	return defaultConfig.Memory.Path
}

// Load reads and parses configuration from a YAML file at the given path.
// Environment variables in the file are expanded using os.ExpandEnv syntax.
// If the file does not exist, default configuration is returned.
// Returns an error if the file cannot be read or parsed.
func Load(path string) (*Config, error) {
	config := DefaultConfig()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return config, nil // Return defaults if no config file
		}
		return nil, fmt.Errorf("failed to read config: %w", err)
	}

	// Expand environment variables
	expanded := os.ExpandEnv(string(data))

	if err := yaml.Unmarshal([]byte(expanded), config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Expand paths
	if config.Memory != nil {
		config.Memory.Path = expandPath(config.Memory.Path)
	}
	for _, project := range config.Projects {
		project.Path = expandPath(project.Path)
	}

	// Log deprecation warnings
	config.CheckDeprecations()

	// Validate configuration (GH-914)
	if err := config.Validate(); err != nil {
		return nil, err
	}

	return config, nil
}

// Save writes the configuration to a YAML file at the given path.
// It creates the parent directory if it does not exist.
//
// TASK-290: file mode is 0600 and parent dir is 0700 because the config
// contains GitHub PAT, Linear API key, Slack bot token, and (optionally)
// Anthropic API key — none of which should be world- or group-readable.
// If a config already exists on disk with looser perms, this Save call will
// tighten them on the next write (existing 0644 files are rewritten 0600).
func Save(config *Config, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// If the directory already existed with looser perms (e.g. an older
	// install left ~/.pilot at 0755), tighten it. Ignore errors — best effort.
	_ = os.Chmod(dir, 0700)

	data, err := yaml.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	// os.WriteFile does NOT change permissions of an existing file — it only
	// applies the mode on create. Explicit Chmod ensures we tighten perms even
	// when a previous version of Pilot left the file at 0644 on disk.
	if err := os.Chmod(path, 0600); err != nil {
		return fmt.Errorf("failed to chmod config to 0600: %w", err)
	}

	return nil
}

// DefaultConfigPath returns the default configuration file path (~/.pilot/config.yaml).
func DefaultConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".pilot", "config.yaml")
}

// Reload re-reads configuration from the given path and updates the receiver in-place.
// This is useful for hot-reloading config without process restart (e.g., on SIGHUP).
// GH-879: Added to support config reload after hot upgrade.
func (c *Config) Reload(path string) error {
	newCfg, err := Load(path)
	if err != nil {
		return fmt.Errorf("failed to reload config: %w", err)
	}

	// Update all fields in-place
	*c = *newCfg

	return nil
}

// expandPath expands ~ to home directory
func expandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		homeDir, _ := os.UserHomeDir()
		return filepath.Join(homeDir, path[1:])
	}
	return path
}
