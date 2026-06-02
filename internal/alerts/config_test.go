package alerts

import (
	"testing"
	"time"
)

func TestFromConfigAlerts(t *testing.T) {
	channels := []ChannelConfigInput{
		{
			Name:       "slack-channel",
			Type:       "slack",
			Enabled:    true,
			Severities: []string{"critical"},
			Slack:      &SlackChannelConfig{Channel: "#alerts"},
		},
		{
			Name:    "webhook-channel",
			Type:    "webhook",
			Enabled: true,
			Webhook: &WebhookChannelConfig{URL: "https://example.com"},
		},
	}

	rules := []RuleConfigInput{
		{
			Name:     "task-failed",
			Type:     "task_failed",
			Enabled:  true,
			Severity: "warning",
			Channels: []string{"slack-channel"},
			Cooldown: 5 * time.Minute,
		},
		{
			Name:    "consecutive",
			Type:    "consecutive_failures",
			Enabled: true,
			Condition: ConditionConfigInput{
				ConsecutiveFailures: 3,
			},
			Severity: "critical",
		},
	}

	defaults := DefaultsConfigInput{
		Cooldown:           10 * time.Minute,
		DefaultSeverity:    "warning",
		SuppressDuplicates: true,
	}

	config := FromConfigAlerts(true, channels, rules, defaults)

	// Verify enabled
	if !config.Enabled {
		t.Error("expected config to be enabled")
	}

	// Verify channels
	if len(config.Channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(config.Channels))
	}

	// Find and verify slack channel
	var slackCh *ChannelConfig
	for i := range config.Channels {
		if config.Channels[i].Name == "slack-channel" {
			slackCh = &config.Channels[i]
			break
		}
	}
	if slackCh == nil {
		t.Fatal("slack-channel not found")
	}
	if slackCh.Slack == nil || slackCh.Slack.Channel != "#alerts" {
		t.Error("slack channel config incorrect")
	}

	// Verify rules
	if len(config.Rules) != 2 {
		t.Errorf("expected 2 rules, got %d", len(config.Rules))
	}

	// Find and verify task-failed rule
	var taskFailedRule *AlertRule
	for i := range config.Rules {
		if config.Rules[i].Name == "task-failed" {
			taskFailedRule = &config.Rules[i]
			break
		}
	}
	if taskFailedRule == nil {
		t.Fatal("task-failed rule not found")
	}
	if taskFailedRule.Type != AlertTypeTaskFailed {
		t.Errorf("expected type TaskFailed, got %s", taskFailedRule.Type)
	}

	// Verify defaults
	if config.Defaults.Cooldown != 10*time.Minute {
		t.Errorf("expected default cooldown 10m, got %v", config.Defaults.Cooldown)
	}
	if config.Defaults.DefaultSeverity != SeverityWarning {
		t.Errorf("expected default severity Warning, got %s", config.Defaults.DefaultSeverity)
	}
	if !config.Defaults.SuppressDuplicates {
		t.Error("expected SuppressDuplicates to be true")
	}
}

func TestFromConfigAlerts_Empty(t *testing.T) {
	config := FromConfigAlerts(false, nil, nil, DefaultsConfigInput{})

	if config.Enabled {
		t.Error("expected config to be disabled")
	}
	if len(config.Channels) != 0 {
		t.Errorf("expected 0 channels, got %d", len(config.Channels))
	}
	if len(config.Rules) != 0 {
		t.Errorf("expected 0 rules, got %d", len(config.Rules))
	}
}
