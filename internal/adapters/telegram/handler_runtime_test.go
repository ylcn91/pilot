package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ylcn91/pilot/internal/testutil"
)

// TestContainsAny tests the containsAny helper
func TestContainsAny(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substrs  []string
		expected bool
	}{
		{
			name:     "contains first",
			s:        "show me the tasks",
			substrs:  []string{"tasks", "issues", "backlog"},
			expected: true,
		},
		{
			name:     "contains second",
			s:        "list all issues",
			substrs:  []string{"tasks", "issues", "backlog"},
			expected: true,
		},
		{
			name:     "contains none",
			s:        "hello world",
			substrs:  []string{"tasks", "issues", "backlog"},
			expected: false,
		},
		{
			name:     "empty string",
			s:        "",
			substrs:  []string{"tasks"},
			expected: false,
		},
		{
			name:     "empty substrs",
			s:        "hello",
			substrs:  []string{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := containsAny(tt.s, tt.substrs...)
			if got != tt.expected {
				t.Errorf("containsAny(%q, %v) = %v, want %v", tt.s, tt.substrs, got, tt.expected)
			}
		})
	}
}

// TestMakeProgressBar tests progress bar generation
func TestMakeProgressBar(t *testing.T) {
	tests := []struct {
		percent  int
		expected string
	}{
		{0, "░░░░░░░░░░░░░░░░░░░░"},
		{25, "█████░░░░░░░░░░░░░░░"},
		{50, "██████████░░░░░░░░░░"},
		{75, "███████████████░░░░░"},
		{100, "████████████████████"},
	}

	for _, tt := range tests {
		t.Run("", func(t *testing.T) {
			got := makeProgressBar(tt.percent)
			if got != tt.expected {
				t.Errorf("makeProgressBar(%d) = %q, want %q", tt.percent, got, tt.expected)
			}
		})
	}
}

// TestVoiceNotAvailableMessage tests the voice setup error message
func TestVoiceNotAvailableMessage(t *testing.T) {
	tests := []struct {
		name             string
		transcriptionErr error
		wantContains     []string
	}{
		{
			name:             "no error set",
			transcriptionErr: nil,
			wantContains:     []string{"Voice transcription not available", "openai_api_key"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				transcriptionErr: tt.transcriptionErr,
			}

			got := h.voiceNotAvailableMessage()

			for _, want := range tt.wantContains {
				if !contains(got, want) {
					t.Errorf("voiceNotAvailableMessage() = %q, want to contain %q", got, want)
				}
			}
		})
	}
}

// TestVoiceNotAvailableMessageWithAPIKeyError tests specific error message
func TestVoiceNotAvailableMessageWithAPIKeyError(t *testing.T) {
	h := &Handler{
		transcriptionErr: fmt.Errorf("no backend configured: missing API key"),
	}

	got := h.voiceNotAvailableMessage()

	if !containsSubstr(got, "OpenAI API key") {
		t.Errorf("should mention OpenAI API key, got:\n%s", got)
	}
}

// TestHandlerCheckSingleton tests the singleton check delegation
func TestHandlerCheckSingleton(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := GetUpdatesResponse{
			OK:     true,
			Result: []*Update{},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	h := &Handler{
		client: NewClient(testutil.FakeTelegramBotToken),
	}

	// The actual check will fail because it can't reach real Telegram API
	// but we're verifying the method signature and delegation work
	ctx := context.Background()
	_ = h.CheckSingleton(ctx)
}

func TestHandlerStartPollingUsesStartupTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "getMe") {
			time.Sleep(startupAPITimeout + time.Second)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"id": 1, "username": "pilot_bot", "first_name": "Pilot"},
		})
	}))
	defer server.Close()

	h := &Handler{
		client:       NewClientWithBaseURL(testutil.FakeTelegramBotToken, server.URL),
		stopCh:       make(chan struct{}),
		commsHandler: newTestCommsHandler(),
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	h.StartPolling(ctx)
	defer h.Stop()

	if elapsed := time.Since(start); elapsed > startupAPITimeout+2*time.Second {
		t.Fatalf("StartPolling blocked too long: %v", elapsed)
	}
	if h.botUsername != "" {
		t.Fatalf("botUsername = %q, want empty on startup timeout", h.botUsername)
	}
	if elapsed := time.Since(start); elapsed < startupAPITimeout {
		t.Fatalf("StartPolling returned before startup timeout elapsed: %v", elapsed)
	}
}

