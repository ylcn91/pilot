package gateway

import (
	"encoding/json"
	"testing"

	"github.com/qf-studio/pilot/internal/codexruntime"
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
