package discord

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ylcn91/pilot/internal/testutil"
)

// newResumeTestServer spins up a WebSocket server that upgrades incoming
// connections and forwards every received frame to recv. It returns the
// ws:// dial URL and the receive channel.
func newResumeTestServer(t *testing.T) (string, <-chan GatewayEvent) {
	t.Helper()

	recv := make(chan GatewayEvent, 1)
	upgrader := websocket.Upgrader{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()

		for {
			var event GatewayEvent
			if err := conn.ReadJSON(&event); err != nil {
				return
			}
			select {
			case recv <- event:
			default:
			}
		}
	}))
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	return wsURL, recv
}

// dialResumeConn connects to the WebSocket test server and returns the client
// side connection, registering cleanup.
func dialResumeConn(t *testing.T, wsURL string) *websocket.Conn {
	t.Helper()

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("dial test gateway: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestGatewayClientResume_MissingSession(t *testing.T) {
	seq := 7
	tests := []struct {
		name      string
		conn      bool
		sessionID string
		seq       *int
	}{
		{name: "no connection", conn: false, sessionID: "sess-1", seq: &seq},
		{name: "empty session id", conn: true, sessionID: "", seq: &seq},
		{name: "nil seq", conn: true, sessionID: "sess-1", seq: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGatewayClient(testutil.FakeBearerToken, DefaultIntents)
			g.sessionID = tt.sessionID
			g.seq = tt.seq

			if tt.conn {
				wsURL, _ := newResumeTestServer(t)
				g.conn = dialResumeConn(t, wsURL)
			}

			err := g.Resume(context.Background())
			if err == nil {
				t.Fatal("expected error for missing session, got nil")
			}
			if !strings.Contains(err.Error(), "cannot resume: missing session") {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestGatewayClientResume_SendsResumePayload(t *testing.T) {
	wsURL, recv := newResumeTestServer(t)

	g := NewGatewayClient(testutil.FakeBearerToken, DefaultIntents)
	g.conn = dialResumeConn(t, wsURL)
	g.sessionID = "session-abc"
	seq := 42
	g.seq = &seq

	if err := g.Resume(context.Background()); err != nil {
		t.Fatalf("Resume returned error: %v", err)
	}

	select {
	case event := <-recv:
		if event.Op != OpcodeResume {
			t.Fatalf("expected opcode %d, got %d", OpcodeResume, event.Op)
		}
		// GatewayEvent.D decodes JSON objects into map[string]interface{}.
		data, ok := event.D.(map[string]interface{})
		if !ok {
			t.Fatalf("expected resume data object, got %T", event.D)
		}
		if got := data["token"]; got != testutil.FakeBearerToken {
			t.Errorf("token = %v, want %s", got, testutil.FakeBearerToken)
		}
		if got := data["session_id"]; got != "session-abc" {
			t.Errorf("session_id = %v, want session-abc", got)
		}
		// JSON numbers decode to float64.
		if got := data["seq"]; got != float64(42) {
			t.Errorf("seq = %v, want 42", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for RESUME frame on server")
	}
}

func TestGatewayClientResume_WriteError(t *testing.T) {
	wsURL, _ := newResumeTestServer(t)

	g := NewGatewayClient(testutil.FakeBearerToken, DefaultIntents)
	conn := dialResumeConn(t, wsURL)
	g.conn = conn
	g.sessionID = "session-xyz"
	seq := 5
	g.seq = &seq

	// Close the underlying connection so WriteJSON fails, exercising the
	// "send resume" error branch.
	if err := conn.Close(); err != nil {
		t.Fatalf("close conn: %v", err)
	}

	err := g.Resume(context.Background())
	if err == nil {
		t.Fatal("expected write error after closing connection, got nil")
	}
	if !strings.Contains(err.Error(), "send resume") {
		t.Fatalf("unexpected error: %v", err)
	}
}