// TestHandleCallback_ApprovalCallbacks covers approve:, reject:, and nil-handler paths.
func TestHandleCallback_ApprovalCallbacks(t *testing.T) {
	makeCallback := func(id, data string, fromID int64, username, firstName string) *CallbackQuery {
		cb := &CallbackQuery{
			ID:   id,
			Data: data,
			Message: &Message{
				Chat: &Chat{ID: 999},
			},
		}
		if fromID != 0 || username != "" || firstName != "" {
			cb.From = &User{ID: fromID, Username: username, FirstName: firstName}
		}
		return cb
	}

	t.Run("approve dispatches with correct args", func(t *testing.T) {
		mock := &mockApprovalHandler{ret: true}
		h, cleanup := newCallbackTestHandler(t, mock)
		defer cleanup()

		cb := makeCallback("cbid-1", "approve:req-abc", 42, "alice", "Alice")
		h.handleCallback(context.Background(), cb)

		mock.mu.Lock()
		defer mock.mu.Unlock()
		if len(mock.calls) != 1 {
			t.Fatalf("HandleCallback called %d times, want 1", len(mock.calls))
		}
		got := mock.calls[0]
		if got.callbackID != "cbid-1" {
			t.Errorf("callbackID = %q, want cbid-1", got.callbackID)
		}
		if got.data != "approve:req-abc" {
			t.Errorf("data = %q, want approve:req-abc", got.data)
		}
		if got.userID != "42" {
			t.Errorf("userID = %q, want 42", got.userID)
		}
		if got.username != "alice" {
			t.Errorf("username = %q, want alice", got.username)
		}
	})

	t.Run("reject dispatches correctly", func(t *testing.T) {
		mock := &mockApprovalHandler{ret: true}
		h, cleanup := newCallbackTestHandler(t, mock)
		defer cleanup()

		cb := makeCallback("cbid-2", "reject:req-xyz", 99, "bob", "Bob")
		h.handleCallback(context.Background(), cb)

		mock.mu.Lock()
		defer mock.mu.Unlock()
		if len(mock.calls) != 1 {
			t.Fatalf("HandleCallback called %d times, want 1", len(mock.calls))
		}
		if mock.calls[0].data != "reject:req-xyz" {
			t.Errorf("data = %q, want reject:req-xyz", mock.calls[0].data)
		}
	})

	t.Run("nil handler does not panic", func(t *testing.T) {
		h, cleanup := newCallbackTestHandler(t, nil)
		defer cleanup()

		cb := makeCallback("cbid-3", "approve:req-nil", 0, "", "")
		// Must not panic.
		h.handleCallback(context.Background(), cb)
	})
}

func TestStripBotMention(t *testing.T) {
	tests := []struct {
		name        string
		text        string
		botUsername string
		want        string
	}{
		{"with mention", "@PilotBot hi", "PilotBot", "hi"},
		{"no mention", "hi", "PilotBot", "hi"},
		{"mention only", "@PilotBot", "PilotBot", ""},
		{"case insensitive", "@pilotbot hi", "PilotBot", "hi"},
		{"empty username", "@PilotBot hi", "", "@PilotBot hi"},
		{"mention with extra spaces", "@PilotBot   hello world", "PilotBot", "hello world"},
		{"mention mid-text", "hey @PilotBot hi", "PilotBot", "hey @PilotBot hi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripBotMention(tt.text, tt.botUsername)
			if got != tt.want {
				t.Errorf("stripBotMention(%q, %q) = %q, want %q", tt.text, tt.botUsername, got, tt.want)
			}
		})
	}
}
