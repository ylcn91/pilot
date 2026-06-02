package main

import (
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/github"
	"github.com/ylcn91/pilot/internal/alerts"
	"github.com/ylcn91/pilot/internal/config"
)

// =============================================================================
// GH-2134: getAlertsConfig wiring tests
// =============================================================================

func TestGetAlertsConfig_NilAlerts(t *testing.T) {
	cfg := &config.Config{}
	result := getAlertsConfig(cfg)
	if result != nil {
		t.Error("expected nil AlertConfig when cfg.Alerts is nil")
	}
}

func TestGetAlertsConfig_DisabledAlerts(t *testing.T) {
	cfg := &config.Config{
		Alerts: &config.AlertsConfig{
			Enabled: false,
			Defaults: config.AlertDefaultsConfig{
				Cooldown:        5 * time.Minute,
				DefaultSeverity: "warning",
			},
		},
	}
	result := getAlertsConfig(cfg)
	if result == nil {
		t.Fatal("expected non-nil AlertConfig even when disabled (FromConfigAlerts decides)")
	}
}

func TestGetAlertsConfig_WithChannelsAndRules(t *testing.T) {
	cfg := &config.Config{
		Alerts: &config.AlertsConfig{
			Enabled: true,
			Channels: []config.AlertChannelConfig{
				{
					Name:       "slack-alerts",
					Type:       "slack",
					Enabled:    true,
					Severities: []string{"critical", "error"},
					Slack: &alerts.SlackChannelConfig{
						Channel: "#alerts",
					},
				},
				{
					Name:    "webhook-alerts",
					Type:    "webhook",
					Enabled: true,
					Webhook: &alerts.WebhookChannelConfig{
						URL: "https://hooks.test/alert",
					},
				},
			},
			Rules: []config.AlertRuleConfig{
				{
					Name:     "task-failure",
					Type:     "task_failure",
					Enabled:  true,
					Severity: "error",
					Channels: []string{"slack-alerts"},
					Cooldown: 10 * time.Minute,
					Condition: config.AlertConditionConfig{
						ConsecutiveFailures: 3,
					},
				},
			},
			Defaults: config.AlertDefaultsConfig{
				Cooldown:           5 * time.Minute,
				DefaultSeverity:    "warning",
				SuppressDuplicates: true,
			},
		},
	}

	result := getAlertsConfig(cfg)
	if result == nil {
		t.Fatal("expected non-nil AlertConfig")
	}
	if !result.Enabled {
		t.Error("expected AlertConfig.Enabled = true")
	}
}

// =============================================================================
// GH-2134: resolveOwnerRepo tests
// =============================================================================

func TestResolveOwnerRepo_FromConfig(t *testing.T) {
	cfg := &config.Config{
		Adapters: &config.AdaptersConfig{
			GitHub: &github.Config{
				Repo: "myorg/myrepo",
			},
		},
	}

	owner, repo, err := resolveOwnerRepo(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if owner != "myorg" {
		t.Errorf("owner = %q, want %q", owner, "myorg")
	}
	if repo != "myrepo" {
		t.Errorf("repo = %q, want %q", repo, "myrepo")
	}
}

func TestResolveOwnerRepo_EmptyConfigFallsBackToGit(t *testing.T) {
	cfg := &config.Config{
		Adapters: &config.AdaptersConfig{},
	}

	// This will try git remote — may succeed or fail depending on environment.
	// We just verify it doesn't panic with nil GitHub config.
	_, _, _ = resolveOwnerRepo(cfg)
}

func TestResolveOwnerRepo_SingleSegmentRepo(t *testing.T) {
	// Single-segment repo string doesn't split into owner/repo
	cfg := &config.Config{
		Adapters: &config.AdaptersConfig{
			GitHub: &github.Config{
				Repo: "just-a-name",
			},
		},
	}

	// Falls back to git remote since split won't produce 2 parts
	_, _, _ = resolveOwnerRepo(cfg)
}
