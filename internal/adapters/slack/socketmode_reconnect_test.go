package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ylcn91/pilot/internal/testutil"
)

func TestListen_DisconnectTriggersReconnect(t *testing.T) {
	var connCount atomic.Int32

	upgrader := websocket.Upgrader{}
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		n := connCount.Add(1)

		if n == 1 {
			// First connection: send disconnect.
			env := map[string]interface{}{
				"envelope_id": "disc-001",
				"type":        "disconnect",
				"reason":      "link_disabled",
			}
			data, _ := json.Marshal(env)
			_ = conn.WriteMessage(websocket.TextMessage, data)
			// Read ack.
			_, _, _ = conn.ReadMessage()
			return
		}

		// Second connection: send a message then stay alive.
		env := map[string]interface{}{
			"envelope_id": "reconnect-msg-001",
			"type":        "events_api",
			"payload": map[string]interface{}{
				"token":   "t",
				"team_id": "T1",
				"type":    "event_callback",
				"event": map[string]interface{}{
					"type":    "message",
					"channel": "C999",
					"user":    "U111",
					"text":    "after reconnect",
					"ts":      "3.3",
				},
			},
		}
		data, _ := json.Marshal(env)
		_ = conn.WriteMessage(websocket.TextMessage, data)
		_, _, _ = conn.ReadMessage() // ack
		// Keep alive long enough for event to be forwarded.
		time.Sleep(500 * time.Millisecond)
	}))
	defer wsSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := fmt.Sprintf(`{"ok":true,"url":%q}`, wsURL)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer apiSrv.Close()

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiSrv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	// Should receive the message from the second connection.
	select {
	case evt := <-events:
		if evt.Text != "after reconnect" {
			t.Errorf("text = %q, want %q", evt.Text, "after reconnect")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for event after reconnect")
	}

	if got := connCount.Load(); got < 2 {
		t.Errorf("expected at least 2 connections (reconnect), got %d", got)
	}

	cancel()
	for range events {
	}
}

func TestListen_ServerDropReconnects(t *testing.T) {
	var connCount atomic.Int32
	var mu sync.Mutex
	var conns []*websocket.Conn

	upgrader := websocket.Upgrader{}
	wsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		mu.Lock()
		conns = append(conns, conn)
		mu.Unlock()

		n := connCount.Add(1)

		if n == 1 {
			// First connection: close abruptly.
			_ = conn.Close()
			return
		}

		// Second connection: send message.
		env := map[string]interface{}{
			"envelope_id": "drop-msg-001",
			"type":        "events_api",
			"payload": map[string]interface{}{
				"token":   "t",
				"team_id": "T1",
				"type":    "event_callback",
				"event": map[string]interface{}{
					"type":    "message",
					"channel": "C123",
					"user":    "U456",
					"text":    "after drop",
					"ts":      "7.7",
				},
			},
		}
		data, _ := json.Marshal(env)
		_ = conn.WriteMessage(websocket.TextMessage, data)
		_, _, _ = conn.ReadMessage() // ack
		time.Sleep(500 * time.Millisecond)
		_ = conn.Close()
	}))
	defer wsSrv.Close()

	wsURL := "ws" + strings.TrimPrefix(wsSrv.URL, "http")

	apiSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		resp := fmt.Sprintf(`{"ok":true,"url":%q}`, wsURL)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(resp))
	}))
	defer apiSrv.Close()

	client := NewSocketModeClientWithBaseURL(testutil.FakeSlackAppToken, apiSrv.URL)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := client.Listen(ctx)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	select {
	case evt := <-events:
		if evt.Text != "after drop" {
			t.Errorf("text = %q, want %q", evt.Text, "after drop")
		}
	case <-time.After(8 * time.Second):
		t.Fatal("timed out waiting for event after server drop")
	}

	cancel()
	for range events {
	}
}
