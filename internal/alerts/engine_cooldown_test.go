package alerts

import (
	"context"
	"testing"
	"time"
)

func TestEngine_ConsecutiveFailures(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{
				Name:       "test-channel",
				Type:       "webhook",
				Enabled:    true,
				Severities: []Severity{SeverityCritical},
			},
		},
		Rules: []AlertRule{
			{
				Name:    "consecutive_failures",
				Type:    AlertTypeConsecutiveFails,
				Enabled: true,
				Condition: RuleCondition{
					ConsecutiveFailures: 3,
				},
				Severity:    SeverityCritical,
				Channels:    []string{"test-channel"},
				Cooldown:    0,
				Description: "Alert on consecutive failures",
			},
		},
		Defaults: AlertDefaults{
			Cooldown:        5 * time.Minute,
			DefaultSeverity: SeverityWarning,
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = engine.Start(ctx)

	project := "/test/project"

	// Send 3 consecutive failures
	for i := 1; i <= 3; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    "TASK-" + string(rune('0'+i)),
			TaskTitle: "Test Task",
			Project:   project,
			Error:     "test error",
			Timestamp: time.Now(),
		})
	}

	// Wait for event processing
	waitForAlerts(t, mockCh, 1, 2*time.Second)

	alerts := mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Errorf("expected 1 consecutive failures alert, got %d", len(alerts))
		return
	}

	alert := alerts[0]
	if alert.Type != AlertTypeConsecutiveFails {
		t.Errorf("expected alert type %s, got %s", AlertTypeConsecutiveFails, alert.Type)
	}
	if alert.Severity != SeverityCritical {
		t.Errorf("expected severity %s, got %s", SeverityCritical, alert.Severity)
	}
}

func TestEngine_CooldownRespected(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{
				Name:       "test-channel",
				Type:       "webhook",
				Enabled:    true,
				Severities: []Severity{SeverityWarning},
			},
		},
		Rules: []AlertRule{
			{
				Name:        "task_failed",
				Type:        AlertTypeTaskFailed,
				Enabled:     true,
				Condition:   RuleCondition{},
				Severity:    SeverityWarning,
				Channels:    []string{"test-channel"},
				Cooldown:    1 * time.Hour, // Long cooldown
				Description: "Alert on task failure",
			},
		},
		Defaults: AlertDefaults{
			Cooldown:        5 * time.Minute,
			DefaultSeverity: SeverityWarning,
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = engine.Start(ctx)

	// Send first failure - should trigger alert
	engine.ProcessEvent(Event{
		Type:      EventTypeTaskFailed,
		TaskID:    "TASK-1",
		TaskTitle: "Test Task 1",
		Project:   "/test/project",
		Error:     "test error",
		Timestamp: time.Now(),
	})

	waitForAlerts(t, mockCh, 1, 2*time.Second)

	// Send second failure - should be suppressed due to cooldown
	engine.ProcessEvent(Event{
		Type:      EventTypeTaskFailed,
		TaskID:    "TASK-2",
		TaskTitle: "Test Task 2",
		Project:   "/test/project",
		Error:     "test error",
		Timestamp: time.Now(),
	})

	engine.flushForTest()

	alerts := mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Errorf("expected 1 alert (second should be suppressed by cooldown), got %d", len(alerts))
	}
}

func TestEngine_ZeroCooldown(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{
				Name:       "test-channel",
				Type:       "webhook",
				Enabled:    true,
				Severities: []Severity{SeverityWarning},
			},
		},
		Rules: []AlertRule{
			{
				Name:        "task_failed",
				Type:        AlertTypeTaskFailed,
				Enabled:     true,
				Condition:   RuleCondition{},
				Severity:    SeverityWarning,
				Channels:    []string{"test-channel"},
				Cooldown:    0, // Zero cooldown - should fire every time
				Description: "Alert on task failure",
			},
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = engine.Start(ctx)

	// Send multiple failures - all should trigger alerts
	for i := 1; i <= 3; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    "TASK-" + string(rune('0'+i)),
			Project:   "/test/project",
			Error:     "test error",
			Timestamp: time.Now(),
		})
	}

	waitForAlerts(t, mockCh, 3, 2*time.Second)

	alerts := mockCh.getAlerts()
	if len(alerts) != 3 {
		t.Errorf("expected 3 alerts with zero cooldown, got %d", len(alerts))
	}
}

func TestEngine_TaskCompletedResetsFails(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{
				Name:       "test-channel",
				Type:       "webhook",
				Enabled:    true,
				Severities: []Severity{SeverityCritical},
			},
		},
		Rules: []AlertRule{
			{
				Name:    "consecutive_failures",
				Type:    AlertTypeConsecutiveFails,
				Enabled: true,
				Condition: RuleCondition{
					ConsecutiveFailures: 3,
				},
				Severity:    SeverityCritical,
				Channels:    []string{"test-channel"},
				Cooldown:    0,
				Description: "Alert on consecutive failures",
			},
		},
		Defaults: AlertDefaults{
			Cooldown:        5 * time.Minute,
			DefaultSeverity: SeverityWarning,
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = engine.Start(ctx)

	project := "/test/project"

	// Send 2 failures
	for i := 1; i <= 2; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    "TASK-" + string(rune('0'+i)),
			Project:   project,
			Error:     "test error",
			Timestamp: time.Now(),
		})
	}

	engine.flushForTest()
	// Send a success - should reset counter
	engine.ProcessEvent(Event{
		Type:      EventTypeTaskCompleted,
		TaskID:    "TASK-3",
		Project:   project,
		Timestamp: time.Now(),
	})

	engine.flushForTest()
	// Send 2 more failures - should not trigger (counter was reset)
	for i := 4; i <= 5; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    "TASK-" + string(rune('0'+i)),
			Project:   project,
			Error:     "test error",
			Timestamp: time.Now(),
		})
	}

	engine.flushForTest()
	alerts := mockCh.getAlerts()
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts (success reset the counter), got %d", len(alerts))
	}
}

func TestEngine_DisabledRulesIgnored(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{
				Name:       "test-channel",
				Type:       "webhook",
				Enabled:    true,
				Severities: []Severity{SeverityWarning},
			},
		},
		Rules: []AlertRule{
			{
				Name:        "task_failed",
				Type:        AlertTypeTaskFailed,
				Enabled:     false, // Disabled
				Condition:   RuleCondition{},
				Severity:    SeverityWarning,
				Channels:    []string{"test-channel"},
				Cooldown:    0,
				Description: "Alert on task failure",
			},
		},
		Defaults: AlertDefaults{
			Cooldown:        5 * time.Minute,
			DefaultSeverity: SeverityWarning,
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = engine.Start(ctx)

	engine.ProcessEvent(Event{
		Type:      EventTypeTaskFailed,
		TaskID:    "TASK-1",
		TaskTitle: "Test Task",
		Project:   "/test/project",
		Error:     "test error",
		Timestamp: time.Now(),
	})

	engine.flushForTest()
	alerts := mockCh.getAlerts()
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts (rule disabled), got %d", len(alerts))
	}
}
