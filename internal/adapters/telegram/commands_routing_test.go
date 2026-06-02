package telegram

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/comms"
)

// TestFormatTimeAgo tests the time formatting helper
func TestFormatTimeAgo(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name     string
		time     time.Time
		expected string
	}{
		{
			name:     "just now",
			time:     now.Add(-30 * time.Second),
			expected: "just now",
		},
		{
			name:     "minutes ago",
			time:     now.Add(-5 * time.Minute),
			expected: "5m ago",
		},
		{
			name:     "hours ago",
			time:     now.Add(-3 * time.Hour),
			expected: "3h ago",
		},
		{
			name:     "days ago",
			time:     now.Add(-2 * 24 * time.Hour),
			expected: "2d ago",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTimeAgo(tt.time)
			if got != tt.expected {
				t.Errorf("formatTimeAgo() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestFormatTimeAgo_OldDates tests formatting of dates older than a week
func TestFormatTimeAgo_OldDates(t *testing.T) {
	// Dates older than a week should show as "Jan 2" format
	oldDate := time.Now().Add(-14 * 24 * time.Hour)
	got := formatTimeAgo(oldDate)

	// Should be in "Jan 2" format
	if !strings.Contains(got, " ") || len(got) < 4 {
		t.Errorf("formatTimeAgo() for old date = %q, expected date format", got)
	}
}

// TestNewCommandHandler tests command handler creation
func TestNewCommandHandler(t *testing.T) {
	h := newTestHandlerForCommands(nil, "/test/path")

	tests := []struct {
		name  string
		store bool
	}{
		{
			name:  "without store",
			store: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cmd *CommandHandler
			if tt.store {
				cmd = NewCommandHandler(h, nil)
			} else {
				cmd = NewCommandHandler(h, nil)
			}

			if cmd == nil {
				t.Fatal("NewCommandHandler returned nil")
			}
			if cmd.handler != h {
				t.Error("handler not set correctly")
			}
		})
	}
}

// TestCommandHandler_HandleCallbackSwitch tests callback-based project switching
func TestCommandHandler_HandleCallbackSwitch(t *testing.T) {
	projects := &MockProjectSource{
		projects: []*comms.ProjectInfo{
			{Name: "project-a", Path: "/path/a"},
			{Name: "project-b", Path: "/path/b"},
		},
	}

	h := newTestHandlerForCommands(projects, "/path/a")
	cmd := NewCommandHandler(h, nil)

	ctx := context.Background()

	// Set initial project
	_ = h.commsHandler.SetActiveProject("chat1", "project-a")

	cmd.HandleCallbackSwitch(ctx, "chat1", "project-b")

	path := h.getActiveProjectPath("chat1")
	if path != "/path/b" {
		t.Errorf("callback switch failed: path = %q, want %q", path, "/path/b")
	}
}

// TestCommandRouting tests that commands are routed correctly
func TestCommandRouting(t *testing.T) {
	projects := &MockProjectSource{
		projects: []*comms.ProjectInfo{
			{Name: "test", Path: "/test/path"},
		},
	}
	h := newTestHandlerForCommands(projects, "/test/path")
	cmd := NewCommandHandler(h, nil)

	commands := []string{
		"/help",
		"/start",
		"/status",
		"/cancel",
		"/queue",
		"/projects",
		"/project",
		"/project test",
		"/switch",
		"/switch test",
		"/history",
		"/budget",
		"/tasks",
		"/list",
		"/stop",
	}

	ctx := context.Background()
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			// Should not panic
			cmd.HandleCommand(ctx, "chat1", command)
		})
	}
}
