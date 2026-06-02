package executor

import (
	"os"
	"time"
)

const (
	BackendTypeAnthropicAPI = "anthropic-api"

	anthropicAPIURL     = "https://api.anthropic.com/v1/messages"
	anthropicAPIVersion = "2023-06-01"

	// Progressive thinking budget
	thinkingHighTurns  = 8
	thinkingHighBudget = 10000
	thinkingLowBudget  = 3000
	maxOutputTokens    = 12000

	// Limits
	apiMaxTurns       = 60
	apiBashTimeout    = 120 // seconds per bash command
	apiMaxRetries     = 5
	apiOutputCap      = 50000  // bytes, cap tool output to prevent context bloat
	apiContextPruneAt = 150000 // estimated tokens before pruning
)

// AnthropicBackend implements Backend using direct Anthropic Messages API calls.
// Replaces Claude Code CLI subprocess with HTTP streaming, giving full control
// over thinking budgets, tool dispatch, and retry logic.
type AnthropicBackend struct {
	apiKey     string
	apiURL     string
	config     *BackendConfig
	retryWaits []time.Duration // nil = use hardcoded defaults; overridden in tests
}

// NewAnthropicBackend creates a new direct API backend.
func NewAnthropicBackend(config *BackendConfig) *AnthropicBackend {
	b := &AnthropicBackend{config: config, apiURL: anthropicAPIURL}

	// Override API URL if custom base URL configured
	if config != nil && config.APIBaseURL != "" {
		b.apiURL = config.ResolveAPIBaseURL() + "/v1/messages"
	}

	// Resolve API key (same priority as effort_classifier.go:84-95)
	for _, key := range []string{"ANTHROPIC_API_KEY", "PILOT_ENGINE_API_KEY", "ANTHROPIC_AUTH_TOKEN", "CLAUDE_CODE_OAUTH_TOKEN"} {
		if v := os.Getenv(key); v != "" {
			b.apiKey = v
			break
		}
	}

	return b
}

func (b *AnthropicBackend) Name() string { return BackendTypeAnthropicAPI }

func (b *AnthropicBackend) IsAvailable() bool { return b.apiKey != "" }

// --- Error Type ---

// AnthropicAPIError implements BackendError for API errors.
type AnthropicAPIError struct {
	ErrType string
	Msg     string
}

func (e *AnthropicAPIError) Error() string        { return e.Msg }
func (e *AnthropicAPIError) ErrorType() string    { return e.ErrType }
func (e *AnthropicAPIError) ErrorMessage() string { return e.Msg }
func (e *AnthropicAPIError) ErrorStderr() string  { return "" }
