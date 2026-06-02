package alerts

import (
	"testing"
	"time"
)

func TestAlert_Struct(t *testing.T) {
	now := time.Now()
	acked := now.Add(1 * time.Minute)
	resolved := now.Add(5 * time.Minute)

	alert := Alert{
		ID:          "test-id",
		Type:        AlertTypeTaskFailed,
		Severity:    SeverityCritical,
		Title:       "Test Title",
		Message:     "Test Message",
		Source:      "task:TASK-123",
		ProjectPath: "/my/project",
		Metadata: map[string]string{
			"key": "value",
		},
		CreatedAt:  now,
		AckedAt:    &acked,
		ResolvedAt: &resolved,
	}

	if alert.ID != "test-id" {
		t.Errorf("expected ID 'test-id', got '%s'", alert.ID)
	}
	if alert.Type != AlertTypeTaskFailed {
		t.Errorf("expected Type TaskFailed, got %s", alert.Type)
	}
	if alert.Severity != SeverityCritical {
		t.Errorf("expected Severity Critical, got %s", alert.Severity)
	}
	if alert.Metadata["key"] != "value" {
		t.Error("expected metadata to contain key-value pair")
	}
	if alert.AckedAt == nil || !alert.AckedAt.Equal(acked) {
		t.Error("expected AckedAt to be set correctly")
	}
	if alert.ResolvedAt == nil || !alert.ResolvedAt.Equal(resolved) {
		t.Error("expected ResolvedAt to be set correctly")
	}
}

func TestDeliveryResult_Struct(t *testing.T) {
	now := time.Now()
	result := DeliveryResult{
		ChannelName: "slack-channel",
		Success:     true,
		Error:       nil,
		SentAt:      now,
		MessageID:   "msg-123",
	}

	if result.ChannelName != "slack-channel" {
		t.Errorf("expected ChannelName 'slack-channel', got '%s'", result.ChannelName)
	}
	if !result.Success {
		t.Error("expected Success to be true")
	}
	if result.Error != nil {
		t.Error("expected Error to be nil")
	}
	if result.MessageID != "msg-123" {
		t.Errorf("expected MessageID 'msg-123', got '%s'", result.MessageID)
	}
}

func TestAlertHistory_Struct(t *testing.T) {
	now := time.Now()
	history := AlertHistory{
		AlertID:     "alert-123",
		RuleName:    "task_failed",
		Source:      "task:TASK-456",
		FiredAt:     now,
		DeliveredTo: []string{"slack", "telegram"},
	}

	if history.AlertID != "alert-123" {
		t.Errorf("expected AlertID 'alert-123', got '%s'", history.AlertID)
	}
	if history.RuleName != "task_failed" {
		t.Errorf("expected RuleName 'task_failed', got '%s'", history.RuleName)
	}
	if len(history.DeliveredTo) != 2 {
		t.Errorf("expected 2 delivery targets, got %d", len(history.DeliveredTo))
	}
}

func TestChannelConfig_AllTypes(t *testing.T) {
	tests := []struct {
		name       string
		config     ChannelConfig
		expectType string
	}{
		{
			name: "slack channel",
			config: ChannelConfig{
				Name:    "my-slack",
				Type:    "slack",
				Enabled: true,
				Slack: &SlackChannelConfig{
					Channel: "#alerts",
				},
			},
			expectType: "slack",
		},
		{
			name: "telegram channel",
			config: ChannelConfig{
				Name:    "my-telegram",
				Type:    "telegram",
				Enabled: true,
				Telegram: &TelegramChannelConfig{
					ChatID: 123456789,
				},
			},
			expectType: "telegram",
		},
		{
			name: "email channel",
			config: ChannelConfig{
				Name:    "my-email",
				Type:    "email",
				Enabled: true,
				Email: &EmailChannelConfig{
					To:      []string{"test@example.com"},
					Subject: "Alert: {{title}}",
				},
			},
			expectType: "email",
		},
		{
			name: "webhook channel",
			config: ChannelConfig{
				Name:    "my-webhook",
				Type:    "webhook",
				Enabled: true,
				Webhook: &WebhookChannelConfig{
					URL:    "https://example.com/webhook",
					Method: "POST",
					Headers: map[string]string{
						"Authorization": "Bearer token",
					},
					Secret: "my-secret",
				},
			},
			expectType: "webhook",
		},
		{
			name: "pagerduty channel",
			config: ChannelConfig{
				Name:    "my-pagerduty",
				Type:    "pagerduty",
				Enabled: true,
				PagerDuty: &PagerDutyChannelConfig{
					RoutingKey: "routing-key-123",
					ServiceID:  "service-456",
				},
			},
			expectType: "pagerduty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config.Type != tt.expectType {
				t.Errorf("expected type %s, got %s", tt.expectType, tt.config.Type)
			}
			if !tt.config.Enabled {
				t.Error("expected channel to be enabled")
			}
		})
	}
}

func TestRuleCondition_AllFields(t *testing.T) {
	condition := RuleCondition{
		ProgressUnchangedFor: 15 * time.Minute,
		ConsecutiveFailures:  5,
		DailySpendThreshold:  100.0,
		BudgetLimit:          1000.0,
		UsageSpikePercent:    200.0,
		Pattern:              "error.*fatal",
		FilePattern:          "*.secret",
		Paths:                []string{"/etc/passwd", "/etc/shadow"},
	}

	if condition.ProgressUnchangedFor != 15*time.Minute {
		t.Errorf("expected ProgressUnchangedFor 15m, got %v", condition.ProgressUnchangedFor)
	}
	if condition.ConsecutiveFailures != 5 {
		t.Errorf("expected ConsecutiveFailures 5, got %d", condition.ConsecutiveFailures)
	}
	if condition.DailySpendThreshold != 100.0 {
		t.Errorf("expected DailySpendThreshold 100.0, got %f", condition.DailySpendThreshold)
	}
	if condition.BudgetLimit != 1000.0 {
		t.Errorf("expected BudgetLimit 1000.0, got %f", condition.BudgetLimit)
	}
	if condition.UsageSpikePercent != 200.0 {
		t.Errorf("expected UsageSpikePercent 200.0, got %f", condition.UsageSpikePercent)
	}
	if condition.Pattern != "error.*fatal" {
		t.Errorf("expected Pattern 'error.*fatal', got '%s'", condition.Pattern)
	}
	if len(condition.Paths) != 2 {
		t.Errorf("expected 2 paths, got %d", len(condition.Paths))
	}
}

func TestAlertRule_AllFields(t *testing.T) {
	rule := AlertRule{
		Name:        "my-rule",
		Type:        AlertTypeTaskFailed,
		Enabled:     true,
		Condition:   RuleCondition{ConsecutiveFailures: 3},
		Severity:    SeverityCritical,
		Channels:    []string{"slack", "telegram"},
		Cooldown:    10 * time.Minute,
		Labels:      map[string]string{"env": "prod"},
		Description: "Test rule description",
	}

	if rule.Name != "my-rule" {
		t.Errorf("expected Name 'my-rule', got '%s'", rule.Name)
	}
	if !rule.Enabled {
		t.Error("expected rule to be enabled")
	}
	if len(rule.Channels) != 2 {
		t.Errorf("expected 2 channels, got %d", len(rule.Channels))
	}
	if rule.Labels["env"] != "prod" {
		t.Error("expected label env=prod")
	}
}
