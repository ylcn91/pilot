package executor

import (
	"encoding/json"
	"testing"
	"time"
)

// sseChunk formats a single SSE data line.
func sseChunk(v interface{}) string {
	b, _ := json.Marshal(v)
	return "data: " + string(b) + "\n\n"
}

func sseDone() string { return "data: [DONE]\n\n" }

// --- Helper: make a backend pointing at a test server ---

func newTestOpenAIBackend(t *testing.T, serverURL string) *OpenAIBackend {
	t.Helper()
	cfg := &BackendConfig{
		OpenAI: &OpenAIConfig{
			APIKey:  "test-api-key",
			BaseURL: serverURL,
			Model:   "test-model",
		},
	}
	b := NewOpenAIBackend(cfg)
	// Inject zero-duration backoffs so retry tests are instant.
	b.retryWaits = []time.Duration{0, 0, 0, 0, 0}
	return b
}

// strPtr returns a pointer to s; used in test message construction.
func strPtr(s string) *string { return &s }

// mustJSON marshals v to JSON or panics.
func mustJSON(v interface{}) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
