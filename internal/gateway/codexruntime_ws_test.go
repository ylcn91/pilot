package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ylcn91/pilot/internal/codexruntime"
)

func TestRunRuntimeSessionUsesInjectedStart(t *testing.T) {
	server := NewServer(&Config{
		Host: "127.0.0.1",
		Port: 9090,
		CodexRuntime: &CodexRuntimeConfig{
			Command: "/opt/bin/codex",
			Args:    []string{"app-server", "--stdio"},
		},
	})

	startErr := errors.New("fake codex unavailable")
	var gotCfg codexruntime.Config
	called := 0
	server.codex.start = func(_ context.Context, cfg codexruntime.Config) (*codexruntime.Client, error) {
		called++
		gotCfg = cfg
		return nil, startErr
	}

	err := server.runRuntimeSession(context.Background(), &Session{ID: "s1"}, runtimeTaskPayload{
		Action: runtimeActionStart,
		Prompt: "do the thing",
		Cwd:    ".",
	})
	if !errors.Is(err, startErr) {
		t.Fatalf("err = %v, want %v", err, startErr)
	}
	if called != 1 {
		t.Fatalf("start called %d times, want 1", called)
	}
	if gotCfg.Command != "/opt/bin/codex" {
		t.Fatalf("command = %q", gotCfg.Command)
	}
	if len(gotCfg.Args) != 2 || gotCfg.Args[0] != "app-server" {
		t.Fatalf("args = %v", gotCfg.Args)
	}
	if !filepath.IsAbs(gotCfg.Cwd) {
		t.Fatalf("cwd = %q, want absolute path", gotCfg.Cwd)
	}
}

func TestRunRuntimeSessionRequiresPrompt(t *testing.T) {
	server := NewServer(&Config{Host: "127.0.0.1", Port: 9090})

	called := false
	server.codex.start = func(_ context.Context, _ codexruntime.Config) (*codexruntime.Client, error) {
		called = true
		return nil, nil
	}

	err := server.runRuntimeSession(context.Background(), &Session{ID: "s1"}, runtimeTaskPayload{
		Action: runtimeActionStart,
	})
	if err == nil {
		t.Fatal("expected error for empty prompt")
	}
	if called {
		t.Fatal("start should not be called when prompt is empty")
	}
}

func TestParseRuntimeSandbox(t *testing.T) {
	got, err := parseRuntimeSandbox("workspace-write")
	if err != nil {
		t.Fatal(err)
	}
	if got != codexruntime.SandboxWorkspaceWrite {
		t.Fatalf("sandbox = %q", got)
	}

	if _, err := parseRuntimeSandbox("bad"); err == nil {
		t.Fatal("expected invalid sandbox error")
	}
}

func TestResolveCodexRuntimeConfig(t *testing.T) {
	server := NewServer(&Config{
		Host: "127.0.0.1",
		Port: 9090,
		CodexRuntime: &CodexRuntimeConfig{
			Command: "/opt/bin/codex",
			Args:    []string{"app-server", "--stdio", "--trace"},
			Model:   "gpt-5.1-codex",
			Sandbox: "workspace-write",
		},
	})

	got, err := server.resolveCodexRuntimeConfig(runtimeTaskPayload{})
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != "/opt/bin/codex" {
		t.Fatalf("command = %q", got.Command)
	}
	if got.Model != "gpt-5.1-codex" {
		t.Fatalf("model = %q", got.Model)
	}
	if got.Sandbox != codexruntime.SandboxWorkspaceWrite {
		t.Fatalf("sandbox = %q", got.Sandbox)
	}
	if len(got.Args) != 3 || got.Args[2] != "--trace" {
		t.Fatalf("args = %v", got.Args)
	}
}

