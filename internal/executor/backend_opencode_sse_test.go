package executor

import (
	"strings"
	"testing"
)

// TestOpenCodeBackendParseSSEStreamCacheTokens verifies that the SSE path
// captures cache_creation/cache_read fields from a usage event, matching the
// synchronous parseAssistantResponse path. GH-2428.
func TestOpenCodeBackendParseSSEStreamCacheTokens(t *testing.T) {
	backend := NewOpenCodeBackend(nil)

	// Two SSE events: a usage event with cache fields, then a result event.
	// SSE format: each event ends with a blank line; we also need a trailing
	// blank line on the last event to trigger dispatch.
	sse := "data: {\"type\":\"usage\",\"usage\":{\"input_tokens\":10,\"output_tokens\":20,\"cache_creation_input_tokens\":3,\"cache_read_input_tokens\":4},\"model\":\"glm-5.1\"}\n\n" +
		"data: {\"type\":\"done\",\"output\":\"ok\"}\n\n"

	result := &BackendResult{}
	opts := ExecuteOptions{}
	if err := backend.parseSSEStream(strings.NewReader(sse), opts, result); err != nil {
		t.Fatalf("parseSSEStream error = %v", err)
	}

	if result.TokensInput != 10 || result.TokensOutput != 20 {
		t.Errorf("tokens = %d/%d, want 10/20", result.TokensInput, result.TokensOutput)
	}
	if result.CacheCreationInputTokens != 3 {
		t.Errorf("CacheCreationInputTokens = %d, want 3", result.CacheCreationInputTokens)
	}
	if result.CacheReadInputTokens != 4 {
		t.Errorf("CacheReadInputTokens = %d, want 4", result.CacheReadInputTokens)
	}
	if result.Model != "glm-5.1" {
		t.Errorf("Model = %q, want glm-5.1", result.Model)
	}
}

// TestOpenCodeBackendParseSSENestedCache verifies that the nested
// {cache:{read,write}} usage layout is also accepted by SSE parsing. GH-2428.
func TestOpenCodeBackendParseSSENestedCache(t *testing.T) {
	backend := NewOpenCodeBackend(nil)

	// Trailing empty line is required so bufio.Scanner yields the blank line
	// that marks the end of the SSE event.
	sse := "data: {\"type\":\"usage\",\"usage\":{\"input_tokens\":1,\"output_tokens\":2,\"cache\":{\"read\":5,\"write\":6}}}\n\n"

	result := &BackendResult{}
	if err := backend.parseSSEStream(strings.NewReader(sse), ExecuteOptions{}, result); err != nil {
		t.Fatalf("parseSSEStream error = %v", err)
	}
	if result.CacheCreationInputTokens != 6 {
		t.Errorf("CacheCreationInputTokens = %d, want 6 (from cache.write)", result.CacheCreationInputTokens)
	}
	if result.CacheReadInputTokens != 5 {
		t.Errorf("CacheReadInputTokens = %d, want 5 (from cache.read)", result.CacheReadInputTokens)
	}
}
