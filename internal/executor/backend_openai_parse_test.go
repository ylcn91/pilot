package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- Token accounting ---

func TestOpenAIBackend_TokenAccounting(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		chunks := []map[string]interface{}{
			{"id": "c1", "model": "m", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{"role": "assistant", "content": "answer"}, "finish_reason": nil},
			}},
			{"id": "c1", "model": "m", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"},
			}, "usage": map[string]interface{}{"prompt_tokens": 42, "completion_tokens": 17, "total_tokens": 59}},
		}
		for _, c := range chunks {
			_, _ = io.WriteString(w, sseChunk(c))
		}
		_, _ = io.WriteString(w, sseDone())
	}))
	defer srv.Close()

	b := newTestOpenAIBackend(t, srv.URL)
	result, err := b.Execute(context.Background(), ExecuteOptions{
		Prompt:      "test",
		ProjectPath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.TokensInput != 42 {
		t.Errorf("TokensInput = %d, want 42", result.TokensInput)
	}
	if result.TokensOutput != 17 {
		t.Errorf("TokensOutput = %d, want 17", result.TokensOutput)
	}
	// Cache tokens are always 0 for OpenAI — verify result fields exist and are 0
	if result.CacheCreationInputTokens != 0 {
		t.Errorf("CacheCreationInputTokens = %d, want 0", result.CacheCreationInputTokens)
	}
	if result.CacheReadInputTokens != 0 {
		t.Errorf("CacheReadInputTokens = %d, want 0", result.CacheReadInputTokens)
	}
}

// --- Request format validation ---

func TestOpenAIBackend_RequestFormat(t *testing.T) {
	var captured []byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = body

		// Verify no Anthropic headers
		if r.Header.Get("anthropic-version") != "" {
			t.Error("anthropic-version header should not be set for OpenAI backend")
		}
		if r.Header.Get("x-api-key") != "" {
			t.Error("x-api-key header should not be set for OpenAI backend")
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			t.Errorf("Authorization = %q, want 'Bearer ...'", auth)
		}

		// Return minimal stop response
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		chunk := map[string]interface{}{
			"id": "c1", "model": "m", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{"role": "assistant", "content": "hi"}, "finish_reason": nil},
			},
		}
		stop := map[string]interface{}{
			"id": "c1", "model": "m", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"},
			},
		}
		_, _ = io.WriteString(w, sseChunk(chunk))
		_, _ = io.WriteString(w, sseChunk(stop))
		_, _ = io.WriteString(w, sseDone())
	}))
	defer srv.Close()

	b := newTestOpenAIBackend(t, srv.URL)
	_, err := b.Execute(context.Background(), ExecuteOptions{
		Prompt:      "test",
		ProjectPath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	var req map[string]interface{}
	if err := json.Unmarshal(captured, &req); err != nil {
		t.Fatalf("captured body is not valid JSON: %v", err)
	}

	// Should have "messages" and "tools" and "stream"
	if _, ok := req["messages"]; !ok {
		t.Error("request missing 'messages' field")
	}
	if _, ok := req["tools"]; !ok {
		t.Error("request missing 'tools' field")
	}
	streamVal, _ := req["stream"].(bool)
	if !streamVal {
		t.Error("request 'stream' should be true")
	}

	// Should NOT have Anthropic-specific fields
	if _, ok := req["thinking"]; ok {
		t.Error("request should not have 'thinking' field")
	}
	if _, ok := req["system"]; ok {
		t.Error("request should not have 'system' field (use system role message instead)")
	}

	// Verify tools use OpenAI function-call format
	tools, _ := req["tools"].([]interface{})
	if len(tools) == 0 {
		t.Fatal("tools array is empty")
	}
	firstTool, _ := tools[0].(map[string]interface{})
	if firstTool["type"] != "function" {
		t.Errorf("tool type = %q, want 'function'", firstTool["type"])
	}
	funcDef, _ := firstTool["function"].(map[string]interface{})
	if _, ok := funcDef["parameters"]; !ok {
		t.Error("tool function missing 'parameters' (not 'input_schema')")
	}
	if _, ok := funcDef["input_schema"]; ok {
		t.Error("tool function should use 'parameters', not 'input_schema'")
	}

	// System prompt must be a system-role message, not a top-level "system" field
	messages, _ := req["messages"].([]interface{})
	if len(messages) == 0 {
		t.Fatal("messages array is empty")
	}
	firstMsg, _ := messages[0].(map[string]interface{})
	if firstMsg["role"] != "system" {
		t.Errorf("first message role = %q, want 'system'", firstMsg["role"])
	}
}

// --- SSE parser unit tests ---

func TestParseOpenAISSEStream_TextOnly(t *testing.T) {
	b := &OpenAIBackend{}
	sse := fmt.Sprintf(
		"data: %s\n\ndata: %s\n\ndata: [DONE]\n\n",
		mustJSON(map[string]interface{}{
			"id": "c1", "model": "gpt-4o",
			"choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{"content": "Hello"}, "finish_reason": nil},
			},
		}),
		mustJSON(map[string]interface{}{
			"id": "c1", "model": "gpt-4o",
			"choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{"prompt_tokens": 5, "completion_tokens": 3},
		}),
	)

	result, err := b.parseSSEStream(strings.NewReader(sse))
	if err != nil {
		t.Fatalf("parseSSEStream error: %v", err)
	}
	if result.Content != "Hello" {
		t.Errorf("Content = %q, want 'Hello'", result.Content)
	}
	if result.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want 'stop'", result.FinishReason)
	}
	if result.PromptTokens != 5 {
		t.Errorf("PromptTokens = %d, want 5", result.PromptTokens)
	}
}

