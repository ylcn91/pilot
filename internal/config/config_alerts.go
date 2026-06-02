package config

import (
	"time"

	"github.com/ylcn91/pilot/internal/alerts"
)

// AlertsConfig holds configuration for the alerting system including
// channels, rules, and default settings.
type AlertsConfig struct {
	Enabled  bool                 `yaml:"enabled"`
	Channels []AlertChannelConfig `yaml:"channels"`
	Rules    []AlertRuleConfig    `yaml:"rules"`
	Defaults AlertDefaultsConfig  `yaml:"defaults"`
}

// AlertChannelConfig configures a destination channel for alerts.
// Supports Slack, Telegram, email, webhooks, and PagerDuty.
// Channel-specific configs use types from the alerts package (single source of truth).
type AlertChannelConfig struct {
	Name       string   `yaml:"name"` // Unique identifier
	Type       string   `yaml:"type"` // "slack", "telegram", "email", "webhook", "pagerduty"
	Enabled    bool     `yaml:"enabled"`
	Severities []string `yaml:"severities"` // Which severities to receive

	// Channel-specific config (types from alerts package)
	Slack     *alerts.SlackChannelConfig     `yaml:"slack,omitempty"`
	Telegram  *alerts.TelegramChannelConfig  `yaml:"telegram,omitempty"`
	Email     *alerts.EmailChannelConfig     `yaml:"email,omitempty"`
	Webhook   *alerts.WebhookChannelConfig   `yaml:"webhook,omitempty"`
	PagerDuty *alerts.PagerDutyChannelConfig `yaml:"pagerduty,omitempty"`
}

// AlertRuleConfig defines a rule that triggers alerts based on specific conditions.
type AlertRuleConfig struct {
	Name        string               `yaml:"name"`
	Type        string               `yaml:"type"` // "task_stuck", "task_failed", etc.
	Enabled     bool                 `yaml:"enabled"`
	Condition   AlertConditionConfig `yaml:"condition"`
	Severity    string               `yaml:"severity"` // "info", "warning", "critical"
	Channels    []string             `yaml:"channels"` // Channel names to send to
	Cooldown    time.Duration        `yaml:"cooldown"` // Min time between alerts
	Description string               `yaml:"description"`
}

// AlertConditionConfig defines the conditions that trigger an alert rule.
type AlertConditionConfig struct {
	ProgressUnchangedFor time.Duration `yaml:"progress_unchanged_for"`
	ConsecutiveFailures  int           `yaml:"consecutive_failures"`
	DailySpendThreshold  float64       `yaml:"daily_spend_threshold"`
	BudgetLimit          float64       `yaml:"budget_limit"`
	UsageSpikePercent    float64       `yaml:"usage_spike_percent"`
	Pattern              string        `yaml:"pattern"`
	FilePattern          string        `yaml:"file_pattern"`
	Paths                []string      `yaml:"paths"`
}

// AlertDefaultsConfig contains default settings applied to all alert rules.
type AlertDefaultsConfig struct {
	Cooldown           time.Duration `yaml:"cooldown"`
	DefaultSeverity    string        `yaml:"default_severity"`
	SuppressDuplicates bool          `yaml:"suppress_duplicates"`
}

// defaultAlertRules returns the default alert rules
func defaultAlertRules() []AlertRuleConfig {
	return []AlertRuleConfig{
		{
			Name:    "task_stuck",
			Type:    "task_stuck",
			Enabled: true,
			Condition: AlertConditionConfig{
				ProgressUnchangedFor: 10 * time.Minute,
			},
			Severity:    "warning",
			Channels:    []string{},
			Cooldown:    15 * time.Minute,
			Description: "Alert when a task has no progress for 10 minutes",
		},
		{
			Name:        "task_failed",
			Type:        "task_failed",
			Enabled:     true,
			Condition:   AlertConditionConfig{},
			Severity:    "warning",
			Channels:    []string{},
			Cooldown:    0,
			Description: "Alert when a task fails",
		},
		{
			Name:    "consecutive_failures",
			Type:    "consecutive_failures",
			Enabled: true,
			Condition: AlertConditionConfig{
				ConsecutiveFailures: 3,
			},
			Severity:    "critical",
			Channels:    []string{},
			Cooldown:    30 * time.Minute,
			Description: "Alert when 3 or more consecutive tasks fail",
		},
		{
			Name:    "daily_spend",
			Type:    "daily_spend_exceeded",
			Enabled: false,
			Condition: AlertConditionConfig{
				DailySpendThreshold: 50.0,
			},
			Severity:    "warning",
			Channels:    []string{},
			Cooldown:    1 * time.Hour,
			Description: "Alert when daily spend exceeds threshold",
		},
		{
			Name:    "budget_depleted",
			Type:    "budget_depleted",
			Enabled: false,
			Condition: AlertConditionConfig{
				BudgetLimit: 500.0,
			},
			Severity:    "critical",
			Channels:    []string{},
			Cooldown:    4 * time.Hour,
			Description: "Alert when budget limit is exceeded",
		},
	}
}
