package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// --- Text-Only Streaming ---

func TestOpenAIBackend_TextOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header
		if r.Header.Get("Authorization") != "Bearer test-api-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		chunks := []map[string]interface{}{
			{"id": "c1", "model": "test-model", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{"role": "assistant", "content": "Hello"}, "finish_reason": nil},
			}},
			{"id": "c1", "model": "test-model", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{"content": " world"}, "finish_reason": nil},
			}},
			{"id": "c1", "model": "test-model", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"},
			}, "usage": map[string]interface{}{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}},
		}
		for _, c := range chunks {
			_, _ = io.WriteString(w, sseChunk(c))
		}
		_, _ = io.WriteString(w, sseDone())
	}))
	defer srv.Close()

	b := newTestOpenAIBackend(t, srv.URL)

	var events []BackendEvent
	result, err := b.Execute(context.Background(), ExecuteOptions{
		Prompt:      "test prompt",
		ProjectPath: t.TempDir(),
		EventHandler: func(e BackendEvent) {
			events = append(events, e)
		},
	})

	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
	if result.Output != "Hello world" {
		t.Errorf("Output = %q, want 'Hello world'", result.Output)
	}
	if result.TokensInput != 10 {
		t.Errorf("TokensInput = %d, want 10", result.TokensInput)
	}
	if result.TokensOutput != 5 {
		t.Errorf("TokensOutput = %d, want 5", result.TokensOutput)
	}

	// Check event sequence: init → text → result
	types := make([]BackendEventType, 0, len(events))
	for _, e := range events {
		types = append(types, e.Type)
	}
	if len(types) < 3 {
		t.Fatalf("expected at least 3 events, got %d: %v", len(types), types)
	}
	if types[0] != EventTypeInit {
		t.Errorf("events[0] = %q, want init", types[0])
	}
	if types[1] != EventTypeText {
		t.Errorf("events[1] = %q, want text", types[1])
	}
	if types[len(types)-1] != EventTypeResult {
		t.Errorf("last event = %q, want result", types[len(types)-1])
	}
}

// --- Tool Call with Argument Fragmentation ---

func TestOpenAIBackend_ToolCallFragmentation(t *testing.T) {
	// Arguments for bash split across three delta chunks
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		chunks := []map[string]interface{}{
			// First chunk: tool call starts with id and name, empty arguments
			{"id": "c1", "model": "test-model", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{
					"role": "assistant",
					"tool_calls": []map[string]interface{}{
						{"index": 0, "id": "call_abc", "type": "function", "function": map[string]interface{}{"name": "bash", "arguments": ""}},
					},
				}, "finish_reason": nil},
			}},
			// Second chunk: argument fragment 1
			{"id": "c1", "model": "test-model", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{
					"tool_calls": []map[string]interface{}{
						{"index": 0, "function": map[string]interface{}{"arguments": `{"com`}},
					},
				}, "finish_reason": nil},
			}},
			// Third chunk: argument fragment 2
			{"id": "c1", "model": "test-model", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{
					"tool_calls": []map[string]interface{}{
						{"index": 0, "function": map[string]interface{}{"arguments": `mand":"ls"}`}},
					},
				}, "finish_reason": nil},
			}},
			// Final chunk: finish_reason = tool_calls
			{"id": "c1", "model": "test-model", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "tool_calls"},
			}, "usage": map[string]interface{}{"prompt_tokens": 20, "completion_tokens": 10}},
		}
		for _, c := range chunks {
			_, _ = io.WriteString(w, sseChunk(c))
		}
		_, _ = io.WriteString(w, sseDone())
	}))
	defer srv.Close()

	b := newTestOpenAIBackend(t, srv.URL)

	var toolUseEvents []BackendEvent
	var toolResultEvents []BackendEvent

	result, err := b.Execute(context.Background(), ExecuteOptions{
		Prompt:      "test prompt",
		ProjectPath: t.TempDir(),
		EventHandler: func(e BackendEvent) {
			if e.Type == EventTypeToolUse {
				toolUseEvents = append(toolUseEvents, e)
			}
			if e.Type == EventTypeToolResult {
				toolResultEvents = append(toolResultEvents, e)
			}
		},
	})

	// The backend will execute bash "ls" — the tool runs but we just check that
	// arguments were parsed correctly from the fragments.
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	if len(toolUseEvents) == 0 {
		t.Fatal("expected at least one tool_use event")
	}
	if toolUseEvents[0].ToolName != "bash" {
		t.Errorf("ToolName = %q, want bash", toolUseEvents[0].ToolName)
	}
	cmd, ok := toolUseEvents[0].ToolInput["command"].(string)
	if !ok || cmd != "ls" {
		t.Errorf("tool input command = %q, want ls", cmd)
	}
	if len(toolResultEvents) == 0 {
		t.Error("expected at least one tool_result event")
	}

	// After one tool call with no follow-up, success depends on whether second
	// call (with tool result) returns stop. result.Success may be false here
	// because the server returned no second turn. That's OK — we're testing
	// fragment accumulation, not end-to-end success.
	_ = result
}

