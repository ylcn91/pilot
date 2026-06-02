package slack

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestSocketModeHandler_MalformedJSON(t *testing.T) {
	client, server := newTestWSPair(t)

	handler, events := NewSocketModeHandler(client)
	handler.PongWait = 5 * time.Second
	handler.PingInterval = 2 * time.Second

	go handler.Run()
	defer handler.Close()

	// Send malformed JSON — handler should log error and continue.
	if err := server.WriteMessage(websocket.TextMessage, []byte(`{not valid json!!!`)); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Handler should still be alive. Send a valid envelope to prove it.
	env := Envelope{
		EnvelopeID: "after-malformed",
		Type:       "events_api",
		Payload:    json.RawMessage(`{"event":{"type":"message","text":"still alive"}}`),
	}
	data, _ := json.Marshal(env)
	if err := server.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read ack for the valid envelope.
	_, ackData, err := server.ReadMessage()
	if err != nil {
		t.Fatalf("read ack: %v", err)
	}
	var ack envelopeAck
	if err := json.Unmarshal(ackData, &ack); err != nil {
		t.Fatalf("unmarshal ack: %v", err)
	}
	if ack.EnvelopeID != "after-malformed" {
		t.Errorf("ack envelope_id = %q, want %q", ack.EnvelopeID, "after-malformed")
	}

	select {
	case evt := <-events:
		if evt.EnvelopeID != "after-malformed" {
			t.Errorf("expected after-malformed, got %q", evt.EnvelopeID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event after malformed JSON")
	}
}

func TestSocketModeHandler_ServerClose(t *testing.T) {
	client, server := newTestWSPair(t)

	handler, events := NewSocketModeHandler(client)
	handler.PongWait = 5 * time.Second
	handler.PingInterval = 2 * time.Second

	go handler.Run()

	// Server closes the connection abruptly.
	_ = server.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseGoingAway, "server shutdown"),
	)
	_ = server.Close()

	// Events channel should close when handler exits.
	select {
	case _, ok := <-events:
		if ok {
			// May receive one last event; drain and check again.
			select {
			case _, ok2 := <-events:
				if ok2 {
					t.Error("expected events channel to close after server disconnect")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for channel close")
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for events channel to close after server close")
	}
}

func TestSocketModeHandler_MultipleEnvelopes(t *testing.T) {
	client, server := newTestWSPair(t)

	handler, events := NewSocketModeHandler(client)
	handler.PongWait = 5 * time.Second
	handler.PingInterval = 2 * time.Second

	go handler.Run()
	defer handler.Close()

	envelopes := []struct {
		id       string
		envType  string
		wantType SocketEventType
	}{
		{"multi-001", "events_api", SocketEventMessage},
		{"multi-002", "interactive", SocketEventInteraction},
		{"multi-003", "slash_commands", SocketEventSlashCmd},
	}

	for _, e := range envelopes {
		env := Envelope{
			EnvelopeID: e.id,
			Type:       e.envType,
			Payload:    json.RawMessage(`{}`),
		}
		data, _ := json.Marshal(env)
		if err := server.WriteMessage(websocket.TextMessage, data); err != nil {
			t.Fatalf("write %s: %v", e.id, err)
		}

		// Read ack.
		_, ackData, err := server.ReadMessage()
		if err != nil {
			t.Fatalf("read ack for %s: %v", e.id, err)
		}
		var ack envelopeAck
		if err := json.Unmarshal(ackData, &ack); err != nil {
			t.Fatalf("unmarshal ack for %s: %v", e.id, err)
		}
		if ack.EnvelopeID != e.id {
			t.Errorf("ack envelope_id = %q, want %q", ack.EnvelopeID, e.id)
		}
	}

	// Read all emitted events and verify ordering.
	for _, e := range envelopes {
		select {
		case evt := <-events:
			if evt.EnvelopeID != e.id {
				t.Errorf("event envelope_id = %q, want %q", evt.EnvelopeID, e.id)
			}
			if evt.Type != e.wantType {
				t.Errorf("event type = %q, want %q", evt.Type, e.wantType)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for event %s", e.id)
		}
	}
}
