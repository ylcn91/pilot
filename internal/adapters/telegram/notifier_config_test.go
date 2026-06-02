package telegram

import (
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

// TestNewNotifier tests notifier creation
func TestNewNotifier(t *testing.T) {
	config := &Config{
		BotToken: testutil.FakeTelegramBotToken,
		ChatID:   "123456",
	}

	notifier := NewNotifier(config)

	if notifier == nil {
		t.Fatal("NewNotifier returned nil")
	}
	if notifier.chatID != "123456" {
		t.Errorf("chatID = %q, want %q", notifier.chatID, "123456")
	}
	if notifier.client == nil {
		t.Error("client is nil")
	}
}

// TestDefaultConfig tests the default config values
func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	if config == nil {
		t.Fatal("DefaultConfig returned nil")
	}
	if config.Enabled {
		t.Error("Enabled should be false by default")
	}
}

// TestGenerateProgressBar tests progress bar generation
func TestGenerateProgressBar(t *testing.T) {
	tests := []struct {
		progress int
		expected string
	}{
		{0, "░░░░░░░░░░"},
		{10, "█░░░░░░░░░"},
		{50, "█████░░░░░"},
		{100, "██████████"},
		// Note: negative values result in negative filled count, which doesn't break the function
		// but the result is implementation-dependent
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := generateProgressBar(tt.progress)
			if got != tt.expected {
				t.Errorf("generateProgressBar(%d) = %q, want %q", tt.progress, got, tt.expected)
			}
		})
	}
}

// TestConfigFields tests that config has expected fields
func TestConfigFields(t *testing.T) {
	config := &Config{
		Enabled:    true,
		BotToken:   "token",
		ChatID:     "chat",
		Polling:    true,
		AllowedIDs: []int64{123, 456},
	}

	if !config.Enabled {
		t.Error("Enabled should be true")
	}
	if config.BotToken != "token" {
		t.Errorf("BotToken = %q, want %q", config.BotToken, "token")
	}
	if config.ChatID != "chat" {
		t.Errorf("ChatID = %q, want %q", config.ChatID, "chat")
	}
	if !config.Polling {
		t.Error("Polling should be true")
	}
	if len(config.AllowedIDs) != 2 {
		t.Errorf("AllowedIDs len = %d, want 2", len(config.AllowedIDs))
	}
}

// TestEscapeMarkdownNotifier tests markdown escaping for notifications
func TestEscapeMarkdownNotifier(t *testing.T) {
	tests := []struct {
		input   string
		wantEsc []string // substrings that should be escaped
	}{
		{
			input:   "hello_world",
			wantEsc: []string{"\\_"},
		},
		{
			input:   "*bold* text",
			wantEsc: []string{"\\*"},
		},
		{
			input:   "[link](url)",
			wantEsc: []string{"\\[", "\\]", "\\(", "\\)"},
		},
		{
			input:   "plain text",
			wantEsc: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := escapeMarkdown(tt.input)
			for _, esc := range tt.wantEsc {
				if !strings.Contains(got, esc) {
					t.Errorf("escapeMarkdown(%q) = %q, want to contain %q", tt.input, got, esc)
				}
			}
		})
	}
}

// TestDefaultApprovalConfig asserts approval is enabled by default for back-compat
func TestDefaultApprovalConfig(t *testing.T) {
	cfg := DefaultApprovalConfig()
	if cfg == nil {
		t.Fatal("DefaultApprovalConfig returned nil")
	}
	if !cfg.Enabled {
		t.Error("Enabled should be true by default")
	}
}

// TestDefaultConfigPlainTextMode tests that PlainTextMode defaults to true
func TestDefaultConfigPlainTextMode(t *testing.T) {
	config := DefaultConfig()

	if !config.PlainTextMode {
		t.Error("PlainTextMode should default to true for better messaging app compatibility")
	}
}

// TestNotifierGetParseMode tests the getParseMode helper
func TestNotifierGetParseMode(t *testing.T) {
	tests := []struct {
		name          string
		plainTextMode bool
		want          string
	}{
		{
			name:          "plain text mode enabled",
			plainTextMode: true,
			want:          "",
		},
		{
			name:          "plain text mode disabled (markdown)",
			plainTextMode: false,
			want:          "Markdown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			notifier := &Notifier{
				plainTextMode: tt.plainTextMode,
			}
			got := notifier.getParseMode()
			if got != tt.want {
				t.Errorf("getParseMode() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestNotifierPlainTextModeConfig tests that notifier correctly inherits PlainTextMode from config
func TestNotifierPlainTextModeConfig(t *testing.T) {
	tests := []struct {
		name          string
		plainTextMode bool
	}{
		{
			name:          "plain text enabled",
			plainTextMode: true,
		},
		{
			name:          "plain text disabled",
			plainTextMode: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				BotToken:      testutil.FakeTelegramBotToken,
				ChatID:        "123456",
				PlainTextMode: tt.plainTextMode,
			}

			notifier := NewNotifier(config)

			if notifier.plainTextMode != tt.plainTextMode {
				t.Errorf("notifier.plainTextMode = %v, want %v", notifier.plainTextMode, tt.plainTextMode)
			}
		})
	}
}