func TestResolveCodexRuntimeConfigTaskOverrides(t *testing.T) {
	server := NewServer(&Config{
		Host: "127.0.0.1",
		Port: 9090,
		CodexRuntime: &CodexRuntimeConfig{
			Model:   "configured-model",
			Sandbox: "read-only",
		},
	})

	got, err := server.resolveCodexRuntimeConfig(runtimeTaskPayload{
		Model:   "payload-model",
		Sandbox: "danger-full-access",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Command != "codex" {
		t.Fatalf("command = %q", got.Command)
	}
	if got.Model != "payload-model" {
		t.Fatalf("model = %q", got.Model)
	}
	if got.Sandbox != codexruntime.SandboxDangerFull {
		t.Fatalf("sandbox = %q", got.Sandbox)
	}
}

func TestResolveCodexRuntimeConfigInvalidSandbox(t *testing.T) {
	server := NewServer(&Config{
		Host: "127.0.0.1",
		Port: 9090,
		CodexRuntime: &CodexRuntimeConfig{
			Sandbox: "bad",
		},
	})

	if _, err := server.resolveCodexRuntimeConfig(runtimeTaskPayload{}); err == nil {
		t.Fatal("expected invalid sandbox error")
	}
}

func TestRuntimeApprovalChoices(t *testing.T) {
	choices := runtimeApprovalChoices("item/fileChange/requestApproval")
	if len(choices) != 4 || choices[0] != "accept" {
		t.Fatalf("unexpected choices: %v", choices)
	}
}

func TestParseRuntimeApprovalResponse(t *testing.T) {
	response, err := parseRuntimeApprovalResponse(runtimeTaskPayload{
		RequestID: json.RawMessage(`42`),
		Decision:  "accept",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.RequestID != 42 || response.Decision != "accept" {
		t.Fatalf("response = %+v", response)
	}

	response, err = parseRuntimeApprovalResponse(runtimeTaskPayload{
		RequestID: json.RawMessage(`"43"`),
		Scope:     "session",
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.RequestID != 43 || response.Scope != "session" {
		t.Fatalf("response = %+v", response)
	}

	if _, err := parseRuntimeApprovalResponse(runtimeTaskPayload{}); err == nil {
		t.Fatal("expected missing request id error")
	}
}

func TestRuntimeApprovalRegistryResolve(t *testing.T) {
	registry := newRuntimeApprovalRegistry()
	ch, cancel, err := registry.register("session-1", 7)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	if err := registry.resolve("session-1", runtimeApprovalResponsePayload{
		RequestID: 7,
		Decision:  "decline",
	}); err != nil {
		t.Fatal(err)
	}

	got := <-ch
	if got.Decision != "decline" {
		t.Fatalf("decision = %q", got.Decision)
	}

	if err := registry.resolve("session-1", runtimeApprovalResponsePayload{RequestID: 7}); err == nil {
		t.Fatal("expected duplicate resolve error")
	}
}

func TestRuntimeSessionRegistryRemoveIfKeepsReplacement(t *testing.T) {
	registry := newRuntimeSessionRegistry()
	first := newRuntimeSessionController(nil, "thread-1", ".", "")
	second := newRuntimeSessionController(nil, "thread-2", ".", "")

	registry.replace("session-1", first)
	registry.replace("session-1", second)
	registry.removeIf("session-1", first)

	got, ok := registry.get("session-1")
	if !ok {
		t.Fatal("expected replacement session to remain")
	}
	if got != second {
		t.Fatalf("session = %p, want %p", got, second)
	}
}

func TestRuntimeSessionControllerRejectsClosedTurn(t *testing.T) {
	controller := newRuntimeSessionController(nil, "thread-1", ".", "")
	controller.close()
	controller.finish()

	if err := controller.enqueueTurn("hello"); err == nil {
		t.Fatal("expected closed session error")
	}
}

func TestDefaultRuntimeApprovalResponse(t *testing.T) {
	response := defaultRuntimeApprovalResponse("item/fileChange/requestApproval", 99)
	if response.RequestID != 99 || response.Decision != string(codexruntime.FileChangeDecline) {
		t.Fatalf("response = %+v", response)
	}

	response = defaultRuntimeApprovalResponse("item/permissions/requestApproval", 100)
	if response.RequestID != 100 || response.Scope != string(codexruntime.PermissionGrantTurn) {
		t.Fatalf("response = %+v", response)
	}
}

func TestRuntimeProgressPayloadEnvelope(t *testing.T) {
	event := codexruntime.Event{
		Type:     codexruntime.EventAgentMessageDelta,
		Method:   "item/agentMessage/delta",
		ThreadID: "thread-1",
		TurnID:   "turn-1",
		Delta:    "hello",
	}
	raw, err := json.Marshal(runtimeProgressPayload{
		Source: runtimeSource,
		Kind:   "event",
		Event:  &event,
	})
	if err != nil {
		t.Fatal(err)
	}

	msg, err := json.Marshal(Message{Type: MessageTypeProgress, Payload: raw})
	if err != nil {
		t.Fatal(err)
	}

	var decoded Message
	if err := json.Unmarshal(msg, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Type != MessageTypeProgress {
		t.Fatalf("type = %q", decoded.Type)
	}
}
