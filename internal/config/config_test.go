package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/gateway"
)

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("DefaultConfig returned nil")
	}

	t.Run("Version", func(t *testing.T) {
		if config.Version != "1.0" {
			t.Errorf("Version = %q, want %q", config.Version, "1.0")
		}
	})

	t.Run("Gateway", func(t *testing.T) {
		if config.Gateway == nil {
			t.Fatal("Gateway config is nil")
		}
		if config.Gateway.Host != "127.0.0.1" {
			t.Errorf("Gateway.Host = %q, want %q", config.Gateway.Host, "127.0.0.1")
		}
		if config.Gateway.Port != 9090 {
			t.Errorf("Gateway.Port = %d, want %d", config.Gateway.Port, 9090)
		}
		if config.Gateway.CodexRuntime == nil {
			t.Fatal("Gateway.CodexRuntime is nil")
		}
		if config.Gateway.CodexRuntime.Command != "codex" {
			t.Errorf("Gateway.CodexRuntime.Command = %q, want %q", config.Gateway.CodexRuntime.Command, "codex")
		}
		if config.Gateway.CodexRuntime.Sandbox != "read-only" {
			t.Errorf("Gateway.CodexRuntime.Sandbox = %q, want %q", config.Gateway.CodexRuntime.Sandbox, "read-only")
		}
	})

	t.Run("Auth", func(t *testing.T) {
		if config.Auth == nil {
			t.Fatal("Auth config is nil")
		}
		if config.Auth.Type != gateway.AuthTypeClaudeCode {
			t.Errorf("Auth.Type = %q, want %q", config.Auth.Type, gateway.AuthTypeClaudeCode)
		}
	})

	t.Run("Adapters", func(t *testing.T) {
		if config.Adapters == nil {
			t.Fatal("Adapters config is nil")
		}
		if config.Adapters.Linear == nil {
			t.Error("Adapters.Linear is nil")
		}
		if config.Adapters.Slack == nil {
			t.Error("Adapters.Slack is nil")
		}
		if config.Adapters.Telegram == nil {
			t.Error("Adapters.Telegram is nil")
		}
		if config.Adapters.GitHub == nil {
			t.Error("Adapters.GitHub is nil")
		}
		if config.Adapters.Jira == nil {
			t.Error("Adapters.Jira is nil")
		}
	})

	t.Run("Orchestrator", func(t *testing.T) {
		if config.Orchestrator == nil {
			t.Fatal("Orchestrator config is nil")
		}
		if config.Orchestrator.Model != "claude-sonnet-4-6" {
			t.Errorf("Orchestrator.Model = %q, want %q", config.Orchestrator.Model, "claude-sonnet-4-6")
		}
		if config.Orchestrator.MaxConcurrent != 2 {
			t.Errorf("Orchestrator.MaxConcurrent = %d, want %d", config.Orchestrator.MaxConcurrent, 2)
		}
		if config.Orchestrator.DailyBrief == nil {
			t.Fatal("Orchestrator.DailyBrief is nil")
		}
		if config.Orchestrator.DailyBrief.Enabled != false {
			t.Error("DailyBrief.Enabled should be false by default")
		}
		if config.Orchestrator.DailyBrief.Schedule != "0 9 * * 1-5" {
			t.Errorf("DailyBrief.Schedule = %q, want %q", config.Orchestrator.DailyBrief.Schedule, "0 9 * * 1-5")
		}
	})

	t.Run("Execution", func(t *testing.T) {
		if config.Orchestrator.Execution == nil {
			t.Fatal("Orchestrator.Execution is nil")
		}
		exec := config.Orchestrator.Execution
		if exec.Mode != "auto" {
			t.Errorf("Execution.Mode = %q, want %q", exec.Mode, "auto")
		}
		if exec.WaitForMerge != true {
			t.Error("Execution.WaitForMerge should be true by default")
		}
		if exec.PollInterval != 30*time.Second {
			t.Errorf("Execution.PollInterval = %v, want %v", exec.PollInterval, 30*time.Second)
		}
		if exec.PRTimeout != 1*time.Hour {
			t.Errorf("Execution.PRTimeout = %v, want %v", exec.PRTimeout, 1*time.Hour)
		}
	})

	t.Run("Memory", func(t *testing.T) {
		if config.Memory == nil {
			t.Fatal("Memory config is nil")
		}
		homeDir, _ := os.UserHomeDir()
		expectedPath := filepath.Join(homeDir, ".pilot", "data")
		if config.Memory.Path != expectedPath {
			t.Errorf("Memory.Path = %q, want %q", config.Memory.Path, expectedPath)
		}
		if config.Memory.CrossProject != true {
			t.Error("Memory.CrossProject should be true by default")
		}
		if config.Memory.SyncToFiles != false {
			t.Error("Memory.SyncToFiles should be false by default")
		}
	})

	t.Run("Dashboard", func(t *testing.T) {
		if config.Dashboard == nil {
			t.Fatal("Dashboard config is nil")
		}
		if config.Dashboard.RefreshInterval != 1000 {
			t.Errorf("Dashboard.RefreshInterval = %d, want %d", config.Dashboard.RefreshInterval, 1000)
		}
		if config.Dashboard.ShowLogs != true {
			t.Error("Dashboard.ShowLogs should be true by default")
		}
	})

	t.Run("Alerts", func(t *testing.T) {
		if config.Alerts == nil {
			t.Fatal("Alerts config is nil")
		}
		if config.Alerts.Enabled != false {
			t.Error("Alerts.Enabled should be false by default")
		}
		if config.Alerts.Defaults.Cooldown != 5*time.Minute {
			t.Errorf("Alerts.Defaults.Cooldown = %v, want %v", config.Alerts.Defaults.Cooldown, 5*time.Minute)
		}
		if config.Alerts.Defaults.DefaultSeverity != "warning" {
			t.Errorf("Alerts.Defaults.DefaultSeverity = %q, want %q", config.Alerts.Defaults.DefaultSeverity, "warning")
		}
		if len(config.Alerts.Rules) == 0 {
			t.Error("Alerts.Rules should have default rules")
		}
	})

	t.Run("Budget", func(t *testing.T) {
		if config.Budget == nil {
			t.Error("Budget config is nil")
		}
	})

	t.Run("Logging", func(t *testing.T) {
		if config.Logging == nil {
			t.Error("Logging config is nil")
		}
	})

	t.Run("Approval", func(t *testing.T) {
		if config.Approval == nil {
			t.Error("Approval config is nil")
		}
	})

	t.Run("Quality", func(t *testing.T) {
		if config.Quality == nil {
			t.Error("Quality config is nil")
		}
	})

	t.Run("Tunnel", func(t *testing.T) {
		if config.Tunnel == nil {
			t.Error("Tunnel config is nil")
		}
	})

	t.Run("Projects", func(t *testing.T) {
		if config.Projects == nil {
			t.Fatal("Projects is nil")
		}
		if len(config.Projects) != 0 {
			t.Errorf("Projects length = %d, want 0", len(config.Projects))
		}
	})
}

