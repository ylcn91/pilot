package gateway

import (
	"encoding/json"
	"testing"

	"github.com/ylcn91/pilot/internal/codexruntime"
)

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
