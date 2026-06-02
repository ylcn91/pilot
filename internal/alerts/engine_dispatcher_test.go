package alerts

import (
	"context"
	"errors"
	"log/slog"
	"testing"
)

func TestDispatcher_DispatchToMultipleChannels(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{Name: "channel-1", Type: "webhook", Enabled: true},
			{Name: "channel-2", Type: "webhook", Enabled: true},
			{Name: "channel-3", Type: "webhook", Enabled: true},
		},
	}

	ch1 := newMockChannel("channel-1", "webhook")
	ch2 := newMockChannel("channel-2", "webhook")
	ch3 := newMockChannel("channel-3", "webhook")

	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(ch1)
	dispatcher.RegisterChannel(ch2)
	dispatcher.RegisterChannel(ch3)

	alert := &Alert{
		ID:       "test-alert-1",
		Type:     AlertTypeTaskFailed,
		Severity: SeverityWarning,
		Title:    "Test Alert",
		Message:  "Test message",
	}

	results := dispatcher.Dispatch(context.Background(), alert, []string{"channel-1", "channel-2"})

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}

	for _, r := range results {
		if !r.Success {
			t.Errorf("expected success for channel %s", r.ChannelName)
		}
	}

	if len(ch1.getAlerts()) != 1 {
		t.Error("channel-1 should have received 1 alert")
	}
	if len(ch2.getAlerts()) != 1 {
		t.Error("channel-2 should have received 1 alert")
	}
	if len(ch3.getAlerts()) != 0 {
		t.Error("channel-3 should not have received any alerts")
	}
}

func TestDispatcher_ChannelNotFound(t *testing.T) {
	config := &AlertConfig{
		Enabled:  true,
		Channels: []ChannelConfig{},
	}

	dispatcher := NewDispatcher(config)

	alert := &Alert{
		ID:       "test-alert-1",
		Type:     AlertTypeTaskFailed,
		Severity: SeverityWarning,
		Title:    "Test Alert",
		Message:  "Test message",
	}

	results := dispatcher.Dispatch(context.Background(), alert, []string{"nonexistent"})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Success {
		t.Error("expected failure for nonexistent channel")
	}
	if results[0].Error != ErrChannelNotFound {
		t.Errorf("expected ErrChannelNotFound, got %v", results[0].Error)
	}
}

func TestDispatcher_RegisterUnregisterChannel(t *testing.T) {
	config := &AlertConfig{Enabled: true}
	dispatcher := NewDispatcher(config)

	ch := newMockChannel("test-channel", "webhook")
	dispatcher.RegisterChannel(ch)

	// Verify channel is registered
	found, ok := dispatcher.GetChannel("test-channel")
	if !ok {
		t.Error("expected channel to be found after registration")
	}
	if found.Name() != "test-channel" {
		t.Errorf("expected channel name 'test-channel', got '%s'", found.Name())
	}

	// Unregister
	dispatcher.UnregisterChannel("test-channel")

	// Verify channel is removed
	_, ok = dispatcher.GetChannel("test-channel")
	if ok {
		t.Error("expected channel to not be found after unregistration")
	}
}

func TestDispatcher_ListChannels(t *testing.T) {
	config := &AlertConfig{Enabled: true}
	dispatcher := NewDispatcher(config)

	dispatcher.RegisterChannel(newMockChannel("ch-1", "webhook"))
	dispatcher.RegisterChannel(newMockChannel("ch-2", "slack"))
	dispatcher.RegisterChannel(newMockChannel("ch-3", "telegram"))

	channels := dispatcher.ListChannels()
	if len(channels) != 3 {
		t.Errorf("expected 3 channels, got %d", len(channels))
	}

	// Verify all names are present
	nameSet := make(map[string]bool)
	for _, name := range channels {
		nameSet[name] = true
	}

	for _, expected := range []string{"ch-1", "ch-2", "ch-3"} {
		if !nameSet[expected] {
			t.Errorf("expected channel '%s' in list", expected)
		}
	}
}

func TestDispatcher_DispatchAll(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{Name: "enabled-warning", Type: "webhook", Enabled: true, Severities: []Severity{SeverityWarning}},
			{Name: "enabled-critical", Type: "webhook", Enabled: true, Severities: []Severity{SeverityCritical}},
			{Name: "disabled", Type: "webhook", Enabled: false, Severities: []Severity{SeverityWarning}},
		},
	}

	chWarning := newMockChannel("enabled-warning", "webhook")
	chCritical := newMockChannel("enabled-critical", "webhook")
	chDisabled := newMockChannel("disabled", "webhook")

	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(chWarning)
	dispatcher.RegisterChannel(chCritical)
	dispatcher.RegisterChannel(chDisabled)

	alert := &Alert{
		ID:       "test",
		Severity: SeverityWarning,
	}

	results := dispatcher.DispatchAll(context.Background(), alert)

	// Should only dispatch to enabled-warning (enabled + matching severity)
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	if len(chWarning.getAlerts()) != 1 {
		t.Error("expected enabled-warning to receive alert")
	}
	if len(chCritical.getAlerts()) != 0 {
		t.Error("expected enabled-critical to not receive alert (severity mismatch)")
	}
	if len(chDisabled.getAlerts()) != 0 {
		t.Error("expected disabled to not receive alert")
	}
}

func TestDispatcher_ChannelSendError(t *testing.T) {
	config := &AlertConfig{
		Enabled: true,
		Channels: []ChannelConfig{
			{Name: "error-channel", Type: "webhook", Enabled: true},
		},
	}

	ch := newMockChannel("error-channel", "webhook")
	ch.setError(errors.New("send failed"))

	dispatcher := NewDispatcher(config)
	dispatcher.RegisterChannel(ch)

	alert := &Alert{
		ID:       "test",
		Severity: SeverityWarning,
	}

	results := dispatcher.Dispatch(context.Background(), alert, []string{"error-channel"})

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	if results[0].Success {
		t.Error("expected failure when channel returns error")
	}
	if results[0].Error == nil {
		t.Error("expected error to be set")
	}
}

func TestDispatcher_SeverityMatches(t *testing.T) {
	config := &AlertConfig{Enabled: true}
	dispatcher := NewDispatcher(config)

	tests := []struct {
		name       string
		severities []Severity
		alert      Severity
		want       bool
	}{
		{
			name:       "empty severities matches all",
			severities: []Severity{},
			alert:      SeverityCritical,
			want:       true,
		},
		{
			name:       "matching severity",
			severities: []Severity{SeverityWarning, SeverityCritical},
			alert:      SeverityWarning,
			want:       true,
		},
		{
			name:       "non-matching severity",
			severities: []Severity{SeverityCritical},
			alert:      SeverityInfo,
			want:       false,
		},
		{
			name:       "nil-like empty",
			severities: nil,
			alert:      SeverityWarning,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := dispatcher.severityMatches(tt.severities, tt.alert)
			if got != tt.want {
				t.Errorf("severityMatches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDispatcher_WithDispatcherLogger(t *testing.T) {
	config := &AlertConfig{Enabled: true}
	logger := slog.Default()

	dispatcher := NewDispatcher(config, WithDispatcherLogger(logger))

	if dispatcher.logger != logger {
		t.Error("expected logger to be set via option")
	}
}