func TestParseOpenAISSEStream_ToolCallFragments(t *testing.T) {
	b := &OpenAIBackend{}

	chunks := []interface{}{
		map[string]interface{}{
			"id": "c1", "model": "m",
			"choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{
					"tool_calls": []map[string]interface{}{
						{"index": 0, "id": "call_xyz", "type": "function",
							"function": map[string]interface{}{"name": "read_file", "arguments": ""}},
					},
				}, "finish_reason": nil},
			},
		},
		map[string]interface{}{
			"id": "c1", "model": "m",
			"choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{
					"tool_calls": []map[string]interface{}{
						{"index": 0, "function": map[string]interface{}{"arguments": `{"pa`}},
					},
				}, "finish_reason": nil},
			},
		},
		map[string]interface{}{
			"id": "c1", "model": "m",
			"choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{
					"tool_calls": []map[string]interface{}{
						{"index": 0, "function": map[string]interface{}{"arguments": `th":"/tmp/x"}`}},
					},
				}, "finish_reason": nil},
			},
		},
		map[string]interface{}{
			"id": "c1", "model": "m",
			"choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "tool_calls"},
			},
			"usage": map[string]interface{}{"prompt_tokens": 8, "completion_tokens": 4},
		},
	}

	var sb strings.Builder
	for _, c := range chunks {
		sb.WriteString(sseChunk(c))
	}
	sb.WriteString(sseDone())

	result, err := b.parseSSEStream(strings.NewReader(sb.String()))
	if err != nil {
		t.Fatalf("parseSSEStream error: %v", err)
	}
	if result.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want 'tool_calls'", result.FinishReason)
	}
	if len(result.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ID != "call_xyz" {
		t.Errorf("ToolCall.ID = %q, want call_xyz", tc.ID)
	}
	if tc.Function.Name != "read_file" {
		t.Errorf("ToolCall.Name = %q, want read_file", tc.Function.Name)
	}
	// Arguments should be the concatenated fragments
	want := `{"path":"/tmp/x"}`
	if tc.Function.Arguments != want {
		t.Errorf("Arguments = %q, want %q", tc.Function.Arguments, want)
	}
}
