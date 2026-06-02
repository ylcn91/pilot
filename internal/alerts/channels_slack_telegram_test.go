package alerts

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/adapters/telegram"
)

// =============================================================================
// Escape Markdown Tests
// =============================================================================

func TestEscapeMarkdown(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple text", "simple text"},
		{"*bold*", "\\*bold\\*"},
		{"_italic_", "\\_italic\\_"},
		{"[link](url)", "\\[link\\]\\(url\\)"},
		{"code `block`", "code \\`block\\`"},
		{"a.b.c", "a\\.b\\.c"},
		{"test!", "test\\!"},
		{"a+b=c", "a\\+b\\=c"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := escapeMarkdown(tt.input)
			if result != tt.expected {
				t.Errorf("escapeMarkdown(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

// =============================================================================
// SlackChannel Tests (without real Slack client)
// =============================================================================

func TestSlackChannel_Name(t *testing.T) {
	// We can test the accessor methods without a real client
	ch := &SlackChannel{
		name:    "my-slack-channel",
		channel: "#alerts",
	}

	if ch.Name() != "my-slack-channel" {
		t.Errorf("expected name 'my-slack-channel', got '%s'", ch.Name())
	}
	if ch.Type() != "slack" {
		t.Errorf("expected type 'slack', got '%s'", ch.Type())
	}
}

func TestSlackChannel_SeverityColor(t *testing.T) {
	ch := &SlackChannel{}

	tests := []struct {
		severity Severity
		expected string
	}{
		{SeverityCritical, "danger"},
		{SeverityWarning, "warning"},
		{SeverityInfo, "#0066cc"},
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			result := ch.severityColor(tt.severity)
			if result != tt.expected {
				t.Errorf("severityColor(%s) = %s, want %s", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestSlackChannel_SeverityEmoji(t *testing.T) {
	ch := &SlackChannel{}

	tests := []struct {
		severity Severity
		expected string
	}{
		{SeverityCritical, "\U0001F6A8"},
		{SeverityWarning, "\u26a0\ufe0f"},
		{SeverityInfo, "\u2139\ufe0f"},
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			result := ch.severityEmoji(tt.severity)
			if result != tt.expected {
				t.Errorf("severityEmoji(%s) = %s, want %s", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestSlackChannel_FormatSlackBlocks(t *testing.T) {
	ch := &SlackChannel{
		name:    "test",
		channel: "#alerts",
	}

	alert := &Alert{
		ID:          "alert-123",
		Type:        AlertTypeTaskFailed,
		Severity:    SeverityCritical,
		Title:       "Critical Alert",
		Message:     "Something went wrong",
		Source:      "task:TASK-1",
		ProjectPath: "/my/project",
	}

	blocks := ch.formatSlackBlocks(alert)

	if len(blocks) < 2 {
		t.Errorf("expected at least 2 blocks, got %d", len(blocks))
	}

	// First block should be header
	if blocks[0].Type != "header" {
		t.Errorf("expected first block type 'header', got '%s'", blocks[0].Type)
	}
}

// =============================================================================
// TelegramChannel Tests (without real Telegram client)
// =============================================================================

func TestTelegramChannel_Name(t *testing.T) {
	ch := &TelegramChannel{
		name:   "my-telegram-channel",
		chatID: 123456789,
	}

	if ch.Name() != "my-telegram-channel" {
		t.Errorf("expected name 'my-telegram-channel', got '%s'", ch.Name())
	}
	if ch.Type() != "telegram" {
		t.Errorf("expected type 'telegram', got '%s'", ch.Type())
	}
}

func TestTelegramChannel_SeverityEmoji(t *testing.T) {
	ch := &TelegramChannel{}

	tests := []struct {
		severity Severity
		expected string
	}{
		{SeverityCritical, "\U0001F6A8"},
		{SeverityWarning, "\u26a0\ufe0f"},
		{SeverityInfo, "\u2139\ufe0f"},
	}

	for _, tt := range tests {
		t.Run(string(tt.severity), func(t *testing.T) {
			result := ch.severityEmoji(tt.severity)
			if result != tt.expected {
				t.Errorf("severityEmoji(%s) = %s, want %s", tt.severity, result, tt.expected)
			}
		})
	}
}

func TestTelegramChannel_FormatMessage(t *testing.T) {
	ch := &TelegramChannel{
		name:   "test",
		chatID: 123456789,
	}

	alert := &Alert{
		ID:          "alert-123",
		Type:        AlertTypeTaskFailed,
		Severity:    SeverityCritical,
		Title:       "Critical Alert",
		Message:     "Something went wrong",
		Source:      "task:TASK-1",
		ProjectPath: "/my/project",
		CreatedAt:   time.Now(),
	}

	msg := ch.formatMessage(alert)

	if msg == "" {
		t.Error("expected non-empty message")
	}

	// Should contain severity
	if len(msg) < 50 {
		t.Error("expected substantial message content")
	}
}

// TestTelegramChannel_UsesMarkdownV2_E4 verifies E4 (TASK-349): the channel sends
// with parse_mode=MarkdownV2 to match escapeMarkdown's MarkdownV2 escaping, so a
// period/'!'-heavy cost alert isn't rejected with "can't parse entities".
func TestTelegramChannel_UsesMarkdownV2_E4(t *testing.T) {
	var gotParseMode string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ParseMode string `json:"parse_mode"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		gotParseMode = req.ParseMode
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer server.Close()

	client := telegram.NewClientWithBaseURL("test-telegram-token", server.URL)
	ch := NewTelegramChannel("test", client, 123456789)

	alert := &Alert{
		ID:          "a1",
		Type:        AlertTypeTaskFailed,
		Severity:    SeverityCritical,
		Title:       "Daily spend $50.00 exceeds threshold $25.00!",
		Message:     "cost-limit - investigate now.",
		Source:      "task:TASK-1",
		ProjectPath: "/my/project",
		CreatedAt:   time.Now(),
	}
	if err := ch.Send(context.Background(), alert); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if gotParseMode != "MarkdownV2" {
		t.Errorf("parse_mode = %q, want MarkdownV2", gotParseMode)
	}
}

// TestEscapeMarkdown_CoversMarkdownV2Specials_E4 verifies escapeMarkdown escapes the
// full MarkdownV2 metacharacter set (the premise that makes the parse-mode switch correct).
func TestEscapeMarkdown_CoversMarkdownV2Specials_E4(t *testing.T) {
	out := escapeMarkdown("Daily spend $50.00!")
	if !strings.Contains(out, `50\.00`) || !strings.HasSuffix(out, `\!`) {
		t.Errorf("period/'!' not escaped for MarkdownV2: %q", out)
	}
	for _, r := range "_*[]()~`>#+-=|{}.!" {
		if got := escapeMarkdown(string(r)); got != "\\"+string(r) {
			t.Errorf("escapeMarkdown(%q) = %q, want escaped", string(r), got)
		}
	}
}
