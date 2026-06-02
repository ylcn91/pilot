package executor

import (
	"testing"
)

func TestNewOpenCodeBackend(t *testing.T) {
	tests := []struct {
		name            string
		config          *OpenCodeConfig
		expectServerURL string
		expectModel     string
		expectTimeout   string
	}{
		{
			name:            "nil config uses defaults",
			config:          nil,
			expectServerURL: "http://127.0.0.1:4096",
			expectModel:     "anthropic/claude-sonnet-4",
			expectTimeout:   "10m0s",
		},
		{
			name: "empty server URL uses default",
			config: &OpenCodeConfig{
				ServerURL: "",
				Model:     "custom-model",
			},
			expectServerURL: "http://127.0.0.1:4096",
			expectModel:     "custom-model",
			expectTimeout:   "10m0s",
		},
		{
			name: "custom config",
			config: &OpenCodeConfig{
				ServerURL:      "http://localhost:5000",
				Model:          "anthropic/claude-opus-4",
				RequestTimeout: "20m",
			},
			expectServerURL: "http://localhost:5000",
			expectModel:     "anthropic/claude-opus-4",
			expectTimeout:   "20m0s",
		},
		{
			name: "empty model uses default",
			config: &OpenCodeConfig{
				ServerURL: "http://localhost:4096",
				Model:     "",
			},
			expectServerURL: "http://localhost:4096",
			expectModel:     "anthropic/claude-sonnet-4",
			expectTimeout:   "10m0s",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			backend := NewOpenCodeBackend(tt.config)
			if backend == nil {
				t.Fatal("NewOpenCodeBackend returned nil")
			}
			if backend.config.ServerURL != tt.expectServerURL {
				t.Errorf("ServerURL = %q, want %q", backend.config.ServerURL, tt.expectServerURL)
			}
			if backend.config.Model != tt.expectModel {
				t.Errorf("Model = %q, want %q", backend.config.Model, tt.expectModel)
			}
			if backend.httpClient.Timeout.String() != tt.expectTimeout {
				t.Errorf("http timeout = %q, want %q", backend.httpClient.Timeout.String(), tt.expectTimeout)
			}
		})
	}
}

func TestOpenCodeBackendName(t *testing.T) {
	backend := NewOpenCodeBackend(nil)
	if backend.Name() != BackendTypeOpenCode {
		t.Errorf("Name() = %q, want %q", backend.Name(), BackendTypeOpenCode)
	}
}

func TestOpenCodeBackendIsAvailable(t *testing.T) {
	backend := NewOpenCodeBackend(&OpenCodeConfig{
		ServerURL:       "http://127.0.0.1:59999", // Non-running server
		AutoStartServer: false,
	})

	// Server not running and auto-start disabled, but opencode CLI check
	// This will depend on whether opencode is installed
	// Just verify it doesn't panic
	_ = backend.IsAvailable()
}

func TestOpenCodeBackendStopServer(t *testing.T) {
	backend := NewOpenCodeBackend(nil)

	// Should not error when no server is managed
	err := backend.StopServer()
	if err != nil {
		t.Errorf("StopServer() error = %v, want nil", err)
	}
}

func TestOpenCodeEventStructs(t *testing.T) {
	// Test openCodeEvent struct
	event := openCodeEvent{
		Type:    "tool.start",
		Tool:    "Read",
		Input:   map[string]interface{}{"file_path": "/test.go"},
		Output:  "",
		Error:   "",
		IsError: false,
		Model:   "anthropic/claude-sonnet-4",
		Delta:   &openCodeDelta{Text: "Hello"},
		Usage:   &openCodeUsage{InputTokens: 100, OutputTokens: 50},
	}

	if event.Type != "tool.start" {
		t.Errorf("Type = %q, want tool.start", event.Type)
	}
	if event.Tool != "Read" {
		t.Errorf("Tool = %q, want Read", event.Tool)
	}
	if event.Delta.Text != "Hello" {
		t.Errorf("Delta.Text = %q, want Hello", event.Delta.Text)
	}
	if event.Usage.InputTokens != 100 {
		t.Errorf("Usage.InputTokens = %d, want 100", event.Usage.InputTokens)
	}
}
