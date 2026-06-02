package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ylcn91/pilot/internal/testutil"
)

// --- Listen tests ---

// setupListenTestServer creates a mock HTTP API server that returns a WS URL,
// and a mock WebSocket server that accepts connections and invokes onConn.
// Returns the SocketModeClient configured to use these servers.
func setupListenTestServer(t *testing.T, onConn func(conn *websocket.Conn)) *SocketModeClient {
	t.Helper()

	upgrader := websocket.Upgrader{}
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		onConn(conn)
	}))
	t.Cleanup(wsSrv.Close)

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := fmt.Sprintf(`{"ok":true,"url":%q}`, wsURL)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	t.Cleanup(apiSrv.Close)

	return NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiSrv.URL)
}

func TestListen_ReceivesMessageEvents(t *testing.T) {
	client := setupListenTestServer(t, func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()

		// Send a message event.
		env := map[string]interface{}{
			"envelope_id": "listen-msg-001",
			"type":        "events_api",
			"payload": map[string]interface{}{
				"token":   "t",
				"team_id": "T1",
				"type":    "event_callback",
				"event": map[string]interface{}{
					"type":    "message",
					"channel": "C123",
					"user":    "U456",
					"text":    "hello from listen",
					"ts":      "1.1",
				},
			},
		}
		data, _ := json.Marshal(env)
		_ = conn.WriteMessage(websocket.TextMessage, data)

		// Read ack.
		_, _, _ = conn.ReadMessage()

		// Keep connection open briefly for event to be forwarded.
		time.Sleep(100 * time.Millisecond)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	select {
	case evt, ok := <-events:
		if !ok {
			t.Fatal("events channel closed before receiving event")
		}
		if evt.Type != EventTypeMessage {
			t.Errorf("event type = %q, want %q", evt.Type, EventTypeMessage)
		}
		if evt.ChannelID != "C123" {
			t.Errorf("channel = %q, want %q", evt.ChannelID, "C123")
		}
		if evt.Text != "hello from listen" {
			t.Errorf("text = %q, want %q", evt.Text, "hello from listen")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	cancel()
	// Channel should close after cancel.
	for range events {
	}
}

func TestListen_AppMentionStripsBot(t *testing.T) {
	client := setupListenTestServer(t, func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()

		env := map[string]interface{}{
			"envelope_id": "listen-mention-001",
			"type":        "events_api",
			"payload": map[string]interface{}{
				"token":   "t",
				"team_id": "T1",
				"type":    "event_callback",
				"event": map[string]interface{}{
					"type":    "app_mention",
					"channel": "C123",
					"user":    "U456",
					"text":    "<@UBOT99> deploy prod",
					"ts":      "2.2",
				},
			},
		}
		data, _ := json.Marshal(env)
		_ = conn.WriteMessage(websocket.TextMessage, data)
		_, _, _ = conn.ReadMessage() // ack
		time.Sleep(100 * time.Millisecond)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	select {
	case evt := <-events:
		if evt.Text != "deploy prod" {
			t.Errorf("text = %q, want %q (bot mention should be stripped)", evt.Text, "deploy prod")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	cancel()
	for range events {
	}
}

func TestListen_AuthFailure(t *testing.T) {
	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":false,"error":"invalid_auth"}`))
	}))
	defer apiSrv.Close()

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiSrv.URL)

	_, err := client.Listen(context.Background())
	if err == nil {
		t.Fatal("expected error for auth failure")
	}
	if !errors.Is(err, ErrAuthFailure) {
		t.Errorf("expected ErrAuthFailure, got: %v", err)
	}
}

func TestListen_ContextCancelClosesChannel(t *testing.T) {
	client := setupListenTestServer(t, func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()
		// Keep connection alive until test cancels.
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	events, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Cancel immediately.
	cancel()

	// Channel should close.
	select {
	case _, ok := <-events:
		if ok {
			// Might get a straggler event; drain and check again.
			for range events {
			}
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for events channel to close")
	}
}

func TestListen_MultipleEvents(t *testing.T) {
	client := setupListenTestServer(t, func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()

		for i := 0; i < 3; i++ {
			env := map[string]interface{}{
				"envelope_id": fmt.Sprintf("multi-%d", i),
				"type":        "events_api",
				"payload": map[string]interface{}{
					"token":   "t",
					"team_id": "T1",
					"type":    "event_callback",
					"event": map[string]interface{}{
						"type":    "message",
						"channel": "C123",
						"user":    "U456",
						"text":    fmt.Sprintf("msg-%d", i),
						"ts":      fmt.Sprintf("%d.%d", i, i),
					},
				},
			}
			data, _ := json.Marshal(env)
			_ = conn.WriteMessage(websocket.TextMessage, data)
			_, _, _ = conn.ReadMessage() // ack
		}
		time.Sleep(200 * time.Millisecond)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	var received []string
	for i := 0; i < 3; i++ {
		select {
		case evt := <-events:
			received = append(received, evt.Text)
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}

	for i, want := range []string{"msg-0", "msg-1", "msg-2"} {
		if received[i] != want {
			t.Errorf("event[%d].Text = %q, want %q", i, received[i], want)
		}
	}

	cancel()
	for range events {
	}
}

func TestListen_SkipsNonEventsAPITypes(t *testing.T) {
	client := setupListenTestServer(t, func(conn *websocket.Conn) {
		defer func() { _ = conn.Close() }()

		// Send interactive envelope — should be skipped (not mapped to SocketEvent).
		interactive := map[string]interface{}{
			"envelope_id": "int-001",
			"type":        "interactive",
			"payload":     map[string]interface{}{"type": "block_actions"},
		}
		data, _ := json.Marshal(interactive)
		_ = conn.WriteMessage(websocket.TextMessage, data)
		_, _, _ = conn.ReadMessage() // ack

		// Then send a real message event.
		msg := map[string]interface{}{
			"envelope_id": "msg-001",
			"type":        "events_api",
			"payload": map[string]interface{}{
				"token":   "t",
				"team_id": "T1",
				"type":    "event_callback",
				"event": map[string]interface{}{
					"type":    "message",
					"channel": "C123",
					"user":    "U456",
					"text":    "the real message",
					"ts":      "5.5",
				},
			},
		}
		data, _ = json.Marshal(msg)
		_ = conn.WriteMessage(websocket.TextMessage, data)
		_, _, _ = conn.ReadMessage() // ack
		time.Sleep(200 * time.Millisecond)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	events, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Should only receive the message event, not the interactive one.
	select {
	case evt := <-events:
		if evt.Text != "the real message" {
			t.Errorf("text = %q, want %q", evt.Text, "the real message")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}

	cancel()
	for range events {
	}
}
