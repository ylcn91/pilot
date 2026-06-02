package alerts

import (
	"context"
	"testing"
	"time"
)

func TestEngine_Escalation_DefaultThreshold(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{
				Name:       "pagerduty",
				Type:       "pagerduty",
				Enabled:    true,
				Severities: []Severity{SeverityCritical},
			},
		},
		Rules: []AlertRule{
			{
				Name:    "escalation",
				Type:    AlertTypeEscalation,
				Enabled: true,
				Condition: RuleCondition{
					EscalationRetries: 0, // Zero - should use default of 3
				},
				Severity:    SeverityCritical,
				Channels:    []string{"pagerduty"},
				Cooldown:    0,
				Description: "Escalate after repeated failures",
			},
		},
	}

	mockCh := newMockChannel("pagerduty", "pagerduty")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = engine.Start(ctx)

	source := "issue:GH-DEFAULT"

	// Send 3 failures (default threshold)
	for i := 1; i <= 3; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    "TASK-" + string(rune('0'+i)),
			Project:   "/test/project",
			Error:     "test error",
			Metadata:  map[string]string{"source": source},
			Timestamp: time.Now(),
		})
	}

	waitForAlerts(t, mockCh, 1, 2*time.Second)

	alerts := mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Errorf("expected 1 escalation alert with default threshold, got %d", len(alerts))
	}
}

func TestEngine_Escalation_FallbackToTaskIDAsSource(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{
				Name:       "pagerduty",
				Type:       "pagerduty",
				Enabled:    true,
				Severities: []Severity{SeverityCritical},
			},
		},
		Rules: []AlertRule{
			{
				Name:    "escalation",
				Type:    AlertTypeEscalation,
				Enabled: true,
				Condition: RuleCondition{
					EscalationRetries: 3,
				},
				Severity:    SeverityCritical,
				Channels:    []string{"pagerduty"},
				Cooldown:    0,
				Description: "Escalate after repeated failures",
			},
		},
	}

	mockCh := newMockChannel("pagerduty", "pagerduty")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = engine.Start(ctx)

	// Send 3 failures with same TaskID but no explicit source in metadata
	taskID := "TASK-SAME"
	for i := 1; i <= 3; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    taskID,
			Project:   "/test/project",
			Error:     "test error",
			Timestamp: time.Now(),
		})
	}

	waitForAlerts(t, mockCh, 1, 2*time.Second)

	alerts := mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Errorf("expected 1 escalation alert using TaskID as source, got %d", len(alerts))
		return
	}

	if alerts[0].Source != taskID {
		t.Errorf("expected source %s (TaskID fallback), got %s", taskID, alerts[0].Source)
	}
}

func TestEngine_CreateEscalationAlert(t *testing.T) {
	config := &AlertConfig{Enabled: true}
	engine := NewEngine(config)

	rule := AlertRule{
		Type:        AlertTypeEscalation,
		Severity:    SeverityCritical,
		Description: "Escalation alert",
	}

	event := Event{
		TaskID:  "TASK-123",
		Project: "/my/project",
		Error:   "original error message",
		Metadata: map[string]string{
			"custom": "value",
		},
	}

	alert := engine.createEscalationAlert(rule, event, "issue:GH-123", 3)

	if alert.ID == "" {
		t.Error("expected non-empty alert ID")
	}
	if alert.Type != AlertTypeEscalation {
		t.Errorf("expected type %s, got %s", AlertTypeEscalation, alert.Type)
	}
	if alert.Severity != SeverityCritical {
		t.Errorf("expected severity %s, got %s", SeverityCritical, alert.Severity)
	}
	if alert.Source != "issue:GH-123" {
		t.Errorf("expected source 'issue:GH-123', got '%s'", alert.Source)
	}
	if alert.Metadata["retry_count"] != "3" {
		t.Errorf("expected retry_count=3, got %s", alert.Metadata["retry_count"])
	}
	if alert.Metadata["escalation_source"] != "issue:GH-123" {
		t.Errorf("expected escalation_source=issue:GH-123, got %s", alert.Metadata["escalation_source"])
	}
	if alert.Metadata["custom"] != "value" {
		t.Errorf("expected custom=value (preserved from event), got %s", alert.Metadata["custom"])
	}
	if alert.ProjectPath != "/my/project" {
		t.Errorf("expected project path '/my/project', got '%s'", alert.ProjectPath)
	}
}
