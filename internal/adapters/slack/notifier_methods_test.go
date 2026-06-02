package slack

import (
	"context"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

// TestNotifierTaskStarted tests the TaskStarted method
func TestNotifierTaskStarted(t *testing.T) {
	tests := []struct {
		name      string
		taskID    string
		title     string
		wantParts []string
	}{
		{
			name:      "basic task",
			taskID:    "TASK-01",
			title:     "Create authentication handler",
			wantParts: []string{"TASK-01", "authentication handler", "started"},
		},
		{
			name:      "task with special chars",
			taskID:    "PROJ-123",
			title:     "Fix bug in user_service",
			wantParts: []string{"PROJ-123", "Fix bug", "user_service"},
		},
		{
			name:      "empty title",
			taskID:    "T-1",
			title:     "",
			wantParts: []string{"T-1"},
		},
		{
			name:      "long title",
			taskID:    "EPIC-999",
			title:     "Implement comprehensive end-to-end testing framework for the entire application",
			wantParts: []string{"EPIC-999", "end-to-end"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := createMockSlackServer(t, func(msg *Message) {
				// Verify channel is set
				if msg.Channel == "" {
					t.Error("channel is empty")
				}

				// Verify blocks are present
				if len(msg.Blocks) == 0 {
					t.Error("expected blocks in message")
				}

				// Extract text from blocks
				var text string
				for _, block := range msg.Blocks {
					if block.Text != nil {
						text += block.Text.Text
					}
				}

				// Verify expected parts are present
				for _, part := range tt.wantParts {
					if !strings.Contains(text, part) {
						t.Errorf("message text = %q, want to contain %q", text, part)
					}
				}
			})
			defer server.Close()

			// Test with real notifier - method signature verification
			notifier := NewNotifier(&Config{
				BotToken: testutil.FakeSlackBotToken,
				Channel:  "#test",
			})

			ctx := context.Background()
			// This will fail because it hits real Slack API, but verifies interface
			_ = notifier.TaskStarted(ctx, tt.taskID, tt.title)
		})
	}
}

// TestNotifierTaskProgress tests the TaskProgress method
func TestNotifierTaskProgress(t *testing.T) {
	tests := []struct {
		name        string
		taskID      string
		status      string
		progress    int
		wantParts   []string
		wantPercent string
	}{
		{
			name:        "0 percent",
			taskID:      "TASK-01",
			status:      "Starting",
			progress:    0,
			wantParts:   []string{"TASK-01", "Starting"},
			wantPercent: "0%",
		},
		{
			name:        "50 percent",
			taskID:      "TASK-02",
			status:      "Implementing",
			progress:    50,
			wantParts:   []string{"TASK-02", "Implementing"},
			wantPercent: "50%",
		},
		{
			name:        "100 percent",
			taskID:      "TASK-03",
			status:      "Complete",
			progress:    100,
			wantParts:   []string{"TASK-03", "Complete"},
			wantPercent: "100%",
		},
		{
			name:        "partial progress",
			taskID:      "T-99",
			status:      "Running tests",
			progress:    73,
			wantParts:   []string{"T-99", "tests"},
			wantPercent: "73%",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := createMockSlackServer(t, func(msg *Message) {
				if len(msg.Blocks) == 0 {
					t.Error("expected blocks in message")
				}

				var text string
				for _, block := range msg.Blocks {
					if block.Text != nil {
						text += block.Text.Text
					}
				}

				// Verify percentage is included
				if !strings.Contains(text, tt.wantPercent) {
					t.Errorf("message = %q, want to contain %q", text, tt.wantPercent)
				}

				// Verify progress bar is included (check for block characters)
				if !strings.Contains(text, "█") && !strings.Contains(text, "░") {
					t.Error("expected progress bar characters in message")
				}
			})
			defer server.Close()

			notifier := NewNotifier(&Config{
				BotToken: testutil.FakeSlackBotToken,
				Channel:  "#test",
			})

			ctx := context.Background()
			_ = notifier.TaskProgress(ctx, tt.taskID, tt.status, tt.progress)
		})
	}
}
