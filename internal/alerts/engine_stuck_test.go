package alerts

import (
	"context"
	"testing"
	"time"
)

// =============================================================================
// Stuck Tasks Evaluation Tests
// =============================================================================

func TestEngine_EvaluateStuckTasks(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{Name: "test-channel", Type: "webhook", Enabled: true},
		},
		Rules: []AlertRule{
			{
				Name:    "task_stuck",
				Type:    AlertTypeTaskStuck,
				Enabled: true,
				Condition: RuleCondition{
					ProgressUnchangedFor: 5 * time.Minute,
				},
				Severity: SeverityWarning,
				Channels: []string{"test-channel"},
				Cooldown: 0,
			},
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	// Add a stuck task (10 min > 5 min threshold but < 20 min orphan threshold)
	engine.mu.Lock()
	engine.taskLastProgress["TASK-STUCK"] = progressState{
		Progress:  50,
		Phase:     "coding",
		UpdatedAt: time.Now().Add(-10 * time.Minute),
	}
	engine.mu.Unlock()

	// Manually trigger evaluation
	ctx := context.Background()
	engine.evaluateStuckTasks(ctx)

	waitForAlerts(t, mockCh, 1, 2*time.Second)

	alerts := mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Errorf("expected 1 stuck task alert, got %d", len(alerts))
		return
	}

	if alerts[0].Type != AlertTypeTaskStuck {
		t.Errorf("expected alert type %s, got %s", AlertTypeTaskStuck, alerts[0].Type)
	}
}

func TestEngine_EvaluateStuckTasks_DefaultThreshold(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{Name: "test-channel", Type: "webhook", Enabled: true},
		},
		Rules: []AlertRule{
			{
				Name:    "task_stuck",
				Type:    AlertTypeTaskStuck,
				Enabled: true,
				Condition: RuleCondition{
					ProgressUnchangedFor: 0, // Zero - should use default
				},
				Severity: SeverityWarning,
				Channels: []string{"test-channel"},
				Cooldown: 0,
			},
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	// Add a task that was just updated - should NOT trigger alert with default 10 min threshold
	engine.mu.Lock()
	engine.taskLastProgress["TASK-RECENT"] = progressState{
		Progress:  50,
		Phase:     "coding",
		UpdatedAt: time.Now(),
	}
	engine.mu.Unlock()

	// Manually trigger evaluation
	ctx := context.Background()
	engine.evaluateStuckTasks(ctx)

	engine.WaitForDispatch()

	alerts := mockCh.getAlerts()
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts for recent task, got %d", len(alerts))
	}
}

func TestEngine_EvaluateStuckTasks_DisabledRule(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{Name: "test-channel", Type: "webhook", Enabled: true},
		},
		Rules: []AlertRule{
			{
				Name:    "task_stuck",
				Type:    AlertTypeTaskStuck,
				Enabled: false, // Disabled
				Condition: RuleCondition{
					ProgressUnchangedFor: 5 * time.Minute,
				},
				Severity: SeverityWarning,
				Channels: []string{"test-channel"},
				Cooldown: 0,
			},
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	// Add a stuck task (10 min > 5 min threshold, < 20 min orphan)
	engine.mu.Lock()
	engine.taskLastProgress["TASK-STUCK"] = progressState{
		Progress:  50,
		Phase:     "coding",
		UpdatedAt: time.Now().Add(-10 * time.Minute),
	}
	engine.mu.Unlock()

	// Manually trigger evaluation
	ctx := context.Background()
	engine.evaluateStuckTasks(ctx)

	engine.WaitForDispatch()

	alerts := mockCh.getAlerts()
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts (rule disabled), got %d", len(alerts))
	}
}

func TestEngine_EvaluateStuckTasks_WrongRuleType(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{Name: "test-channel", Type: "webhook", Enabled: true},
		},
		Rules: []AlertRule{
			{
				Name:     "task_failed", // Wrong type - should not evaluate stuck
				Type:     AlertTypeTaskFailed,
				Enabled:  true,
				Severity: SeverityWarning,
				Channels: []string{"test-channel"},
				Cooldown: 0,
			},
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	// Add a stuck task (10 min > default 10 min threshold won't fire, but rule type is wrong anyway)
	engine.mu.Lock()
	engine.taskLastProgress["TASK-STUCK"] = progressState{
		Progress:  50,
		Phase:     "coding",
		UpdatedAt: time.Now().Add(-15 * time.Minute),
	}
	engine.mu.Unlock()

	// Manually trigger evaluation
	ctx := context.Background()
	engine.evaluateStuckTasks(ctx)

	engine.WaitForDispatch()

	alerts := mockCh.getAlerts()
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts (wrong rule type), got %d", len(alerts))
	}
}
