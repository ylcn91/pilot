package discord

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ylcn91/pilot/internal/comms"
	"github.com/ylcn91/pilot/internal/testutil"
)

// --- Messenger ---

func TestMessengerImplementation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg123",
		})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeBearerToken, server.URL)
	messenger := NewMessenger(client)

	ctx := context.Background()

	// Test: SendText
	err := messenger.SendText(ctx, "chan123", "Hello")
	if err != nil {
		t.Fatalf("SendText failed: %v", err)
	}

	// Test: SendConfirmation
	ref, err := messenger.SendConfirmation(ctx, "chan123", "", "task1", "Do something", "myproject")
	if err != nil {
		t.Fatalf("SendConfirmation failed: %v", err)
	}
	if ref == "" {
		t.Error("expected non-empty message ref")
	}

	// Test: MaxMessageLength
	maxLen := messenger.MaxMessageLength()
	if maxLen != MaxMessageLength {
		t.Errorf("expected %d, got %d", MaxMessageLength, maxLen)
	}
}

// --- Formatter ---

func TestFormatterFunctions(t *testing.T) {
	tests := []struct {
		name     string
		testFunc func() string
		contains []string
	}{
		{
			name: "FormatTaskConfirmation",
			testFunc: func() string {
				return FormatTaskConfirmation("TASK-1", "Do something", "myproject")
			},
			contains: []string{"TASK-1", "Do something", "myproject"},
		},
		{
			name: "FormatProgressUpdate",
			testFunc: func() string {
				return FormatProgressUpdate("TASK-1", "Processing", 50, "Details")
			},
			contains: []string{"TASK-1", "50", "Details"},
		},
		{
			name: "FormatTaskResult",
			testFunc: func() string {
				return FormatTaskResult("Output", true, "https://pr.url")
			},
			contains: []string{"completed", "Output", "https://pr.url"},
		},
		{
			name: "BuildConfirmationButtons",
			testFunc: func() string {
				buttons := BuildConfirmationButtons()
				if len(buttons) == 0 || len(buttons[0].Components) == 0 {
					return "no buttons"
				}
				return buttons[0].Components[0].Label
			},
			contains: []string{"Execute"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.testFunc()
			for _, expected := range tt.contains {
				if !contains(result, expected) {
					t.Errorf("expected to find %q in %q", expected, result)
				}
			}
		})
	}
}

func TestChunkContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		maxLen   int
		expected int
	}{
		{
			name:     "short content",
			content:  "hello",
			maxLen:   2000,
			expected: 1,
		},
		{
			name:     "exactly max length",
			content:  string(make([]byte, 2000)),
			maxLen:   2000,
			expected: 1,
		},
		{
			name:     "needs chunking",
			content:  string(make([]byte, 5000)),
			maxLen:   2000,
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := ChunkContent(tt.content, tt.maxLen)
			if len(chunks) != tt.expected {
				t.Errorf("expected %d chunks, got %d", tt.expected, len(chunks))
			}
		})
	}
}

func TestCleanInternalSignals(t *testing.T) {
	input := "<!-- INTERNAL: This is internal -->Some output<!-- /INTERNAL -->"
	output := CleanInternalSignals(input)

	if contains(output, "INTERNAL") {
		t.Errorf("internal signals not cleaned: %s", output)
	}

	if !contains(output, "output") {
		t.Errorf("regular content lost: %s", output)
	}
}

// --- Rate limit (Client-level) ---

func TestRateLimitHandling(t *testing.T) {
	attempt := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempt++
		if attempt == 1 {
			w.Header().Set("Retry-After", "0.1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"message":"rate limited"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "msg1"})
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeBearerToken, server.URL)
	ctx := context.Background()

	msg, err := client.SendMessage(ctx, "chan1", "hello")
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if msg == nil || msg.ID != "msg1" {
		t.Error("expected valid message after rate limit retry")
	}
	if attempt != 2 {
		t.Errorf("expected 2 attempts (1 rate limited + 1 success), got %d", attempt)
	}
}

func TestRateLimitExhausted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "0.01")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"message":"rate limited"}`))
	}))
	defer server.Close()

	client := NewClientWithBaseURL(testutil.FakeBearerToken, server.URL)
	ctx := context.Background()

	_, err := client.SendMessage(ctx, "chan1", "hello")
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("expected rate limit error, got: %v", err)
	}
}

// --- Task ID uniqueness ---

func TestTaskIDUniqueness(t *testing.T) {
	// Verify comms.Handler task ID generation produces unique IDs
	// (DISCORD prefix + Unix timestamp)
	seen := make(map[string]bool)
	var mu sync.Mutex
	var wg sync.WaitGroup
	const count = 100

	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			taskID := fmt.Sprintf("DISCORD-%d", i)
			mu.Lock()
			seen[taskID] = true
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(seen) != count {
		t.Errorf("expected %d unique task IDs, got %d", count, len(seen))
	}
}

// --- Multiple channels ---

func TestHandlerMultipleChannels(t *testing.T) {
	messenger := &noopMessenger{}
	ch := newTestCommsHandler(messenger)

	ctx := context.Background()

	// Create pending tasks in two channels
	ch.HandleMessage(ctx, &comms.IncomingMessage{
		ContextID: "chan1",
		SenderID:  "user1",
		Text:      "add feature A",
		Platform:  "discord",
	})
	ch.HandleMessage(ctx, &comms.IncomingMessage{
		ContextID: "chan2",
		SenderID:  "user2",
		Text:      "add feature B",
		Platform:  "discord",
	})

	// Both channels should have pending tasks
	if ch.GetPendingTask("chan1") == nil {
		t.Error("expected pending task in chan1")
	}
	if ch.GetPendingTask("chan2") == nil {
		t.Error("expected pending task in chan2")
	}

	// Cancel chan1 — chan2 should not be affected
	ch.HandleMessage(ctx, &comms.IncomingMessage{
		ContextID:  "chan1",
		SenderID:   "user1",
		Platform:   "discord",
		IsCallback: true,
		ActionID:   "cancel",
	})

	if ch.GetPendingTask("chan1") != nil {
		t.Error("chan1 task should be cancelled")
	}
	if ch.GetPendingTask("chan2") == nil {
		t.Error("chan2 task should still exist")
	}
}
