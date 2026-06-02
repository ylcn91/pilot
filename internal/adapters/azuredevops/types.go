package azuredevops

import "time"

// Config holds Azure DevOps adapter configuration
type Config struct {
	Enabled           bool                     `yaml:"enabled"`
	PAT               string                   `yaml:"pat"`             // Personal Access Token
	Organization      string                   `yaml:"organization"`    // Azure DevOps org name
	Project           string                   `yaml:"project"`         // Project name
	Repository        string                   `yaml:"repository"`      // Repository name (optional, defaults to project)
	BaseURL           string                   `yaml:"base_url"`        // Default: https://dev.azure.com
	WebhookSecret     string                   `yaml:"webhook_secret"`  // For basic auth on webhook endpoint
	PilotTag          string                   `yaml:"pilot_tag"`       // Tag to watch (Azure uses tags, not labels)
	WorkItemTypes     []string                 `yaml:"work_item_types"` // e.g., ["Bug", "Task", "User Story"]
	Polling           *PollingConfig           `yaml:"polling"`
	StaleLabelCleanup *StaleLabelCleanupConfig `yaml:"stale_label_cleanup"`
}

// PollingConfig holds Azure DevOps polling settings
type PollingConfig struct {
	Enabled  bool          `yaml:"enabled"`
	Interval time.Duration `yaml:"interval"` // Poll interval (default 30s)
}

// StaleLabelCleanupConfig holds settings for auto-cleanup of stale pilot-in-progress tags
type StaleLabelCleanupConfig struct {
	Enabled   bool          `yaml:"enabled"`
	Interval  time.Duration `yaml:"interval"`  // How often to check for stale tags (default: 30m)
	Threshold time.Duration `yaml:"threshold"` // How long before a tag is considered stale (default: 1h)
}

// DefaultConfig returns default Azure DevOps configuration
func DefaultConfig() *Config {
	return &Config{
		Enabled:  false,
		BaseURL:  "https://dev.azure.com",
		PilotTag: "pilot",
		WorkItemTypes: []string{
			"Bug",
			"Task",
			"User Story",
		},
		Polling: &PollingConfig{
			Enabled:  false,
			Interval: 30 * time.Second,
		},
		StaleLabelCleanup: &StaleLabelCleanupConfig{
			Enabled:   true,
			Interval:  30 * time.Minute,
			Threshold: 1 * time.Hour,
		},
	}
}

// ListWorkItemsOptions holds options for listing work items via WIQL
type ListWorkItemsOptions struct {
	Tags          []string
	States        []string // New, Active, Resolved, Closed
	WorkItemTypes []string
	UpdatedAfter  time.Time
}
