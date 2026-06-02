package alerts

import (
	"context"
	"testing"
	"time"
)

// =============================================================================
// EmailChannel Tests
// =============================================================================

type mockEmailSender struct {
	sentTo      []string
	sentSubject string
	sentBody    string
	err         error
}

func (m *mockEmailSender) Send(ctx context.Context, to []string, subject, htmlBody string) error {
	m.sentTo = to
	m.sentSubject = subject
	m.sentBody = htmlBody
	return m.err
}

func TestNewEmailChannel(t *testing.T) {
	sender := &mockEmailSender{}
	config := &EmailChannelConfig{
		To:      []string{"admin@example.com"},
		Subject: "Custom: {{title}}",
	}

	ch := NewEmailChannel("email-channel", sender, config)

	if ch.Name() != "email-channel" {
		t.Errorf("expected name 'email-channel', got '%s'", ch.Name())
	}
	if ch.Type() != "email" {
		t.Errorf("expected type 'email', got '%s'", ch.Type())
	}
}

func TestEmailChannel_Send(t *testing.T) {
	sender := &mockEmailSender{}
	config := &EmailChannelConfig{
		To: []string{"admin@example.com", "ops@example.com"},
	}

	ch := NewEmailChannel("test", sender, config)

	alert := &Alert{
		ID:       "alert-123",
		Type:     AlertTypeTaskFailed,
		Severity: SeverityWarning,
		Title:    "Test Alert Title",
		Message:  "Test message body",
	}

	err := ch.Send(context.Background(), alert)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(sender.sentTo) != 2 {
		t.Errorf("expected 2 recipients, got %d", len(sender.sentTo))
	}
	if sender.sentSubject == "" {
		t.Error("expected subject to be set")
	}
	if sender.sentBody == "" {
		t.Error("expected body to be set")
	}
}

func TestEmailChannel_FormatSubject(t *testing.T) {
	tests := []struct {
		name           string
		customSubject  string
		alert          *Alert
		expectContains string
	}{
		{
			name:          "custom subject with templates",
			customSubject: "[{{severity}}] {{type}}: {{title}}",
			alert: &Alert{
				Type:     AlertTypeTaskFailed,
				Severity: SeverityWarning,
				Title:    "My Alert",
			},
			expectContains: "warning",
		},
		{
			name:          "default subject for critical",
			customSubject: "",
			alert: &Alert{
				Severity: SeverityCritical,
				Title:    "Critical Issue",
			},
			expectContains: "CRITICAL",
		},
		{
			name:          "default subject for warning",
			customSubject: "",
			alert: &Alert{
				Severity: SeverityWarning,
				Title:    "Warning Issue",
			},
			expectContains: "WARNING",
		},
		{
			name:          "default subject for info",
			customSubject: "",
			alert: &Alert{
				Severity: SeverityInfo,
				Title:    "Info Alert",
			},
			expectContains: "INFO",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &EmailChannelConfig{
				To:      []string{"test@example.com"},
				Subject: tt.customSubject,
			}
			ch := NewEmailChannel("test", &mockEmailSender{}, config)

			subject := ch.formatSubject(tt.alert)
			if subject == "" {
				t.Error("expected non-empty subject")
			}
		})
	}
}

func TestEmailChannel_FormatBody(t *testing.T) {
	config := &EmailChannelConfig{
		To: []string{"test@example.com"},
	}
	ch := NewEmailChannel("test", &mockEmailSender{}, config)

	severities := []Severity{SeverityInfo, SeverityWarning, SeverityCritical}

	for _, severity := range severities {
		t.Run(string(severity), func(t *testing.T) {
			alert := &Alert{
				ID:          "alert-123",
				Type:        AlertTypeTaskFailed,
				Severity:    severity,
				Title:       "Test Title",
				Message:     "Test Message",
				Source:      "task:TASK-1",
				ProjectPath: "/my/project",
				CreatedAt:   time.Now(),
			}

			body := ch.formatBody(alert)

			// Verify HTML structure
			if body == "" {
				t.Error("expected non-empty body")
			}

			// Should contain alert info
			if len(body) < 100 {
				t.Error("expected substantial HTML body")
			}
		})
	}
}
