package discord

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

// --- Mention stripping ---

func TestMentionStripping(t *testing.T) {
	tests := []struct {
		name     string
		botID    string
		content  string
		expected string
	}{
		{
			name:     "strip bot mention",
			botID:    "123456789",
			content:  "<@123456789> deploy the thing",
			expected: "deploy the thing",
		},
		{
			name:     "strip nickname mention",
			botID:    "123456789",
			content:  "<@!123456789> deploy the thing",
			expected: "deploy the thing",
		},
		{
			name:     "no mention",
			botID:    "123456789",
			content:  "deploy the thing",
			expected: "deploy the thing",
		},
		{
			name:     "different user mention preserved",
			botID:    "123456789",
			content:  "<@987654321> deploy the thing",
			expected: "<@987654321> deploy the thing",
		},
		{
			name:     "empty bot ID strips leading mention (fallback)",
			botID:    "",
			content:  "<@123456789> deploy the thing",
			expected: "deploy the thing",
		},
		{
			name:     "empty bot ID strips nickname mention (fallback)",
			botID:    "",
			content:  "<@!123456789> deploy the thing",
			expected: "deploy the thing",
		},
		{
			name:     "mention only",
			botID:    "123456789",
			content:  "<@123456789>",
			expected: "",
		},
		{
			name:     "empty bot ID mention only",
			botID:    "",
			content:  "<@123456789>",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(&HandlerConfig{
				BotToken: testutil.FakeBearerToken,
				BotID:    tt.botID,
			}, nil)

			result := h.stripMention(tt.content)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestMentionStrippedBeforeIntentClassification(t *testing.T) {
	messenger := &noopMessenger{}
	ch := newTestCommsHandler(messenger)
	h := newTestHandler(ch)

	// Simulate "@Pilot hi" which Discord delivers as "<@1481980896998326383> hi"
	msg := MessageCreate{
		ID:        "msg1",
		ChannelID: "chan1",
		Author:    User{ID: "user1", Username: "testuser"},
		Content:   "<@1481980896998326383> hi",
	}

	msgData, _ := json.Marshal(msg)
	event := &GatewayEvent{
		T: stringPtr("MESSAGE_CREATE"),
		D: json.RawMessage(msgData),
	}

	ctx := context.Background()
	h.handleMessageCreate(ctx, event)

	// Greeting should produce a text response (via comms.Handler)
	messenger.mu.Lock()
	textCount := len(messenger.texts)
	messenger.mu.Unlock()

	if textCount == 0 {
		t.Error("expected greeting response to be sent via comms.Handler")
	}
}

// --- comms.Handler delegation ---

func TestMessageDelegatedToCommsHandler(t *testing.T) {
	messenger := &noopMessenger{}
	ch := newTestCommsHandler(messenger)
	h := newTestHandler(ch)

	// A task-like message should trigger a confirmation via comms.Handler
	msg := MessageCreate{
		ID:        "msg1",
		ChannelID: "chan1",
		Author:    User{ID: "user1", Username: "testuser"},
		Content:   "add a logout button to the navbar",
	}

	msgData, _ := json.Marshal(msg)
	event := &GatewayEvent{
		T: stringPtr("MESSAGE_CREATE"),
		D: json.RawMessage(msgData),
	}

	ctx := context.Background()
	h.handleMessageCreate(ctx, event)

	// comms.Handler should have created a pending task and sent a confirmation
	messenger.mu.Lock()
	confirmCount := len(messenger.confirms)
	messenger.mu.Unlock()

	if confirmCount == 0 {
		t.Error("expected confirmation to be sent for task via comms.Handler")
	}

	// Verify the pending task exists in comms.Handler
	pending := ch.GetPendingTask("chan1")
	if pending == nil {
		t.Error("expected pending task in comms.Handler")
	}
}

func TestGreetingDelegatedToCommsHandler(t *testing.T) {
	messenger := &noopMessenger{}
	ch := newTestCommsHandler(messenger)
	h := newTestHandler(ch)

	msg := MessageCreate{
		ID:        "msg1",
		ChannelID: "chan1",
		Author:    User{ID: "user1", Username: "testuser"},
		Content:   "hello",
	}

	msgData, _ := json.Marshal(msg)
	event := &GatewayEvent{
		T: stringPtr("MESSAGE_CREATE"),
		D: json.RawMessage(msgData),
	}

	ctx := context.Background()
	h.handleMessageCreate(ctx, event)

	// Should get a text response (greeting) but no confirmation/task
	messenger.mu.Lock()
	textCount := len(messenger.texts)
	confirmCount := len(messenger.confirms)
	messenger.mu.Unlock()

	if textCount == 0 {
		t.Error("expected greeting text response")
	}
	if confirmCount != 0 {
		t.Error("greeting should not create a confirmation")
	}
}
