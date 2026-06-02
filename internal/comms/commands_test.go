package comms

import (
	"context"
	"testing"

	"github.com/ylcn91/pilot/internal/memory"
)

// TestCommandHandler_HandleHelp tests the /help command.
func TestCommandHandler_HandleHelp(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/help")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if len(messenger.messages[0]) == 0 {
		t.Error("help message is empty")
	}

	if !containsString(messenger.messages[0], "Pilot Bot") {
		t.Error("help message missing bot name")
	}

	if !containsString(messenger.messages[0], "/status") {
		t.Error("help message missing /status command")
	}

	if !containsString(messenger.messages[0], "/queue") {
		t.Error("help message missing /queue command")
	}
}

// TestCommandHandler_HandleStart tests the /start command (alias for /help).
func TestCommandHandler_HandleStart(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/start")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if !containsString(messenger.messages[0], "Pilot Bot") {
		t.Error("/start should show help")
	}
}

// TestCommandHandler_HandleStatus tests the /status command.
func TestCommandHandler_HandleStatus(t *testing.T) {
	tests := []struct {
		name      string
		messenger *mockMessenger
		setupFunc func(cmd *CommandHandler)
		wantText  string
	}{
		{
			name:      "no functions configured",
			messenger: &mockMessenger{},
			setupFunc: func(cmd *CommandHandler) {},
			wantText:  "Status",
		},
		{
			name:      "with active project",
			messenger: &mockMessenger{},
			setupFunc: func(cmd *CommandHandler) {
				cmd.SetActiveProjectFunc(func(contextID string) (string, string) {
					return "MyProject", "/path/to/project"
				})
			},
			wantText: "MyProject",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewCommandHandler(tt.messenger, nil)
			tt.setupFunc(cmd)

			ctx := context.Background()
			cmd.HandleCommand(ctx, "chat1", "/status")

			if len(tt.messenger.messages) == 0 {
				t.Fatal("no messages sent")
			}

			if !containsString(tt.messenger.messages[0], tt.wantText) {
				t.Errorf("message missing %q: %s", tt.wantText, tt.messenger.messages[0])
			}
		})
	}
}

// TestCommandHandler_HandleQueue tests the /queue command.
func TestCommandHandler_HandleQueue(t *testing.T) {
	tests := []struct {
		name      string
		messenger *mockMessenger
		store     *memory.Store
		wantText  string
	}{
		{
			name:      "no store",
			messenger: &mockMessenger{},
			store:     nil,
			wantText:  "not available",
		},
		{
			name:      "empty queue",
			messenger: &mockMessenger{},
			store:     mustCreateMemoryStore(t),
			wantText:  "Queue is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := NewCommandHandler(tt.messenger, tt.store)

			ctx := context.Background()
			cmd.HandleCommand(ctx, "chat1", "/queue")

			if len(tt.messenger.messages) == 0 {
				t.Fatal("no messages sent")
			}

			if !containsString(tt.messenger.messages[0], tt.wantText) {
				t.Errorf("message missing %q: %s", tt.wantText, tt.messenger.messages[0])
			}
		})
	}
}

// TestCommandHandler_HandleProjects tests the /projects command.
func TestCommandHandler_HandleProjects(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	// No projects function configured
	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/projects")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if !containsString(messenger.messages[0], "not configured") {
		t.Error("message should indicate projects not configured")
	}
}

// TestCommandHandler_HandleTasks tests the /tasks command.
func TestCommandHandler_HandleTasks(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	// No list function configured
	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/tasks")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if !containsString(messenger.messages[0], "not found") {
		t.Error("message should indicate tasks not found")
	}
}

// TestCommandHandler_HandleCancel tests the /cancel command.
func TestCommandHandler_HandleCancel(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/cancel")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if !containsString(messenger.messages[0], "No task to cancel") {
		t.Error("message should indicate no task to cancel")
	}
}

// TestCommandHandler_HandleStop tests the /stop command.
func TestCommandHandler_HandleStop(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/stop")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if !containsString(messenger.messages[0], "No task") {
		t.Error("message should indicate no running task")
	}
}

// TestCommandHandler_HandleBudget tests the /budget command.
func TestCommandHandler_HandleBudget(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/budget")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if !containsString(messenger.messages[0], "not available") {
		t.Error("message should indicate budget not available without store")
	}
}

// TestCommandHandler_HandleHistory tests the /history command.
func TestCommandHandler_HandleHistory(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/history")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if !containsString(messenger.messages[0], "not available") {
		t.Error("message should indicate history not available without store")
	}
}

// TestCommandHandler_HandleBrief tests the /brief command.
func TestCommandHandler_HandleBrief(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/brief")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if !containsString(messenger.messages[0], "not available") {
		t.Error("message should indicate brief not available without store")
	}
}
