package codexruntime

import (
	"encoding/json"
	"testing"
)

func TestMapNotificationAgentMessageDelta(t *testing.T) {
	msg := notification(t, `{
		"method":"item/agentMessage/delta",
		"params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","delta":"PONG"}
	}`)

	event, err := MapNotification(msg)
	if err != nil {
		t.Fatal(err)
	}
	if event.Type != EventAgentMessageDelta {
		t.Fatalf("type = %q, want %q", event.Type, EventAgentMessageDelta)
	}
	if event.ThreadID != "thread-1" || event.TurnID != "turn-1" || event.ItemID != "item-1" {
		t.Fatalf("ids were not mapped: %+v", event)
	}
	if event.Delta != "PONG" {
		t.Fatalf("delta = %q, want PONG", event.Delta)
	}
}

func TestMapNotificationDiffPlanAndPatch(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		want   EventType
		assert func(t *testing.T, event Event)
	}{
		{
			name: "diff",
			raw:  `{"method":"turn/diff/updated","params":{"threadId":"thread-1","turnId":"turn-1","diff":"diff --git"}}`,
			want: EventDiffUpdated,
			assert: func(t *testing.T, event Event) {
				if event.Diff != "diff --git" {
					t.Fatalf("diff = %q", event.Diff)
				}
			},
		},
		{
			name: "plan",
			raw:  `{"method":"turn/plan/updated","params":{"threadId":"thread-1","turnId":"turn-1","explanation":"next","plan":[{"step":"Read","status":"in_progress"}]}}`,
			want: EventPlanUpdated,
			assert: func(t *testing.T, event Event) {
				if event.Explanation != "next" {
					t.Fatalf("explanation = %q", event.Explanation)
				}
				if len(event.Plan) == 0 {
					t.Fatal("plan should be preserved")
				}
			},
		},
		{
			name: "patch",
			raw:  `{"method":"item/fileChange/patchUpdated","params":{"threadId":"thread-1","turnId":"turn-1","itemId":"item-1","changes":[{"path":"README.md"}]}}`,
			want: EventPatchUpdated,
			assert: func(t *testing.T, event Event) {
				if len(event.Changes) == 0 {
					t.Fatal("changes should be preserved")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := MapNotification(notification(t, tt.raw))
			if err != nil {
				t.Fatal(err)
			}
			if event.Type != tt.want {
				t.Fatalf("type = %q, want %q", event.Type, tt.want)
			}
			tt.assert(t, event)
		})
	}
}

func TestMapNotificationRejectsResponses(t *testing.T) {
	_, err := MapNotification(Message{ID: json.RawMessage(`1`)})
	if err == nil {
		t.Fatal("expected error")
	}
}

func notification(t *testing.T, raw string) Message {
	t.Helper()
	var msg Message
	if err := json.Unmarshal([]byte(raw), &msg); err != nil {
		t.Fatal(err)
	}
	return msg
}
