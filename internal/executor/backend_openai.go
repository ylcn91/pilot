package executor

import (
	"os"
	"strings"
	"time"
)

const (
	BackendTypeOpenAIAPI = "openai-api"
	openaiDefaultBaseURL = "https://api.openai.com/v1"
	openaiDefaultModel   = "gpt-4o"
)

// OpenAIBackend implements Backend using direct OpenAI-compatible /v1/chat/completions API.
// Supports OpenAI, OpenRouter, Groq, Together, Synthetic, vLLM, Ollama, and any provider
// exposing an OpenAI-compatible endpoint — without a CLI wrapper.
type OpenAIBackend struct {
	apiKey     string
	model      string
	apiURL     string // full endpoint URL
	config     *BackendConfig
	retryWaits []time.Duration // nil = use hardcoded defaults; overridden in tests
}

// NewOpenAIBackend creates a new OpenAI-compatible direct HTTP backend.
func NewOpenAIBackend(config *BackendConfig) *OpenAIBackend {
	b := &OpenAIBackend{config: config}

	// Resolve base URL
	baseURL := openaiDefaultBaseURL
	if config != nil && config.OpenAI != nil && config.OpenAI.BaseURL != "" {
		baseURL = strings.TrimRight(config.OpenAI.BaseURL, "/")
	}
	b.apiURL = baseURL + "/chat/completions"

	// Resolve model
	b.model = openaiDefaultModel
	if config != nil && config.OpenAI != nil && config.OpenAI.Model != "" {
		b.model = config.OpenAI.Model
	} else if config != nil && config.DefaultModel != "" {
		b.model = config.DefaultModel
	}

	// Resolve API key: config takes priority over env vars
	if config != nil && config.OpenAI != nil && config.OpenAI.APIKey != "" {
		b.apiKey = config.OpenAI.APIKey
	} else if config != nil && config.APIAuthToken != "" {
		b.apiKey = config.APIAuthToken
	} else {
		for _, key := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "PILOT_ENGINE_API_KEY"} {
			if v := os.Getenv(key); v != "" {
				b.apiKey = v
				break
			}
		}
	}

	return b
}

func (b *OpenAIBackend) Name() string      { return BackendTypeOpenAIAPI }
func (b *OpenAIBackend) IsAvailable() bool { return b.apiKey != "" }
