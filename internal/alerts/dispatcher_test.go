package alerts

import (
	"log/slog"
	"testing"
)

// =============================================================================
// Dispatcher Creation Tests
// =============================================================================

func TestNewDispatcher(t *testing.T) {
	config := &AlertConfig{Enabled: true}
	d := NewDispatcher(config)

	if d == nil {
		t.Fatal("expected non-nil dispatcher")
	}
	if d.config != config {
		t.Error("expected config to be set")
	}
	if d.channels == nil {
		t.Error("expected channels map to be initialized")
	}
	if d.logger == nil {
		t.Error("expected default logger")
	}
}

func TestNewDispatcher_WithLogger(t *testing.T) {
	config := &AlertConfig{Enabled: true}
	logger := slog.Default()
	d := NewDispatcher(config, WithDispatcherLogger(logger))

	if d.logger != logger {
		t.Error("expected custom logger to be set")
	}
}

// =============================================================================
// Channel Registration Tests
// =============================================================================

func TestDispatcher_RegisterChannel(t *testing.T) {
	d := NewDispatcher(&AlertConfig{})
	ch := newMockChannel("test-ch", "mock")

	d.RegisterChannel(ch)

	got, ok := d.GetChannel("test-ch")
	if !ok {
		t.Fatal("expected channel to be registered")
	}
	if got.Name() != "test-ch" {
		t.Errorf("expected name 'test-ch', got '%s'", got.Name())
	}
}

func TestDispatcher_UnregisterChannel(t *testing.T) {
	d := NewDispatcher(&AlertConfig{})
	ch := newMockChannel("remove-me", "mock")

	d.RegisterChannel(ch)
	d.UnregisterChannel("remove-me")

	_, ok := d.GetChannel("remove-me")
	if ok {
		t.Error("expected channel to be unregistered")
	}
}

func TestDispatcher_GetChannel_NotFound(t *testing.T) {
	d := NewDispatcher(&AlertConfig{})

	_, ok := d.GetChannel("nonexistent")
	if ok {
		t.Error("expected channel not found")
	}
}

func TestDispatcher_ListChannels_AllRegistered(t *testing.T) {
	d := NewDispatcher(&AlertConfig{})
	d.RegisterChannel(newMockChannel("ch-a", "mock"))
	d.RegisterChannel(newMockChannel("ch-b", "mock"))
	d.RegisterChannel(newMockChannel("ch-c", "mock"))

	names := d.ListChannels()
	if len(names) != 3 {
		t.Fatalf("expected 3 channels, got %d", len(names))
	}

	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}
	for _, expected := range []string{"ch-a", "ch-b", "ch-c"} {
		if !nameSet[expected] {
			t.Errorf("expected channel %q in list", expected)
		}
	}
}

func TestDispatcher_ListChannels_Empty(t *testing.T) {
	d := NewDispatcher(&AlertConfig{})
	names := d.ListChannels()
	if len(names) != 0 {
		t.Errorf("expected 0 channels, got %d", len(names))
	}
}

// =============================================================================
// Severity Matching Tests
// =============================================================================

func TestDispatcher_SeverityMatches_TableDriven(t *testing.T) {
	d := NewDispatcher(&AlertConfig{})

	tests := []struct {
		name       string
		severities []Severity
		alert      Severity
		want       bool
	}{
		{"empty filter accepts all", []Severity{}, SeverityInfo, true},
		{"nil filter accepts all", nil, SeverityCritical, true},
		{"matches critical", []Severity{SeverityCritical}, SeverityCritical, true},
		{"no match", []Severity{SeverityCritical}, SeverityInfo, false},
		{"multi-severity match", []Severity{SeverityWarning, SeverityCritical}, SeverityWarning, true},
		{"multi-severity no match", []Severity{SeverityWarning, SeverityCritical}, SeverityInfo, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.severityMatches(tt.severities, tt.alert)
			if got != tt.want {
				t.Errorf("severityMatches(%v, %s) = %v, want %v", tt.severities, tt.alert, got, tt.want)
			}
		})
	}
}

// =============================================================================
// ChannelError Tests
// =============================================================================

func TestChannelError_Message(t *testing.T) {
	err := &ChannelError{Message: "test error"}
	if err.Error() != "test error" {
		t.Errorf("expected 'test error', got '%s'", err.Error())
	}
}

func TestErrChannelNotFound(t *testing.T) {
	if ErrChannelNotFound == nil {
		t.Fatal("ErrChannelNotFound should not be nil")
	}
	if ErrChannelNotFound.Error() != "channel not found" {
		t.Errorf("expected 'channel not found', got '%s'", ErrChannelNotFound.Error())
	}
}
