package alerts

import (
	"context"
	"testing"
	"time"
)

func TestEngine_ChannelAcceptsSeverity(t *testing.T) {
	config := &AlertConfig{Enabled: true}
	engine := NewEngine(config)

	tests := []struct {
		name       string
		channel    ChannelConfig
		severity   Severity
		wantAccept bool
	}{
		{
			name: "empty severities accepts all",
			channel: ChannelConfig{
				Name:       "test",
				Severities: []Severity{},
			},
			severity:   SeverityCritical,
			wantAccept: true,
		},
		{
			name: "matching severity",
			channel: ChannelConfig{
				Name:       "test",
				Severities: []Severity{SeverityWarning, SeverityCritical},
			},
			severity:   SeverityWarning,
			wantAccept: true,
		},
		{
			name: "non-matching severity",
			channel: ChannelConfig{
				Name:       "test",
				Severities: []Severity{SeverityCritical},
			},
			severity:   SeverityInfo,
			wantAccept: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := engine.channelAcceptsSeverity(tt.channel, tt.severity)
			if got != tt.wantAccept {
				t.Errorf("channelAcceptsSeverity() = %v, want %v", got, tt.wantAccept)
			}
		})
	}
}

func TestEngine_FireAlertWithoutDispatcher(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Rules: []AlertRule{
			{
				Name:     "task_failed",
				Type:     AlertTypeTaskFailed,
				Enabled:  true,
				Severity: SeverityWarning,
				Channels: []string{"test"},
				Cooldown: 0,
			},
		},
	}

	// Engine without dispatcher
	engine := NewEngine(config)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = engine.Start(ctx)

	// Should not panic
	engine.ProcessEvent(Event{
		Type:      EventTypeTaskFailed,
		TaskID:    "TASK-1",
		Timestamp: time.Now(),
	})

	engine.flushForTest()
	// Verify history is still recorded
	history := engine.GetAlertHistory(10)
	// No history recorded because dispatcher is nil
	if len(history) != 0 {
		t.Errorf("expected 0 history entries without dispatcher, got %d", len(history))
	}
}

func TestEngine_EmptyChannelList(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{Name: "enabled-channel", Type: "webhook", Enabled: true},
			{Name: "disabled-channel", Type: "webhook", Enabled: false},
		},
		Rules: []AlertRule{
			{
				Name:     "task_failed",
				Type:     AlertTypeTaskFailed,
				Enabled:  true,
				Severity: SeverityWarning,
				Channels: []string{}, // Empty - should use all enabled channels
				Cooldown: 0,
			},
		},
	}

	mockEnabled := newMockChannel("enabled-channel", "webhook")
	mockDisabled := newMockChannel("disabled-channel", "webhook")

	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockEnabled)
	dispatcher.RegisterChannel(mockDisabled)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = engine.Start(ctx)

	engine.ProcessEvent(Event{
		Type:      EventTypeTaskFailed,
		TaskID:    "TASK-1",
		Timestamp: time.Now(),
	})

	waitForAlerts(t, mockEnabled, 1, 2*time.Second)
	if len(mockEnabled.getAlerts()) != 1 {
		t.Error("expected enabled channel to receive alert")
	}
	if len(mockDisabled.getAlerts()) != 0 {
		t.Error("expected disabled channel to not receive alert")
	}
}

func TestAlertHistory(t *testing.T) {
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
				Cooldown:    0,
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

	// Send some failures
	for i := 1; i <= 3; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    "TASK-" + string(rune('0'+i)),
			Project:   "/test/project",
			Error:     "test error",
			Timestamp: time.Now(),
		})
	}
	engine.flushForTest()

	history := engine.GetAlertHistory(10)
	if len(history) != 3 {
		t.Errorf("expected 3 history entries, got %d", len(history))
	}

	// Check that history is in reverse order (most recent first)
	if len(history) >= 2 && history[0].FiredAt.Before(history[1].FiredAt) {
		t.Error("expected history to be in reverse chronological order")
	}
}

func TestEngine_GetAlertHistory_LimitBehavior(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{Name: "test", Type: "webhook", Enabled: true},
		},
		Rules: []AlertRule{
			{
				Name:     "task_failed",
				Type:     AlertTypeTaskFailed,
				Enabled:  true,
				Severity: SeverityWarning,
				Channels: []string{"test"},
				Cooldown: 0,
			},
		},
	}

	mockCh := newMockChannel("test", "webhook")
	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(mockCh)

	engine := NewEngine(config, WithDispatcher(dispatcher))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = engine.Start(ctx)

	// Send 5 failures
	for i := 0; i < 5; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    "TASK-" + string(rune('0'+i)),
			Timestamp: time.Now(),
		})
	}
	engine.flushForTest()

	tests := []struct {
		name      string
		limit     int
		wantCount int
	}{
		{"zero limit returns all", 0, 5},
		{"negative limit returns all", -1, 5},
		{"limit exceeds count", 10, 5},
		{"limit less than count", 2, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			history := engine.GetAlertHistory(tt.limit)
			if len(history) != tt.wantCount {
				t.Errorf("GetAlertHistory(%d) = %d entries, want %d", tt.limit, len(history), tt.wantCount)
			}
		})
	}
}

func TestEngine_UpdateConfig(t *testing.T) {
	config1 := &AlertConfig{Enabled: true}
	config2 := &AlertConfig{Enabled: false}

	engine := NewEngine(config1)

	if !engine.GetConfig().Enabled {
		t.Error("expected config to be enabled initially")
	}

	engine.UpdateConfig(config2)

	if engine.GetConfig().Enabled {
		t.Error("expected config to be disabled after update")
	}
}
