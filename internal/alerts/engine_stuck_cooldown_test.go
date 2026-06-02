package alerts

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"
)

// =============================================================================
// GH-2204: Per-task cooldown, multi-task alert, orphan eviction tests
// =============================================================================

func TestEngine_EvaluateStuckTasks_MultipleTasksAllFire(t *testing.T) {
	// Bug 2 regression: with per-rule cooldown, only 1 of N stuck tasks would fire.
	// With per-task cooldown, all N should fire on the same cycle.
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
				Cooldown: 15 * time.Minute,
			},
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	// Add 10 stuck tasks (all between threshold and orphan threshold)
	engine.mu.Lock()
	for i := 0; i < 10; i++ {
		engine.taskLastProgress[fmt.Sprintf("GH-%d", i)] = progressState{
			Progress:  0,
			Phase:     "",
			UpdatedAt: time.Now().Add(-10 * time.Minute), // > 5 min threshold, < 20 min orphan
		}
	}
	engine.mu.Unlock()

	ctx := context.Background()
	engine.evaluateStuckTasks(ctx)

	waitForAlerts(t, mockCh, 10, 2*time.Second)

	alerts := mockCh.getAlerts()
	if len(alerts) != 10 {
		t.Errorf("expected 10 stuck task alerts (one per task), got %d", len(alerts))
	}
}

func TestEngine_EvaluateStuckTasks_PerTaskCooldown(t *testing.T) {
	// Same task on second cycle within cooldown should NOT re-alert.
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
				Cooldown: 15 * time.Minute,
			},
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	engine.mu.Lock()
	engine.taskLastProgress["GH-100"] = progressState{
		Progress:  0,
		UpdatedAt: time.Now().Add(-10 * time.Minute),
	}
	engine.mu.Unlock()

	ctx := context.Background()

	// First cycle: should fire
	engine.evaluateStuckTasks(ctx)
	waitForAlerts(t, mockCh, 1, 2*time.Second)

	alerts := mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert on first cycle, got %d", len(alerts))
	}

	// Second cycle immediately after: should NOT fire (per-task cooldown)
	engine.evaluateStuckTasks(ctx)
	engine.WaitForDispatch()

	alerts = mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Errorf("expected still 1 alert after second cycle (cooldown), got %d", len(alerts))
	}
}

func TestEngine_EvaluateStuckTasks_ProgressResetsCooldown(t *testing.T) {
	// After progress advances, cooldown resets and task can re-alert if stuck again.
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
				Cooldown: 1 * time.Hour, // Long cooldown
			},
		},
	}

	mockCh := newMockChannel("test-channel", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	engine.mu.Lock()
	engine.taskLastProgress["GH-200"] = progressState{
		Progress:  0,
		UpdatedAt: time.Now().Add(-10 * time.Minute),
	}
	engine.mu.Unlock()

	ctx := context.Background()

	// First cycle: fires
	engine.evaluateStuckTasks(ctx)
	waitForAlerts(t, mockCh, 1, 2*time.Second)

	alerts := mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Fatalf("expected 1 alert, got %d", len(alerts))
	}

	// Simulate progress event — resets cooldown
	engine.handleTaskProgress(Event{
		Type:      EventTypeTaskProgress,
		TaskID:    "GH-200",
		Progress:  30,
		Phase:     "coding",
		Timestamp: time.Now(),
	})

	// Simulate getting stuck again (manually set UpdatedAt back)
	engine.mu.Lock()
	s := engine.taskLastProgress["GH-200"]
	s.UpdatedAt = time.Now().Add(-10 * time.Minute)
	engine.taskLastProgress["GH-200"] = s
	engine.mu.Unlock()

	// Should fire again because progress reset the cooldown
	engine.evaluateStuckTasks(ctx)
	waitForAlerts(t, mockCh, 1, 2*time.Second)

	alerts = mockCh.getAlerts()
	if len(alerts) != 2 {
		t.Errorf("expected 2 alerts (cooldown reset by progress), got %d", len(alerts))
	}
}

func TestEngine_EvaluateStuckTasks_OrphanEviction(t *testing.T) {
	// Tasks stuck for 4× threshold should be evicted from the map.
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
					ProgressUnchangedFor: 10 * time.Minute,
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

	logger := slog.Default()
	engine := NewEngine(config, WithDispatcher(dispatcher), WithLogger(logger))

	// Add an orphaned task: stuck for 50 min > 4 * 10 min = 40 min orphan threshold
	engine.mu.Lock()
	engine.taskLastProgress["GH-ORPHAN"] = progressState{
		Progress:  0,
		UpdatedAt: time.Now().Add(-50 * time.Minute),
	}
	// Also add a non-orphan stuck task: 15 min > 10 min threshold, < 40 min orphan
	engine.taskLastProgress["GH-STUCK"] = progressState{
		Progress:  0,
		UpdatedAt: time.Now().Add(-15 * time.Minute),
	}
	engine.mu.Unlock()

	ctx := context.Background()
	engine.evaluateStuckTasks(ctx)

	waitForAlerts(t, mockCh, 1, 2*time.Second)

	// Orphan should be evicted, non-orphan should alert
	engine.mu.RLock()
	_, orphanExists := engine.taskLastProgress["GH-ORPHAN"]
	_, stuckExists := engine.taskLastProgress["GH-STUCK"]
	engine.mu.RUnlock()

	if orphanExists {
		t.Error("expected GH-ORPHAN to be evicted from taskLastProgress")
	}
	if !stuckExists {
		t.Error("expected GH-STUCK to still exist in taskLastProgress")
	}

	alerts := mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Errorf("expected 1 alert (only GH-STUCK, orphan evicted), got %d", len(alerts))
	}
}

func TestEngine_TaskProgressUpdatesStuckState(t *testing.T) {
	// Verify that handleTaskProgress updates UpdatedAt so stuck detection resets.
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

	// Start a task
	engine.handleTaskStarted(Event{
		Type:      EventTypeTaskStarted,
		TaskID:    "GH-300",
		Timestamp: time.Now().Add(-10 * time.Minute), // Started 10 min ago
	})

	// Without progress, it would be stuck. Send a progress event.
	engine.handleTaskProgress(Event{
		Type:      EventTypeTaskProgress,
		TaskID:    "GH-300",
		Progress:  40,
		Phase:     "impl",
		Timestamp: time.Now(), // Fresh timestamp
	})

	ctx := context.Background()
	engine.evaluateStuckTasks(ctx)
	engine.WaitForDispatch()

	alerts := mockCh.getAlerts()
	if len(alerts) != 0 {
		t.Errorf("expected 0 alerts (progress updated recently), got %d", len(alerts))
	}
}