func TestDefaultConfigPath(t *testing.T) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("Failed to get home directory: %v", err)
	}

	expected := filepath.Join(homeDir, ".pilot", "config.yaml")
	result := DefaultConfigPath()

	if result != expected {
		t.Errorf("DefaultConfigPath() = %q, want %q", result, expected)
	}
}

func TestDefaultAlertRules(t *testing.T) {
	rules := defaultAlertRules()

	if len(rules) == 0 {
		t.Fatal("defaultAlertRules() returned empty slice")
	}

	// Verify expected rules exist
	ruleNames := make(map[string]bool)
	for _, rule := range rules {
		ruleNames[rule.Name] = true
	}

	expectedRules := []string{"task_stuck", "task_failed", "consecutive_failures", "daily_spend", "budget_depleted"}
	for _, name := range expectedRules {
		if !ruleNames[name] {
			t.Errorf("Expected rule %q not found in default rules", name)
		}
	}

	// Verify task_stuck rule configuration
	for _, rule := range rules {
		if rule.Name == "task_stuck" {
			if rule.Condition.ProgressUnchangedFor != 10*time.Minute {
				t.Errorf("task_stuck ProgressUnchangedFor = %v, want %v", rule.Condition.ProgressUnchangedFor, 10*time.Minute)
			}
			if rule.Severity != "warning" {
				t.Errorf("task_stuck Severity = %q, want %q", rule.Severity, "warning")
			}
			if !rule.Enabled {
				t.Error("task_stuck should be enabled by default")
			}
		}
		if rule.Name == "consecutive_failures" {
			if rule.Condition.ConsecutiveFailures != 3 {
				t.Errorf("consecutive_failures ConsecutiveFailures = %d, want %d", rule.Condition.ConsecutiveFailures, 3)
			}
			if rule.Severity != "critical" {
				t.Errorf("consecutive_failures Severity = %q, want %q", rule.Severity, "critical")
			}
		}
	}
}
