package executor

// IntentJudgeConfig configures the LLM intent judge that compares diffs against
// the original issue to catch scope creep and missing requirements.
//
// Example YAML configuration:
//
//	executor:
//	  intent_judge:
//	    enabled: true
//	    model: "claude-haiku-4-5-20251001"
//	    max_diff_chars: 8000
type IntentJudgeConfig struct {
	// Enabled controls whether the intent judge runs after execution.
	// Default: true (when config block is present).
	Enabled *bool `yaml:"enabled,omitempty"`

	// Model is the model to use for intent evaluation. Default: "claude-haiku-4-5-20251001"
	Model string `yaml:"model,omitempty"`

	// MaxDiffChars is the maximum diff size in characters before truncation.
	// Default: 8000.
	MaxDiffChars int `yaml:"max_diff_chars,omitempty"`
}

// DefaultIntentJudgeConfig returns default intent judge configuration.
func DefaultIntentJudgeConfig() *IntentJudgeConfig {
	enabled := true
	return &IntentJudgeConfig{
		Enabled:      &enabled,
		Model:        "claude-haiku-4-5-20251001",
		MaxDiffChars: 8000,
	}
}

// PreFlightJudgeConfig configures the pre-flight issue quality judge (GH-2802).
// When enabled, Haiku evaluates each issue before dispatch and declines
// vague/question/conflicting/stale/out-of-scope issues without burning a worker slot.
//
// Example YAML configuration:
//
//	executor:
//	  pre_flight_judge:
//	    enabled: true
//	    api_key: "${ANTHROPIC_API_KEY}"
type PreFlightJudgeConfig struct {
	// Enabled controls whether the pre-flight judge runs before dispatch. Default: false.
	Enabled bool `yaml:"enabled"`

	// Deprecated: API key is no longer used. Pre-flight judge calls Claude Code subprocess
	// and bills to the operator's subscription. Field retained for backwards-compatible YAML
	// parsing; will be removed in a future major version.
	APIKey string `yaml:"api_key,omitempty"`
}
