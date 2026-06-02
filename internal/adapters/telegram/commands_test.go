package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/testutil"
)

// mockTelegramServer creates a test server that captures sent messages
type mockTelegramServer struct {
	server        *httptest.Server
	sentMessages  []string
	sentKeyboards [][]InlineKeyboardButton
}

func newMockTelegramServer() *mockTelegramServer {
	m := &mockTelegramServer{
		sentMessages:  []string{},
		sentKeyboards: [][]InlineKeyboardButton{},
	}

	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse the request to capture sent messages
		if strings.Contains(r.URL.Path, "/sendMessage") {
			var req SendMessageRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err == nil {
				m.sentMessages = append(m.sentMessages, req.Text)
				if req.ReplyMarkup != nil {
					m.sentKeyboards = append(m.sentKeyboards, req.ReplyMarkup.InlineKeyboard...)
				}
			}
		}

		// Return success response
		response := SendMessageResponse{
			OK: true,
			Result: &Result{
				MessageID: 123,
				ChatID:    456,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))

	return m
}

func (m *mockTelegramServer) close() {
	m.server.Close()
}

// newTestHandlerForCommands creates a Handler wired with a commsHandler for command tests.
func newTestHandlerForCommands(projects comms.ProjectSource, projectPath string) *Handler {
	ch := comms.NewHandler(&comms.HandlerConfig{
		Messenger:    &noopMessenger{},
		Projects:     projects,
		ProjectPath:  projectPath,
		TaskIDPrefix: "TG",
	})
	return &Handler{
		client:       NewClient(testutil.FakeTelegramBotToken),
		projects:     projects,
		projectPath:  projectPath,
		commsHandler: ch,
	}
}

// TestCommandHandler_HandleHelp tests the /help command
func TestCommandHandler_HandleHelp(t *testing.T) {
	mock := newMockTelegramServer()
	defer mock.close()

	h := newTestHandlerForCommands(nil, "/test/path")
	cmd := NewCommandHandler(h, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "123", "/help")

	// Check that message was formatted (we can't check exact content due to mock server)
	// The handler will try to send but the mock server won't match URLs
	// This test primarily validates that no panic occurs
}

// TestCommandHandler_HandleStatus tests the /status command
func TestCommandHandler_HandleStatus(t *testing.T) {
	h := newTestHandlerForCommands(nil, "/test/path")
	cmd := NewCommandHandler(h, nil)

	tests := []struct {
		name   string
		chatID string
	}{
		{
			name:   "no running tasks",
			chatID: "chat1",
		},
		{
			name:   "different chat",
			chatID: "chat2",
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This will fail to send (no real Telegram) but should not panic
			cmd.HandleCommand(ctx, tt.chatID, "/status")
		})
	}
}

// TestCommandHandler_HandleCancel tests the /cancel command
func TestCommandHandler_HandleCancel(t *testing.T) {
	h := newTestHandlerForCommands(nil, "/test/path")
	cmd := NewCommandHandler(h, nil)

	tests := []struct {
		name   string
		chatID string
	}{
		{
			name:   "nothing to cancel",
			chatID: "chat1",
		},
		{
			name:   "different chat",
			chatID: "chat2",
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd.HandleCommand(ctx, tt.chatID, "/cancel")
			// Verify no panic; cancel state managed by commsHandler
		})
	}
}

// TestCommandHandler_HandleQueue tests the /queue command
func TestCommandHandler_HandleQueue(t *testing.T) {
	h := newTestHandlerForCommands(nil, "/test/path")

	tests := []struct {
		name     string
		store    bool
		hasQueue bool
	}{
		{
			name:     "no store",
			store:    false,
			hasQueue: false,
		},
		// Note: Testing with actual store would require database setup
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cmd *CommandHandler
			if tt.store {
				// Would need actual store
				cmd = NewCommandHandler(h, nil)
			} else {
				cmd = NewCommandHandler(h, nil)
			}

			cmd.HandleCommand(ctx, "chat1", "/queue")
			// Just verify no panic
		})
	}
}

// TestCommandHandler_HandleProjects tests the /projects command
func TestCommandHandler_HandleProjects(t *testing.T) {
	tests := []struct {
		name     string
		projects comms.ProjectSource
	}{
		{
			name:     "no projects configured",
			projects: nil,
		},
		{
			name: "with projects",
			projects: &MockProjectSource{
				projects: []*comms.ProjectInfo{
					{Name: "project-a", Path: "/path/a", Navigator: true},
					{Name: "project-b", Path: "/path/b", Navigator: false},
				},
			},
		},
		{
			name: "empty project list",
			projects: &MockProjectSource{
				projects: []*comms.ProjectInfo{},
			},
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newTestHandlerForCommands(tt.projects, "/default/path")
			cmd := NewCommandHandler(h, nil)

			cmd.HandleCommand(ctx, "chat1", "/projects")
			// Just verify no panic
		})
	}
}

// TestCommandHandler_HandleSwitch tests the /switch command
func TestCommandHandler_HandleSwitch(t *testing.T) {
	projects := &MockProjectSource{
		projects: []*comms.ProjectInfo{
			{Name: "project-a", Path: "/path/a"},
			{Name: "project-b", Path: "/path/b"},
		},
	}

	h := newTestHandlerForCommands(projects, "/path/a")
	cmd := NewCommandHandler(h, nil)

	tests := []struct {
		name     string
		command  string
		wantPath string
	}{
		{
			name:     "switch to existing project",
			command:  "/switch project-b",
			wantPath: "/path/b",
		},
		{
			name:     "switch to non-existent project",
			command:  "/switch unknown",
			wantPath: "/path/b", // Stays at last known project (from previous subtest)
		},
		{
			name:     "show current project",
			command:  "/switch",
			wantPath: "/path/b", // Should show current
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd.HandleCommand(ctx, "chat1", tt.command)

			path := h.getActiveProjectPath("chat1")
			if path != tt.wantPath {
				t.Errorf("active project path = %q, want %q", path, tt.wantPath)
			}
		})
	}
}

// TestCommandHandler_HandleHistory tests the /history command
func TestCommandHandler_HandleHistory(t *testing.T) {
	h := newTestHandlerForCommands(nil, "/test/path")

	tests := []struct {
		name  string
		store bool
	}{
		{
			name:  "no store",
			store: false,
		},
	}

	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cmd *CommandHandler
			if tt.store {
				cmd = NewCommandHandler(h, nil) // Would need actual store
			} else {
				cmd = NewCommandHandler(h, nil)
			}

			cmd.HandleCommand(ctx, "chat1", "/history")
			// Just verify no panic
		})
	}
}

// TestCommandHandler_HandleBudget tests the /budget command
func TestCommandHandler_HandleBudget(t *testing.T) {
	h := newTestHandlerForCommands(nil, "/test/path")
	cmd := NewCommandHandler(h, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/budget")
	// Just verify no panic
}

// TestCommandHandler_HandleTasks tests the /tasks command
func TestCommandHandler_HandleTasks(t *testing.T) {
	h := newTestHandlerForCommands(nil, "/nonexistent/path")
	cmd := NewCommandHandler(h, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/tasks")
	// Just verify no panic
}

// TestCommandHandler_UnknownCommand tests handling of unknown commands
func TestCommandHandler_UnknownCommand(t *testing.T) {
	h := newTestHandlerForCommands(nil, "/test/path")
	cmd := NewCommandHandler(h, nil)

	ctx := context.Background()
	cmd.HandleCommand(ctx, "chat1", "/unknown_command")
	// Just verify no panic
}
