package executor

import (
	"testing"
)

// --- Constructor Tests ---

func TestNewOpenAIBackend_Defaults(t *testing.T) {
	b := NewOpenAIBackend(nil)
	if b == nil {
		t.Fatal("NewOpenAIBackend(nil) returned nil")
	}
	if b.apiURL != openaiDefaultBaseURL+"/chat/completions" {
		t.Errorf("apiURL = %q, want default", b.apiURL)
	}
	if b.model != openaiDefaultModel {
		t.Errorf("model = %q, want %q", b.model, openaiDefaultModel)
	}
}

func TestNewOpenAIBackend_CustomConfig(t *testing.T) {
	cfg := &BackendConfig{
		OpenAI: &OpenAIConfig{
			APIKey:  "test-openai-key",
			BaseURL: "https://openrouter.ai/api/v1",
			Model:   "anthropic/claude-sonnet-4",
		},
	}
	b := NewOpenAIBackend(cfg)
	if b.apiKey != "test-openai-key" {
		t.Errorf("apiKey = %q, want test-openai-key", b.apiKey)
	}
	if b.apiURL != "https://openrouter.ai/api/v1/chat/completions" {
		t.Errorf("apiURL = %q, want openrouter endpoint", b.apiURL)
	}
	if b.model != "anthropic/claude-sonnet-4" {
		t.Errorf("model = %q", b.model)
	}
}

func TestNewOpenAIBackend_DefaultModelFallback(t *testing.T) {
	cfg := &BackendConfig{
		DefaultModel: "gpt-4o-mini",
		OpenAI:       &OpenAIConfig{APIKey: "k"},
	}
	b := NewOpenAIBackend(cfg)
	if b.model != "gpt-4o-mini" {
		t.Errorf("model = %q, want gpt-4o-mini from DefaultModel fallback", b.model)
	}
}

func TestOpenAIBackend_Name(t *testing.T) {
	b := NewOpenAIBackend(nil)
	if b.Name() != BackendTypeOpenAIAPI {
		t.Errorf("Name() = %q, want %q", b.Name(), BackendTypeOpenAIAPI)
	}
}

func TestOpenAIBackend_IsAvailable(t *testing.T) {
	noKey := NewOpenAIBackend(nil)
	if noKey.IsAvailable() {
		t.Error("IsAvailable() should be false without a key")
	}

	withKey := NewOpenAIBackend(&BackendConfig{OpenAI: &OpenAIConfig{APIKey: "test-key"}})
	if !withKey.IsAvailable() {
		t.Error("IsAvailable() should be true with a key set")
	}
}

// --- Factory registration ---

func TestNewBackend_OpenAIAPI(t *testing.T) {
	cfg := &BackendConfig{
		Type:   BackendTypeOpenAIAPI,
		OpenAI: &OpenAIConfig{APIKey: "test-key"},
	}
	b, err := NewBackend(cfg)
	if err != nil {
		t.Fatalf("NewBackend error: %v", err)
	}
	if b.Name() != BackendTypeOpenAIAPI {
		t.Errorf("Name() = %q, want %q", b.Name(), BackendTypeOpenAIAPI)
	}
	_, ok := b.(*OpenAIBackend)
	if !ok {
		t.Error("backend should be *OpenAIBackend")
	}
}
