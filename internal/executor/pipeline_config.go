package executor

import "fmt"

// backendTypeCodexAppServer is the codex-app-server runtime. It is NOT a
// runnable Backend (NewBackend has no case for it), so it is rejected as a
// pipeline stage type.
const backendTypeCodexAppServer = "codex-app-server"

// PipelineConfig configures a per-phase backend handoff so a single task can be
// planned by one backend, executed by another, and reviewed by a third. Any
// stage may be nil, in which case that phase falls back to the run's primary
// backend (BackendConfig.Type). A nil PipelineConfig preserves today's
// single-backend behavior exactly.
type PipelineConfig struct {
	Plan    *StageConfig `yaml:"plan,omitempty"`
	Execute *StageConfig `yaml:"execute,omitempty"`
	Review  *StageConfig `yaml:"review,omitempty"`
}

// StageConfig selects the backend (and optional model/effort overrides) for one
// pipeline phase. Type must be a runnable Backend constant.
type StageConfig struct {
	Type   string `yaml:"type,omitempty"`
	Model  string `yaml:"model,omitempty"`
	Effort string `yaml:"effort,omitempty"`
}

// runnablePipelineBackends are the backend types a pipeline stage may target.
// codex-app-server is intentionally absent: it is not a runnable Backend (it has
// no NewBackend switch case); use codex-exec for a pipeline stage instead.
var runnablePipelineBackends = map[string]bool{
	BackendTypeCodexExec:    true,
	BackendTypeClaudeCode:   true,
	BackendTypeQwenCode:     true,
	BackendTypeAnthropicAPI: true,
	BackendTypeOpenAIAPI:    true,
	BackendTypeOpenCode:     true,
}

// Validate checks each non-nil stage targets a runnable backend. A nil pipeline
// or nil stage is valid (it falls back to the primary backend).
func (p *PipelineConfig) Validate() error {
	if p == nil {
		return nil
	}
	stages := []struct {
		name  string
		stage *StageConfig
	}{
		{"plan", p.Plan},
		{"execute", p.Execute},
		{"review", p.Review},
	}
	for _, s := range stages {
		if s.stage == nil {
			continue
		}
		if s.stage.Type == "" {
			return fmt.Errorf("pipeline.%s.type is required when the stage is present", s.name)
		}
		if s.stage.Type == backendTypeCodexAppServer {
			return fmt.Errorf("pipeline.%s.type %q is not a runnable Backend; use codex-exec for a pipeline stage", s.name, s.stage.Type)
		}
		if !runnablePipelineBackends[s.stage.Type] {
			return fmt.Errorf("pipeline.%s.type %q is not a known backend (use one of: claude-code, codex-exec, qwen-code, anthropic-api, openai-api, opencode)", s.name, s.stage.Type)
		}
	}
	return nil
}
