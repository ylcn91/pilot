package comms

import (
	"context"
	"testing"
)

// TestCommandHandler_HandleRun tests the /run command.
func TestCommandHandler_HandleRun(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		hasHandler bool
		wantText   string
	}{
		{
			name:       "without handler, no args",
			input:      "/run",
			hasHandler: false,
			wantText:   "Usage: /run",
		},
		{
			name:       "without handler, with args",
			input:      "/run 42",
			hasHandler: false,
			wantText:   "Usage: /run",
		},
		{
			name:       "with handler",
			input:      "/run 42",
			hasHandler: true,
			wantText:   "", // Handler is called instead
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messenger := &mockMessenger{}
			cmd := NewCommandHandler(messenger, nil)

			if tt.hasHandler {
				cmd.SetRunCommandFunc(func(ctx context.Context, contextID, taskID string) {
					// Just mark that handler was called
					_ = messenger.SendText(ctx, contextID, "Handler called with "+taskID)
				})
			}

			ctx := context.Background()
			cmd.HandleCommand(ctx, "chat1", tt.input)

			if len(messenger.messages) == 0 {
				t.Fatal("no messages sent")
			}

			if tt.wantText != "" && !containsString(messenger.messages[0], tt.wantText) {
				t.Errorf("message missing %q: %s", tt.wantText, messenger.messages[0])
			}
		})
	}
}

// TestNewCommandHandlerWithConfig verifies callbacks wired via the config
// struct behave identically to the same callbacks wired via SetXxxFunc.
func TestNewCommandHandlerWithConfig(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandlerWithConfig(messenger, nil, CommandHandlerConfig{
		RunCommandFunc: func(ctx context.Context, contextID, taskID string) {
			_ = messenger.SendText(ctx, contextID, "Handler called with "+taskID)
		},
	})

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/run 42")

	if len(messenger.messages) == 0 {
		t.Fatal("no messages sent")
	}
	if !containsString(messenger.messages[0], "Handler called with 42") {
		t.Errorf("config-wired run func not invoked: %s", messenger.messages[0])
	}
}

// TestNewCommandHandlerWithConfig_NilFallsThrough verifies a nil config field
// behaves exactly like never calling the corresponding setter.
func TestNewCommandHandlerWithConfig_NilFallsThrough(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandlerWithConfig(messenger, nil, CommandHandlerConfig{})

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/run 42")

	if len(messenger.messages) == 0 {
		t.Fatal("no messages sent")
	}
	if !containsString(messenger.messages[0], "Usage: /run") {
		t.Errorf("expected usage fallthrough, got: %s", messenger.messages[0])
	}
}

// TestCommandHandler_HandleSwitch tests the /switch command.
func TestCommandHandler_HandleSwitch(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		setupFunc func(cmd *CommandHandler)
		wantText  string
	}{
		{
			name:      "no setup",
			input:     "/switch myproject",
			setupFunc: func(cmd *CommandHandler) {},
			wantText:  "not configured",
		},
		{
			name:  "successful switch",
			input: "/switch myproject",
			setupFunc: func(cmd *CommandHandler) {
				cmd.SetSetProjectFunc(func(ctx, projectName string) error {
					return nil
				})
				cmd.SetActiveProjectFunc(func(contextID string) (string, string) {
					return "MyProject", "/path"
				})
			},
			wantText: "Switched",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messenger := &mockMessenger{}
			cmd := NewCommandHandler(messenger, nil)
			tt.setupFunc(cmd)

			ctx := context.Background()
			cmd.HandleCommand(ctx, "chat1", tt.input)

			if len(messenger.messages) == 0 {
				t.Fatal("no messages sent")
			}

			if !containsString(messenger.messages[0], tt.wantText) {
				t.Errorf("message missing %q: %s", tt.wantText, messenger.messages[0])
			}
		})
	}
}

// TestCommandHandler_HandleUnknown tests unknown commands.
func TestCommandHandler_HandleUnknown(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/unknown")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if !containsString(messenger.messages[0], "Unknown command") {
		t.Error("message should indicate unknown command")
	}

	if !containsString(messenger.messages[0], "/help") {
		t.Error("message should suggest using /help")
	}
}

// TestCommandHandler_HandleNoPR tests the /nopr command.
func TestCommandHandler_HandleNoPR(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/nopr create a new feature")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if !containsString(messenger.messages[0], "without PR") {
		t.Error("message should indicate task without PR")
	}
}

// TestCommandHandler_HandlePR tests the /pr command.
func TestCommandHandler_HandlePR(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/pr create a new feature")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if !containsString(messenger.messages[0], "with PR") {
		t.Error("message should indicate task with PR")
	}
}

// TestCommandHandler_CommandParsing tests various command formats.
func TestCommandHandler_CommandParsing(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		verify func(t *testing.T, messages []string)
	}{
		{
			name:  "command with extra whitespace",
			input: "  /help  ",
			verify: func(t *testing.T, messages []string) {
				if len(messages) != 1 {
					t.Error("should handle extra whitespace")
				}
			},
		},
		{
			name:  "command with multiple args",
			input: "/pr this is a very long task description",
			verify: func(t *testing.T, messages []string) {
				if !containsString(messages[0], "with PR") {
					t.Error("should handle multi-word args")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			messenger := &mockMessenger{}
			cmd := NewCommandHandler(messenger, nil)

			ctx := context.Background()
			cmd.HandleCommand(ctx, "chat1", tt.input)

			tt.verify(t, messenger.messages)
		})
	}
}

// TestCommandHandler_ListAlias tests /list as alias for /tasks.
func TestCommandHandler_ListAlias(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/list")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if !containsString(messenger.messages[0], "not found") {
		t.Error("/list should behave like /tasks")
	}
}

// TestCommandHandler_ProjectAlias tests /project as alias for /switch.
func TestCommandHandler_ProjectAlias(t *testing.T) {
	messenger := &mockMessenger{}
	cmd := NewCommandHandler(messenger, nil)

	ctx := context.Background()
	// /project without args should show current project
	cmd.HandleCommand(ctx, "chat1", "/project")

	if len(messenger.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messenger.messages))
	}

	if !containsString(messenger.messages[0], "Active") {
		t.Error("/project should show active project")
	}
}
