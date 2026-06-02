package executor

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- Retry on 429 ---

func TestOpenAIBackend_Retry429(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		// Second attempt succeeds
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		chunk := map[string]interface{}{
			"id": "c1", "model": "m", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{"role": "assistant", "content": "ok"}, "finish_reason": nil},
			},
		}
		_, _ = io.WriteString(w, sseChunk(chunk))
		stop := map[string]interface{}{
			"id": "c1", "model": "m", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"},
			},
		}
		_, _ = io.WriteString(w, sseChunk(stop))
		_, _ = io.WriteString(w, sseDone())
	}))
	defer srv.Close()

	b := newTestOpenAIBackend(t, srv.URL)
	// Inject zero-duration backoffs so the retry is instant.
	b.retryWaits = []time.Duration{0, 0, 0, 0, 0}

	result, err := b.Execute(context.Background(), ExecuteOptions{
		Prompt:      "test",
		ProjectPath: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if !result.Success {
		t.Error("expected success after retry")
	}
	if attempts < 2 {
		t.Errorf("expected at least 2 attempts, got %d", attempts)
	}
}

// --- Context cancellation during 429 backoff ---
//
// Verifies that cancelling ctx while callAPI is waiting between retries returns
// ctx.Err() promptly — not after the full backoff duration.

func TestOpenAIBackend_CtxCancelDuring429Backoff(t *testing.T) {
	longWait := 5 * time.Second

	readyCh := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		select {
		case readyCh <- struct{}{}:
		default:
		}
	}))
	defer srv.Close()

	b := &OpenAIBackend{
		apiKey:     "test-key",
		model:      "test-model",
		apiURL:     srv.URL + "/chat/completions",
		config:     &BackendConfig{},
		retryWaits: []time.Duration{longWait, longWait, longWait, longWait, longWait},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-readyCh
		cancel()
	}()

	start := time.Now()
	req := &openaiRequest{
		Model:    "test-model",
		Messages: []openaiMsg{{Role: "user", Content: strPtr("hi")}},
	}
	_, err := b.callAPI(ctx, req)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error on context cancellation, got nil")
	}
	if err != context.Canceled {
		t.Errorf("error = %v, want context.Canceled", err)
	}
	if elapsed >= longWait {
		t.Errorf("callAPI took %v, want << %v (backoff not honouring ctx)", elapsed, longWait)
	}
}

// --- Retry on 500 ---

func TestOpenAIBackend_Retry500(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		chunk := map[string]interface{}{
			"id": "c1", "model": "m", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{"role": "assistant", "content": "recovered"}, "finish_reason": nil},
			},
		}
		_, _ = io.WriteString(w, sseChunk(chunk))
		stop := map[string]interface{}{
			"id": "c1", "model": "m", "choices": []map[string]interface{}{
				{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"},
			},
		}
		_, _ = io.WriteString(w, sseChunk(stop))
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
	if !result.Success {
		t.Error("expected success after 500 retry")
	}
	if attempts < 2 {
		t.Errorf("expected at least 2 attempts, got %d", attempts)
	}
}

// --- Malformed SSE handling ---

func TestOpenAIBackend_MalformedSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)

		// Mix of valid lines, garbage, and empty lines
		lines := []string{
			"data: not-valid-json\n\n",
			":comment line\n\n",
			"\n",
			sseChunk(map[string]interface{}{
				"id": "c1", "model": "m",
				"choices": []map[string]interface{}{
					{"index": 0, "delta": map[string]interface{}{"role": "assistant", "content": "valid"}, "finish_reason": nil},
				},
			}),
			"data: {broken json\n\n",
			sseChunk(map[string]interface{}{
				"id": "c1", "model": "m",
				"choices": []map[string]interface{}{
					{"index": 0, "delta": map[string]interface{}{}, "finish_reason": "stop"},
				},
			}),
			sseDone(),
		}
		for _, l := range lines {
			_, _ = io.WriteString(w, l)
		}
	}))
	defer srv.Close()

	b := newTestOpenAIBackend(t, srv.URL)
	result, err := b.Execute(context.Background(), ExecuteOptions{
		Prompt:      "test",
		ProjectPath: t.TempDir(),
	})

	// Malformed lines should be skipped; valid content should accumulate
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.Output != "valid" {
		t.Errorf("Output = %q, want 'valid'", result.Output)
	}
}

// --- No API key returns error ---

func TestOpenAIBackend_NoAPIKey(t *testing.T) {
	b := &OpenAIBackend{
		apiKey: "",
		model:  openaiDefaultModel,
		apiURL: "https://api.openai.com/v1/chat/completions",
		config: &BackendConfig{},
	}
	_, err := b.Execute(context.Background(), ExecuteOptions{Prompt: "test"})
	if err == nil {
		t.Error("expected error when no API key configured")
	}
	if !strings.Contains(err.Error(), "API key") {
		t.Errorf("error = %q, want API key mention", err.Error())
	}
}
