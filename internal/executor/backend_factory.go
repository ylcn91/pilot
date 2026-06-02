package executor

import (
	"fmt"
)

// NewBackend creates a Backend instance based on configuration.
func NewBackend(config *BackendConfig) (Backend, error) {
	if config == nil {
		config = DefaultBackendConfig()
	}

	heartbeatTimeout := config.EffectiveHeartbeatTimeout()

	switch config.Type {
	case BackendTypeCodexExec, "":
		b := NewCodexExecBackend(config.CodexExec)
		b.SetHeartbeatTimeout(heartbeatTimeout)
		return b, nil

	case BackendTypeClaudeCode:
		b := NewClaudeCodeBackend(config.ClaudeCode)
		b.SetHeartbeatTimeout(heartbeatTimeout)
		// GH-2371: single-source provider routing — inject configured
		// api_base_url / api_auth_token / default_model into the CC
		// subprocess env so users don't need to also edit
		// ~/.claude/settings.json.
		b.SetProviderEnv(config.APIBaseURL, config.APIAuthToken, config.DefaultModel)
		// GH-3028: wire RSS telemetry + optional memory cap.
		b.SetSubprocessLimits(config.SubprocessLimits)
		return b, nil

	case BackendTypeOpenCode:
		return NewOpenCodeBackend(config.OpenCode), nil

	case BackendTypeQwenCode:
		b := NewQwenCodeBackend(config.QwenCode)
		b.SetHeartbeatTimeout(heartbeatTimeout)
		return b, nil

	case BackendTypeAnthropicAPI:
		return NewAnthropicBackend(config), nil

	case BackendTypeOpenAIAPI:
		return NewOpenAIBackend(config), nil

	default:
		return nil, fmt.Errorf("unknown backend type: %s", config.Type)
	}
}

// NewStageBackend builds the Backend for one pipeline stage. It clones base,
// overrides Type (and Model/Effort when the stage sets them), and reuses the
// existing NewBackend switch — no new backend wiring. A nil stage yields a
// backend equivalent to base.
func NewStageBackend(stage *StageConfig, base BackendConfig) (Backend, error) {
	cfg := base // shallow copy; only the fields we override below are touched
	if stage != nil {
		if stage.Type != "" {
			cfg.Type = stage.Type
		}
		if stage.Model != "" {
			// DefaultModel is the executor-wide model override threaded into
			// every backend by NewBackend; setting it routes the stage's model
			// regardless of which backend type it targets.
			cfg.DefaultModel = stage.Model
		}
		if stage.Effort != "" && cfg.Type == BackendTypeCodexExec {
			// codex-exec is the only backend with a config-level effort knob.
			ce := CodexExecConfig{}
			if cfg.CodexExec != nil {
				ce = *cfg.CodexExec
			}
			ce.Effort = stage.Effort
			cfg.CodexExec = &ce
		}
	}
	return NewBackend(&cfg)
}

// NewBackendFromType creates a Backend instance using default config for the type.
func NewBackendFromType(backendType string) (Backend, error) {
	config := DefaultBackendConfig()
	config.Type = backendType
	return NewBackend(config)
}
