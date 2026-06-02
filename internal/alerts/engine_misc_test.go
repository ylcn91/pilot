package alerts

import (
	"log/slog"
	"testing"
	"time"
)

// =============================================================================
// Alert Creation Tests
// =============================================================================

func TestEngine_CreateAlert(t *testing.T) {
	config := &AlertConfig{Enabled: true}
	engine := NewEngine(config)

	tests := []struct {
		name       string
		rule       AlertRule
		event      Event
		message    string
		wantSource string
	}{
		{
			name: "with task ID",
			rule: AlertRule{
				Type:        AlertTypeTaskFailed,
				Severity:    SeverityWarning,
				Description: "Task failed alert",
			},
			event: Event{
				TaskID:  "TASK-123",
				Project: "/my/project",
				Metadata: map[string]string{
					"custom": "value",
				},
			},
			message:    "Task failed",
			wantSource: "task:TASK-123",
		},
		{
			name: "without task ID",
			rule: AlertRule{
				Type:        AlertTypeDailySpend,
				Severity:    SeverityCritical,
				Description: "Budget alert",
			},
			event: Event{
				Project: "/my/project",
			},
			message:    "Budget exceeded",
			wantSource: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			alert := engine.createAlert(tt.rule, tt.event, tt.message)

			if alert.ID == "" {
				t.Error("expected non-empty alert ID")
			}
			if alert.Type != tt.rule.Type {
				t.Errorf("expected type %s, got %s", tt.rule.Type, alert.Type)
			}
			if alert.Severity != tt.rule.Severity {
				t.Errorf("expected severity %s, got %s", tt.rule.Severity, alert.Severity)
			}
			if alert.Title != tt.rule.Description {
				t.Errorf("expected title '%s', got '%s'", tt.rule.Description, alert.Title)
			}
			if alert.Message != tt.message {
				t.Errorf("expected message '%s', got '%s'", tt.message, alert.Message)
			}
			if alert.Source != tt.wantSource {
				t.Errorf("expected source '%s', got '%s'", tt.wantSource, alert.Source)
			}
			if alert.ProjectPath != tt.event.Project {
				t.Errorf("expected project path '%s', got '%s'", tt.event.Project, alert.ProjectPath)
			}
			if alert.CreatedAt.IsZero() {
				t.Error("expected non-zero CreatedAt")
			}
		})
	}
}

// =============================================================================
// Channel Error Tests
// =============================================================================

func TestChannelError(t *testing.T) {
	err := &ChannelError{Message: "test error message"}

	if err.Error() != "test error message" {
		t.Errorf("expected 'test error message', got '%s'", err.Error())
	}
}

// =============================================================================
// Engine Option Tests
// =============================================================================

func TestEngine_WithLogger(t *testing.T) {
	config := &AlertConfig{Enabled: true}
	logger := slog.Default()

	engine := NewEngine(config, WithLogger(logger))

	if engine.logger != logger {
		t.Error("expected logger to be set via option")
	}
}

// =============================================================================
// Event Queue Full Test
// =============================================================================

func TestEngine_ProcessEvent_QueueFull(t *testing.T) {
	config := &AlertConfig{Enabled: true}
	engine := NewEngine(config)

	// Fill up the event queue (capacity is 100)
	for i := 0; i < 110; i++ {
		engine.ProcessEvent(Event{
			Type:      EventTypeTaskFailed,
			TaskID:    "TASK-" + string(rune('0'+i)),
			Timestamp: time.Now(),
		})
	}

	// Should not panic - events beyond capacity are dropped
}
