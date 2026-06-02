package discord

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

func TestInteractionDelegatedToCommsHandler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg1"}`))
	}))
	defer server.Close()

	messenger := &noopMessenger{}
	ch := newTestCommsHandler(messenger)
	h := newTestHandler(ch)
	h.apiClient = NewClientWithBaseURL(testutil.FakeBearerToken, server.URL)

	// Inject a pending task into comms.Handler
	ctx := context.Background()
	ch.HandleMessage(ctx, &comms.IncomingMessage{
		ContextID: "chan123",
		SenderID:  "user123",
		Text:      "add a feature",
		Platform:  "discord",
	})

	// Verify pending task exists
	pending := ch.GetPendingTask("chan123")
	if pending == nil {
		t.Fatal("expected pending task to be created")
	}

	// Simulate cancel button click
	interaction := InteractionCreate{
		ID:        "int123",
		Token:     "token123",
		Type:      3, // MESSAGE_COMPONENT
		ChannelID: "chan123",
		User: &User{
			ID:       "user123",
			Username: "testuser",
		},
		Data: InteractionData{
			CustomID: "cancel_task",
		},
	}

	intData, _ := json.Marshal(interaction)
	event := &GatewayEvent{
		T: stringPtr("INTERACTION_CREATE"),
		D: json.RawMessage(intData),
	}

	h.handleInteractionCreate(ctx, event)

	// Verify task was removed from pending
	pending = ch.GetPendingTask("chan123")
	if pending != nil {
		t.Error("expected pending task to be removed after cancel")
	}
}

func TestExecuteButtonNormalizesActionID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg1"}`))
	}))
	defer server.Close()

	messenger := &noopMessenger{}
	ch := newTestCommsHandler(messenger)
	h := newTestHandler(ch)
	h.apiClient = NewClientWithBaseURL(testutil.FakeBearerToken, server.URL)

	ctx := context.Background()

	// Without a pending task, execute button should trigger "No pending task" message
	interaction := InteractionCreate{
		ID:        "int1",
		Token:     "tok1",
		Type:      3,
		ChannelID: "chan1",
		User:      &User{ID: "user1"},
		Data:      InteractionData{CustomID: "execute_task"},
	}

	intData, _ := json.Marshal(interaction)
	event := &GatewayEvent{
		T: stringPtr("INTERACTION_CREATE"),
		D: json.RawMessage(intData),
	}

	h.handleInteractionCreate(ctx, event)

	// Should have sent "No pending task" text
	messenger.mu.Lock()
	found := false
	for _, msg := range messenger.texts {
		if strings.Contains(msg.text, "No pending task") {
			found = true
			break
		}
	}
	messenger.mu.Unlock()

	if !found {
		t.Error("expected 'No pending task' message when execute is clicked without pending task")
	}
}

// --- Interaction response type ---

func TestInteractionResponseType(t *testing.T) {
	var receivedType int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.URL.Path, "/interactions/") {
			var payload struct {
				Type int `json:"type"`
			}
			_ = json.NewDecoder(r.Body).Decode(&payload)
			receivedType = payload.Type
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg1"}`))
	}))
	defer server.Close()

	messenger := &noopMessenger{}
	ch := newTestCommsHandler(messenger)
	h := newTestHandler(ch)
	h.apiClient = NewClientWithBaseURL(testutil.FakeBearerToken, server.URL)

	// Add a pending task via comms.Handler
	ctx := context.Background()
	ch.HandleMessage(ctx, &comms.IncomingMessage{
		ContextID: "chan1",
		SenderID:  "user1",
		Text:      "deploy the app",
		Platform:  "discord",
	})

	interaction := InteractionCreate{
		ID:        "int1",
		Token:     "tok1",
		Type:      3,
		ChannelID: "chan1",
		User:      &User{ID: "user1"},
		Data:      InteractionData{CustomID: "cancel_task"},
	}

	intData, _ := json.Marshal(interaction)
	event := &GatewayEvent{
		T: stringPtr("INTERACTION_CREATE"),
		D: json.RawMessage(intData),
	}

	h.handleInteractionCreate(ctx, event)

	if receivedType != InteractionResponseDeferredUpdateMessage {
		t.Errorf("expected interaction response type %d, got %d",
			InteractionResponseDeferredUpdateMessage, receivedType)
	}
}

// --- Non-MESSAGE_COMPONENT interaction ignored ---

func TestNonButtonInteractionIgnored(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	messenger := &noopMessenger{}
	ch := newTestCommsHandler(messenger)
	h := newTestHandler(ch)
	h.apiClient = NewClientWithBaseURL(testutil.FakeBearerToken, server.URL)

	// Type 2 = APPLICATION_COMMAND, not a button click
	interaction := InteractionCreate{
		ID:   "int1",
		Type: 2,
	}

	intData, _ := json.Marshal(interaction)
	event := &GatewayEvent{
		T: stringPtr("INTERACTION_CREATE"),
		D: json.RawMessage(intData),
	}

	ctx := context.Background()
	h.handleInteractionCreate(ctx, event) // should not panic or delegate
}
