package discord

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ylcn91/pilot/internal/testutil"
)

// newHelloTestServer spins up a WebSocket server that, on connect, sends a
// HELLO frame with the given heartbeat interval and then drains all incoming
// frames (IDENTIFY + heartbeats) until the client disconnects.
func newHelloTestServer(t *testing.T, heartbeatMS int) string {
	t.Helper()

	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		hello := GatewayEvent{Op: OpcodeHello, D: Hello{HeartbeatInterval: heartbeatMS}}
		if err := conn.WriteJSON(hello); err != nil {
			return
		}

		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	return "ws" + strings.TrimPrefix(server.URL, "http")
}

// TestHandleHello_CloseJoinsHeartbeat verifies that Close() waits for the
// heartbeat goroutine started by handleHello to exit, so it cannot race a
// WriteJSON against the connection being closed. Run with -race to catch the
// regression.
func TestHandleHello_CloseJoinsHeartbeat(t *testing.T) {
	wsURL := newHelloTestServer(t, 5) // fire heartbeats every 5ms

	g := NewGatewayClient(testutil.FakeBearerToken, DefaultIntents)
	g.conn = dialResumeConn(t, wsURL)

	if err := g.handleHello(context.Background()); err != nil {
		t.Fatalf("handleHello returned error: %v", err)
	}

	// Let a few heartbeats fire so the loop is actively writing.
	time.Sleep(30 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		_ = g.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return; heartbeat goroutine likely not joined")
	}

	// After Close returns the heartbeat goroutine must have fully exited.
	// hbWG.Wait() returning instantly confirms it; a hang here would mean a
	// leaked goroutine.
	g.hbWG.Wait()
}

// TestClose_Idempotent verifies Close remains safe to call multiple times and
// concurrently after the heartbeat goroutine is running.
func TestClose_Idempotent(t *testing.T) {
	wsURL := newHelloTestServer(t, 5)

	g := NewGatewayClient(testutil.FakeBearerToken, DefaultIntents)
	g.conn = dialResumeConn(t, wsURL)

	if err := g.handleHello(context.Background()); err != nil {
		t.Fatalf("handleHello returned error: %v", err)
	}

	time.Sleep(20 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = g.Close()
		}()
	}
	wg.Wait()
}
