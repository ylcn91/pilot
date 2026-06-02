package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/ylcn91/pilot/internal/codexruntime"
)

func TestCodexRuntimeGatewayE2E(t *testing.T) {
	requireCodexE2E(t)

	server := NewServer(&Config{
		Host: "127.0.0.1",
		Port: 0,
		CodexRuntime: &CodexRuntimeConfig{
			Command: "codex",
			Sandbox: string(codexruntime.SandboxReadOnly),
		},
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", server.handleWebSocket)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)

	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial gateway websocket: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	sendRuntimeTask(t, conn, runtimeTaskPayload{
		Action:  runtimeActionStart,
		Prompt:  "Reply exactly: pilot-gateway-start-e2e-ok",
		Cwd:     cwd,
		Sandbox: string(codexruntime.SandboxReadOnly),
	})
	first := readRuntimeTurn(t, conn)
	if !strings.Contains(first, "pilot-gateway-start-e2e-ok") {
		t.Fatalf("first turn output = %q", first)
	}

	sendRuntimeTask(t, conn, runtimeTaskPayload{
		Action: runtimeActionTurn,
		Prompt: "Reply exactly: pilot-gateway-turn-e2e-ok",
	})
	second := readRuntimeTurn(t, conn)
	if !strings.Contains(second, "pilot-gateway-turn-e2e-ok") {
		t.Fatalf("second turn output = %q", second)
	}

	sendRuntimeTask(t, conn, runtimeTaskPayload{Action: runtimeActionStop})
}

func requireCodexE2E(t *testing.T) {
	t.Helper()
	if os.Getenv("PILOT_CODEX_E2E") != "1" {
		t.Skip("set PILOT_CODEX_E2E=1 to run live Codex CLI integration tests")
	}
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skipf("codex CLI not found in PATH: %v", err)
	}
}

func sendRuntimeTask(t *testing.T, conn *websocket.Conn, task runtimeTaskPayload) {
	t.Helper()
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(Message{
		Type:    MessageTypeTask,
		Payload: raw,
	}); err != nil {
		t.Fatalf("send runtime task: %v", err)
	}
}

func readRuntimeTurn(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	var output strings.Builder

	deadline := time.Now().Add(3 * time.Minute)
	for {
		if err := conn.SetReadDeadline(deadline); err != nil {
			t.Fatalf("set read deadline: %v", err)
		}

		var msg Message
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read gateway message: %v", err)
		}
		if msg.Type != MessageTypeProgress {
			continue
		}

		var payload runtimeProgressPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			t.Fatalf("decode runtime progress: %v", err)
		}
		if payload.Kind == "error" {
			t.Fatalf("runtime error: %s", payload.Error)
		}
		if payload.Event == nil {
			continue
		}

		switch payload.Event.Type {
		case codexruntime.EventAgentMessageDelta:
			output.WriteString(payload.Event.Delta)
		case codexruntime.EventTurnCompleted:
			return output.String()
		}
	}
}
