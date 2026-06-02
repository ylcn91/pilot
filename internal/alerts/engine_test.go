package alerts

import (
	"context"
	"testing"
	"time"
)

// =============================================================================
// Engine Tests
// =============================================================================

func TestNewEngine(t *testing.T) {
	tests := []struct {
		name        string
		config      *AlertConfig
		wantEnabled bool
	}{
		{
			name: "enabled config",
			config: &AlertConfig{
				Enabled: true,
				Rules:   []AlertRule{},
			},
			wantEnabled: true,
		},
		{
			name: "disabled config",
			config: &AlertConfig{
				Enabled: false,
			},
			wantEnabled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewEngine(tt.config)
			if engine == nil {
				t.Fatal("expected non-nil engine")
			}
			if engine.config.Enabled != tt.wantEnabled {
				t.Errorf("expected Enabled=%v, got %v", tt.wantEnabled, engine.config.Enabled)
			}
		})
	}
}

func TestEngine_WithOptions(t *testing.T) {
	config := &AlertConfig{Enabled: true}
	dispatcher := NewDispatcher(config)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	if engine.dispatcher != dispatcher {
		t.Error("expected dispatcher to be set via option")
	}
}

func TestEngine_Start_Disabled(t *testing.T) {
	config := &AlertConfig{Enabled: false}
	engine := NewEngine(config)

	err := engine.Start(context.Background())
	if err != nil {
		t.Errorf("expected no error for disabled engine, got %v", err)
	}
}

func TestEngine_Start_Enabled(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Rules:   []AlertRule{},
	}
	engine := NewEngine(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := engine.Start(ctx)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	// Stop should not panic
	engine.Stop()
}

func TestEngine_ProcessEvent_Disabled(t *testing.T) {
	config := &AlertConfig{Enabled: false}
	mockCh := newMockChannel("test", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	engine.ProcessEvent(Event{
		Type:      EventTypeTaskFailed,
		TaskID:    "TASK-1",
		Timestamp: time.Now(),
	})

	if len(mockCh.getAlerts()) != 0 {
		t.Error("expected no alerts when engine is disabled")
	}
}

func TestEngine_ProcessTaskFailedEvent(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{
				Name:       "test-slack",
				Type:       "slack",
				Enabled:    true,
				Severities: []Severity{SeverityWarning, SeverityCritical},
			},
		},
		Rules: []AlertRule{
			{
				Name:        "task_failed",
				Type:        AlertTypeTaskFailed,
				Enabled:     true,
				Condition:   RuleCondition{},
				Severity:    SeverityWarning,
				Channels:    []string{"test-slack"},
				Cooldown:    0,
				Description: "Alert when task fails",
			},
		},
		Defaults: AlertDefaults{
			Cooldown:           5 * time.Minute,
			DefaultSeverity:    SeverityWarning,
			SuppressDuplicates: true,
		},
	}

	mockCh := newMockChannel("test-slack", "slack")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := engine.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start engine: %v", err)
	}

	// Process a task failed event
	engine.ProcessEvent(Event{
		Type:      EventTypeTaskFailed,
		TaskID:    "TASK-123",
		TaskTitle: "Test Task",
		Project:   "/test/project",
		Error:     "test error message",
		Timestamp: time.Now(),
	})

	waitForAlerts(t, mockCh, 1, 2*time.Second)

	alerts := mockCh.getAlerts()
	if len(alerts) != 1 {
		t.Errorf("expected 1 alert, got %d", len(alerts))
		return
	}

	alert := alerts[0]
	if alert.Type != AlertTypeTaskFailed {
		t.Errorf("expected alert type %s, got %s", AlertTypeTaskFailed, alert.Type)
	}
	if alert.Severity != SeverityWarning {
		t.Errorf("expected severity %s, got %s", SeverityWarning, alert.Severity)
	}
	if alert.Source != "task:TASK-123" {
		t.Errorf("expected source task:TASK-123, got %s", alert.Source)
	}
}

func TestEngine_TaskStartedEvent(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Rules:   []AlertRule{},
	}

	engine := NewEngine(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_ = engine.Start(ctx)

	now := time.Now()
	engine.ProcessEvent(Event{
		Type:      EventTypeTaskStarted,
		TaskID:    "TASK-100",
		Phase:     "planning",
		Timestamp: now,
	})

	engine.flushForTest()

	engine.mu.RLock()
	state, exists := engine.taskLastProgress["TASK-100"]
	engine.mu.RUnlock()

	if !exists {
		t.Fatal("expected task progress state to be tracked")
	}
	if state.Progress != 0 {
		t.Errorf("expected progress 0, got %d", state.Progress)
	}
	if state.Phase != "planning" {
		t.Errorf("expected phase 'planning', got '%s'", state.Phase)
	}
}

func TestEngine_TaskProgressEvent(t *testing.T) {
	tests := []struct {
		name           string
		initialState   *progressState
		event          Event
		expectProgress int
		expectPhase    string
	}{
		{
			name:         "new task progress",
			initialState: nil,
			event: Event{
				Type:      EventTypeTaskProgress,
				TaskID:    "TASK-1",
				Progress:  50,
				Phase:     "coding",
				Timestamp: time.Now(),
			},
			expectProgress: 50,
			expectPhase:    "coding",
		},
		{
			name: "progress increases",
			initialState: &progressState{
				Progress:  30,
				Phase:     "planning",
				UpdatedAt: time.Now().Add(-1 * time.Minute),
			},
			event: Event{
				Type:      EventTypeTaskProgress,
				TaskID:    "TASK-2",
				Progress:  60,
				Phase:     "coding",
				Timestamp: time.Now(),
			},
			expectProgress: 60,
			expectPhase:    "coding",
		},
		{
			name: "progress unchanged lower value ignored",
			initialState: &progressState{
				Progress:  80,
				Phase:     "testing",
				UpdatedAt: time.Now().Add(-1 * time.Minute),
			},
			event: Event{
				Type:      EventTypeTaskProgress,
				TaskID:    "TASK-3",
				Progress:  50,
				Phase:     "testing",
				Timestamp: time.Now(),
			},
			expectProgress: 80,
			expectPhase:    "testing",
		},
		{
			name: "phase change updates state",
			initialState: &progressState{
				Progress:  50,
				Phase:     "coding",
				UpdatedAt: time.Now().Add(-1 * time.Minute),
			},
			event: Event{
				Type:      EventTypeTaskProgress,
				TaskID:    "TASK-4",
				Progress:  50,
				Phase:     "testing",
				Timestamp: time.Now(),
			},
			expectProgress: 50,
			expectPhase:    "testing",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &AlertConfig{Enabled: true, Rules: []AlertRule{}}
			engine := NewEngine(config)

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_ = engine.Start(ctx)

			if tt.initialState != nil {
				engine.mu.Lock()
				engine.taskLastProgress[tt.event.TaskID] = *tt.initialState
				engine.mu.Unlock()
			}

			engine.ProcessEvent(tt.event)
			engine.flushForTest()

			engine.mu.RLock()
			state := engine.taskLastProgress[tt.event.TaskID]
			engine.mu.RUnlock()

			if state.Progress != tt.expectProgress {
				t.Errorf("expected progress %d, got %d", tt.expectProgress, state.Progress)
			}
			if state.Phase != tt.expectPhase {
				t.Errorf("expected phase '%s', got '%s'", tt.expectPhase, state.Phase)
			}
		})
	}
}
