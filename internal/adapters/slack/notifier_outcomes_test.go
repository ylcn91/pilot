package slack

import (
	"context"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

// TestNotifierTaskCompleted tests the TaskCompleted method
func TestNotifierTaskCompleted(t *testing.T) {
	tests := []struct {
		name      string
		taskID    string
		title     string
		prURL     string
		wantParts []string
		wantColor bool
	}{
		{
			name:      "without PR",
			taskID:    "TASK-01",
			title:     "Add user authentication",
			prURL:     "",
			wantParts: []string{"TASK-01", "authentication", "completed"},
			wantColor: true,
		},
		{
			name:      "with PR URL",
			taskID:    "TASK-02",
			title:     "Refactor database layer",
			prURL:     "https://github.com/org/repo/pull/123",
			wantParts: []string{"TASK-02", "database", "github.com"},
			wantColor: true,
		},
		{
			name:      "empty title with PR",
			taskID:    "T-1",
			title:     "",
			prURL:     "https://github.com/org/repo/pull/1",
			wantParts: []string{"T-1", "PR"},
			wantColor: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := createMockSlackServer(t, func(msg *Message) {
				if len(msg.Blocks) == 0 {
					t.Error("expected blocks in message")
				}

				// Check for attachments with color
				if tt.wantColor && len(msg.Attachments) == 0 {
					t.Error("expected attachments for completed task")
				}

				if len(msg.Attachments) > 0 && msg.Attachments[0].Color != "good" {
					t.Errorf("attachment color = %q, want good", msg.Attachments[0].Color)
				}
			})
			defer server.Close()

			notifier := NewNotifier(&Config{
				BotToken: testutil.FakeSlackBotToken,
				Channel:  "#test",
			})

			ctx := context.Background()
			_ = notifier.TaskCompleted(ctx, tt.taskID, tt.title, tt.prURL)
		})
	}
}

// TestNotifierTaskFailed tests the TaskFailed method
func TestNotifierTaskFailed(t *testing.T) {
	tests := []struct {
		name      string
		taskID    string
		title     string
		errorMsg  string
		wantParts []string
		wantColor string
	}{
		{
			name:      "simple error",
			taskID:    "TASK-01",
			title:     "Deploy to production",
			errorMsg:  "Build failed",
			wantParts: []string{"TASK-01", "failed", "Build failed"},
			wantColor: "danger",
		},
		{
			name:      "multiline error",
			taskID:    "TASK-02",
			title:     "Run test suite",
			errorMsg:  "Error: test failed\nExpected: true\nGot: false",
			wantParts: []string{"TASK-02", "Error", "Expected"},
			wantColor: "danger",
		},
		{
			name:      "error with special chars",
			taskID:    "T-99",
			title:     "Parse JSON",
			errorMsg:  "unexpected token '<' at position 0",
			wantParts: []string{"T-99", "unexpected token"},
			wantColor: "danger",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := createMockSlackServer(t, func(msg *Message) {
				if len(msg.Blocks) == 0 {
					t.Error("expected blocks in message")
				}

				// Check for danger color attachment
				if len(msg.Attachments) == 0 {
					t.Error("expected attachments for failed task")
				}

				if len(msg.Attachments) > 0 && msg.Attachments[0].Color != tt.wantColor {
					t.Errorf("attachment color = %q, want %q", msg.Attachments[0].Color, tt.wantColor)
				}

				var text string
				for _, block := range msg.Blocks {
					if block.Text != nil {
						text += block.Text.Text
					}
				}

				// Verify error message is in code block (```)
				if !strings.Contains(text, "```") {
					t.Error("expected error message in code block")
				}
			})
			defer server.Close()

			notifier := NewNotifier(&Config{
				BotToken: testutil.FakeSlackBotToken,
				Channel:  "#test",
			})

			ctx := context.Background()
			_ = notifier.TaskFailed(ctx, tt.taskID, tt.title, tt.errorMsg)
		})
	}
}

