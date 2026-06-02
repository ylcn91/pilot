package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestOpenCodeBackendSendMessageModelObject verifies that sendMessage
// serialises `model` as an object {providerID, modelID}, never a string
// (GH-2413). Also checks `model` is omitted when no model is configured.
func TestOpenCodeBackendSendMessageModelObject(t *testing.T) {
	tests := []struct {
		name      string
		model     string
		provider  string
		wantOmit  bool
		wantProv  string
		wantModel string
	}{
		{
			name:      "combined model serialises as object",
			model:     "openai/gpt-5.4-mini",
			wantProv:  "openai",
			wantModel: "gpt-5.4-mini",
		},
		{
			name:      "bare model + provider serialises as object",
			model:     "gpt-5.4-mini",
			provider:  "openai",
			wantProv:  "openai",
			wantModel: "gpt-5.4-mini",
		},
		{
			name:     "empty model omits field",
			model:    "",
			wantOmit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var captured map[string]interface{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				_ = json.Unmarshal(body, &captured)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"info":{"id":"m1","tokens":{"input":0,"output":0,"cache":{"read":0,"write":0}}},"parts":[{"type":"text","text":"ok"}]}`))
			}))
			defer srv.Close()

			b := &OpenCodeBackend{
				config: &OpenCodeConfig{
					ServerURL: srv.URL,
					Model:     tt.model,
					Provider:  tt.provider,
				},
				httpClient: &http.Client{Timeout: 5 * time.Second},
			}
			b.log = NewOpenCodeBackend(nil).log

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if _, err := b.sendMessage(ctx, "ses_1", ExecuteOptions{Prompt: "hi"}); err != nil {
				t.Fatalf("sendMessage error = %v", err)
			}

			modelField, present := captured["model"]
			if tt.wantOmit {
				if present {
					t.Fatalf("expected model field omitted, got %v", modelField)
				}
				return
			}
			obj, ok := modelField.(map[string]interface{})
			if !ok {
				t.Fatalf("model field is not an object: %T = %v", modelField, modelField)
			}
			if obj["providerID"] != tt.wantProv {
				t.Errorf("providerID = %v, want %q", obj["providerID"], tt.wantProv)
			}
			if obj["modelID"] != tt.wantModel {
				t.Errorf("modelID = %v, want %q", obj["modelID"], tt.wantModel)
			}
		})
	}
}

// TestOpenCodeBackendSendMessagePayloadShape verifies the full payload JSON
// shape matches OpenCode v1.4.x's PromptInput schema (GH-2413).
func TestOpenCodeBackendSendMessagePayloadShape(t *testing.T) {
	var raw bytes.Buffer
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(&raw, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"info":{"id":"m1","tokens":{"input":0,"output":0,"cache":{"read":0,"write":0}}},"parts":[]}`))
	}))
	defer srv.Close()

	b := &OpenCodeBackend{
		config: &OpenCodeConfig{
			ServerURL: srv.URL,
			Model:     "anthropic/claude-sonnet-4",
		},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	b.log = NewOpenCodeBackend(nil).log

	if _, err := b.sendMessage(context.Background(), "ses_1", ExecuteOptions{Prompt: "hello"}); err != nil {
		t.Fatalf("sendMessage error = %v", err)
	}

	// Reject the legacy string form.
	if strings.Contains(raw.String(), `"model":"anthropic/claude-sonnet-4"`) {
		t.Fatalf("payload still sends model as string: %s", raw.String())
	}
	// Require the object form.
	if !strings.Contains(raw.String(), `"providerID":"anthropic"`) ||
		!strings.Contains(raw.String(), `"modelID":"claude-sonnet-4"`) {
		t.Fatalf("payload missing object-form model: %s", raw.String())
	}
}

// TestOpenCodeBackendDirectoryHeader verifies that both POST /session and
// POST /session/:id/message send the X-OpenCode-Directory header so that
// attached OpenCode servers create sessions in the target project path,
// not in the server's cwd. GH-2415.
func TestOpenCodeBackendDirectoryHeader(t *testing.T) {
	const projectPath = "/tmp/some path/with spaces"
	wantHeader := url.QueryEscape(projectPath)

	var sessionHeader, messageHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/session":
			sessionHeader = r.Header.Get("X-OpenCode-Directory")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"ses_1"}`))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/session/"):
			messageHeader = r.Header.Get("X-OpenCode-Directory")
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"info":{"id":"m1","tokens":{"input":0,"output":0,"cache":{"read":0,"write":0}}},"parts":[{"type":"text","text":"ok"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	b := &OpenCodeBackend{
		config: &OpenCodeConfig{
			ServerURL: srv.URL,
			Model:     "anthropic/claude-sonnet-4",
		},
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
	b.log = NewOpenCodeBackend(nil).log

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sessionID, err := b.createSession(ctx, projectPath)
	if err != nil {
		t.Fatalf("createSession error = %v", err)
	}
	if sessionHeader != wantHeader {
		t.Errorf("createSession X-OpenCode-Directory = %q, want %q", sessionHeader, wantHeader)
	}

	if _, err := b.sendMessage(ctx, sessionID, ExecuteOptions{Prompt: "hi", ProjectPath: projectPath}); err != nil {
		t.Fatalf("sendMessage error = %v", err)
	}
	if messageHeader != wantHeader {
		t.Errorf("sendMessage X-OpenCode-Directory = %q, want %q", messageHeader, wantHeader)
	}
}
