package alerts

import (
	"context"
	"testing"
	"time"
)

// =============================================================================
// Escalation Tests (GH-848)
// =============================================================================

func TestEngine_Escalation_AfterThreeFailures(t *testing.T) {
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

	source := "issue:GH-123"

	// Send 3 failures for the same source
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
		t.Errorf("expected 1 escalation alert, got %d", len(alerts))
		return
	}

	alert := alerts[0]
	if alert.Type != AlertTypeEscalation {
		t.Errorf("expected alert type %s, got %s", AlertTypeEscalation, alert.Type)
	}
	if alert.Severity != SeverityCritical {
		t.Errorf("expected severity %s, got %s", SeverityCritical, alert.Severity)
	}
	if alert.Source != source {
		t.Errorf("expected source %s, got %s", source, alert.Source)
	}
	if alert.Metadata["retry_count"] != "3" {
		t.Errorf("expected retry_count=3 in metadata, got %s", alert.Metadata["retry_count"])
	}
}

func TestEngine_Escalation_NoEscalationBeforeThreshold(t *testing.T) {
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

	source := "issue:GH-456"

	// Send only 2 failures (below threshold)
	for i := 1; i <= 2; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    "TASK-" + string(rune('0'+i)),
			Project:   "/test/project",
			Error:     "test error",
			Metadata:  map[string]string{"source": source},
			Timestamp: time.Now(),
		})
	}

	engine.flushForTest()

	alerts := mockCh.getAlerts()
	if len(alerts) != 0 {
		t.Errorf("expected 0 escalation alerts (below threshold), got %d", len(alerts))
	}
}

func TestEngine_Escalation_ResetOnSuccess(t *testing.T) {
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

	source := "issue:GH-789"

	// Send 2 failures
	for i := 1; i <= 2; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    "TASK-" + string(rune('0'+i)),
			Project:   "/test/project",
			Error:     "test error",
			Metadata:  map[string]string{"source": source},
			Timestamp: time.Now(),
		})
	}
	engine.flushForTest()

	// Send success - should reset counter
	engine.ProcessEvent(Event{
		Type:      EventTypeTaskCompleted,
		TaskID:    "TASK-3",
		Project:   "/test/project",
		Metadata:  map[string]string{"source": source},
		Timestamp: time.Now(),
	})
	engine.flushForTest()

	// Send 2 more failures - should not escalate (counter was reset)
	for i := 4; i <= 5; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    "TASK-" + string(rune('0'+i)),
			Project:   "/test/project",
			Error:     "test error",
			Metadata:  map[string]string{"source": source},
			Timestamp: time.Now(),
		})
	}
	engine.flushForTest()

	alerts := mockCh.getAlerts()
	if len(alerts) != 0 {
		t.Errorf("expected 0 escalation alerts (success reset counter), got %d", len(alerts))
	}
}

func TestEngine_Escalation_DifferentSourcesTrackedSeparately(t *testing.T) {
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

	// Send 2 failures for source A
	for i := 1; i <= 2; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    "TASK-A-" + string(rune('0'+i)),
			Project:   "/test/project",
			Error:     "error A",
			Metadata:  map[string]string{"source": "issue:A"},
			Timestamp: time.Now(),
		})
	}

	// Send 2 failures for source B
	for i := 1; i <= 2; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    "TASK-B-" + string(rune('0'+i)),
			Project:   "/test/project",
			Error:     "error B",
			Metadata:  map[string]string{"source": "issue:B"},
			Timestamp: time.Now(),
		})
	}

	engine.flushForTest()

	// Neither should have escalated (both at 2 failures)
	alerts := mockCh.getAlerts()
	if len(alerts) != 0 {
		t.Errorf("expected 0 escalation alerts (neither source at threshold), got %d", len(alerts))
	}

	// Third failure for source A - should escalate
	engine.ProcessEvent(Event{
		Type:      EventTypeTaskFailed,
		TaskID:    "TASK-A-3",
		Project:   "/test/project",
		Error:     "error A",
		Metadata:  map[string]string{"source": "issue:A"},
		Timestamp: time.Now(),
	})

	waitForAlerts(t, mockCh, 1, 2*time.Second)

	alerts = mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Errorf("expected 1 escalation alert for source A, got %d", len(alerts))
		return
	}

	if alerts[0].Source != "issue:A" {
		t.Errorf("expected source issue:A, got %s", alerts[0].Source)
	}
}