// --- Multi-Turn: tool call → result → final text ---

func TestOpenAIBackend_MultiTurn(t *testing.T) {
	callCount := 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		callCount++

		if callCount == 1 {
			// First turn: tool call
			chunks := []map[string]interface{}{
				{"id": "c1", "model": "test-model", "choices": []map[string]interface{}{
					{"index": 0, "delta": map[string]interface{}{
						"role": "assistant",
						"tool_calls": []map[string]interface{}{
							{"index": 0, "id": "call_001", "type": "function",
								"function": map[string]interface{}{"name": "bash", "arguments": `{"command":"echo hello"}`}},
						},
					}, "finish_reason": nil},
				}},
				{"id": "c1", "model": "test-model", "choices": []map[string]interface{}{
					{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "tool_calls"},
				}, "usage": map[string]interface{}{"prompt_tokens": 30, "completion_tokens": 15}},
			}
			for _, c := range chunks {
				_, _ = io.WriteString(w, sseChunk(c))
			}
		} else {
			// Second turn: final text after seeing tool result
			chunks := []map[string]interface{}{
				{"id": "c2", "model": "test-model", "choices": []map[string]interface{}{
					{"index": 0, "delta": map[string]interface{}{"role": "assistant", "content": "Done!"}, "finish_reason": nil},
				}},
				{"id": "c2", "model": "test-model", "choices": []map[string]interface{}{
					{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"},
				}, "usage": map[string]interface{}{"prompt_tokens": 50, "completion_tokens": 5}},
			}
			for _, c := range chunks {
				_, _ = io.WriteString(w, sseChunk(c))
			}
		}
		_, _ = io.WriteString(w, sseDone())
	}))
	defer srv.Close()

	b := newTestOpenAIBackend(t, srv.URL)

	var eventTypes []BackendEventType
	result, err := b.Execute(context.Background(), ExecuteOptions{
		Prompt:      "test prompt",
		ProjectPath: t.TempDir(),
		EventHandler: func(e BackendEvent) {
			eventTypes = append(eventTypes, e.Type)
		},
	})

	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if callCount != 2 {
		t.Errorf("server called %d times, want 2", callCount)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
	if result.Output != "Done!" {
		t.Errorf("Output = %q, want 'Done!'", result.Output)
	}
	// Token sums: 30+50=80 prompt, 15+5=20 completion
	if result.TokensInput != 80 {
		t.Errorf("TokensInput = %d, want 80", result.TokensInput)
	}
	if result.TokensOutput != 20 {
		t.Errorf("TokensOutput = %d, want 20", result.TokensOutput)
	}

	// Verify event sequence contains tool_use and tool_result
	hasToolUse := false
	hasToolResult := false
	for _, et := range eventTypes {
		if et == EventTypeToolUse {
			hasToolUse = true
		}
		if et == EventTypeToolResult {
			hasToolResult = true
		}
	}
	if !hasToolUse {
		t.Error("expected tool_use event")
	}
	if !hasToolResult {
		t.Error("expected tool_result event")
	}
}