// TestNotifierPRReady tests the PRReady method
func TestNotifierPRReady(t *testing.T) {
	tests := []struct {
		name         string
		taskID       string
		title        string
		prURL        string
		filesChanged int
		wantParts    []string
		wantColor    string
	}{
		{
			name:         "single file changed",
			taskID:       "TASK-01",
			title:        "Fix typo in README",
			prURL:        "https://github.com/org/repo/pull/1",
			filesChanged: 1,
			wantParts:    []string{"TASK-01", "README", "1 files"},
			wantColor:    "#6366f1",
		},
		{
			name:         "multiple files changed",
			taskID:       "TASK-02",
			title:        "Major refactoring",
			prURL:        "https://github.com/org/repo/pull/42",
			filesChanged: 15,
			wantParts:    []string{"TASK-02", "refactoring", "15 files"},
			wantColor:    "#6366f1",
		},
		{
			name:         "zero files changed",
			taskID:       "T-1",
			title:        "Update config",
			prURL:        "https://github.com/org/repo/pull/99",
			filesChanged: 0,
			wantParts:    []string{"T-1", "0 files"},
			wantColor:    "#6366f1",
		},
		{
			name:         "large PR",
			taskID:       "EPIC-100",
			title:        "Complete rewrite",
			prURL:        "https://github.com/org/repo/pull/500",
			filesChanged: 200,
			wantParts:    []string{"EPIC-100", "200 files"},
			wantColor:    "#6366f1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := createMockSlackServer(t, func(msg *Message) {
				if len(msg.Blocks) == 0 {
					t.Error("expected blocks in message")
				}

				// Check for custom color attachment
				if len(msg.Attachments) == 0 {
					t.Error("expected attachments for PR ready")
				}

				if len(msg.Attachments) > 0 && msg.Attachments[0].Color != tt.wantColor {
					t.Errorf("attachment color = %q, want %q", msg.Attachments[0].Color, tt.wantColor)
				}

				var text string
				for _, block := range msg.Blocks {
					if block.Text != nil {
						text += block.Text.Text
					}
				}

				// Verify PR URL is included as a link
				if !strings.Contains(text, tt.prURL) {
					t.Errorf("message = %q, want to contain PR URL %q", text, tt.prURL)
				}
			})
			defer server.Close()

			notifier := NewNotifier(&Config{
				BotToken: testutil.FakeSlackBotToken,
				Channel:  "#test",
			})

			ctx := context.Background()
			_ = notifier.PRReady(ctx, tt.taskID, tt.title, tt.prURL, tt.filesChanged)
		})
	}
}

// TestNotifierMessageFormatting tests that messages are properly formatted
func TestNotifierMessageFormatting(t *testing.T) {
	tests := []struct {
		name      string
		method    string
		taskID    string
		title     string
		wantEmoji string
	}{
		{
			name:      "task started has rocket emoji",
			method:    "started",
			taskID:    "T-1",
			title:     "Test",
			wantEmoji: "🚀",
		},
		{
			name:      "task progress has timer emoji",
			method:    "progress",
			taskID:    "T-2",
			title:     "Test",
			wantEmoji: "⏳",
		},
		{
			name:      "task completed has checkmark emoji",
			method:    "completed",
			taskID:    "T-3",
			title:     "Test",
			wantEmoji: "✅",
		},
		{
			name:      "task failed has X emoji",
			method:    "failed",
			taskID:    "T-4",
			title:     "Test",
			wantEmoji: "❌",
		},
		{
			name:      "PR ready has bell emoji",
			method:    "pr_ready",
			taskID:    "T-5",
			title:     "Test",
			wantEmoji: "🔔",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := createMockSlackServer(t, func(msg *Message) {
				var text string
				for _, block := range msg.Blocks {
					if block.Text != nil {
						text += block.Text.Text
					}
				}

				if !strings.Contains(text, tt.wantEmoji) {
					t.Errorf("message = %q, want to contain emoji %s", text, tt.wantEmoji)
				}
			})
			defer server.Close()

			notifier := NewNotifier(&Config{
				BotToken: testutil.FakeSlackBotToken,
				Channel:  "#test",
			})

			ctx := context.Background()
			switch tt.method {
			case "started":
				_ = notifier.TaskStarted(ctx, tt.taskID, tt.title)
			case "progress":
				_ = notifier.TaskProgress(ctx, tt.taskID, tt.title, 50)
			case "completed":
				_ = notifier.TaskCompleted(ctx, tt.taskID, tt.title, "")
			case "failed":
				_ = notifier.TaskFailed(ctx, tt.taskID, tt.title, "error")
			case "pr_ready":
				_ = notifier.PRReady(ctx, tt.taskID, tt.title, "https://github.com/test", 1)
			}
		})
	}
}
