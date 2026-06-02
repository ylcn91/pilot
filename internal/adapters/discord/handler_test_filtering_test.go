package discord

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ylcn91/pilot/internal/testutil"
)

// --- Guild/Channel filtering ---

func TestHandlerGuildFiltering(t *testing.T) {
	tests := []struct {
		name            string
		allowedGuilds   []string
		allowedChannels []string
		guildID         string
		channelID       string
		allowed         bool
	}{
		{
			name:          "allowed guild",
			allowedGuilds: []string{"guild123"},
			guildID:       "guild123",
			allowed:       true,
		},
		{
			name:          "disallowed guild",
			allowedGuilds: []string{"guild123"},
			guildID:       "guild456",
			allowed:       false,
		},
		{
			name:            "allowed channel",
			allowedChannels: []string{"chan123"},
			channelID:       "chan123",
			allowed:         true,
		},
		{
			name:            "disallowed channel",
			allowedChannels: []string{"chan123"},
			channelID:       "chan456",
			allowed:         false,
		},
		{
			name:    "no restrictions",
			guildID: "any",
			allowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(&HandlerConfig{
				BotToken:        testutil.FakeBearerToken,
				AllowedGuilds:   tt.allowedGuilds,
				AllowedChannels: tt.allowedChannels,
			}, nil)

			result := h.isAllowed(tt.guildID, tt.channelID)
			if result != tt.allowed {
				t.Errorf("expected %v, got %v", tt.allowed, result)
			}
		})
	}
}

func TestHandlerDMAllowlisting(t *testing.T) {
	tests := []struct {
		name            string
		allowedGuilds   []string
		allowedChannels []string
		guildID         string
		channelID       string
		allowed         bool
	}{
		{
			name:          "DM with guild allowlist only — permitted",
			allowedGuilds: []string{"guild123"},
			guildID:       "",
			channelID:     "dm-chan-1",
			allowed:       true,
		},
		{
			name:            "DM with guild and channel allowlist — denied (channel not listed)",
			allowedGuilds:   []string{"guild123"},
			allowedChannels: []string{"chan456"},
			guildID:         "",
			channelID:       "dm-chan-1",
			allowed:         false,
		},
		{
			name:            "DM with channel allowlist only — denied",
			allowedChannels: []string{"chan456"},
			guildID:         "",
			channelID:       "dm-chan-1",
			allowed:         false,
		},
		{
			name:            "DM with channel allowlist — permitted (channel listed)",
			allowedChannels: []string{"dm-chan-1"},
			guildID:         "",
			channelID:       "dm-chan-1",
			allowed:         true,
		},
		{
			name:    "DM with no restrictions — permitted",
			guildID: "",
			allowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHandler(&HandlerConfig{
				BotToken:        testutil.FakeBearerToken,
				AllowedGuilds:   tt.allowedGuilds,
				AllowedChannels: tt.allowedChannels,
			}, nil)

			result := h.isAllowed(tt.guildID, tt.channelID)
			if result != tt.allowed {
				t.Errorf("expected %v, got %v", tt.allowed, result)
			}
		})
	}
}

func TestHandlerBotMessageSkipping(t *testing.T) {
	messenger := &noopMessenger{}
	ch := newTestCommsHandler(messenger)
	h := newTestHandler(ch)

	// Test: bot message should be skipped (no message sent)
	botMsg := MessageCreate{
		ID:        "msg123",
		ChannelID: "chan123",
		GuildID:   "guild123",
		Author: User{
			ID:       "123456789",
			Username: "PilotBot",
			Bot:      true,
		},
		Content: "Some bot message",
	}

	msgData, _ := json.Marshal(botMsg)
	event := &GatewayEvent{
		T: stringPtr("MESSAGE_CREATE"),
		D: json.RawMessage(msgData),
	}

	ctx := context.Background()
	h.handleMessageCreate(ctx, event)

	messenger.mu.Lock()
	textCount := len(messenger.texts)
	messenger.mu.Unlock()

	if textCount != 0 {
		t.Errorf("expected no messages for bot message, got %d", textCount)
	}
}

// --- Handler lifecycle ---

func TestHandlerUnknownEventHandling(t *testing.T) {
	h := newTestHandler(nil)

	event := &GatewayEvent{
		T: stringPtr("UNKNOWN_EVENT"),
		D: json.RawMessage(`{}`),
	}

	ctx := context.Background()
	h.processEvent(ctx, event)
	// No assertion needed - just ensuring it doesn't crash
}

func TestHandlerStopIdempotent(t *testing.T) {
	h := newTestHandler(nil)

	// Calling Stop multiple times should not panic
	h.Stop()
	h.Stop()
	h.Stop()
}

func TestHandlerNilCommsHandler(t *testing.T) {
	// Handler with nil commsHandler should not panic on events
	h := newTestHandler(nil)

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
	h.handleMessageCreate(ctx, event) // should not panic
}

// --- Guild filtering blocks message ---

func TestGuildFilterBlocksMessage(t *testing.T) {
	messenger := &noopMessenger{}
	ch := newTestCommsHandler(messenger)

	h := NewHandler(&HandlerConfig{
		BotToken:      testutil.FakeBearerToken,
		AllowedGuilds: []string{"guild-allowed"},
	}, ch)

	msg := MessageCreate{
		ID:        "msg1",
		ChannelID: "chan1",
		GuildID:   "guild-blocked",
		Author:    User{ID: "user1", Username: "testuser"},
		Content:   "add a feature",
	}

	msgData, _ := json.Marshal(msg)
	event := &GatewayEvent{
		T: stringPtr("MESSAGE_CREATE"),
		D: json.RawMessage(msgData),
	}

	ctx := context.Background()
	h.handleMessageCreate(ctx, event)

	// Message from blocked guild should not reach comms.Handler
	if ch.GetPendingTask("chan1") != nil {
		t.Error("message from blocked guild should not create pending task")
	}

	messenger.mu.Lock()
	textCount := len(messenger.texts)
	messenger.mu.Unlock()
	if textCount != 0 {
		t.Error("no messages should be sent for blocked guild")
	}
}

// --- Empty message after mention strip ---

func TestEmptyAfterMentionStrip(t *testing.T) {
	messenger := &noopMessenger{}
	ch := newTestCommsHandler(messenger)
	h := NewHandler(&HandlerConfig{
		BotToken: testutil.FakeBearerToken,
		BotID:    "123456789",
	}, ch)

	msg := MessageCreate{
		ID:        "msg1",
		ChannelID: "chan1",
		Author:    User{ID: "user1", Username: "testuser"},
		Content:   "<@123456789>",
	}

	msgData, _ := json.Marshal(msg)
	event := &GatewayEvent{
		T: stringPtr("MESSAGE_CREATE"),
		D: json.RawMessage(msgData),
	}

	ctx := context.Background()
	h.handleMessageCreate(ctx, event)

	// Empty message after strip should be ignored
	messenger.mu.Lock()
	textCount := len(messenger.texts)
	messenger.mu.Unlock()

	if textCount != 0 {
		t.Errorf("expected no messages for empty-after-strip, got %d", textCount)
	}
}
